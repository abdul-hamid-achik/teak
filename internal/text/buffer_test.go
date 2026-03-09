package text

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBufferInsertAndString(t *testing.T) {
	b := NewBuffer()
	b.InsertAtCursor([]byte("hello"))
	if got := b.Rope().String(); got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
	if b.Cursor != (Position{0, 5}) {
		t.Errorf("cursor = %v, want {0, 5}", b.Cursor)
	}
}

func TestBufferBackspace(t *testing.T) {
	b := NewBuffer()
	b.InsertAtCursor([]byte("hello"))
	b.Backspace()
	if got := b.Rope().String(); got != "hell" {
		t.Errorf("got %q, want %q", got, "hell")
	}
	if b.Cursor != (Position{0, 4}) {
		t.Errorf("cursor = %v, want {0, 4}", b.Cursor)
	}
}

func TestBufferDelete(t *testing.T) {
	b := NewBuffer()
	b.InsertAtCursor([]byte("hello"))
	b.Cursor = Position{0, 0}
	b.Delete()
	if got := b.Rope().String(); got != "ello" {
		t.Errorf("got %q, want %q", got, "ello")
	}
}

func TestBufferNewline(t *testing.T) {
	b := NewBuffer()
	b.InsertAtCursor([]byte("hello"))
	b.InsertNewline()
	b.InsertAtCursor([]byte("world"))
	if got := b.Rope().String(); got != "hello\nworld" {
		t.Errorf("got %q, want %q", got, "hello\nworld")
	}
	if b.Cursor != (Position{1, 5}) {
		t.Errorf("cursor = %v, want {1, 5}", b.Cursor)
	}
}

func TestBufferUndoRedo(t *testing.T) {
	b := NewBuffer()
	b.InsertAtCursor([]byte("hello"))
	// wait to ensure separate undo groups
	time.Sleep(400 * time.Millisecond)
	b.InsertAtCursor([]byte(" world"))

	if got := b.Rope().String(); got != "hello world" {
		t.Errorf("before undo: got %q, want %q", got, "hello world")
	}

	b.Undo()
	if got := b.Rope().String(); got != "hello" {
		t.Errorf("after first undo: got %q, want %q", got, "hello")
	}

	b.Redo()
	if got := b.Rope().String(); got != "hello world" {
		t.Errorf("after redo: got %q, want %q", got, "hello world")
	}

	b.Undo()
	b.Undo()
	if got := b.Rope().String(); got != "" {
		t.Errorf("after double undo: got %q, want %q", got, "")
	}
}

func TestBufferSelection(t *testing.T) {
	b := NewBuffer()
	b.InsertAtCursor([]byte("hello world"))
	b.SetSelection(Position{0, 0}, Position{0, 5})

	selected := string(b.SelectedText())
	if selected != "hello" {
		t.Errorf("selected = %q, want %q", selected, "hello")
	}

	b.DeleteSelection()
	if got := b.Rope().String(); got != " world" {
		t.Errorf("after delete selection: got %q, want %q", got, " world")
	}
	if b.Selections == nil || b.Selections.Count() == 0 || !b.Selections.Primary().IsEmpty() {
		t.Error("selection should be empty after delete")
	}
}

func TestBufferFileSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	b := NewBuffer()
	b.InsertAtCursor([]byte("hello world"))
	err := b.SaveAs(path)
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if b.Dirty() {
		t.Error("should not be dirty after save")
	}

	b2, err := NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile: %v", err)
	}
	if got := b2.Rope().String(); got != "hello world" {
		t.Errorf("loaded content = %q, want %q", got, "hello world")
	}
	if b2.FilePath != path {
		t.Errorf("FilePath = %q, want %q", b2.FilePath, path)
	}
}

func TestBufferDirtyFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("original"), 0644)

	b, _ := NewBufferFromFile(path)
	if b.Dirty() {
		t.Error("should not be dirty after load")
	}

	b.InsertAtCursor([]byte("X"))
	if !b.Dirty() {
		t.Error("should be dirty after edit")
	}

	b.SaveAs(path)
	if b.Dirty() {
		t.Error("should not be dirty after save")
	}
}

func TestBufferMoveCursor(t *testing.T) {
	b := NewBufferFromBytes([]byte("abc\ndef\nghi"))

	b.Cursor = Position{1, 1}

	b.MoveCursor(DirLeft)
	if b.Cursor != (Position{1, 0}) {
		t.Errorf("after left: %v", b.Cursor)
	}

	b.MoveCursor(DirLeft) // wrap to previous line
	if b.Cursor != (Position{0, 3}) {
		t.Errorf("after left wrap: %v", b.Cursor)
	}

	b.MoveCursor(DirDown)
	if b.Cursor != (Position{1, 3}) {
		t.Errorf("after down: %v", b.Cursor)
	}

	b.MoveCursor(DirRight) // wrap to next line
	if b.Cursor != (Position{2, 0}) {
		t.Errorf("after right wrap: %v", b.Cursor)
	}

	b.MoveCursor(DirUp)
	if b.Cursor != (Position{1, 0}) {
		t.Errorf("after up: %v", b.Cursor)
	}
}
