package toolpath

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

func TestZeroValueResolverDoesNotPanic(t *testing.T) {
	t.Setenv("PATH", "")
	var r Resolver
	if _, err := r.Resolve("definitely-not-installed"); err == nil {
		t.Fatal("zero-value Resolver unexpectedly resolved a missing tool")
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

func TestVecgrepHintUsesTapFormula(t *testing.T) {
	if got, want := Hint("vecgrep"), "brew install abdul-hamid-achik/tap/vecgrep"; got != want {
		t.Fatalf("Hint(vecgrep) = %q, want %q", got, want)
	}
}

func TestDefaultLanguageServerHintsAreActionable(t *testing.T) {
	tests := map[string]string{
		"jdtls":                       "brew install jdtls",
		"zls":                         "brew install zls",
		"solargraph":                  "gem install solargraph",
		"elixir-ls":                   "brew install elixir-ls",
		"OmniSharp":                   "dotnet tool install --global omnisharp-roslyn",
		"bp":                          "go install github.com/abdul-hamid-achik/blueprint/cmd/bp@latest",
		"vscode-json-language-server": "npm install -g vscode-langservers-extracted",
	}
	for name, want := range tests {
		if got := Hint(name); got != want {
			t.Errorf("Hint(%q) = %q, want %q", name, got, want)
		}
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

func TestResolveMakesRelativeExplicitPathAbsolute(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "tool")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDir, fixture)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := New(nil).Resolve(relative)
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", relative, err)
	}
	if !filepath.IsAbs(resolved) {
		t.Fatalf("Resolve(%q) = %q, want an absolute path", relative, resolved)
	}
	if filepath.Clean(resolved) != filepath.Clean(fixture) {
		t.Fatalf("Resolve(%q) = %q, want %q", relative, resolved, fixture)
	}
}

func TestRelativeOverrideIsNormalizedBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "configured-tool")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(workingDir, fixture)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := New(map[string]string{"configured": relative}).Resolve("configured")
	if err != nil {
		t.Fatalf("Resolve(configured) error = %v", err)
	}
	if !filepath.IsAbs(resolved) || filepath.Clean(resolved) != filepath.Clean(fixture) {
		t.Fatalf("Resolve(configured) = %q, want absolute %q", resolved, fixture)
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

func TestCommandTreatsNilContextAsBackground(t *testing.T) {
	dir := t.TempDir()
	fixture := writeExecutable(t, dir, "codemap")
	r := newTestResolver(t, dir, map[string]string{"codemap": fixture})

	//nolint:staticcheck // This test verifies the documented nil-context normalization contract.
	cmd, err := r.Command(nil, "codemap")
	if err != nil {
		t.Fatalf("Command(nil) error = %v", err)
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command(nil).Run() error = %v", err)
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

func TestCommandAppliesBoundedProcessLifecycle(t *testing.T) {
	dir := t.TempDir()
	fixture := writeExecutable(t, dir, "codemap")
	r := newTestResolver(t, dir, map[string]string{"codemap": fixture})

	cmd, err := r.Command(t.Context(), "codemap")
	if err != nil {
		t.Fatalf("Command() error = %v", err)
	}
	if cmd.WaitDelay <= 0 {
		t.Fatalf("Command() WaitDelay = %s, want a positive descendant-drain bound", cmd.WaitDelay)
	}
}

func TestRunBoundedCapsOutputBeforeCollectingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}

	cmd := exec.CommandContext(t.Context(), "sh", "-c", "printf '0123456789'")
	ConfigureCommand(cmd)
	stdout, stderr, err := RunBounded(cmd, 3, 3)
	if err == nil || !IsOutputLimit(err) {
		t.Fatalf("RunBounded() error = %v, want output-limit error", err)
	}
	if len(stdout) > 3 || len(stderr) > 3 {
		t.Fatalf("RunBounded() collected stdout=%d stderr=%d bytes, want each <= 3", len(stdout), len(stderr))
	}
}

func TestVersionProbeUsesBoundedAbsoluteCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codemap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' 'codemap version test'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, dir, map[string]string{"codemap": path})

	got, err := r.Version(t.Context(), "codemap")
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if got != "codemap version test" {
		t.Fatalf("Version() = %q, want probe output", got)
	}
}

func TestVersionProbeForwardsConfiguredEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codemap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n[ \"$TEAK_VERSION_FIXTURE\" = ready ] || exit 7\nprintf '%s\\n' 'codemap configured environment'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, dir, map[string]string{"codemap": path})

	got, err := r.VersionWithEnv(t.Context(), "codemap", map[string]string{"TEAK_VERSION_FIXTURE": "ready"})
	if err != nil {
		t.Fatalf("VersionWithEnv() error = %v", err)
	}
	if got != "codemap configured environment" {
		t.Fatalf("VersionWithEnv() = %q, want configured-environment output", got)
	}
}

func TestVersionProbeStopsWhenOutputLimitIsExceeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codemap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nwhile :; do printf '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, dir, map[string]string{"codemap": path})

	started := time.Now()
	_, err := r.Version(t.Context(), "codemap")
	if err == nil || !strings.Contains(err.Error(), "output exceeds") {
		t.Fatalf("Version() error = %v, want an output-limit error", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Version() took %s after exceeding the output limit, want prompt cancellation", elapsed)
	}
}

func TestVersionProbeSupportsKnownToolsAndLanguageServers(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "yaml-language-server", version: "yaml-language-server fixture"},
		{name: "lua-language-server", version: "lua-language-server fixture"},
		{name: "opencode", version: "opencode fixture"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, tt.name)
			if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+tt.version+"'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			r := newTestResolver(t, dir, map[string]string{tt.name: path})

			if !r.HasVersionProbe(tt.name) {
				t.Fatalf("HasVersionProbe(%q) = false, want true", tt.name)
			}
			got, err := r.Version(t.Context(), tt.name)
			if err != nil {
				t.Fatalf("Version() error = %v", err)
			}
			if got != tt.version {
				t.Fatalf("Version() = %q, want %q", got, tt.version)
			}
		})
	}
}

func TestVersionProbeDistinguishesUnsupportedAndFailed(t *testing.T) {
	dir := t.TempDir()
	failing := filepath.Join(dir, "codemap")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nprintf '%s\\n' 'broken' >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, dir, map[string]string{"codemap": failing})

	if _, err := r.Version(t.Context(), "codemap"); err == nil || !strings.Contains(err.Error(), "version probe") {
		t.Fatalf("failed Version() error = %v, want probe failure", err)
	}
	if _, err := r.Version(t.Context(), "custom-tool"); !errors.Is(err, ErrVersionProbeUnsupported) {
		t.Fatalf("unsupported Version() error = %v, want ErrVersionProbeUnsupported", err)
	}
}

func TestVersionProbeClassifiesItsInternalDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "codemap")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n/bin/sleep 10\nprintf '%s\\n' 'too late'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, dir, map[string]string{"codemap": path})

	started := time.Now()
	_, err := r.Version(t.Context(), "codemap")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Version() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 6*time.Second {
		t.Fatalf("Version() took %s after its internal deadline, want a bounded probe", elapsed)
	}
}

func TestVersionProbeCancelsShimProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process groups are platform-specific")
	}
	dir := t.TempDir()
	shim := filepath.Join(dir, "codemap")
	// Absolute sleep path: these tests run with an empty PATH, and a bare
	// "sleep" would fail to resolve on some platforms, collapsing the shim's
	// background work into an instant and voiding what the test measures.
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n(/bin/sleep 30) &\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, dir, map[string]string{"codemap": shim})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	if _, err := r.Version(ctx, "codemap"); err == nil {
		t.Fatal("Version() error = nil, want cancellation")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Version() took %s after cancellation, want a bounded probe", elapsed)
	}
}

func TestCommandCancelsShimProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process groups are platform-specific")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "descendant-survived")
	shim := filepath.Join(dir, "codemap")
	// Absolute sleep path: these tests run with an empty PATH, and a bare
	// "sleep" would fail to resolve on some platforms, collapsing the shim's
	// background work into an instant and voiding what the test measures.
	script := "#!/bin/sh\n(/bin/sleep 0.3; : > " + marker + ") &\nwhile :; do :; done\n"
	if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestResolver(t, dir, map[string]string{"codemap": shim})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	cmd, err := r.Command(ctx, "codemap")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err == nil {
		t.Fatal("Command.Run() error = nil, want cancellation")
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("cancelled command left a descendant running")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat descendant marker: %v", err)
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
				_, _ = r.Resolve("codemap")
				_, _ = r.Resolve("absent-tool")
				r.Invalidate("codemap")
			}
		}()
	}
	for range 8 {
		<-done
	}
}
