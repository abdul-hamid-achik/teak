package search

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/toolpath"
)

// rgJSONFixture is realistic `rg --json` output captured against a small
// two-file tree (mirrors what `rg --json needle .` actually prints), so the
// parser is tested against real ripgrep output shape without requiring rg to
// be installed for the test to run.
const rgJSONFixture = `{"type":"begin","data":{"path":{"text":"./a.txt"}}}
{"type":"match","data":{"path":{"text":"./a.txt"},"lines":{"text":"hello world\n"},"line_number":1,"absolute_offset":0,"submatches":[{"match":{"text":"hello"},"start":0,"end":5}]}}
{"type":"end","data":{"path":{"text":"./a.txt"},"binary_offset":null,"stats":{"elapsed":{"secs":0,"nanos":35250,"human":"0.000035s"},"searches":1,"searches_with_match":1,"bytes_searched":12,"bytes_printed":233,"matched_lines":1,"matches":1}}}
{"type":"begin","data":{"path":{"text":"./sub/b.txt"}}}
{"type":"match","data":{"path":{"text":"./sub/b.txt"},"lines":{"text":"hello again\n"},"line_number":3,"absolute_offset":0,"submatches":[{"match":{"text":"hello"},"start":0,"end":5}]}}
{"type":"end","data":{"path":{"text":"./sub/b.txt"},"binary_offset":null,"stats":{"elapsed":{"secs":0,"nanos":2750,"human":"0.000003s"},"searches":1,"searches_with_match":1,"bytes_searched":12,"bytes_printed":241,"matched_lines":1,"matches":1}}}
{"data":{"elapsed_total":{"human":"0.001862s","nanos":1861625,"secs":0},"stats":{"bytes_printed":474,"bytes_searched":24,"elapsed":{"human":"0.000038s","nanos":38000,"secs":0},"matched_lines":2,"matches":2,"searches":2,"searches_with_match":2}},"type":"summary"}
`

func TestParseRipgrepJSON(t *testing.T) {
	results, err := parseRipgrepJSON([]byte(rgJSONFixture), 100)
	if err != nil {
		t.Fatalf("parseRipgrepJSON() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}

	if got, want := results[0].FilePath, "a.txt"; got != want {
		t.Errorf("results[0].FilePath = %q, want %q", got, want)
	}
	if got, want := results[0].Line, 0; got != want {
		t.Errorf("results[0].Line = %d, want %d (rg is 1-based, Teak is 0-based)", got, want)
	}
	if got, want := results[0].Col, 0; got != want {
		t.Errorf("results[0].Col = %d, want %d", got, want)
	}
	if got, want := results[0].Preview, "hello world"; got != want {
		t.Errorf("results[0].Preview = %q, want %q", got, want)
	}

	if got, want := results[1].FilePath, filepath.FromSlash("sub/b.txt"); got != want {
		t.Errorf("results[1].FilePath = %q, want %q", got, want)
	}
	if got, want := results[1].Line, 2; got != want {
		t.Errorf("results[1].Line = %d, want %d", got, want)
	}
}

func TestParseRipgrepJSONRespectsMaxResults(t *testing.T) {
	results, err := parseRipgrepJSON([]byte(rgJSONFixture), 1)
	if err != nil {
		t.Fatalf("parseRipgrepJSON() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (capped), got %d", len(results))
	}
	if results[0].FilePath != "a.txt" {
		t.Errorf("expected the first match to survive the cap, got %q", results[0].FilePath)
	}
}

// TestParseRipgrepJSONTruncatedTrailingLine simulates output cut off
// mid-object by the bounded stdout buffer (maxRipgrepOutputBytes): the
// earlier, complete match lines must still be returned rather than the whole
// parse failing.
func TestParseRipgrepJSONTruncatedTrailingLine(t *testing.T) {
	truncated := rgJSONFixture[:len(rgJSONFixture)-40] // chop the tail mid-JSON
	results, err := parseRipgrepJSON([]byte(truncated), 100)
	if err != nil {
		t.Fatalf("parseRipgrepJSON() error = %v", err)
	}
	if len(results) < 1 {
		t.Fatalf("expected at least the first well-formed match to survive truncation, got %d results", len(results))
	}
	if results[0].FilePath != "a.txt" {
		t.Errorf("expected first match to be a.txt, got %q", results[0].FilePath)
	}
}

func TestParseRipgrepJSONReportsOversizedLineAfterResults(t *testing.T) {
	valid := `{"type":"match","data":{"path":{"text":"./a.go"},"lines":{"text":"needle\n"},"line_number":1,"submatches":[{"start":0}]}}`
	input := []byte(valid + "\n" + strings.Repeat("x", maxSearchLineBytes))

	results, err := parseRipgrepJSON(input, maxTextSearchResults)
	if err == nil {
		t.Fatal("parseRipgrepJSON() error = nil, want scanner error for oversized line")
	}
	if len(results) != 1 || results[0].FilePath != "a.go" {
		t.Fatalf("parseRipgrepJSON() results = %+v, want the valid prefix preserved", results)
	}
}

func TestParseRipgrepJSONIgnoresNonMatchLines(t *testing.T) {
	input := `{"type":"begin","data":{"path":{"text":"./a.txt"}}}
{"type":"end","data":{"path":{"text":"./a.txt"}}}
{"type":"summary","data":{}}
`
	results, err := parseRipgrepJSON([]byte(input), 100)
	if err != nil {
		t.Fatalf("parseRipgrepJSON() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for begin/end/summary-only input, got %d", len(results))
	}
}

func TestParseRipgrepJSONRejectsInvalidMatchRecords(t *testing.T) {
	input := `{"type":"match","data":{"path":{"text":""},"lines":{"text":"bad\n"},"line_number":0,"submatches":[{"start":-1}]}}
{"type":"match","data":{"path":{"text":"./valid.go"},"lines":{"text":"needle\n"},"line_number":2,"submatches":[{"start":0}]}}`
	results, err := parseRipgrepJSON([]byte(input), maxTextSearchResults)
	if err != nil {
		t.Fatalf("parseRipgrepJSON() error = %v", err)
	}
	if len(results) != 1 || results[0].FilePath != "valid.go" || results[0].Line != 1 || results[0].Col != 0 {
		t.Fatalf("parseRipgrepJSON() = %+v, want only the valid 0-based result", results)
	}
}

func TestParseRipgrepJSONEmptyInput(t *testing.T) {
	results, err := parseRipgrepJSON(nil, 100)
	if err != nil {
		t.Fatalf("parseRipgrepJSON() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestRipgrepArgsFixedStringsForLiteralQuery(t *testing.T) {
	args := ripgrepArgs("a.b+c", SearchOpts{})
	if !containsArg(args, "--fixed-strings") {
		t.Errorf("expected --fixed-strings for a non-regex query, got %v", args)
	}
	if !containsArg(args, "--ignore-case") {
		t.Errorf("expected --ignore-case by default (case-insensitive), got %v", args)
	}
}

func TestRipgrepArgsRegexQueryOmitsFixedStrings(t *testing.T) {
	args := ripgrepArgs("a.b+c", SearchOpts{Regex: true})
	if containsArg(args, "--fixed-strings") {
		t.Errorf("did not expect --fixed-strings for a regex query, got %v", args)
	}
}

func TestRipgrepArgsCaseSensitive(t *testing.T) {
	args := ripgrepArgs("Needle", SearchOpts{CaseSensitive: true})
	if !containsArg(args, "--case-sensitive") {
		t.Errorf("expected --case-sensitive, got %v", args)
	}
	if containsArg(args, "--ignore-case") {
		t.Errorf("did not expect --ignore-case, got %v", args)
	}
}

func TestRipgrepArgsWholeWord(t *testing.T) {
	args := ripgrepArgs("cat", SearchOpts{WholeWord: true})
	if !containsArg(args, "--word-regexp") {
		t.Errorf("expected --word-regexp, got %v", args)
	}
}

func TestRipgrepArgsSeparatesQueryFromFlags(t *testing.T) {
	// A query starting with '-' must never be interpreted as a flag; it must
	// appear after a literal "--" argument.
	args := ripgrepArgs("-not-a-flag", SearchOpts{})
	dashIdx, queryIdx := -1, -1
	for i, a := range args {
		if a == "--" {
			dashIdx = i
		}
		if a == "-not-a-flag" {
			queryIdx = i
		}
	}
	if dashIdx == -1 || queryIdx == -1 || queryIdx != dashIdx+1 {
		t.Fatalf("expected query immediately after a literal \"--\", got %v", args)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// --- Dispatch / fallback behaviour ---
//
// These tests override the ripgrepAvailableFn/ripgrepCommandFn seams instead
// of depending on whether rg happens to be installed on the machine running
// the tests, per the requirement that tests must not require rg to pass.

func withRipgrepSeams(t *testing.T, available func() bool, command func(ctx context.Context, args ...string) (*exec.Cmd, error)) {
	t.Helper()
	origAvailable := ripgrepAvailableFn
	origCommand := ripgrepCommandFn
	ripgrepAvailableFn = available
	if command != nil {
		ripgrepCommandFn = command
	}
	t.Cleanup(func() {
		ripgrepAvailableFn = origAvailable
		ripgrepCommandFn = origCommand
	})
}

func TestTextSearchContextFallsBackWhenRipgrepUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package main\n// needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	called := false
	withRipgrepSeams(t, func() bool { return false }, func(ctx context.Context, args ...string) (*exec.Cmd, error) {
		called = true
		return toolpath.Command(ctx, "rg", args...)
	})

	results, err := TextSearchContext(context.Background(), tmpDir, "needle", SearchOpts{})
	if err != nil {
		t.Fatalf("TextSearchContext() error = %v", err)
	}
	if called {
		t.Error("ripgrepCommandFn should not be invoked when ripgrepAvailableFn reports unavailable")
	}
	if len(results) != 1 {
		t.Fatalf("expected the Go walker fallback to find 1 result, got %d", len(results))
	}
	if results[0].FilePath != "a.go" {
		t.Errorf("FilePath = %q, want %q", results[0].FilePath, "a.go")
	}
}

func TestTextSearchFallbackFindsUnknownTextExtension(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "component.svelte")
	if err := os.WriteFile(path, []byte("<script>const needle = true</script>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	withRipgrepSeams(t, func() bool { return false }, nil)
	results, err := TextSearchContext(context.Background(), root, "needle", SearchOpts{})
	if err != nil {
		t.Fatalf("TextSearchContext() error = %v", err)
	}
	if len(results) != 1 || results[0].FilePath != "component.svelte" {
		t.Fatalf("fallback results = %+v, want component.svelte", results)
	}
}

func TestTextSearchFallbackSearchesWorkspaceNamedCommonSkipDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	withRipgrepSeams(t, func() bool { return false }, nil)
	results, err := TextSearchContext(context.Background(), root, "needle", SearchOpts{})
	if err != nil {
		t.Fatalf("TextSearchContext() error = %v", err)
	}
	if len(results) != 1 || results[0].FilePath != "main.go" {
		t.Fatalf("workspace-root results = %+v, want main.go", results)
	}
}

// TestTextSearchContextFallsBackOnRipgrepFailure simulates rg resolving but
// then failing on this particular invocation (e.g. exit code 2 for a
// rejected pattern, per requirement #4). The dispatcher must silently retry
// with the Go walker instead of surfacing an error.
func TestTextSearchContextFallsBackOnRipgrepFailure(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package main\n// needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	withRipgrepSeams(t, func() bool { return true }, func(ctx context.Context, args ...string) (*exec.Cmd, error) {
		// A command that always exits 2, standing in for rg rejecting a
		// pattern it cannot parse as Rust regex syntax.
		return exec.CommandContext(ctx, "sh", "-c", "exit 2"), nil
	})

	results, err := TextSearchContext(context.Background(), tmpDir, "needle", SearchOpts{})
	if err != nil {
		t.Fatalf("TextSearchContext() error = %v, want fallback with no error", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected the Go walker fallback to find 1 result, got %d", len(results))
	}
}

func TestTextSearchContextFallsBackAfterRipgrepOutputLimit(t *testing.T) {
	tmpDir := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("package main\n// needle here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	withRipgrepSeams(t, func() bool { return true }, func(ctx context.Context, args ...string) (*exec.Cmd, error) {
		// Emit one valid match and then enough output to trip the bounded
		// stdout buffer. The fallback must discover the second file rather
		// than treating the partial rg result as a complete success.
		script := `printf '%s\n' '{"type":"match","data":{"path":{"text":"./a.go"},"lines":{"text":"needle\\n"},"line_number":2,"submatches":[{"start":3}]}}'; dd if=/dev/zero bs=1048576 count=9 2>/dev/null`
		return exec.CommandContext(ctx, "sh", "-c", script), nil
	})

	results, err := TextSearchContext(context.Background(), tmpDir, "needle", SearchOpts{})
	if err != nil {
		t.Fatalf("TextSearchContext() error = %v, want bounded fallback", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected the fallback to find both files after rg output truncation, got %d: %+v", len(results), results)
	}
}

func TestTextSearchContextFallsBackAfterRipgrepParserStreamError(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package main\n// needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	withRipgrepSeams(t, func() bool { return true }, func(ctx context.Context, args ...string) (*exec.Cmd, error) {
		// The first record is valid, but the following JSON line exceeds the
		// parser's line budget. The dispatcher must reject that partial stream
		// and let the complete Go walker recover the result.
		script := `printf '%s\n' '{"type":"match","data":{"path":{"text":"./a.go"},"lines":{"text":"needle\n"},"line_number":2,"submatches":[{"start":3}]}}'; head -c 1048577 /dev/zero`
		return exec.CommandContext(ctx, "sh", "-c", script), nil
	})

	results, err := TextSearchContext(context.Background(), tmpDir, "needle", SearchOpts{})
	if err != nil {
		t.Fatalf("TextSearchContext() error = %v, want parser-error fallback", err)
	}
	if len(results) != 1 || results[0].FilePath != "a.go" {
		t.Fatalf("fallback results = %+v, want complete a.go result", results)
	}
}

// TestTextSearchContextRipgrepNoMatchIsNotFallback verifies that rg exiting 1
// (ran fine, no matches) is treated as a genuine empty result, not routed
// through the fallback engine.
func TestTextSearchContextRipgrepNoMatchIsNotFallback(t *testing.T) {
	tmpDir := t.TempDir()
	// This file would match under the Go walker fallback; if the dispatcher
	// incorrectly fell back on rg's "no matches" exit code, this result would
	// leak through, making the test fail loudly.
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package main\n// needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	withRipgrepSeams(t, func() bool { return true }, func(ctx context.Context, args ...string) (*exec.Cmd, error) {
		return exec.CommandContext(ctx, "sh", "-c", "exit 1"), nil
	})

	results, err := TextSearchContext(context.Background(), tmpDir, "needle", SearchOpts{})
	if err != nil {
		t.Fatalf("TextSearchContext() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results from a genuine rg 'no matches' exit, got %d (fallback leaked through)", len(results))
	}
}

func TestRipgrepSearchContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ripgrepSearchContext(ctx, t.TempDir(), "needle", SearchOpts{}, maxTextSearchResults)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ripgrepSearchContext() error = %v, want context.Canceled", err)
	}
}

// TestRipgrepIntegration exercises the real rg binary end to end when it is
// installed, as a sanity check beyond the seam-based unit tests above. It
// skips itself (rather than failing) when rg is not available, so it never
// requires rg to be installed for `go test` to pass.
func TestRipgrepIntegration(t *testing.T) {
	if !toolpath.Available("rg") {
		t.Skip("rg not installed; skipping real-binary integration test")
	}

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package main\n// needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "sub", "b.go"), []byte("package sub\n// another needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := ripgrepSearchContext(context.Background(), tmpDir, "needle", SearchOpts{}, maxTextSearchResults)
	if err != nil {
		t.Fatalf("ripgrepSearchContext() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
}
