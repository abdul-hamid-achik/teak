package text

import (
	"bytes"
	"os"
	"strings"
	"unicode/utf8"
)

// Direction constants for cursor movement.
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

// Buffer wraps a Rope with cursor, selection, undo, and file I/O.
type Buffer struct {
	rope      *Rope
	Cursor    Position
	Selection *Selection
	undo      *UndoStack
	FilePath  string
	dirty     bool
	savedRope *Rope
	version   int
}

// NewBuffer creates an empty buffer.
func NewBuffer() *Buffer {
	r := NewFromString("")
	return &Buffer{
		rope:      r,
		undo:      NewUndoStack(),
		savedRope: r,
	}
}

// NewBufferFromBytes creates a buffer with initial content.
func NewBufferFromBytes(data []byte) *Buffer {
	r := New(data)
	return &Buffer{
		rope:      r,
		undo:      NewUndoStack(),
		savedRope: r,
	}
}

// NewBufferFromFile loads a buffer from a file path.
func NewBufferFromFile(path string) (*Buffer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := New(data)
	return &Buffer{
		rope:      r,
		undo:      NewUndoStack(),
		FilePath:  path,
		savedRope: r,
	}, nil
}

// LoadContent replaces the buffer contents with data, resetting cursor and undo.
// Used for async file loading into a placeholder buffer.
func (b *Buffer) LoadContent(data []byte) {
	b.LoadContentWithTabSize(data, 4)
}

// LoadContentWithTabSize replaces the buffer contents, expanding tabs to spaces.
func (b *Buffer) LoadContentWithTabSize(data []byte, tabSize int) {
	// Expand tabs to spaces for consistent rendering
	expanded := expandTabs(data, tabSize)
	r := New(expanded)
	b.rope = r
	b.savedRope = r
	b.Cursor = Position{}
	b.Selection = nil
	b.undo = NewUndoStack()
	b.dirty = false
	b.version++
}

// expandTabs replaces tab characters with spaces aligned to tabSize stops.
func expandTabs(data []byte, tabSize int) []byte {
	if !bytes.ContainsRune(data, '\t') {
		return data
	}
	var result []byte
	col := 0
	for _, b := range data {
		if b == '\t' {
			spaces := tabSize - (col % tabSize)
			for range spaces {
				result = append(result, ' ')
			}
			col += spaces
		} else if b == '\n' {
			result = append(result, b)
			col = 0
		} else {
			result = append(result, b)
			col++
		}
	}
	return result
}

// Rope returns the underlying rope.
func (b *Buffer) Rope() *Rope {
	return b.rope
}

// Dirty returns true if the buffer has unsaved changes.
func (b *Buffer) Dirty() bool {
	return b.dirty
}

// LineCount returns the number of lines.
func (b *Buffer) LineCount() int {
	return b.rope.LineCount()
}

// Line returns the content of the given line.
func (b *Buffer) Line(line int) []byte {
	return b.rope.Line(line)
}

// InsertAtCursor inserts text at the current cursor position.
func (b *Buffer) InsertAtCursor(text []byte) {
	if b.Selection != nil {
		b.DeleteSelection()
	}
	isCharInsert := len(text) == 1
	b.undo.Save(b.rope, b.Cursor, isCharInsert)
	offset := b.rope.PositionToOffset(b.Cursor)
	b.rope = b.rope.Insert(offset, text)
	b.dirty = true
	b.version++
	b.Cursor = b.rope.OffsetToPosition(offset + len(text))
}

// InsertNewline inserts a newline at the cursor.
func (b *Buffer) InsertNewline() {
	b.InsertAtCursor([]byte{'\n'})
}

// InsertNewlineWithIndent inserts a newline and copies leading whitespace from the current line.
func (b *Buffer) InsertNewlineWithIndent() {
	ws := LeadingWhitespace(b.rope.Line(b.Cursor.Line))
	b.InsertAtCursor(append([]byte{'\n'}, ws...))
}

// DedentLine removes up to tabSize leading spaces from the current line, adjusting the cursor.
func (b *Buffer) DedentLine(tabSize int) {
	lineContent := b.rope.Line(b.Cursor.Line)
	n := Dedent(lineContent, tabSize)
	if n == 0 {
		return
	}
	b.undo.Save(b.rope, b.Cursor, false)
	lineStart := b.rope.LineStart(b.Cursor.Line)
	b.rope = b.rope.Delete(lineStart, n)
	b.dirty = true
	b.version++
	b.Cursor.Col = max(0, b.Cursor.Col-n)
}

// Backspace deletes the character before the cursor.
func (b *Buffer) Backspace() {
	if b.Selection != nil {
		b.DeleteSelection()
		return
	}
	offset := b.rope.PositionToOffset(b.Cursor)
	if offset == 0 {
		return
	}
	delLen := 1
	if offset >= 2 {
		lineContent := b.rope.Line(b.Cursor.Line)
		col := b.Cursor.Col
		if b.Cursor.Col == 0 {
			delLen = 1
		} else if col <= len(lineContent) {
			_, size := utf8.DecodeLastRune(lineContent[:col])
			if size > 0 {
				delLen = size
			}
		}
	}
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = b.rope.Delete(offset-delLen, delLen)
	b.dirty = true
	b.version++
	b.Cursor = b.rope.OffsetToPosition(offset - delLen)
}

// Delete deletes the character at the cursor.
func (b *Buffer) Delete() {
	if b.Selection != nil {
		b.DeleteSelection()
		return
	}
	offset := b.rope.PositionToOffset(b.Cursor)
	if offset >= b.rope.Len() {
		return
	}
	delLen := 1
	lineContent := b.rope.Line(b.Cursor.Line)
	col := b.Cursor.Col
	if col < len(lineContent) {
		_, size := utf8.DecodeRune(lineContent[col:])
		if size > 0 {
			delLen = size
		}
	}
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = b.rope.Delete(offset, delLen)
	b.dirty = true
	b.version++
}

// DeleteSelection removes the selected text and clears the selection.
func (b *Buffer) DeleteSelection() {
	if b.Selection == nil {
		return
	}
	start, end := b.Selection.Ordered()
	startOff := b.rope.PositionToOffset(start)
	endOff := b.rope.PositionToOffset(end)
	n := endOff - startOff
	if n <= 0 {
		b.Selection = nil
		return
	}
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = b.rope.Delete(startOff, n)
	b.dirty = true
	b.version++
	b.Cursor = start
	b.Selection = nil
}

// ReplaceRange replaces text between start and end positions with newText.
func (b *Buffer) ReplaceRange(start, end Position, newText []byte) {
	startOff := b.rope.PositionToOffset(start)
	endOff := b.rope.PositionToOffset(end)
	n := endOff - startOff
	b.undo.Save(b.rope, b.Cursor, false)
	if n > 0 {
		b.rope = b.rope.Delete(startOff, n)
	}
	if len(newText) > 0 {
		b.rope = b.rope.Insert(startOff, newText)
	}
	b.dirty = true
	b.version++
}

// MoveCursor moves the cursor in the given direction.
func (b *Buffer) MoveCursor(dir Direction) {
	switch dir {
	case DirLeft:
		if b.Cursor.Col > 0 {
			lineContent := b.rope.Line(b.Cursor.Line)
			_, size := utf8.DecodeLastRune(lineContent[:b.Cursor.Col])
			b.Cursor.Col -= size
		} else if b.Cursor.Line > 0 {
			b.Cursor.Line--
			b.Cursor.Col = b.rope.LineLen(b.Cursor.Line)
		}
	case DirRight:
		lineLen := b.rope.LineLen(b.Cursor.Line)
		if b.Cursor.Col < lineLen {
			lineContent := b.rope.Line(b.Cursor.Line)
			_, size := utf8.DecodeRune(lineContent[b.Cursor.Col:])
			b.Cursor.Col += size
		} else if b.Cursor.Line < b.rope.LineCount()-1 {
			b.Cursor.Line++
			b.Cursor.Col = 0
		}
	case DirUp:
		if b.Cursor.Line > 0 {
			b.Cursor.Line--
			b.Cursor.Col = min(b.Cursor.Col, b.rope.LineLen(b.Cursor.Line))
		}
	case DirDown:
		if b.Cursor.Line < b.rope.LineCount()-1 {
			b.Cursor.Line++
			b.Cursor.Col = min(b.Cursor.Col, b.rope.LineLen(b.Cursor.Line))
		}
	}
}

// SetSelection sets the selection anchored at the anchor, with head as the cursor.
func (b *Buffer) SetSelection(anchor, head Position) {
	b.Selection = &Selection{Anchor: anchor, Head: head}
	b.Cursor = head
}

// ClearSelection clears any active selection.
func (b *Buffer) ClearSelection() {
	b.Selection = nil
}

// CursorToLineStart moves the cursor to the beginning of the current line.
func (b *Buffer) CursorToLineStart() {
	b.Cursor.Col = 0
}

// CursorToLineEnd moves the cursor to the end of the current line.
func (b *Buffer) CursorToLineEnd() {
	b.Cursor.Col = b.rope.LineLen(b.Cursor.Line)
}

// Save writes the buffer to its FilePath.
func (b *Buffer) Save() error {
	if b.FilePath == "" {
		return nil
	}
	return b.SaveAs(b.FilePath)
}

// SaveAs writes the buffer to the given path.
func (b *Buffer) SaveAs(path string) error {
	data := b.rope.Bytes()
	err := os.WriteFile(path, data, 0644)
	if err != nil {
		return err
	}
	b.FilePath = path
	b.dirty = false
	b.savedRope = b.rope
	return nil
}

// Undo undoes the last edit.
func (b *Buffer) Undo() {
	rope, cursor, ok := b.undo.Undo(b.rope, b.Cursor)
	if !ok {
		return
	}
	b.rope = rope
	b.Cursor = cursor
	b.Selection = nil
	b.dirty = b.rope != b.savedRope
	b.version++
}

// Redo redoes the last undone edit.
func (b *Buffer) Redo() {
	rope, cursor, ok := b.undo.Redo(b.rope, b.Cursor)
	if !ok {
		return
	}
	b.rope = rope
	b.Cursor = cursor
	b.Selection = nil
	b.dirty = b.rope != b.savedRope
	b.version++
}

// Content returns the full buffer content as a string.
func (b *Buffer) Content() string {
	return b.rope.String()
}

// Version returns a monotonically increasing version number, incremented on each edit.
func (b *Buffer) Version() int {
	return b.version
}

// Bytes returns the full buffer content as a byte slice.
func (b *Buffer) Bytes() []byte {
	return b.rope.Bytes()
}

// SelectedText returns the currently selected text, or empty if no selection.
func (b *Buffer) SelectedText() []byte {
	if b.Selection == nil || b.Selection.IsEmpty() {
		return nil
	}
	start, end := b.Selection.Ordered()
	startOff := b.rope.PositionToOffset(start)
	endOff := b.rope.PositionToOffset(end)
	return b.rope.Slice(startOff, endOff).Bytes()
}

// Word boundary helpers

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t'
}

func trimLeadingWhitespace(b []byte) []byte {
	return bytes.TrimLeft(b, " \t")
}

// MoveCursorWordLeft moves the cursor to the start of the previous word.
func (b *Buffer) MoveCursorWordLeft() {
	line := b.rope.Line(b.Cursor.Line)
	col := b.Cursor.Col

	if col == 0 {
		if b.Cursor.Line > 0 {
			b.Cursor.Line--
			b.Cursor.Col = b.rope.LineLen(b.Cursor.Line)
		}
		return
	}

	if col > len(line) {
		col = len(line)
	}

	// Skip whitespace backwards
	for col > 0 && isSpaceByte(line[col-1]) {
		col--
	}
	if col == 0 {
		b.Cursor.Col = 0
		return
	}

	// Skip same-class characters backwards
	if isWordByte(line[col-1]) {
		for col > 0 && isWordByte(line[col-1]) {
			col--
		}
	} else {
		for col > 0 && !isWordByte(line[col-1]) && !isSpaceByte(line[col-1]) {
			col--
		}
	}
	b.Cursor.Col = col
}

// MoveCursorWordRight moves the cursor to the start of the next word.
func (b *Buffer) MoveCursorWordRight() {
	line := b.rope.Line(b.Cursor.Line)
	col := b.Cursor.Col
	lineLen := len(line)

	if col >= lineLen {
		if b.Cursor.Line < b.rope.LineCount()-1 {
			b.Cursor.Line++
			b.Cursor.Col = 0
		}
		return
	}

	// Skip same-class characters forward
	if isWordByte(line[col]) {
		for col < lineLen && isWordByte(line[col]) {
			col++
		}
	} else if !isSpaceByte(line[col]) {
		for col < lineLen && !isWordByte(line[col]) && !isSpaceByte(line[col]) {
			col++
		}
	}

	// Skip whitespace forward
	for col < lineLen && isSpaceByte(line[col]) {
		col++
	}
	b.Cursor.Col = col
}

// BackspaceWord deletes from the cursor to the start of the previous word.
func (b *Buffer) BackspaceWord() {
	if b.Selection != nil {
		b.DeleteSelection()
		return
	}
	startPos := b.Cursor
	b.MoveCursorWordLeft()
	if startPos == b.Cursor {
		return
	}
	startOff := b.rope.PositionToOffset(b.Cursor)
	endOff := b.rope.PositionToOffset(startPos)
	n := endOff - startOff
	b.undo.Save(b.rope, startPos, false)
	b.rope = b.rope.Delete(startOff, n)
	b.dirty = true
	b.version++
}

// DeleteWord deletes from the cursor to the start of the next word.
func (b *Buffer) DeleteWord() {
	if b.Selection != nil {
		b.DeleteSelection()
		return
	}
	saved := b.Cursor
	b.MoveCursorWordRight()
	endPos := b.Cursor
	b.Cursor = saved
	if saved == endPos {
		return
	}
	startOff := b.rope.PositionToOffset(saved)
	endOff := b.rope.PositionToOffset(endPos)
	n := endOff - startOff
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = b.rope.Delete(startOff, n)
	b.dirty = true
	b.version++
}

// SelectAll selects the entire buffer content.
func (b *Buffer) SelectAll() {
	lastLine := b.rope.LineCount() - 1
	lastCol := b.rope.LineLen(lastLine)
	b.SetSelection(Position{0, 0}, Position{lastLine, lastCol})
}

// CursorToDocStart moves the cursor to the beginning of the document.
func (b *Buffer) CursorToDocStart() {
	b.Cursor = Position{0, 0}
}

// CursorToDocEnd moves the cursor to the end of the document.
func (b *Buffer) CursorToDocEnd() {
	lastLine := b.rope.LineCount() - 1
	b.Cursor = Position{lastLine, b.rope.LineLen(lastLine)}
}

// ExtendSelection calls move and extends the selection from the current anchor.
// If no selection exists, anchors at the current cursor position before moving.
func (b *Buffer) ExtendSelection(move func()) {
	anchor := b.Cursor
	if b.Selection != nil {
		anchor = b.Selection.Anchor
	}
	move()
	if anchor == b.Cursor {
		b.ClearSelection()
	} else {
		b.SetSelection(anchor, b.Cursor)
	}
}

// SelectWordAtCursor selects the word under the cursor using isWordByte boundaries.
func (b *Buffer) SelectWordAtCursor() {
	line := b.rope.Line(b.Cursor.Line)
	col := b.Cursor.Col
	if col >= len(line) {
		return
	}
	ch := line[col]
	if isSpaceByte(ch) {
		return
	}

	start, end := col, col
	if isWordByte(ch) {
		for start > 0 && isWordByte(line[start-1]) {
			start--
		}
		for end < len(line) && isWordByte(line[end]) {
			end++
		}
	} else {
		// Punctuation: select contiguous punctuation
		for start > 0 && !isWordByte(line[start-1]) && !isSpaceByte(line[start-1]) {
			start--
		}
		for end < len(line) && !isWordByte(line[end]) && !isSpaceByte(line[end]) {
			end++
		}
	}
	b.SetSelection(
		Position{Line: b.Cursor.Line, Col: start},
		Position{Line: b.Cursor.Line, Col: end},
	)
}

// SelectNextOccurrence selects the next occurrence of the current selection, or selects word at cursor.
func (b *Buffer) SelectNextOccurrence() {
	if b.Selection == nil || b.Selection.IsEmpty() {
		b.SelectWordAtCursor()
		return
	}
	sel := b.SelectedText()
	if len(sel) == 0 {
		return
	}
	content := b.rope.Bytes()
	_, end := b.Selection.Ordered()
	endOff := b.rope.PositionToOffset(end)

	// Search forward from end of selection
	needle := string(sel)
	haystack := string(content)
	idx := strings.Index(haystack[endOff:], needle)
	if idx >= 0 {
		matchOff := endOff + idx
		matchEnd := matchOff + len(needle)
		b.SetSelection(b.rope.OffsetToPosition(matchOff), b.rope.OffsetToPosition(matchEnd))
		return
	}
	// Wrap around
	idx = strings.Index(haystack[:endOff], needle)
	if idx >= 0 {
		matchEnd := idx + len(needle)
		b.SetSelection(b.rope.OffsetToPosition(idx), b.rope.OffsetToPosition(matchEnd))
	}
}

// SelectLine selects the current line.
func (b *Buffer) SelectLine() {
	lineStart := Position{Line: b.Cursor.Line, Col: 0}
	if b.Cursor.Line < b.rope.LineCount()-1 {
		b.SetSelection(lineStart, Position{Line: b.Cursor.Line + 1, Col: 0})
	} else {
		b.SetSelection(lineStart, Position{Line: b.Cursor.Line, Col: b.rope.LineLen(b.Cursor.Line)})
	}
}

// ToggleLineComment toggles a line comment prefix on the current line or selection range.
func (b *Buffer) ToggleLineComment(prefix string) {
	if prefix == "" {
		return
	}
	startLine := b.Cursor.Line
	endLine := b.Cursor.Line
	if b.Selection != nil {
		s, e := b.Selection.Ordered()
		startLine = s.Line
		endLine = e.Line
		if e.Col == 0 && endLine > startLine {
			endLine--
		}
	}

	// Check if all lines are commented
	allCommented := true
	commentPrefix := prefix + " "
	for line := startLine; line <= endLine; line++ {
		content := b.rope.Line(line)
		trimmed := trimLeadingWhitespace(content)
		if len(trimmed) == 0 {
			continue // skip empty lines
		}
		if !strings.HasPrefix(string(trimmed), commentPrefix) && !strings.HasPrefix(string(trimmed), prefix) {
			allCommented = false
			break
		}
	}

	b.undo.Save(b.rope, b.Cursor, false)

	if allCommented {
		// Uncomment: remove prefix in reverse order
		for line := endLine; line >= startLine; line-- {
			content := b.rope.Line(line)
			idx := strings.Index(string(content), prefix)
			if idx < 0 {
				continue
			}
			removeLen := len(prefix)
			lineStart := b.rope.LineStart(line)
			// Also remove trailing space after prefix
			if idx+removeLen < len(content) && content[idx+removeLen] == ' ' {
				removeLen++
			}
			b.rope = b.rope.Delete(lineStart+idx, removeLen)
		}
	} else {
		// Comment: find min indent, insert prefix at that column in reverse order
		minIndent := -1
		for line := startLine; line <= endLine; line++ {
			content := b.rope.Line(line)
			if len(trimLeadingWhitespace(content)) == 0 {
				continue
			}
			indent := len(content) - len(trimLeadingWhitespace(content))
			if minIndent < 0 || indent < minIndent {
				minIndent = indent
			}
		}
		if minIndent < 0 {
			minIndent = 0
		}
		for line := endLine; line >= startLine; line-- {
			lineStart := b.rope.LineStart(line)
			b.rope = b.rope.Insert(lineStart+minIndent, []byte(commentPrefix))
		}
	}
	b.dirty = true
	b.version++
}

// MoveLineUp swaps the current line with the line above.
func (b *Buffer) MoveLineUp() {
	if b.Cursor.Line == 0 {
		return
	}
	b.undo.Save(b.rope, b.Cursor, false)
	curLine := b.Cursor.Line
	curContent := b.rope.Line(curLine)
	aboveContent := b.rope.Line(curLine - 1)

	// Replace above line with current, and current with above
	curStart := b.rope.LineStart(curLine)
	aboveStart := b.rope.LineStart(curLine - 1)

	// Delete both lines and re-insert swapped
	// Current line: from curStart to curStart+len(curContent)+1 (incl newline)
	// Above line: from aboveStart to aboveStart+len(aboveContent)+1 (incl newline)
	// Simpler: just swap the content bytes
	aboveLen := len(aboveContent)
	curLen := len(curContent)

	// Delete current line content (not newline)
	b.rope = b.rope.Delete(curStart, curLen)
	b.rope = b.rope.Insert(curStart, aboveContent)

	// Delete above line content (not newline)
	b.rope = b.rope.Delete(aboveStart, aboveLen)
	b.rope = b.rope.Insert(aboveStart, curContent)

	b.Cursor.Line--
	b.dirty = true
	b.version++
}

// MoveLineDown swaps the current line with the line below.
func (b *Buffer) MoveLineDown() {
	if b.Cursor.Line >= b.rope.LineCount()-1 {
		return
	}
	b.undo.Save(b.rope, b.Cursor, false)
	curLine := b.Cursor.Line
	curContent := b.rope.Line(curLine)
	belowContent := b.rope.Line(curLine + 1)

	belowStart := b.rope.LineStart(curLine + 1)
	curStart := b.rope.LineStart(curLine)

	belowLen := len(belowContent)
	curLen := len(curContent)

	// Delete below line content first (higher offset)
	b.rope = b.rope.Delete(belowStart, belowLen)
	b.rope = b.rope.Insert(belowStart, curContent)

	// Delete current line content
	b.rope = b.rope.Delete(curStart, curLen)
	b.rope = b.rope.Insert(curStart, belowContent)

	b.Cursor.Line++
	b.dirty = true
	b.version++
}

// DuplicateLineDown duplicates the current line below.
func (b *Buffer) DuplicateLineDown() {
	b.undo.Save(b.rope, b.Cursor, false)
	content := b.rope.Line(b.Cursor.Line)
	lineStart := b.rope.LineStart(b.Cursor.Line)
	// Insert newline + copy after the current line
	insert := append([]byte{'\n'}, content...)
	b.rope = b.rope.Insert(lineStart+len(content), insert)
	b.Cursor.Line++
	b.dirty = true
	b.version++
}

// DuplicateLineUp duplicates the current line above.
func (b *Buffer) DuplicateLineUp() {
	b.undo.Save(b.rope, b.Cursor, false)
	content := b.rope.Line(b.Cursor.Line)
	lineStart := b.rope.LineStart(b.Cursor.Line)
	insert := append(append([]byte{}, content...), '\n')
	b.rope = b.rope.Insert(lineStart, insert)
	// Cursor stays on the same content (now one line down), but we want it on the duplicate above
	// So don't change Cursor.Line
	b.dirty = true
	b.version++
}

// DeleteLine deletes the current line.
func (b *Buffer) DeleteLine() {
	b.undo.Save(b.rope, b.Cursor, false)
	lineStart := b.rope.LineStart(b.Cursor.Line)
	lineLen := len(b.rope.Line(b.Cursor.Line))

	if b.Cursor.Line < b.rope.LineCount()-1 {
		// Delete line content + trailing newline
		b.rope = b.rope.Delete(lineStart, lineLen+1)
	} else if b.Cursor.Line > 0 {
		// Last line: delete preceding newline + content
		b.rope = b.rope.Delete(lineStart-1, lineLen+1)
		b.Cursor.Line--
	} else {
		// Only line: replace with empty
		b.rope = b.rope.Delete(lineStart, lineLen)
	}
	b.Cursor.Col = min(b.Cursor.Col, b.rope.LineLen(b.Cursor.Line))
	b.dirty = true
	b.version++
}

// IndentLines indents the current line or all lines in selection.
func (b *Buffer) IndentLines(tabSize int) {
	startLine := b.Cursor.Line
	endLine := b.Cursor.Line
	if b.Selection != nil {
		s, e := b.Selection.Ordered()
		startLine = s.Line
		endLine = e.Line
		if e.Col == 0 && endLine > startLine {
			endLine--
		}
	}

	b.undo.Save(b.rope, b.Cursor, false)
	indent := IndentString(tabSize)
	for line := endLine; line >= startLine; line-- {
		lineStart := b.rope.LineStart(line)
		b.rope = b.rope.Insert(lineStart, indent)
	}
	b.dirty = true
	b.version++
}

// DedentLines removes one level of indentation from the current line or selection.
func (b *Buffer) DedentLines(tabSize int) {
	startLine := b.Cursor.Line
	endLine := b.Cursor.Line
	if b.Selection != nil {
		s, e := b.Selection.Ordered()
		startLine = s.Line
		endLine = e.Line
		if e.Col == 0 && endLine > startLine {
			endLine--
		}
	}

	b.undo.Save(b.rope, b.Cursor, false)
	for line := endLine; line >= startLine; line-- {
		content := b.rope.Line(line)
		n := Dedent(content, tabSize)
		if n > 0 {
			lineStart := b.rope.LineStart(line)
			b.rope = b.rope.Delete(lineStart, n)
		}
	}
	b.dirty = true
	b.version++
}
