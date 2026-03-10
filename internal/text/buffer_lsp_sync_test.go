package text

import "testing"

func TestOperationsClearLastChangeForFullSyncFallback(t *testing.T) {
	tests := []struct {
		name   string
		setup  func() *Buffer
		mutate func(*Buffer)
	}{
		{
			name: "backspace word",
			setup: func() *Buffer {
				b := NewBufferFromBytes([]byte("alpha beta"))
				b.Cursor = Position{Line: 0, Col: len("alpha beta")}
				b.InsertAtCursor([]byte("!"))
				return b
			},
			mutate: func(b *Buffer) {
				b.BackspaceWord()
			},
		},
		{
			name: "toggle line comment",
			setup: func() *Buffer {
				b := NewBufferFromBytes([]byte("alpha\nbeta"))
				b.Cursor = Position{Line: 0, Col: 0}
				b.InsertAtCursor([]byte("!"))
				b.Cursor = Position{Line: 0, Col: 0}
				return b
			},
			mutate: func(b *Buffer) {
				b.ToggleLineComment("//")
			},
		},
		{
			name: "move line up",
			setup: func() *Buffer {
				b := NewBufferFromBytes([]byte("line0\nline1\nline2"))
				b.Cursor = Position{Line: 1, Col: 0}
				b.InsertAtCursor([]byte("!"))
				return b
			},
			mutate: func(b *Buffer) {
				b.Cursor = Position{Line: 1, Col: 0}
				b.MoveLineUp()
			},
		},
		{
			name: "move line down",
			setup: func() *Buffer {
				b := NewBufferFromBytes([]byte("line0\nline1\nline2"))
				b.Cursor = Position{Line: 0, Col: 0}
				b.InsertAtCursor([]byte("!"))
				return b
			},
			mutate: func(b *Buffer) {
				b.Cursor = Position{Line: 0, Col: 0}
				b.MoveLineDown()
			},
		},
		{
			name: "duplicate line down",
			setup: func() *Buffer {
				b := NewBufferFromBytes([]byte("line0\nline1"))
				b.Cursor = Position{Line: 0, Col: 0}
				b.InsertAtCursor([]byte("!"))
				return b
			},
			mutate: func(b *Buffer) {
				b.Cursor = Position{Line: 0, Col: 0}
				b.DuplicateLineDown()
			},
		},
		{
			name: "duplicate line up",
			setup: func() *Buffer {
				b := NewBufferFromBytes([]byte("line0\nline1"))
				b.Cursor = Position{Line: 1, Col: 0}
				b.InsertAtCursor([]byte("!"))
				return b
			},
			mutate: func(b *Buffer) {
				b.Cursor = Position{Line: 1, Col: 0}
				b.DuplicateLineUp()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.setup()
			if b.LastChange() == nil {
				t.Fatal("setup precondition failed: LastChange() should be non-nil")
			}

			tt.mutate(b)

			if b.LastChange() != nil {
				t.Fatalf("LastChange() = %#v, want nil after complex edit", b.LastChange())
			}
		})
	}
}

func TestLoadContentClearsLastChange(t *testing.T) {
	b := NewBufferFromBytes([]byte("hello"))
	b.InsertAtCursor([]byte("!"))
	if b.LastChange() == nil {
		t.Fatal("setup precondition failed: LastChange() should be non-nil")
	}

	b.LoadContent([]byte("reloaded content"))

	if b.LastChange() != nil {
		t.Fatalf("LastChange() = %#v, want nil after LoadContent", b.LastChange())
	}
}
