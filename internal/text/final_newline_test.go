package text

import "testing"

func TestEnsureFinalNewlineInsertsOnce(t *testing.T) {
	buf := NewBufferFromBytes([]byte("hello"))
	if !buf.EnsureFinalNewline() {
		t.Fatal("expected an insert")
	}
	if got := buf.Content(); got != "hello\n" {
		t.Fatalf("content = %q", got)
	}
	if buf.EnsureFinalNewline() {
		t.Fatal("second call should be a no-op")
	}
	if got := buf.Content(); got != "hello\n" {
		t.Fatalf("content after no-op = %q", got)
	}
}

func TestEnsureFinalNewlineEmptyFile(t *testing.T) {
	buf := NewBufferFromBytes(nil)
	if !buf.EnsureFinalNewline() {
		t.Fatal("empty file should gain a newline")
	}
	if got := buf.Content(); got != "\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestEnsureFinalNewlineKeepsCursor(t *testing.T) {
	buf := NewBufferFromBytes([]byte("ab"))
	buf.SetCursor(Position{Line: 0, Col: 1})
	buf.EnsureFinalNewline()
	if got := buf.Cursor; got != (Position{Line: 0, Col: 1}) {
		t.Fatalf("cursor = %+v", got)
	}
}
