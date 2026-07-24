package toolpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeExecutable creates a runnable stub binary in dir and returns its path.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// newTestResolver builds a Resolver whose only search location is dir, so tests
// never depend on what happens to be installed on the machine running them.
func newTestResolver(t *testing.T, dir string, overrides map[string]string) *Resolver {
	t.Helper()
	t.Setenv("PATH", "")
	r := New(overrides)
	r.extraDirs = []string{dir}
	return r
}

func TestResolveFindsBinaryInExtraDirWhenPathIsEmpty(t *testing.T) {
	dir := t.TempDir()
	want := writeExecutable(t, dir, "codemap")
	r := newTestResolver(t, dir, nil)

	got, err := r.Resolve("codemap")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestResolvePrefersPathOverExtraDirs(t *testing.T) {
	pathDir := t.TempDir()
	extraDir := t.TempDir()
	wantPath := writeExecutable(t, pathDir, "gopls")
	writeExecutable(t, extraDir, "gopls")

	t.Setenv("PATH", pathDir)
	r := New(nil)
	r.extraDirs = []string{extraDir}

	got, err := r.Resolve("gopls")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	// PATH must win so an activated toolchain or venv is not overridden by a
	// stray global install.
	if got != wantPath {
		t.Errorf("Resolve = %q, want PATH entry %q", got, wantPath)
	}
}

func TestResolveMissingReturnsMissingToolErrorWithHint(t *testing.T) {
	r := newTestResolver(t, t.TempDir(), nil)

	_, err := r.Resolve("dlv")
	if err == nil {
		t.Fatal("Resolve: expected error for absent binary")
	}
	if !IsMissing(err) {
		t.Errorf("IsMissing(%v) = false, want true", err)
	}
	var missing *MissingToolError
	if !errors.As(err, &missing) {
		t.Fatalf("errors.As: %v is not a *MissingToolError", err)
	}
	if missing.Hint == "" {
		t.Error("MissingToolError.Hint is empty, want the dlv install command")
	}
}

func TestResolveNotFoundIsRetriedAfterCacheExpiry(t *testing.T) {
	dir := t.TempDir()
	r := newTestResolver(t, dir, nil)

	clock := time.Now()
	r.now = func() time.Time { return clock }

	if _, err := r.Resolve("vecgrep"); err == nil {
		t.Fatal("Resolve: expected initial miss")
	}

	// Installing the tool mid-session must take effect without restarting the
	// editor. This is the regression that made a missing language server stay
	// missing for the whole session.
	want := writeExecutable(t, dir, "vecgrep")

	if _, err := r.Resolve("vecgrep"); err == nil {
		t.Error("Resolve: expected miss to still be cached before TTL expiry")
	}

	clock = clock.Add(cacheTTL + time.Second)

	got, err := r.Resolve("vecgrep")
	if err != nil {
		t.Fatalf("Resolve after TTL: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Resolve after TTL = %q, want %q", got, want)
	}
}

func TestInvalidateForcesImmediateRelookup(t *testing.T) {
	dir := t.TempDir()
	r := newTestResolver(t, dir, nil)

	if _, err := r.Resolve("bob"); err == nil {
		t.Fatal("Resolve: expected initial miss")
	}

	want := writeExecutable(t, dir, "bob")
	r.Invalidate("bob")

	got, err := r.Resolve("bob")
	if err != nil {
		t.Fatalf("Resolve after Invalidate: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Resolve after Invalidate = %q, want %q", got, want)
	}
}

func TestOverrideWins(t *testing.T) {
	dir := t.TempDir()
	overrideDir := t.TempDir()
	writeExecutable(t, dir, "codemap")
	want := writeExecutable(t, overrideDir, "codemap-custom")

	r := newTestResolver(t, dir, map[string]string{"codemap": want})

	got, err := r.Resolve("codemap")
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Resolve = %q, want override %q", got, want)
	}
}

func TestBrokenOverrideDoesNotSilentlyFallBack(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "codemap")

	r := newTestResolver(t, dir, map[string]string{
		"codemap": filepath.Join(t.TempDir(), "does-not-exist"),
	})

	// Falling back would run a different binary than the user configured, which
	// is worse than reporting the misconfiguration.
	if _, err := r.Resolve("codemap"); err == nil {
		t.Fatal("Resolve: expected error for unusable override, got success")
	}
}

func TestNonExecutableFileIsNotResolved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codemap")
	if err := os.WriteFile(path, []byte("not executable"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := newTestResolver(t, dir, nil)

	if _, err := r.Resolve("codemap"); err == nil {
		t.Error("Resolve: expected error for non-executable file, got success")
	}
}

func TestDirectoryIsNotResolvedAsBinary(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "codemap"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	r := newTestResolver(t, dir, nil)

	if _, err := r.Resolve("codemap"); err == nil {
		t.Error("Resolve: expected error for directory, got success")
	}
}

func TestResolveAcceptsExplicitPath(t *testing.T) {
	dir := t.TempDir()
	want := writeExecutable(t, dir, "custom-lsp")
	r := newTestResolver(t, t.TempDir(), nil)

	got, err := r.Resolve(want)
	if err != nil {
		t.Fatalf("Resolve(%q): unexpected error: %v", want, err)
	}
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

func TestCommandUsesAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	want := writeExecutable(t, dir, "codemap")
	r := newTestResolver(t, dir, nil)

	cmd, err := r.Command(t.Context(), "codemap", "symbols")
	if err != nil {
		t.Fatalf("Command: unexpected error: %v", err)
	}
	// An absolute Path is the point: it removes any dependency on the PATH the
	// child process would otherwise inherit.
	if cmd.Path != want {
		t.Errorf("cmd.Path = %q, want %q", cmd.Path, want)
	}
}

func TestCommandFailsWithoutSpawningWhenToolIsMissing(t *testing.T) {
	r := newTestResolver(t, t.TempDir(), nil)

	cmd, err := r.Command(t.Context(), "codemap", "symbols")
	if err == nil {
		t.Fatal("Command: expected error for missing binary")
	}
	if cmd != nil {
		t.Error("Command: expected nil *exec.Cmd alongside error")
	}
}

func TestWellKnownDirsAreDeduplicatedAndNonEmpty(t *testing.T) {
	dirs := wellKnownDirs()
	if len(dirs) == 0 {
		t.Fatal("wellKnownDirs returned no directories")
	}
	seen := make(map[string]bool, len(dirs))
	for _, dir := range dirs {
		if dir == "" {
			t.Error("wellKnownDirs contains an empty entry")
		}
		if seen[dir] {
			t.Errorf("wellKnownDirs contains duplicate %q", dir)
		}
		seen[dir] = true
	}
}

func TestResolveIsSafeForConcurrentUse(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "codemap")
	r := newTestResolver(t, dir, nil)

	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 50 {
				r.Resolve("codemap")
				r.Resolve("absent-tool")
				r.Invalidate("codemap")
			}
		}()
	}
	for range 8 {
		<-done
	}
}
