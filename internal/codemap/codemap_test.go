package codemap

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"teak/internal/toolpath"
)

func TestIndexRSSBudgetUsesConservativeDefault(t *testing.T) {
	const want = uint64(512 << 20)
	if defaultIndexRSSBudget != want {
		t.Fatalf("default index RSS budget = %d, want %d", defaultIndexRSSBudget, want)
	}
	if got := indexRSSBudgetFromContext(WithIndexRSSBudget(context.Background(), 0)); got != want {
		t.Fatalf("zero WithIndexRSSBudget() = %d, want default %d", got, want)
	}
	if got := indexRSSBudgetFromContext(WithIndexRSSBudget(context.Background(), 768<<20)); got != 768<<20 {
		t.Fatalf("explicit WithIndexRSSBudget() = %d, want 768 MiB", got)
	}
}

func TestQueryRSSBudgetUsesConservativeDefault(t *testing.T) {
	const want = uint64(512 << 20)
	if got := queryRSSBudgetFromContext(context.Background()); got != want {
		t.Fatalf("default query RSS budget = %d, want %d", got, want)
	}
	if got := queryRSSBudgetFromContext(WithQueryRSSBudget(context.Background(), 768<<20)); got != 768<<20 {
		t.Fatalf("explicit WithQueryRSSBudget() = %d, want 768 MiB", got)
	}
}

func TestRunWithTimeoutAcceptsNilContext(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("runWithTimeout() panicked with nil context: %v", recovered)
		}
	}()

	// The command is intentionally invalid. The contract under test is that
	// a nil caller context is normalized before context.WithTimeout is used;
	// the command's eventual error is not relevant here.
	_, _ = runWithTimeout(nil, t.TempDir(), time.Millisecond, "--invalid-test-flag")
}

func TestBoundedOutputBufferCallsLimitHandlerOnce(t *testing.T) {
	calls := 0
	buffer := &boundedOutputBuffer{
		limit: 3,
		onLimit: func() {
			calls++
		},
	}

	if _, err := buffer.Write([]byte("abcd")); err == nil {
		t.Fatal("boundedOutputBuffer.Write() error = nil, want output-limit error")
	}
	if _, err := buffer.Write([]byte("more")); err == nil {
		t.Fatal("boundedOutputBuffer.Write() after limit error = nil, want output-limit error")
	}
	if calls != 1 {
		t.Fatalf("limit handler calls = %d, want exactly one", calls)
	}
}

func TestBoundedOutputBufferCapsIoCopy(t *testing.T) {
	calls := 0
	buffer := &boundedOutputBuffer{
		limit: 3,
		onLimit: func() {
			calls++
		},
	}

	if _, err := io.Copy(buffer, strings.NewReader("abcdef")); err == nil {
		t.Fatal("io.Copy() error = nil, want output-limit error")
	}
	if buffer.Len() != 3 || !buffer.exceeded || calls != 1 {
		t.Fatalf("io.Copy() buffer length=%d exceeded=%t calls=%d, want 3/true/1", buffer.Len(), buffer.exceeded, calls)
	}
}

func TestBoundedOutputBufferAllowsExactIoCopyLimit(t *testing.T) {
	buffer := &boundedOutputBuffer{limit: 3}

	// LimitReader does not implement io.WriterTo, so io.Copy exercises the
	// buffer's ReadFrom override rather than strings.Reader's fast path.
	n, err := io.Copy(buffer, io.LimitReader(strings.NewReader("abc"), 3))
	if err != nil {
		t.Fatalf("io.Copy() error = %v, want nil for an exact fit", err)
	}
	if n != 3 || buffer.Len() != 3 || buffer.exceeded {
		t.Fatalf("bounded buffer n=%d length=%d exceeded=%t, want 3/3/false", n, buffer.Len(), buffer.exceeded)
	}
}

func TestStatusReadyAcceptsCurrentCodemapShape(t *testing.T) {
	tests := []struct {
		name       string
		statusJSON string
		want       bool
	}{
		{
			name:       "registered and fresh",
			statusJSON: `{"registered":true,"nodes":12,"edges":9,"files":3,"stale":{"changed":[],"new":[],"deleted":[]}}`,
			want:       true,
		},
		{
			name:       "registered but stale",
			statusJSON: `{"registered":true,"nodes":12,"edges":9,"files":3,"stale":{"changed":["main.go"],"new":[],"deleted":[]}}`,
			want:       false,
		},
		{
			name:       "legacy indexed and fresh",
			statusJSON: `{"indexed":true,"stale":{"changed":[],"new":[],"deleted":[]}}`,
			want:       true,
		},
		{
			name:       "unregistered",
			statusJSON: `{"registered":false,"stale":{"changed":[],"new":[],"deleted":[]}}`,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseStatus([]byte(tt.statusJSON))
			if err != nil {
				t.Fatalf("parseStatus() error = %v", err)
			}
			if got.Ready() != tt.want {
				t.Fatalf("status.Ready() = %v, want %v", got.Ready(), tt.want)
			}
		})
	}
}

func TestStatusAcceptsNumericAndLegacyStaleCounts(t *testing.T) {
	numeric, err := parseStatus([]byte(`{"registered":true,"stale":{"changed":88,"new":42,"deleted":3}}`))
	if err != nil {
		t.Fatalf("numeric status parse: %v", err)
	}
	if numeric.Stale.Changed != 88 || numeric.Stale.New != 42 || numeric.Stale.Deleted != 3 || numeric.Ready() {
		t.Fatalf("numeric status = %#v, want stale counts and not ready", numeric)
	}

	legacy, err := parseStatus([]byte(`{"registered":true,"stale":{"changed":["a.go"],"new":[],"deleted":[]}}`))
	if err != nil {
		t.Fatalf("legacy status parse: %v", err)
	}
	if legacy.Stale.Changed != 1 || legacy.Stale.New != 0 || legacy.Stale.Deleted != 0 {
		t.Fatalf("legacy status = %#v, want normalized counts", legacy.Stale)
	}
}

func TestParseStructuralManifest(t *testing.T) {
	got, err := parseStructuralManifest([]byte(`{"schema_version":1,"export_schema_version":1,"total_records":8,"complete":true,"freshness":{"checked":true,"fresh":false,"changed":1,"new":2,"deleted":3}}`))
	if err != nil {
		t.Fatalf("parseStructuralManifest() error = %v", err)
	}
	if got.SchemaVersion != 1 || got.ExportSchemaVersion != 1 || got.TotalRecords != 8 || !got.Complete ||
		got.Freshness.Fresh || got.Freshness.Changed != 1 || got.Freshness.New != 2 || got.Freshness.Deleted != 3 {
		t.Fatalf("parseStructuralManifest() = %#v, want manifest fields", got)
	}
	if got.Ready() {
		t.Fatal("stale structural manifest must not be ready")
	}
	got.Freshness.Fresh = true
	if !got.Ready() {
		t.Fatal("complete and fresh structural manifest should be ready")
	}
}

func TestStructuralManifestReadinessRequiresSchemaMarker(t *testing.T) {
	for name, data := range map[string]string{
		"missing schema":        `{"total_records":8,"complete":true,"freshness":{"checked":true,"fresh":true}}`,
		"unknown schema":        `{"schema_version":2,"export_schema_version":1,"total_records":8,"complete":true,"freshness":{"checked":true,"fresh":true}}`,
		"missing export schema": `{"schema_version":1,"total_records":8,"complete":true,"freshness":{"checked":true,"fresh":true}}`,
		"unknown export schema": `{"schema_version":1,"export_schema_version":2,"total_records":8,"complete":true,"freshness":{"checked":true,"fresh":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			manifest, err := parseStructuralManifest([]byte(data))
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Ready() {
				t.Fatalf("manifest.Ready() = true for unrecognized schema: %#v", manifest)
			}
		})
	}
}

func TestStructuralStatusReadinessRequiresSchemaMarker(t *testing.T) {
	status := StatusResult{
		Registered:          true,
		Indexed:             true,
		Structural:          true,
		SchemaVersion:       0,
		ExportSchemaVersion: 0,
		FreshnessChecked:    true,
		Fresh:               true,
	}
	if status.Ready() {
		t.Fatalf("structural status with missing schema marker is ready: %#v", status)
	}
	status.SchemaVersion = 1
	status.ExportSchemaVersion = 1
	if !status.Ready() {
		t.Fatalf("structural status with schema version 1 is not ready: %#v", status)
	}
}

func TestParseSymbolListAcceptsResultsEnvelope(t *testing.T) {
	data := []byte(`{"symbol":"run","found":true,"results":[{"symbol":"caller","file":"main.go","start_line":4}]}`)
	got, err := parseSymbolList(data, "callers")
	if err != nil {
		t.Fatalf("parseSymbolList() error = %v", err)
	}
	if len(got) != 1 || got[0].Symbol != "caller" {
		t.Fatalf("parseSymbolList() = %#v, want one caller", got)
	}
}

func TestParseSymbolListRejectsUnknownObject(t *testing.T) {
	if _, err := parseSymbolList([]byte(`{"found":false,"message":"not found"}`), "callees"); err == nil {
		t.Fatal("parseSymbolList() error = nil, want missing result error")
	}
}

func TestValidateImpactDepthBoundsWork(t *testing.T) {
	if err := validateImpactDepth(3); err != nil {
		t.Fatalf("validateImpactDepth(3) = %v, want nil", err)
	}
	for _, depth := range []int{-1, maxImpactDepth + 1} {
		if err := validateImpactDepth(depth); err == nil {
			t.Fatalf("validateImpactDepth(%d) = nil, want error", depth)
		}
	}
}

func TestGraphQueriesUseBoundedJSONContracts(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap-graph-fixture")
	const script = `#!/bin/sh
case "$1" in
  context) printf '%s\n' '{"definitions":[{"symbol":"run","start_line":4}],"callers":[],"callees":[],"references":[],"tests":[]}' ;;
  impact) printf '%s\n' '{"locations":[{"symbol":"run"}],"direct_callers":[],"blast_radius":[{"symbol":"caller","depth":1}],"tests":[],"test_commands":["go test ./..."]}' ;;
  callers) printf '%s\n' '{"callers":[{"symbol":"caller","start_line":2}]}' ;;
  callees) printf '%s\n' '[{"symbol":"callee","start_line":3}]' ;;
  symbols) printf '%s\n' '{"project":"fixture","file":"main.go","symbols":[{"symbol":"run","start_line":4}]}' ;;
  symbol-at) printf '%s\n' '{"symbol":"run","start_line":8}' ;;
  find) printf '%s\n' '[{"symbol":"run","start_line":4}]' ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	if got := (Symbol{StartLine: 4}).Line0(); got != 3 {
		t.Fatalf("Symbol.Line0() = %d, want 3", got)
	}
	if got := (Symbol{}).Line0(); got != 0 {
		t.Fatalf("zero Symbol.Line0() = %d, want 0", got)
	}

	contextResult, err := Context(context.Background(), root, "run")
	if err != nil || len(contextResult.Definitions) != 1 {
		t.Fatalf("Context() = %#v, %v, want one definition", contextResult, err)
	}
	impact, err := Impact(context.Background(), root, "run", 3)
	if err != nil || len(impact.BlastRadius) != 1 || impact.BlastRadius[0].Depth != 1 {
		t.Fatalf("Impact() = %#v, %v, want one blast-radius record", impact, err)
	}
	callers, err := Callers(context.Background(), root, "run")
	if err != nil || len(callers) != 1 || callers[0].Symbol != "caller" {
		t.Fatalf("Callers() = %#v, %v, want caller", callers, err)
	}
	callees, err := Callees(context.Background(), root, "run")
	if err != nil || len(callees) != 1 || callees[0].Symbol != "callee" {
		t.Fatalf("Callees() = %#v, %v, want callee", callees, err)
	}
	symbol, err := SymbolAt(context.Background(), root, "main.go", 7)
	if err != nil || symbol.Symbol != "run" || symbol.StartLine != 8 {
		t.Fatalf("SymbolAt() = %#v, %v, want run at line 8", symbol, err)
	}
	found, err := Find(context.Background(), root, "run")
	if err != nil || len(found) != 1 || found[0].Symbol != "run" {
		t.Fatalf("Find() = %#v, %v, want run", found, err)
	}
	symbols, err := Symbols(context.Background(), root, "main.go")
	if err != nil || len(symbols) != 1 || symbols[0].Symbol != "run" {
		t.Fatalf("Symbols() = %#v, %v, want run", symbols, err)
	}
}

func TestStatusUsesStructuralManifestWithoutInvokingFullStatus(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap-safe-status-fixture")
	marker := filepath.Join(root, "full-status-invoked")
	const script = `#!/bin/sh
case "$1" in
  structural-manifest)
    printf '%s\n' '{"schema_version":1,"export_schema_version":1,"total_records":9,"complete":true,"freshness":{"checked":true,"fresh":true,"changed":0,"new":0,"deleted":0}}'
    ;;
  status)
    touch "full-status-invoked"
    exit 99
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status, err := Status(context.Background(), root)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Structural || status.Records != 9 || !status.Ready() {
		t.Fatalf("Status() = %#v, want ready structural status with 9 records", status)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("full status marker exists or stat failed: %v", err)
	}
}

func TestFullStatusRequiresExplicitOptIn(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "full-status-invoked")
	fixture := filepath.Join(t.TempDir(), "codemap-full-status-fixture")
	const script = `#!/bin/sh
case "$1" in
  status)
    [ "$2" = "--full" ] && [ "$3" = "--json" ] || exit 3
    touch "full-status-invoked"
    printf '%s\n' '{"registered":true,"nodes":1,"edges":2,"files":3,"stale":{"changed":0,"new":0,"deleted":0}}'
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	if _, err := FullStatus(context.Background(), root); !errors.Is(err, ErrFullStatusOptInRequired) {
		t.Fatalf("FullStatus() error = %v, want explicit opt-in error", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("FullStatus() launched codemap without opt-in: stat error = %v", err)
	}

	status, err := FullStatus(WithFullStatus(context.Background()), root)
	if err != nil {
		t.Fatalf("FullStatus(WithFullStatus()) error = %v", err)
	}
	if status.Nodes != 1 || status.Edges != 2 || status.Files != 3 || !status.Ready() {
		t.Fatalf("authorized FullStatus() = %#v, want ready graph statistics", status)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("authorized FullStatus() did not run codemap: %v", err)
	}
}

func TestFullStatusTerminatesWhenRSSBudgetIsExceeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	marker := filepath.Join(root, "status-finished")
	fixture := filepath.Join(t.TempDir(), "codemap-rss-fixture")
	const script = `#!/bin/sh
case "$1" in
  status)
    sleep 5
    : > "status-finished"
    printf '%s\n' '{"registered":true,"nodes":1,"edges":0,"files":1,"stale":{"changed":0,"new":0,"deleted":0}}'
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	previousSampler := rssSampler
	rssSampler = func(context.Context, int) (uint64, error) { return 2, nil }
	t.Cleanup(func() {
		rssSampler = previousSampler
		toolpath.Configure(nil)
	})

	_, err := FullStatus(WithFullStatusBudget(context.Background(), 1), root)
	if !errors.Is(err, ErrFullStatusRSSBudgetExceeded) {
		t.Fatalf("FullStatus() error = %v, want RSS budget error", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("RSS-limited status completed or stat failed: %v", statErr)
	}
}

func TestFullStatusChecksRSSBudgetBeforeFirstPollInterval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap-short-rss-fixture")
	const script = `#!/bin/sh
case "$1" in
  status)
    sleep 0.02
    printf '%s\n' '{"registered":true,"nodes":1,"edges":0,"files":1,"stale":{"changed":0,"new":0,"deleted":0}}'
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	previousSampler := rssSampler
	previousInterval := rssPollInterval
	rssSampler = func(context.Context, int) (uint64, error) { return 2, nil }
	rssPollInterval = time.Second
	t.Cleanup(func() {
		rssSampler = previousSampler
		rssPollInterval = previousInterval
		toolpath.Configure(nil)
	})

	_, err := FullStatus(WithFullStatusBudget(context.Background(), 1), root)
	if !errors.Is(err, ErrFullStatusRSSBudgetExceeded) {
		t.Fatalf("FullStatus() error = %v, want immediate RSS budget error", err)
	}
}

func TestCodemapQueryTerminatesWhenRSSBudgetIsExceeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	marker := filepath.Join(root, "query-finished")
	fixture := filepath.Join(t.TempDir(), "codemap-query-rss-fixture")
	const script = `#!/bin/sh
case "$1" in
  context)
    sleep 5
    : > "query-finished"
    printf '%s\n' '{"definitions":[]}'
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	previousSampler := rssSampler
	rssSampler = func(context.Context, int) (uint64, error) { return 2, nil }
	t.Cleanup(func() {
		rssSampler = previousSampler
		toolpath.Configure(nil)
	})

	_, err := Context(WithQueryRSSBudget(context.Background(), 1), root, "run")
	if !errors.Is(err, ErrQueryRSSBudgetExceeded) {
		t.Fatalf("Context() error = %v, want query RSS budget error", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("RSS-limited query completed or stat failed: %v", statErr)
	}
}

func TestStatusDoesNotReportUnverifiedStructuralManifestReady(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap-unverified-status-fixture")
	const script = `#!/bin/sh
case "$1" in
  structural-manifest)
	    printf '%s\n' '{"schema_version":1,"export_schema_version":1,"total_records":9,"complete":true,"freshness":{"checked":false,"fresh":false,"changed":0,"new":0,"deleted":0}}'
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status, err := Status(context.Background(), root)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Ready() {
		t.Fatalf("Status() = %#v, want unverified structural manifest to be not ready", status)
	}
}

func TestEnsureReadyUsesMemorySafeIndexFlags(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap-memory-safe-index-fixture")
	const script = `#!/bin/sh
case "$1" in
  structural-manifest)
    if [ -f .codemap-indexed ]; then
      printf '%s\n' '{"schema_version":1,"export_schema_version":1,"total_records":9,"complete":true,"freshness":{"checked":true,"fresh":true,"changed":0,"new":0,"deleted":0}}'
    else
      printf '%s\n' 'not initialized' >&2
      exit 1
    fi
    ;;
  init)
    exit 0
    ;;
  index)
    printf '%s\n' "$@" > .codemap-index-args
    : > .codemap-indexed
    exit 0
    ;;
  *)
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() {
		CancelIndexing()
		_ = WaitForIndexingShutdown(context.Background())
		toolpath.Configure(nil)
	})

	if err := EnsureReady(context.Background(), root); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	args, err := os.ReadFile(filepath.Join(root, ".codemap-index-args"))
	if err != nil {
		t.Fatalf("read index args: %v", err)
	}
	want := "index\n--no-embed\n--no-lsp\n--no-tips\n"
	if string(args) != want {
		t.Fatalf("codemap index args = %q, want %q", args, want)
	}
}

func TestEnsureReadyRejectsSuccessfulIndexWithUnreadyManifest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap-post-index-stale-fixture")
	const script = `#!/bin/sh
case "$1" in
  structural-manifest)
    printf '%s\n' '{"schema_version":1,"export_schema_version":1,"total_records":9,"complete":true,"freshness":{"checked":true,"fresh":false,"changed":1,"new":0,"deleted":0}}'
    ;;
  index)
    : > .codemap-indexed
    exit 0
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() {
		CancelIndexing()
		_ = WaitForIndexingShutdown(context.Background())
		toolpath.Configure(nil)
	})

	err := EnsureReady(context.Background(), root)
	if !errors.Is(err, ErrIndexDidNotProduceReadyManifest) {
		t.Fatalf("EnsureReady() error = %v, want post-index manifest error", err)
	}
}

func TestEnsureReadyTerminatesWhenIndexRSSBudgetIsExceeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	marker := filepath.Join(root, "index-finished")
	fixture := filepath.Join(t.TempDir(), "codemap-index-rss-fixture")
	const script = `#!/bin/sh
case "$1" in
  structural-manifest)
    printf '%s\n' 'not initialized' >&2
    exit 1
    ;;
  init)
    exit 0
    ;;
  index)
    sleep 5
    : > "index-finished"
    ;;
  *)
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	previousSampler := rssSampler
	rssSampler = func(context.Context, int) (uint64, error) { return 2, nil }
	t.Cleanup(func() {
		rssSampler = previousSampler
		CancelIndexing()
		_ = WaitForIndexingShutdown(context.Background())
		toolpath.Configure(nil)
	})

	err := EnsureReady(WithIndexRSSBudget(context.Background(), 1), root)
	if !errors.Is(err, ErrIndexRSSBudgetExceeded) {
		t.Fatalf("EnsureReady() error = %v, want RSS budget error", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("RSS-limited index completed or stat failed: %v", statErr)
	}
}

func TestEnsureReadyTerminatesWhenInitRSSBudgetIsExceeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	marker := filepath.Join(root, "init-finished")
	fixture := filepath.Join(t.TempDir(), "codemap-init-rss-fixture")
	const script = `#!/bin/sh
case "$1" in
  structural-manifest)
    printf '%s\n' 'not initialized' >&2
    exit 1
    ;;
  init)
    sleep 5
    : > "init-finished"
    ;;
  index)
    exit 0
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	previousSampler := rssSampler
	rssSampler = func(context.Context, int) (uint64, error) { return 2, nil }
	t.Cleanup(func() {
		rssSampler = previousSampler
		CancelIndexing()
		_ = WaitForIndexingShutdown(context.Background())
		toolpath.Configure(nil)
	})

	err := EnsureReady(WithIndexRSSBudget(context.Background(), 1), root)
	if !errors.Is(err, ErrIndexRSSBudgetExceeded) {
		t.Fatalf("EnsureReady() error = %v, want init RSS budget error", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("RSS-limited init completed or stat failed: %v", statErr)
	}
}

func TestEnsureReadyLegacyManifestUsesDirectIndexWithoutStatusFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	trace := filepath.Join(root, ".codemap-calls")
	legacyStatusMarker := filepath.Join(root, ".legacy-status-called")
	fixture := filepath.Join(t.TempDir(), "codemap-legacy-index-fixture")
	script := `#!/bin/sh
case "$1" in
  structural-manifest)
    printf '%s\n' 'unknown command: structural-manifest' >&2
    exit 2
    ;;
  index)
    printf 'index\n' >> "` + trace + `"
    exit 0
    ;;
  status)
    : > "` + legacyStatusMarker + `"
    printf '%s\n' 'legacy status must not run' >&2
    exit 9
    ;;
  *)
    printf '%s\n' "$1" >> "` + trace + `"
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	if err := EnsureReady(context.Background(), root); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	calls, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(calls)); got != "index" {
		t.Fatalf("legacy codemap calls = %q, want direct index only", got)
	}
	if _, err := os.Stat(legacyStatusMarker); !os.IsNotExist(err) {
		t.Fatalf("legacy status fallback was invoked: stat error = %v", err)
	}
}

func TestEnsureReadyLegacyIndexInitializesOnlyAfterMissingProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable script")
	}

	root := t.TempDir()
	trace := filepath.Join(root, ".codemap-calls")
	initialized := filepath.Join(root, ".codemap-initialized")
	legacyStatusMarker := filepath.Join(root, ".legacy-status-called")
	fixture := filepath.Join(t.TempDir(), "codemap-legacy-init-fixture")
	script := `#!/bin/sh
case "$1" in
  structural-manifest)
    printf '%s\n' 'unknown command: structural-manifest' >&2
    exit 2
    ;;
  index)
    printf 'index\n' >> "` + trace + `"
    if [ ! -f "` + initialized + `" ]; then
      printf '%s\n' 'not a codemap project' >&2
      exit 1
    fi
    exit 0
    ;;
  init)
    printf 'init\n' >> "` + trace + `"
    : > "` + initialized + `"
    exit 0
    ;;
  status)
    : > "` + legacyStatusMarker + `"
    printf '%s\n' 'legacy status must not run' >&2
    exit 9
    ;;
  *)
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	if err := EnsureReady(context.Background(), root); err != nil {
		t.Fatalf("EnsureReady() error = %v", err)
	}
	calls, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(calls)); got != "index\ninit\nindex" {
		t.Fatalf("legacy codemap calls = %q, want index/init/index", got)
	}
	if _, err := os.Stat(legacyStatusMarker); !os.IsNotExist(err) {
		t.Fatalf("legacy status fallback was invoked: stat error = %v", err)
	}
}
