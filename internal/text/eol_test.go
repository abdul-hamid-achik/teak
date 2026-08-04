package text

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeLineEndings(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantEOL LineEnding
	}{
		{"lf unchanged", []byte("a\nb\n"), []byte("a\nb\n"), LF},
		{"crlf normalized", []byte("a\r\nb\r\n"), []byte("a\nb\n"), CRLF},
		{"mixed dominated by crlf", []byte("a\r\nb\nc\r\n"), []byte("a\nb\nc\n"), CRLF},
		{"lone cr preserved", []byte("a\rb\n"), []byte("a\rb\n"), LF},
		{"empty", []byte(""), []byte(""), LF},
		{"trailing cr without lf preserved", []byte("abc\r"), []byte("abc\r"), LF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, eol := NormalizeLineEndings(tt.input)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("NormalizeLineEndings() = %q, want %q", got, tt.want)
			}
			if eol != tt.wantEOL {
				t.Errorf("eol = %v, want %v", eol, tt.wantEOL)
			}
		})
	}
}

func TestNewBufferFromFileNormalizesCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crlf.txt")
	if err := os.WriteFile(path, []byte("line1\r\nline2\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b, err := NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile: %v", err)
	}
	if b.Content() != "line1\nline2\n" {
		t.Fatalf("content = %q, want CR stripped", b.Content())
	}
	if b.LineEnding() != CRLF {
		t.Errorf("LineEnding() = %v, want CRLF", b.LineEnding())
	}
}

func TestSaveRestoresCRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")
	if err := os.WriteFile(path, []byte("line1\r\nline2\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b, err := NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile: %v", err)
	}

	// Edit in the middle of a line: with normalized content there is no CR to
	// orphan, and the save must round-trip Windows line endings.
	b.SetCursor(Position{Line: 0, Col: 5})
	b.InsertAtCursor([]byte("X"))
	if err := b.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "line1X\r\nline2\r\n" {
		t.Fatalf("saved bytes = %q, want CRLF preserved", data)
	}
}

func TestSaveKeepsLFForLFFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lf.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b, err := NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile: %v", err)
	}
	if err := b.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "line1\nline2\n" {
		t.Fatalf("saved bytes = %q, want LF unchanged", data)
	}
}

func TestBackspaceAtLineStartOnNormalizedCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")
	if err := os.WriteFile(path, []byte("foo\r\nbar\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b, err := NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile: %v", err)
	}

	// Backspace at the start of line 1 joins the lines by deleting exactly the
	// newline — no orphan CR may survive in the buffer or on disk.
	b.SetCursor(Position{Line: 1, Col: 0})
	b.Backspace()
	if b.Content() != "foobar\n" {
		t.Fatalf("content = %q, want %q", b.Content(), "foobar\n")
	}
	if err := b.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "foobar\r\n" {
		t.Fatalf("saved bytes = %q, want joined line with CRLF ending", data)
	}
}

func TestInsertAtLineEndOnNormalizedCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crlf.txt")
	if err := os.WriteFile(path, []byte("foo\r\nbar\r\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	b, err := NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile: %v", err)
	}

	// Typing at the end of a line must land before the newline, not between a
	// CR and LF pair.
	b.SetCursor(Position{Line: 0, Col: 3})
	b.InsertAtCursor([]byte("X"))
	if b.Content() != "fooX\nbar\n" {
		t.Fatalf("content = %q, want %q", b.Content(), "fooX\nbar\n")
	}
	if err := b.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "fooX\r\nbar\r\n" {
		t.Fatalf("saved bytes = %q, want CRLF preserved", data)
	}
}

func TestLoadContentNormalizesCRLF(t *testing.T) {
	b := NewBuffer()
	b.LoadContent([]byte("a\r\nb\r\n"))
	if b.Content() != "a\nb\n" {
		t.Fatalf("content = %q, want CR stripped", b.Content())
	}
	if b.LineEnding() != CRLF {
		t.Errorf("LineEnding() = %v, want CRLF", b.LineEnding())
	}
}
