package editor

import (
	"context"
	"sort"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/clipboard"
	"teak/internal/editor/overlays"
	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

// TokenizeCompleteMsg carries the result of async tokenization.
//
// Performance Note: When Partial is true (viewport tokenization), only the
// visible region and a margin around it are tokenized. This provides 145x
// speedup for large files (1.8ms vs 264ms for 10K lines).
type TokenizeCompleteMsg struct {
	EditorID   uint64
	Version    int
	Generation uint64
	Lines      [][]highlight.StyledToken
	Batch      highlight.TokenBatch
	Partial    bool // true when result is from viewport-only tokenization
}

const (
	initialHighlightPrefixBytes = 64 << 10
	maxFullHighlightBytes       = 8 << 20
	maxFullHighlightLines       = 250_000
	// Interactive edits use a viewport pass sooner than initial loading. A full
	// Chroma pass is correct but can create hundreds of MiB of temporary data
	// for an otherwise ordinary multi-thousand-line file; off-screen ranges are
	// refreshed on demand when the viewport reaches them.
	maxInteractiveHighlightBytes = 1 << 20
	maxInteractiveHighlightLines = 8_000
	// asyncPasteThresholdBytes keeps terminal bracketed pastes and OS clipboard
	// results large enough to affect a frame out of Update. The hard 16 MiB
	// limit remains owned by clipboard.MaxClipboardBytes.
	asyncPasteThresholdBytes = 64 << 10
	// MaxSynchronousMultilineEditLines bounds line-by-line comment/indent
	// operations. On Apple M5, the worst-case indent benchmark is ~0.5ms at
	// this budget; 10,000 lines took ~305ms and allocated ~1.5GiB.
	MaxSynchronousMultilineEditLines = 128
)

// RequestCompletionCmd is a command that triggers completion from the app layer.
type RequestCompletionCmd struct{}

// Diagnostic represents a diagnostic message from an LSP server (decoupled from LSP types).
type Diagnostic struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Severity  int // 1=error, 2=warning, 3=info, 4=hint
	Message   string
}

// BreakpointClickMsg is emitted when the user clicks the line number gutter.
type BreakpointClickMsg struct{ Line int }

// OccurrenceSearchLimitMsg lets the app explain why a synchronous
// multi-cursor shortcut was rejected on an exceptionally large document.
type OccurrenceSearchLimitMsg struct {
	EditorID uint64
	MaxBytes int
}

// MultilineEditLimitMsg lets the app explain a rejected line-by-line editor
// command without performing an unbounded synchronous structural transform.
type MultilineEditLimitMsg struct {
	EditorID  uint64
	Operation string
	MaxLines  int
}

// RetokenizeMsg triggers syntax re-tokenization after edits or scrolls.
//
// Performance Strategy:
//   - Edit-triggered (ViewportOnly=false): Full file tokenization for small
//     documents, or a viewport-only pass for large interactive documents. The
//     latter keeps the current screen responsive; off-screen ranges are
//     refreshed when they become visible.
//   - Scroll-triggered (ViewportOnly=true): Viewport-only tokenization
//     provides 145x speedup for large files. Only the visible region plus a
//     margin for multi-line construct context is materialized.
//
// Debouncing:
//   - Edit-triggered: 150ms debounce (scheduleRetokenize)
//   - Scroll-triggered: Immediate (scheduleRetokenizeImmediate)
//     Scrolling should feel instant, so no debounce.
type RetokenizeMsg struct {
	EditorID     uint64
	Version      int  // Buffer version (for staleness detection)
	ViewportOnly bool // true for scroll-triggered (fast), false for edit-triggered (full)
}

// Editor is a sub-model managing text editing with mouse and keyboard.
type Editor struct {
	id                      uint64
	Buffer                  *text.Buffer
	Viewport                Viewport
	Config                  Config
	theme                   ui.Theme
	dragging                bool
	Highlighter             *highlight.Highlighter
	Diagnostics             []Diagnostic
	autocomplete            overlays.Autocomplete
	hover                   overlays.Hover
	signatureHelp           overlays.SignatureHelp
	contextMenu             ContextMenu
	find                    FindModel
	HasLSP                  bool
	TriggerCharacters       []string    // from LSP server capabilities
	DebugGutter             *GutterOpts // set by app when debugging
	PluginHighlights        map[int][]HighlightRange
	PluginHighlightVersion  int
	Folds                   FoldState   // code folding state
	Wrap                    *WrapLayout // word wrap layout (nil when disabled)
	lastVersion             int
	tokenizer               *tokenizeScheduler
	lastClickTime           time.Time
	lastClickPos            text.Position
	clickCount              int
	clipboardGeneration     uint64
	clipboardCopyGeneration uint64
	pasteGeneration         uint64
	wrapDegraded            bool
	wrapLayoutVersion       int
	wrapLayoutTabSize       int
}

// tokenizeScheduler is shared by value-copied Editor models. Bubble Tea runs
// Update serially, so only its commands observe the cancellation context.
// Every started job gets a strictly increasing generation; a completion can be
// committed only if it belongs to the newest generation.
type tokenizeScheduler struct {
	full     tokenizeLane
	viewport tokenizeLane
}

type tokenizeLane struct {
	generation uint64
	cancel     context.CancelFunc
}

var nextEditorID atomic.Uint64

// New creates a new Editor with the given buffer, theme, and config.
// The first screenful is tokenized synchronously so the first render has color.
// Call ScheduleInitialTokenize to kick off the full async tokenization.
func New(buf *text.Buffer, theme ui.Theme, cfg Config) Editor {
	var hl *highlight.Highlighter
	if buf.FilePath != "" {
		hl = highlight.New(buf.FilePath, theme)
		// Synchronously tokenize first screenful (~60 lines) so the first
		// frame renders with syntax highlighting, avoiding the unstyled flash.
		// The prefix is bounded so a single gigantic first line cannot freeze
		// the UI or duplicate an entire file during construction.
		hl.TokenizePrefix(buf.Rope().PrefixLines(60, initialHighlightPrefixBytes), 60)
	}

	return Editor{
		id:                     nextEditorID.Add(1),
		Buffer:                 buf,
		Viewport:               Viewport{TabSize: cfg.TabSize},
		Config:                 cfg,
		theme:                  theme,
		Highlighter:            hl,
		PluginHighlights:       make(map[int][]HighlightRange),
		PluginHighlightVersion: -1,
		autocomplete:           overlays.NewAutocomplete(theme),
		hover:                  overlays.NewHover(theme),
		signatureHelp:          overlays.NewSignatureHelp(theme),
		contextMenu:            NewContextMenu(theme),
		find:                   NewFindModel(theme),
		lastVersion:            -1,
		tokenizer:              &tokenizeScheduler{},
	}
}

// ScheduleInitialTokenize returns a command that runs full async tokenization
// for bounded documents, or a sparse viewport pass for exceptionally large
// ones. The prefix was already tokenized synchronously in New().
func (e *Editor) ScheduleInitialTokenize() tea.Cmd {
	if e.Highlighter == nil || e.Buffer == nil {
		return nil
	}
	hl := e.Highlighter
	rope := e.Buffer.Rope()
	editorID := e.id
	version := e.Buffer.Version()
	e.lastVersion = version
	if shouldUseSparseHighlight(rope) {
		ctx, generation := e.beginViewportTokenize()
		viewStart, viewEnd := e.visibleTokenRange()
		snapshot := highlight.CaptureViewport(rope, viewStart, viewEnd)
		return tokenizeViewportCmd(ctx, hl, snapshot, editorID, version, generation)
	}
	ctx, generation := e.beginFullTokenize()
	return func() tea.Msg {
		content, err := rope.BytesContext(ctx)
		if err != nil {
			return nil
		}
		lines, complete := hl.TokenizeToLinesContext(ctx, content)
		if !complete {
			return nil
		}
		return TokenizeCompleteMsg{EditorID: editorID, Version: version, Generation: generation, Lines: lines}
	}
}

func shouldUseSparseHighlight(rope *text.Rope) bool {
	return rope != nil && (rope.Len() > maxFullHighlightBytes || rope.LineCount() > maxFullHighlightLines)
}

func shouldUseInteractiveSparseHighlight(rope *text.Rope) bool {
	return rope != nil && (rope.Len() > maxInteractiveHighlightBytes || rope.LineCount() > maxInteractiveHighlightLines)
}

func tokenizeViewportCmd(ctx context.Context, hl *highlight.Highlighter, snapshot highlight.ViewportSnapshot, editorID uint64, version int, generation uint64) tea.Cmd {
	return func() tea.Msg {
		batch, complete := hl.TokenizeViewportSnapshotBatch(ctx, snapshot)
		if !complete {
			return nil
		}
		return TokenizeCompleteMsg{
			EditorID: editorID, Version: version, Generation: generation,
			Lines: batch.Lines, Batch: batch, Partial: true,
		}
	}
}

// ID returns the stable identity carried by async editor messages.
func (e Editor) ID() uint64 {
	return e.id
}

func (e *Editor) beginFullTokenize() (context.Context, uint64) {
	if e.tokenizer == nil {
		e.tokenizer = &tokenizeScheduler{}
	}
	// A new full pass represents the newest buffer version. It supersedes both
	// lanes, including a viewport snapshot captured from the previous version.
	e.invalidateTokenizeLane(&e.tokenizer.full)
	e.invalidateTokenizeLane(&e.tokenizer.viewport)
	ctx, cancel := context.WithCancel(context.Background())
	e.tokenizer.full.cancel = cancel
	return ctx, e.tokenizer.full.generation
}

func (e *Editor) beginViewportTokenize() (context.Context, uint64) {
	if e.tokenizer == nil {
		e.tokenizer = &tokenizeScheduler{}
	}
	// Scrolling coalesces only other viewport work. A full pass for the same
	// version remains useful because it fills every cache gap.
	e.invalidateTokenizeLane(&e.tokenizer.viewport)
	ctx, cancel := context.WithCancel(context.Background())
	e.tokenizer.viewport.cancel = cancel
	return ctx, e.tokenizer.viewport.generation
}

func (e *Editor) invalidateTokenizeLane(lane *tokenizeLane) {
	if lane.cancel != nil {
		lane.cancel()
	}
	lane.generation++
}

// SetSize sets the available editor dimensions.
func (e *Editor) SetSize(width, height int) {
	unchangedSize := e.Viewport.Width == width && e.Viewport.Height == height
	e.Viewport.Width = width
	e.Viewport.Height = height
	if !e.Config.WordWrap || e.Buffer == nil {
		e.Wrap = nil
		e.wrapDegraded = false
		e.Viewport.WrapScrollY = 0
		return
	}

	if unchangedSize && e.Wrap != nil && !e.wrapDegraded &&
		e.wrapLayoutVersion == e.Buffer.Version() && e.wrapLayoutTabSize == e.Config.TabSize {
		return
	}

	if e.Config.WordWrap {
		metrics := computeGutterMetrics(e.Buffer.LineCount(), e.DebugGutter, false)
		baseTextWidth := metrics.textWidth(width)
		reserveScrollbar := e.wrapLikelyNeedsScrollbar(baseTextWidth, height)
		wrapWidth := baseTextWidth
		if reserveScrollbar {
			wrapWidth = max(1, wrapWidth-1)
		}
		if e.Wrap == nil {
			e.Wrap = NewWrapLayoutWithTabSize(e.Buffer.Line, e.Buffer.LineCount(), wrapWidth, e.Config.TabSize)
		} else {
			e.Wrap.Rebuild(e.Buffer.Line, e.Buffer.LineCount(), wrapWidth)
		}
		if e.Wrap.Degraded() {
			e.disableWordWrapLayout()
			return
		}
		// The heuristic covers large buffers without reading their lines. For a
		// compact document whose wrapping alone needs a scrollbar, measure only
		// the short prefix needed to prove it, then rebuild sparse descriptors.
		if !reserveScrollbar && e.Wrap.HasMoreRowsThan(max(1, height)) {
			wrapWidth = max(1, baseTextWidth-1)
			e.Wrap.Rebuild(e.Buffer.Line, e.Buffer.LineCount(), wrapWidth)
		}
		e.wrapDegraded = false
		e.wrapLayoutVersion = e.Buffer.Version()
		e.wrapLayoutTabSize = e.Config.TabSize
		e.Viewport.wrapScrollY(e.Wrap)
	}
}

// wrapLikelyNeedsScrollbar reaches a decision for large documents from cheap
// rope metadata. It deliberately avoids Buffer.Line: the sparse layout will
// refine the rare compact/ambiguous case with HasMoreRowsThan.
func (e Editor) wrapLikelyNeedsScrollbar(textWidth, height int) bool {
	if e.Buffer == nil {
		return false
	}
	height = max(1, height)
	if e.Buffer.LineCount() > height {
		return true
	}
	return e.Buffer.Rope().Len() > max(1, textWidth)*height
}

func (e *Editor) disableWordWrapLayout() {
	e.Wrap = nil
	e.wrapDegraded = true
	e.wrapLayoutVersion = -1
	e.wrapLayoutTabSize = 0
	e.Viewport.WrapScrollY = 0
}

// WordWrapDegraded reports that word wrap is enabled in configuration but the
// current document exceeds the synchronous layout budget. The editor remains
// usable with ordinary horizontal scrolling.
func (e Editor) WordWrapDegraded() bool {
	return e.Config.WordWrap && e.wrapDegraded
}

// WordWrapStatus returns a short, user-facing explanation for the active
// fallback. It is intentionally empty while full word wrap is available.
func (e Editor) WordWrapStatus() string {
	if e.WordWrapDegraded() {
		return "Word wrap disabled for this large document"
	}
	return ""
}

// refreshWordWrapAfterBufferChange applies a known buffer edit without
// rebuilding visual rows for the entire document. Common typing, deletion and
// paste paths carry an EditChange, so only changed logical lines are scanned;
// the exact visual-row prefix is then updated using integers. Complex edits
// (for example multi-cursor mutations and undo) deliberately fall back to a
// full rebuild to preserve correct deep scrolling.
func (e *Editor) refreshWordWrapAfterBufferChange() {
	if e.Buffer == nil || !e.Config.WordWrap || e.Wrap == nil || e.wrapLayoutVersion == e.Buffer.Version() {
		return
	}

	change := e.Buffer.LastChange()
	if e.wrapLayoutVersion >= 0 && e.Buffer.Version() == e.wrapLayoutVersion+1 && change != nil &&
		e.Wrap.ApplyEdit(e.Buffer.Line, e.Buffer.LineCount(), change.StartLine, change.EndLine, change.Text) {
		metrics := computeGutterMetrics(e.Buffer.LineCount(), e.DebugGutter, false)
		expectedWidth := metrics.textWidth(e.Viewport.Width)
		if e.wrapLikelyNeedsScrollbar(expectedWidth, e.Viewport.Height) || e.Wrap.HasMoreRowsThan(max(1, e.Viewport.Height)) {
			expectedWidth = max(1, expectedWidth-1)
		}
		if e.Wrap.Width() == expectedWidth {
			e.wrapDegraded = false
			e.wrapLayoutVersion = e.Buffer.Version()
			e.wrapLayoutTabSize = e.Config.TabSize
			e.Viewport.wrapScrollY(e.Wrap)
			return
		}
	}

	// Unknown/multi-location edits and a gutter-width transition are rare. A
	// sparse descriptor rebuild is safer than retaining stale visual offsets;
	// it does not synchronously enumerate the document's lines.
	e.SetSize(e.Viewport.Width, e.Viewport.Height)
}

// Update handles input messages, returns updated editor and optional command.
func (e Editor) Update(msg tea.Msg) (Editor, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		// Context menu intercepts keys when visible
		if e.contextMenu.Visible {
			switch msg.String() {
			case "up":
				e.contextMenu.MoveUp()
				return e, nil
			case "down":
				e.contextMenu.MoveDown()
				return e, nil
			case "enter":
				if item := e.contextMenu.Selected(); item != nil {
					action := item.Action
					e.contextMenu.Hide()
					return e.dispatchContextMenuAction(action)
				}
				e.contextMenu.Hide()
				return e, nil
			case "esc", "escape":
				e.contextMenu.Hide()
				return e, nil
			default:
				e.contextMenu.Hide()
				return e, nil
			}
		}

		// Find widget intercepts keys when visible
		if e.find.Visible() {
			switch msg.String() {
			case "esc", "escape":
				e.find.Hide()
				return e, nil
			case "enter", "f3":
				e.UpdateFind(msg)
				return e, nil
			case "shift+f3", "shift+enter":
				e.UpdateFind(msg)
				return e, nil
			case "ctrl+r":
				e.UpdateFind(msg)
				return e, nil
			default:
				e.UpdateFind(msg)
				return e, nil
			}
		}

		// Autocomplete intercepts some keys when visible
		if e.autocomplete.Visible {
			switch msg.String() {
			case "left", "right", "home", "end", "ctrl+home", "ctrl+end":
				// Navigation moves the cursor away from the completion point,
				// so the popup must not linger on stale suggestions.
				e.autocomplete.Hide()
			case "up":
				e.autocomplete.MoveUp()
				return e, nil
			case "down":
				e.autocomplete.MoveDown()
				return e, nil
			case "enter", "tab":
				if item := e.autocomplete.Selected(); item != nil {
					e.applyCompletion(*item)
				}
				e.autocomplete.Hide()
				return e, e.scheduleRetokenize()
			case "esc", "escape":
				e.autocomplete.Hide()
				return e, nil
			}
		}
		// Moving the cursor invalidates hover content anchored to the old
		// position; dismiss it instead of letting it drift after the cursor.
		if e.hover.Visible && isNavigationKey(msg.String()) {
			e.hover.Hide()
		}
		return e.handleKeyPress(msg)
	case tea.MouseClickMsg:
		return e.handleMouseClick(msg)
	case tea.MouseMotionMsg:
		return e.handleMouseMotion(msg)
	case tea.MouseReleaseMsg:
		e.dragging = false
		return e, nil
	case tea.MouseWheelMsg:
		return e.handleMouseWheel(msg)
	case tea.PasteMsg:
		return e.handlePastePayload(msg.Content, nil)
	case ClipboardPasteResultMsg:
		if !e.acceptsClipboardPaste(msg) || msg.Content == "" {
			return e, nil
		}
		return e.handlePastePayload(msg.Content, msg.Err)
	case PastePreparedMsg:
		if msg.EditorID != e.id || msg.Generation != e.pasteGeneration || msg.Version != e.Buffer.Version() || msg.Err != nil || msg.Rope == nil {
			return e, nil
		}
		e.Buffer.ReplaceRopeSnapshot(msg.Rope, msg.Cursor)
		e.Buffer.RestoreSelections(msg.Selections, msg.Primary)
		e.refreshWordWrapAfterBufferChange()
		e.EnsureCursorVisible()
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, e.scheduleRetokenize()
	case ClipboardCopyPreparedMsg:
		if msg.EditorID != e.id || msg.Generation != e.clipboardCopyGeneration || msg.Version != e.Buffer.Version() {
			return e, nil
		}
		if msg.Err != nil {
			return e, clipboardCopyRejectedCmd(e.id, msg.Err)
		}
		return e.handlePreparedClipboardCopy(msg)
	case FindDebounceMsg:
		// Only the newest generation gets to start a scan; earlier ticks are
		// superseded keystrokes.
		if msg.EditorID != e.id || msg.Generation != e.find.Generation() {
			return e, nil
		}
		return e, e.find.ScanCmd(e.Buffer.Rope(), e.Buffer.Cursor)

	case FindResultsMsg:
		if msg.EditorID != e.id {
			return e, nil
		}
		if e.find.ApplyResults(msg) {
			if match := e.find.CurrentMatchPosition(); match != nil {
				e.Buffer.SetCursor(match.Start)
				e.EnsureCursorVisible()
			}
		}
		return e, nil

	case RetokenizeMsg:
		if e.Highlighter == nil {
			return e, nil
		}
		if msg.EditorID != 0 && msg.EditorID != e.id {
			return e, nil
		}
		// Discard stale retokenize messages
		if msg.Version != e.Buffer.Version() {
			return e, nil
		}
		// Skip duplicate version (but allow viewport-only re-tokenization for scroll)
		if msg.Version == e.lastVersion && !msg.ViewportOnly {
			return e, nil
		}
		// Launch async tokenization
		hl := e.Highlighter
		editorID := e.id
		version := msg.Version
		if msg.ViewportOnly {
			ctx, generation := e.beginViewportTokenize()
			// Capture the immutable rope region before the async command. Do not
			// retain e.Buffer: edits may replace its rope while this is running.
			viewStart, viewEnd := e.visibleTokenRange()
			snapshot := highlight.CaptureViewport(e.Buffer.Rope(), viewStart, viewEnd)
			return e, tokenizeViewportCmd(ctx, hl, snapshot, editorID, version, generation)
		}
		if shouldUseSparseHighlight(e.Buffer.Rope()) || shouldUseInteractiveSparseHighlight(e.Buffer.Rope()) {
			ctx, generation := e.beginViewportTokenize()
			viewStart, viewEnd := e.visibleTokenRange()
			snapshot := highlight.CaptureViewport(e.Buffer.Rope(), viewStart, viewEnd)
			return e, tokenizeViewportCmd(ctx, hl, snapshot, editorID, version, generation)
		}
		// Edit-triggered: tokenize the full file
		e.lastVersion = msg.Version
		ctx, generation := e.beginFullTokenize()
		rope := e.Buffer.Rope()
		return e, func() tea.Msg {
			content, err := rope.BytesContext(ctx)
			if err != nil {
				return nil
			}
			lines, complete := hl.TokenizeToLinesContext(ctx, content)
			if !complete {
				return nil
			}
			return TokenizeCompleteMsg{EditorID: editorID, Version: version, Generation: generation, Lines: lines}
		}
	case TokenizeCompleteMsg:
		if e.Highlighter == nil {
			return e, nil
		}
		if msg.EditorID != 0 && msg.EditorID != e.id {
			return e, nil
		}
		if e.acceptsTokenizeComplete(msg) {
			if msg.Partial {
				e.Highlighter.MergeBatch(msg.Batch)
			} else {
				e.Highlighter.SetLines(msg.Lines)
			}
		}
		return e, nil
	}
	return e, nil
}

func (e Editor) acceptsTokenizeComplete(msg TokenizeCompleteMsg) bool {
	if e.tokenizer == nil || e.Buffer == nil || msg.Version != e.Buffer.Version() {
		return false
	}
	lane := &e.tokenizer.full
	if msg.Partial {
		lane = &e.tokenizer.viewport
	}
	return msg.Generation == lane.generation
}

const maxAsyncPasteResultBytes = clipboard.MaxClipboardBytes

func (e Editor) handlePastePayload(content string, sourceErr error) (Editor, tea.Cmd) {
	if content == "" {
		return e, nil
	}
	selectionCount := 1
	if e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 {
		selectionCount = e.Buffer.Selections.Count()
	}
	if len(content) > maxAsyncPasteResultBytes/selectionCount {
		return e, clipboardOperationLimitCmd(e.id, "Paste")
	}
	if len(content) >= asyncPasteThresholdBytes || len(content)*selectionCount >= asyncPasteThresholdBytes {
		return e, e.queueAsyncPaste(content, sourceErr)
	}
	if err := clipboard.Validate(content); err != nil {
		return e, nil
	}
	e.Buffer.InsertAtCursor([]byte(content))
	e.refreshWordWrapAfterBufferChange()
	e.EnsureCursorVisible()
	if e.Highlighter != nil {
		e.Highlighter.Invalidate()
	}
	return e, e.scheduleRetokenize()
}

func (e *Editor) queueAsyncPaste(content string, sourceErr error) tea.Cmd {
	e.pasteGeneration++
	selections := []text.Selection(nil)
	var primary text.Selection
	if e.Buffer.Selections != nil {
		selections = append(selections, e.Buffer.Selections.All()...)
		primary = e.Buffer.Selections.Primary()
	}
	return preparePasteCmd(e.id, e.pasteGeneration, e.Buffer.Version(), e.Buffer.Rope(), e.Buffer.Cursor, selections, primary, content, sourceErr)
}

func (e Editor) selectionClipboardCopy(cut bool) (Editor, tea.Cmd, bool) {
	if e.Buffer.Selections == nil || e.Buffer.Selections.Count() == 0 {
		return e, nil, false
	}
	selection := e.Buffer.Selections.Primary()
	if selection.IsEmpty() {
		return e, nil, false
	}
	start, end := selection.Ordered()
	snapshot := e.Buffer.Rope()
	// Do not initialize Rope's whole-document line index just to copy a
	// selection. The uncached conversion walks tree metadata without flattening
	// the document, including for a selection that ends at line end.
	startOffset, startOK := snapshot.PositionToOffsetUncached(start)
	endOffset, endOK := snapshot.PositionToOffsetUncached(end)
	if !startOK || !endOK {
		return e, nil, false
	}
	if endOffset <= startOffset {
		return e, nil, false
	}
	if endOffset-startOffset > clipboard.MaxClipboardBytes {
		operation := "Copy"
		if cut {
			operation = "Cut"
		}
		return e, clipboardOperationLimitCmd(e.id, operation), true
	}
	e.clipboardCopyGeneration++
	return e, prepareClipboardCopyCmd(e.id, e.clipboardCopyGeneration, e.Buffer.Version(), snapshot, start, end, startOffset, endOffset, cut), true
}

func (e Editor) handlePreparedClipboardCopy(msg ClipboardCopyPreparedMsg) (Editor, tea.Cmd) {
	copyCmd := copyToClipboardCmd(e.id, msg.Content)
	if !msg.Cut || !e.matchesPrimarySelection(msg.Start, msg.End) {
		return e, copyCmd
	}
	e.Buffer.SetSelection(msg.Start, msg.End)
	e.Buffer.DeleteSelection()
	e.refreshWordWrapAfterBufferChange()
	e.EnsureCursorVisible()
	if e.Highlighter != nil {
		e.Highlighter.Invalidate()
	}
	return e, tea.Batch(copyCmd, e.scheduleRetokenize())
}

func (e Editor) matchesPrimarySelection(start, end text.Position) bool {
	if e.Buffer.Selections == nil || e.Buffer.Selections.Count() == 0 {
		return false
	}
	gotStart, gotEnd := e.Buffer.Selections.Primary().Ordered()
	return gotStart == start && gotEnd == end
}

func (e Editor) selectedLineSpan() int {
	startLine, endLine := e.Buffer.Cursor.Line, e.Buffer.Cursor.Line
	if e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 && !e.Buffer.Selections.Primary().IsEmpty() {
		start, end := e.Buffer.Selections.Primary().Ordered()
		startLine, endLine = start.Line, end.Line
		if end.Col == 0 && endLine > startLine {
			endLine--
		}
	}
	return endLine - startLine + 1
}

func (e Editor) multilineEditWithinBudget(operation string) (tea.Cmd, bool) {
	if e.selectedLineSpan() <= MaxSynchronousMultilineEditLines {
		return nil, true
	}
	return func() tea.Msg {
		return MultilineEditLimitMsg{EditorID: e.id, Operation: operation, MaxLines: MaxSynchronousMultilineEditLines}
	}, false
}

func (e Editor) handleKeyPress(msg tea.KeyPressMsg) (Editor, tea.Cmd) {
	versionBefore := e.Buffer.Version()
	edited := false
	switch msg.String() {
	// --- Navigation ---
	case "left":
		e.Buffer.MoveCursor(text.DirLeft)
		e.Buffer.ClearSelection()
	case "right":
		e.Buffer.MoveCursor(text.DirRight)
		e.Buffer.ClearSelection()
	case "up":
		e.Buffer.MoveCursor(text.DirUp)
		e.Buffer.ClearSelection()
	case "down":
		e.Buffer.MoveCursor(text.DirDown)
		e.Buffer.ClearSelection()
	case "ctrl+left":
		e.Buffer.MoveCursorWordLeft()
		e.Buffer.ClearSelection()
	case "ctrl+right":
		e.Buffer.MoveCursorWordRight()
		e.Buffer.ClearSelection()
	case "home":
		e.Buffer.CursorToLineStart()
		e.Buffer.ClearSelection()
	case "end":
		e.Buffer.CursorToLineEnd()
		e.Buffer.ClearSelection()
	case "ctrl+home":
		e.Buffer.CursorToDocStart()
		e.Buffer.ClearSelection()
	case "ctrl+end":
		e.Buffer.CursorToDocEnd()
		e.Buffer.ClearSelection()
	case "pgup":
		if e.Wrap != nil && e.Config.WordWrap {
			e.moveCursorByWrappedRows(-e.Viewport.Height)
		} else {
			target := max(0, e.Buffer.Cursor.Line-e.Viewport.Height)
			e.Buffer.SetCursor(text.Position{
				Line: target,
				Col:  min(e.Buffer.Cursor.Col, e.Buffer.Rope().LineLen(target)),
			})
		}
		e.Buffer.ClearSelection()
		if e.Wrap == nil || !e.Config.WordWrap {
			e.Viewport.ScrollUp(e.Viewport.Height)
		}
		// Trigger viewport tokenization if scrolled outside tokenized range
		if e.needsRetokenize() {
			return e, e.scheduleRetokenizeImmediate()
		}
	case "pgdown":
		if e.Wrap != nil && e.Config.WordWrap {
			e.moveCursorByWrappedRows(e.Viewport.Height)
		} else {
			maxLine := e.Buffer.LineCount() - 1
			target := min(maxLine, e.Buffer.Cursor.Line+e.Viewport.Height)
			e.Buffer.SetCursor(text.Position{
				Line: target,
				Col:  min(e.Buffer.Cursor.Col, e.Buffer.Rope().LineLen(target)),
			})
		}
		e.Buffer.ClearSelection()
		if e.Wrap == nil || !e.Config.WordWrap {
			e.Viewport.ScrollDown(e.Viewport.Height, e.Buffer.LineCount()-1)
		}
		// Trigger viewport tokenization if scrolled outside tokenized range
		if e.needsRetokenize() {
			return e, e.scheduleRetokenizeImmediate()
		}

	// --- Selection ---
	case "shift+left":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirLeft) })
	case "shift+right":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirRight) })
	case "shift+up":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirUp) })
	case "shift+down":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursor(text.DirDown) })
	case "ctrl+shift+left":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursorWordLeft() })
	case "ctrl+shift+right":
		e.Buffer.ExtendSelection(func() { e.Buffer.MoveCursorWordRight() })
	case "shift+home":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToLineStart() })
	case "shift+end":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToLineEnd() })
	case "ctrl+shift+home":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToDocStart() })
	case "ctrl+shift+end":
		e.Buffer.ExtendSelection(func() { e.Buffer.CursorToDocEnd() })
	case "ctrl+a":
		e.Buffer.SelectAll()

	// --- Clipboard ---
	case "ctrl+c":
		if updated, cmd, handled := e.selectionClipboardCopy(false); handled {
			return updated, cmd
		}
	case "ctrl+x":
		if updated, cmd, handled := e.selectionClipboardCopy(true); handled {
			return updated, cmd
		}
	case "ctrl+v":
		return e, e.requestClipboardPaste()

	// --- Editing ---
	case "backspace":
		// Delete both brackets when backspacing between empty pair
		if IsBetweenBrackets(e.Buffer, e.Buffer.Cursor) {
			start := text.Position{Line: e.Buffer.Cursor.Line, Col: e.Buffer.Cursor.Col - 1}
			end := text.Position{Line: e.Buffer.Cursor.Line, Col: e.Buffer.Cursor.Col + 1}
			e.Buffer.ReplaceRange(start, end, nil)
			e.Buffer.SetCursor(start)
			edited = true
			break
		}
		e.Buffer.Backspace()
		edited = true
	case "ctrl+backspace":
		e.Buffer.BackspaceWord()
		edited = true
	case "delete":
		e.Buffer.Delete()
		edited = true
	case "ctrl+delete":
		e.Buffer.DeleteWord()
		edited = true
	case "enter":
		if e.Config.AutoIndent {
			e.Buffer.InsertNewlineWithIndent()
		} else {
			e.Buffer.InsertNewline()
		}
		edited = true
	case "tab":
		e.Buffer.InsertAtCursor(text.IndentString(e.Config.TabSize))
		edited = true
	case "shift+tab":
		if e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 && !e.Buffer.Selections.Primary().IsEmpty() {
			if cmd, allowed := e.multilineEditWithinBudget("Dedent"); !allowed {
				return e, cmd
			}
			e.Buffer.DedentLines(e.Config.TabSize)
		} else {
			e.Buffer.DedentLine(e.Config.TabSize)
		}
		edited = true
	case "ctrl+z":
		e.Buffer.Undo()
		edited = true
	case "ctrl+shift+z", "ctrl+y":
		e.Buffer.Redo()
		edited = true

	// --- New shortcuts ---
	case "ctrl+/":
		if cmd, allowed := e.multilineEditWithinBudget("Toggle comment"); !allowed {
			return e, cmd
		}
		e.Buffer.ToggleLineComment(e.Config.CommentPrefix)
		edited = true
	case "alt+up":
		e.Buffer.MoveLineUp()
		edited = true
	case "alt+down":
		e.Buffer.MoveLineDown()
		edited = true
	case "alt+shift+up":
		e.Buffer.DuplicateLineUp()
		edited = true
	case "alt+shift+down":
		e.Buffer.DuplicateLineDown()
		edited = true
	case "ctrl+shift+k":
		e.Buffer.DeleteLine()
		edited = true
	case "ctrl+d":
		if !e.Buffer.SelectNextOccurrence() {
			editorID := e.id
			return e, func() tea.Msg {
				return OccurrenceSearchLimitMsg{EditorID: editorID, MaxBytes: text.MaxOccurrenceSearchBytes}
			}
		}
	case "ctrl+u":
		if !e.Buffer.SelectAllOccurrences() {
			editorID := e.id
			return e, func() tea.Msg {
				return OccurrenceSearchLimitMsg{EditorID: editorID, MaxBytes: text.MaxOccurrenceSearchBytes}
			}
		}
	case "ctrl+alt+up":
		e.Buffer.AddCursorAbove()
		edited = true
	case "ctrl+alt+down":
		e.Buffer.AddCursorBelow()
		edited = true
	case "ctrl+shift+l":
		e.Buffer.SplitSelectionIntoLines()
		edited = true
	case "ctrl+l":
		e.Buffer.SelectLine()
	case "ctrl+]":
		if cmd, allowed := e.multilineEditWithinBudget("Indent"); !allowed {
			return e, cmd
		}
		e.Buffer.IndentLines(e.Config.TabSize)
		edited = true

	case "esc", "escape":
		e.hover.Hide()
		e.signatureHelp.Hide()
	default:
		if msg.Text != "" {
			ch := msg.Text[0]
			// Skip over closing bracket if it's already the next character
			if len(msg.Text) == 1 && IsCloseBracket(ch) {
				line := e.Buffer.Line(e.Buffer.Cursor.Line)
				if e.Buffer.Cursor.Col < len(line) && line[e.Buffer.Cursor.Col] == ch {
					e.Buffer.MoveCursor(text.DirRight)
					break
				}
			}
			e.Buffer.InsertAtCursor([]byte(msg.Text))
			// Auto-close bracket
			if len(msg.Text) == 1 {
				if close := AutoClosePair(ch); close != 0 {
					e.Buffer.InsertAtCursor([]byte{close})
					e.Buffer.MoveCursor(text.DirLeft)
				}
			}
			edited = true
		}
	}
	if edited && e.Buffer.Version() != versionBefore {
		e.refreshWordWrapAfterBufferChange()
	}
	e.EnsureCursorVisible()
	if edited {
		e.hover.Hide()
		e.signatureHelp.Hide()
		if e.Highlighter != nil {
			e.invalidateHighlightForLastChange()
		}
		e.refilterAutocomplete()
		return e, tea.Batch(e.scheduleRetokenize(), e.TriggerCompletion())
	}
	return e, nil
}

// refilterAutocomplete narrows an open popup to what the user has actually
// typed since the server answered, closing it when nothing matches. Without
// this the popup kept showing items that no longer match the prefix and never
// dismissed itself.
func (e *Editor) refilterAutocomplete() {
	if !e.autocomplete.Visible {
		return
	}
	e.autocomplete.Filter(e.completionPrefix())
}

// completionPrefix returns the identifier prefix immediately before the
// cursor, used to filter autocomplete items client-side.
func (e Editor) completionPrefix() string {
	buf := e.Buffer
	if buf == nil {
		return ""
	}
	line := buf.Line(buf.Cursor.Line)
	col := buf.Cursor.Col
	if col > len(line) {
		col = len(line)
	}
	start := col
	for start > 0 {
		r, size := utf8.DecodeLastRune(line[:start])
		if !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			break
		}
		start -= size
	}
	return string(line[start:col])
}

// isNavigationKey reports keys that move the cursor without editing, which
// must dismiss anchored overlays such as hover.
func isNavigationKey(key string) bool {
	switch key {
	case "up", "down", "left", "right", "home", "end", "pageup", "pagedown",
		"ctrl+up", "ctrl+down", "ctrl+left", "ctrl+right", "ctrl+home", "ctrl+end":
		return true
	}
	return false
}

// invalidateHighlightForLastChange marks the highlight cache stale for the edit
// the buffer just applied, keeping the surrounding tokens on screen until the
// asynchronous tokenization lands. It falls back to a full invalidation when the
// buffer did not report what changed.
func (e *Editor) invalidateHighlightForLastChange() {
	change := e.Buffer.LastChange()
	if change == nil {
		e.Highlighter.Invalidate()
		return
	}
	// An edit replaces [StartLine, EndLine] with Text, so the document gains the
	// newlines in Text and loses the lines the replaced span covered.
	lineDelta := strings.Count(change.Text, "\n") - (change.EndLine - change.StartLine)
	e.Highlighter.InvalidateEdited(change.StartLine, change.EndLine, lineDelta)
}

// TriggerCompletion returns a command that triggers completion if appropriate.
// Call this after text input to show completions automatically.
func (e Editor) TriggerCompletion() tea.Cmd {
	// Only trigger if we're in a valid file with LSP
	if !e.HasLSP || e.Buffer.FilePath == "" {
		return nil
	}

	// Check if we're at a position that should trigger completion
	line := e.Buffer.Line(e.Buffer.Cursor.Line)
	if e.Buffer.Cursor.Col <= 0 || e.Buffer.Cursor.Col > len(line) {
		return nil
	}

	// Decode the rune before the cursor. Cursor.Col is a UTF-8 byte offset,
	// so indexing line[Col-1] would inspect a continuation byte for a
	// multibyte rune.
	ch, _ := utf8.DecodeLastRune(line[:e.Buffer.Cursor.Col])

	// Trigger on identifier characters, including non-ASCII letters/digits.
	if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_' {
		return func() tea.Msg { return RequestCompletionCmd{} }
	}

	// Check LSP-advertised trigger characters first
	if len(e.TriggerCharacters) > 0 {
		s := string(ch)
		for _, tc := range e.TriggerCharacters {
			if s == tc {
				return func() tea.Msg { return RequestCompletionCmd{} }
			}
		}
	} else {
		// Fallback: trigger on common characters when no LSP info available
		if ch == '.' || ch == ':' {
			return func() tea.Msg { return RequestCompletionCmd{} }
		}
	}

	return nil
}

func (e Editor) handleMouseClick(msg tea.MouseClickMsg) (Editor, tea.Cmd) {
	m := msg.Mouse()

	// While the find widget is open it owns the first view row; map mouse
	// coordinates into the text area below it.
	if e.find.Visible() {
		m.Y--
		if m.Y < 0 {
			return e, nil
		}
	}

	// Left-click dismisses context menu
	if e.contextMenu.Visible && m.Button == tea.MouseLeft {
		e.contextMenu.Hide()
		return e, nil
	}

	// Right-click opens context menu
	if m.Button == tea.MouseRight {
		// A context menu takes ownership of pointer input. Do not retain a
		// left-button selection drag behind it: terminals can report a delayed
		// motion event after the menu has appeared.
		e.dragging = false
		pos := e.screenToBuffer(m.X, m.Y)
		// Only move cursor if no selection (preserve selection for cut/copy)
		if e.Buffer.Selections == nil || e.Buffer.Selections.Count() == 0 || e.Buffer.Selections.Primary().IsEmpty() {
			e.Buffer.SetCursor(pos)
		}
		e.contextMenu.Show(e.buildEditorMenuItems(), m.X, m.Y)
		return e, nil
	}

	if m.Button == tea.MouseLeft {
		metrics := e.currentGutterMetrics()
		foldCol := metrics.markerWidth + metrics.lineNumberWidth
		gutterEnd := metrics.contentWidth()

		// Click on fold indicator column → toggle fold
		if metrics.foldWidth > 0 && m.X >= foldCol && m.X < gutterEnd {
			pos := e.screenToBuffer(m.X, m.Y)
			e.Folds.Toggle(pos.Line)
			return e, nil
		}

		// Click on marker or line number area → toggle breakpoint
		if m.X < foldCol {
			pos := e.screenToBuffer(m.X, m.Y)
			return e, func() tea.Msg { return BreakpointClickMsg{Line: pos.Line} }
		}

		pos := e.screenToBuffer(m.X, m.Y)
		if m.Mod == tea.ModShift {
			anchor := e.Buffer.Cursor
			if e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 {
				anchor = e.Buffer.Selections.Primary().Anchor
			}
			e.Buffer.SetSelection(anchor, pos)
		} else {
			now := time.Now()
			if pos == e.lastClickPos && now.Sub(e.lastClickTime) < 400*time.Millisecond {
				e.clickCount++
			} else {
				e.clickCount = 1
			}
			e.lastClickTime = now
			e.lastClickPos = pos
			e.Buffer.SetCursor(pos)

			switch e.clickCount {
			case 2:
				e.Buffer.SelectWordAtCursor()
				e.dragging = false
			case 3:
				e.Buffer.SelectLine()
				// A fourth click starts a fresh click sequence. This avoids
				// repeatedly selecting the line while the user starts a drag.
				e.clickCount = 0
				e.lastClickTime = time.Time{}
				e.dragging = false
			default:
				e.dragging = true
			}
		}
	}
	return e, nil
}

func (e Editor) handleMouseMotion(msg tea.MouseMotionMsg) (Editor, tea.Cmd) {
	if !e.dragging || e.Buffer == nil || e.Buffer.LineCount() == 0 {
		return e, nil
	}
	m := msg.Mouse()
	if e.Viewport.Height <= 0 || e.Viewport.Width <= 0 {
		return e, nil
	}
	// While the find widget is open it owns the first view row; map motion
	// coordinates into the text area below it.
	if e.find.Visible() {
		m.Y--
	}

	// Terminals report motion coordinates outside the widget while a button is
	// held. Scroll one visual row per event, then map the selection to the
	// nearest visible row. Limiting each event to one row keeps a fast pointer
	// from skipping large sections of a document.
	y := min(max(m.Y, 0), e.Viewport.Height-1)
	scrolled := false
	if m.Y < 0 {
		scrolled = e.scrollForDrag(-1)
	} else if m.Y >= e.Viewport.Height {
		scrolled = e.scrollForDrag(1)
	}
	x := min(max(m.X, 0), e.Viewport.Width-1)
	pos := e.screenToBuffer(x, y)
	anchor := e.Buffer.Cursor
	if e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 {
		anchor = e.Buffer.Selections.Primary().Anchor
	}
	e.Buffer.SetSelection(anchor, pos)
	if scrolled && e.needsRetokenize() {
		return e, e.scheduleRetokenizeImmediate()
	}
	return e, nil
}

// IsDragging reports whether this editor owns an active left-button selection
// drag. The app uses it to keep routing motion after the pointer crosses an
// adjacent pane or the editor's top/bottom edge.
func (e Editor) IsDragging() bool {
	return e.dragging
}

// CancelDrag stops an in-progress selection drag when another surface takes
// mouse or keyboard ownership (for example a modal or context menu).
func (e *Editor) CancelDrag() {
	if e != nil {
		e.dragging = false
	}
}

// SetContextMenuPosition changes the menu's editor-local anchor. The app
// computes this from its body rectangle so rendering and hit-testing use the
// same clamped terminal cells.
func (e *Editor) SetContextMenuPosition(x, y int) {
	if e == nil {
		return
	}
	e.contextMenu.X = max(0, x)
	e.contextMenu.Y = max(0, y)
}

// scrollForDrag moves the viewport by one visual row and reports whether it
// changed. Word wrap and collapsed folds use visual rows; normal files use
// logical lines. Callers must clamp their hit-test row after this operation.
func (e *Editor) scrollForDrag(direction int) bool {
	if direction == 0 || e.Buffer == nil || e.Viewport.Height <= 0 {
		return false
	}
	if e.Wrap != nil && e.Config.WordWrap {
		before := e.Viewport.WrapScrollY
		if direction < 0 {
			e.Viewport.ScrollWrapUp(1)
		} else {
			e.Viewport.ScrollWrapDown(1, e.Wrap)
		}
		return e.Viewport.WrapScrollY != before
	}

	before := e.Viewport.ScrollY
	if len(e.Folds.Regions) > 0 {
		maxScroll := max(0, e.Folds.TotalVisibleLines(e.Buffer.LineCount())-e.Viewport.Height)
		e.Viewport.ScrollY = min(maxScroll, max(0, e.Viewport.ScrollY+direction))
	} else if direction < 0 {
		e.Viewport.ScrollUp(1)
	} else {
		e.Viewport.ScrollDown(1, e.Buffer.LineCount()-1)
	}
	return e.Viewport.ScrollY != before
}

func (e Editor) handleMouseWheel(msg tea.MouseWheelMsg) (Editor, tea.Cmd) {
	m := msg.Mouse()
	switch m.Button {
	case tea.MouseWheelUp:
		if e.Wrap != nil && e.Config.WordWrap {
			e.Viewport.ScrollWrapUp(3)
		} else {
			e.Viewport.ScrollUp(3)
		}
	case tea.MouseWheelDown:
		if e.Wrap != nil && e.Config.WordWrap {
			e.Viewport.ScrollWrapDown(3, e.Wrap)
		} else {
			e.Viewport.ScrollDown(3, e.Buffer.LineCount()-1)
		}
	}
	if e.needsRetokenize() {
		return e, e.scheduleRetokenizeImmediate()
	}
	return e, nil
}

// scheduleRetokenizeImmediate sends a RetokenizeMsg without debounce,
// used when scrolling past the tokenized range (user is waiting to see color).
func (e Editor) scheduleRetokenizeImmediate() tea.Cmd {
	if e.Highlighter == nil {
		return nil
	}
	editorID := e.id
	version := e.Buffer.Version()
	return func() tea.Msg {
		return RetokenizeMsg{EditorID: editorID, Version: version, ViewportOnly: true}
	}
}

func (e Editor) scheduleRetokenize() tea.Cmd {
	if e.Highlighter == nil {
		return nil
	}
	editorID := e.id
	version := e.Buffer.Version()
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return RetokenizeMsg{EditorID: editorID, Version: version}
	})
}

// needsRetokenize checks if the viewport has scrolled outside the tokenized range.
func (e Editor) needsRetokenize() bool {
	if e.Highlighter == nil {
		return false
	}
	viewStart, viewEnd := e.visibleTokenRange()
	return !e.Highlighter.CoversRange(viewStart, viewEnd)
}

func (e Editor) visibleTokenRange() (int, int) {
	if e.Wrap != nil && e.Config.WordWrap {
		return e.Wrap.VisibleBufferRange(e.Viewport.wrapScrollY(e.Wrap), e.Viewport.Height)
	}
	return e.Viewport.ScrollY, e.Viewport.ScrollY + e.Viewport.Height
}

func (e *Editor) moveCursorByWrappedRows(delta int) {
	if e.Wrap == nil || e.Buffer == nil || e.Buffer.LineCount() == 0 {
		return
	}
	line := e.Buffer.Line(e.Buffer.Cursor.Line)
	currentOffset, currentCol := e.Wrap.PositionForByte(e.Buffer.Cursor.Line, e.Buffer.Cursor.Col, line)
	currentVisual := e.Wrap.VisualRow(e.Buffer.Cursor.Line) + currentOffset
	targetVisual := max(0, currentVisual+delta)
	if e.Wrap.TotalRowsKnown() {
		targetVisual = min(targetVisual, max(0, e.Wrap.TotalRows()-1))
	}
	targetLine, targetOffset := e.Wrap.BufferLine(targetVisual)
	targetContent := e.Buffer.Line(targetLine)
	start, end, displayStart, ok := e.Wrap.SegmentBoundsForLine(targetLine, targetOffset, targetContent)
	if !ok {
		return
	}
	endDisplay := advanceDisplayColumn(targetContent, start, end, displayStart, e.Viewport.tabSize())
	targetDisplay := min(displayStart+currentCol, endDisplay)
	e.Buffer.SetCursor(text.Position{
		Line: targetLine,
		Col:  byteColumnAtDisplayFrom(targetContent, start, displayStart, targetDisplay, e.Viewport.tabSize()),
	})
}

// View renders the editor content.
func (e *Editor) View() string {
	pluginHighlights := e.PluginHighlightRanges()
	extra := append(e.diagnosticHighlights(), e.findMatchHighlights()...)
	if len(extra) > 0 {
		pluginHighlights = append(pluginHighlights, extra...)
		sort.SliceStable(pluginHighlights, func(i, j int) bool {
			if pluginHighlights[i].Line != pluginHighlights[j].Line {
				return pluginHighlights[i].Line < pluginHighlights[j].Line
			}
			if pluginHighlights[i].StartCol != pluginHighlights[j].StartCol {
				return pluginHighlights[i].StartCol < pluginHighlights[j].StartCol
			}
			return pluginHighlights[i].EndCol < pluginHighlights[j].EndCol
		})
	}
	var view string
	if e.Wrap != nil && e.Config.WordWrap {
		view = e.Viewport.RenderWithWrapHighlights(e.Buffer, e.theme, e.Highlighter, e.Diagnostics, e.DebugGutter, e.Wrap, pluginHighlights)
	} else if len(e.Folds.Regions) > 0 {
		view = e.Viewport.RenderWithFoldsHighlights(e.Buffer, e.theme, e.Highlighter, e.Diagnostics, e.DebugGutter, &e.Folds, pluginHighlights)
	} else {
		view = e.Viewport.RenderHighlights(e.Buffer, e.theme, e.Highlighter, e.Diagnostics, e.DebugGutter, pluginHighlights)
	}
	if fv := e.find.View(); fv != "" {
		view = fv + "\n" + view
	}
	return view
}

// findMatchHighlights turns the visible find matches into highlight ranges so
// the existing render pipeline paints them. The current match gets a distinct
// style so the user can see where the next Enter will land.
func (e Editor) findMatchHighlights() []HighlightRange {
	if !e.find.Visible() || e.Buffer == nil || len(e.find.matches) == 0 {
		return nil
	}
	startLine, endLine := e.visibleBufferLineRange()
	if startLine > endLine {
		return nil
	}
	current := e.find.CurrentMatchPosition()
	var ranges []HighlightRange
	for _, m := range e.find.matches {
		if m.Start.Line < startLine {
			continue
		}
		if m.Start.Line > endLine {
			break
		}
		style := e.theme.FindMatch
		if current != nil && current.Start == m.Start && current.End == m.End {
			style = e.theme.FindMatchCurrent
		}
		ranges = append(ranges, HighlightRange{
			Namespace: -1,
			Line:      m.Start.Line,
			StartCol:  m.Start.Col,
			EndCol:    m.End.Col,
			Style:     style,
		})
	}
	return ranges
}

// diagnosticHighlights turns visible LSP diagnostics into underline highlight
// ranges so errors and warnings are marked in the text itself, not just by a
// colored gutter number. Multi-line diagnostics underline every covered line.
func (e Editor) diagnosticHighlights() []HighlightRange {
	if e.Buffer == nil || len(e.Diagnostics) == 0 {
		return nil
	}
	startLine, endLine := e.visibleBufferLineRange()
	if startLine > endLine {
		return nil
	}
	var ranges []HighlightRange
	for _, d := range e.Diagnostics {
		if d.EndLine < startLine || d.StartLine > endLine {
			continue
		}
		style := diagnosticStyle(e.theme, d.Severity)
		first := max(d.StartLine, startLine)
		last := min(d.EndLine, endLine)
		for line := first; line <= last; line++ {
			lineLen := len(e.Buffer.Line(line))
			start := 0
			if line == d.StartLine {
				start = min(d.StartCol, lineLen)
			}
			end := lineLen
			if line == d.EndLine {
				end = min(d.EndCol, lineLen)
			}
			if end <= start {
				continue
			}
			ranges = append(ranges, HighlightRange{
				Namespace: -2,
				Line:      line,
				StartCol:  start,
				EndCol:    end,
				Style:     style,
			})
		}
	}
	return ranges
}

func diagnosticStyle(theme ui.Theme, severity int) lipgloss.Style {
	switch severity {
	case 1:
		return theme.DiagError
	case 2:
		return theme.DiagWarning
	case 3:
		return theme.DiagInfo
	default:
		return theme.DiagHint
	}
}

// visibleBufferLineRange returns the buffer line window currently on screen
// for plain, wrapped, or folded rendering.
func (e Editor) visibleBufferLineRange() (int, int) {
	lineCount := e.Buffer.LineCount()
	if lineCount == 0 {
		return 0, -1
	}
	if e.Wrap != nil && e.Config.WordWrap {
		start, end := e.Wrap.VisibleBufferRange(e.Viewport.WrapScrollY, max(1, e.Viewport.Height))
		if start > end || start >= lineCount {
			return 0, -1
		}
		return start, min(end, lineCount-1)
	}
	if len(e.Folds.Regions) > 0 {
		start := e.Viewport.foldedScrollStart(&e.Folds, lineCount)
		visible := e.Folds.VisibleLines(start, e.Viewport.Height, lineCount)
		if len(visible) == 0 {
			return 0, -1
		}
		return visible[0], visible[len(visible)-1]
	}
	start := min(e.Viewport.ScrollY, lineCount-1)
	end := min(start+max(1, e.Viewport.Height)-1, lineCount-1)
	return start, end
}

// effectiveGutterWidth computes the total gutter width matching what Render produces.
func (e Editor) effectiveGutterWidth() int {
	return e.currentGutterMetrics().totalWidth()
}

func (e Editor) shouldRenderFoldGutter() bool {
	return len(e.Folds.Regions) > 0 && (e.Wrap == nil || !e.Config.WordWrap)
}

func (e Editor) currentGutterMetrics() gutterMetrics {
	return computeGutterMetrics(e.Buffer.LineCount(), e.DebugGutter, e.shouldRenderFoldGutter())
}

// visibleLinesForClick returns the visible lines slice when folds are active, nil otherwise.
func (e Editor) visibleLinesForClick() []int {
	if !e.shouldRenderFoldGutter() {
		return nil
	}
	startLine := e.Viewport.foldedScrollStart(&e.Folds, e.Buffer.LineCount())
	return e.Folds.VisibleLines(startLine, e.Viewport.Height, e.Buffer.LineCount())
}

// screenToBuffer maps screen coordinates to buffer position, handling wrap/fold modes.
func (e Editor) screenToBuffer(screenX, screenY int) text.Position {
	gw := e.effectiveGutterWidth()
	if e.Wrap != nil && e.Config.WordWrap {
		return e.Viewport.ScreenToBufferPositionWrap(screenX, screenY, e.Buffer, gw, e.Wrap)
	}
	return e.Viewport.ScreenToBufferPosition(screenX, screenY, e.Buffer, gw, e.visibleLinesForClick())
}

// StatusColumn returns the 1-based display column of the cursor for status
// reporting. Buffer columns are byte offsets, so multibyte characters and
// tab expansion must be converted before the number is shown to the user.
func (e Editor) StatusColumn() int {
	lineContent := e.Buffer.Line(e.Buffer.Cursor.Line)
	col := e.Buffer.Cursor.Col
	if col > len(lineContent) {
		col = len(lineContent)
	}
	return displayColumn(lineContent, col, e.Viewport.tabSize()) + 1
}

// CursorPosition returns the screen position of the cursor within the editor
// view. While the find widget is open it occupies the first view row, so all
// text rows shift down by one.
func (e Editor) CursorPosition() (int, int) {
	x, y := e.cursorPositionInText()
	if e.find.Visible() {
		y++
	}
	return x, y
}

func (e Editor) cursorPositionInText() (int, int) {
	gw := e.effectiveGutterWidth()
	lineContent := e.Buffer.Line(e.Buffer.Cursor.Line)
	col := e.Buffer.Cursor.Col
	if col > len(lineContent) {
		col = len(lineContent)
	}
	displayCol := displayColumn(lineContent, col, e.Viewport.tabSize())

	// Word wrap mode: cursor position accounts for wrapped visual rows
	if e.Wrap != nil && e.Config.WordWrap {
		wrapRow, wrapCol := e.Wrap.PositionForByte(e.Buffer.Cursor.Line, col, lineContent)
		x := wrapCol + gw
		visualRow := e.Wrap.VisualRow(e.Buffer.Cursor.Line) + wrapRow - e.Viewport.wrapScrollY(e.Wrap)
		return x, visualRow
	}

	x := displayCol - e.Viewport.ScrollX + gw

	// When folds are active, map buffer line to screen row via visible lines
	if len(e.Folds.Regions) > 0 {
		visLines := e.visibleLinesForClick()
		for i, vl := range visLines {
			if vl == e.Buffer.Cursor.Line {
				return x, i
			}
		}
	}
	y := e.Buffer.Cursor.Line - e.Viewport.ScrollY
	return x, y
}

// EnsureCursorVisible keeps the cursor in view using display-width-aware math.
func (e *Editor) EnsureCursorVisible() {
	if e.Buffer == nil {
		return
	}

	margin := clampScrollMargin(e.Config.ScrollMargin, e.Viewport.Height)
	textWidth := e.currentGutterMetrics().textWidth(e.Viewport.Width)
	if e.Wrap != nil && e.Config.WordWrap {
		e.Viewport.ScrollX = 0
		line := e.Buffer.Line(e.Buffer.Cursor.Line)
		wrapRow, _ := e.Wrap.PositionForByte(e.Buffer.Cursor.Line, e.Buffer.Cursor.Col, line)
		cursorVisualRow := e.Wrap.VisualRow(e.Buffer.Cursor.Line) + wrapRow
		if cursorVisualRow < e.Viewport.WrapScrollY+margin {
			e.Viewport.WrapScrollY = max(0, cursorVisualRow-margin)
		}
		if cursorVisualRow >= e.Viewport.WrapScrollY+max(1, e.Viewport.Height)-margin {
			e.Viewport.WrapScrollY = max(0, cursorVisualRow-max(1, e.Viewport.Height)+margin+1)
		}
		e.Viewport.wrapScrollY(e.Wrap)
		return
	}

	// Folds hide lines, so scrolling must be computed in visual rows whenever
	// any region is collapsed.
	var folds *FoldState
	if e.Folds.HasCollapsedRegions() {
		folds = &e.Folds
	}
	e.Viewport.ensureCursorVisibleWithFolds(e.Buffer, e.Buffer.Cursor, textWidth, folds, margin)
}

// ShowAutocomplete displays completion items.
func (e *Editor) ShowAutocomplete(items []overlays.AutocompleteItem) {
	e.autocomplete.Show(items)
}

// HideAutocomplete dismisses the autocomplete popup.
func (e *Editor) HideAutocomplete() {
	e.autocomplete.Hide()
}

// ShowHover displays hover information.
func (e *Editor) ShowHover(content string) {
	e.hover.Show(content)
}

// HideHover dismisses the hover popup.
func (e *Editor) HideHover() {
	e.hover.Hide()
}

// ShowSignatureHelp displays signature help.
func (e *Editor) ShowSignatureHelp(help *overlays.SignatureData) {
	e.signatureHelp.Show(help)
}

// HideSignatureHelp dismisses the signature help popup.
func (e *Editor) HideSignatureHelp() {
	e.signatureHelp.Hide()
}

// SignatureHelpView returns the signature help popup rendering if visible.
func (e Editor) SignatureHelpView() string {
	return e.signatureHelp.View()
}

// AutocompleteView returns the autocomplete popup rendering if visible.
func (e Editor) AutocompleteView() string {
	return e.autocomplete.View()
}

// HoverView returns the hover popup rendering if visible.
func (e Editor) HoverView() string {
	return e.hover.View()
}

// LSPOverlayView returns the one LSP popup that may occupy editor cells.
//
// Completion owns keyboard navigation and insertion, so it intentionally wins
// over the informational popups. Signature help is more immediately relevant
// than hover while entering a call. Keeping this decision in the editor makes
// the app renderer and input routing agree without exposing overlay state.
func (e Editor) LSPOverlayView() string {
	if view := e.autocomplete.View(); view != "" {
		return view
	}
	if view := e.signatureHelp.View(); view != "" {
		return view
	}
	return e.hover.View()
}

// IsAutocompleteVisible returns whether autocomplete popup is showing.
func (e Editor) IsAutocompleteVisible() bool {
	return e.autocomplete.Visible
}

// AutocompleteSelectAt selects and inserts the completion at the given index.
// It returns the retokenize command the caller must run, and whether a
// completion was inserted. The command matters: accepting a completion with the
// mouse changes the buffer exactly as the keyboard path does, so it must
// refresh syntax highlighting the same way instead of leaving stale colours.
func (e *Editor) AutocompleteSelectAt(idx int) (tea.Cmd, bool) {
	if item := e.autocomplete.SelectAt(idx); item != nil {
		e.applyCompletion(*item)
		e.autocomplete.Hide()
		return e.scheduleRetokenize(), true
	}
	return nil, false
}

// applyCompletion inserts a completion, honouring the server's replacement
// range when it supplied one. Servers routinely return a textEdit covering the
// identifier already typed together with the full replacement text; inserting
// that at the cursor instead of replacing the range leaves the prefix behind
// (typing "fm" and accepting "fmt" produced "fmfmt").
func (e *Editor) applyCompletion(item overlays.AutocompleteItem) {
	if item.HasEdit && e.completionEditIsApplicable(item.Edit) {
		start := text.Position{Line: item.Edit.StartLine, Col: item.Edit.StartCol}
		end := text.Position{Line: item.Edit.EndLine, Col: item.Edit.EndCol}
		startOffset := e.Buffer.Rope().PositionToOffset(start)
		e.Buffer.ReplaceRange(start, end, []byte(item.InsertText))
		e.Buffer.SetCursor(e.Buffer.Rope().OffsetToPosition(startOffset + len(item.InsertText)))
	} else {
		e.Buffer.InsertAtCursor([]byte(item.InsertText))
	}
	e.refreshWordWrapAfterBufferChange()
	e.EnsureCursorVisible()
}

// completionEditIsApplicable rejects ranges that no longer describe the buffer.
// A completion request is asynchronous, so by the time the user accepts it the
// text may have moved; applying a stale range would corrupt unrelated content.
func (e *Editor) completionEditIsApplicable(edit overlays.AutocompleteEdit) bool {
	if edit.StartLine < 0 || edit.StartCol < 0 || edit.EndCol < 0 {
		return false
	}
	if edit.EndLine < edit.StartLine {
		return false
	}
	if edit.EndLine == edit.StartLine && edit.EndCol < edit.StartCol {
		return false
	}
	lineCount := e.Buffer.LineCount()
	if edit.StartLine >= lineCount || edit.EndLine >= lineCount {
		return false
	}
	if edit.StartCol > e.Buffer.Rope().LineLen(edit.StartLine) || edit.EndCol > e.Buffer.Rope().LineLen(edit.EndLine) {
		return false
	}
	// The edit must cover the cursor: that is what makes it the replacement for
	// what the user is currently typing rather than an edit elsewhere.
	cursor := e.Buffer.Cursor
	if cursor.Line < edit.StartLine || cursor.Line > edit.EndLine {
		return false
	}
	if cursor.Line == edit.StartLine && cursor.Col < edit.StartCol {
		return false
	}
	if cursor.Line == edit.EndLine && cursor.Col > edit.EndCol {
		return false
	}
	return true
}

// ShowFind opens the in-buffer find widget.
func (e *Editor) ShowFind() {
	e.find.SetEditorID(e.id)
	e.find.Show()
}

// HideFind closes the in-buffer find widget.
func (e *Editor) HideFind() {
	e.find.Hide()
}

// IsFindVisible returns whether the in-buffer find widget is open.
func (e Editor) IsFindVisible() bool {
	return e.find.Visible()
}

// UpdateFind handles input for the find widget. Returns a command if the
// cursor should move to a match.
func (e *Editor) UpdateFind(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	e.find, cmd = e.find.Update(msg, e.Buffer)
	if m := e.find.CurrentMatchPosition(); m != nil {
		e.Buffer.SetCursor(m.Start)
		e.EnsureCursorVisible()
	}
	return cmd
}

// FindView renders the find widget.
func (e Editor) FindView() string {
	return e.find.View()
}

// FindMatchRangesForLine returns find match highlight ranges for a line.
func (e Editor) FindMatchRangesForLine(line int) []selectionByteRange {
	return e.find.FindMatchRangesForLine(line)
}

// CurrentFindMatchRangesForLine returns the current match highlight for a line.
func (e Editor) CurrentFindMatchRangesForLine(line int) []selectionByteRange {
	return e.find.CurrentMatchRangesForLine(line)
}

// ContextMenuView returns the context menu popup rendering if visible.
func (e Editor) ContextMenuView() string {
	return e.contextMenu.View()
}

// IsContextMenuVisible returns whether the context menu is showing.
func (e Editor) IsContextMenuVisible() bool {
	return e.contextMenu.Visible
}

// ContextMenuPosition returns the screen position of the context menu.
func (e Editor) ContextMenuPosition() (int, int) {
	return e.contextMenu.X, e.contextMenu.Y
}

// HideContextMenu dismisses the context menu.
func (e *Editor) HideContextMenu() {
	e.contextMenu.Hide()
}

// ClickContextMenuItem handles a mouse click at the given menu-relative Y index.
// Returns the action string if an item was selected, or "" if dismissed.
func (e *Editor) ClickContextMenuItem(relY int) (Editor, tea.Cmd, string) {
	if item := e.contextMenu.SelectAt(relY); item != nil {
		action := item.Action
		e.contextMenu.Hide()
		ed, cmd := e.dispatchContextMenuAction(action)
		return ed, cmd, action
	}
	e.contextMenu.Hide()
	return *e, nil, ""
}

// ContextMenuItemCount returns the number of visible context menu items.
func (e Editor) ContextMenuItemCount() int {
	return e.contextMenu.ItemCount()
}

// buildEditorMenuItems returns context menu items based on current editor state.
func (e Editor) buildEditorMenuItems() []ContextMenuItem {
	hasSelection := e.Buffer.Selections != nil && e.Buffer.Selections.Count() > 0 && !e.Buffer.Selections.Primary().IsEmpty()

	items := []ContextMenuItem{
		{Label: "Cut", Shortcut: "Ctrl+X", Action: "cut", Disabled: !hasSelection},
		{Label: "Copy", Shortcut: "Ctrl+C", Action: "copy", Disabled: !hasSelection},
		{Label: "Paste", Shortcut: "Ctrl+V", Action: "paste"},
		{Label: ""}, // separator
		{Label: "Select All", Shortcut: "Ctrl+A", Action: "select_all"},
	}

	if e.HasLSP {
		items = append(items,
			ContextMenuItem{Label: ""}, // separator
			ContextMenuItem{Label: "Go to Definition", Shortcut: "F12", Action: "goto_definition"},
			ContextMenuItem{Label: "Find References", Action: "find_references"},
			ContextMenuItem{Label: "Rename Symbol", Action: "rename_symbol"},
		)
	}

	items = append(items,
		ContextMenuItem{Label: ""}, // separator
		ContextMenuItem{Label: "Undo", Shortcut: "Ctrl+Z", Action: "undo"},
		ContextMenuItem{Label: "Redo", Shortcut: "Ctrl+Y", Action: "redo"},
		ContextMenuItem{Label: ""}, // separator
		ContextMenuItem{Label: "Toggle Comment", Shortcut: "Ctrl+/", Action: "toggle_comment"},
	)

	return items
}

// dispatchContextMenuAction handles editor-local actions and returns a message for app-level ones.
func (e Editor) dispatchContextMenuAction(action string) (Editor, tea.Cmd) {
	switch action {
	case "cut":
		if updated, cmd, handled := e.selectionClipboardCopy(true); handled {
			return updated, cmd
		}
		return e, nil
	case "copy":
		if updated, cmd, handled := e.selectionClipboardCopy(false); handled {
			return updated, cmd
		}
		return e, nil
	case "paste":
		return e, e.requestClipboardPaste()
	case "select_all":
		e.Buffer.SelectAll()
		return e, nil
	case "undo":
		e.Buffer.Undo()
		e.refreshWordWrapAfterBufferChange()
		e.EnsureCursorVisible()
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, e.scheduleRetokenize()
	case "redo":
		e.Buffer.Redo()
		e.refreshWordWrapAfterBufferChange()
		e.EnsureCursorVisible()
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, e.scheduleRetokenize()
	case "toggle_comment":
		if cmd, allowed := e.multilineEditWithinBudget("Toggle comment"); !allowed {
			return e, cmd
		}
		e.Buffer.ToggleLineComment(e.Config.CommentPrefix)
		e.refreshWordWrapAfterBufferChange()
		e.EnsureCursorVisible()
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
		return e, e.scheduleRetokenize()
	default:
		// LSP actions dispatch to the app layer
		return e, func() tea.Msg {
			return ContextMenuActionMsg{Action: action}
		}
	}
}
