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
	if !strings.Contains(s, "$") {
		return s, -1
	}
	var b strings.Builder
	b.Grow(len(s))
	caret := -1
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				b.WriteByte('$')
				i++
				continue
			}
			inner := s[i+2 : i+2+end]
			name, def, hasDef := strings.Cut(inner, ":")
			if tab, err := strconv.Atoi(name); err == nil {
				if tab == 0 {
					caret = b.Len()
				}
				if hasDef {
					b.WriteString(def)
				}
				i += 3 + end
				continue
			}
			b.WriteByte('$')
			i++
			continue
		}
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > i+1 {
			if tab, err := strconv.Atoi(s[i+1 : j]); err == nil && tab == 0 {
				caret = b.Len()
			}
			i = j
			continue
		}
		b.WriteByte('$')
		i++
	}
	return b.String(), caret
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
