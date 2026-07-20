package text

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// MaxBufferFileBytes bounds the synchronous Buffer constructor as well as the
// application loader. A Buffer represents an in-memory editable document, not
// an unbounded stream.
const MaxBufferFileBytes int64 = 64 << 20

var (
	ErrBufferFileTooLarge   = errors.New("buffer file exceeds the 64 MiB editor limit")
	ErrBufferFileNotRegular = errors.New("buffer input is not a regular file")
	ErrBufferFileChanged    = errors.New("buffer file changed while opening")
)

// Direction constants for cursor movement.
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

// EditChange describes an incremental text change for LSP sync.
// StartLine/StartCol and EndLine/EndCol are 0-based positions in the
// document BEFORE the edit. Text is the replacement string.
type EditChange struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Text      string
}

// Buffer wraps a Rope with cursor, selection, undo, and file I/O.
type Buffer struct {
	rope       *Rope
	Cursor     Position
	Selections *Selections
	undo       *UndoStack
	FilePath   string
	dirty      bool
	savedRope  *Rope
	version    int
	lastChange *EditChange // incremental change from last edit, nil if unknown
}

// NewBuffer creates an empty buffer.
func NewBuffer() *Buffer {
	r := NewFromString("")
	return &Buffer{
		rope:       r,
		Selections: NewSelections(Position{}),
		undo:       NewUndoStack(),
		savedRope:  r,
	}
}

// NewBufferFromBytes creates a buffer with initial content.
func NewBufferFromBytes(data []byte) *Buffer {
	r := New(data)
	return &Buffer{
		rope:       r,
		Selections: NewSelections(Position{}),
		undo:       NewUndoStack(),
		savedRope:  r,
	}
}

// NewBufferFromRope creates a buffer sharing an immutable rope snapshot. It
// avoids materialising large documents when background work needs to prepare a
// candidate edit without touching the live buffer.
func NewBufferFromRope(rope *Rope) *Buffer {
	if rope == nil {
		return NewBuffer()
	}
	return &Buffer{
		rope:       rope,
		Selections: NewSelections(Position{}),
		undo:       NewUndoStack(),
		savedRope:  rope,
	}
}

// NewBufferFromFile loads a buffer from a file path.
func NewBufferFromFile(path string) (*Buffer, error) {
	file, err := openRegularBufferFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxBufferFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxBufferFileBytes {
		return nil, ErrBufferFileTooLarge
	}
	// io.ReadAll returned this allocation exclusively for this buffer. Transfer
	// it into the immutable rope instead of immediately making a second copy.
	r := NewOwned(data)
	return &Buffer{
		rope:       r,
		Selections: NewSelections(Position{}),
		undo:       NewUndoStack(),
		FilePath:   path,
		savedRope:  r,
	}, nil
}

// LoadContent replaces the buffer contents with data, resetting cursor and undo.
// Used for async file loading into a placeholder buffer.
func (b *Buffer) LoadContent(data []byte) {
	b.LoadContentWithTabSize(data, 4)
}

// LoadContentWithTabSize replaces the buffer contents without changing bytes.
//
// The tabSize argument is retained for compatibility with existing callers. Tab
// expansion is a presentation concern owned by the editor; changing it here
// would silently corrupt a document on its next save.
func (b *Buffer) LoadContentWithTabSize(data []byte, tabSize int) {
	_ = tabSize
	b.LoadRopeSnapshot(New(data))
}

// LoadRopeSnapshot installs a document prepared by a background loader without
// flattening or copying it on the UI goroutine. A load establishes a new clean
// save baseline and clears edit, selection, and undo state.
func (b *Buffer) LoadRopeSnapshot(rope *Rope) {
	if rope == nil {
		rope = NewFromString("")
	}
	b.rope = rope
	b.savedRope = rope
	if b.Selections == nil {
		b.Selections = NewSelections(Position{})
	} else {
		b.Selections.selections = []Selection{{Anchor: Position{}, Head: Position{}}}
		b.Selections.primary = 0
		b.Selections.dirty = false
	}
	b.Cursor = Position{}
	b.undo = NewUndoStack()
	b.dirty = false
	b.version++
	b.lastChange = nil
}

// Rope returns the underlying rope.
func (b *Buffer) Rope() *Rope {
	return b.rope
}

// SavedRope returns the immutable snapshot that the buffer currently believes
// is persisted at FilePath. Save preparation uses it as an O(1) baseline for a
// bounded off-UI disk preflight; callers must not mutate Rope values.
func (b *Buffer) SavedRope() *Rope {
	return b.savedRope
}

// ReplaceRopeSnapshot applies a previously prepared immutable document in
// constant time. It is intentionally a full-sync edit: callers use it for
// multi-edit/background transformations where a single incremental LSP range
// would be misleading. The former rope remains an undo point. Cursor must be
// a valid byte-boundary position produced for the supplied rope; validating an
// arbitrary cursor here could require scanning a pathological giant line on
// the Bubble Tea update goroutine.
func (b *Buffer) ReplaceRopeSnapshot(rope *Rope, cursor Position) {
	if rope == nil || rope == b.rope {
		return
	}
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = rope
	b.Cursor = cursor
	b.Selections = NewSelections(cursor)
	b.dirty = b.rope != b.savedRope
	b.version++
	b.lastChange = nil
}

// RestoreSelections installs a bounded selection snapshot without changing
// document history or version. It is paired with ReplaceRopeSnapshot by
// background multi-cursor transformations such as a large paste.
func (b *Buffer) RestoreSelections(selections []Selection, primary int) {
	if len(selections) == 0 {
		b.Selections = NewSelections(b.Cursor)
		return
	}
	if len(selections) > MaxSelections {
		selections = selections[:MaxSelections]
		if primary >= len(selections) {
			primary = 0
		}
	}
	cloned := append([]Selection(nil), selections...)
	if primary < 0 || primary >= len(cloned) {
		primary = 0
	}
	// Snapshots may come from a canceled background operation. Restore the
	// same sorted, non-overlapping invariant as interactive cursor commands so
	// consumers such as the viewport can safely sweep selections by line.
	b.Selections = &Selections{selections: cloned, primary: primary, dirty: true}
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
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
	if len(text) == 0 {
		return
	}

	// If single selection with content, use existing logic
	if b.Selections.Count() == 1 {
		sel := b.Selections.Primary()
		if !sel.IsEmpty() {
			// Selection replace
			start, end := sel.Ordered()
			b.DeleteSelection()
			b.undo.Save(b.rope, b.Cursor, false)
			offset := b.rope.PositionToOffset(b.Cursor)
			b.rope = b.rope.Insert(offset, text)
			b.dirty = true
			b.version++
			b.Cursor = b.rope.OffsetToPosition(offset + len(text))
			b.lastChange = &EditChange{
				StartLine: start.Line, StartCol: start.Col,
				EndLine: end.Line, EndCol: end.Col,
				Text: string(text),
			}
			return
		}

		b.undo.Save(b.rope, b.Cursor, false)
		offset := b.rope.PositionToOffset(b.Cursor)
		b.rope = b.rope.Insert(offset, text)
		b.dirty = true
		b.version++
		b.Cursor = b.rope.OffsetToPosition(offset + len(text))
		b.Selections.selections[0] = Selection{Anchor: b.Cursor, Head: b.Cursor}
		b.lastChange = &EditChange{
			StartLine: sel.Head.Line, StartCol: sel.Head.Col,
			EndLine: sel.Head.Line, EndCol: sel.Head.Col,
			Text: string(text),
		}
		return
	}

	// Multiple selections replace every selected range, or insert at each
	// collapsed cursor. Normalize first so no two edits compete for the same
	// span; ranges are half-open and adjacent selections intentionally remain
	// independent edits.
	b.Selections.Normalize()
	type replacement struct {
		start int
		end   int
	}
	replacements := make([]replacement, len(b.Selections.selections))
	for i, sel := range b.Selections.selections {
		start, end := sel.Ordered()
		replacements[i] = replacement{
			start: b.rope.PositionToOffset(start),
			end:   b.rope.PositionToOffset(end),
		}
	}
	primary := b.Selections.primary

	b.undo.Save(b.rope, b.Cursor, false)

	// Apply from end to beginning so every offset remains relative to the
	// original rope. A replacement is delete+insert at its start offset.
	for i := len(replacements) - 1; i >= 0; i-- {
		replacement := replacements[i]
		if replacement.end > replacement.start {
			b.rope = b.rope.Delete(replacement.start, replacement.end-replacement.start)
		}
		b.rope = b.rope.Insert(replacement.start, text)
	}

	// Rebase each collapsed cursor into the final document. Earlier edits shift
	// every following replacement by their net byte delta.
	collapsed := make([]Selection, len(replacements))
	shift := 0
	for i, replacement := range replacements {
		newOffset := replacement.start + shift + len(text)
		pos := b.rope.OffsetToPosition(newOffset)
		collapsed[i] = Selection{Anchor: pos, Head: pos}
		shift += len(text) - (replacement.end - replacement.start)
	}
	b.Selections = &Selections{selections: collapsed, primary: primary}
	b.Cursor = b.Selections.PrimaryCursor()

	b.dirty = true
	b.version++

	// Multi-cursor edits require a full-sync fallback for LSP.
	b.lastChange = nil
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
	b.lastChange = &EditChange{
		StartLine: b.Cursor.Line, StartCol: 0,
		EndLine: b.Cursor.Line, EndCol: n,
		Text: "",
	}
}

// Backspace deletes the character before the cursor.
func (b *Buffer) Backspace() {
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
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
	endPos := b.Cursor
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = b.rope.Delete(offset-delLen, delLen)
	b.dirty = true
	b.version++
	b.Cursor = b.rope.OffsetToPosition(offset - delLen)
	b.lastChange = &EditChange{
		StartLine: b.Cursor.Line, StartCol: b.Cursor.Col,
		EndLine: endPos.Line, EndCol: endPos.Col,
		Text: "",
	}
}

// Delete deletes the character at the cursor.
func (b *Buffer) Delete() {
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
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
	startPos := b.Cursor
	endPos := b.rope.OffsetToPosition(offset + delLen)
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = b.rope.Delete(offset, delLen)
	b.dirty = true
	b.version++
	b.lastChange = &EditChange{
		StartLine: startPos.Line, StartCol: startPos.Col,
		EndLine: endPos.Line, EndCol: endPos.Col,
		Text: "",
	}
}

// DeleteSelection removes all active selections.
func (b *Buffer) DeleteSelection() {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}

	// Single selection: use optimized path
	if b.Selections.Count() == 1 {
		sel := b.Selections.Primary()
		if sel.IsEmpty() {
			return
		}
		start, end := sel.Ordered()
		startOff := b.rope.PositionToOffset(start)
		endOff := b.rope.PositionToOffset(end)
		n := endOff - startOff
		if n <= 0 {
			b.Selections.Clear()
			return
		}
		b.undo.Save(b.rope, b.Cursor, false)
		b.rope = b.rope.Delete(startOff, n)
		b.dirty = true
		b.version++
		b.Cursor = start
		b.Selections.Clear()
		// Collapse the remaining selection to cursor
		b.Selections.selections[0] = Selection{Anchor: b.Cursor, Head: b.Cursor}
		b.lastChange = &EditChange{
			StartLine: start.Line, StartCol: start.Col,
			EndLine: end.Line, EndCol: end.Col,
			Text: "",
		}
		return
	}

	// Multiple selections: delete all
	originalSelections := make([]Selection, len(b.Selections.selections))
	copy(originalSelections, b.Selections.selections)
	primarySelection := originalSelections[b.Selections.primary]
	primaryStart, _ := primarySelection.Ordered()
	primaryStartOff := b.rope.PositionToOffset(primaryStart)

	b.undo.Save(b.rope, b.Cursor, false)

	type selectionRange struct {
		start int
		end   int
	}
	ranges := make([]selectionRange, 0, len(originalSelections))
	for _, sel := range originalSelections {
		if sel.IsEmpty() {
			continue
		}
		start, end := sel.Ordered()
		startOff := b.rope.PositionToOffset(start)
		endOff := b.rope.PositionToOffset(end)
		if endOff > startOff {
			ranges = append(ranges, selectionRange{start: startOff, end: endOff})
		}
	}
	if len(ranges) == 0 {
		b.Selections = NewSelections(primaryStart)
		b.Cursor = primaryStart
		return
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start != ranges[j].start {
			return ranges[i].start > ranges[j].start
		}
		return ranges[i].end > ranges[j].end
	})

	// Delete from end to beginning
	deletedBeforePrimary := 0
	for _, r := range ranges {
		if r.end <= primaryStartOff {
			deletedBeforePrimary += r.end - r.start
		}
		b.rope = b.rope.Delete(r.start, r.end-r.start)
	}

	b.dirty = true
	b.version++

	newPrimaryOff := primaryStartOff - deletedBeforePrimary
	if newPrimaryOff < 0 {
		newPrimaryOff = 0
	}
	newPrimary := b.rope.OffsetToPosition(newPrimaryOff)
	b.Cursor = newPrimary
	b.Selections = NewSelections(newPrimary)
	b.lastChange = nil
}

// SetCursor sets the cursor position and updates the primary selection.
func (b *Buffer) SetCursor(pos Position) {
	b.Cursor = pos
	if b.Selections != nil {
		b.Selections.selections[b.Selections.primary] = Selection{Anchor: pos, Head: pos}
	}
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
	b.lastChange = &EditChange{
		StartLine: start.Line, StartCol: start.Col,
		EndLine: end.Line, EndCol: end.Col,
		Text: string(newText),
	}
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
	if b.Selections == nil {
		b.Selections = NewSelections(anchor)
	} else {
		b.Selections.selections[b.Selections.primary] = Selection{Anchor: anchor, Head: head}
		b.Selections.Clear() // Ensure only one selection
	}
	b.Cursor = head
}

// ClearSelection clears any active selection.
func (b *Buffer) ClearSelection() {
	b.SetSelection(b.Cursor, b.Cursor)
}

// CursorToLineStart moves the cursor to the beginning of the current line.
func (b *Buffer) CursorToLineStart() {
	b.Cursor.Col = 0
}

// CursorToLineEnd moves the cursor to the end of the current line.
func (b *Buffer) CursorToLineEnd() {
	b.Cursor.Col = b.rope.LineLen(b.Cursor.Line)
}

// MoveCursors moves all cursors in the given direction.
func (b *Buffer) MoveCursors(dir Direction) {
	for i := range b.Selections.selections {
		sel := &b.Selections.selections[i]
		oldHead := sel.Head

		switch dir {
		case DirLeft:
			if sel.Head.Col > 0 {
				lineContent := b.rope.Line(sel.Head.Line)
				_, size := utf8.DecodeLastRune(lineContent[:sel.Head.Col])
				sel.Head.Col -= size
			} else if sel.Head.Line > 0 {
				sel.Head.Line--
				sel.Head.Col = b.rope.LineLen(sel.Head.Line)
			}
		case DirRight:
			lineLen := b.rope.LineLen(sel.Head.Line)
			if sel.Head.Col < lineLen {
				lineContent := b.rope.Line(sel.Head.Line)
				_, size := utf8.DecodeRune(lineContent[sel.Head.Col:])
				sel.Head.Col += size
			} else if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head.Line++
				sel.Head.Col = 0
			}
		case DirUp:
			if sel.Head.Line > 0 {
				sel.Head.Line--
				sel.Head.Col = min(sel.Head.Col, b.rope.LineLen(sel.Head.Line))
			}
		case DirDown:
			if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head.Line++
				sel.Head.Col = min(sel.Head.Col, b.rope.LineLen(sel.Head.Line))
			}
		}

		// Update anchor if not extending selection
		if sel.Anchor == oldHead {
			sel.Anchor = sel.Head
		}
	}

	b.Selections.Normalize()
	// Update b.Cursor to match primary
	b.Cursor = b.Selections.PrimaryCursor()
}

// ExtendCursors extends all selections in the given direction.
func (b *Buffer) ExtendCursors(dir Direction) {
	for i := range b.Selections.selections {
		sel := &b.Selections.selections[i]

		switch dir {
		case DirLeft:
			if sel.Head.Col > 0 {
				lineContent := b.rope.Line(sel.Head.Line)
				_, size := utf8.DecodeLastRune(lineContent[:sel.Head.Col])
				sel.Head.Col -= size
			} else if sel.Head.Line > 0 {
				sel.Head.Line--
				sel.Head.Col = b.rope.LineLen(sel.Head.Line)
			}
		case DirRight:
			lineLen := b.rope.LineLen(sel.Head.Line)
			if sel.Head.Col < lineLen {
				lineContent := b.rope.Line(sel.Head.Line)
				_, size := utf8.DecodeRune(lineContent[sel.Head.Col:])
				sel.Head.Col += size
			} else if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head.Line++
				sel.Head.Col = 0
			}
		case DirUp:
			if sel.Head.Line > 0 {
				sel.Head.Line--
				sel.Head.Col = min(sel.Head.Col, b.rope.LineLen(sel.Head.Line))
			}
		case DirDown:
			if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head.Line++
				sel.Head.Col = min(sel.Head.Col, b.rope.LineLen(sel.Head.Line))
			}
		}
		// Don't update anchor - we're extending
	}

	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
}

// Save writes the buffer to its FilePath.
func (b *Buffer) Save() error {
	if b.FilePath == "" {
		return nil
	}
	return b.SaveAs(b.FilePath)
}

// SaveAs writes the buffer to the given path atomically.
// It writes to a temporary file first, then renames it to the target path.
func (b *Buffer) SaveAs(path string) error {
	snapshot := b.rope
	if err := WriteRopeAtomically(path, snapshot); err != nil {
		return err
	}
	b.MarkSavedSnapshot(path, snapshot)
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
	// Undo snapshots intentionally contain text and the active cursor, not a
	// stale set of multicursors. Collapse to the restored cursor so selection
	// coordinates cannot refer to the document that was just undone.
	b.Selections = NewSelections(b.Cursor)
	b.dirty = b.rope != b.savedRope
	b.version++
	b.lastChange = nil // undo: fall back to full sync
}

// Redo redoes the last undone edit.
func (b *Buffer) Redo() {
	rope, cursor, ok := b.undo.Redo(b.rope, b.Cursor)
	if !ok {
		return
	}
	b.rope = rope
	b.Cursor = cursor
	// See Undo: selection snapshots are not part of UndoStack.
	b.Selections = NewSelections(b.Cursor)
	b.dirty = b.rope != b.savedRope
	b.version++
	b.lastChange = nil // redo: fall back to full sync
}

// Content returns the full buffer content as a string.
func (b *Buffer) Content() string {
	return b.rope.String()
}

// Version returns a monotonically increasing version number, incremented on each edit.
func (b *Buffer) Version() int {
	return b.version
}

// LastChange returns the incremental change from the last edit, or nil
// if the change is unknown (e.g. undo/redo or multi-line indent).
func (b *Buffer) LastChange() *EditChange {
	return b.lastChange
}

// Bytes returns the full buffer content as a byte slice.
func (b *Buffer) Bytes() []byte {
	return b.rope.Bytes()
}

// SelectedText returns the currently selected text from the primary selection, or empty if no selection.
func (b *Buffer) SelectedText() []byte {
	if b.Selections == nil || b.Selections.Count() == 0 || b.Selections.Primary().IsEmpty() {
		return nil
	}
	start, end := b.Selections.Primary().Ordered()
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
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
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
	b.lastChange = nil // word-wise delete may span mixed token classes; use full-sync fallback
}

// DeleteWord deletes from the cursor to the start of the next word.
func (b *Buffer) DeleteWord() {
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
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
	b.lastChange = &EditChange{
		StartLine: saved.Line, StartCol: saved.Col,
		EndLine: endPos.Line, EndCol: endPos.Col,
		Text: "",
	}
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
	if b.Selections != nil && b.Selections.Count() > 0 {
		anchor = b.Selections.Primary().Anchor
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

// SelectNextOccurrence selects the next occurrence of the current selection,
// or selects the word at the cursor. It returns false when the document exceeds
// the bounded synchronous scan budget.
func (b *Buffer) SelectNextOccurrence() bool {
	if b.Selections == nil || b.Selections.Count() == 0 || b.Selections.Primary().IsEmpty() {
		b.SelectWordAtCursor()
		return true
	}
	if b.rope.Len() > MaxOccurrenceSearchBytes {
		return false
	}
	sel := b.SelectedText()
	if len(sel) == 0 {
		return true
	}
	content := b.rope.Bytes()
	_, end := b.Selections.Primary().Ordered()
	endOff := b.rope.PositionToOffset(end)

	// Search forward from end of selection
	idx := bytes.Index(content[endOff:], sel)
	if idx >= 0 {
		matchOff := endOff + idx
		matchEnd := matchOff + len(sel)
		newSel := Selection{
			Anchor: b.rope.OffsetToPosition(matchOff),
			Head:   b.rope.OffsetToPosition(matchEnd),
		}
		b.Selections.Add(newSel)
		b.Selections.Normalize()
		b.Cursor = b.Selections.PrimaryCursor()
		return true
	}
	// Wrap around
	idx = bytes.Index(content[:endOff], sel)
	if idx >= 0 {
		matchEnd := idx + len(sel)
		newSel := Selection{
			Anchor: b.rope.OffsetToPosition(idx),
			Head:   b.rope.OffsetToPosition(matchEnd),
		}
		b.Selections.Add(newSel)
		b.Selections.Normalize()
		b.Cursor = b.Selections.PrimaryCursor()
	}
	return true
}

// SelectAllOccurrences selects all occurrences of the current primary
// selection. It returns false when the document exceeds the bounded
// synchronous scan budget.
func (b *Buffer) SelectAllOccurrences() bool {
	if b.Selections == nil || b.Selections.Count() == 0 || b.Selections.Primary().IsEmpty() {
		b.SelectWordAtCursor()
	}
	if b.rope.Len() > MaxOccurrenceSearchBytes {
		return false
	}

	primary := b.Selections.Primary()
	start, end := primary.Ordered()
	startOff := b.rope.PositionToOffset(start)
	endOff := b.rope.PositionToOffset(end)
	selectedText := b.rope.Slice(startOff, endOff).Bytes()

	if len(selectedText) == 0 {
		return true
	}

	content := b.rope.Bytes()
	// Clear existing selections except primary
	b.Selections.Clear()

	// Find all occurrences
	idx := 0
	for {
		pos := bytes.Index(content[idx:], selectedText)
		if pos < 0 {
			break
		}
		matchOff := idx + pos
		matchEnd := matchOff + len(selectedText)
		newSel := Selection{
			Anchor: b.rope.OffsetToPosition(matchOff),
			Head:   b.rope.OffsetToPosition(matchEnd),
		}
		b.Selections.Add(newSel)
		idx = matchEnd
	}

	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
	return true
}

// AddCursorAbove adds a cursor on the line above each selection.
func (b *Buffer) AddCursorAbove() {
	if b.Selections == nil {
		return
	}
	selections := b.Selections.All()
	for _, sel := range selections {
		if sel.Head.Line > 0 {
			newPos := Position{
				Line: sel.Head.Line - 1,
				Col:  min(sel.Head.Col, b.rope.LineLen(sel.Head.Line-1)),
			}
			b.Selections.Add(Selection{Anchor: newPos, Head: newPos})
		}
	}
	b.Selections.Normalize()
}

// AddCursorBelow adds a cursor on the line below each selection.
func (b *Buffer) AddCursorBelow() {
	if b.Selections == nil {
		return
	}
	selections := b.Selections.All()
	for i := len(selections) - 1; i >= 0; i-- {
		sel := selections[i]
		if sel.Head.Line < b.rope.LineCount()-1 {
			newPos := Position{
				Line: sel.Head.Line + 1,
				Col:  min(sel.Head.Col, b.rope.LineLen(sel.Head.Line+1)),
			}
			b.Selections.Add(Selection{Anchor: newPos, Head: newPos})
		}
	}
	b.Selections.Normalize()
}

// SplitSelectionIntoLines splits the current selection into multiple selections,
// one per line covered by the selection.
func (b *Buffer) SplitSelectionIntoLines() {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}

	primary := b.Selections.Primary()
	if primary.IsEmpty() {
		return
	}

	start, end := primary.Ordered()
	firstLine := start.Line
	lastLine := end.Line
	if lastLine-firstLine+1 > MaxSelections {
		// Keep the portion nearest the active head. This honours the hard
		// multicursor limit without silently discarding the user's focused end
		// of a very large selection.
		if primary.Head.Line <= firstLine {
			lastLine = firstLine + MaxSelections - 1
		} else {
			firstLine = lastLine - MaxSelections + 1
		}
	}

	// Build a new selection set rather than Clear(): Clear intentionally retains
	// the previous primary selection, which would otherwise leave the original
	// cross-line selection alongside its split children.
	selections := make([]Selection, 0, lastLine-firstLine+1)
	primaryIndex := -1

	// Add one selection per line. Empty intermediate lines receive a collapsed
	// cursor, which lets a subsequent multi-cursor edit affect every covered
	// line. A terminal line reached only at column zero is outside the half-open
	// source selection and is deliberately omitted.
	for line := firstLine; line <= lastLine; line++ {
		lineLen := b.rope.LineLen(line)

		// Determine column range for this line
		colStart := 0
		colEnd := lineLen

		if line == start.Line {
			colStart = start.Col
		}
		if line == end.Line {
			colEnd = end.Col
		}

		include := colStart < colEnd || (line > start.Line && line < end.Line && lineLen == 0)
		if include {
			selection := Selection{
				Anchor: Position{Line: line, Col: colStart},
				Head:   Position{Line: line, Col: colEnd},
			}
			selections = append(selections, selection)

			// The original head determines the active (primary) child. If the
			// head lies on an excluded half-open endpoint, use the closest child
			// below as a sensible visible cursor.
			if line == primary.Head.Line && primary.Head.Col >= colStart && primary.Head.Col <= colEnd {
				primaryIndex = len(selections) - 1
			}
		}
	}

	if len(selections) == 0 {
		return
	}
	if primaryIndex < 0 {
		if primary.Head.Line <= firstLine {
			primaryIndex = 0
		} else {
			primaryIndex = len(selections) - 1
		}
	}
	b.Selections = &Selections{selections: selections, primary: primaryIndex}
	b.Cursor = b.Selections.PrimaryCursor()
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
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
		s, e := b.Selections.Primary().Ordered()
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
	b.lastChange = nil // multi-line structural edit; use full-sync fallback
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
	b.lastChange = nil // line swap is non-local for incremental sync
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
	b.lastChange = nil // line swap is non-local for incremental sync
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
	b.lastChange = nil // line duplicate is non-local for incremental sync
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
	b.lastChange = nil // line duplicate is non-local for incremental sync
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
	b.lastChange = nil // complex operation, fall back to full sync
}

// IndentLines indents the current line or all lines in selection.
func (b *Buffer) IndentLines(tabSize int) {
	startLine := b.Cursor.Line
	endLine := b.Cursor.Line
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
		s, e := b.Selections.Primary().Ordered()
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
	b.lastChange = nil // multi-line indent: fall back to full sync
}

// DedentLines removes one level of indentation from the current line or selection.
func (b *Buffer) DedentLines(tabSize int) {
	startLine := b.Cursor.Line
	endLine := b.Cursor.Line
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
		s, e := b.Selections.Primary().Ordered()
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
	b.lastChange = nil // multi-line dedent: fall back to full sync
}
