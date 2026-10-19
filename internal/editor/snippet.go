package editor

import (
	"strconv"
	"strings"

	"teak/internal/editor/overlays"
	"teak/internal/text"
)

// expandSnippet turns a subset of LSP snippets into plain text. $0 / ${0}
// mark the caret; ${1:default} becomes default; $n placeholders are removed.
// The returned caret is a byte offset into the expanded text, or -1 to leave
// the cursor at the end.
func expandSnippet(s string) (string, int) {
	// Completion items do not carry their insertTextFormat this far, so plain
	// text reaches here too. Without a "$" there is no snippet syntax to
	// expand, and leaving the text untouched keeps literal backslashes (such as
	// Windows paths) in plain-text items intact.
	if !strings.Contains(s, "$") {
		return s, -1
	}
	p := snippetParser{s: s, closes: snippetPlaceholderCloses(s), caret: -1}
	p.buf = make([]byte, 0, len(s))
	p.text(0, len(s))
	return string(p.buf), p.caret
}

// snippetParser expands LSP snippet syntax. Placeholders are matched up front
// so an unterminated "${" stays literal and nested defaults such as
// "${1:${2:x}}" resolve to their innermost text.
type snippetParser struct {
	s      string
	closes map[int]int // index of each "${" that has a matching "}" -> index of that "}"
	buf    []byte
	caret  int
}

// snippetPlaceholderCloses pairs every "${" with its "}", skipping escaped
// characters. Opens left without a close are absent from the map.
func snippetPlaceholderCloses(s string) map[int]int {
	closes := make(map[int]int)
	var open []int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if i+1 < len(s) && isSnippetEscapable(s[i+1]) {
				i++
			}
		case '$':
			if i+1 < len(s) && s[i+1] == '{' {
				open = append(open, i)
				i++
			}
		case '}':
			if n := len(open); n > 0 {
				closes[open[n-1]] = i
				open = open[:n-1]
			}
		}
	}
	return closes
}

func isSnippetEscapable(c byte) bool {
	return c == '$' || c == '}' || c == '\\'
}

// text writes s[i:end], honoring escapes and placeholders.
func (p *snippetParser) text(i, end int) {
	for i < end {
		switch c := p.s[i]; {
		case c == '\\' && i+1 < end && isSnippetEscapable(p.s[i+1]):
			p.buf = append(p.buf, p.s[i+1])
			i += 2
		case c == '$':
			i = p.dollar(i, end)
		default:
			p.buf = append(p.buf, c)
			i++
		}
	}
}

// dollar expands the construct starting at the '$' at i and returns the index
// after it. Forms that are not a recognised tabstop are kept as literal text.
func (p *snippetParser) dollar(i, end int) int {
	s := p.s
	if i+1 < end && s[i+1] == '{' {
		closeAt, matched := p.closes[i]
		if matched && closeAt < end {
			k := i + 2
			for k < closeAt && isSnippetDigit(s[k]) {
				k++
			}
			tab, err := strconv.Atoi(s[i+2 : k])
			if k > i+2 && err == nil {
				switch {
				case k == closeAt:
					p.markTabstop(tab)
					return closeAt + 1
				case s[k] == ':':
					p.markTabstop(tab)
					p.text(k+1, closeAt)
					return closeAt + 1
				}
			}
		}
		p.buf = append(p.buf, '$')
		return i + 1
	}
	j := i + 1
	for j < end && isSnippetDigit(s[j]) {
		j++
	}
	if j > i+1 {
		if tab, err := strconv.Atoi(s[i+1 : j]); err == nil {
			p.markTabstop(tab)
		}
		return j
	}
	p.buf = append(p.buf, '$')
	return i + 1
}

// markTabstop records the caret when the tabstop is $0.
func (p *snippetParser) markTabstop(tab int) {
	if tab == 0 {
		p.caret = len(p.buf)
	}
}

func isSnippetDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func (e *Editor) applyAdditionalCompletionEdits(edits []overlays.AutocompleteTextEdit) {
	if e.Buffer == nil || len(edits) == 0 {
		return
	}
	ordered := append([]overlays.AutocompleteTextEdit(nil), edits...)
	// Apply from the end of the document so earlier ranges stay valid.
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if additionalEditBefore(ordered[j], ordered[i]) {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	for _, edit := range ordered {
		if !e.additionalEditInRange(edit) {
			continue
		}
		start := text.Position{Line: edit.StartLine, Col: edit.StartCol}
		end := text.Position{Line: edit.EndLine, Col: edit.EndCol}
		e.Buffer.ReplaceRange(start, end, []byte(edit.NewText))
	}
}

func additionalEditBefore(a, b overlays.AutocompleteTextEdit) bool {
	if a.StartLine != b.StartLine {
		return a.StartLine > b.StartLine
	}
	return a.StartCol > b.StartCol
}

func (e Editor) additionalEditInRange(edit overlays.AutocompleteTextEdit) bool {
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
	return true
}
