package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileFromDiskConfinesPathAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside.txt")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(inside, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFileFromDisk(context.Background(), root, inside, nil, nil)
	if err != nil || got != "inside" {
		t.Fatalf("ReadFileFromDisk() = %q, %v", got, err)
	}
	if _, err := ReadFileFromDisk(context.Background(), root, outside, nil, nil); err == nil {
		t.Fatal("ReadFileFromDisk() accepted a path outside the workspace")
	}
	link := filepath.Join(root, "outside-link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ReadFileFromDisk(context.Background(), root, link, nil, nil); err == nil {
		t.Fatal("ReadFileFromDisk() followed a symlink outside the workspace")
	}
	insideLink := filepath.Join(root, "inside-link.txt")
	if err := os.Symlink(inside, insideLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ReadFileFromDisk(context.Background(), root, insideLink, nil, nil); err == nil {
		t.Fatal("ReadFileFromDisk() accepted a symlinked workspace file")
	}
}

func TestReadFileFromDiskHonorsLimitsAndCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lines.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	line, limit := 2, 1
	got, err := ReadFileFromDisk(context.Background(), root, path, &line, &limit)
	if err != nil || got != "two" {
		t.Fatalf("ReadFileFromDisk() = %q, %v; want two", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadFileFromDisk(ctx, root, path, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadFileFromDisk() error = %v, want context.Canceled", err)
	}
	invalidLimit := -1
	if _, err := ReadFileFromDisk(context.Background(), root, path, nil, &invalidLimit); err == nil {
		t.Fatal("ReadFileFromDisk() accepted a negative line limit")
	}
}

func TestReadFileFromDiskBoundsLargeFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxACPReadFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFileFromDisk(context.Background(), root, path, nil, nil); err == nil {
		t.Fatal("ReadFileFromDisk() accepted an oversized file")
	}
}

func TestReadWorkspaceFilePinnedRootRejectsSwapToOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := readWorkspaceFileWithBeforeOpen(context.Background(), root, target, maxACPReadFileBytes, func() {
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	})
	if err == nil {
		t.Fatal("pinned workspace read accepted a post-validation symlink to outside")
	}
	if strings.Contains(err.Error(), "outside-secret") {
		t.Fatalf("pinned workspace read exposed outside content in error: %v", err)
	}
}

func TestReadWorkspaceFilePinnedRootRejectsSwapToInsideSymlink(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "secret.txt")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(secret, []byte("inside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := readWorkspaceFileWithBeforeOpen(context.Background(), root, target, maxACPReadFileBytes, func() {
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("secret.txt", target); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	})
	if err == nil {
		t.Fatal("pinned workspace read accepted a post-validation internal symlink")
	}
}

func TestReadWorkspaceFilePinnedRootRejectsRegularFileReplacement(t *testing.T) {
	root := t.TempDir()
	replacement := filepath.Join(root, "replacement.txt")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(replacement, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := readWorkspaceFileWithBeforeOpen(context.Background(), root, target, maxACPReadFileBytes, func() {
		if err := os.Rename(replacement, target); err != nil {
			t.Fatal(err)
		}
	})
	if err == nil {
		t.Fatal("pinned workspace read accepted a replacement regular file")
	}
}

func TestReadWorkspaceFilePinnedRootRejectsParentSwapToOutsideSymlink(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	outsideDir := t.TempDir()
	target := filepath.Join(parent, "target.txt")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideDir, "target.txt"), []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := readWorkspaceFileWithBeforeOpen(context.Background(), root, target, maxACPReadFileBytes, func() {
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(parent); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outsideDir, parent); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	})
	if err == nil {
		t.Fatal("pinned workspace read accepted an outside parent symlink after validation")
	}
}

func TestReadWorkspaceFilePinnedRootRejectsParentSwapToInsideSymlink(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	secretParent := filepath.Join(root, "secret-parent")
	target := filepath.Join(parent, "target.txt")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secretParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretParent, "target.txt"), []byte("inside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := readWorkspaceFileWithBeforeOpen(context.Background(), root, target, maxACPReadFileBytes, func() {
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(parent); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("secret-parent", parent); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	})
	if err == nil {
		t.Fatal("pinned workspace read accepted an internal parent symlink after validation")
	}
}

func TestReadFileFromDiskRejectsNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFileFromDisk(context.Background(), root, directory, nil, nil); err == nil {
		t.Fatal("ReadFileFromDisk() accepted a directory")
	}
}

func TestBuildTaggedFileBlocksRejectsUnsafeAndBoundsTotal(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocks, err := buildTaggedFileBlocks(context.Background(), root, []TaggedFile{{Path: first}, {Path: second}})
	if err != nil || len(blocks) != 2 {
		t.Fatalf("buildTaggedFileBlocks() = %d blocks, %v", len(blocks), err)
	}
	if _, err := buildTaggedFileBlocks(context.Background(), root, []TaggedFile{{Path: filepath.Join(root, "..", "outside")}}); err == nil {
		t.Fatal("buildTaggedFileBlocks() accepted an outside path")
	}
	tooMany := make([]TaggedFile, maxTaggedFiles+1)
	if _, err := buildTaggedFileBlocks(context.Background(), root, tooMany); err == nil {
		t.Fatal("buildTaggedFileBlocks() accepted too many files")
	}
}
