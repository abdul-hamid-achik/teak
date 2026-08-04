package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func TestVecgrepStatusAgainstDeterministicFixture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep-fixture")
	script := `#!/bin/sh
if [ "$1" = "status" ]; then
  printf '%s\n' '{"index_fresh":true,"stats":{"files":7},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"},"lightweight":true}'
  exit 0
fi
printf '%s\n' 'unsupported fixture command' >&2
exit 2
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status, err := VecgrepStatusContext(context.Background(), root)
	if err != nil {
		t.Fatalf("VecgrepStatusContext() error = %v", err)
	}
	if !status.IndexFresh || !status.FreshnessKnown || status.Files != 7 || status.PendingChanges != 0 {
		t.Fatalf("VecgrepStatusContext() = %#v, want fresh fixture status", status)
	}
}

func TestVecgrepStatusRefusesUnboundedLegacyFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep-legacy-status")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + trace + `"
case "$*" in
  "status --lightweight --format json") printf '%s\n' 'unknown option: --lightweight' >&2; exit 2 ;;
  "status --format json") printf '%s\n' 'unrecognized option: --format' >&2; exit 2 ;;
  "status") printf '%s\n' 'Index statistics:\n  Files: 1\nReindex status:\n  Freshness: fresh (fresh)' ; exit 0 ;;
esac
exit 2
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	_, err := VecgrepStatusContext(context.Background(), root)
	if !errors.Is(err, ErrVecgrepLightweightUnsupported) {
		t.Fatalf("VecgrepStatusContext() error = %v, want lightweight unsupported sentinel", err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "status --lightweight --format json" {
		t.Fatalf("unbounded legacy status fallback was invoked: calls = %q", got)
	}
}

func TestVecgrepLegacyStatusRequiresOptIn(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep-legacy-status")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	_, err := VecgrepLegacyStatusContext(context.Background(), root)
	if !errors.Is(err, ErrVecgrepLegacyStatusOptInRequired) {
		t.Fatalf("VecgrepLegacyStatusContext() error = %v, want explicit opt-in sentinel", err)
	}
}

func TestVecgrepLegacyStatusUsesBoundedFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep-legacy-status")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + trace + `"
case "$*" in
  "status --lightweight --format json") printf '%s\n' 'unknown option: --lightweight' >&2; exit 2 ;;
  "status --format json") printf '%s\n' 'unrecognized option: --format' >&2; exit 2 ;;
  "status") printf '%s\n' 'Index statistics:\n  Files: 1\nReindex status:\n  Freshness: fresh (fresh)' ; exit 0 ;;
esac
exit 2
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
	previousSampler := semanticRSSSampler
	previousInterval := semanticRSSPollInterval
	semanticRSSSampler = func(context.Context, int) (uint64, error) { return 1, nil }
	semanticRSSPollInterval = time.Millisecond
	t.Cleanup(func() {
		semanticRSSSampler = previousSampler
		semanticRSSPollInterval = previousInterval
	})

	status, err := VecgrepLegacyStatusContext(WithVecgrepLegacyStatus(context.Background(), 1<<20), root)
	if err != nil {
		t.Fatalf("VecgrepLegacyStatusContext() error = %v", err)
	}
	if !status.Ready() || !status.FreshnessKnown || status.FilesKnown {
		t.Fatalf("legacy status = %#v, want ready prose fallback without file stats", status)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "status --lightweight --format json\nstatus --format json\nstatus" {
		t.Fatalf("bounded legacy status calls = %q", got)
	}
}

func TestVecgrepLegacyStatusRejectsAmbiguousLightweightResponse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep-ambiguous-status")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + trace + `"
case "$*" in
  "status --lightweight --format json")
    # The command succeeds but omits lightweight:true. Treat this as an
    # ambiguous legacy response rather than trusting it as vector-free.
    printf '%s\n' '{"index_fresh":true,"stats":{"files":1},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"}}'
    ;;
  "status --format json")
    printf '%s\n' '{"index_fresh":true,"stats":{"files":1},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"}}'
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
	previousSampler := semanticRSSSampler
	previousInterval := semanticRSSPollInterval
	semanticRSSSampler = func(context.Context, int) (uint64, error) { return 1, nil }
	semanticRSSPollInterval = time.Millisecond
	t.Cleanup(func() {
		semanticRSSSampler = previousSampler
		semanticRSSPollInterval = previousInterval
	})

	status, err := VecgrepLegacyStatusContext(WithVecgrepLegacyStatus(context.Background(), 1<<20), root)
	if err != nil {
		t.Fatalf("VecgrepLegacyStatusContext() error = %v", err)
	}
	if !status.Ready() {
		t.Fatalf("ambiguous legacy status = %#v, want bounded fallback status", status)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "status --lightweight --format json\nstatus --format json" {
		t.Fatalf("ambiguous lightweight calls = %q, want marker validation and bounded fallback", got)
	}
}

func TestVecgrepLegacyStatusFailsClosedOnRSSBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep-legacy-status-budget")
	script := `#!/bin/sh
case "$*" in
  "status --lightweight --format json") printf '%s\n' 'unknown option: --lightweight' >&2; exit 2 ;;
  "status --format json") while :; do sleep 1; done ;;
esac
exit 2
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
	previousSampler := semanticRSSSampler
	previousInterval := semanticRSSPollInterval
	semanticRSSSampler = func(context.Context, int) (uint64, error) { return 2, nil }
	semanticRSSPollInterval = time.Second
	t.Cleanup(func() {
		semanticRSSSampler = previousSampler
		semanticRSSPollInterval = previousInterval
	})

	_, err := VecgrepLegacyStatusContext(WithVecgrepLegacyStatus(context.Background(), 1), root)
	if !errors.Is(err, ErrVecgrepLegacyStatusRSSBudgetExceeded) {
		t.Fatalf("VecgrepLegacyStatusContext() error = %v, want RSS budget sentinel", err)
	}
}

func TestVecgrepLegacyStatusFailsClosedWhenRSSIsUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep-legacy-status-rss")
	script := `#!/bin/sh
case "$*" in
  "status --lightweight --format json") printf '%s\n' 'unknown option: --lightweight' >&2; exit 2 ;;
  "status --format json") while :; do sleep 1; done ;;
esac
exit 2
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
	previousSampler := semanticRSSSampler
	previousInterval := semanticRSSPollInterval
	semanticRSSSampler = func(context.Context, int) (uint64, error) {
		return 0, errors.New("fixture RSS sampler unavailable")
	}
	semanticRSSPollInterval = time.Millisecond
	t.Cleanup(func() {
		semanticRSSSampler = previousSampler
		semanticRSSPollInterval = previousInterval
	})

	_, err := VecgrepLegacyStatusContext(WithVecgrepLegacyStatus(context.Background(), 1<<20), root)
	if !errors.Is(err, ErrVecgrepLegacyStatusRSSUnavailable) {
		t.Fatalf("VecgrepLegacyStatusContext() error = %v, want RSS unavailable sentinel", err)
	}
}

func TestVecgrepLightweightStatusDoesNotFallBackToVectorLoading(t *testing.T) {
	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep-legacy")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + trace + `"
case "$2" in
  --lightweight) printf '%s\n' 'unknown flag: --lightweight' >&2; exit 2 ;;
esac
printf '%s\n' '{"index_fresh":true,"stats":{"files":7},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"}}'
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	_, err := VecgrepLightweightStatusContext(context.Background(), root)
	if !errors.Is(err, ErrVecgrepLightweightUnsupported) {
		t.Fatalf("VecgrepLightweightStatusContext() error = %v, want unsupported sentinel", err)
	}
	data, readErr := os.ReadFile(trace)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := strings.TrimSpace(string(data)); got != "status --lightweight --format json" {
		t.Fatalf("legacy status fallback was invoked: calls = %q", got)
	}
}

func TestSemanticIndexSetupDoesNotFallBackToLegacyStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	legacyMarker := filepath.Join(t.TempDir(), "legacy-status-called")
	fixture := filepath.Join(t.TempDir(), "vecgrep-index-setup")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + trace + `"
case "$*" in
  "status --lightweight --format json") printf '%s\n' 'unknown flag: --lightweight' >&2; exit 2 ;;
  "status --format json"|"status") : > "` + legacyMarker + `"; printf '%s\n' 'unknown flag: legacy status must not run' >&2; exit 2 ;;
  index) exit 0 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	if err := setupVecgrepContext(context.Background(), root); err != nil {
		t.Fatalf("setupVecgrepContext() error = %v", err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "status --lightweight --format json\nindex" {
		t.Fatalf("explicit indexing status calls = %q, want lightweight probe and direct index only", got)
	}
	if _, err := os.Stat(legacyMarker); !os.IsNotExist(err) {
		t.Fatalf("explicit indexing invoked legacy status: stat error = %v", err)
	}
}

func TestSemanticIndexSetupInitializesOnlyAfterIndexReportsMissingProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	initialized := filepath.Join(t.TempDir(), "initialized")
	fixture := filepath.Join(t.TempDir(), "vecgrep-init-fallback")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + trace + `"
case "$*" in
  "status --lightweight --format json") printf '%s\n' 'unknown flag: --lightweight' >&2; exit 2 ;;
  "index")
    if [ ! -f "` + initialized + `" ]; then printf '%s\n' 'not in a vecgrep project' >&2; exit 1; fi
    exit 0
    ;;
  "init ` + root + `") : > "` + initialized + `"; exit 0 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	if err := setupVecgrepContext(context.Background(), root); err != nil {
		t.Fatalf("setupVecgrepContext() error = %v", err)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "status --lightweight --format json\nindex\ninit "+root+"\nindex" {
		t.Fatalf("index initialization calls = %q, want probe/index/init/index", got)
	}
}

func TestVecgrepLightweightStatusRequiresVectorFreeResponseMarker(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep-ambiguous")
	script := `#!/bin/sh
if [ "$1" = "status" ]; then
  printf '%s\n' '{"index_fresh":true,"stats":{"files":7},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"}}'
  exit 0
fi
exit 2
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	_, err := VecgrepLightweightStatusContext(context.Background(), root)
	if !errors.Is(err, ErrVecgrepLightweightUnsupported) {
		t.Fatalf("VecgrepLightweightStatusContext() error = %v, want unsupported sentinel", err)
	}
}

func TestSemanticSearchReadyUsesExistingIndexWithoutBuilding(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep-fixture")
	script := `#!/bin/sh
TRACE="` + trace + `"
case "$1" in
  status)
    printf '%s\n' '{"index_fresh":true,"stats":{"files":1},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"},"lightweight":true}'
    ;;
  search)
    printf '%s\n' '{"schema_version":1,"index":{"indexed":true,"fresh":true,"chunks":1},"hits":[{"file_path":"main.go","relative_path":"main.go","line":2,"col":1,"end_line":2,"preview":"semantic fixture hit","score":0.9,"symbol_name":"main","chunk_type":"function"}]}'
    ;;
  *)
    printf '%s\n' "$*" >> "$TRACE"
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	results, err := SemanticSearchReadyContext(context.Background(), root, "meaning")
	if err != nil {
		t.Fatalf("SemanticSearchReadyContext() error = %v", err)
	}
	if len(results) != 1 || results[0].Preview != "semantic fixture hit" {
		t.Fatalf("semantic results = %#v", results)
	}
	data, err := os.ReadFile(trace)
	if err == nil && strings.Contains(string(data), "index") {
		t.Fatalf("semantic read-only search invoked indexing: %q", data)
	}
}

func TestSemanticSearchReadyRejectsStaleIndexBeforeQuery(t *testing.T) {
	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep-fixture")
	script := `#!/bin/sh
TRACE="` + trace + `"
case "$1" in
  status)
    printf '%s\n' '{"index_fresh":false,"stats":{"files":1},"pending_changes":{"total_pending":1},"freshness":{"state":"stale"},"lightweight":true}'
    ;;
  search)
    printf '%s\n' 'search should not run' >> "$TRACE"
    exit 3
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	_, err := SemanticSearchReadyContext(context.Background(), root, "meaning")
	if !errors.Is(err, ErrSemanticIndexNotReady) {
		t.Fatalf("SemanticSearchReadyContext() error = %v, want ErrSemanticIndexNotReady", err)
	}
	data, readErr := os.ReadFile(trace)
	if readErr == nil && strings.Contains(string(data), "search should not run") {
		t.Fatalf("stale semantic search reached vecgrep query: %q", data)
	}
}

func TestSemanticSearchIndexContextHonorsCallerDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep-fixture")
	const script = `#!/bin/sh
case "$1" in
  status)
    printf '%s\n' '{"index_fresh":false,"stats":{"files":1},"pending_changes":{"total_pending":1},"freshness":{"state":"stale"},"lightweight":true}'
    ;;
  index)
    sleep 5
    ;;
  search)
    printf '%s\n' 'search should not run' >&2
    exit 3
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := SemanticSearchIndexContext(ctx, root, "meaning")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SemanticSearchIndexContext() error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("deadline cancellation took %s, want under one second", elapsed)
	}
}

func TestSemanticSearchIndexContextRejectsOversizedSetupOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "index-finished")
	fixture := filepath.Join(t.TempDir(), "vecgrep-fixture")
	script := `#!/bin/sh
MARKER="` + marker + `"
case "$1" in
  status)
    printf '%s\n' "Error: not in a vecgrep project: run 'vecgrep init' first" >&2
    exit 1
    ;;
  init)
    exit 0
    ;;
  index)
    i=0
    while [ "$i" -lt 10000 ]; do
      printf '%s' '0123456789012345678901234567890123456789'
      i=$((i + 1))
    done
    touch "$MARKER"
    sleep 5
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	started := time.Now()
	_, err := SemanticSearchIndexContext(context.Background(), root, "meaning")
	if !errors.Is(err, errSemanticOutputLimit) {
		t.Fatalf("SemanticSearchIndexContext() error = %v, want errSemanticOutputLimit", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("oversized setup output took %s, want bounded termination", elapsed)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("vecgrep index continued after its output exceeded the limit")
	}
}
