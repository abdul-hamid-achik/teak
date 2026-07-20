package text

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewBufferFromFileRejectsOversizedSparseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sparse.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxBufferFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := NewBufferFromFile(path); !errors.Is(err, ErrBufferFileTooLarge) {
		t.Fatalf("NewBufferFromFile() error = %v, want ErrBufferFileTooLarge", err)
	}
}

func TestNewBufferFromFileRejectsNonRegularInput(t *testing.T) {
	if _, err := NewBufferFromFile(t.TempDir()); !errors.Is(err, ErrBufferFileNotRegular) {
		t.Fatalf("NewBufferFromFile(directory) error = %v, want ErrBufferFileNotRegular", err)
	}
}

func TestNewBufferFromFileFollowsSymlinkToRegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("symlink content"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	path := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	buffer, err := NewBufferFromFile(path)
	if err != nil {
		t.Fatalf("NewBufferFromFile(symlink): %v", err)
	}
	if got, want := buffer.Rope().String(), "symlink content"; got != want {
		t.Fatalf("loaded content = %q, want %q", got, want)
	}
}
