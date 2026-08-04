package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/toolpath"
)

func TestHeadlessVecgrepHealthRejectsLegacyFullStatus(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' 'vecgrep legacy fixture'; exit 0; fi
printf '%s\n' "$*" >> "` + trace + `"
if [ "$2" = "--lightweight" ]; then printf '%s\n' 'unknown flag: --lightweight' >&2; exit 2; fi
printf '%s\n' '{"index_fresh":true,"stats":{"files":1},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"}}'
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectVecgrepStatus(context.Background(), t.TempDir())
	if status.State != "unsupported" || status.Ready {
		t.Fatalf("legacy vecgrep health = %#v, want unsupported and not ready", status)
	}
	if status.Mode != "lightweight-status" {
		t.Fatalf("legacy vecgrep health mode = %q, want lightweight-status", status.Mode)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "status --lightweight --format json" {
		t.Fatalf("legacy vecgrep status fallback was invoked: %q", got)
	}
}

func TestHeadlessVecgrepHealthStopsAfterFailedVersionProbe(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "vecgrep-trace")
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + trace + `"
case "$1" in
  --version) printf '%s\n' 'broken vecgrep fixture' >&2; exit 7 ;;
  status) printf '%s\n' 'status must not run after a failed version probe' >&2; exit 9 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectVecgrepStatus(context.Background(), t.TempDir())
	if status.State != "failed" || status.Ready || status.VersionProbe != "failed" {
		t.Fatalf("vecgrep health = %#v, want failed without readiness", status)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "--version" {
		t.Fatalf("vecgrep health invoked work after failed version probe: %q", got)
	}
}

func TestHeadlessVecgrepStatusPreservesCancelledVersionProbe(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	script := `#!/bin/sh
case "$1" in
  --version) printf '%s\n' 'vecgrep fixture' ;;
  status) printf '%s\n' '{"index_fresh":true,"stats":{"files":1},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"}}' ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status := collectVecgrepStatus(ctx, t.TempDir())
	if status.State != "cancelled" || status.VersionProbe != "cancelled" || status.Ready {
		t.Fatalf("cancelled vecgrep status = %#v, want cancelled and not ready", status)
	}
}

func TestHeadlessSemanticSearchReportsUnsupportedLightweightStatus(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' 'vecgrep legacy fixture'; exit 0; fi
printf '%s\n' 'unknown flag: --lightweight' >&2
exit 2
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"search", "--semantic", "--json", "--root", t.TempDir(), "meaning",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("legacy vecgrep semantic health unexpectedly succeeded")
	}
	response := readHeadlessSemanticJSON(t, &stdout)
	if response.State != "unsupported" || response.Hint == "" {
		t.Fatalf("legacy vecgrep semantic response = %#v, want unsupported with upgrade hint", response)
	}
	if stderr.Len() != 0 {
		t.Fatalf("legacy vecgrep semantic search wrote stderr: %s", stderr.String())
	}
}

func TestHeadlessSemanticIndexDoesNotReloadLegacyStatusAfterSearch(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "legacy-status-invoked")
	state := filepath.Join(t.TempDir(), "indexed")
	statusCalls := filepath.Join(t.TempDir(), "status-calls")
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	script := `#!/bin/sh
TRACE="` + trace + `"
STATE="` + state + `"
STATUS_CALLS="` + statusCalls + `"
case "$1" in
  --version)
    printf '%s\n' 'vecgrep fixture'
    ;;
  status)
    if [ "$2" != "--lightweight" ]; then
      : > "$TRACE"
      printf '%s\n' 'legacy status must not be used' >&2
      exit 9
    fi
    if [ ! -f "$STATUS_CALLS" ]; then
      : > "$STATUS_CALLS"
      printf '%s\n' '{"index_fresh":false,"stats":{"files":1},"pending_changes":{"total_pending":1},"freshness":{"state":"stale"},"lightweight":true}'
    else
      printf '%s\n' 'unknown flag: --lightweight' >&2
      exit 2
    fi
    ;;
  index)
    : > "$STATE"
    ;;
  search)
    printf '%s\n' '{"schema_version":1,"index":{"indexed":true,"fresh":true,"chunks":1},"hits":[{"file_path":"main.go","relative_path":"main.go","line":1,"col":1,"end_line":1,"preview":"fixture hit","score":0.9}]}'
    ;;
  init)
    ;;
  *)
    printf '%s\n' 'unexpected vecgrep command' >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"search", "--semantic", "--index", "--json", "--root", root, "meaning",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("explicit semantic search code = %d, stderr=%s, stdout=%s", code, stderr.String(), stdout.String())
	}
	response := readHeadlessSemanticJSON(t, &stdout)
	if response.State != "ready" || !response.Indexed {
		t.Fatalf("explicit semantic response = %#v, want ready indexed response", response)
	}
	if response.Hint == "" || !strings.Contains(strings.ToLower(response.Hint), "upgrade vecgrep") {
		t.Fatalf("explicit semantic response hint = %q, want actionable lightweight-status hint", response.Hint)
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatalf("explicit semantic search invoked legacy status: stat error = %v", err)
	}
}

func writeHeadlessSemanticFixture(t *testing.T, trace, state string) string {
	t.Helper()
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	script := `#!/bin/sh
TRACE="` + trace + `"
STATE="` + state + `"
case "$1" in
  --version)
    printf '%s\n' 'vecgrep fixture 1.0'
    ;;
  status)
    if [ -f "$STATE" ]; then
      printf '%s\n' '{"index_fresh":true,"stats":{"files":1},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"},"lightweight":true}'
    else
      printf '%s\n' '{"index_fresh":false,"stats":{"files":1},"pending_changes":{"total_pending":1},"freshness":{"state":"stale"},"lightweight":true}'
    fi
    ;;
  index)
    printf '%s\n' index >> "$TRACE"
    : > "$STATE"
    ;;
  init)
    printf '%s\n' init >> "$TRACE"
    ;;
  search)
    printf '%s\n' search >> "$TRACE"
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
	return fixture
}

func readHeadlessSemanticJSON(t *testing.T, stdout *bytes.Buffer) headlessSearchResponse {
	t.Helper()
	var response headlessSearchResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode semantic response: %v; stdout=%s", err, stdout.String())
	}
	return response
}

func TestHeadlessSemanticSearchReadOnlyRequiresFreshIndex(t *testing.T) {
	root := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	state := filepath.Join(t.TempDir(), "indexed")
	fixture := writeHeadlessSemanticFixture(t, trace, state)
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"search", "--semantic", "--json", "--root", root, "meaning",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("read-only semantic search unexpectedly succeeded with a stale index")
	}
	response := readHeadlessSemanticJSON(t, &stdout)
	if response.Mode != "semantic" || response.State != "stale" || response.Indexed {
		t.Fatalf("semantic stale response = %#v", response)
	}
	if data, err := os.ReadFile(trace); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("read-only semantic search invoked a mutating command: %q", data)
	}
	if stderr.Len() != 0 {
		t.Fatalf("read-only semantic search wrote stderr: %s", stderr.String())
	}
}

func TestHeadlessSemanticSearchIndexRequiresExplicitOptIn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace")
	state := filepath.Join(t.TempDir(), "indexed")
	fixture := writeHeadlessSemanticFixture(t, trace, state)
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"search", "--semantic", "--index", "--json", "--root", root, "meaning",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("explicit semantic indexing exit code = %d, stderr=%s", code, stderr.String())
	}
	response := readHeadlessSemanticJSON(t, &stdout)
	if response.Mode != "semantic" || response.State != "ready" || !response.Indexed || len(response.Results) != 1 {
		t.Fatalf("semantic ready response = %#v", response)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("read semantic trace: %v", err)
	}
	traceText := string(data)
	if !strings.Contains(traceText, "index") || !strings.Contains(traceText, "search") {
		t.Fatalf("explicit semantic indexing trace = %q, want index and search", traceText)
	}
}

func TestHeadlessSemanticSearchRejectsOutOfWorkspaceResult(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(outsideFile, []byte("package secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"status) printf '%s\\n' '{\"index_fresh\":true,\"stats\":{\"files\":1},\"pending_changes\":{\"total_pending\":0},\"freshness\":{\"state\":\"fresh\"},\"lightweight\":true}' ;;\n" +
		"search) printf '%s\\n' '{\"schema_version\":1,\"hits\":[{\"relative_path\":\"" + filepath.ToSlash(outsideFile) + "\",\"line\":1,\"col\":0,\"preview\":\"secret\"}]}' ;;\n" +
		"esac\n"
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"vecgrep": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"search", "--semantic", "--json", "--root", root, "meaning",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("semantic search accepted an out-of-workspace result")
	}
	response := readHeadlessSemanticJSON(t, &stdout)
	if response.State != "failed" || !strings.Contains(response.Detail, "outside workspace") {
		t.Fatalf("semantic boundary response = %#v, want failed outside-workspace result", response)
	}
	if stderr.Len() != 0 {
		t.Fatalf("semantic boundary search wrote stderr: %s", stderr.String())
	}
}

func TestHeadlessSemanticSearchRejectsIndexWithoutSemanticMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"search", "--index", "--json", "--root", t.TempDir(), "meaning",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("search --index exit code = %d, want 2", code)
	}
	var response headlessErrorResponse
	if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
		t.Fatalf("decode invalid semantic options: %v; stderr=%s", err, stderr.String())
	}
	if response.Code != "invalid_argument" {
		t.Fatalf("invalid semantic options = %#v", response)
	}
}
