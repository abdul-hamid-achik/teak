package highlight

import (
	"bytes"
	"context"
	"image/color"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"teak/internal/text"
	"teak/internal/ui"
)

// StyledToken represents a token with its lipgloss style.
type StyledToken struct {
	Text  string
	Style lipgloss.Style

	// Prefix and Suffix are the escape sequences Style.Render would wrap Text
	// in. Rendering a token by concatenating them is equivalent to calling
	// Render but skips lipgloss's generic per-call pipeline (border, margin,
	// padding, width and alignment checks), which dominated frame time: a full
	// viewport spent ~3.5ms and 17k allocations in Style.Render alone.
	//
	// FastSGR reports whether that equivalence was verified for this style.
	// Styles whose output depends on the text (width, alignment) do not qualify
	// and must go through Render.
	Prefix  string
	Suffix  string
	FastSGR bool
}

// WithBackground returns a token whose fast SGR path exactly matches the same
// lipgloss style with background applied. Call this while preparing cached
// tokens, not during rendering: deriving the escape pair intentionally probes
// lipgloss for byte-for-byte equivalence.
func (t StyledToken) WithBackground(background color.Color) StyledToken {
	t.Style = t.Style.Background(background)
	pair := deriveSGR(t.Style)
	t.Prefix = pair.prefix
	t.Suffix = pair.suffix
	t.FastSGR = pair.fast
	return t
}

// Render writes the token's styled text. It uses the precomputed escape
// sequences when they are known to be equivalent, and falls back to lipgloss
// otherwise.
//
// Text containing a tab always takes the slow path: lipgloss expands tabs to
// spaces as part of rendering, so concatenating the escape sequences around a
// raw tab would emit different bytes. Callers normally expand tabs themselves
// before reaching here, making this a rare fallback rather than a hot path.
func (t StyledToken) Render(text string) string {
	if t.FastSGR && !strings.ContainsRune(text, '\t') {
		return t.Prefix + text + t.Suffix
	}
	return t.Style.Render(text)
}

// WriteTo renders a token directly into a string writer. The fast path writes
// its precomputed SGR pieces separately, avoiding one temporary concatenated
// string per token in the viewport frame. It is byte-for-byte equivalent to
// Render; styles that depend on their input still use lipgloss.Render.
func (t StyledToken) WriteTo(w io.StringWriter, text string) {
	if t.FastSGR && !strings.ContainsRune(text, '\t') {
		_, _ = w.WriteString(t.Prefix)
		_, _ = w.WriteString(text)
		_, _ = w.WriteString(t.Suffix)
		return
	}
	_, _ = w.WriteString(t.Style.Render(text))
}

// ViewportSnapshot is an immutable slice of a rope captured by the UI goroutine
// before a viewport tokenization command is started. Tokenization commands must
// not retain a mutable Buffer: an edit may replace its rope while the command is
// still running.
type ViewportSnapshot struct {
	Content   []byte
	LineCount int
	StartLine int
	ViewStart int
	ViewEnd   int
}

// TokenBatch carries a contiguous tokenized range without padding it to the
// document's total line count. Viewport work must stay proportional to what is
// visible: padding a 64-million-line generated file would otherwise allocate
// well over a gigabyte of empty slice headers.
type TokenBatch struct {
	StartLine  int
	Lines      [][]StyledToken
	TotalLines int
}

type tokenRange struct {
	start int
	end   int
}

// lineEdit maps current document lines back to the dense cache that existed
// before a structural edit. Keeping these edits lazy avoids copying a
// document-sized token slice from Bubble Tea's Update path on every newline.
type lineEdit struct {
	start        int
	newLineCount int
	delta        int
}

const maxPendingLineEdits = 256

// Highlighter provides syntax highlighting for a file.
type Highlighter struct {
	lexer          chroma.Lexer
	lines          [][]StyledToken
	sparseLines    map[int][]StyledToken
	lineCount      int
	coveredRanges  []tokenRange
	dirty          bool
	styleMap       map[chroma.TokenType]lipgloss.Style
	sgrMap         map[chroma.TokenType]sgrPair
	editorSGR      sgrPair
	theme          ui.Theme
	tokenizedStart int
	tokenizedEnd   int
	pendingEdits   []lineEdit
}

// New creates a new Highlighter based on the filename for language detection.
func New(filename string, theme ui.Theme) *Highlighter {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	styleMap := buildStyleMap(theme)
	return &Highlighter{
		lexer:          lexer,
		dirty:          true,
		styleMap:       styleMap,
		sgrMap:         buildSGRMap(styleMap),
		editorSGR:      deriveSGR(theme.Editor),
		theme:          theme,
		tokenizedStart: -1,
		tokenizedEnd:   -1,
	}
}

// Tokenize processes the full buffer content and caches per-line tokens.
func (h *Highlighter) Tokenize(content []byte) {
	h.lines = h.tokenizeContent(content, -1, -1)
	h.sparseLines = nil
	h.lineCount = len(h.lines)
	h.coveredRanges = []tokenRange{{start: 0, end: len(h.lines)}}
	h.tokenizedStart = 0
	h.tokenizedEnd = len(h.lines)
	h.pendingEdits = nil
	h.dirty = false
}

// TokenizeToLines tokenizes content and returns the result without mutating state.
// Safe for use from goroutines (lexer and styleMap are immutable after creation).
func (h *Highlighter) TokenizeToLines(content []byte) [][]StyledToken {
	lines, _ := h.TokenizeToLinesContext(context.Background(), content)
	return lines
}

// TokenizeToLinesContext is the cancellable variant of TokenizeToLines. A
// false return means the context was canceled and the returned lines must not
// be installed.
func (h *Highlighter) TokenizeToLinesContext(ctx context.Context, content []byte) ([][]StyledToken, bool) {
	return h.tokenizeContentContext(ctx, content, -1, -1)
}

// TokenizeViewportToLines tokenizes content but only materializes styled tokens
// for lines in [viewStart-margin, viewEnd+margin]. The lexer still runs on full
// content to maintain correct state.
func (h *Highlighter) TokenizeViewportToLines(content []byte, viewStart, viewEnd int) [][]StyledToken {
	lines, _ := h.TokenizeViewportToLinesContext(context.Background(), content, viewStart, viewEnd)
	return lines
}

// TokenizeViewportToLinesContext is the cancellable variant of
// TokenizeViewportToLines.
func (h *Highlighter) TokenizeViewportToLinesContext(ctx context.Context, content []byte, viewStart, viewEnd int) ([][]StyledToken, bool) {
	return h.tokenizeContentContext(ctx, content, viewStart, viewEnd)
}

// TokenizeViewport tokenizes only the viewport region of a buffer with a margin
// for context. This is much faster than tokenizing the entire file for large files.
// The margin helps handle multi-line constructs (comments, strings) that cross
// viewport boundaries.
//
// Performance: This method is O(viewport_size + margin) instead of O(file_size).
// For a 100K line file, this is ~145x faster than full tokenization (1.8ms vs 264ms).
//
// Memory Trade-off: Returns a slice sized to buf.LineCount() for compatibility
// with existing code. This wastes ~8 bytes per line outside the viewport (nil pointers).
// For a 1M line file, that's ~8MB of wasted memory. This is acceptable for most
// use cases but may need optimization for extremely large files (>500K lines).
//
// Multi-line Constructs: The 200-line margin handles 99%+ of real-world cases.
// Very long multi-line strings/comments (>200 lines) may have incorrect highlighting
// at viewport boundaries. This is an acceptable trade-off for the performance gain.
//
// Thread Safety: This convenience method reads the Buffer while it captures a
// snapshot, so call it only while the Buffer is not being mutated. For an async
// tea.Cmd, call CaptureViewport on the UI goroutine and pass the result to
// TokenizeViewportSnapshot instead. The returned slice should be merged into
// the highlighter's cache by the caller (usually in the main Bubble Tea Update
// loop).
//
// Returns nil if buf is nil. Returns empty slice if viewStart >= viewEnd.
func (h *Highlighter) TokenizeViewport(buf *text.Buffer, viewStart, viewEnd int) [][]StyledToken {
	if buf == nil {
		return nil
	}
	snapshot := CaptureViewport(buf.Rope(), viewStart, viewEnd)
	lines, _ := h.TokenizeViewportSnapshot(context.Background(), snapshot)
	return lines
}

// CaptureViewport copies the needed viewport region from an immutable rope.
// Call it on the UI goroutine before starting an asynchronous tokenization job.
func CaptureViewport(rope *text.Rope, viewStart, viewEnd int) ViewportSnapshot {
	if rope == nil {
		return ViewportSnapshot{}
	}

	// Handle edge cases gracefully
	if viewStart < 0 {
		viewStart = 0
	}
	if viewEnd <= viewStart {
		viewEnd = viewStart + 1
	}

	const margin = 200 // Large margin for multi-line constructs

	lineCount := rope.LineCount()
	startLine := min(lineCount, max(0, viewStart-margin))
	endLine := min(lineCount, max(startLine, viewEnd+margin))

	// Extract content from just the target lines
	var content bytes.Buffer
	// Rough estimate: average 80 bytes per line
	content.Grow((endLine - startLine) * 80)

	for i := startLine; i < endLine; i++ {
		content.Write(rope.Line(i))
		content.WriteByte('\n')
	}
	return ViewportSnapshot{
		Content:   content.Bytes(),
		LineCount: lineCount,
		StartLine: startLine,
		ViewStart: viewStart,
		ViewEnd:   viewEnd,
	}
}

// TokenizeViewportSnapshotBatch tokenizes a snapshot captured by
// CaptureViewport. It never reads a Buffer or Rope, so it is safe to run in a
// tea.Cmd, and returns only the captured range rather than padding to every
// source line.
func (h *Highlighter) TokenizeViewportSnapshotBatch(ctx context.Context, snapshot ViewportSnapshot) (TokenBatch, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TokenBatch{}, false
	}
	if snapshot.LineCount == 0 {
		return TokenBatch{}, true
	}

	// Tokenize the extracted content.
	tokens, complete := h.tokenizeContentContext(ctx, snapshot.Content, snapshot.ViewStart-snapshot.StartLine, snapshot.ViewEnd-snapshot.StartLine)
	if !complete {
		return TokenBatch{}, false
	}

	// streamTokenizeContext starts at the beginning of the snapshot so the
	// lexer can establish state for multiline constructs. Its output therefore
	// contains nil placeholders before the actual viewport context. They are
	// not tokenized lines and must not become cache coverage: doing so makes a
	// later scroll through that gap incorrectly skip a required retokenization.
	const tokenizeMargin = 50
	viewStart := snapshot.ViewStart - snapshot.StartLine
	viewEnd := snapshot.ViewEnd - snapshot.StartLine
	rangeStart := max(0, viewStart-tokenizeMargin)
	rangeEnd := max(rangeStart, viewEnd+tokenizeMargin)
	if rangeStart > len(tokens) {
		rangeStart = len(tokens)
	}
	if rangeEnd > len(tokens) {
		rangeEnd = len(tokens)
	}

	return TokenBatch{
		StartLine:  snapshot.StartLine + rangeStart,
		Lines:      tokens[rangeStart:rangeEnd],
		TotalLines: snapshot.LineCount,
	}, true
}

// TokenizeViewportSnapshot preserves the older padded return shape for callers
// that need it. Editor commands use TokenizeViewportSnapshotBatch so opening a
// very large file never allocates a document-sized sparse slice.
func (h *Highlighter) TokenizeViewportSnapshot(ctx context.Context, snapshot ViewportSnapshot) ([][]StyledToken, bool) {
	batch, complete := h.TokenizeViewportSnapshotBatch(ctx, snapshot)
	if !complete {
		return nil, false
	}
	result := make([][]StyledToken, batch.TotalLines)
	for i, line := range batch.Lines {
		if at := batch.StartLine + i; at >= 0 && at < len(result) {
			result[at] = line
		}
	}

	return result, true
}

// SetLines sets cached lines from a full tokenization result, replacing the cache entirely.
func (h *Highlighter) SetLines(lines [][]StyledToken) {
	h.lines = lines
	h.sparseLines = nil
	h.lineCount = len(lines)
	h.coveredRanges = []tokenRange{{start: 0, end: len(lines)}}
	h.tokenizedStart = 0
	h.tokenizedEnd = len(lines)
	h.pendingEdits = nil
	h.dirty = false
}

// MergeLines merges a partial (viewport-scoped) tokenization result into the
// existing cache. Lines with tokens in the new result overwrite old data;
// lines that are nil/empty in the new result keep their old cached tokens.
func (h *Highlighter) MergeLines(lines [][]StyledToken) {
	h.MergeBatch(TokenBatch{Lines: lines, TotalLines: len(lines)})
}

// MergeBatch merges a viewport result without resizing the cache to source
// line count. Prefix highlighting may remain in h.lines while distant viewport
// ranges live in a sparse map until a bounded full pass replaces the cache.
func (h *Highlighter) MergeBatch(batch TokenBatch) {
	if batch.TotalLines > h.lineCount {
		h.lineCount = batch.TotalLines
	}
	if h.sparseLines == nil {
		h.sparseLines = make(map[int][]StyledToken)
	}
	h.addCoveredRange(batch.StartLine, batch.StartLine+len(batch.Lines))

	// Track the actual range of tokenized lines
	mergedStart := -1
	mergedEnd := -1
	for i, line := range batch.Lines {
		if len(line) > 0 {
			lineNum := batch.StartLine + i
			if lineNum < 0 || (batch.TotalLines > 0 && lineNum >= batch.TotalLines) {
				continue
			}
			h.sparseLines[lineNum] = line
			if mergedStart == -1 {
				mergedStart = lineNum
			}
			mergedEnd = lineNum + 1
		}
	}

	// Update tokenized range to include the newly merged region
	if mergedStart >= 0 {
		if h.tokenizedStart < 0 || mergedStart < h.tokenizedStart {
			h.tokenizedStart = mergedStart
		}
		if mergedEnd > h.tokenizedEnd {
			h.tokenizedEnd = mergedEnd
		}
	}
	h.dirty = false
}

func (h *Highlighter) addCoveredRange(start, end int) {
	if end <= start {
		return
	}
	merged := tokenRange{start: start, end: end}
	ranges := make([]tokenRange, 0, len(h.coveredRanges)+1)
	inserted := false
	for _, existing := range h.coveredRanges {
		if existing.end < merged.start {
			ranges = append(ranges, existing)
			continue
		}
		if merged.end < existing.start {
			if !inserted {
				ranges = append(ranges, merged)
				inserted = true
			}
			ranges = append(ranges, existing)
			continue
		}
		merged.start = min(merged.start, existing.start)
		merged.end = max(merged.end, existing.end)
	}
	if !inserted {
		ranges = append(ranges, merged)
	}
	h.coveredRanges = ranges
}

// CoversRange reports whether every line in [start, end) has been tokenized.
// It deliberately tracks disjoint sparse viewport batches rather than treating
// their min/max as one continuous range with hidden cache gaps.
func (h *Highlighter) CoversRange(start, end int) bool {
	if end <= start {
		return true
	}
	for _, covered := range h.coveredRanges {
		if covered.start > start {
			return false
		}
		if covered.end >= end {
			return true
		}
	}
	return false
}

// TokenizedRange returns the range of lines that have been tokenized.
// Returns (-1, -1) if no viewport-scoped tokenization has been done.
func (h *Highlighter) TokenizedRange() (int, int) {
	return h.tokenizedStart, h.tokenizedEnd
}

// TokenizePrefix synchronously tokenizes the first maxLines of content.
// Used to provide instant highlighting on file open (first frame has color).
func (h *Highlighter) TokenizePrefix(content []byte, maxLines int) {
	// Find byte offset for prefix
	end := len(content)
	lines := 0
	for i, b := range content {
		if b == '\n' {
			lines++
			if lines >= maxLines {
				end = i + 1
				break
			}
		}
	}

	result, _ := h.streamTokenizeContext(context.Background(), string(content[:end]), -1, -1)
	h.lines = result
	h.sparseLines = nil
	h.lineCount = len(result)
	h.coveredRanges = []tokenRange{{start: 0, end: len(result)}}
	h.tokenizedStart = 0
	h.tokenizedEnd = len(result)
	h.pendingEdits = nil
	h.dirty = false
}

func (h *Highlighter) tokenizeContent(content []byte, viewStart, viewEnd int) [][]StyledToken {
	lines, _ := h.tokenizeContentContext(context.Background(), content, viewStart, viewEnd)
	return lines
}

func (h *Highlighter) tokenizeContentContext(ctx context.Context, content []byte, viewStart, viewEnd int) ([][]StyledToken, bool) {
	return h.streamTokenizeContext(ctx, string(content), viewStart, viewEnd)
}

// streamTokenize uses Chroma's iterator lazily, streaming tokens and splitting
// into lines on the fly. When a viewport range is specified, it stops consuming
// the lexer after passing viewEnd+margin, avoiding lexing the full file tail.
func (h *Highlighter) streamTokenizeContext(ctx context.Context, content string, viewStart, viewEnd int) ([][]StyledToken, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, false
	}
	iterator, err := h.lexer.Tokenise(nil, content)
	if err != nil {
		return nil, true
	}

	const tokenizeMargin = 50
	rangeStart := -1
	rangeEnd := -1
	if viewStart >= 0 && viewEnd >= 0 {
		rangeStart = max(0, viewStart-tokenizeMargin)
		rangeEnd = viewEnd + tokenizeMargin
	}

	var lines [][]StyledToken
	var currentLine []StyledToken
	lineNum := 0

	inRange := func() bool {
		return rangeStart < 0 || (lineNum >= rangeStart && lineNum <= rangeEnd)
	}

	for tok := iterator(); tok.Type != chroma.EOFType; tok = iterator() {
		if err := ctx.Err(); err != nil {
			return nil, false
		}
		if tok.Value == "" {
			continue
		}

		val := tok.Value
		style := lipgloss.Style{}
		var sgr sgrPair
		styleResolved := false

		for {
			if err := ctx.Err(); err != nil {
				return nil, false
			}
			nlIdx := strings.IndexByte(val, '\n')
			if nlIdx < 0 {
				break
			}
			// Text before the newline
			part := val[:nlIdx]
			if len(part) > 0 && inRange() {
				if !styleResolved {
					style, sgr = h.resolveToken(tok.Type)
					styleResolved = true
				}
				currentLine = append(currentLine, newStyledToken(part, style, sgr))
			}
			lines = append(lines, currentLine)
			currentLine = nil
			lineNum++
			val = val[nlIdx+1:]

			// Early exit: past viewport range, stop lexing
			if rangeEnd >= 0 && lineNum > rangeEnd {
				return lines, true
			}
		}
		// Remaining text (no newline)
		if len(val) > 0 && inRange() {
			if !styleResolved {
				style, sgr = h.resolveToken(tok.Type)
				styleResolved = true
			}
			currentLine = append(currentLine, newStyledToken(val, style, sgr))
		}
	}

	if currentLine != nil {
		lines = append(lines, currentLine)
	}
	if len(lines) == 0 {
		lines = append(lines, nil)
	}
	return lines, true
}

// Line returns the styled tokens for a given line number (0-based).
// Returns nil if the line hasn't been tokenized.
func (h *Highlighter) Line(lineNum int) []StyledToken {
	if lineNum < 0 {
		return nil
	}
	if line, ok := h.sparseLines[lineNum]; ok {
		return line
	}
	physicalLine, ok := h.denseLine(lineNum)
	if !ok || h.lines == nil || physicalLine >= len(h.lines) {
		return nil
	}
	return h.lines[physicalLine]
}

// denseLine maps a current logical line to the old dense cache. Structural
// edits are applied in reverse order. A line inside a replaced range is
// unavailable until asynchronous tokenization installs fresh tokens.
func (h *Highlighter) denseLine(line int) (int, bool) {
	for i := len(h.pendingEdits) - 1; i >= 0; i-- {
		edit := h.pendingEdits[i]
		newEnd := edit.start + edit.newLineCount
		if line < edit.start {
			continue
		}
		if line < newEnd {
			return 0, false
		}
		line -= edit.delta
	}
	if line < 0 {
		return 0, false
	}
	return line, true
}

// LineCount returns the number of tokenized lines.
func (h *Highlighter) LineCount() int {
	if h.lineCount > 0 {
		return h.lineCount
	}
	return 0
}

// InvalidateEdited marks the cache stale after an edit spanning buffer lines
// [startLine, endLine] that changed the document's line count by lineDelta.
//
// Unlike Invalidate it keeps the existing tokens for painting, having first
// moved them to follow the edit: lines above it are untouched, the edited lines
// themselves are dropped because their tokens are certainly wrong, and lines
// below it shift by lineDelta. Coverage is still cleared, so the editor knows
// the range is not refreshed and schedules a real tokenization — the concern
// that motivated clearing everything is preserved, while the viewport keeps
// rendering plausible colours in the meantime instead of flashing to plain text
// on every keystroke.
//
// The tokens on screen may be briefly imprecise, which for the ~150ms until the
// async pass lands is far less jarring than losing all syntax colour.
func (h *Highlighter) InvalidateEdited(startLine, endLine, lineDelta int) {
	if h.lines == nil && h.sparseLines == nil {
		h.Invalidate()
		return
	}
	if startLine < 0 {
		startLine = 0
	}
	if endLine < startLine {
		endLine = startLine
	}

	h.shiftLines(startLine, endLine, lineDelta)
	h.shiftSparseLines(startLine, endLine, lineDelta)

	if h.lineCount > 0 {
		h.lineCount = max(h.lineCount+lineDelta, 0)
	}
	// Coverage must not survive: it is what tells the editor a range is already
	// tokenized, and none of it is trustworthy after an edit.
	h.coveredRanges = nil
	h.tokenizedStart = -1
	h.tokenizedEnd = -1
	h.dirty = true
}

// shiftLines moves the dense token cache to follow an edit.
func (h *Highlighter) shiftLines(startLine, endLine, lineDelta int) {
	if h.lines == nil {
		return
	}
	// Typing a character neither adds nor removes lines, which is the common
	// case: only the edited lines need dropping, with no reallocation. Resolve
	// through pending structural edits because the dense cache may still use
	// coordinates from an older document version.
	if lineDelta == 0 {
		for line := startLine; line <= endLine; line++ {
			if physicalLine, ok := h.denseLine(line); ok && physicalLine < len(h.lines) {
				h.lines[physicalLine] = nil
			}
		}
		return
	}

	if len(h.pendingEdits) >= maxPendingLineEdits {
		// A full pass will be scheduled by the editor. Dropping the dense cache
		// is preferable to retaining an unbounded edit history or making every
		// render walk thousands of transformations.
		h.lines = nil
		h.pendingEdits = nil
		return
	}
	h.pendingEdits = append(h.pendingEdits, lineEdit{
		start:        startLine,
		newLineCount: max(0, endLine-startLine+1+lineDelta),
		delta:        lineDelta,
	})
}

// shiftSparseLines moves the sparse token cache to follow an edit.
func (h *Highlighter) shiftSparseLines(startLine, endLine, lineDelta int) {
	if h.sparseLines == nil {
		return
	}
	shifted := make(map[int][]StyledToken, len(h.sparseLines))
	for line, tokens := range h.sparseLines {
		if line >= startLine && line <= endLine {
			continue // certainly wrong now
		}
		target := line
		if line > endLine {
			target = line + lineDelta
		}
		if target >= 0 {
			shifted[target] = tokens
		}
	}
	h.sparseLines = shifted
}

// Invalidate marks the cached tokens as stale.
func (h *Highlighter) Invalidate() {
	// Do not retain sparse lines or coverage across an edit. Keeping either
	// makes a later return to the same viewport render old tokens and tells the
	// editor that a range has already been refreshed. Clearing maps/slices is
	// constant-time and avoids any allocation proportional to document size.
	h.lines = nil
	h.sparseLines = nil
	h.pendingEdits = nil
	h.lineCount = 0
	h.coveredRanges = nil
	h.tokenizedStart = -1
	h.tokenizedEnd = -1
	h.dirty = true
}

// IsDirty returns true if tokens need re-generation.
func (h *Highlighter) IsDirty() bool {
	return h.dirty
}

// resolveToken returns the style for a token type along with its precomputed
// escape sequences, walking up the token hierarchy to find a match.
func (h *Highlighter) resolveToken(tt chroma.TokenType) (lipgloss.Style, sgrPair) {
	for t := tt; t > 0; t = t.Parent() {
		if style, ok := h.styleMap[t]; ok {
			return style, h.sgrMap[t]
		}
	}
	return h.theme.Editor, h.editorSGR
}
