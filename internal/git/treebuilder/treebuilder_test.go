package treebuilder

import (
	"testing"

	"teak/internal/git"
)

func TestBuildEmpty(t *testing.T) {
	entries := []git.StatusEntry{}
	result := Build(entries, true)

	if len(result) != 0 {
		t.Errorf("Build empty: got %d children, want 0", len(result))
	}
}

func TestBuildSingleFile(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "foo.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Fatalf("Build single file: got %d children, want 1", len(result))
	}

	if result[0].Name != "foo.txt" {
		t.Errorf("Name = %q, want %q", result[0].Name, "foo.txt")
	}
	if result[0].Path != "foo.txt" {
		t.Errorf("Path = %q, want %q", result[0].Path, "foo.txt")
	}
	if result[0].IsDir {
		t.Error("IsDir = true, want false")
	}
	if !result[0].Staged {
		t.Error("Staged = false, want true")
	}
}

func TestBuildNestedFile(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "dir/subdir/file.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Fatalf("Build nested file: got %d top-level children, want 1", len(result))
	}

	dir := result[0]
	if dir.Name != "dir" {
		t.Errorf("dir.Name = %q, want %q", dir.Name, "dir")
	}
	if dir.Path != "dir" {
		t.Errorf("dir.Path = %q, want %q", dir.Path, "dir")
	}
	if !dir.IsDir {
		t.Error("dir.IsDir = false, want true")
	}

	if len(dir.Children) != 1 {
		t.Fatalf("dir.Children count = %d, want 1", len(dir.Children))
	}

	subdir := dir.Children[0]
	if subdir.Name != "subdir" {
		t.Errorf("subdir.Name = %q, want %q", subdir.Name, "subdir")
	}
	if subdir.Path != "dir/subdir" {
		t.Errorf("subdir.Path = %q, want %q", subdir.Path, "dir/subdir")
	}

	if len(subdir.Children) != 1 {
		t.Fatalf("subdir.Children count = %d, want 1", len(subdir.Children))
	}

	file := subdir.Children[0]
	if file.Name != "file.txt" {
		t.Errorf("file.Name = %q, want %q", file.Name, "file.txt")
	}
	if file.Path != "dir/subdir/file.txt" {
		t.Errorf("file.Path = %q, want %q", file.Path, "dir/subdir/file.txt")
	}
	if file.IsDir {
		t.Error("file.IsDir = true, want false")
	}
}

func TestBuildMultipleFilesSameDir(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "a.txt", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "b.txt", IndexStatus: 'A', WorkStatus: ' '},
		{Path: "c.txt", IndexStatus: 'D', WorkStatus: ' '},
	}
	result := Build(entries, true)

	if len(result) != 3 {
		t.Fatalf("got %d children, want 3", len(result))
	}

	names := make(map[string]bool)
	for _, n := range result {
		names[n.Name] = true
	}

	if !names["a.txt"] {
		t.Error("missing a.txt")
	}
	if !names["b.txt"] {
		t.Error("missing b.txt")
	}
	if !names["c.txt"] {
		t.Error("missing c.txt")
	}
}

func TestBuildDirectoryEntry(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "mydir/", IndexStatus: ' ', WorkStatus: ' ', IsDir: true},
		{Path: "mydir/file.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Fatalf("got %d children, want 1", len(result))
	}

	dir := result[0]
	if dir.Name != "mydir" {
		t.Errorf("Name = %q, want %q", dir.Name, "mydir")
	}
	if !dir.IsDir {
		t.Error("IsDir = false, want true")
	}
	if dir.Entry == nil {
		t.Error("Entry = nil, want non-nil for directory")
	}
}

func TestBuildStagedVsUnstaged(t *testing.T) {
	stagedEntries := []git.StatusEntry{
		{Path: "staged.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	unstagedEntries := []git.StatusEntry{
		{Path: "unstaged.txt", IndexStatus: ' ', WorkStatus: 'M'},
	}

	stagedResult := Build(stagedEntries, true)
	if len(stagedResult) != 1 {
		t.Fatal("staged result empty")
	}
	if !stagedResult[0].Staged {
		t.Error("Staged = false, want true")
	}

	unstagedResult := Build(unstagedEntries, false)
	if len(unstagedResult) != 1 {
		t.Fatal("unstaged result empty")
	}
	if unstagedResult[0].Staged {
		t.Error("Staged = true, want false")
	}
}

func TestBuildEmptyPath(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "valid.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Errorf("got %d children, want 1 (empty path should be skipped)", len(result))
	}
}

func TestBuildDepthCalculation(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "a/b/c/deep.txt", IndexStatus: 'M', WorkStatus: ' '},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Fatalf("got %d top-level children", len(result))
	}

	a := result[0]
	if a.Depth != 0 {
		t.Errorf("a.Depth = %d, want 0", a.Depth)
	}

	b := a.Children[0]
	if b.Depth != 1 {
		t.Errorf("b.Depth = %d, want 1", b.Depth)
	}

	c := b.Children[0]
	if c.Depth != 2 {
		t.Errorf("c.Depth = %d, want 2", c.Depth)
	}

	deep := c.Children[0]
	if deep.Depth != 3 {
		t.Errorf("deep.Depth = %d, want 3", deep.Depth)
	}
}

func TestBuildPathWithTrailingSlash(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "dir/", IndexStatus: ' ', WorkStatus: ' ', IsDir: true},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Fatalf("got %d children, want 1", len(result))
	}

	if result[0].Path != "dir" {
		t.Errorf("Path = %q, want %q (trailing slash should be trimmed)", result[0].Path, "dir")
	}
}

func TestBuildReuseExistingDir(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "dir/a.txt", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "dir/b.txt", IndexStatus: 'A', WorkStatus: ' '},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Fatalf("got %d top-level children, want 1", len(result))
	}

	dir := result[0]
	if len(dir.Children) != 2 {
		t.Errorf("dir has %d children, want 2", len(dir.Children))
	}
}

func TestBuildUntrackedFile(t *testing.T) {
	entries := []git.StatusEntry{
		{Path: "newfile.txt", IndexStatus: '?', WorkStatus: '?'},
	}
	result := Build(entries, true)

	if len(result) != 1 {
		t.Fatalf("got %d children, want 1", len(result))
	}

	if !result[0].Entry.IsUntracked() {
		t.Error("IsUntracked = false, want true")
	}
}
