package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"teak/internal/lsp"
	"teak/internal/toolpath"
)

func TestHeadlessLSPDiagnosticsAgainstFixture(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.fixture"), []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("TEAK_HEADLESS_LSP_FIXTURE", "1")
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`[[lsp]]
extensions = [".fixture"]
command = %s
args = ["-test.run=^TestHeadlessLSPFixtureProcess$", "--"]
language_id = "fixture"
`, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	code := runHeadlessCLI([]string{
		"lsp", "diagnostics", "--json", "--root", root, "main.fixture",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("headless diagnostics exit code = %d, stderr=%s", code, stderr.String())
	}
	var response headlessLSPDiagnosticsResponse
	if err := json.Unmarshal([]byte(stdout.String()), &response); err != nil {
		t.Fatalf("decode diagnostics response: %v; stdout=%s", err, stdout.String())
	}
	if response.State != "ready" || response.RelativePath != "main.fixture" || len(response.Diagnostics) != 1 {
		t.Fatalf("diagnostics response = %#v", response)
	}
	diagnostic := response.Diagnostics[0]
	if diagnostic.Message != "fixture diagnostic" || diagnostic.Line != 0 || diagnostic.Column != 1 || diagnostic.Severity != 1 {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestHeadlessLSPDiagnosticsReportsMissingServer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.fixture"), []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[[lsp]]
extensions = [".fixture"]
command = "/definitely/missing/teak-fixture-lsp"
language_id = "fixture"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	response, err := collectHeadlessLSPDiagnostics(root, filepath.Join(root, "main.fixture"))
	if err != nil {
		t.Fatalf("collect missing-server diagnostics: %v", err)
	}
	if response.State != "missing" || response.Hint != "" {
		// An absolute custom command has no package install hint, but the state
		// must still be actionable and machine-readable.
		t.Fatalf("missing-server response = %#v", response)
	}
}

func TestHeadlessLSPDiagnosticsReportsUnsupportedFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(file, []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	response, err := collectHeadlessLSPDiagnostics(root, file)
	if err != nil {
		t.Fatalf("collect unsupported diagnostics: %v", err)
	}
	if response.State != "unsupported" || len(response.Diagnostics) != 0 {
		t.Fatalf("unsupported response = %#v", response)
	}
}

func TestHeadlessLSPDiagnosticsCancellationStopsInitialization(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.fixture")
	if err := os.WriteFile(file, []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("TEAK_HEADLESS_LSP_FIXTURE", "1")
	t.Setenv("TEAK_HEADLESS_LSP_BLOCK_INIT", "1")
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`[[lsp]]
extensions = [".fixture"]
command = %s
args = ["-test.run=^TestHeadlessLSPFixtureProcess$", "--"]
language_id = "fixture"
`, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		response headlessLSPDiagnosticsResponse
		err      error
	}
	done := make(chan result, 1)
	started := time.Now()
	go func() {
		response, err := collectHeadlessLSPDiagnosticsContext(ctx, root, file)
		done <- result{response: response, err: err}
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("cancelled diagnostics returned error: %v", result.err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("cancelled diagnostics took %s, want prompt shutdown", elapsed)
		}
		if result.response.State != "cancelled" {
			t.Fatalf("cancelled diagnostics state = %q, want cancelled", result.response.State)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled diagnostics did not stop the language server promptly")
	}
}

func TestHeadlessLSPFormatCancellationReturnsStructuredState(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.fixture")
	if err := os.WriteFile(file, []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("TEAK_HEADLESS_LSP_FIXTURE", "1")
	t.Setenv("TEAK_HEADLESS_LSP_BLOCK_INIT", "1")
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`[[lsp]]
extensions = [".fixture"]
command = %s
args = ["-test.run=^TestHeadlessLSPFixtureProcess$", "--"]
language_id = "fixture"
`, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan struct {
		response headlessLSPFormatResponse
		err      error
	}, 1)
	go func() {
		response, err := collectHeadlessLSPFormatContext(ctx, root, file)
		result <- struct {
			response headlessLSPFormatResponse
			err      error
		}{response: response, err: err}
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("cancelled format returned error: %v", got.err)
		}
		if got.response.State != "cancelled" || got.response.Applied {
			t.Fatalf("cancelled format response = %#v, want cancelled without apply", got.response)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled format did not return promptly")
	}
}

func TestRunHeadlessLSPStageEnforcesTimeout(t *testing.T) {
	started := time.Now()
	_, timedOut := runHeadlessLSPStage(25*time.Millisecond, func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	if !timedOut {
		t.Fatal("runHeadlessLSPStage() did not report timeout")
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("runHeadlessLSPStage() took %s, want bounded return", elapsed)
	}
}

func TestHeadlessLSPStatusDoesNotClaimAvailableAfterVersionProbeFailure(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '%s\\n' 'broken gopls' >&2; exit 7; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"gopls": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[[lsp]]\nextensions = [\".go\"]\ncommand = \"gopls\"\nlanguage_id = \"go\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatus(t.TempDir())
	if err != nil {
		t.Fatalf("collect LSP status: %v", err)
	}
	var entry *headlessLSPEntry
	for i := range response.Servers {
		if response.Servers[i].Command == "gopls" {
			entry = &response.Servers[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("LSP status = %#v, missing gopls", response.Servers)
	}
	if entry.State != "failed" || !entry.Available || entry.Ready || entry.VersionProbe != "failed" {
		t.Fatalf("gopls status = %#v, want failed probe", *entry)
	}
}

func TestHeadlessLSPStatusClassifiesInternalVersionDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then sleep 10; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"gopls": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[[lsp]]\nextensions = [\".go\"]\ncommand = \"gopls\"\nlanguage_id = \"go\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatus(t.TempDir())
	if err != nil {
		t.Fatalf("collect LSP status: %v", err)
	}
	var entry *headlessLSPEntry
	for i := range response.Servers {
		if response.Servers[i].Command == "gopls" {
			entry = &response.Servers[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("LSP status = %#v, missing gopls", response.Servers)
	}
	if entry.State != "timed_out" || !entry.Available || entry.Ready || entry.VersionProbe != "timed_out" {
		t.Fatalf("gopls status = %#v, want timed_out probe", *entry)
	}
}

func TestHeadlessLSPStatusReportsUnsupportedProbeExplicitly(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "fixture-lsp")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"fixture-lsp": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[[lsp]]\nextensions = [\".fixture\"]\ncommand = \"fixture-lsp\"\nlanguage_id = \"fixture\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatus(t.TempDir())
	if err != nil {
		t.Fatalf("collect LSP status: %v", err)
	}
	var entry *headlessLSPEntry
	for i := range response.Servers {
		if response.Servers[i].Command == "fixture-lsp" {
			entry = &response.Servers[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("LSP status = %#v, missing fixture-lsp", response.Servers)
	}
	if entry.State != "unsupported" || !entry.Available || entry.Ready || entry.VersionProbe != "unsupported" || entry.ProtocolProbe != "" || !strings.Contains(entry.Hint, "--probe") {
		t.Fatalf("fixture LSP status = %#v, want explicit opt-in probe state", *entry)
	}
}

func TestHeadlessLSPStatusUsesProtocolProbeWithoutVersionCommand(t *testing.T) {
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`[[lsp]]
extensions = [".json"]
command = %s
args = ["-test.run=^TestDoctorProtocolProbeFixtureProcess$", "--"]
language_id = "json"
env = { TEAK_DOCTOR_LSP_FIXTURE = "1", TEAK_LSP_CAPABILITIES = "1" }
`, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatusWithProbe(root)
	if err != nil {
		t.Fatalf("collect protocol-only LSP status: %v", err)
	}
	var entry *headlessLSPEntry
	for i := range response.Servers {
		if response.Servers[i].LanguageID == "json" {
			entry = &response.Servers[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("LSP status = %#v, missing protocol-only server", response.Servers)
	}
	wantCapabilities := []string{"hover", "definition", "references", "document_symbols", "formatting"}
	if entry.State != "available" || !entry.Available || !entry.Ready || entry.VersionProbe != "unsupported" || entry.ProtocolProbe != "ready" || entry.CapabilityProbe != "ready" || !reflect.DeepEqual(entry.Capabilities, wantCapabilities) || entry.Hint != "" {
		t.Fatalf("protocol-only LSP status = %#v, want ready protocol probe", *entry)
	}
}

func TestHeadlessLSPStatusDeduplicatesIdenticalProtocolProbes(t *testing.T) {
	root := t.TempDir()
	configHome := t.TempDir()
	marker := filepath.Join(t.TempDir(), "protocol-probes.log")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	quotedCommand := strconv.Quote(os.Args[0])
	config := fmt.Sprintf(`[[lsp]]
extensions = [".json"]
command = %s
args = ["-test.run=^TestDoctorProtocolProbeFixtureProcess$", "--"]
language_id = "json"
env = { TEAK_DOCTOR_LSP_FIXTURE = "1", TEAK_LSP_PROBE_MARKER = %q }

[[lsp]]
extensions = [".jsonc"]
command = %s
args = ["-test.run=^TestDoctorProtocolProbeFixtureProcess$", "--"]
language_id = "jsonc"
env = { TEAK_DOCTOR_LSP_FIXTURE = "1", TEAK_LSP_PROBE_MARKER = %q }
`, quotedCommand, marker, quotedCommand, marker)
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatusWithProbe(root)
	if err != nil {
		t.Fatalf("collect deduplicated protocol status: %v", err)
	}
	for _, languageID := range []string{"json", "jsonc"} {
		var entry *headlessLSPEntry
		for i := range response.Servers {
			if response.Servers[i].LanguageID == languageID {
				entry = &response.Servers[i]
				break
			}
		}
		if entry == nil || !entry.Ready || entry.ProtocolProbe != "ready" {
			t.Fatalf("%s protocol status = %#v, want ready", languageID, entry)
		}
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read protocol probe marker: %v", err)
	}
	if got := strings.Count(string(data), "probe\n"); got != 1 {
		t.Fatalf("protocol probe launches = %d, want one shared launch", got)
	}
}

func TestHeadlessLSPStatusReportsProtocolProbeFailure(t *testing.T) {
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`[[lsp]]
extensions = [".json"]
command = %s
args = ["-test.run=^TestDoctorProtocolProbeFixtureProcess$", "--"]
language_id = "json"
`, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatusWithProbe(root)
	if err != nil {
		t.Fatalf("collect failed protocol-only LSP status: %v", err)
	}
	var entry *headlessLSPEntry
	for i := range response.Servers {
		if response.Servers[i].LanguageID == "json" {
			entry = &response.Servers[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("LSP status = %#v, missing protocol-only server", response.Servers)
	}
	if entry.State != "failed" || !entry.Available || entry.Ready || entry.VersionProbe != "unsupported" || entry.ProtocolProbe != "failed" || entry.CapabilityProbe != "failed" || len(entry.Capabilities) != 0 || entry.Hint == "" {
		t.Fatalf("failed protocol-only LSP status = %#v, want explicit protocol failure", *entry)
	}
}

func TestHeadlessLSPStatusReportsReadyAfterVersionProbe(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '%s\\n' 'fixture gopls 1.0'; exit 0; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"gopls": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[[lsp]]\nextensions = [\".go\"]\ncommand = \"gopls\"\nlanguage_id = \"go\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatus(t.TempDir())
	if err != nil {
		t.Fatalf("collect LSP status: %v", err)
	}
	var entry *headlessLSPEntry
	for i := range response.Servers {
		if response.Servers[i].Command == "gopls" {
			entry = &response.Servers[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("LSP status = %#v, missing gopls", response.Servers)
	}
	if entry.State != "available" || !entry.Available || !entry.Ready || entry.VersionProbe != "ready" || entry.Version == "" {
		t.Fatalf("gopls status = %#v, want ready version probe", *entry)
	}
}

func TestHeadlessLSPStatusForwardsConfiguredEnvironmentToVersionProbe(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\n[ \"$TEAK_LSP_VERSION_FIXTURE\" = ready ] || exit 7\nif [ \"$1\" = \"version\" ]; then printf '%s\\n' 'fixture gopls configured environment'; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"gopls": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[[lsp]]\nextensions = [\".go\"]\ncommand = \"gopls\"\nlanguage_id = \"go\"\nenv = { TEAK_LSP_VERSION_FIXTURE = \"ready\" }\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatus(t.TempDir())
	if err != nil {
		t.Fatalf("collect LSP status: %v", err)
	}
	var entry *headlessLSPEntry
	for i := range response.Servers {
		if response.Servers[i].Command == "gopls" {
			entry = &response.Servers[i]
			break
		}
	}
	if entry == nil {
		t.Fatalf("LSP status = %#v, missing gopls", response.Servers)
	}
	if entry.State != "available" || !entry.Ready || entry.VersionProbe != "ready" || entry.Version != "fixture gopls configured environment" {
		t.Fatalf("gopls status = %#v, want ready environment-aware version probe", *entry)
	}
}

func TestHeadlessLSPStatusDoesNotShareVersionProbeAcrossEnvironments(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' \"fixture-$TEAK_LSP_VERSION_FIXTURE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"gopls": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[[lsp]]
extensions = [".first"]
command = "gopls"
language_id = "first"
env = { TEAK_LSP_VERSION_FIXTURE = "first" }

[[lsp]]
extensions = [".second"]
command = "gopls"
language_id = "second"
env = { TEAK_LSP_VERSION_FIXTURE = "second" }
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := collectHeadlessLSPStatus(t.TempDir())
	if err != nil {
		t.Fatalf("collect LSP status: %v", err)
	}
	versions := make(map[string]string, len(response.Servers))
	for _, entry := range response.Servers {
		if entry.LanguageID == "first" || entry.LanguageID == "second" {
			if entry.State != "available" || !entry.Ready || entry.VersionProbe != "ready" {
				t.Fatalf("LSP status = %#v, want ready environment-specific probes", response.Servers)
			}
			versions[entry.LanguageID] = entry.Version
		}
	}
	if versions["first"] != "fixture-first" || versions["second"] != "fixture-second" {
		t.Fatalf("environment-specific versions = %#v, want distinct probe results", versions)
	}
}

func TestHeadlessLSPStatusProbesServersConcurrently(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	first := filepath.Join(t.TempDir(), "gopls")
	second := filepath.Join(t.TempDir(), "rust-analyzer")
	fixture := []byte("#!/bin/sh\nwhile :; do sleep 30; done\n")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, fixture, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	toolpath.Configure(map[string]string{"gopls": first, "rust-analyzer": second})
	t.Cleanup(func() { toolpath.Configure(nil) })

	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `[[lsp]]
extensions = [".first"]
command = "gopls"
language_id = "first"

[[lsp]]
extensions = [".second"]
command = "rust-analyzer"
language_id = "second"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	response, err := collectHeadlessLSPStatus(root)
	if err != nil {
		t.Fatalf("collect LSP status: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 3500*time.Millisecond {
		t.Fatalf("LSP status probes took %s, want concurrent shared-budget completion", elapsed)
	}
	entries := make(map[string]headlessLSPEntry, len(response.Servers))
	for _, entry := range response.Servers {
		entries[entry.Command] = entry
	}
	for _, command := range []string{"gopls", "rust-analyzer"} {
		entry, ok := entries[command]
		if !ok {
			t.Fatalf("LSP status missing %q: %#v", command, response.Servers)
		}
		if entry.State != "timed_out" || entry.VersionProbe != "timed_out" || entry.Ready {
			t.Fatalf("LSP status for %q = %#v, want timed-out probe", command, entry)
		}
	}
}

func TestHeadlessLSPStatusPreservesParentCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nwhile :; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"gopls": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[[lsp]]\nextensions = [\".go\"]\ncommand = \"gopls\"\nlanguage_id = \"go\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan struct {
		response headlessLSPResponse
		err      error
	}, 1)
	go func() {
		response, err := collectHeadlessLSPStatusContext(ctx, root)
		result <- struct {
			response headlessLSPResponse
			err      error
		}{response: response, err: err}
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("cancelled LSP status returned error: %v", got.err)
		}
		var entry headlessLSPEntry
		found := false
		for _, candidate := range got.response.Servers {
			if candidate.Command == "gopls" {
				entry = candidate
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("cancelled LSP status = %#v, missing gopls", got.response.Servers)
		}
		if entry.State != "cancelled" || entry.VersionProbe != "cancelled" || entry.Ready {
			t.Fatalf("cancelled LSP status entry = %#v, want cancelled probe", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled LSP status did not return promptly")
	}
}

func TestHeadlessLSPFormatPreviewAndApply(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.fixture")
	if err := os.WriteFile(file, []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("TEAK_HEADLESS_LSP_FIXTURE", "1")
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`[[lsp]]
extensions = [".fixture"]
command = %s
args = ["-test.run=^TestHeadlessLSPFixtureProcess$", "--"]
language_id = "fixture"
`, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	preview, err := collectHeadlessLSPFormat(root, file)
	if err != nil {
		t.Fatalf("collect format preview: %v", err)
	}
	if preview.State != "ready" || !preview.Changed || preview.Edits != 1 || preview.Content != "formatted source\n" {
		t.Fatalf("format preview = %#v", preview)
	}

	var stdout, stderr strings.Builder
	code := runHeadlessCLI([]string{
		"lsp", "format", "--json", "--apply", "--expected-sha256", preview.InputSHA256,
		"--root", root, "main.fixture",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("headless format apply exit code = %d, stderr=%s", code, stderr.String())
	}
	var applied headlessLSPFormatResponse
	if err := json.Unmarshal([]byte(stdout.String()), &applied); err != nil {
		t.Fatalf("decode applied format response: %v; stdout=%s", err, stdout.String())
	}
	if !applied.Applied || applied.OutputSHA256 == applied.InputSHA256 {
		t.Fatalf("applied format response = %#v", applied)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "formatted source\n" {
		t.Fatalf("formatted file = %q", data)
	}
}

func TestApplyHeadlessTextEditsRejectsOverlaps(t *testing.T) {
	_, err := applyHeadlessTextEdits("abcdef\n", []lsp.TextEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, NewText: "x"},
		{StartLine: 0, StartCol: 2, EndLine: 0, EndCol: 4, NewText: "y"},
	})
	if err == nil || !strings.Contains(err.Error(), "overlapping") {
		t.Fatalf("overlapping edits error = %v", err)
	}
}

func TestHeadlessLSPFixtureProcess(t *testing.T) {
	if os.Getenv("TEAK_HEADLESS_LSP_FIXTURE") != "1" || !strings.Contains(strings.Join(os.Args, " "), "-test.run=^TestHeadlessLSPFixtureProcess$") {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		body, err := readHeadlessLSPFixtureFrame(reader)
		if err != nil {
			return
		}
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &request) != nil {
			return
		}
		switch request.Method {
		case "initialize":
			if os.Getenv("TEAK_HEADLESS_LSP_BLOCK_INIT") == "1" {
				time.Sleep(5 * time.Second)
			}
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{
					"capabilities": map[string]any{
						"positionEncoding":           "utf-8",
						"textDocumentSync":           1,
						"documentFormattingProvider": true,
						"hoverProvider":              true,
						"definitionProvider":         true,
						"referencesProvider":         true,
						"documentSymbolProvider":     true,
					},
				},
			})
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if json.Unmarshal(request.Params, &params) != nil {
				return
			}
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"method":  "textDocument/publishDiagnostics",
				"params": map[string]any{
					"uri":     params.TextDocument.URI,
					"version": 1,
					"diagnostics": []map[string]any{{
						"severity": 1,
						"source":   "fixture",
						"message":  "fixture diagnostic",
						"range": map[string]any{
							"start": map[string]int{"line": 0, "character": 1},
							"end":   map[string]int{"line": 0, "character": 4},
						},
					}},
				},
			})
		case "textDocument/formatting":
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": []map[string]any{{
					"range": map[string]any{
						"start": map[string]int{"line": 0, "character": 0},
						"end":   map[string]int{"line": 0, "character": 14},
					},
					"newText": "formatted source",
				}},
			})
		case "textDocument/hover":
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{
					"contents": map[string]string{"kind": "plaintext", "value": "fixture hover"},
				},
			})
		case "textDocument/definition":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if json.Unmarshal(request.Params, &params) != nil {
				return
			}
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": []map[string]any{{
					"uri": params.TextDocument.URI,
					"range": map[string]any{
						"start": map[string]int{"line": 0, "character": 0},
						"end":   map[string]int{"line": 0, "character": 14},
					},
				}},
			})
		case "textDocument/references":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if json.Unmarshal(request.Params, &params) != nil {
				return
			}
			locations := []map[string]any{{
				"uri": params.TextDocument.URI,
				"range": map[string]any{
					"start": map[string]int{"line": 0, "character": 0},
					"end":   map[string]int{"line": 0, "character": 14},
				},
			}}
			if external := os.Getenv("TEAK_LSP_EXTERNAL_PATH"); external != "" {
				locations = append(locations, map[string]any{
					"uri": lsp.FileURI(external),
					"range": map[string]any{
						"start": map[string]int{"line": 2, "character": 1},
						"end":   map[string]int{"line": 2, "character": 5},
					},
				})
			}
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result":  locations,
			})
		case "textDocument/documentSymbol":
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": []map[string]any{{
					"name": "Fixture",
					"kind": 12,
					"range": map[string]any{
						"start": map[string]int{"line": 0, "character": 0},
						"end":   map[string]int{"line": 0, "character": 14},
					},
					"selectionRange": map[string]any{
						"start": map[string]int{"line": 0, "character": 0},
						"end":   map[string]int{"line": 0, "character": 7},
					},
				}},
			})
		case "shutdown":
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result":  nil,
			})
		case "exit":
			return
		}
	}
}

func TestDoctorProtocolProbeFixtureProcess(t *testing.T) {
	if os.Getenv("TEAK_DOCTOR_LSP_FIXTURE") != "1" || !strings.Contains(strings.Join(os.Args, " "), "-test.run=^TestDoctorProtocolProbeFixtureProcess$") {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		body, err := readHeadlessLSPFixtureFrame(reader)
		if err != nil {
			return
		}
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &request) != nil {
			return
		}
		switch request.Method {
		case "initialize":
			if marker := os.Getenv("TEAK_LSP_PROBE_MARKER"); marker != "" {
				if file, err := os.OpenFile(marker, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
					_, _ = io.WriteString(file, "probe\n")
					_ = file.Close()
				}
			}
			capabilities := map[string]any{
				"positionEncoding": "utf-8",
				"textDocumentSync": 1,
			}
			if os.Getenv("TEAK_LSP_CAPABILITIES") == "1" {
				for name, value := range map[string]any{
					"documentFormattingProvider": true,
					"hoverProvider":              true,
					"definitionProvider":         true,
					"referencesProvider":         true,
					"documentSymbolProvider":     true,
				} {
					capabilities[name] = value
				}
			}
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result": map[string]any{
					"capabilities": capabilities,
				},
			})
		case "shutdown":
			sendHeadlessLSPFixtureMessage(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result":  nil,
			})
		case "exit":
			return
		}
	}
}

func readHeadlessLSPFixtureFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			contentLength, err = strconv.Atoi(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]))
			if err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func sendHeadlessLSPFixtureMessage(message any) {
	body, err := json.Marshal(message)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(body))
	_, _ = os.Stdout.Write(body)
}
