package text

import "time"

const groupTimeout = 300 * time.Millisecond
const maxUndoEntries = 1000

// undoEntry stores a snapshot of the rope, cursor, and selections.
type undoEntry struct {
	rope       *Rope
	cursor     Position
	selections []Selection
	primary    int
}

// UndoStack manages undo/redo using rope snapshots.
// Since ropes are persistent (immutable), snapshots share structure and are cheap.
type UndoStack struct {
	undo              []undoEntry
	redo              []undoEntry
	lastTime          time.Time
	lastWasCharInsert bool
	lastCharInsertEnd Position
	hasCharInsertEnd  bool
}

// NewUndoStack returns a new empty UndoStack.
func NewUndoStack() *UndoStack {
	return &UndoStack{}
}

// Save records a snapshot before an edit. Call this before mutating the rope.
// isCharInsert should be true for single-character inserts (for auto-grouping).
func (u *UndoStack) Save(rope *Rope, cursor Position, isCharInsert bool) {
	u.saveEntry(undoEntry{rope: rope, cursor: cursor}, isCharInsert)
}

func (u *UndoStack) saveEntry(entry undoEntry, isCharInsert bool) {
	now := time.Now()

	// auto-grouping: skip saving if this is a consecutive char insert within timeout
	if isCharInsert && u.lastWasCharInsert && u.hasCharInsertEnd &&
		u.lastCharInsertEnd == entry.cursor && now.Sub(u.lastTime) < groupTimeout && len(u.undo) > 0 {
		u.lastTime = now
		u.redo = nil
		return
	}

	u.undo = append(u.undo, entry)
	if len(u.undo) > maxUndoEntries {
		u.undo = u.undo[len(u.undo)-maxUndoEntries:]
	}
	u.redo = nil
	u.lastTime = now
	u.lastWasCharInsert = isCharInsert
	u.hasCharInsertEnd = false
}

// MarkCharInsertEnd records the cursor after a character insertion. The next
// character can join the undo group only when it starts exactly there; moving
// the cursor must create a new undo boundary even within the time window.
func (u *UndoStack) MarkCharInsertEnd(cursor Position) {
	if u.lastWasCharInsert {
		u.lastCharInsertEnd = cursor
		u.hasCharInsertEnd = true
	}
}

// Undo returns the previous rope and cursor, pushing current state to redo.
func (u *UndoStack) Undo(currentRope *Rope, currentCursor Position) (*Rope, Position, bool) {
	rope, cursor, _, _, ok := u.undoState(currentRope, currentCursor, nil, 0, true)
	return rope, cursor, ok
}

func (u *UndoStack) undoState(currentRope *Rope, currentCursor Position, currentSels []Selection, currentPrimary int, undo bool) (*Rope, Position, []Selection, int, bool) {
	src, dst := &u.undo, &u.redo
	if !undo {
		src, dst = &u.redo, &u.undo
	}
	if len(*src) == 0 {
		return nil, Position{}, nil, 0, false
	}
	*dst = append(*dst, undoEntry{
		rope:       currentRope,
		cursor:     currentCursor,
		selections: cloneSelections(currentSels),
		primary:    currentPrimary,
	})
	entry := (*src)[len(*src)-1]
	*src = (*src)[:len(*src)-1]
	u.lastWasCharInsert = false
	u.hasCharInsertEnd = false
	return entry.rope, entry.cursor, cloneSelections(entry.selections), entry.primary, true
}

// Redo returns the next rope and cursor, pushing current state to undo.
func (u *UndoStack) Redo(currentRope *Rope, currentCursor Position) (*Rope, Position, bool) {
	rope, cursor, _, _, ok := u.undoState(currentRope, currentCursor, nil, 0, false)
	return rope, cursor, ok
}

// CanUndo returns true if there are snapshots to undo to.
func (u *UndoStack) CanUndo() bool {
	return len(u.undo) > 0
}

// CanRedo returns true if there are snapshots to redo to.
func (u *UndoStack) CanRedo() bool {
	return len(u.redo) > 0
}

func cloneSelections(selections []Selection) []Selection {
	if len(selections) == 0 {
		return nil
	}
	return append([]Selection(nil), selections...)
}
