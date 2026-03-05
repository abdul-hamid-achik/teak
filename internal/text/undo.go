package text

import "time"

const groupTimeout = 300 * time.Millisecond
const maxUndoEntries = 1000

// undoEntry stores a snapshot of the rope and cursor at a point in time.
type undoEntry struct {
	rope   *Rope
	cursor Position
}

// UndoStack manages undo/redo using rope snapshots.
// Since ropes are persistent (immutable), snapshots share structure and are cheap.
type UndoStack struct {
	undo              []undoEntry
	redo              []undoEntry
	lastTime          time.Time
	lastWasCharInsert bool
}

// NewUndoStack returns a new empty UndoStack.
func NewUndoStack() *UndoStack {
	return &UndoStack{}
}

// Save records a snapshot before an edit. Call this before mutating the rope.
// isCharInsert should be true for single-character inserts (for auto-grouping).
func (u *UndoStack) Save(rope *Rope, cursor Position, isCharInsert bool) {
	now := time.Now()

	// auto-grouping: skip saving if this is a consecutive char insert within timeout
	if isCharInsert && u.lastWasCharInsert && now.Sub(u.lastTime) < groupTimeout && len(u.undo) > 0 {
		u.lastTime = now
		// clear redo on new edit
		u.redo = nil
		return
	}

	u.undo = append(u.undo, undoEntry{rope: rope, cursor: cursor})
	if len(u.undo) > maxUndoEntries {
		// Keep only the most recent entries
		u.undo = u.undo[len(u.undo)-maxUndoEntries:]
	}
	u.redo = nil
	u.lastTime = now
	u.lastWasCharInsert = isCharInsert
}

// Undo returns the previous rope and cursor, pushing current state to redo.
func (u *UndoStack) Undo(currentRope *Rope, currentCursor Position) (*Rope, Position, bool) {
	if len(u.undo) == 0 {
		return nil, Position{}, false
	}
	// push current state to redo
	u.redo = append(u.redo, undoEntry{rope: currentRope, cursor: currentCursor})
	// pop from undo
	entry := u.undo[len(u.undo)-1]
	u.undo = u.undo[:len(u.undo)-1]
	u.lastWasCharInsert = false
	return entry.rope, entry.cursor, true
}

// Redo returns the next rope and cursor, pushing current state to undo.
func (u *UndoStack) Redo(currentRope *Rope, currentCursor Position) (*Rope, Position, bool) {
	if len(u.redo) == 0 {
		return nil, Position{}, false
	}
	// push current state to undo
	u.undo = append(u.undo, undoEntry{rope: currentRope, cursor: currentCursor})
	// pop from redo
	entry := u.redo[len(u.redo)-1]
	u.redo = u.redo[:len(u.redo)-1]
	u.lastWasCharInsert = false
	return entry.rope, entry.cursor, true
}

// CanUndo returns true if there are snapshots to undo to.
func (u *UndoStack) CanUndo() bool {
	return len(u.undo) > 0
}

// CanRedo returns true if there are snapshots to redo to.
func (u *UndoStack) CanRedo() bool {
	return len(u.redo) > 0
}
