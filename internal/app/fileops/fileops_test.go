package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := Write(path, data); err != nil {
		t.Fatalf("Write(%q) failed: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := Mkdir(path); err != nil {
		t.Fatalf("Mkdir(%q) failed: %v", path, err)
	}
}

func TestReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Write
	content := []byte("hello world")
	if err := Write(testFile, content); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read
	data, err := Read(testFile)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("got %q, want %q", string(data), string(content))
	}
}

func TestExists(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "exists.txt")

	if Exists(testFile) {
		t.Error("expected file to not exist")
	}

	// Create file
	mustWrite(t, testFile, []byte("test"))

	if !Exists(testFile) {
		t.Error("expected file to exist")
	}
}

func TestIsDir(t *testing.T) {
	tmpDir := t.TempDir()

	if !IsDir(tmpDir) {
		t.Error("expected tmpDir to be a directory")
	}

	testFile := filepath.Join(tmpDir, "file.txt")
	mustWrite(t, testFile, []byte("test"))

	if IsDir(testFile) {
		t.Error("expected file to not be a directory")
	}
}

func TestMkdir(t *testing.T) {
	tmpDir := t.TempDir()
	newDir := filepath.Join(tmpDir, "a", "b", "c")

	if err := Mkdir(newDir); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if !IsDir(newDir) {
		t.Error("expected directory to exist")
	}
}

func TestRename(t *testing.T) {
	tmpDir := t.TempDir()
	oldPath := filepath.Join(tmpDir, "old.txt")
	newPath := filepath.Join(tmpDir, "new.txt")

	mustWrite(t, oldPath, []byte("test"))

	if err := Rename(oldPath, newPath); err != nil {
		t.Fatalf("Rename failed: %v", err)
	}

	if Exists(oldPath) {
		t.Error("old path should not exist")
	}
	if !Exists(newPath) {
		t.Error("new path should exist")
	}
}

func TestDelete(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "delete.txt")

	mustWrite(t, testFile, []byte("test"))

	if err := Delete(testFile); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if Exists(testFile) {
		t.Error("file should not exist after delete")
	}
}

func TestDeleteAll(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "dir")

	mustMkdir(t, testDir)
	mustWrite(t, filepath.Join(testDir, "file.txt"), []byte("test"))

	if err := DeleteAll(testDir); err != nil {
		t.Fatalf("DeleteAll failed: %v", err)
	}

	if Exists(testDir) {
		t.Error("directory should not exist after DeleteAll")
	}
}

func TestCreateFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "subdir", "newfile.txt")

	if err := CreateFile(testFile); err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	if !Exists(testFile) {
		t.Error("file should exist")
	}

	info, err := Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Errorf("expected empty file, got size %d", info.Size())
	}
}

func TestCopy(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst.txt")

	content := []byte("copy test")
	mustWrite(t, src, content)

	if err := Copy(src, dst); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}

	data, err := Read(dst)
	if err != nil {
		t.Fatalf("Read dst failed: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("got %q, want %q", string(data), string(content))
	}
}

func TestListDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Empty dir
	entries, err := ListDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}

	// Add files
	mustWrite(t, filepath.Join(tmpDir, "a.txt"), []byte("a"))
	mustWrite(t, filepath.Join(tmpDir, "b.txt"), []byte("b"))

	entries, err = ListDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestIsEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	if !IsEmptyDir(tmpDir) {
		t.Error("expected empty dir")
	}

	mustWrite(t, filepath.Join(tmpDir, "file.txt"), []byte("test"))

	if IsEmptyDir(tmpDir) {
		t.Error("expected non-empty dir")
	}
}

func TestFileCount(t *testing.T) {
	tmpDir := t.TempDir()

	if count := FileCount(tmpDir); count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	mustWrite(t, filepath.Join(tmpDir, "a.txt"), []byte("a"))
	mustWrite(t, filepath.Join(tmpDir, "b.txt"), []byte("b"))

	if count := FileCount(tmpDir); count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestPathHelpers(t *testing.T) {
	tests := []struct {
		fn   func(string) string
		args []string
		want string
	}{
		{Base, []string{"/foo/bar.txt"}, "bar.txt"},
		{Ext, []string{"/foo/bar.txt"}, ".txt"},
		{Clean, []string{"/foo/./bar/.."}, string(filepath.Separator) + "foo"},
	}

	for _, tt := range tests {
		got := tt.fn(tt.args[0])
		if got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}

	// Test Join separately (variadic)
	if got := Join("/foo", "bar", "baz.txt"); got != "/foo/bar/baz.txt" {
		t.Errorf("Join got %q, want %q", got, "/foo/bar/baz.txt")
	}
}

func TestWalk(t *testing.T) {
	tmpDir := t.TempDir()

	mustWrite(t, filepath.Join(tmpDir, "a.txt"), []byte("a"))
	mustMkdir(t, filepath.Join(tmpDir, "subdir"))
	mustWrite(t, filepath.Join(tmpDir, "subdir", "b.txt"), []byte("b"))

	var paths []string
	err := Walk(tmpDir, func(path string, info os.DirEntry) error {
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(paths) != 3 {
		t.Errorf("expected 3 paths, got %d: %v", len(paths), paths)
	}
}

func TestSave(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "subdir", "saved.txt")

	if err := Save(testFile, []byte("saved content")); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := Read(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "saved content" {
		t.Errorf("got %q, want %q", string(data), "saved content")
	}
}

func TestSaveAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "atomic.txt")

	if err := SaveAtomic(testFile, []byte("atomic content")); err != nil {
		t.Fatalf("SaveAtomic failed: %v", err)
	}

	data, err := Read(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "atomic content" {
		t.Errorf("got %q, want %q", string(data), "atomic content")
	}

	tmpExists := Exists(testFile + ".tmp")
	if tmpExists {
		t.Error("temp file should not exist after SaveAtomic")
	}
}

func TestSaveIfDifferent(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "different.txt")

	mustWrite(t, testFile, []byte("original"))

	saved, err := SaveIfDifferent(testFile, []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if saved {
		t.Error("should not save identical content")
	}

	saved, err = SaveIfDifferent(testFile, []byte("modified"))
	if err != nil {
		t.Fatal(err)
	}
	if !saved {
		t.Error("should save different content")
	}
}

func TestSaveIfDifferentNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "new.txt")

	saved, err := SaveIfDifferent(testFile, []byte("new content"))
	if err != nil {
		t.Fatal(err)
	}
	if !saved {
		t.Error("should save new file")
	}
}

func TestIsRegularFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "file.txt")
	mustWrite(t, testFile, []byte("test"))

	if !IsRegularFile(testFile) {
		t.Error("expected regular file")
	}

	if IsRegularFile(tmpDir) {
		t.Error("directory is not a regular file")
	}

	if IsRegularFile(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("nonexistent path is not a regular file")
	}
}

func TestGetFileSize(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "size.txt")
	mustWrite(t, testFile, []byte("hello"))

	size, err := GetFileSize(testFile)
	if err != nil {
		t.Fatal(err)
	}
	if size != 5 {
		t.Errorf("size = %d, want 5", size)
	}
}

func TestGetFileSizeError(t *testing.T) {
	_, err := GetFileSize(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestIsReadable(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "readable.txt")
	mustWrite(t, testFile, []byte("test"))

	if !IsReadable(testFile) {
		t.Error("file should be readable")
	}

	if IsReadable(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("nonexistent file should not be readable")
	}
}

func TestGetExt(t *testing.T) {
	tests := []struct {
		path, want string
	}{
		{"/foo/bar.txt", ".txt"},
		{"/foo/bar.TXT", ".txt"},
		{"/foo.bar/baz", ""},
		{"noextension", ""},
	}

	for _, tt := range tests {
		got := GetExt(tt.path)
		if got != tt.want {
			t.Errorf("GetExt(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestGetBaseName(t *testing.T) {
	if got := GetBaseName("/foo/bar.txt"); got != "bar.txt" {
		t.Errorf("GetBaseName = %q, want %q", got, "bar.txt")
	}
}

func TestIsHiddenFile(t *testing.T) {
	if !IsHiddenFile(".hidden") {
		t.Error(".hidden should be considered hidden")
	}

	if IsHiddenFile("visible.txt") {
		t.Error("visible.txt should not be hidden")
	}

	if IsHiddenFile("a") {
		t.Error("single char 'a' should not be hidden")
	}
}

func TestNormalizePath(t *testing.T) {
	normalized := NormalizePath(filepath.Join(".", "subdir"))
	if filepath.Base(normalized) != "subdir" {
		t.Errorf("NormalizePath = %q, expected to end with subdir", normalized)
	}
}

func TestValidatePath(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "valid.txt")
	mustWrite(t, testFile, []byte("test"))

	err := ValidatePath(testFile)
	if err != nil {
		t.Errorf("ValidatePath failed: %v", err)
	}

	err = ValidatePath("")
	if err != ErrEmptyPath {
		t.Errorf("ValidatePath('') = %v, want ErrEmptyPath", err)
	}

	err = ValidatePath(filepath.Join(tmpDir, "nonexistent"))
	if err != ErrFileNotExist {
		t.Errorf("ValidatePath(nonexistent) = %v, want ErrFileNotExist", err)
	}

	err = ValidatePath(tmpDir)
	if err != ErrIsDirectory {
		t.Errorf("ValidatePath(dir) = %v, want ErrIsDirectory", err)
	}
}

func TestCanClose(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "closable.txt")
	mustWrite(t, testFile, []byte("test"))

	opts := CloseOptions{Force: true}
	canClose, err := CanClose(testFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !canClose {
		t.Error("Force close should succeed")
	}

	opts = CloseOptions{Force: false, SaveFirst: true}
	canClose, err = CanClose(testFile, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !canClose {
		t.Error("SaveFirst close should succeed for existing file")
	}

	opts = CloseOptions{Force: false, SaveFirst: true}
	canClose, err = CanClose(filepath.Join(tmpDir, "nonexistent"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !canClose {
		t.Error("SaveFirst should succeed for nonexistent file")
	}
}

func TestEnsureClosed(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "test.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if err := EnsureClosed(f); err != nil {
		t.Error("EnsureClosed failed", err)
	}

	if err := EnsureClosed(nil); err != nil {
		t.Error("EnsureClosed(nil) should not fail")
	}
}

func TestCloseIfNotNull(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "test.txt"))
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := CloseIfNotNull(f); err != nil {
		t.Error("CloseIfNotNull failed", err)
	}

	if err := CloseIfNotNull(nil); err != nil {
		t.Error("CloseIfNotNull(nil) should not fail")
	}
}

func TestHasPermission(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "perm.txt")
	mustWrite(t, testFile, []byte("test"))

	if !HasPermission(testFile) {
		t.Error("file should have write permission")
	}

	nonexistent := filepath.Join(tmpDir, "nonexistent")
	if HasPermission(nonexistent) {
		t.Error("nonexistent file should return false")
	}
}

func TestGlob(t *testing.T) {
	tmpDir := t.TempDir()
	mustWrite(t, filepath.Join(tmpDir, "a.txt"), []byte("a"))
	mustWrite(t, filepath.Join(tmpDir, "b.txt"), []byte("b"))
	mustWrite(t, filepath.Join(tmpDir, "c.md"), []byte("c"))

	matches, err := Glob(filepath.Join(tmpDir, "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Errorf("Glob *.txt returned %d matches, want 2", len(matches))
	}
}

func TestAbs(t *testing.T) {
	abs, err := Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(abs) {
		t.Error("Abs should return absolute path")
	}
}

func TestRel(t *testing.T) {
	tmpDir := t.TempDir()
	subdir := filepath.Join(tmpDir, "subdir")

	rel, err := Rel(tmpDir, subdir)
	if err != nil {
		t.Fatal(err)
	}
	if rel != "subdir" {
		t.Errorf("Rel = %q, want %q", rel, "subdir")
	}
}
