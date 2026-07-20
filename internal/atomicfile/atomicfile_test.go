package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesExistingFileWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, func(file *os.File) error {
		_, err := file.WriteString("new")
		return err
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "new" {
		t.Errorf("saved data = %q, want new", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file permissions = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("directory permissions = %o, want 700", got)
	}
}

func TestWritePreservesExistingFileWhenWriterFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Write(path, func(*os.File) error { return errors.New("injected write failure") })
	if err == nil {
		t.Fatal("Write() succeeded after writer failure")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(data); got != "old" {
		t.Errorf("existing file changed after failed write: %q", got)
	}
}

func TestWriteRejectsSymlinkDestinationWithoutTouchingTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("symlink support varies by test environment")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "state.json")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := Write(link, func(file *os.File) error {
		_, err := file.WriteString("new")
		return err
	})
	if err == nil {
		t.Fatal("Write() accepted a symlink destination")
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(data); got != "target" {
		t.Errorf("symlink target changed: %q", got)
	}
}
