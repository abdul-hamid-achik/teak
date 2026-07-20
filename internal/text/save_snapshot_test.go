package text

import (
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

func TestWriteRopeAtomicallyRejectsSymlinkDestination(t *testing.T) {
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

	if err := WriteRopeAtomically(link, NewFromString("replacement")); err == nil {
		t.Fatal("WriteRopeAtomically() accepted a symlink destination")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "target"; got != want {
		t.Fatalf("symlink target content = %q, want %q", got, want)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink destination was replaced: info=%v err=%v", info, err)
	}
}
