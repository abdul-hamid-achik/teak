package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"teak/internal/toolpath"
)

// fakeVecgrepScript writes a stub `vecgrep` binary and returns its path plus
// a state directory the test uses to observe and control it, so these tests
// never depend on the real vecgrep binary being installed:
//
//   - `vecgrep status --format json` reports fresh (via index_fresh/stats/
//     pending_changes, matching the real CLI's JSON shape) once
//     "<state>/indexed" exists, and exits 1 ("not a vecgrep project")
//     otherwise.
//   - `vecgrep init` just drops a marker file.
//   - `vecgrep index` appends a line to "<state>/index_calls.log" (so tests
//     can assert how many times it actually ran), touches "<state>/started",
//     then blocks until "<state>/release" exists before touching
//     "<state>/indexed" and exiting 0. This lets a test hold indexing open
//     for as long as it needs to exercise cancellation/lifetime behavior.
//   - `vecgrep search` returns an empty hit list.
func fakeVecgrepScript(t *testing.T) (bin, stateDir string) {
	t.Helper()
	stateDir = t.TempDir()
	binDir := t.TempDir()
	bin = filepath.Join(binDir, "vecgrep")

	script := `#!/bin/sh
STATE="` + stateDir + `"
case "$1" in
  status)
    if [ -f "$STATE/indexed" ]; then
      printf '%s' '{"index_fresh": true, "stats": {"files": 1}, "pending_changes": {"total_pending": 0}}'
      exit 0
    fi
    echo "Error: not in a vecgrep project: run 'vecgrep init' first" 1>&2
    exit 1
    ;;
  init)
    touch "$STATE/initialized"
    exit 0
    ;;
  index)
    echo call >> "$STATE/index_calls.log"
    touch "$STATE/started"
    while [ ! -f "$STATE/release" ]; do
      sleep 0.02
    done
    touch "$STATE/indexed"
    exit 0
    ;;
  search)
    printf '{"hits":[]}'
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake vecgrep script: %v", err)
	}
	return bin, stateDir
}

// configureFakeVecgrep points the process-wide toolpath resolver at bin for
// the duration of the test. Overrides win over PATH (see toolpath.Resolver),
// so this works whether or not a real vecgrep is installed on the machine
// running the test.
func configureFakeVecgrep(t *testing.T, bin string) {
	t.Helper()
	toolpath.Configure(map[string]string{"vecgrep": bin})
	t.Cleanup(func() { toolpath.Configure(nil) })
}

// subprocessTimeout bounds waits on the fake vecgrep subprocess. It is a
// safety net against a hang, not a performance assertion: these tests spawn a
// real shell script, and under a full parallel test run process startup and
// filesystem latency can easily exceed a second or two. Generous headroom costs
// nothing when the code works and avoids flaking the suite when it is merely
// busy.
const subprocessTimeout = 30 * time.Second

func waitForFileToExist(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to exist", path)
}

// TestIndexingSurvivesLeaderContextCancellation is the regression test for
// the reported bug: a first-time vecgrep index build was killed the moment
// its caller's per-query search context was canceled, which search.go does
// on every keystroke, Tab mode switch, Ctrl+R toggle, and Escape. The build
// must now run under its own lifetime (see beginIndexBuild in semantic.go)
// and survive that cancellation.
func TestIndexingSurvivesLeaderContextCancellation(t *testing.T) {
	bin, state := fakeVecgrepScript(t)
	configureFakeVecgrep(t, bin)

	rootDir := t.TempDir()
	leaderCtx, leaderCancel := context.WithCancel(context.Background())

	result := make(chan error, 1)
	go func() {
		result <- ensureVecgrepReadyContext(leaderCtx, rootDir)
	}()

	waitForFileToExist(t, filepath.Join(state, "started"), subprocessTimeout)

	// Simulate the user editing the query (or hitting Escape/Tab) while this
	// caller happens to be the single-flight leader for the index build.
	leaderCancel()

	// Give the (buggy, pre-fix) exec.CommandContext-kill path a chance to
	// have fired, then confirm indexing is still in progress rather than
	// having been torn down or reported as failed.
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-result:
		t.Fatalf("ensureVecgrepReadyContext returned early (err=%v) after its own context was canceled; the index build must not be tied to a single caller's context", err)
	default:
	}
	if _, err := os.Stat(filepath.Join(state, "indexed")); err == nil {
		t.Fatal("index finished before release was signaled; test fixture is broken")
	}

	// Let indexing finish and confirm it completed successfully even though
	// the context that originally triggered it was canceled long ago.
	if err := os.WriteFile(filepath.Join(state, "release"), nil, 0o644); err != nil {
		t.Fatalf("signal release: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("ensureVecgrepReadyContext() error = %v, want nil", err)
		}
	case <-time.After(subprocessTimeout):
		t.Fatal("indexing did not complete after release was signaled")
	}
	if _, ok := vecgrepReady.Load(rootDir); !ok {
		t.Fatal("vecgrepReady was not marked ready after a successful index build")
	}
}

// TestCancelIndexingStopsSubprocessAndWaitForShutdown covers the shutdown
// path required alongside decoupling indexing from the per-query context:
// something must still be able to stop an in-flight build (workspace/app
// exit) so its subprocess is never leaked past process exit. This mirrors
// lsp.Client's Shutdown/WaitForShutdown pair.
func TestCancelIndexingStopsSubprocessAndWaitForShutdown(t *testing.T) {
	bin, state := fakeVecgrepScript(t)
	configureFakeVecgrep(t, bin)

	rootDir := t.TempDir()
	result := make(chan error, 1)
	go func() {
		result <- ensureVecgrepReadyContext(context.Background(), rootDir)
	}()
	waitForFileToExist(t, filepath.Join(state, "started"), subprocessTimeout)

	CancelIndexing()

	waitCtx, cancel := context.WithTimeout(context.Background(), subprocessTimeout)
	defer cancel()
	if !WaitForIndexingShutdown(waitCtx) {
		t.Fatal("WaitForIndexingShutdown did not observe the build finishing before its deadline")
	}

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ensureVecgrepReadyContext() error = %v, want context.Canceled", err)
		}
	case <-time.After(subprocessTimeout):
		t.Fatal("leader did not return after CancelIndexing")
	}

	// "indexed" is only written after "release" exists, which this test
	// never creates. If it exists anyway, the subprocess ran to completion
	// instead of being stopped by CancelIndexing.
	if _, err := os.Stat(filepath.Join(state, "indexed")); err == nil {
		t.Fatal("index subprocess ran to completion; CancelIndexing did not stop it")
	}
}

// TestConcurrentSemanticSearchesShareOneIndexBuild ensures the fix for
// context lifetime did not regress the existing single-flight guarantee:
// many concurrent first-time semantic searches for the same workspace must
// still share one `vecgrep index` invocation instead of racing to start
// their own.
func TestConcurrentSemanticSearchesShareOneIndexBuild(t *testing.T) {
	bin, state := fakeVecgrepScript(t)
	configureFakeVecgrep(t, bin)
	// Let the build finish immediately so every caller can complete.
	if err := os.WriteFile(filepath.Join(state, "release"), nil, 0o644); err != nil {
		t.Fatalf("pre-signal release: %v", err)
	}

	rootDir := t.TempDir()
	const callers = 8
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- ensureVecgrepReadyContext(context.Background(), rootDir)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ensureVecgrepReadyContext() error = %v, want nil", err)
		}
	}

	data, err := os.ReadFile(filepath.Join(state, "index_calls.log"))
	if err != nil {
		t.Fatalf("read index_calls.log: %v", err)
	}
	got := 0
	if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
		got = len(strings.Split(trimmed, "\n"))
	}
	if got != 1 {
		t.Fatalf("vecgrep index invoked %d times, want 1 (concurrent searches must share one build)", got)
	}
}

// TestIsIndexedFromStatus covers both the preferred structured JSON path and
// the prose fallback for older vecgrep releases. It is a regression test for
// a real bug found while reviewing this function: vecgrep's default-format
// `status` output always prints a "Files:" line inside "Index statistics:"
// whether or not the index is current, and the previous implementation
// matched that line (and the word "indexed") before ever checking for
// "stale". Combined with InvalidateSemanticIndex being called on every save,
// external file change, and tree change, that meant the staleness check
// almost always reported "already indexed" and `vecgrep index` was
// effectively never run again after the first time - invalidation looked
// like it kept the index fresh but silently did nothing.
func TestIsIndexedFromStatus(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{
			name: "json fresh with files and no pending changes",
			out:  `{"index_fresh": true, "stats": {"files": 3}, "pending_changes": {"total_pending": 0}}`,
			want: true,
		},
		{
			name: "json stale due to pending changes",
			out:  `{"index_fresh": false, "stats": {"files": 3}, "pending_changes": {"total_pending": 1}}`,
			want: false,
		},
		{
			name: "json index_fresh true but nothing has ever been indexed",
			out:  `{"index_fresh": true, "stats": {"files": 0}, "pending_changes": {"total_pending": 0}}`,
			want: false,
		},
		{
			name: "json total_pending zero but index_fresh explicitly false",
			out:  `{"index_fresh": false, "stats": {"files": 5}, "pending_changes": {"total_pending": 0}}`,
			want: false,
		},
		{
			name: "prose fallback fresh",
			out:  "Index statistics:\n  Files:      1\n\nReindex status:\n  Freshness:    fresh (fresh)\n",
			want: true,
		},
		{
			name: "prose fallback stale must not be masked by the Files: line",
			out:  "Index statistics:\n  Files:      1\n\nReindex status:\n  Freshness:    stale (raw_source_drift)\n",
			want: false,
		},
		{
			name: "prose fallback not initialized",
			out:  "Error: not in a vecgrep project: run 'vecgrep init' first\n",
			want: false,
		},
		{
			name: "malformed json falls back to prose and still detects stale",
			out:  "{not valid json} Freshness: stale",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIndexedFromStatus([]byte(tt.out)); got != tt.want {
				t.Errorf("isIndexedFromStatus(%q) = %v, want %v", tt.out, got, tt.want)
			}
		})
	}
}
