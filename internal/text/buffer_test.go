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

func TestBufferReplaceRopeSnapshotIsUndoable(t *testing.T) {
	b := NewBufferFromBytes([]byte("before"))
	original := b.Rope()
	next := NewFromString("after\ntext")

	b.ReplaceRopeSnapshot(next, Position{Line: 1, Col: 4})
	if got := b.Content(); got != "after\ntext" {
		t.Fatalf("content = %q, want replacement", got)
	}
	if got := b.Cursor; got != (Position{Line: 1, Col: 4}) {
		t.Fatalf("cursor = %#v, want replacement cursor", got)
	}
	if !b.Dirty() || b.LastChange() != nil {
		t.Fatal("snapshot replacement must be dirty and require full LSP sync")
	}

	b.Undo()
	if b.Rope() != original || b.Content() != "before" {
		t.Fatalf("undo = %q, want original rope", b.Content())
	}
}

func TestBufferLoadRopeSnapshotSharesPreparedDocumentAndResetsState(t *testing.T) {
	b := NewBufferFromBytes([]byte("before"))
	b.InsertAtCursor([]byte("dirty "))
	b.SetSelection(Position{}, Position{Col: 3})
	prepared := NewFromString("after\nsnapshot")
	beforeVersion := b.Version()

	b.LoadRopeSnapshot(prepared)

	if b.Rope() != prepared {
		t.Fatal("LoadRopeSnapshot materialized the prepared immutable rope")
	}
	if b.Dirty() {
		t.Fatal("LoadRopeSnapshot left a freshly loaded document dirty")
	}
	if b.Cursor != (Position{}) || b.Selections.Count() != 1 || !b.Selections.Primary().IsEmpty() {
		t.Fatalf("selection state was not reset: cursor=%+v selections=%+v", b.Cursor, b.Selections.All())
	}
	if got, want := b.Version(), beforeVersion+1; got != want {
		t.Fatalf("version = %d, want %d", got, want)
	}
	if b.LastChange() != nil {
		t.Fatalf("LastChange() = %#v, want nil full-sync load", b.LastChange())
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

func TestBufferClearSelectionCollapsesPrimaryAtCursor(t *testing.T) {
	tests := []struct {
		name   string
		anchor Position
		head   Position
	}{
		{
			name:   "forward selection",
			anchor: Position{Line: 0, Col: 1},
			head:   Position{Line: 0, Col: 4},
		},
		{
			name:   "backward multiline selection",
			anchor: Position{Line: 1, Col: 2},
			head:   Position{Line: 0, Col: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBufferFromBytes([]byte("hello\nworld"))
			b.SetSelection(tt.anchor, tt.head)

			b.ClearSelection()

			if got := b.Selections.Count(); got != 1 {
				t.Fatalf("selection count = %d, want 1", got)
			}
			if got, want := b.Selections.Primary(), (Selection{Anchor: tt.head, Head: tt.head}); got != want {
				t.Errorf("primary selection = %#v, want %#v", got, want)
			}
			if got := b.Cursor; got != tt.head {
				t.Errorf("cursor = %#v, want %#v", got, tt.head)
			}
		})
	}
}

func TestBufferClearSelectionInitializesMissingSelections(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello"))
	b.Cursor = Position{Line: 0, Col: 3}
	b.Selections = nil

	b.ClearSelection()

	if b.Selections == nil {
		t.Fatal("Selections = nil, want a primary cursor selection")
	}
	if got, want := b.Selections.Primary(), (Selection{
		Anchor: b.Cursor,
		Head:   b.Cursor,
	}); got != want {
		t.Errorf("primary selection = %#v, want %#v", got, want)
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

func TestBufferLoadContentPreservesTabBytes(t *testing.T) {
	b := NewBuffer()
	data := []byte("a\tb\n\t你\n")

	b.LoadContentWithTabSize(data, 8)

	if got := b.Bytes(); string(got) != string(data) {
		t.Fatalf("Bytes() = %q, want original tab bytes %q", got, data)
	}
	if b.Dirty() {
		t.Fatal("loaded buffer is dirty")
	}

	path := filepath.Join(t.TempDir(), "tabs.txt")
	if err := b.SaveAs(path); err != nil {
		t.Fatalf("SaveAs() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("saved bytes = %q, want original tab bytes %q", got, data)
	}
}

func TestBufferDirtyFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	b, _ := NewBufferFromFile(path)
	if b.Dirty() {
		t.Error("should not be dirty after load")
	}

	b.InsertAtCursor([]byte("X"))
	if !b.Dirty() {
		t.Error("should be dirty after edit")
	}

	if err := b.SaveAs(path); err != nil {
		t.Fatalf("SaveAs() error = %v", err)
	}
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
