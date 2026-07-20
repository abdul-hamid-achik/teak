package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestWalkProjectFilesContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files, err := walkProjectFilesContext(ctx, t.TempDir(), maxQuickOpenFiles)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("walkProjectFilesContext() error = %v, want context.Canceled", err)
	}
	if files != nil {
		t.Fatalf("walkProjectFilesContext() files = %v, want nil", files)
	}
}

func TestWalkProjectFilesContextCapsAndSortsResults(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.go", "a.go", "m.go"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	files, err := walkProjectFilesContext(context.Background(), root, 2)
	if !errors.Is(err, errQuickOpenLimit) {
		t.Fatalf("walkProjectFilesContext() error = %v, want limit error", err)
	}
	if !reflect.DeepEqual(files, []string{"a.go", "m.go"}) {
		t.Fatalf("walkProjectFilesContext() files = %v, want sorted capped list", files)
	}
}

func TestWalkProjectFilesContextIgnoresUnsafeGitignore(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside-patterns")
	if err := os.WriteFile(target, []byte("*.go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ignorePath := filepath.Join(root, ".gitignore")
	if err := os.Symlink(target, ignorePath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	files, err := walkProjectFilesContext(context.Background(), root, maxQuickOpenFiles)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, []string{"main.go", "outside-patterns"}) {
		t.Fatalf("symlinked .gitignore files = %v, want regular files included", files)
	}

	if err := os.Remove(ignorePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignorePath, []byte(strings.Repeat("*.go\n", 300_000)), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err = walkProjectFilesContext(context.Background(), root, maxQuickOpenFiles)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, []string{"main.go", "outside-patterns"}) {
		t.Fatalf("oversized .gitignore files = %v, want regular files included", files)
	}
}

func TestWalkProjectFilesContextReadsBatchesAndCapsEntries(t *testing.T) {
	root := t.TempDir()
	previous := quickOpenReadDirBatches
	t.Cleanup(func() { quickOpenReadDirBatches = previous })

	entry := quickOpenTestDirEntry{name: ".generated"}
	maxRequested, batches := 0, 0
	quickOpenReadDirBatches = func(ctx context.Context, path string, batchSize int, visit func([]os.DirEntry) bool) error {
		if path != root {
			t.Fatalf("batch path = %q, want %q", path, root)
		}
		maxRequested = max(maxRequested, batchSize)
		for delivered := 0; delivered < maxQuickOpenEntries+1; delivered += batchSize {
			batches++
			count := min(batchSize, maxQuickOpenEntries+1-delivered)
			entries := make([]os.DirEntry, count)
			for i := range entries {
				entries[i] = entry
			}
			if !visit(entries) {
				return nil
			}
		}
		return nil
	}

	files, err := walkProjectFilesContext(context.Background(), root, maxQuickOpenFiles)
	if !errors.Is(err, errQuickOpenLimit) {
		t.Fatalf("walkProjectFilesContext() error = %v, want entry limit", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %v, want hidden entries skipped", files)
	}
	if maxRequested != quickOpenBatchSize || batches < 2 {
		t.Fatalf("batch size/calls = %d/%d, want %d/multiple", maxRequested, batches, quickOpenBatchSize)
	}
}

func TestWalkProjectFilesContextCancelsActiveBatchRead(t *testing.T) {
	root := t.TempDir()
	previous := quickOpenReadDirBatches
	t.Cleanup(func() { quickOpenReadDirBatches = previous })
	entered := make(chan struct{})
	quickOpenReadDirBatches = func(ctx context.Context, _ string, _ int, _ func([]os.DirEntry) bool) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := walkProjectFilesContext(ctx, root, maxQuickOpenFiles)
		result <- err
	}()
	<-entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("walkProjectFilesContext() error = %v, want context.Canceled", err)
	}
}

type quickOpenTestDirEntry struct{ name string }

func (e quickOpenTestDirEntry) Name() string             { return e.name }
func (quickOpenTestDirEntry) IsDir() bool                { return false }
func (quickOpenTestDirEntry) Type() os.FileMode          { return 0 }
func (quickOpenTestDirEntry) Info() (os.FileInfo, error) { return nil, errors.New("not needed") }

func TestFileListScanCancellationInvalidatesGeneration(t *testing.T) {
	m := testModel(modelState{})
	cmd := m.startFileListScan()
	if cmd == nil || m.fileListCancel == nil {
		t.Fatal("startFileListScan did not create a cancellable command")
	}
	generation := m.fileListGeneration
	m.cancelFileListScan()
	if m.fileListGeneration == generation {
		t.Fatal("cancelFileListScan did not invalidate the generation")
	}
	msg := cmd().(FileListMsg)
	if !errors.Is(msg.Err, context.Canceled) {
		t.Fatalf("cancelled scan error = %v, want context.Canceled", msg.Err)
	}
}

func BenchmarkWalkProjectFilesContext(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 1_000; i++ {
		name := filepath.Join(root, "pkg", "file"+strconv.Itoa(i)+".go")
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(name, nil, 0o600); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := walkProjectFilesContext(context.Background(), root, maxQuickOpenFiles); err != nil {
			b.Fatal(err)
		}
	}
}
