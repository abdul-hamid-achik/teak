package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/codemap"
	"teak/internal/toolpath"
)

func writeHeadlessCodemapFixture(t *testing.T) string {
	t.Helper()
	fixture := filepath.Join(t.TempDir(), "codemap")
	script := `#!/bin/sh
case "$1" in
context)
  printf '%s\n' '{"definitions":[{"symbol":"Greeter","fqn":"fixture.Greeter","kind":"type","file":"main.go","start_line":3,"end_line":5,"signature":"type Greeter struct{}","doc":"fixture"}],"callers":[],"callees":[{"symbol":"fmt.Println","fqn":"fmt.Println","kind":"function","file":"main.go","start_line":8,"end_line":8}],"references":[],"tests":[]}'
  ;;
impact)
  printf '%s\n' '{"locations":[],"direct_callers":[],"blast_radius":[{"symbol":"Greeter","fqn":"fixture.Greeter","kind":"type","file":"main.go","start_line":3,"end_line":5,"depth":0}],"tests":[],"test_commands":["go test ./..."]}'
  ;;
callers)
  printf '%s\n' '{"callers":[]}'
  ;;
callees)
  printf '%s\n' '{"callees":[]}'
  ;;
find)
  printf '%s\n' '[{"symbol":"Greeter","fqn":"fixture.Greeter","kind":"type","file":"main.go","start_line":3,"end_line":5}]'
  ;;
symbols)
  printf '%s\n' '[{"symbol":"Greeter","fqn":"fixture.Greeter","kind":"type","file":"main.go","start_line":3,"end_line":5,"signature":"type Greeter struct{}"}]'
  ;;
symbol-at)
  printf '%s\n' '{"symbol":"Greeter","fqn":"fixture.Greeter","kind":"type","file":"main.go","start_line":3,"end_line":5}'
  ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestHeadlessCodemapContextIsReadOnlyAndStructured(t *testing.T) {
	fixture := writeHeadlessCodemapFixture(t)
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"codemap", "context", "--json", "--root", root, "Greeter",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("headless codemap context exit code = %d, stderr=%s", code, stderr.String())
	}
	var response headlessCodemapResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode codemap response: %v; stdout=%s", err, stdout.String())
	}
	if response.State != "ready" || response.Operation != "context" || response.Symbol != "Greeter" || response.Context == nil {
		t.Fatalf("codemap response = %#v", response)
	}
	if len(response.Context.Definitions) != 1 || response.Context.Definitions[0].Symbol != "Greeter" || len(response.Context.Callees) != 1 {
		t.Fatalf("codemap context = %#v", response.Context)
	}
}

func TestHeadlessCodemapImpactHonorsBoundedDepth(t *testing.T) {
	fixture := writeHeadlessCodemapFixture(t)
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"codemap", "impact", "--json", "--depth", "3", "--root", root, "Greeter",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("headless codemap impact exit code = %d, stderr=%s", code, stderr.String())
	}
	var response headlessCodemapResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode impact response: %v; stdout=%s", err, stdout.String())
	}
	if response.State != "ready" || response.Impact == nil || len(response.Impact.BlastRadius) != 1 || len(response.Impact.TestCommands) != 1 {
		t.Fatalf("codemap impact response = %#v", response)
	}
}

func TestHeadlessCodemapSymbolsAndSymbolAtAreBounded(t *testing.T) {
	fixture := writeHeadlessCodemapFixture(t)
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	root := t.TempDir()
	var symbolsOut, symbolsErr bytes.Buffer
	code := runHeadlessCLI([]string{
		"codemap", "symbols", "--json", "--root", root, "main.go",
	}, strings.NewReader(""), &symbolsOut, &symbolsErr)
	if code != 0 {
		t.Fatalf("headless codemap symbols exit code = %d, stderr=%s", code, symbolsErr.String())
	}
	var symbols headlessCodemapResponse
	if err := json.Unmarshal(symbolsOut.Bytes(), &symbols); err != nil {
		t.Fatalf("decode codemap symbols response: %v; stdout=%s", err, symbolsOut.String())
	}
	if symbols.State != "ready" || symbols.Operation != "symbols" || len(symbols.Results) != 1 || symbols.Results[0].File != "main.go" {
		t.Fatalf("codemap symbols response = %#v", symbols)
	}

	var symbolAtOut, symbolAtErr bytes.Buffer
	code = runHeadlessCLI([]string{
		"codemap", "symbol-at", "--line", "3", "--json", "--root", root, "main.go",
	}, strings.NewReader(""), &symbolAtOut, &symbolAtErr)
	if code != 0 {
		t.Fatalf("headless codemap symbol-at exit code = %d, stderr=%s", code, symbolAtErr.String())
	}
	var symbolAt headlessCodemapResponse
	if err := json.Unmarshal(symbolAtOut.Bytes(), &symbolAt); err != nil {
		t.Fatalf("decode codemap symbol-at response: %v; stdout=%s", err, symbolAtOut.String())
	}
	if symbolAt.State != "ready" || symbolAt.Operation != "symbol-at" || len(symbolAt.Results) != 1 || symbolAt.Results[0].Symbol != "Greeter" {
		t.Fatalf("codemap symbol-at response = %#v", symbolAt)
	}
}

func TestHeadlessCodemapSymbolPathAndLineValidation(t *testing.T) {
	fixture := writeHeadlessCodemapFixture(t)
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	cases := [][]string{
		{"codemap", "symbols", "--json", "--root", t.TempDir(), "../outside.go"},
		{"codemap", "symbols", "--json", "--root", t.TempDir(), "/absolute.go"},
		{"codemap", "symbol-at", "--json", "--root", t.TempDir(), "main.go"},
		{"codemap", "symbol-at", "--line", "-1", "--json", "--root", t.TempDir(), "main.go"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runHeadlessCLI(args, strings.NewReader(""), &stdout, &stderr); code == 0 {
				t.Fatalf("invalid codemap invocation unexpectedly succeeded: stdout=%s", stdout.String())
			}
		})
	}
}

func TestHeadlessCodemapMissingToolIsActionable(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "codemap-missing")
	toolpath.Configure(map[string]string{"codemap": missing})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{
		"codemap", "context", "--json", "--root", t.TempDir(), "Greeter",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("headless codemap context unexpectedly succeeded with a missing tool")
	}
	var response headlessErrorResponse
	if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
		t.Fatalf("decode missing-tool error: %v; stderr=%s", err, stderr.String())
	}
	if response.Code != "tool_unavailable" || response.State != "error" {
		t.Fatalf("missing-tool error = %#v", response)
	}
}

func TestHeadlessCodemapStatusPreservesCancelledVersionProbe(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "codemap")
	script := `#!/bin/sh
case "$1" in
  --version) printf '%s\n' 'codemap fixture' ;;
  structural-manifest) printf '%s\n' '{"schema_version":1,"export_schema_version":1,"total_records":1,"complete":true,"freshness":{"checked":true,"fresh":true,"changed":0,"new":0,"deleted":0}}' ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status := collectCodemapStatus(ctx, t.TempDir())
	if status.State != "cancelled" || status.VersionProbe != "cancelled" || status.Ready {
		t.Fatalf("cancelled codemap status = %#v, want cancelled and not ready", status)
	}
}

func TestHeadlessCodemapResultsAreBounded(t *testing.T) {
	symbols := make([]codemap.Symbol, maxHeadlessCodemapResults+7)
	bounded, truncated := boundHeadlessCodemapSymbols(symbols)
	if len(bounded) != maxHeadlessCodemapResults || !truncated {
		t.Fatalf("bounded codemap symbols = len(%d), truncated=%v", len(bounded), truncated)
	}
}

func TestHeadlessToolStatusUsesStructuralManifestWithoutLegacyStatus(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "codemap-trace")
	fixture := filepath.Join(t.TempDir(), "codemap")
	script := `#!/bin/sh
case "$1" in
  --version) printf '%s\n' 'codemap fixture 2.0' ;;
  structural-manifest) printf '%s\n' '{"schema_version":1,"export_schema_version":1,"total_records":42,"complete":true,"freshness":{"checked":true,"fresh":true,"changed":0,"new":0,"deleted":0}}' ;;
  status) printf '%s\n' status-called >> "$CODEMAP_TRACE"; exit 99 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEMAP_TRACE", trace)
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectCodemapStatus(context.Background(), t.TempDir())
	if status.State != "ready" || !status.Ready || status.Records != 42 || status.VersionProbe != "ready" || status.Mode != "structural-manifest" {
		t.Fatalf("codemap health = %#v, want ready structural manifest", status)
	}
	if data, err := os.ReadFile(trace); err == nil && strings.Contains(string(data), "status-called") {
		t.Fatalf("legacy codemap status path was invoked: %q", data)
	}
}

func TestHeadlessCodemapHealthStopsAfterFailedVersionProbe(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "codemap-trace")
	fixture := filepath.Join(t.TempDir(), "codemap")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$CODEMAP_TRACE"
case "$1" in
  --version) printf '%s\n' 'broken codemap fixture' >&2; exit 7 ;;
  structural-manifest|status) printf '%s\n' 'status command must not run after a failed version probe' >&2; exit 9 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEMAP_TRACE", trace)
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectCodemapStatus(context.Background(), t.TempDir())
	if status.State != "failed" || status.Ready || status.VersionProbe != "failed" {
		t.Fatalf("codemap health = %#v, want failed without readiness", status)
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "--version" {
		t.Fatalf("codemap health invoked work after failed version probe: %q", got)
	}
}

func TestHeadlessToolStatusRequiresCheckedStructuralManifest(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "codemap")
	script := `#!/bin/sh
case "$1" in
  --version) printf '%s\n' 'codemap fixture 2.0' ;;
  structural-manifest) printf '%s\n' '{"schema_version":1,"total_records":42,"complete":true,"freshness":{"checked":false,"fresh":true,"changed":0,"new":0,"deleted":0}}' ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectCodemapStatus(context.Background(), t.TempDir())
	if status.State != "stale" || status.Ready {
		t.Fatalf("codemap health = %#v, want stale and not ready until freshness is checked", status)
	}
}

func TestHeadlessToolStatusReportsUnsupportedStructuralManifest(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "codemap-trace")
	fixture := filepath.Join(t.TempDir(), "codemap")
	script := `#!/bin/sh
case "$1" in
  --version) printf '%s\n' 'legacy codemap 1.0' ;;
  structural-manifest) printf '%s\n' 'unknown command: structural-manifest' >&2; exit 2 ;;
  status) printf '%s\n' status-called >> "$CODEMAP_TRACE"; exit 99 ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEMAP_TRACE", trace)
	toolpath.Configure(map[string]string{"codemap": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectCodemapStatus(context.Background(), t.TempDir())
	if status.State != "unsupported" || status.Ready || status.Hint == "" {
		t.Fatalf("legacy codemap health = %#v, want unsupported with hint", status)
	}
	if data, err := os.ReadFile(trace); err == nil && strings.Contains(string(data), "status-called") {
		t.Fatalf("legacy codemap status path was invoked: %q", data)
	}
}
