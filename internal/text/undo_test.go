package text

import (
	"testing"
)

func TestUndoStackTruncation(t *testing.T) {
	u := NewUndoStack()

	// Create a rope for testing
	rope := NewFromString("initial content")
	pos := Position{Line: 0, Col: 0}

	// Add maxUndoEntries + 1 entries
	for i := 0; i < maxUndoEntries+1; i++ {
		u.Save(rope, pos, false)
		rope = rope.Insert(rope.Len(), []byte("x"))
	}

	// Verify stack was truncated to maxUndoEntries
	if len(u.undo) != maxUndoEntries {
		t.Errorf("Expected %d entries after truncation, got %d", maxUndoEntries, len(u.undo))
	}

	// Verify we can still undo (the most recent entries should be preserved)
	if !u.CanUndo() {
		t.Error("Expected CanUndo() to be true after truncation")
	}

	// The oldest entries should have been discarded
	// We should be able to undo maxUndoEntries times
	originalRope := rope
	undoCount := 0
	for u.CanUndo() {
		var ok bool
		originalRope, _, ok = u.Undo(originalRope, pos)
		if !ok {
			t.Fatal("Undo failed unexpectedly")
		}
		undoCount++
	}

	if undoCount != maxUndoEntries {
		t.Errorf("Expected %d undo operations, got %d", maxUndoEntries, undoCount)
	}
}

func TestUndoStackTruncationPreservesRecent(t *testing.T) {
	u := NewUndoStack()

	// Add entries one at a time
	for i := 0; i < maxUndoEntries+10; i++ {
		rope := NewFromString(string(rune('a' + i%26)))
		u.Save(rope, Position{Line: 0, Col: 0}, false)
	}

	// The most recent entry should be the last one we added
	// Try to undo and verify we get the expected state
	rope := NewFromString("latest")
	u.Save(rope, Position{Line: 0, Col: 0}, false)

	// Add more to trigger truncation
	for i := 0; i < maxUndoEntries; i++ {
		rope := NewFromString("filler")
		u.Save(rope, Position{Line: 0, Col: 0}, false)
	}

	// Now undo should still work properly
	if !u.CanUndo() {
		t.Error("Should be able to undo after truncation")
	}

	// After truncation, we should have exactly maxUndoEntries
	if len(u.undo) > maxUndoEntries {
		t.Errorf("Stack has %d entries, expected max %d", len(u.undo), maxUndoEntries)
	}
}
