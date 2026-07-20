package text

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveAsAtomic(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	// Create buffer with content
	buf := NewBufferFromBytes([]byte("hello world"))

	// Save
	if err := buf.SaveAs(path); err != nil {
		t.Fatalf("SaveAs failed: %v", err)
	}

	// Verify file exists and has correct content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("Content mismatch: got %q, want %q", string(data), "hello world")
	}

	// Verify no temp file left behind
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("Expected 1 file, got %d: %v", len(entries), entries)
	}
}

func TestSaveAsOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	// Create existing file
	if err := os.WriteFile(path, []byte("old content"), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	// Overwrite with buffer
	buf := NewBufferFromBytes([]byte("new content"))
	if err := buf.SaveAs(path); err != nil {
		t.Fatalf("SaveAs failed: %v", err)
	}

	// Verify new content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("Content mismatch: got %q, want %q", string(data), "new content")
	}
}

func TestSaveAsUsesUniqueTempAndPreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission assertion")
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyTemp := path + ".tmp"
	if err := os.WriteFile(legacyTemp, []byte("do not reuse"), 0o600); err != nil {
		t.Fatal(err)
	}

	buf := NewBufferFromBytes([]byte("new"))
	if err := buf.SaveAs(path); err != nil {
		t.Fatalf("SaveAs() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("saved permissions = %o, want %o", got, want)
	}
	data, err := os.ReadFile(legacyTemp)
	if err != nil {
		t.Fatalf("legacy temp was removed or reused: %v", err)
	}
	if got, want := string(data), "do not reuse"; got != want {
		t.Fatalf("legacy temp content = %q, want %q", got, want)
	}
}

func TestSaveUpdatesState(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	buf := NewBufferFromBytes([]byte("content"))
	// Make a change to make it dirty
	buf.InsertAtCursor([]byte("!"))

	// Before save
	if buf.FilePath != "" {
		t.Error("Expected empty FilePath before save")
	}
	if !buf.Dirty() {
		t.Error("Expected Dirty() to be true before save (after modification)")
	}

	// Save
	if err := buf.SaveAs(path); err != nil {
		t.Fatalf("SaveAs failed: %v", err)
	}

	// After save
	if buf.FilePath != path {
		t.Errorf("FilePath = %q, want %q", buf.FilePath, path)
	}
	if buf.Dirty() {
		t.Error("Expected Dirty() to be false after save")
	}
}

func TestSaveWithEmptyPath(t *testing.T) {
	buf := NewBufferFromBytes([]byte("content"))

	// Save without setting FilePath returns nil (no-op) - this is the original behavior
	err := buf.Save()
	if err != nil {
		t.Errorf("Save() with empty FilePath should return nil (no-op), got error: %v", err)
	}
}

func TestSaveLargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "large.txt")

	// Create 1MB content
	content := make([]byte, 1024*1024)
	for i := range content {
		content[i] = byte('a' + i%26)
	}

	buf := NewBufferFromBytes(content)

	if err := buf.SaveAs(path); err != nil {
		t.Fatalf("SaveAs failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if len(data) != len(content) {
		t.Errorf("Size mismatch: got %d, want %d", len(data), len(content))
	}
}
