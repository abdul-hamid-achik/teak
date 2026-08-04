package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"teak/internal/toolpath"
)

func writeFcheapFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fcheap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureFcheap(t *testing.T, fixture string) {
	t.Helper()
	toolpath.Configure(map[string]string{"fcheap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
}

func TestStashFileParsesJSONFixture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeFcheapFixture(t, `
printf '%s\n' '{"id":"stash-1","name":"before-edit","source":"main.go","files":1}'
`)
	configureFcheap(t, fixture)

	result, err := StashFile(context.Background(), filepath.Join(t.TempDir(), "main.go"))
	if err != nil {
		t.Fatalf("StashFile() error = %v", err)
	}
	if result.ID != "stash-1" || result.Files != 1 {
		t.Fatalf("StashFile() = %#v, want fixture result", result)
	}
}

func TestRestoreStashRejectsOversizedOutputBeforeParsing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeFcheapFixture(t, `
head -c 5000000 /dev/zero
`)
	configureFcheap(t, fixture)

	_, err := RestoreStash(context.Background(), "stash-1")
	if !errors.Is(err, toolpath.ErrOutputLimit) {
		t.Fatalf("RestoreStash() error = %v, want toolpath.ErrOutputLimit", err)
	}
}

func TestListAndRestoreStashParseJSONFixtures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeFcheapFixture(t, `
case "$1" in
  list) printf '%s\n' '[{"id":"stash-1","name":"before","source":"main.go","created":"now","files":1,"tool":"teak"}]' ;;
  restore) printf '%s\n' '{"path":"/tmp/restored"}' ;;
  *) exit 2 ;;
esac
`)
	configureFcheap(t, fixture)

	entries, err := ListStashes(context.Background(), 10)
	if err != nil || len(entries) != 1 || entries[0].ID != "stash-1" {
		t.Fatalf("ListStashes() = %#v, %v, want one fixture entry", entries, err)
	}
	path, err := RestoreStash(context.Background(), "stash-1")
	if err != nil || path != "/tmp/restored" {
		t.Fatalf("RestoreStash() = %q, %v, want fixture path", path, err)
	}
}

func TestVaultReportsCommandAndJSONErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := writeFcheapFixture(t, `
case "$1" in
  list) printf '%s\n' 'not-json' ;;
  restore) printf '%s\n' 'permission denied' >&2; exit 9 ;;
  *) exit 2 ;;
esac
`)
	configureFcheap(t, fixture)

	if _, err := ListStashes(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "parse fcheap list") {
		t.Fatalf("ListStashes() error = %v, want JSON parse context", err)
	}
	if _, err := RestoreStash(context.Background(), "stash-1"); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("RestoreStash() error = %v, want command stderr detail", err)
	}
}

func TestVaultAvailableReflectsToolResolution(t *testing.T) {
	configureFcheap(t, filepath.Join(t.TempDir(), "missing-fcheap"))
	if Available() {
		t.Fatal("Available() = true for missing fcheap")
	}
}
