package text

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteRopeAtomicallyUsesUniqueTempAndPreservesPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "document.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	legacyTemp := path + ".tmp"
	if err := os.WriteFile(legacyTemp, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteRopeAtomically(path, NewFromString("after")); err != nil {
		t.Fatalf("WriteRopeAtomically() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "after"; got != want {
		t.Fatalf("saved data = %q, want %q", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("permissions = %o, want %o", got, want)
	}
	data, err = os.ReadFile(legacyTemp)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "keep me"; got != want {
		t.Fatalf("legacy temp was reused: got %q, want %q", got, want)
	}
}

func TestMarkSavedSnapshotKeepsLaterEditsDirty(t *testing.T) {
	buf := NewBufferFromBytes([]byte("before"))
	buf.SelectAll()
	buf.InsertAtCursor([]byte("first"))
	snapshot := buf.Rope()
	buf.SelectAll()
	buf.InsertAtCursor([]byte("second"))

	buf.MarkSavedSnapshot("document.txt", snapshot)

	if !buf.Dirty() {
		t.Fatal("later edits must remain dirty")
	}
	if got, want := buf.FilePath, "document.txt"; got != want {
		t.Fatalf("FilePath = %q, want %q", got, want)
	}
}

func TestSavedRopeTracksTheLastConfirmedDiskSnapshot(t *testing.T) {
	buf := NewBufferFromBytes([]byte("disk"))
	baseline := buf.SavedRope()
	if baseline == nil || baseline.String() != "disk" {
		t.Fatalf("initial SavedRope() = %v, want disk baseline", baseline)
	}
	buf.InsertAtCursor([]byte("local"))
	if buf.SavedRope() != baseline {
		t.Fatal("editing changed the confirmed disk snapshot")
	}
	snapshot := buf.Rope()
	buf.MarkSavedSnapshot("main.go", snapshot)
	if buf.SavedRope() != snapshot {
		t.Fatal("MarkSavedSnapshot did not advance the confirmed disk snapshot")
	}
}

func TestWriteRopeAtomicallyCreatesPrivateFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission assertion")
	}

	path := filepath.Join(t.TempDir(), "new.txt")
	if err := WriteRopeAtomically(path, NewFromString("secret")); err != nil {
		t.Fatalf("WriteRopeAtomically() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("new file permissions = %o, want %o", got, want)
	}
}

// Opening already follows symlinks (see
// TestNewBufferFromFileFollowsSymlinkToRegularFile), so saving must too;
// otherwise a symlinked dotfile can be edited but never written back.
func TestWriteRopeAtomicallyWritesThroughSymlinkToRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteRopeAtomically(link, NewFromString("replacement")); err != nil {
		t.Fatalf("WriteRopeAtomically(symlink) error = %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "replacement"; got != want {
		t.Errorf("target content = %q, want %q", got, want)
	}
	// The link itself must survive: replacing it with a regular file would
	// silently detach the user's dotfile from whatever manages it.
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink was replaced by a regular file: info=%v err=%v", info, err)
	}
}

func TestWriteRopeAtomicallyRejectsSymlinkToNonRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}

	dir := t.TempDir()
	link := filepath.Join(dir, "link-to-dir")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}

	// Following a link is only safe when it lands on a regular file.
	if err := WriteRopeAtomically(link, NewFromString("x")); err == nil {
		t.Error("WriteRopeAtomically() followed a symlink pointing at a directory")
	}
}

// An atomic rename only needs write permission on the containing directory, so
// without an explicit check a file the user deliberately marked read-only is
// overwritten silently.
func TestWriteRopeAtomicallyRefusesReadOnlyDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "protected.txt")
	if err := os.WriteFile(path, []byte("original"), 0o444); err != nil {
		t.Fatal(err)
	}

	err := WriteRopeAtomically(path, NewFromString("overwritten"))
	if err == nil {
		t.Fatal("WriteRopeAtomically() overwrote a read-only file")
	}
	if !errors.Is(err, ErrDestinationReadOnly) {
		t.Errorf("error = %v, want it to wrap ErrDestinationReadOnly so callers can offer to override", err)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got, want := string(data), "original"; got != want {
		t.Errorf("content = %q, want %q — the file must be untouched", got, want)
	}
}

func TestWriteRopeAtomicallyAcceptsWritableDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "writable.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteRopeAtomically(path, NewFromString("updated")); err != nil {
		t.Fatalf("WriteRopeAtomically() error = %v", err)
	}
	data, _ := os.ReadFile(path)
	if got, want := string(data), "updated"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}
