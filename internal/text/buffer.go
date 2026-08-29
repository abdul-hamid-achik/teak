package text

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"unicode"
	"unicode/utf8"
)

// MaxBufferFileBytes bounds the synchronous Buffer constructor as well as the
// application loader. A Buffer represents an in-memory editable document, not
// an unbounded stream.
const MaxBufferFileBytes int64 = 64 << 20

// maxInteractiveAutoIndentBytes bounds the aggregate indentation inspected and
// copied by one Enter keypress. The budget is divided across active selections
// so a thousand cursors or one pathological whitespace-only line cannot make
// Update perform document-sized work. An indentation exceeding its fair share
// falls back to a plain newline for that selection.
const maxInteractiveAutoIndentBytes = 64 << 10

// MaxStructuralPrefixBytes bounds synchronous leading-whitespace inspection
// for structural commands such as ToggleLineComment. The editor additionally
// caps the number of targeted lines; together the limits keep Update work
// independent of pathological logical-line length and total document size.
const MaxStructuralPrefixBytes = 64 << 10

// StructuralEditResult describes whether a synchronous structural command
// changed text, was a valid no-op, or refused work above its prefix budget.
type StructuralEditResult uint8

const (
	StructuralEditNoChange StructuralEditResult = iota
	StructuralEditApplied
	StructuralEditLimit
)

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
	lineEnding LineEnding  // newline convention of the loaded file; content is always LF
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
	// Normalize CRLF to LF so edits cannot orphan CR bytes mid-line; the
	// original convention is remembered and restored on save.
	data, ending := NormalizeLineEndings(data)
	// io.ReadAll returned this allocation exclusively for this buffer. Transfer
	// it into the immutable rope instead of immediately making a second copy.
	r := NewOwned(data)
	return &Buffer{
		rope:       r,
		Selections: NewSelections(Position{}),
		undo:       NewUndoStack(),
		FilePath:   path,
		savedRope:  r,
		lineEnding: ending,
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
	normalized, ending := NormalizeLineEndings(data)
	b.lineEnding = ending
	b.LoadRopeSnapshot(New(normalized), ending)
}

// LineEnding reports the newline convention the buffer was loaded with. Saves
// restore it; buffer content itself is always LF-terminated.
func (b *Buffer) LineEnding() LineEnding {
	return b.lineEnding
}

// LoadRopeSnapshot installs a document prepared by a background loader without
// flattening or copying it on the UI goroutine. A load establishes a new clean
// save baseline and clears edit, selection, and undo state. The line ending is
// supplied by the loader, which saw the raw file bytes; the snapshot itself is
// already LF-normalized.
func (b *Buffer) LoadRopeSnapshot(rope *Rope, ending LineEnding) {
	if rope == nil {
		rope = NewFromString("")
	}
	b.lineEnding = ending
	b.rope = rope
	b.savedRope = rope
	if b.Selections == nil {
		b.Selections = NewSelections(Position{})
	} else {
		b.Selections.selections = []Selection{{Anchor: Position{}, Head: Position{}}}
		b.Selections.primary = 0
		b.Selections.dirty = false
	}
	b.SetCursor(Position{})
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
	b.Selections = NewSelections(cursor)
	b.SetCursor(cursor)
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

// MarkDirty flags the buffer as having unsaved changes without editing it.
// Crash recovery uses it to restore buffers that were dirty when the previous
// session died: their content matches the recovery record, not the disk.
func (b *Buffer) MarkDirty() {
	b.dirty = true
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
// isTypedRune reports whether text is a single character that should merge into
// the current undo group. A newline ends the group: Enter is a natural boundary
// a user expects Ctrl+Z to stop at, and treating it as ordinary typing would let
// one undo swallow several lines.
func isTypedRune(text []byte) bool {
	if len(text) == 0 || bytes.ContainsRune(text, '\n') {
		return false
	}
	r, size := utf8.DecodeRune(text)
	return r != utf8.RuneError && size == len(text)
}

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
			startOff := b.rope.PositionToOffset(start)
			endOff := b.rope.PositionToOffset(end)
			b.undo.Save(b.rope, b.Cursor, false)
			b.rope = b.rope.Delete(startOff, endOff-startOff)
			b.rope = b.rope.Insert(startOff, text)
			b.dirty = true
			b.version++
			// Replacing a selection collapses it, so the primary selection must
			// follow the cursor. Leaving it behind made the next keystroke
			// record its position from a stale selection.
			b.SetCursor(b.rope.OffsetToPosition(startOff + len(text)))
			b.lastChange = &EditChange{
				StartLine: start.Line, StartCol: start.Col,
				EndLine: end.Line, EndCol: end.Col,
				Text: string(text),
			}
			return
		}

		// A plain single-character insert at a collapsed cursor is ordinary
		// typing, so it joins the previous keystroke's undo group. Every call
		// site used to pass false, making the grouping in UndoStack.Save dead
		// code: Ctrl+Z then undid one character at a time and the undo stack
		// filled with one snapshot per keystroke.
		b.undo.Save(b.rope, b.Cursor, isTypedRune(text))
		// Record where the edit actually lands, which is the cursor. Deriving
		// this from the primary selection meant that any path moving the cursor
		// without collapsing the selection — a bracket skip-over, a find jump,
		// opening a file at a line — produced a change record pointing at a
		// different position. That record is sent verbatim to the language
		// server as an incremental edit, so its copy of the document silently
		// and permanently diverged from the buffer.
		origin := b.Cursor
		offset := b.rope.PositionToOffset(b.Cursor)
		b.rope = b.rope.Insert(offset, text)
		b.dirty = true
		b.version++
		b.SetCursor(b.rope.OffsetToPosition(offset + len(text)))
		b.undo.MarkCharInsertEnd(b.Cursor)
		b.lastChange = &EditChange{
			StartLine: origin.Line, StartCol: origin.Col,
			EndLine: origin.Line, EndCol: origin.Col,
			Text: string(text),
		}
		return
	}

	// Multiple selections replace every selected range, or insert at each
	// collapsed cursor. Normalize first so no two edits compete for the same
	// span; ranges are half-open and adjacent selections intentionally remain
	// independent edits.
	b.Selections.Normalize()
	edits := make([]EditOp, len(b.Selections.selections))
	for i, sel := range b.Selections.selections {
		start, end := sel.Ordered()
		startOffset := b.rope.PositionToOffset(start)
		endOffset := b.rope.PositionToOffset(end)
		edits[i] = EditOp{
			Offset: startOffset,
			Delete: endOffset - startOffset,
			Insert: text,
			Cursor: startOffset + len(text),
		}
	}
	b.ApplySelectionEdits(edits)
}

// ApplySelectionEdits applies one ordered, non-overlapping replacement per
// normalized selection. Every operation addresses the original immutable rope
// and supplies its cursor in the document after that replacement alone. The
// method validates the complete set before saving Undo or changing state,
// applies text from end to start, then rebases every cursor through earlier
// replacements. A cursor-only set moves selections without dirtying the
// document; multiple text edits use the LSP full-sync fallback.
func (b *Buffer) ApplySelectionEdits(ops []EditOp) bool {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return false
	}
	b.Selections.Normalize()
	if len(ops) != b.Selections.Count() {
		return false
	}

	const maxInt = int(^uint(0) >> 1)
	docLen := b.rope.Len()
	finalLen := docLen
	previousEnd := 0
	changed := false
	for i, op := range ops {
		if op.Offset < 0 || op.Offset > docLen || op.Delete < 0 || op.Delete > docLen-op.Offset {
			return false
		}
		end := op.Offset + op.Delete
		if i > 0 && op.Offset < previousEnd {
			return false
		}
		previousEnd = end
		if len(op.Insert) > maxInt-(finalLen-op.Delete) {
			return false
		}
		finalLen += len(op.Insert) - op.Delete
		if op.Cursor < 0 || op.Cursor > docLen-op.Delete+len(op.Insert) {
			return false
		}
		if op.Delete > 0 || len(op.Insert) > 0 {
			if op.Cursor < op.Offset || op.Cursor > op.Offset+len(op.Insert) {
				return false
			}
			changed = true
		}
	}

	oldRope := b.rope
	primary := b.Selections.PrimaryIndex()
	b.Cursor = b.Selections.PrimaryCursor()
	if changed {
		isTypedInsert := len(ops) == 1 && ops[0].Delete == 0 &&
			isTypedRune(ops[0].Insert) && ops[0].Cursor == ops[0].Offset+len(ops[0].Insert)
		b.undo.Save(b.rope, b.Cursor, isTypedInsert)
		for i := len(ops) - 1; i >= 0; i-- {
			op := ops[i]
			if op.Delete > 0 {
				b.rope = b.rope.Delete(op.Offset, op.Delete)
			}
			if len(op.Insert) > 0 {
				b.rope = b.rope.Insert(op.Offset, op.Insert)
			}
		}
	}

	collapsed := make([]Selection, len(ops))
	shift := 0
	for i, op := range ops {
		target := op.Cursor + shift
		target = max(0, min(target, finalLen))
		pos := b.rope.OffsetToPosition(target)
		collapsed[i] = Selection{Anchor: pos, Head: pos}
		shift += len(op.Insert) - op.Delete
	}
	b.Selections = &Selections{selections: collapsed, primary: primary, dirty: true}
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()

	if !changed {
		return false
	}
	b.undo.MarkCharInsertEnd(b.Cursor)
	b.dirty = true
	b.version++
	if len(ops) == 1 {
		op := ops[0]
		start := oldRope.OffsetToPosition(op.Offset)
		end := oldRope.OffsetToPosition(op.Offset + op.Delete)
		b.lastChange = &EditChange{
			StartLine: start.Line, StartCol: start.Col,
			EndLine: end.Line, EndCol: end.Col,
			Text: string(op.Insert),
		}
	} else {
		b.lastChange = nil
	}
	return true
}

// InsertNewline inserts a newline at the cursor.
func (b *Buffer) InsertNewline() {
	b.InsertAtCursor([]byte{'\n'})
}

// InsertNewlineWithIndent inserts a newline at every selection and copies the
// leading whitespace of each insertion line. Prefix reads share a fixed budget
// and the resulting replacements are committed as one undoable transaction.
func (b *Buffer) InsertNewlineWithIndent() {
	if b.Selections == nil || b.Selections.Count() == 0 {
		b.Selections = NewSelections(b.ClampPosition(b.Cursor))
	}
	b.Selections.Normalize()
	selectionCount := b.Selections.Count()
	perSelectionLimit := maxInteractiveAutoIndentBytes / selectionCount
	scratch := make([]byte, perSelectionLimit+1)
	edits := make([]EditOp, selectionCount)
	for i, selection := range b.Selections.All() {
		start, end := selection.Ordered()
		startOffset, startOK := b.rope.PositionToOffsetUncached(start)
		endOffset, endOK := b.rope.PositionToOffsetUncached(end)
		if !startOK || !endOK || endOffset < startOffset {
			return
		}

		indentLen := b.leadingWhitespaceLen(start.Line, perSelectionLimit, scratch)
		insert := []byte{'\n'}
		if indentLen > 0 {
			insert = make([]byte, indentLen+1)
			insert[0] = '\n'
			copy(insert[1:], scratch[:indentLen])
		}
		edits[i] = EditOp{
			Offset: startOffset,
			Delete: endOffset - startOffset,
			Insert: insert,
			Cursor: startOffset + len(insert),
		}
	}
	b.ApplySelectionEdits(edits)
}

// leadingWhitespaceLen copies at most limit+1 bytes from a logical line into
// scratch and returns its complete leading-whitespace length when it fits. A
// zero result means either no indentation or a prefix larger than the budget;
// callers deliberately treat both as a plain newline. Rope.ReadAt visits only
// intersecting leaves and avoids materializing the containing line.
func (b *Buffer) leadingWhitespaceLen(line, limit int, scratch []byte) int {
	if limit <= 0 || len(scratch) < limit+1 {
		return 0
	}
	lineStart := b.rope.LineStart(line)
	n, _ := b.rope.ReadAt(scratch[:limit+1], int64(lineStart))
	indentLen := 0
	for indentLen < n && (scratch[indentLen] == ' ' || scratch[indentLen] == '\t') {
		indentLen++
	}
	if indentLen > limit {
		return 0
	}
	return indentLen
}

// DedentLine removes one leading tab or up to tabSize leading spaces from the
// current line, adjusting the cursor.
func (b *Buffer) DedentLine(tabSize int) StructuralEditResult {
	line := b.Cursor.Line
	b.SetCursor(b.ClampPosition(b.Cursor))
	n := b.dedentBytesAtLine(line, tabSize)
	if n == 0 {
		return StructuralEditNoChange
	}
	return b.applyStructuralLineEdits([]structuralLineEdit{{line: line, delete: n}})
}

// Backspace deletes the character before the cursor.
func (b *Buffer) Backspace() {
	if b.Selections != nil && b.Selections.Count() > 0 && !b.Selections.Primary().IsEmpty() {
		b.DeleteSelection()
		return
	}
	if b.Selections != nil && b.Selections.Count() > 1 {
		if b.allSelectionsEmpty() {
			b.deleteAtCursors(true)
		} else {
			b.DeleteSelection()
		}
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
	// Backspace and Delete both branch on whether the primary selection is
	// empty, so leaving a stale one behind makes a later press delete a range
	// the user is no longer on.
	b.SetCursor(b.rope.OffsetToPosition(offset - delLen))
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
	if b.Selections != nil && b.Selections.Count() > 1 {
		if b.allSelectionsEmpty() {
			b.deleteAtCursors(false)
		} else {
			b.DeleteSelection()
		}
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
	// Delete leaves the cursor at the same byte offset. Reconcile the primary
	// selection nevertheless: callers may have moved the cursor through a
	// compatibility path that did not call SetCursor.
	b.SetCursor(b.Cursor)
	b.lastChange = &EditChange{
		StartLine: startPos.Line, StartCol: startPos.Col,
		EndLine: endPos.Line, EndCol: endPos.Col,
		Text: "",
	}
}

// allSelectionsEmpty reports whether every selection is a collapsed cursor.
func (b *Buffer) allSelectionsEmpty() bool {
	for _, sel := range b.Selections.selections {
		if !sel.IsEmpty() {
			return false
		}
	}
	return true
}

type cursorDeleteRange struct {
	start int
	end   int
}

// applyCursorDeletionRanges deletes one original-document range per selection
// and rebases every resulting cursor through the union of those ranges. Word
// operations can overlap when several cursors sit in the same token, so ranges
// are merged before mutating the immutable rope. The target for each cursor is
// its own range start; collapsed no-op ranges preserve cursors that coexist
// with selected text elsewhere in the document.
func (b *Buffer) applyCursorDeletionRanges(ranges []cursorDeleteRange) bool {
	if b.Selections == nil || len(ranges) != b.Selections.Count() {
		return false
	}
	docLen := b.rope.Len()
	changed := false
	for i := range ranges {
		ranges[i].start = max(0, min(ranges[i].start, docLen))
		ranges[i].end = max(ranges[i].start, min(ranges[i].end, docLen))
		changed = changed || ranges[i].end > ranges[i].start
	}
	if !changed {
		return false
	}

	unions := make([]cursorDeleteRange, 0, len(ranges))
	for _, r := range ranges {
		if r.end > r.start {
			unions = append(unions, r)
		}
	}
	sort.Slice(unions, func(i, j int) bool {
		if unions[i].start != unions[j].start {
			return unions[i].start < unions[j].start
		}
		return unions[i].end < unions[j].end
	})
	merged := unions[:0]
	for _, r := range unions {
		if len(merged) == 0 || r.start > merged[len(merged)-1].end {
			merged = append(merged, r)
			continue
		}
		merged[len(merged)-1].end = max(merged[len(merged)-1].end, r.end)
	}

	primary := b.Selections.PrimaryIndex()
	b.Cursor = b.Selections.PrimaryCursor()
	b.undo.Save(b.rope, b.Cursor, false)
	for i := len(merged) - 1; i >= 0; i-- {
		r := merged[i]
		b.rope = b.rope.Delete(r.start, r.end-r.start)
	}

	rebased := make([]Selection, len(ranges))
	for i, r := range ranges {
		target := r.start
		shift := 0
		mappedTarget := target
		mapped := false
		for _, deleted := range merged {
			switch {
			case target < deleted.start:
				mappedTarget = target - shift
				mapped = true
			case target <= deleted.end:
				mappedTarget = deleted.start - shift
				mapped = true
			default:
				shift += deleted.end - deleted.start
			}
			if mapped {
				break
			}
		}
		if !mapped {
			mappedTarget = target - shift
		}
		pos := b.rope.OffsetToPosition(mappedTarget)
		rebased[i] = Selection{Anchor: pos, Head: pos}
	}
	b.Selections = &Selections{selections: rebased, primary: primary, dirty: true}
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
	b.dirty = true
	b.version++
	b.lastChange = nil
	return true
}

// deleteAtCursors applies Backspace (backward=true) or Delete (backward=false)
// once at every collapsed cursor. Each deletion shifts every following cursor,
// so edits are applied end-to-start against the original rope and all
// selections are rebased into the final document; editing only the primary
// cursor used to strand the remaining selections at stale offsets, corrupting
// the next multi-cursor edit.
func (b *Buffer) deleteAtCursors(backward bool) {
	b.Selections.Normalize()
	ranges := make([]cursorDeleteRange, 0, len(b.Selections.selections))
	for _, sel := range b.Selections.selections {
		offset := b.rope.PositionToOffset(sel.Head)
		if backward {
			if offset == 0 {
				ranges = append(ranges, cursorDeleteRange{start: offset, end: offset})
				continue
			}
			delLen := 1
			if _, size, ok := b.rope.RuneBefore(offset); ok && size > 0 {
				delLen = size
			}
			ranges = append(ranges, cursorDeleteRange{start: offset - delLen, end: offset})
			continue
		}
		if offset >= b.rope.Len() {
			ranges = append(ranges, cursorDeleteRange{start: offset, end: offset})
			continue
		}
		delLen := 1
		if _, size, ok := b.rope.RuneAt(offset); ok && size > 0 {
			delLen = size
		}
		ranges = append(ranges, cursorDeleteRange{start: offset, end: offset + delLen})
	}
	b.applyCursorDeletionRanges(ranges)
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
		b.SetCursor(start)
		b.Selections.Clear()
		b.lastChange = &EditChange{
			StartLine: start.Line, StartCol: start.Col,
			EndLine: end.Line, EndCol: end.Col,
			Text: "",
		}
		return
	}

	// Multiple selections retain one rebased cursor per original selection,
	// including collapsed cursors which have no text to delete themselves.
	b.Selections.Normalize()
	ranges := make([]cursorDeleteRange, len(b.Selections.selections))
	for i, sel := range b.Selections.selections {
		start, end := sel.Ordered()
		ranges[i] = cursorDeleteRange{
			start: b.rope.PositionToOffset(start),
			end:   b.rope.PositionToOffset(end),
		}
	}
	b.applyCursorDeletionRanges(ranges)
}

// SetCursor sets the cursor position and updates the primary selection.
func (b *Buffer) SetCursor(pos Position) {
	b.Cursor = pos
	if b.Selections == nil {
		b.Selections = NewSelections(pos)
		return
	}
	if len(b.Selections.selections) == 0 {
		b.Selections = NewSelections(pos)
		return
	}
	if b.Selections.primary < 0 || b.Selections.primary >= len(b.Selections.selections) {
		b.Selections.primary = 0
	}
	b.Selections.selections[b.Selections.primary] = Selection{Anchor: pos, Head: pos}
}

// ClampPosition returns pos confined to the current content: a line inside the
// document and a column inside that line.
func (b *Buffer) ClampPosition(pos Position) Position {
	if pos.Line < 0 {
		pos.Line = 0
	}
	if last := b.LineCount() - 1; pos.Line > last {
		pos.Line = max(last, 0)
	}
	if pos.Col < 0 {
		pos.Col = 0
	}
	pos.Col = b.clampLineColumn(pos.Line, pos.Col)
	return pos
}

// clampLineColumn confines a byte column to its line and repairs positions
// inside a valid multi-byte rune. It probes at most utf8.UTFMax bytes directly
// from the rope, so cursor movement never materializes a potentially enormous
// logical line inside Update. A stray continuation byte is malformed input in
// its own right and remains a navigable one-byte position.
func (b *Buffer) clampLineColumn(line, col int) int {
	lineLen := b.rope.LineLen(line)
	col = max(0, min(col, lineLen))
	if col == 0 || col == lineLen {
		return col
	}

	lineStart := b.rope.LineStart(line)
	offset := lineStart + col
	if utf8.RuneStart(b.rope.ByteAt(offset)) {
		return col
	}

	start := offset
	for start > lineStart && offset-start < utf8.UTFMax && !utf8.RuneStart(b.rope.ByteAt(start)) {
		start--
	}
	_, size, ok := b.rope.RuneAt(start)
	if ok && size > 1 && start+size > offset {
		return start - lineStart
	}
	return col
}

// ClampCursor confines the cursor and every selection to the current content.
//
// Edits that rewrite the document wholesale — formatting, a rename, an applied
// code action — can leave the cursor addressing a line or column that no longer
// exists. The status bar then reports an impossible position and the next
// keystroke resolves the stale offset, teleporting the cursor. Call this after
// any such rewrite.
func (b *Buffer) ClampCursor() {
	if b.Selections == nil {
		// A compatibility caller may have cleared the public selection field
		// before asking us to repair a position. Rebuild the canonical single
		// selection as well; leaving it nil would make the next edit dereference
		// a missing selection set.
		b.SetCursor(b.ClampPosition(b.Cursor))
		return
	}
	b.Cursor = b.ClampPosition(b.Cursor)
	for i, sel := range b.Selections.selections {
		b.Selections.selections[i] = Selection{
			Anchor: b.ClampPosition(sel.Anchor),
			Head:   b.ClampPosition(sel.Head),
		}
	}
	// Cursor is the primary selection head. Preserve the anchor for a single
	// selection while repairing its head if a legacy caller assigned Cursor
	// directly; multi-cursor state keeps its own primary head as the projection.
	if b.Selections.Count() == 1 {
		b.Selections.selections[b.Selections.primary].Head = b.Cursor
	} else {
		b.Cursor = b.Selections.PrimaryCursor()
	}
}

// ReplaceRange replaces text between start and end positions with newText.
func (b *Buffer) ReplaceRange(start, end Position, newText []byte) {
	oldRope := b.rope
	startOff := b.rope.PositionToOffset(start)
	endOff := b.rope.PositionToOffset(end)
	n := endOff - startOff
	oldCursor := b.Cursor
	oldSelections := []Selection(nil)
	primary := 0
	if b.Selections != nil {
		oldSelections = append(oldSelections, b.Selections.selections...)
		primary = b.Selections.PrimaryIndex()
	}
	b.undo.Save(b.rope, b.Cursor, false)
	if n > 0 {
		b.rope = b.rope.Delete(startOff, n)
	}
	if len(newText) > 0 {
		b.rope = b.rope.Insert(startOff, newText)
	}
	// ReplaceRange is used by completion, formatting, plugins, and workspace
	// edits. Those callers can replace text before the cursor without changing
	// the cursor explicitly. Rebase every position against the same edit so a
	// later Backspace/Delete cannot consult coordinates from the old rope.
	b.Cursor = mapPositionAfterReplace(oldRope, b.rope, oldCursor, startOff, endOff, len(newText))
	if len(oldSelections) == 0 {
		b.Selections = NewSelections(b.Cursor)
	} else {
		mapped := make([]Selection, len(oldSelections))
		for i, selection := range oldSelections {
			mapped[i] = Selection{
				Anchor: mapPositionAfterReplace(oldRope, b.rope, selection.Anchor, startOff, endOff, len(newText)),
				Head:   mapPositionAfterReplace(oldRope, b.rope, selection.Head, startOff, endOff, len(newText)),
			}
		}
		b.Selections = &Selections{selections: mapped, primary: primary, dirty: true}
		b.Selections.Normalize()
		b.Cursor = b.Selections.PrimaryCursor()
	}
	b.dirty = true
	b.version++
	b.lastChange = &EditChange{
		StartLine: start.Line, StartCol: start.Col,
		EndLine: end.Line, EndCol: end.Col,
		Text: string(newText),
	}
}

// mapPositionAfterReplace maps a position from oldRope to the document after
// replacing [startOff,endOff) with newLen bytes. Positions inside the replaced
// range resolve to the end of the replacement, which is the useful cursor
// position for a completion or an external edit that swallowed a selection.
func mapPositionAfterReplace(oldRope, newRope *Rope, pos Position, startOff, endOff, newLen int) Position {
	off := oldRope.PositionToOffset(pos)
	switch {
	case off < startOff:
		// unchanged
	case off >= endOff:
		off += newLen - (endOff - startOff)
	default:
		off = startOff + newLen
	}
	return newRope.OffsetToPosition(off)
}

// MoveCursor moves the cursor in the given direction.
func (b *Buffer) MoveCursor(dir Direction) {
	b.Cursor = b.ClampPosition(b.Cursor)
	switch dir {
	case DirLeft:
		if b.Cursor.Col > 0 {
			offset := b.rope.PositionToOffset(b.Cursor)
			_, size, ok := b.rope.RuneBefore(offset)
			if ok {
				b.Cursor.Col -= size
			}
		} else if b.Cursor.Line > 0 {
			b.Cursor.Line--
			b.Cursor.Col = b.rope.LineLen(b.Cursor.Line)
		}
	case DirRight:
		lineLen := b.rope.LineLen(b.Cursor.Line)
		if b.Cursor.Col < lineLen {
			offset := b.rope.PositionToOffset(b.Cursor)
			_, size, ok := b.rope.RuneAt(offset)
			if ok {
				b.Cursor.Col += size
			}
		} else if b.Cursor.Line < b.rope.LineCount()-1 {
			b.Cursor.Line++
			b.Cursor.Col = 0
		}
	case DirUp:
		if b.Cursor.Line > 0 {
			b.Cursor = b.ClampPosition(Position{Line: b.Cursor.Line - 1, Col: b.Cursor.Col})
		}
	case DirDown:
		if b.Cursor.Line < b.rope.LineCount()-1 {
			b.Cursor = b.ClampPosition(Position{Line: b.Cursor.Line + 1, Col: b.Cursor.Col})
		}
	}
	// Keep the primary selection aligned with the cursor. Normal movement is
	// allowed to collapse a selection; shift-movement saves the anchor and
	// restores it in ExtendSelection after this method returns.
	b.syncPrimarySelection()
}

// SetSelection sets the selection anchored at the anchor, with head as the cursor.
func (b *Buffer) SetSelection(anchor, head Position) {
	if b.Selections == nil || len(b.Selections.selections) == 0 {
		b.Selections = NewSelections(anchor)
	}
	if b.Selections.primary < 0 || b.Selections.primary >= len(b.Selections.selections) {
		b.Selections.primary = 0
	}
	b.Selections.selections[b.Selections.primary] = Selection{Anchor: anchor, Head: head}
	b.Selections.Clear() // Ensure only one selection
	b.Cursor = head
}

// syncPrimarySelection collapses the primary selection onto the cursor. Use it
// after moving or repositioning the cursor outside SetSelection/SetCursor, so
// the two can never disagree: Backspace and Delete branch on whether the
// primary selection is empty, and edit records are derived from the cursor.
//
// It is a no-op with multiple cursors, where the selections are managed as a set.
func (b *Buffer) syncPrimarySelection() {
	if b.Selections == nil || b.Selections.Count() != 1 {
		return
	}
	b.Selections.selections[b.Selections.primary] = Selection{Anchor: b.Cursor, Head: b.Cursor}
}

// ClearSelection clears any active selection.
func (b *Buffer) ClearSelection() {
	b.SetSelection(b.Cursor, b.Cursor)
}

// CursorToLineStart moves the cursor to the beginning of the current line.
func (b *Buffer) CursorToLineStart() {
	cursor := b.Cursor
	cursor.Col = 0
	b.SetCursor(cursor)
}

// CursorToLineEnd moves the cursor to the end of the current line.
func (b *Buffer) CursorToLineEnd() {
	cursor := b.Cursor
	cursor.Col = b.rope.LineLen(cursor.Line)
	b.SetCursor(cursor)
}

// MoveCursors moves all cursors in the given direction.
func (b *Buffer) MoveCursors(dir Direction) {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}
	b.Selections.Normalize()
	for i := range b.Selections.selections {
		sel := &b.Selections.selections[i]
		sel.Anchor = b.ClampPosition(sel.Anchor)
		sel.Head = b.ClampPosition(sel.Head)
		if !sel.IsEmpty() && (dir == DirLeft || dir == DirRight) {
			start, end := sel.Ordered()
			target := start
			if dir == DirRight {
				target = end
			}
			sel.Anchor = target
			sel.Head = target
			continue
		}

		switch dir {
		case DirLeft:
			if sel.Head.Col > 0 {
				offset := b.rope.PositionToOffset(sel.Head)
				if _, size, ok := b.rope.RuneBefore(offset); ok {
					sel.Head.Col -= size
				}
			} else if sel.Head.Line > 0 {
				sel.Head.Line--
				sel.Head.Col = b.rope.LineLen(sel.Head.Line)
			}
		case DirRight:
			lineLen := b.rope.LineLen(sel.Head.Line)
			if sel.Head.Col < lineLen {
				offset := b.rope.PositionToOffset(sel.Head)
				if _, size, ok := b.rope.RuneAt(offset); ok {
					sel.Head.Col += size
				}
			} else if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head.Line++
				sel.Head.Col = 0
			}
		case DirUp:
			if sel.Head.Line > 0 {
				sel.Head = b.ClampPosition(Position{Line: sel.Head.Line - 1, Col: sel.Head.Col})
			}
		case DirDown:
			if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head = b.ClampPosition(Position{Line: sel.Head.Line + 1, Col: sel.Head.Col})
			}
		}

		sel.Anchor = sel.Head
	}

	b.Selections.dirty = true
	b.Selections.Normalize()
	// Update b.Cursor to match primary
	b.Cursor = b.Selections.PrimaryCursor()
}

// ExtendCursors extends all selections in the given direction.
func (b *Buffer) ExtendCursors(dir Direction) {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}
	b.Selections.Normalize()
	for i := range b.Selections.selections {
		sel := &b.Selections.selections[i]
		sel.Anchor = b.ClampPosition(sel.Anchor)
		sel.Head = b.ClampPosition(sel.Head)

		switch dir {
		case DirLeft:
			if sel.Head.Col > 0 {
				offset := b.rope.PositionToOffset(sel.Head)
				if _, size, ok := b.rope.RuneBefore(offset); ok {
					sel.Head.Col -= size
				}
			} else if sel.Head.Line > 0 {
				sel.Head.Line--
				sel.Head.Col = b.rope.LineLen(sel.Head.Line)
			}
		case DirRight:
			lineLen := b.rope.LineLen(sel.Head.Line)
			if sel.Head.Col < lineLen {
				offset := b.rope.PositionToOffset(sel.Head)
				if _, size, ok := b.rope.RuneAt(offset); ok {
					sel.Head.Col += size
				}
			} else if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head.Line++
				sel.Head.Col = 0
			}
		case DirUp:
			if sel.Head.Line > 0 {
				sel.Head = b.ClampPosition(Position{Line: sel.Head.Line - 1, Col: sel.Head.Col})
			}
		case DirDown:
			if sel.Head.Line < b.rope.LineCount()-1 {
				sel.Head = b.ClampPosition(Position{Line: sel.Head.Line + 1, Col: sel.Head.Col})
			}
		}
		// Don't update anchor - we're extending
	}

	b.Selections.dirty = true
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
}

type selectionCollapseMode uint8

const (
	collapseAfterMove selectionCollapseMode = iota
	collapseToStart
	collapseToEnd
)

func (b *Buffer) transformSelections(move func(Position) Position, extend bool, collapse selectionCollapseMode) {
	if b.Selections == nil || b.Selections.Count() == 0 || move == nil {
		return
	}
	b.Selections.Normalize()
	for i := range b.Selections.selections {
		sel := &b.Selections.selections[i]
		if !extend && !sel.IsEmpty() && collapse != collapseAfterMove {
			start, end := sel.Ordered()
			target := start
			if collapse == collapseToEnd {
				target = end
			}
			sel.Anchor = target
			sel.Head = target
			continue
		}
		sel.Head = move(b.ClampPosition(sel.Head))
		if !extend {
			sel.Anchor = sel.Head
		}
	}
	b.Selections.dirty = true
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
}

// MoveCursorsWordLeft and MoveCursorsWordRight move every active cursor while
// sharing one bounded scan budget across the whole selection set.
func (b *Buffer) MoveCursorsWordLeft() {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}
	budget := max(utf8.UTFMax, maxInteractiveWordNavigationBytes/b.Selections.Count())
	b.transformSelections(func(pos Position) Position {
		return b.wordPositionLeft(pos, budget)
	}, false, collapseToStart)
}

func (b *Buffer) MoveCursorsWordRight() {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}
	budget := max(utf8.UTFMax, maxInteractiveWordNavigationBytes/b.Selections.Count())
	b.transformSelections(func(pos Position) Position {
		return b.wordPositionRight(pos, budget)
	}, false, collapseToEnd)
}

func (b *Buffer) ExtendCursorsWordLeft() {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}
	budget := max(utf8.UTFMax, maxInteractiveWordNavigationBytes/b.Selections.Count())
	b.transformSelections(func(pos Position) Position {
		return b.wordPositionLeft(pos, budget)
	}, true, collapseAfterMove)
}

func (b *Buffer) ExtendCursorsWordRight() {
	if b.Selections == nil || b.Selections.Count() == 0 {
		return
	}
	budget := max(utf8.UTFMax, maxInteractiveWordNavigationBytes/b.Selections.Count())
	b.transformSelections(func(pos Position) Position {
		return b.wordPositionRight(pos, budget)
	}, true, collapseAfterMove)
}

func (b *Buffer) MoveCursorsToLineStart() {
	b.transformSelections(func(pos Position) Position {
		pos.Col = 0
		return pos
	}, false, collapseAfterMove)
}

func (b *Buffer) MoveCursorsToLineEnd() {
	b.transformSelections(func(pos Position) Position {
		pos.Col = b.rope.LineLen(pos.Line)
		return pos
	}, false, collapseAfterMove)
}

func (b *Buffer) ExtendCursorsToLineStart() {
	b.transformSelections(func(pos Position) Position {
		pos.Col = 0
		return pos
	}, true, collapseAfterMove)
}

func (b *Buffer) ExtendCursorsToLineEnd() {
	b.transformSelections(func(pos Position) Position {
		pos.Col = b.rope.LineLen(pos.Line)
		return pos
	}, true, collapseAfterMove)
}

func (b *Buffer) ExtendCursorsToDocStart() {
	b.transformSelections(func(pos Position) Position {
		return Position{Line: 0, Col: 0}
	}, true, collapseAfterMove)
}

func (b *Buffer) ExtendCursorsToDocEnd() {
	lastLine := b.rope.LineCount() - 1
	lastCol := b.rope.LineLen(lastLine)
	b.transformSelections(func(pos Position) Position {
		return Position{Line: lastLine, Col: lastCol}
	}, true, collapseAfterMove)
}

// MoveCursorsByLines moves every cursor by a bounded logical-line delta. Page
// navigation uses it to preserve a multicursor set; ClampPosition keeps each
// byte column on a valid UTF-8 boundary when target lines differ in encoding.
func (b *Buffer) MoveCursorsByLines(delta int) {
	if delta == 0 {
		return
	}
	lastLine := max(0, b.rope.LineCount()-1)
	b.transformSelections(func(pos Position) Position {
		pos.Line = min(lastLine, max(0, pos.Line+delta))
		return b.ClampPosition(pos)
	}, false, collapseAfterMove)
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
	if err := WriteRopeAtomicallyWithLineEnding(path, snapshot, b.lineEnding); err != nil {
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

// Word boundary helpers retain byte offsets for Rope and LSP positions while
// classifying complete UTF-8 runes. Letters, numbers, combining marks, and
// underscore form words; Unicode whitespace separates them; every other rune
// belongs to the punctuation/symbol class. Invalid bytes decode one at a time
// as RuneError and therefore remain a safely navigable symbol run.
type wordRuneClass uint8

const (
	wordRuneSymbol wordRuneClass = iota
	wordRuneWord
	wordRuneSpace
)

// maxInteractiveWordNavigationBytes caps all word-boundary text copied by one
// keyboard command. Multicursor commands divide this budget among cursors, so
// a thousand cursors cannot turn Ctrl+Arrow into document-sized work in Update.
const maxInteractiveWordNavigationBytes = 64 << 10

func classifyWordRune(r rune) wordRuneClass {
	switch {
	case r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
		return wordRuneWord
	case unicode.IsSpace(r):
		return wordRuneSpace
	default:
		return wordRuneSymbol
	}
}

func wordRuneAt(line []byte, col int) (wordRuneClass, int) {
	r, size := utf8.DecodeRune(line[col:])
	return classifyWordRune(r), size
}

func wordRuneBefore(line []byte, col int) (int, wordRuneClass) {
	r, size := utf8.DecodeLastRune(line[:col])
	return col - size, classifyWordRune(r)
}

// wordRuneBoundary clamps a possibly stale direct Cursor assignment to the
// start of its rune so word motion never leaves the cursor on a continuation
// byte. Normal editor paths already provide boundary-aligned columns.
func wordRuneBoundary(line []byte, col int) int {
	col = max(0, min(col, len(line)))
	if col == len(line) || utf8.RuneStart(line[col]) {
		return col
	}
	start := col
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	_, size := utf8.DecodeRune(line[start:])
	if size > 1 && start+size > col {
		return start
	}
	// A stray continuation byte is its own invalid one-byte symbol, not part
	// of the preceding rune.
	return col
}

func trimLeadingWhitespace(b []byte) []byte {
	return bytes.TrimLeft(b, " \t")
}

func wordColumnLeft(line []byte, col int) int {
	col = wordRuneBoundary(line, col)
	if col == 0 {
		return 0
	}

	// Skip whitespace backwards
	for col > 0 {
		start, class := wordRuneBefore(line, col)
		if class != wordRuneSpace {
			break
		}
		col = start
	}
	if col == 0 {
		return 0
	}

	// Skip same-class characters backwards
	_, targetClass := wordRuneBefore(line, col)
	for col > 0 {
		start, class := wordRuneBefore(line, col)
		if class != targetClass {
			break
		}
		col = start
	}
	return col
}

func wordColumnRight(line []byte, col int) int {
	col = wordRuneBoundary(line, col)
	lineLen := len(line)
	if col >= lineLen {
		return lineLen
	}

	// Skip same-class characters forward
	targetClass, _ := wordRuneAt(line, col)
	if targetClass != wordRuneSpace {
		for col < lineLen {
			class, size := wordRuneAt(line, col)
			if class != targetClass {
				break
			}
			col += size
		}
	}

	// Skip whitespace forward
	for col < lineLen {
		class, size := wordRuneAt(line, col)
		if class != wordRuneSpace {
			break
		}
		col += size
	}
	return col
}

func (b *Buffer) wordPositionLeft(pos Position, budget int) Position {
	pos = b.ClampPosition(pos)
	if pos.Col == 0 {
		if pos.Line > 0 {
			pos.Line--
			pos.Col = b.rope.LineLen(pos.Line)
		}
		return pos
	}
	budget = max(utf8.UTFMax, budget)
	startCol := max(0, pos.Col-budget)
	startCol = b.ClampPosition(Position{Line: pos.Line, Col: startCol}).Col
	lineStart := b.rope.LineStart(pos.Line)
	segment := []byte(b.rope.StringRange(lineStart+startCol, lineStart+pos.Col))
	pos.Col = startCol + wordColumnLeft(segment, len(segment))
	return pos
}

func (b *Buffer) wordPositionRight(pos Position, budget int) Position {
	pos = b.ClampPosition(pos)
	lineLen := b.rope.LineLen(pos.Line)
	if pos.Col >= lineLen {
		if pos.Line < b.rope.LineCount()-1 {
			pos.Line++
			pos.Col = 0
		}
		return pos
	}
	budget = max(utf8.UTFMax, budget)
	endCol := min(lineLen, pos.Col+budget)
	endCol = b.ClampPosition(Position{Line: pos.Line, Col: endCol}).Col
	lineStart := b.rope.LineStart(pos.Line)
	segment := []byte(b.rope.StringRange(lineStart+pos.Col, lineStart+endCol))
	pos.Col += wordColumnRight(segment, 0)
	return pos
}

// MoveCursorWordLeft moves the cursor to the start of the previous word.
func (b *Buffer) MoveCursorWordLeft() {
	b.SetCursor(b.wordPositionLeft(b.Cursor, maxInteractiveWordNavigationBytes))
}

// MoveCursorWordRight moves the cursor to the start of the next word.
func (b *Buffer) MoveCursorWordRight() {
	b.SetCursor(b.wordPositionRight(b.Cursor, maxInteractiveWordNavigationBytes))
}

// BackspaceWord deletes from the cursor to the start of the previous word.
func (b *Buffer) BackspaceWord() {
	if b.Selections != nil && b.Selections.Count() > 1 {
		b.Selections.Normalize()
		budget := max(utf8.UTFMax, maxInteractiveWordNavigationBytes/b.Selections.Count())
		ranges := make([]cursorDeleteRange, len(b.Selections.selections))
		for i, sel := range b.Selections.selections {
			start, end := sel.Ordered()
			if sel.IsEmpty() {
				start = b.wordPositionLeft(sel.Head, budget)
				end = sel.Head
			}
			ranges[i] = cursorDeleteRange{
				start: b.rope.PositionToOffset(start),
				end:   b.rope.PositionToOffset(end),
			}
		}
		b.applyCursorDeletionRanges(ranges)
		return
	}
	if b.Selections != nil && b.Selections.Count() == 1 && !b.Selections.Primary().IsEmpty() {
		b.DeleteSelection()
		return
	}
	startPos := b.Cursor
	target := b.wordPositionLeft(startPos, maxInteractiveWordNavigationBytes)
	b.SetCursor(target)
	if startPos == target {
		return
	}
	startOff := b.rope.PositionToOffset(target)
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
	if b.Selections != nil && b.Selections.Count() > 1 {
		b.Selections.Normalize()
		budget := max(utf8.UTFMax, maxInteractiveWordNavigationBytes/b.Selections.Count())
		ranges := make([]cursorDeleteRange, len(b.Selections.selections))
		for i, sel := range b.Selections.selections {
			start, end := sel.Ordered()
			if sel.IsEmpty() {
				start = sel.Head
				end = b.wordPositionRight(sel.Head, budget)
			}
			ranges[i] = cursorDeleteRange{
				start: b.rope.PositionToOffset(start),
				end:   b.rope.PositionToOffset(end),
			}
		}
		b.applyCursorDeletionRanges(ranges)
		return
	}
	if b.Selections != nil && b.Selections.Count() == 1 && !b.Selections.Primary().IsEmpty() {
		b.DeleteSelection()
		return
	}
	saved := b.Cursor
	endPos := b.wordPositionRight(saved, maxInteractiveWordNavigationBytes)
	b.SetCursor(saved)
	if saved == endPos {
		return
	}
	startOff := b.rope.PositionToOffset(saved)
	endOff := b.rope.PositionToOffset(endPos)
	n := endOff - startOff
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = b.rope.Delete(startOff, n)
	b.SetCursor(saved)
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
	b.SetCursor(Position{0, 0})
}

// CursorToDocEnd moves the cursor to the end of the document.
func (b *Buffer) CursorToDocEnd() {
	lastLine := b.rope.LineCount() - 1
	b.SetCursor(Position{lastLine, b.rope.LineLen(lastLine)})
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

// SelectWordAtCursor selects the Unicode word or symbol run under the cursor.
func (b *Buffer) SelectWordAtCursor() {
	line := b.rope.Line(b.Cursor.Line)
	col := wordRuneBoundary(line, b.Cursor.Col)
	if col >= len(line) {
		return
	}
	class, size := wordRuneAt(line, col)
	if class == wordRuneSpace {
		return
	}

	start := col
	for start > 0 {
		previous, previousClass := wordRuneBefore(line, start)
		if previousClass != class {
			break
		}
		start = previous
	}
	end := col + size
	for end < len(line) {
		nextClass, nextSize := wordRuneAt(line, end)
		if nextClass != class {
			break
		}
		end += nextSize
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
			newPos := b.ClampPosition(Position{
				Line: sel.Head.Line - 1,
				Col:  min(sel.Head.Col, b.rope.LineLen(sel.Head.Line-1)),
			})
			b.Selections.Add(Selection{Anchor: newPos, Head: newPos})
		}
	}
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
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
			newPos := b.ClampPosition(Position{
				Line: sel.Head.Line + 1,
				Col:  min(sel.Head.Col, b.rope.LineLen(sel.Head.Line+1)),
			})
			b.Selections.Add(Selection{Anchor: newPos, Head: newPos})
		}
	}
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
}

// DropSecondaryCursors collapses a multi-cursor selection set back to a single
// caret at the primary cursor. It is the keyboard escape hatch for multi-cursor
// modes: without it, a stray Ctrl+D chain or Ctrl+U leaves every subsequent
// keystroke editing N places until a mouse click defuses it. A single selection
// is left untouched. The change is cursor-only, so history, version, the dirty
// flag, and the incremental change record are all preserved.
func (b *Buffer) DropSecondaryCursors() {
	if b.Selections == nil || b.Selections.Count() <= 1 {
		return
	}
	cursor := b.Selections.PrimaryCursor()
	b.Selections = NewSelections(cursor)
	b.Cursor = cursor
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

type selectedLineRange struct {
	start int
	end   int // inclusive
}

// selectedLineRanges projects normalized character selections onto logical
// line blocks. Half-open selection endpoints at column zero do not include the
// endpoint line. Overlapping blocks merge so no structural command can edit a
// line twice; callers choose whether adjacent blocks retain independent toggle
// semantics or merge into one union.
func (b *Buffer) selectedLineRanges(mergeAdjacent bool) []selectedLineRange {
	if b.Selections == nil || b.Selections.Count() == 0 {
		b.Selections = NewSelections(b.ClampPosition(b.Cursor))
	}
	b.Selections.Normalize()
	lastLine := max(0, b.rope.LineCount()-1)
	ranges := make([]selectedLineRange, 0, b.Selections.Count())
	for _, selection := range b.Selections.All() {
		start, end := selection.Ordered()
		startLine := min(lastLine, max(0, start.Line))
		endLine := min(lastLine, max(startLine, end.Line))
		if !selection.IsEmpty() && end.Col == 0 && endLine > startLine {
			endLine--
		}
		ranges = append(ranges, selectedLineRange{start: startLine, end: endLine})
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start != ranges[j].start {
			return ranges[i].start < ranges[j].start
		}
		return ranges[i].end < ranges[j].end
	})
	merged := ranges[:0]
	for _, selected := range ranges {
		if len(merged) == 0 {
			merged = append(merged, selected)
			continue
		}
		last := &merged[len(merged)-1]
		joins := selected.start <= last.end
		if mergeAdjacent {
			joins = selected.start <= last.end+1
		}
		if joins {
			last.end = max(last.end, selected.end)
			continue
		}
		merged = append(merged, selected)
	}
	return merged
}

// SelectedLineCount returns the number of unique logical lines targeted by
// the current selection set. The editor uses it before synchronous structural
// commands so many collapsed cursors cannot bypass the multiline budget.
func (b *Buffer) SelectedLineCount() int {
	total := 0
	for _, selected := range b.selectedLineRanges(true) {
		total += selected.end - selected.start + 1
	}
	return total
}

type structuralLineEdit struct {
	line       int
	col        int
	delete     int
	insert     []byte
	shiftAtCol bool
}

// applyStructuralLineEdits validates one edit per logical line, applies the
// immutable-rope mutations from bottom to top, then rebases every cursor and
// selection through the per-line column delta. It records exactly one Undo
// snapshot, document version, and full-sync LSP change.
func (b *Buffer) applyStructuralLineEdits(edits []structuralLineEdit) StructuralEditResult {
	if len(edits) == 0 {
		return StructuralEditNoChange
	}
	valid := edits[:0]
	for _, edit := range edits {
		if edit.delete == 0 && len(edit.insert) == 0 {
			continue
		}
		if edit.line < 0 || edit.line >= b.rope.LineCount() || edit.col < 0 || edit.delete < 0 ||
			bytes.IndexByte(edit.insert, '\n') >= 0 {
			return StructuralEditNoChange
		}
		lineLen := b.rope.LineLen(edit.line)
		if edit.col > lineLen || edit.delete > lineLen-edit.col {
			return StructuralEditNoChange
		}
		valid = append(valid, edit)
	}
	if len(valid) == 0 {
		return StructuralEditNoChange
	}
	edits = valid
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].line != edits[j].line {
			return edits[i].line < edits[j].line
		}
		return edits[i].col < edits[j].col
	})
	for i := 1; i < len(edits); i++ {
		if edits[i-1].line == edits[i].line {
			return StructuralEditNoChange
		}
	}

	if b.Selections == nil || b.Selections.Count() == 0 {
		b.Selections = NewSelections(b.ClampPosition(b.Cursor))
	}
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
	b.undo.Save(b.rope, b.Cursor, false)
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		offset := b.rope.LineStart(edit.line) + edit.col
		if edit.delete > 0 {
			b.rope = b.rope.Delete(offset, edit.delete)
		}
		if len(edit.insert) > 0 {
			b.rope = b.rope.Insert(offset, edit.insert)
		}
	}

	adjust := func(pos Position) Position {
		idx := sort.Search(len(edits), func(i int) bool { return edits[i].line >= pos.Line })
		if idx >= len(edits) || edits[idx].line != pos.Line {
			return pos
		}
		edit := edits[idx]
		delta := len(edit.insert) - edit.delete
		if delta == 0 || pos.Col < edit.col || (pos.Col == edit.col && !edit.shiftAtCol) {
			return pos
		}
		col := max(edit.col, pos.Col+delta)
		col = min(col, b.rope.LineLen(pos.Line))
		return Position{Line: pos.Line, Col: col}
	}
	primary := b.Selections.PrimaryIndex()
	rebased := make([]Selection, len(b.Selections.selections))
	for i, selection := range b.Selections.selections {
		rebased[i] = Selection{Anchor: adjust(selection.Anchor), Head: adjust(selection.Head)}
	}
	b.Selections = &Selections{selections: rebased, primary: primary, dirty: true}
	b.Selections.Normalize()
	b.Cursor = b.Selections.PrimaryCursor()
	b.dirty = true
	b.version++
	b.lastChange = nil
	return StructuralEditApplied
}

func (b *Buffer) dedentBytesAtLine(line, tabSize int) int {
	if tabSize <= 0 || line < 0 || line >= b.rope.LineCount() || b.rope.LineLen(line) == 0 {
		return 0
	}
	start := b.rope.LineStart(line)
	first, ok := b.rope.ByteAtSafe(start)
	if !ok {
		return 0
	}
	if first == '\t' {
		return 1
	}
	n := 0
	lineLen := b.rope.LineLen(line)
	for n < tabSize && n < lineLen {
		current, exists := b.rope.ByteAtSafe(start + n)
		if !exists || current != ' ' {
			break
		}
		n++
	}
	return n
}

type commentLineInfo struct {
	line      int
	indent    int
	empty     bool
	commented bool
	remove    int
}

// inspectCommentLine finds indentation and an optional comment marker without
// materializing the logical line. budget is shared by the complete command;
// returning false rejects the transaction before Undo or text can change.
func (b *Buffer) inspectCommentLine(line int, prefix []byte, budget *int) (commentLineInfo, bool) {
	info := commentLineInfo{line: line}
	lineLen := b.rope.LineLen(line)
	lineStart := b.rope.LineStart(line)
	for info.indent < lineLen {
		if *budget <= 0 {
			return commentLineInfo{}, false
		}
		current, ok := b.rope.ByteAtSafe(lineStart + info.indent)
		if !ok {
			return commentLineInfo{}, false
		}
		*budget = *budget - 1
		if current != ' ' && current != '\t' {
			break
		}
		info.indent++
	}
	if info.indent == lineLen {
		info.empty = true
		return info, true
	}
	if len(prefix) > lineLen-info.indent {
		return info, true
	}
	for i, want := range prefix {
		if *budget <= 0 {
			return commentLineInfo{}, false
		}
		got, ok := b.rope.ByteAtSafe(lineStart + info.indent + i)
		if !ok {
			return commentLineInfo{}, false
		}
		*budget = *budget - 1
		if got != want {
			return info, true
		}
	}
	info.commented = true
	info.remove = len(prefix)
	spaceOffset := info.indent + len(prefix)
	if spaceOffset < lineLen {
		if *budget <= 0 {
			return commentLineInfo{}, false
		}
		next, ok := b.rope.ByteAtSafe(lineStart + spaceOffset)
		if !ok {
			return commentLineInfo{}, false
		}
		*budget = *budget - 1
		if next == ' ' {
			info.remove++
		}
	}
	return info, true
}

// ToggleLineComment toggles every independent selection block. Overlapping
// line spans merge to prevent double edits, while adjacent collapsed cursors
// remain independent so one can comment as another uncomments. Blank lines are
// skipped, and the entire command is rejected if prefix inspection exceeds the
// fixed synchronous byte budget.
func (b *Buffer) ToggleLineComment(prefix string) StructuralEditResult {
	if prefix == "" {
		return StructuralEditNoChange
	}
	if len(prefix) > MaxStructuralPrefixBytes {
		return StructuralEditLimit
	}
	blocks := b.selectedLineRanges(false)
	budget := MaxStructuralPrefixBytes
	prefixBytes := []byte(prefix)
	commentPrefix := []byte(prefix + " ")
	edits := make([]structuralLineEdit, 0, b.SelectedLineCount())
	for _, block := range blocks {
		infos := make([]commentLineInfo, 0, block.end-block.start+1)
		allCommented := true
		nonEmpty := 0
		minIndent := -1
		for line := block.start; line <= block.end; line++ {
			info, ok := b.inspectCommentLine(line, prefixBytes, &budget)
			if !ok {
				return StructuralEditLimit
			}
			infos = append(infos, info)
			if info.empty {
				continue
			}
			nonEmpty++
			allCommented = allCommented && info.commented
			if minIndent < 0 || info.indent < minIndent {
				minIndent = info.indent
			}
		}
		if nonEmpty == 0 {
			continue
		}
		for _, info := range infos {
			if info.empty {
				continue
			}
			if allCommented {
				edits = append(edits, structuralLineEdit{line: info.line, col: info.indent, delete: info.remove})
				continue
			}
			edits = append(edits, structuralLineEdit{
				line:       info.line,
				col:        minIndent,
				insert:     commentPrefix,
				shiftAtCol: true,
			})
		}
	}
	return b.applyStructuralLineEdits(edits)
}

// MoveLineUp swaps every selected line block with the line above it.
func (b *Buffer) MoveLineUp() {
	b.applyLineTransform(LineTransformMoveUp)
}

// MoveLineDown swaps every selected line block with the line below it.
func (b *Buffer) MoveLineDown() {
	b.applyLineTransform(LineTransformMoveDown)
}

// DuplicateLineDown duplicates every independent selected line block below.
func (b *Buffer) DuplicateLineDown() {
	b.applyLineTransform(LineTransformDuplicateDown)
}

// DuplicateLineUp duplicates every independent selected line block above.
func (b *Buffer) DuplicateLineUp() {
	b.applyLineTransform(LineTransformDuplicateUp)
}

// DeleteLine deletes every unique selected line block.
func (b *Buffer) DeleteLine() {
	b.applyLineTransform(LineTransformDelete)
}

// IndentLines indents every unique logical line targeted by the selections.
func (b *Buffer) IndentLines(tabSize int) StructuralEditResult {
	indent := IndentString(tabSize)
	if len(indent) == 0 {
		return StructuralEditNoChange
	}
	edits := make([]structuralLineEdit, 0, b.SelectedLineCount())
	for _, selected := range b.selectedLineRanges(true) {
		for line := selected.start; line <= selected.end; line++ {
			edits = append(edits, structuralLineEdit{line: line, insert: indent})
		}
	}
	return b.applyStructuralLineEdits(edits)
}

// DedentLines removes one indentation level from every unique logical line
// targeted by the selections.
func (b *Buffer) DedentLines(tabSize int) StructuralEditResult {
	edits := make([]structuralLineEdit, 0, b.SelectedLineCount())
	for _, selected := range b.selectedLineRanges(true) {
		for line := selected.start; line <= selected.end; line++ {
			if n := b.dedentBytesAtLine(line, tabSize); n > 0 {
				edits = append(edits, structuralLineEdit{line: line, delete: n})
			}
		}
	}
	return b.applyStructuralLineEdits(edits)
}
