package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"teak/internal/toolpath"
)

func writeHeadlessSurfaceTool(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func configureHeadlessSurfaceTools(t *testing.T) {
	t.Helper()
	codemap := writeHeadlessSurfaceTool(t, "codemap", `
case "$1" in
  --version) printf '%s\n' 'codemap fixture 1.0' ;;
  structural-manifest) printf '%s\n' '{"schema_version":1,"export_schema_version":1,"total_records":3,"complete":true,"freshness":{"checked":true,"fresh":true,"changed":0,"new":0,"deleted":0}}' ;;
  *) exit 2 ;;
esac`)
	vecgrep := writeHeadlessSurfaceTool(t, "vecgrep", `
case "$1" in
  --version) printf '%s\n' 'vecgrep fixture 1.0' ;;
  status) printf '%s\n' '{"index_fresh":true,"stats":{"files":2},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"},"lightweight":true}' ;;
  *) exit 2 ;;
esac`)
	hitspec := writeHeadlessSurfaceTool(t, "hitspec", `
case "$1" in
  --version) printf '%s\n' 'hitspec fixture 1.0' ;;
  validate) printf '%s\n' 'Validate hitspec files for syntax errors.' ;;
  *) exit 2 ;;
esac`)
	toolpath.Configure(map[string]string{
		"codemap": codemap,
		"vecgrep": vecgrep,
		"hitspec": hitspec,
	})
	t.Cleanup(func() { toolpath.Configure(nil) })
}

func TestRunHeadlessToolsJSONReportsVerifiedHealth(t *testing.T) {
	configureHeadlessSurfaceTools(t)
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"tools", "status", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("tools status exit code = %d, stderr=%s", code, stderr.String())
	}
	var response headlessToolsResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode tools response: %v; stdout=%s", err, stdout.String())
	}
	if response.Workspace != root || len(response.Tools) != 3 {
		t.Fatalf("tools response = %#v, want workspace and three tools", response)
	}
	for _, tool := range response.Tools {
		if !tool.Available || !tool.Ready || tool.State == "failed" || tool.VersionProbe != "ready" || tool.DurationMS < 0 {
			t.Errorf("tool health = %#v, want verified ready state", tool)
		}
		if tool.CapabilityProbe != "ready" || tool.Capability == "" {
			t.Errorf("tool health = %#v, want verified capability", tool)
		}
	}
	if response.Metrics.HeapAllocBytes == 0 || response.Metrics.HeapSysBytes == 0 {
		t.Fatalf("runtime metrics = %#v, want non-zero heap values", response.Metrics)
	}
}

func TestHeadlessOutputBufferNotifiesCancellationOnce(t *testing.T) {
	cancellations := 0
	buffer := &headlessOutputBuffer{
		limit: 4,
		onLimit: func() {
			cancellations++
		},
	}

	if _, err := buffer.Write([]byte("12345")); err != nil {
		t.Fatalf("first bounded write error = %v, want nil", err)
	}
	if _, err := buffer.Write([]byte("6789")); err != nil {
		t.Fatalf("second bounded write error = %v, want nil", err)
	}
	if !buffer.truncated {
		t.Fatal("bounded output did not record truncation")
	}
	if buffer.Len() != 4 {
		t.Fatalf("bounded output length = %d, want 4", buffer.Len())
	}
	if cancellations != 1 {
		t.Fatalf("cancellation callback count = %d, want 1", cancellations)
	}
}

func TestHeadlessOutputBufferCapsIoCopy(t *testing.T) {
	cancellations := 0
	buffer := &headlessOutputBuffer{
		limit: 4,
		onLimit: func() {
			cancellations++
		},
	}

	if _, err := io.Copy(buffer, strings.NewReader("123456789")); err != nil {
		t.Fatalf("io.Copy() error = %v, want nil", err)
	}
	if buffer.Len() != 4 || !buffer.truncated || cancellations != 1 {
		t.Fatalf("io.Copy() buffer length=%d truncated=%t cancellations=%d, want 4/true/1", buffer.Len(), buffer.truncated, cancellations)
	}
}

func TestHeadlessOutputBufferAcceptsExactIoCopyLimit(t *testing.T) {
	cancellations := 0
	buffer := &headlessOutputBuffer{
		limit: 4,
		onLimit: func() {
			cancellations++
		},
	}

	if _, err := io.Copy(buffer, strings.NewReader("1234")); err != nil {
		t.Fatalf("io.Copy() error = %v, want nil", err)
	}
	if buffer.Len() != 4 || buffer.truncated || cancellations != 0 {
		t.Fatalf("io.Copy() buffer length=%d truncated=%t cancellations=%d, want 4/false/0", buffer.Len(), buffer.truncated, cancellations)
	}
}

func TestRunDoctorCLIJSONIsMachineReadable(t *testing.T) {
	root := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("XDG_STATE_HOME", stateHome)
	// Use deterministic fixture commands for the project tools; the doctor
	// command's resolver is process-global by design.
	codemap := writeHeadlessSurfaceTool(t, "codemap-doctor", `printf '%s\n' 'codemap doctor fixture'`)
	vecgrep := writeHeadlessSurfaceTool(t, "vecgrep-doctor", `printf '%s\n' 'vecgrep doctor fixture'`)
	hitspec := writeHeadlessSurfaceTool(t, "hitspec-doctor", `printf '%s\n' 'hitspec doctor fixture'`)
	gitFixture := writeHeadlessSurfaceTool(t, "git-doctor", `printf '%s\n' 'git doctor fixture'`)
	rgFixture := writeHeadlessSurfaceTool(t, "rg-doctor", `printf '%s\n' 'rg doctor fixture'`)
	glyphFixture := writeHeadlessSurfaceTool(t, "glyph-doctor", `printf '%s\n' 'glyph doctor fixture'`)
	toolpath.Configure(map[string]string{
		"codemap": codemap,
		"vecgrep": vecgrep,
		"hitspec": hitspec,
		"git":     gitFixture,
		"rg":      rgFixture,
		"glyph":   glyphFixture,
	})

	var stdout, stderr bytes.Buffer
	code := runDoctorCLI([]string{"--json", "--root", root}, &stdout, &stderr, "test-version")
	if code != 0 {
		t.Fatalf("doctor exit code = %d, stderr=%s, stdout=%s", code, stderr.String(), stdout.String())
	}
	var report doctorReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode doctor response: %v; stdout=%s", err, stdout.String())
	}
	if report.Version != "test-version" || report.Workspace != root || len(report.Checks) == 0 {
		t.Fatalf("doctor report = %#v, want version/workspace/checks", report)
	}
	for _, check := range report.Checks {
		if check.Status == "fail" {
			t.Fatalf("doctor reported failure: %#v", check)
		}
	}
}

func TestRunHeadlessContextJSONReportsProjectEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"context", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("context exit code = %d, stderr=%s", code, stderr.String())
	}
	var response headlessContextResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode context response: %v; stdout=%s", err, stdout.String())
	}
	if response.Workspace != root || response.Truncated {
		t.Fatalf("context response = %#v, want bounded complete workspace", response)
	}
	seen := map[string]bool{}
	for _, entry := range response.Entries {
		seen[entry.Name] = true
	}
	if !seen["go.mod"] || !seen["README.md"] {
		t.Fatalf("context entries = %#v, want go.mod and README.md", response.Entries)
	}
}

func TestCollectHeadlessContextBoundsLargeDirectory(t *testing.T) {
	root := t.TempDir()
	for i := headlessMaxContextEntries + 4; i >= 0; i-- {
		name := fmt.Sprintf("entry-%04d", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	response, err := collectHeadlessContext(root)
	if err != nil {
		t.Fatalf("collectHeadlessContext() error = %v", err)
	}
	if !response.Truncated || len(response.Entries) != headlessMaxContextEntries {
		t.Fatalf("context response = truncated:%t entries:%d, want truncated with %d entries", response.Truncated, len(response.Entries), headlessMaxContextEntries)
	}
	if response.Entries[0].Name != "entry-0000" {
		t.Fatalf("first entry = %q, want deterministic lexical order", response.Entries[0].Name)
	}
	if response.Entries[len(response.Entries)-1].Name != "entry-1999" {
		t.Fatalf("last entry = %q, want bounded lexical prefix", response.Entries[len(response.Entries)-1].Name)
	}
}

func TestCollectHeadlessContextDepthReturnsBoundedRelativePaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "src", "main.go"),
		filepath.Join(root, "src", "nested", "types.go"),
		filepath.Join(root, "README.md"),
	} {
		if err := os.WriteFile(path, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	response, err := collectHeadlessContextDepth(root, 2)
	if err != nil {
		t.Fatalf("collectHeadlessContextDepth() error = %v", err)
	}
	seen := make(map[string]headlessContextEntry)
	for _, entry := range response.Entries {
		seen[entry.Path] = entry
	}
	for _, path := range []string{"src", "src/main.go", "src/nested", "src/nested/types.go", "README.md", "linked"} {
		if _, ok := seen[path]; !ok {
			t.Fatalf("context entries missing %q: %#v", path, response.Entries)
		}
	}
	if _, followed := seen["linked/secret.txt"]; followed {
		t.Fatalf("context followed symlink outside workspace: %#v", response.Entries)
	}
}

func TestRunHeadlessGitJSONReportsUntrackedFile(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for the headless git fixture")
	}
	root := t.TempDir()
	if output, err := exec.Command(gitPath, "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	for _, args := range [][]string{
		{"-C", root, "config", "user.email", "fixture@example.com"},
		{"-C", root, "config", "user.name", "Teak Fixture"},
	} {
		if output, err := exec.Command(gitPath, args...).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "tracked.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(gitPath, "-C", root, "add", "tracked.go").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	if output, err := exec.Command(gitPath, "-C", root, "commit", "-qm", "fixture").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"git": gitPath})
	t.Cleanup(func() { toolpath.Configure(nil) })

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"git", "status", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("git status exit code = %d, stderr=%s", code, stderr.String())
	}
	var response headlessGitResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode git response: %v; stdout=%s", err, stdout.String())
	}
	if response.State != "ready" || len(response.Entries) != 1 || response.Entries[0].Path != "new.go" || !response.Entries[0].Untracked {
		t.Fatalf("git response = %#v, want one untracked file", response)
	}
}
