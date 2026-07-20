package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/lsp"
)

func TestWorkspaceTextEditRejectsPathOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.txt")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := prepareWorkspaceEdit(context.Background(), root, nil, lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{lsp.FileURI(outside): {{
		StartLine: 0,
		StartCol:  0,
		EndLine:   0,
		EndCol:    4,
		NewText:   "pwned",
	}}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("prepareWorkspaceEdit() error = %v, want outside workspace", err)
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "safe" {
		t.Fatalf("outside file changed to %q", data)
	}
}

func TestWorkspaceFileOperationsRejectPathsOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside.txt")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := prepareWorkspaceEdit(context.Background(), root, nil, lsp.WorkspaceEdit{DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &lsp.WorkspaceFileOperation{
		Kind: lsp.FileOpDelete,
		URI:  lsp.FileURI(outside),
	}}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("prepareWorkspaceEdit() error = %v, want outside workspace", err)
	}
	if _, statErr := os.Stat(outside); statErr != nil {
		t.Fatalf("outside file was removed: %v", statErr)
	}
}

func TestWorkspaceEditRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	outsideDir := filepath.Join(base, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outside, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := prepareWorkspaceEdit(context.Background(), root, nil, lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(filepath.Join(root, "escape", "secret.txt")): {{NewText: "pwned"}},
	}}, nil)
	if err == nil {
		t.Fatal("prepareWorkspaceEdit() accepted a symlink escape")
	}
	data, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "safe" {
		t.Fatalf("symlink target changed to %q", data)
	}
}

func TestWorkspaceEditRejectsOversizedClosedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxEditorFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = prepareWorkspaceEdit(context.Background(), root, nil, lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{lsp.FileURI(path): {{
		StartLine: 0,
		StartCol:  0,
		EndLine:   0,
		EndCol:    0,
		NewText:   "unexpected",
	}}}}, nil)
	if !errors.Is(err, errEditorFileTooLarge) {
		t.Fatalf("prepareWorkspaceEdit() error = %v, want %v", err, errEditorFileTooLarge)
	}
}
