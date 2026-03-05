package highlight

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// StyledToken represents a token with its lipgloss style.
type StyledToken struct {
	Text  string
	Style lipgloss.Style
}

// Highlighter provides syntax highlighting for a file.
type Highlighter struct {
	lexer          chroma.Lexer
	lines          [][]StyledToken
	dirty          bool
	styleMap       map[chroma.TokenType]lipgloss.Style
	theme          ui.Theme
	tokenizedStart int
	tokenizedEnd   int
}

// New creates a new Highlighter based on the filename for language detection.
func New(filename string, theme ui.Theme) *Highlighter {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	return &Highlighter{
		lexer:          lexer,
		dirty:          true,
		styleMap:       buildStyleMap(theme),
		theme:          theme,
		tokenizedStart: -1,
		tokenizedEnd:   -1,
	}
}

// Tokenize processes the full buffer content and caches per-line tokens.
func (h *Highlighter) Tokenize(content []byte) {
	h.lines = h.tokenizeContent(content, -1, -1)
	h.tokenizedStart = 0
	h.tokenizedEnd = len(h.lines)
	h.dirty = false
}

// TokenizeToLines tokenizes content and returns the result without mutating state.
// Safe for use from goroutines (lexer and styleMap are immutable after creation).
func (h *Highlighter) TokenizeToLines(content []byte) [][]StyledToken {
	return h.tokenizeContent(content, -1, -1)
}

// TokenizeViewportToLines tokenizes content but only materializes styled tokens
// for lines in [viewStart-margin, viewEnd+margin]. The lexer still runs on full
// content to maintain correct state.
func (h *Highlighter) TokenizeViewportToLines(content []byte, viewStart, viewEnd int) [][]StyledToken {
	return h.tokenizeContent(content, viewStart, viewEnd)
}

// SetLines sets cached lines from an async tokenization result.
// If the new result is shorter than the existing cache (i.e. a viewport-scoped
// partial tokenization), it merges the new lines into the existing array so
// that lines outside the viewport retain their previous highlighting.
func (h *Highlighter) SetLines(lines [][]StyledToken) {
	if h.lines != nil && len(lines) < len(h.lines) {
		// Partial result — merge non-empty lines into existing cache.
		// Viewport-scoped tokenization leaves lines outside the range as nil/empty;
		// we keep the old data for those.
		for i, line := range lines {
			if i < len(h.lines) && len(line) > 0 {
				h.lines[i] = line
			}
		}
	} else {
		h.lines = lines
		h.tokenizedStart = 0
		h.tokenizedEnd = len(lines)
	}
	h.dirty = false
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

	result := h.streamTokenize(string(content[:end]), -1, -1)
	h.lines = result
	h.tokenizedStart = 0
	h.tokenizedEnd = len(result)
	h.dirty = false
}

func (h *Highlighter) tokenizeContent(content []byte, viewStart, viewEnd int) [][]StyledToken {
	return h.streamTokenize(string(content), viewStart, viewEnd)
}

// streamTokenize uses Chroma's iterator lazily, streaming tokens and splitting
// into lines on the fly. When a viewport range is specified, it stops consuming
// the lexer after passing viewEnd+margin, avoiding lexing the full file tail.
func (h *Highlighter) streamTokenize(content string, viewStart, viewEnd int) [][]StyledToken {
	iterator, err := h.lexer.Tokenise(nil, content)
	if err != nil {
		return nil
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
		if tok.Value == "" {
			continue
		}

		val := tok.Value
		style := lipgloss.Style{}
		styleResolved := false

		for {
			nlIdx := strings.IndexByte(val, '\n')
			if nlIdx < 0 {
				break
			}
			// Text before the newline
			part := val[:nlIdx]
			if len(part) > 0 && inRange() {
				if !styleResolved {
					style = h.styleForToken(tok.Type)
					styleResolved = true
				}
				currentLine = append(currentLine, StyledToken{Text: part, Style: style})
			}
			lines = append(lines, currentLine)
			currentLine = nil
			lineNum++
			val = val[nlIdx+1:]

			// Early exit: past viewport range, stop lexing
			if rangeEnd >= 0 && lineNum > rangeEnd {
				return lines
			}
		}
		// Remaining text (no newline)
		if len(val) > 0 && inRange() {
			if !styleResolved {
				style = h.styleForToken(tok.Type)
				styleResolved = true
			}
			currentLine = append(currentLine, StyledToken{Text: val, Style: style})
		}
	}

	if currentLine != nil {
		lines = append(lines, currentLine)
	}
	if len(lines) == 0 {
		lines = append(lines, nil)
	}
	return lines
}

// Line returns the styled tokens for a given line number (0-based).
// Returns nil if the line hasn't been tokenized.
func (h *Highlighter) Line(lineNum int) []StyledToken {
	if h.lines == nil || lineNum < 0 || lineNum >= len(h.lines) {
		return nil
	}
	return h.lines[lineNum]
}

// LineCount returns the number of tokenized lines.
func (h *Highlighter) LineCount() int {
	if h.lines == nil {
		return 0
	}
	return len(h.lines)
}

// Invalidate marks the cached tokens as stale.
func (h *Highlighter) Invalidate() {
	h.dirty = true
}

// IsDirty returns true if tokens need re-generation.
func (h *Highlighter) IsDirty() bool {
	return h.dirty
}

func (h *Highlighter) styleForToken(tt chroma.TokenType) lipgloss.Style {
	// Walk up the token type hierarchy to find a match
	for t := tt; t > 0; t = t.Parent() {
		if style, ok := h.styleMap[t]; ok {
			return style
		}
	}
	return h.theme.Editor
}

