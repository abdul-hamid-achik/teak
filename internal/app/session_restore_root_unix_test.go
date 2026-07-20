//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"teak/internal/session"
)

func TestSessionRestorePinnedRootRejectsWorkspaceReplacement(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "safe.go")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldLoad, oldRead := loadSessionForRestore, readSessionRestoreFile
	loadSessionForRestore = func(context.Context) (session.State, error) {
		return session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: path}}}, nil
	}
	var read string
	readSessionRestoreFile = func(ctx context.Context, pinned *os.Root, name string, limit int64) ([]byte, os.FileInfo, error) {
		moved := filepath.Join(parent, "workspace-original")
		if err := os.Rename(root, moved); err != nil {
			return nil, nil, err
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(filepath.Join(root, "safe.go"), []byte("replacement"), 0o600); err != nil {
			return nil, nil, err
		}
		data, info, err := readSessionRestoreFileFromRoot(ctx, pinned, name, limit)
		read = string(data)
		return data, info, err
	}
	defer func() {
		loadSessionForRestore = oldLoad
		readSessionRestoreFile = oldRead
	}()

	msg := sessionRestoreCmd(context.Background(), 9, root)()
	result, ok := msg.(sessionRestoreResultMsg)
	if !ok {
		t.Fatalf("session restore returned %T", msg)
	}
	if !errors.Is(result.Err, errSessionRestoreWorkspaceChanged) {
		t.Fatalf("restore error = %v, want workspace replacement error", result.Err)
	}
	if read != "original" {
		t.Fatalf("pinned root read %q, want original content", read)
	}
	if len(result.State.Tabs) != 0 || len(result.Files) != 0 {
		t.Fatalf("replacement result retained tabs: %+v / %+v", result.State.Tabs, result.Files)
	}
}

func TestSessionRestorePinnedRootRejectsParentSymlinkSwap(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "generated")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(inside, "safe.go")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "safe.go"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldLoad, oldRead := loadSessionForRestore, readSessionRestoreFile
	loadSessionForRestore = func(context.Context) (session.State, error) {
		return session.State{RootDir: root, ActiveTab: 0, Tabs: []session.TabState{{FilePath: path}}}, nil
	}
	var read string
	readSessionRestoreFile = func(ctx context.Context, pinned *os.Root, name string, limit int64) ([]byte, os.FileInfo, error) {
		if err := os.RemoveAll(inside); err != nil {
			return nil, nil, err
		}
		if err := os.Symlink(outside, inside); err != nil {
			return nil, nil, err
		}
		data, info, err := readSessionRestoreFileFromRoot(ctx, pinned, name, limit)
		read = string(data)
		return data, info, err
	}
	defer func() {
		loadSessionForRestore = oldLoad
		readSessionRestoreFile = oldRead
	}()

	msg := sessionRestoreCmd(context.Background(), 10, root)()
	result, ok := msg.(sessionRestoreResultMsg)
	if !ok {
		t.Fatalf("session restore returned %T", msg)
	}
	if result.Err != nil {
		t.Fatalf("restore error = %v", result.Err)
	}
	if read == "outside" {
		t.Fatal("pinned root followed a replacement parent symlink outside the workspace")
	}
	if len(result.State.Tabs) != 0 || len(result.Files) != 0 {
		t.Fatalf("parent-symlink replacement restored a tab: %+v / %+v", result.State.Tabs, result.Files)
	}
}
