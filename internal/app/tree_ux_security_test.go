package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
)

func newTreeUXModel(t *testing.T, root string) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	t.Cleanup(m.cleanup)
	return m
}

func completeTreeAction(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("tree action command = nil")
	}
	msg := cmd()
	if _, ok := msg.(treeActionResultMsg); !ok {
		t.Fatalf("tree action result = %T, want treeActionResultMsg", msg)
	}
	updatedAny, _ := model.Update(msg)
	return updatedAny.(Model)
}

func TestTreeNewItemRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"..", ".", "../escape", "dir/file", `dir\\file`, "\x00file"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			m := newTreeUXModel(t, root)
			m.newFileMode = true
			m.newItemDir = root
			m.newItemInput = name

			updatedAny, _ := m.handleNewItemInput(tea.KeyPressMsg{Code: tea.KeyEnter})
			updated := updatedAny.(Model)
			if !strings.Contains(updated.status, "Invalid name") {
				t.Fatalf("status = %q, want invalid-name error", updated.status)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("unsafe item altered workspace: %#v", entries)
			}
		})
	}
}

func TestTreeNewItemDoesNotFollowEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	m := newTreeUXModel(t, root)
	m.newFileMode = true
	m.newItemDir = filepath.Join(root, "linked")
	m.newItemInput = "escape.txt"

	updatedAny, cmd := m.handleNewItemInput(tea.KeyPressMsg{Code: tea.KeyEnter})
	updated := completeTreeAction(t, updatedAny.(Model), cmd)
	if !strings.Contains(updated.status, "Error creating") {
		t.Fatalf("status = %q, want create error", updated.status)
	}
	if _, err := os.Stat(filepath.Join(outside, "escape.txt")); !os.IsNotExist(err) {
		t.Fatalf("created file outside workspace: %v", err)
	}
}

func TestDeleteLastRunePreservesUTF8(t *testing.T) {
	if got := deleteLastRune("café🙂"); got != "café" {
		t.Errorf("deleteLastRune = %q, want %q", got, "café")
	}
}

func TestTreeDeleteDirectoryClosesNestedCleanTabsAfterRemoval(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	if err := os.MkdirAll(filepath.Join(root, "dir", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	addDirtyEditor(t, &m, filepath.Join("dir", "nested", "child.go"), "package p\n", "package p\n")
	addDirtyEditor(t, &m, "other.go", "package p\n", "package p\n")
	m.deleteConfirm = true
	m.deleteTarget = filepath.Join(root, "dir")

	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := completeTreeAction(t, updatedAny.(Model), cmd)
	if _, err := os.Stat(filepath.Join(root, "dir")); !os.IsNotExist(err) {
		t.Fatalf("directory still exists: %v", err)
	}
	if updated.tabBar.FindTab(filepath.Join(root, "dir", "nested", "child.go")) >= 0 {
		t.Fatal("nested deleted file tab remained open")
	}
	if updated.tabBar.FindTab(filepath.Join(root, "other.go")) < 0 {
		t.Fatal("unrelated tab was closed")
	}
}

func TestTreeDeleteRefusesWhenNestedTabIsDirty(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	if err := os.MkdirAll(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	addDirtyEditor(t, &m, filepath.Join("dir", "child.go"), "old\n", "new\n")
	m.deleteConfirm = true
	m.deleteTarget = filepath.Join(root, "dir")

	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := completeTreeAction(t, updatedAny.(Model), cmd)
	if _, err := os.Stat(filepath.Join(root, "dir")); err != nil {
		t.Fatalf("directory was deleted despite dirty tab: %v", err)
	}
	if !strings.Contains(updated.status, "unsaved") {
		t.Fatalf("status = %q, want unsaved warning", updated.status)
	}
}

func TestTreeDeleteDoesNotTraverseEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(outsideFile, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	m := newTreeUXModel(t, root)
	m.deleteConfirm = true
	m.deleteTarget = filepath.Join(root, "linked")

	updatedAny, cmd := m.handleDeleteConfirm(tea.KeyPressMsg{Text: "y"})
	updated := completeTreeAction(t, updatedAny.(Model), cmd)
	if _, err := os.Stat(outsideFile); err != nil {
		t.Fatalf("outside target was changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "linked")); !os.IsNotExist(err) {
		t.Fatalf("workspace symlink was not removed: %v", err)
	}
	if !strings.Contains(updated.status, "Deleted") {
		t.Fatalf("status = %q, want deletion success", updated.status)
	}
}

func TestTreeCopyPathReportsAsyncResult(t *testing.T) {
	root := t.TempDir()
	m := newTreeUXModel(t, root)
	m.treeContextPath = filepath.Join(root, "hello.go")
	updatedAny, cmd := m.handleTreeContextMenuAction("tree_copy_path")
	updated := updatedAny.(Model)
	if updated.status != "" {
		t.Fatalf("copy status changed before command completed: %q", updated.status)
	}
	if cmd == nil {
		t.Fatal("copy path returned no command")
	}
	msg := cmd()
	if _, ok := msg.(treeCopyPathResultMsg); !ok {
		t.Fatalf("copy command message = %T, want treeCopyPathResultMsg", msg)
	}
}
