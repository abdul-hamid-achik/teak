package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	agentruntime "teak/internal/agent/runtime"
	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/session"
	"teak/internal/toolpath"
)

func TestLSPConfigsFromConfigPreservesEnvironment(t *testing.T) {
	cfg := config.Config{LSP: []config.LSPConfig{{
		Extensions: []string{".fixture"},
		Command:    "fixture-lsp",
		LanguageID: "fixture",
		Env:        map[string]string{"TEAK_FIXTURE_MODE": "1"},
	}}}
	configs := lspConfigsFromConfig(cfg)
	var fixture *lsp.ServerConfig
	for index := range configs {
		if configs[index].LanguageID == "fixture" {
			fixture = &configs[index]
			break
		}
	}
	if fixture == nil || fixture.Env["TEAK_FIXTURE_MODE"] != "1" {
		t.Fatalf("converted LSP configs = %#v, want environment preserved", configs)
	}
}

func TestConfigureToolpathFromConfigUsesToolOverrides(t *testing.T) {
	configHome := t.TempDir()
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(t.TempDir(), "codemap")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(fmt.Sprintf("[tools]\ncodemap = %q\n", fixture)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Cleanup(func() { toolpath.Configure(nil) })

	if err := configureToolpathFromConfig(); err != nil {
		t.Fatalf("configureToolpathFromConfig() error = %v", err)
	}
	if got, err := toolpath.Resolve("codemap"); err != nil || got != fixture {
		t.Fatalf("configured codemap = %q, error = %v; want %q", got, err, fixture)
	}
}

func TestHandleCLI(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantPaths   []string
		wantLines   []int // parallel to paths; -1 = unset
		wantHandled bool
		wantOutput  string
		wantErr     bool
		wantErrPart string
	}{
		{
			name: "no arguments starts editor",
		},
		{
			name:      "file argument starts editor",
			args:      []string{"notes.md"},
			wantPaths: []string{"notes.md"},
			wantLines: []int{-1},
		},
		{
			name:      "multiple files open as tabs",
			args:      []string{"notes.md", "readme.md"},
			wantPaths: []string{"notes.md", "readme.md"},
			wantLines: []int{-1, -1},
		},
		{
			name:        "long version flag exits early",
			args:        []string{"--version"},
			wantHandled: true,
			wantOutput:  "teak 1.2.3-test\n",
		},
		{
			name:        "short version flag exits early",
			args:        []string{"-v"},
			wantHandled: true,
			wantOutput:  "teak 1.2.3-test\n",
		},
		{
			name:        "help flag exits early",
			args:        []string{"--help"},
			wantHandled: true,
			wantOutput:  "Usage:",
		},
		{
			name:        "short help flag exits early",
			args:        []string{"-h"},
			wantHandled: true,
			wantOutput:  "Usage:",
		},
		{
			name:    "unknown long flag is rejected",
			args:    []string{"--verbose"},
			wantErr: true,
		},
		{
			name:    "unknown short flag is rejected",
			args:    []string{"-x"},
			wantErr: true,
		},
		{
			name:      "plus line before file",
			args:      []string{"+10", "main.go"},
			wantPaths: []string{"main.go"},
			wantLines: []int{9},
		},
		{
			name:      "path colon line",
			args:      []string{"main.go:42"},
			wantPaths: []string{"main.go"},
			wantLines: []int{41},
		},
		{
			name:      "path colon line colon col",
			args:      []string{"main.go:42:7"},
			wantPaths: []string{"main.go"},
			wantLines: []int{41},
		},
		{
			name:        "plus line without file errors",
			args:        []string{"+5"},
			wantErr:     true,
			wantErrPart: "+line",
		},
		{
			name:        "conflicting line specs error",
			args:        []string{"+5", "main.go:10"},
			wantErr:     true,
			wantErrPart: "conflicting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer

			targets, handled, err := handleCLI(tt.args, &stdout, "1.2.3-test")

			if (err != nil) != tt.wantErr {
				t.Fatalf("handleCLI() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErrPart != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrPart)) {
				t.Fatalf("handleCLI() error = %v, want containing %q", err, tt.wantErrPart)
			}
			if handled != tt.wantHandled {
				t.Errorf("handleCLI() handled = %v, want %v", handled, tt.wantHandled)
			}
			if tt.wantOutput != "" && !strings.Contains(stdout.String(), tt.wantOutput) {
				t.Errorf("handleCLI() output = %q, want containing %q", stdout.String(), tt.wantOutput)
			}
			if len(targets) != len(tt.wantPaths) {
				t.Fatalf("handleCLI() paths = %v, want %v", targets, tt.wantPaths)
			}
			for i := range tt.wantPaths {
				if targets[i].path != tt.wantPaths[i] {
					t.Errorf("path[%d] = %q, want %q", i, targets[i].path, tt.wantPaths[i])
				}
				if i < len(tt.wantLines) && targets[i].line != tt.wantLines[i] {
					t.Errorf("line[%d] = %d, want %d", i, targets[i].line, tt.wantLines[i])
				}
			}
		})
	}
}

func TestDoctorCommandParsing(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    doctorOptions
		help    bool
		wantErr string
	}{
		{name: "defaults"},
		{name: "json and root", args: []string{"--json", "--root", "/tmp/project"}, want: doctorOptions{json: true, root: "/tmp/project"}},
		{name: "help", args: []string{"--help"}, help: true},
		{name: "missing root", args: []string{"--root"}, wantErr: "requires a directory"},
		{name: "unknown option", args: []string{"--verbose"}, wantErr: "unknown doctor option"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, help, err := parseDoctorArgs(tt.args)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("parseDoctorArgs() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("parseDoctorArgs() error = %v, want containing %q", err, tt.wantErr)
			}
			if help != tt.help {
				t.Fatalf("help = %v, want %v", help, tt.help)
			}
			if got != tt.want {
				t.Fatalf("options = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDoctorWorkspace(t *testing.T) {
	dir := t.TempDir()
	got, err := doctorWorkspace(dir)
	if err != nil {
		t.Fatalf("doctorWorkspace() error = %v", err)
	}
	if got != dir {
		t.Fatalf("doctorWorkspace() = %q, want %q", got, dir)
	}

	file := filepath.Join(dir, "file.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := doctorWorkspace(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("doctorWorkspace(file) error = %v, want not-a-directory error", err)
	}
}

func TestDoctorJSONReportIsMachineReadable(t *testing.T) {
	dir := t.TempDir()
	report := collectDoctor(dir, "test", toolpath.New(nil))
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal doctor report: %v", err)
	}
	var decoded doctorReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal doctor report: %v", err)
	}
	if decoded.Version != "test" || decoded.Workspace != dir {
		t.Fatalf("decoded report = %#v", decoded)
	}
	if len(decoded.Checks) == 0 {
		t.Fatal("doctor report has no checks")
	}
}

func TestCollectDoctorDefaultsNilResolver(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("collectDoctor(nil resolver) panicked: %v", recovered)
		}
	}()

	report := collectDoctor(t.TempDir(), "test", nil)
	if len(report.Checks) == 0 {
		t.Fatal("collectDoctor(nil resolver) returned no checks")
	}
}

func TestDoctorReportsInvalidConfigurationAsFailure(t *testing.T) {
	root := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[editor]\ntab_size = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := collectDoctor(root, "test", toolpath.New(nil))
	for _, check := range report.Checks {
		if check.Name != "config" {
			continue
		}
		if check.Status != "fail" || !strings.Contains(check.Detail, "tab_size") {
			t.Fatalf("config check = %#v, want validation failure", check)
		}
		return
	}
	t.Fatal("doctor report did not include a config check")
}

func TestDoctorReportsStructuredInstallActionForMissingLanguageServer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.fixture"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[[lsp]]\nextensions = [\".fixture\"]\ncommand = \"teak-missing-onboarding-lsp\"\nlanguage_id = \"fixture\"\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	report := collectDoctor(root, "test", toolpath.New(nil))
	for _, action := range report.Actions {
		if action.Component != "lsp" || action.Name != "fixture" {
			continue
		}
		if action.State != "missing" || action.Action != "install" || action.Hint == "" {
			t.Fatalf("doctor action = %#v, want actionable missing-LSP install action", action)
		}
		return
	}
	t.Fatalf("doctor actions = %#v, want missing fixture LSP install action", report.Actions)
}

func TestDoctorActionsClassifyFailedProbeAndDeduplicate(t *testing.T) {
	actions := buildDoctorActions(
		[]doctorCheck{
			{Name: "tool:gopls", Status: "warn", Detail: "/tmp/gopls; version probe failed: exit status 7"},
			{Name: "tool:gopls", Status: "warn", Detail: "/tmp/gopls; version probe failed: exit status 7"},
		},
		[]doctorLanguage{{LanguageID: "go", Server: "gopls", State: "failed", Files: 2, Hint: "repair gopls"}},
	)
	toolActions := 0
	for _, action := range actions {
		if action.Component == "tool" && action.Name == "gopls" {
			toolActions++
			if action.State != "failed" || action.Action != "repair" {
				t.Fatalf("tool action = %#v, want failed repair action", action)
			}
		}
	}
	if toolActions != 1 {
		t.Fatalf("gopls tool actions = %d, want one deduplicated action: %#v", toolActions, actions)
	}
	for _, action := range actions {
		if action.Component == "lsp" && action.Name == "go" {
			if action.State != "failed" || action.Action != "repair" || action.Hint == "" {
				t.Fatalf("LSP action = %#v, want failed repair action", action)
			}
			return
		}
	}
	t.Fatalf("actions = %#v, want failed Go LSP repair action", actions)
}

func TestDoctorActionsClassifyTimedOutProbe(t *testing.T) {
	actions := buildDoctorActions(
		[]doctorCheck{{Name: "tool:gopls", Status: "warn", Detail: "/tmp/gopls; version probe timed out: context deadline exceeded"}},
		[]doctorLanguage{{LanguageID: "go", Server: "gopls", State: "timed_out", Files: 1, Hint: "retry doctor"}},
	)
	toolFound, lspFound := false, false
	for _, action := range actions {
		switch {
		case action.Component == "tool" && action.Name == "gopls":
			toolFound = true
			if action.State != "timed_out" || action.Action != "repair" {
				t.Fatalf("tool action = %#v, want timed_out repair action", action)
			}
		case action.Component == "lsp" && action.Name == "go":
			lspFound = true
			if action.State != "timed_out" || action.Action != "repair" || action.Hint == "" {
				t.Fatalf("LSP action = %#v, want timed_out repair action", action)
			}
		}
	}
	if !toolFound || !lspFound {
		t.Fatalf("actions = %#v, want tool and LSP timeout actions", actions)
	}
}

func TestDoctorReportsResolvedToolVersion(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "codemap")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'codemap version fixture' ;;\nstructural-manifest) printf '%s\\n' 'structural-manifest help' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := collectDoctor(root, "test", toolpath.New(map[string]string{"codemap": fixture}))
	for _, check := range report.Checks {
		if check.Name != "tool:codemap" {
			continue
		}
		if check.Status != "pass" || check.Version != "codemap version fixture" || check.Capability != "structural-manifest" || !strings.Contains(check.Detail, fixture) {
			t.Fatalf("codemap check = %#v, want passing version probe", check)
		}
		return
	}
	t.Fatal("doctor report did not include tool:codemap")
}

func TestDoctorReportsVecgrepCapabilityGap(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'vecgrep version 2.22.0' ;;\nstatus) printf '%s\\n' 'Usage: vecgrep status' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := collectDoctor(root, "test", toolpath.New(map[string]string{"vecgrep": fixture}))
	for _, check := range report.Checks {
		if check.Name != "tool:vecgrep" {
			continue
		}
		if check.Status != "warn" || check.Capability != "" || !strings.Contains(check.Detail, "capability probe failed") || !strings.Contains(check.Hint, "status --lightweight") {
			t.Fatalf("vecgrep check = %#v, want an actionable capability warning", check)
		}
		for _, action := range report.Actions {
			if action.Component == "tool" && action.Name == "vecgrep" {
				if action.State != "unsupported" || action.Action != "upgrade" {
					t.Fatalf("vecgrep action = %#v, want unsupported upgrade action", action)
				}
				return
			}
		}
		t.Fatalf("doctor actions = %#v, want vecgrep upgrade action", report.Actions)
	}
	t.Fatal("doctor report did not include tool:vecgrep")
}

func TestDoctorReportsVecgrepLightweightCapability(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "vecgrep")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'vecgrep version fixture' ;;\nstatus) printf '%s\\n' 'Usage: vecgrep status --lightweight' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report := collectDoctor(root, "test", toolpath.New(map[string]string{"vecgrep": fixture}))
	for _, check := range report.Checks {
		if check.Name != "tool:vecgrep" {
			continue
		}
		if check.Status != "pass" || check.Capability != "lightweight-status" || check.Version != "vecgrep version fixture" {
			t.Fatalf("vecgrep check = %#v, want verified lightweight capability", check)
		}
		return
	}
	t.Fatal("doctor report did not include tool:vecgrep")
}

func TestDoctorForwardsConfiguredEnvironmentToLanguageProbe(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\n[ \"$TEAK_LSP_VERSION_FIXTURE\" = ready ] || exit 7\nif [ \"$1\" = \"version\" ]; then printf '%s\\n' 'fixture gopls configured environment'; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	report := collectDoctor(root, "test", toolpath.New(map[string]string{"gopls": fixture}))
	var language *doctorLanguage
	for i := range report.Languages {
		if report.Languages[i].LanguageID == "go" {
			language = &report.Languages[i]
			break
		}
	}
	if language == nil || language.State != "available" || language.VersionProbe != "ready" || language.Version != "fixture gopls configured environment" {
		t.Fatalf("doctor languages = %#v, want environment-aware ready probe", report.Languages)
	}
}

func TestDoctorToolProbeClassifiesInternalDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := filepath.Join(t.TempDir(), "codemap")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\n/bin/sleep 10\nprintf '%s\\n' 'too late'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	checks := []doctorCheck{{Name: "tool:codemap", Status: "pass", Detail: fixture}}
	probeDoctorTools(t.Context(), toolpath.New(map[string]string{"codemap": fixture}), checks, []doctorToolProbe{{Index: 0, Name: "codemap"}})

	if checks[0].Status != "warn" || !strings.Contains(checks[0].Detail, "version probe timed out") {
		t.Fatalf("doctor check = %#v, want a warning with a timed-out probe", checks[0])
	}
}

func TestHeadlessToolStatusReportsVersionProbe(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'hitspec version fixture' ;;\nvalidate) printf '%s\\n' 'validate hitspec files' ;;\n*) exit 2 ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectSimpleToolStatus("hitspec")
	if status.State != "available" || !status.Ready || status.Version != "hitspec version fixture" || status.VersionProbe != "ready" || status.Mode != "validate-contract" || status.Capability != "validate" || status.CapabilityProbe != "ready" {
		t.Fatalf("headless hitspec status = %#v, want verified version", status)
	}
}

func TestHeadlessToolStatusReportsHitspecCapabilityGap(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '%s\\n' 'hitspec version fixture'; exit 0; fi\nprintf '%s\\n' 'unknown command: validate' >&2\nexit 2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectSimpleToolStatus("hitspec")
	if status.State != "unsupported" || status.Ready || status.VersionProbe != "ready" || status.CapabilityProbe != "failed" || status.Capability != "validate" {
		t.Fatalf("headless hitspec status = %#v, want unsupported validate capability", status)
	}
	if status.Hint == "" || !strings.Contains(status.Detail, "capability probe failed") {
		t.Fatalf("headless hitspec status = %#v, want actionable capability failure", status)
	}
}

func TestHeadlessToolStatusPreservesHitspecCapabilityTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'hitspec version fixture' ;;\nvalidate) /bin/sleep 10 ;;\n*) exit 2 ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status := collectSimpleToolStatusContext(ctx, "hitspec")
	if status.State != "timed_out" || status.Ready || status.VersionProbe != "ready" || status.CapabilityProbe != "timed_out" {
		t.Fatalf("headless hitspec status = %#v, want timed-out capability probe", status)
	}
}

func TestDoctorReportsHitspecCapabilityGap(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'hitspec version fixture' ;;\nvalidate) printf '%s\\n' 'unknown command: validate' >&2; exit 2 ;;\n*) exit 2 ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	report := collectDoctor(root, "test", toolpath.New(map[string]string{"hitspec": fixture}))
	for _, check := range report.Checks {
		if check.Name != "tool:hitspec" {
			continue
		}
		if check.Status != "warn" || check.Capability != "" || !strings.Contains(check.Detail, "capability probe failed") || !strings.Contains(check.Hint, "validate") {
			t.Fatalf("hitspec check = %#v, want actionable capability warning", check)
		}
		return
	}
	t.Fatal("doctor report did not include tool:hitspec")
}

func TestDoctorReportsHitspecValidateCapability(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\ncase \"$1\" in\n--version) printf '%s\\n' 'hitspec version fixture' ;;\nvalidate) printf '%s\\n' 'Validate hitspec files for syntax errors.' ;;\n*) exit 2 ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	report := collectDoctor(root, "test", toolpath.New(map[string]string{"hitspec": fixture}))
	for _, check := range report.Checks {
		if check.Name != "tool:hitspec" {
			continue
		}
		if check.Status != "pass" || check.Capability != "validate" || check.Version != "hitspec version fixture" {
			t.Fatalf("hitspec check = %#v, want verified validate capability", check)
		}
		return
	}
	t.Fatal("doctor report did not include tool:hitspec")
}

func TestHeadlessToolStatusDoesNotClaimReadyAfterVersionProbeFailure(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then printf '%s\\n' 'broken fixture' >&2; exit 7; fi\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectSimpleToolStatus("hitspec")
	if status.State != "failed" || status.Ready || status.VersionProbe != "failed" {
		t.Fatalf("headless hitspec status = %#v, want failed and not ready", status)
	}
	if !strings.Contains(status.Detail, "version probe failed") {
		t.Fatalf("headless hitspec detail = %q, want version probe failure", status.Detail)
	}
}

func TestHeadlessToolStatusPreservesCancelledProbe(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "hitspec")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' 'hitspec fixture'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"hitspec": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status := collectSimpleToolStatusContext(ctx, "hitspec")
	if status.State != "cancelled" || status.Ready || status.VersionProbe != "cancelled" {
		t.Fatalf("cancelled hitspec status = %#v, want cancelled and not ready", status)
	}
}

func TestHeadlessSimpleToolStatusDoesNotClaimReadyWithoutVersionProbe(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "custom-tool")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolpath.Configure(map[string]string{"custom-tool": fixture})
	t.Cleanup(func() { toolpath.Configure(nil) })

	status := collectSimpleToolStatus("custom-tool")
	if status.State != "unsupported" || status.Ready || !status.Available || status.VersionProbe != "unsupported" {
		t.Fatalf("headless custom tool status = %#v, want unsupported and not ready", status)
	}
	if status.Hint == "" || !strings.Contains(status.Detail, "version probe") {
		t.Fatalf("headless custom tool status = %#v, want actionable probe detail", status)
	}
}

func TestDetectDoctorLanguagesReportsRelevantServers(t *testing.T) {
	root := t.TempDir()
	for path, contents := range map[string]string{
		"main.go":                 "package main\n",
		"tool.py":                 "print('ok')\n",
		"vendor/ignored.go":       "package ignored\n",
		"node_modules/ignored.py": "print('ignored')\n",
		"target/ignored.py":       "print('ignored')\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then printf '%s\\n' 'fixture-lsp 1.0'; else exit 0; fi\n"), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got := detectDoctorLanguages(root, []lsp.ServerConfig{
		{Extensions: []string{".go"}, Command: "gopls", LanguageID: "go"},
		{Extensions: []string{".py"}, Command: "pylsp", LanguageID: "python"},
	}, toolpath.New(map[string]string{"gopls": fixture}))
	if len(got) != 2 {
		t.Fatalf("detected languages = %#v, want Go and Python", got)
	}
	if got[0].LanguageID != "go" || got[0].Files != 1 || got[0].State != "available" || got[0].Path != fixture {
		t.Fatalf("Go language = %#v, want available fixture server and one file", got[0])
	}
	if got[1].LanguageID != "python" || got[1].Files != 1 || got[1].State != "missing" {
		t.Fatalf("Python language = %#v, want one file and missing server", got[1])
	}
	if got[1].Hint == "" {
		t.Fatal("missing Python server has no actionable hint")
	}
}

func TestDoctorToolCheckReusesFailedProtocolProbe(t *testing.T) {
	language := doctorLanguage{
		LanguageID:    "python",
		Server:        "pylsp",
		State:         "failed",
		VersionProbe:  "unsupported",
		ProtocolProbe: "failed",
		Hint:          "language server handshake failed",
	}
	check := doctorCheck{Name: "tool:pylsp", Status: "pass", Detail: "/tmp/pylsp"}

	matched, ok := doctorLanguageProbeForTool([]doctorLanguage{language}, "pylsp")
	if !ok {
		t.Fatal("doctorLanguageProbeForTool() did not reuse a failed protocol probe")
	}
	applyDoctorLanguageProbe(&check, matched)
	if check.Status != "warn" || !strings.Contains(check.Detail, "protocol probe failed") {
		t.Fatalf("tool check = %#v, want warning from failed protocol probe", check)
	}
}

func TestDetectDoctorLanguagesReportsBrokenResolvedServer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' 'fixture failed' >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := detectDoctorLanguages(root, []lsp.ServerConfig{
		{Extensions: []string{".go"}, Command: "gopls", LanguageID: "go"},
	}, toolpath.New(map[string]string{"gopls": fixture}))
	if len(got) != 1 {
		t.Fatalf("detected languages = %#v, want one language", got)
	}
	if got[0].State != "failed" || got[0].Path != fixture || got[0].Hint == "" {
		t.Fatalf("broken language = %#v, want failed state, resolved path, and hint", got[0])
	}
}

func TestDetectDoctorLanguagesReportsTimedOutResolvedServer(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixture := filepath.Join(t.TempDir(), "gopls")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, _ := detectDoctorLanguagesContext(ctx, root, []lsp.ServerConfig{
		{Extensions: []string{".go"}, Command: "gopls", LanguageID: "go"},
	}, toolpath.New(map[string]string{"gopls": fixture}))
	if len(got) != 1 {
		t.Fatalf("detected languages = %#v, want one language", got)
	}
	if got[0].State != "timed_out" || got[0].VersionProbe != "timed_out" || got[0].Hint == "" {
		t.Fatalf("timed out language = %#v, want timed_out state and probe", got[0])
	}
}

func TestDetectDoctorLanguagesReportsProtocolFailureForResolvedServerWithoutVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tool.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixture := filepath.Join(t.TempDir(), "pylsp")
	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := detectDoctorLanguages(root, []lsp.ServerConfig{
		{Extensions: []string{".py"}, Command: "pylsp", LanguageID: "python"},
	}, toolpath.New(map[string]string{"pylsp": fixture}))
	if len(got) != 1 {
		t.Fatalf("detected languages = %#v, want one language", got)
	}
	if got[0].State != "failed" || got[0].VersionProbe != "unsupported" || got[0].ProtocolProbe != "failed" || got[0].Hint == "" {
		t.Fatalf("unknown-probe language = %#v, want explicit protocol probe failure", got[0])
	}
}

func TestDetectDoctorLanguagesVerifiesInstalledYAMLAndLuaServers(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"config.yaml": "name: teak\n",
		"init.lua":    "return {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	overrides := make(map[string]string, 2)
	for _, name := range []string{"yaml-language-server", "lua-language-server"} {
		fixture := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(fixture, []byte("#!/bin/sh\nprintf '%s\\n' 'fixture-"+name+" 1.0'\n"), 0o755); err != nil {
			t.Fatalf("write %s fixture: %v", name, err)
		}
		overrides[name] = fixture
	}

	got := detectDoctorLanguages(root, []lsp.ServerConfig{
		{Extensions: []string{".yaml"}, Command: "yaml-language-server", LanguageID: "yaml"},
		{Extensions: []string{".lua"}, Command: "lua-language-server", LanguageID: "lua"},
	}, toolpath.New(overrides))
	if len(got) != 2 {
		t.Fatalf("detected languages = %#v, want YAML and Lua", got)
	}
	for _, language := range got {
		if language.State != "available" || language.VersionProbe != "ready" || language.Version == "" {
			t.Fatalf("language = %#v, want available language with verified version", language)
		}
	}
}

func TestDetectDoctorLanguagesUsesProtocolProbeWithoutVersionCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := detectDoctorLanguages(root, []lsp.ServerConfig{
		{
			Extensions: []string{".json"},
			Command:    os.Args[0],
			Args:       []string{"-test.run=^TestDoctorProtocolProbeFixtureProcess$", "--"},
			LanguageID: "json",
			Env:        map[string]string{"TEAK_DOCTOR_LSP_FIXTURE": "1", "TEAK_LSP_CAPABILITIES": "1"},
		},
	}, toolpath.New(nil))
	if len(got) != 1 {
		t.Fatalf("detected languages = %#v, want one language", got)
	}
	language := got[0]
	wantCapabilities := []string{"hover", "definition", "references", "document_symbols", "formatting"}
	if language.State != "available" || language.VersionProbe != "unsupported" || language.ProtocolProbe != "ready" || language.CapabilityProbe != "ready" || !reflect.DeepEqual(language.Capabilities, wantCapabilities) {
		t.Fatalf("language = %#v, want available protocol-verified server without version", language)
	}
}

func TestDetectDoctorLanguagesProbesKnownServersConcurrently(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"main.go": "package main\n",
		"main.c":  "int main(void) { return 0; }\n",
		"main.rs": "fn main() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	markerDir := t.TempDir()
	servers := []struct {
		name, extension, language string
	}{
		{name: "gopls", extension: ".go", language: "go"},
		{name: "clangd", extension: ".c", language: "c"},
		{name: "rust-analyzer", extension: ".rs", language: "rust"},
	}
	markers := make([]string, len(servers))
	for i, server := range servers {
		markers[i] = filepath.Join(markerDir, server.name+".started")
	}
	overrides := make(map[string]string, len(servers))
	for i, server := range servers {
		fixture := filepath.Join(t.TempDir(), server.name)
		conditions := make([]string, 0, len(markers))
		for _, marker := range markers {
			conditions = append(conditions, fmt.Sprintf("[ ! -f %q ]", marker))
		}
		waitForAll := strings.Join(conditions, " || ")
		// Each fixture waits until every sibling has started. A sequential
		// implementation deadlocks until the shared context expires; a
		// concurrent implementation crosses the barrier and returns normally.
		script := fmt.Sprintf("#!/bin/sh\ntouch %q\nwhile %s; do sleep 0.01; done\nprintf 'fixture-%s\\n'\n", markers[i], waitForAll, server.name)
		if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
			t.Fatalf("write %s fixture: %v", server.name, err)
		}
		overrides[server.name] = fixture
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	configs := make([]lsp.ServerConfig, 0, len(servers))
	for _, server := range servers {
		configs = append(configs, lsp.ServerConfig{
			Extensions: []string{server.extension}, Command: server.name, LanguageID: server.language,
		})
	}
	got, _ := detectDoctorLanguagesContext(ctx, root, configs, toolpath.New(overrides))
	if len(got) != len(servers) {
		t.Fatalf("detected languages = %#v, want %d entries", got, len(servers))
	}
	for _, language := range got {
		if language.State != "available" || language.VersionProbe != "ready" {
			t.Fatalf("language = %#v, want successful concurrent probe", language)
		}
	}
}

func TestDetectDoctorLanguagesReusesProbeForSharedServer(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"main.c":   "int main(void) { return 0; }\n",
		"main.cpp": "int main() { return 0; }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	countPath := filepath.Join(t.TempDir(), "clangd.probes")
	fixture := filepath.Join(t.TempDir(), "clangd")
	script := fmt.Sprintf("#!/bin/sh\nprintf 'probe\\n' >> %q\nprintf 'clangd fixture 1.0\\n'\n", countPath)
	if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
		t.Fatalf("write clangd fixture: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	report := collectDoctor(root, "test", toolpath.New(map[string]string{"clangd": fixture}))
	if len(report.Languages) != 2 {
		t.Fatalf("detected languages = %#v, want C and C++", report.Languages)
	}
	for _, language := range report.Languages {
		if language.State != "available" || language.VersionProbe != "ready" {
			t.Fatalf("language = %#v, want verified shared server", language)
		}
	}
	data, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatalf("read probe count: %v", err)
	}
	if got := strings.Count(string(data), "probe\n"); got != 1 {
		t.Fatalf("clangd version probe count = %d, want one shared probe", got)
	}
}

func TestDoctorToolProbesRunConcurrently(t *testing.T) {
	markerDir := t.TempDir()
	names := []string{"git", "rg", "codemap"}
	markers := make([]string, len(names))
	for i, name := range names {
		markers[i] = filepath.Join(markerDir, name+".started")
	}
	overrides := make(map[string]string, len(names))
	for i, name := range names {
		fixture := filepath.Join(t.TempDir(), name)
		conditions := make([]string, 0, len(markers))
		for _, marker := range markers {
			conditions = append(conditions, fmt.Sprintf("[ ! -f %q ]", marker))
		}
		waitForAll := strings.Join(conditions, " || ")
		script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = \"structural-manifest\" ]; then printf 'structural-manifest help\\n'; exit 0; fi\ntouch %q\nwhile %s; do sleep 0.01; done\nprintf 'fixture-%s\\n'\n", markers[i], waitForAll, name)
		if err := os.WriteFile(fixture, []byte(script), 0o755); err != nil {
			t.Fatalf("write %s fixture: %v", name, err)
		}
		overrides[name] = fixture
	}

	checks := make([]doctorCheck, len(names))
	probes := make([]doctorToolProbe, len(names))
	for i, name := range names {
		checks[i] = doctorCheck{Name: "tool:" + name, Status: "pass", Detail: overrides[name]}
		probes[i] = doctorToolProbe{Index: i, Name: name}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	probeDoctorTools(ctx, toolpath.New(overrides), checks, probes)
	for _, check := range checks {
		if check.Status != "pass" || check.Version != "fixture-"+strings.TrimPrefix(check.Name, "tool:") {
			t.Fatalf("tool check = %#v, want successful concurrent probe", check)
		}
	}
}

func TestDetectDoctorLanguagesHonorsCanceledScanContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, scan := detectDoctorLanguagesContext(ctx, root, []lsp.ServerConfig{
		{Extensions: []string{".go"}, Command: "gopls", LanguageID: "go"},
	}, toolpath.New(nil))
	if len(got) != 0 {
		t.Fatalf("detected languages = %#v, want no results after cancellation", got)
	}
	if !scan.Truncated || scan.ScannedFiles != 0 {
		t.Fatalf("language scan = %#v, want canceled/truncated scan with no files", scan)
	}
}

func TestDetectDoctorLanguagesReportsDepthTruncation(t *testing.T) {
	root := t.TempDir()
	deep := root
	for i := 0; i <= doctorMaxScanDepth; i++ {
		deep = filepath.Join(deep, fmt.Sprintf("level-%02d", i))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "deep.go"), []byte("package deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, scan := detectDoctorLanguagesContext(context.Background(), root, []lsp.ServerConfig{
		{Extensions: []string{".go"}, Command: "gopls", LanguageID: "go"},
	}, toolpath.New(nil))
	if len(got) != 0 {
		t.Fatalf("detected languages = %#v, want deep file outside scan budget", got)
	}
	if !scan.Truncated || scan.ScannedFiles != 0 {
		t.Fatalf("language scan = %#v, want depth-truncated scan with no files", scan)
	}
}

func TestHeadlessArgs(t *testing.T) {
	opts, positional, help, err := parseHeadlessArgs([]string{"--json", "--root", "/tmp/project", "--regex", "query"})
	if err != nil {
		t.Fatalf("parseHeadlessArgs() error = %v", err)
	}
	if help || !opts.json || !opts.regex || opts.root != "/tmp/project" {
		t.Fatalf("options = %#v, help=%v", opts, help)
	}
	if len(positional) != 1 || positional[0] != "query" {
		t.Fatalf("positional = %#v", positional)
	}

	if _, _, _, err := parseHeadlessArgs([]string{"--unknown"}); err == nil {
		t.Fatal("parseHeadlessArgs() accepted unknown option")
	}
}

func TestReadHeadlessBufferIsWorkspaceBounded(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "main.go")
	if err := os.WriteFile(inside, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write buffer: %v", err)
	}
	response, err := readHeadlessBuffer(root, inside)
	if err != nil {
		t.Fatalf("readHeadlessBuffer() error = %v", err)
	}
	if response.RelativePath != "main.go" || response.Lines != 3 || response.Content == "" || response.SHA256 == "" {
		t.Fatalf("response = %#v", response)
	}

	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0o644); err != nil {
		t.Fatalf("write outside buffer: %v", err)
	}
	if _, err := readHeadlessBuffer(root, outside); err == nil || !strings.Contains(err.Error(), "outside workspace") {
		t.Fatalf("outside read error = %v, want workspace boundary error", err)
	}
}

func TestWriteHeadlessBufferRequiresCurrentSHA256(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatalf("write buffer: %v", err)
	}
	current, err := readHeadlessBuffer(root, path)
	if err != nil {
		t.Fatalf("read current buffer: %v", err)
	}
	updated, err := writeHeadlessBuffer(root, path, current.SHA256, []byte("after\n"))
	if err != nil {
		t.Fatalf("writeHeadlessBuffer() error = %v", err)
	}
	if updated.Content != "after\n" {
		t.Fatalf("updated content = %q", updated.Content)
	}
	if _, err := writeHeadlessBuffer(root, path, current.SHA256, []byte("stale\n")); err == nil || !strings.Contains(err.Error(), "changed since it was read") {
		t.Fatalf("stale write error = %v, want optimistic-concurrency error", err)
	}
}

func TestHeadlessJSONErrorsHaveStableSchema(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI(
		[]string{"buffer", "write", "--json", "--root", root, "main.go"},
		strings.NewReader(""), &stdout, &stderr,
	)
	if code != 2 {
		t.Fatalf("runHeadlessCLI() code = %d, want 2; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var response struct {
		State   string `json:"state"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
		t.Fatalf("JSON error = %q: %v", stderr.String(), err)
	}
	if response.State != "error" || response.Code == "" || response.Message == "" {
		t.Fatalf("error response = %#v, want state/code/message", response)
	}
}

func TestHeadlessAgentReapStaleRequiresConfirmationAndPersistsRecovery(t *testing.T) {
	root := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	store := agentruntime.FileStore{Path: headlessAgentStorePath(root)}
	old := time.Now().Add(-10 * time.Minute)
	if err := store.Save([]agentruntime.RunRecord{{
		ID:                    "run_stale",
		Status:                agentruntime.RunRunning,
		Spec:                  agentruntime.RunSpec{Objective: "stuck", Workspace: root},
		EffectiveCapabilities: agentruntime.Capabilities{Read: true},
		CreatedAt:             old,
		StartedAt:             old,
		LastHeartbeatAt:       old,
	}}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"agent", "reap-stale", "--json", "--root", root, "--max-silence", "1m"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--confirm") {
		t.Fatalf("unconfirmed reap code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = runHeadlessCLI([]string{"agent", "reap-stale", "--confirm", "--json", "--root", root, "--max-silence", "1m"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("confirmed reap code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var response headlessAgentReapResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode reap response: %v; stdout=%s", err, stdout.String())
	}
	if response.State != "reaped" || len(response.Reaped) != 1 || response.Reaped[0] != "run_stale" {
		t.Fatalf("reap response = %#v", response)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != agentruntime.RunInterrupted {
		t.Fatalf("persisted records = %#v, want interrupted stale run", records)
	}
}

func TestHeadlessAgentCancelRequiresConfirmationAndPersistsCancellation(t *testing.T) {
	root := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	store := agentruntime.FileStore{Path: headlessAgentStorePath(root)}
	now := time.Now()
	if err := store.Save([]agentruntime.RunRecord{{
		ID:                    "run_active",
		Status:                agentruntime.RunRunning,
		Spec:                  agentruntime.RunSpec{Objective: "active", Workspace: root},
		EffectiveCapabilities: agentruntime.Capabilities{Read: true},
		CreatedAt:             now,
		StartedAt:             now,
		LastHeartbeatAt:       now,
	}}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"agent", "cancel", "--json", "--root", root, "run_active"}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--confirm") {
		t.Fatalf("unconfirmed cancel code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = runHeadlessCLI([]string{"agent", "cancel", "--confirm", "--json", "--root", root, "run_active"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("confirmed cancel code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var response headlessAgentCancelResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode cancel response: %v; stdout=%s", err, stdout.String())
	}
	if response.State != "cancelled" || response.RunID != "run_active" {
		t.Fatalf("cancel response = %#v", response)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != agentruntime.RunCancelled {
		t.Fatalf("persisted records = %#v, want cancelled run", records)
	}
}

func TestHeadlessWriteLockSerializesAccess(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := acquireHeadlessWriteLock(root, path)
	if err != nil {
		t.Fatal(err)
	}
	firstUnlocked := false
	defer func() {
		if !firstUnlocked {
			_ = first.Unlock()
		}
	}()

	secondDone := make(chan error, 1)
	go func() {
		second, acquireErr := acquireHeadlessWriteLock(root, path)
		if acquireErr == nil {
			_ = second.Unlock()
		}
		secondDone <- acquireErr
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second writer acquired lock while first held it: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := first.Unlock(); err != nil {
		t.Fatal(err)
	}
	firstUnlocked = true
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second writer failed after unlock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second writer did not acquire lock after unlock")
	}
}

func TestCollectHeadlessContextIsBoundedAndDescriptive(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "internal"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Teak\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	response, err := collectHeadlessContext(root)
	if err != nil {
		t.Fatalf("collectHeadlessContext() error = %v", err)
	}
	if len(response.Entries) != 2 || response.Truncated {
		t.Fatalf("context response = %#v", response)
	}
	if response.Entries[0].Name != "README.md" || response.Entries[0].Kind != "file" {
		t.Fatalf("entries = %#v", response.Entries)
	}
}

func TestToolFailureStateDistinguishesUninitializedProjects(t *testing.T) {
	for _, tt := range []struct {
		message string
		want    string
	}{
		{message: "codemap status: not a codemap project", want: "uninitialized"},
		{message: "vecgrep status: permission denied", want: "error"},
		{message: "vecgrep: no index", want: "uninitialized"},
	} {
		if got := toolFailureState(errors.New(tt.message)); got != tt.want {
			t.Errorf("toolFailureState(%q) = %q, want %q", tt.message, got, tt.want)
		}
	}
}

func TestToolFailureStateContextPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := toolFailureStateContext(ctx, context.Canceled); got != "cancelled" {
		t.Fatalf("toolFailureStateContext() = %q, want cancelled after cancellation", got)
	}
}

func TestCollectHeadlessSessionReportsMissingAndPresentState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))

	missing := collectHeadlessSession(root)
	if missing.State != "missing" || missing.Session != nil {
		t.Fatalf("missing session = %#v", missing)
	}

	state := session.State{
		Version:   1,
		RootDir:   root,
		ActiveTab: 0,
		Tabs: []session.TabState{{
			FilePath:   filepath.Join(root, "main.go"),
			CursorLine: 2,
		}},
	}
	if err := session.Save(state); err != nil {
		t.Fatalf("save session: %v", err)
	}
	present := collectHeadlessSession(root)
	if present.State != "present" || present.Session == nil || len(present.Session.Tabs) != 1 {
		t.Fatalf("present session = %#v", present)
	}
}

func TestHeadlessNamedSessionLifecycle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	state := session.State{
		Version:   1,
		RootDir:   root,
		ActiveTab: 0,
		Tabs:      []session.TabState{{FilePath: filepath.Join(root, "main.go")}},
	}
	if err := session.Save(state); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runHeadlessCLI([]string{"session", "save", "review", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("session save exit code = %d, stderr = %s", code, stderr.String())
	}
	var saved headlessSessionResponse
	if err := json.Unmarshal(stdout.Bytes(), &saved); err != nil {
		t.Fatalf("decode session save response: %v", err)
	}
	if saved.Name != "review" || saved.State != "present" {
		t.Fatalf("saved session response = %#v", saved)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessCLI([]string{"session", "list", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("session list exit code = %d, stderr = %s", code, stderr.String())
	}
	var listed headlessSessionListResponse
	if err := json.Unmarshal(stdout.Bytes(), &listed); err != nil {
		t.Fatalf("decode session list response: %v", err)
	}
	if len(listed.Names) != 1 || listed.Names[0] != "review" {
		t.Fatalf("listed named sessions = %#v", listed)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessCLI([]string{"session", "show", "--name", "review", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("session show exit code = %d, stderr = %s", code, stderr.String())
	}
	var shown headlessSessionResponse
	if err := json.Unmarshal(stdout.Bytes(), &shown); err != nil {
		t.Fatalf("decode named session response: %v", err)
	}
	if shown.Name != "review" || shown.Session == nil || len(shown.Session.Tabs) != 1 {
		t.Fatalf("shown named session = %#v", shown)
	}
}

func TestHeadlessSessionHealthAndCleanupOnlyRemoveStaleSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	existing := filepath.Join(root, "main.go")
	if err := os.WriteFile(existing, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveNamed(session.State{
		Version:   1,
		RootDir:   root,
		ActiveTab: 0,
		Tabs:      []session.TabState{{FilePath: existing}},
	}, "healthy"); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveNamed(session.State{
		Version:   1,
		RootDir:   root,
		ActiveTab: 0,
		Tabs:      []session.TabState{{FilePath: filepath.Join(root, "deleted.go")}},
	}, "stale"); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := runHeadlessCLI([]string{"session", "health", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("session health exit code = %d, stderr = %s", code, stderr.String())
	}
	var health headlessSessionHealthResponse
	if err := json.Unmarshal(stdout.Bytes(), &health); err != nil {
		t.Fatalf("decode session health response: %v", err)
	}
	if health.State != "stale" || len(health.Sessions) != 2 {
		t.Fatalf("session health = %#v", health)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runHeadlessCLI([]string{"session", "cleanup", "--confirm", "--json", "--root", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("session cleanup exit code = %d, stderr = %s", code, stderr.String())
	}
	var cleanup headlessSessionCleanupResponse
	if err := json.Unmarshal(stdout.Bytes(), &cleanup); err != nil {
		t.Fatalf("decode session cleanup response: %v", err)
	}
	if len(cleanup.Removed) != 1 || cleanup.Removed[0] != "stale" || len(cleanup.Skipped) != 1 || cleanup.Skipped[0] != "healthy" {
		t.Fatalf("session cleanup = %#v", cleanup)
	}
	if _, err := session.LoadNamed(context.Background(), root, "healthy"); err != nil {
		t.Fatalf("healthy session removed: %v", err)
	}
	if _, err := session.LoadNamed(context.Background(), root, "stale"); !os.IsNotExist(err) {
		t.Fatalf("stale session remains, error = %v", err)
	}
}

func TestHeadlessExecRequiresExplicitConfirmation(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := runHeadlessCLI([]string{"exec", "--json", "--root", root, "--", "true"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("headless exec without --confirm succeeded")
	}
}

func TestHeadlessExecRejectsOversizedArguments(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{
		"exec", "--confirm", "--json", "--root", root, "--",
		"true", strings.Repeat("x", headlessMaxCommandArgsBytes),
	}
	if code := runHeadlessCLI(args, strings.NewReader(""), &stdout, &stderr); code != 2 {
		t.Fatalf("oversized exec exit code = %d, stderr = %s, want 2", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "command arguments exceed") {
		t.Fatalf("oversized exec error = %q, want bounded argument error", stderr.String())
	}
}

func TestHeadlessSearchReportsResultLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(strings.Repeat("needle\n", 101)), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runHeadlessCLI([]string{"search", "--json", "--root", root, "needle"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("headless search exit code = %d, stderr=%s", code, stderr.String())
	}
	var response headlessSearchResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode headless search response: %v; output=%s", err, stdout.String())
	}
	if len(response.Results) != 100 || !response.Truncated {
		t.Fatalf("headless search response = results:%d truncated:%t, want 100/true", len(response.Results), response.Truncated)
	}
}

func TestClassifyHeadlessProcessFailurePrefersDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	state, exitCode := classifyHeadlessProcessFailure(ctx, errors.New("signal: killed"))
	if state != "timed_out" || exitCode != -1 {
		t.Fatalf("classifyHeadlessProcessFailure() = %q/%d, want timed_out/-1", state, exitCode)
	}
}

func TestHeadlessExecRunsDirectFixtureCommand(t *testing.T) {
	t.Setenv("TEAK_HEADLESS_EXEC_FIXTURE", "1")
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	args := []string{
		"exec", "--confirm", "--json", "--root", root, "--",
		os.Args[0], "-test.run=^TestHeadlessExecFixtureProcess$", "--",
	}
	if code := runHeadlessCLI(args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("headless exec exit code = %d, stderr = %s, stdout = %s", code, stderr.String(), stdout.String())
	}
	var response headlessExecResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode headless exec response: %v", err)
	}
	if response.State != "completed" || !strings.Contains(response.Stdout, "fixture command ok") || response.ExitCode != 0 {
		t.Fatalf("headless exec response = %#v", response)
	}
}

func TestHeadlessExecOutputLimitCancelsExternalProcess(t *testing.T) {
	t.Setenv("TEAK_HEADLESS_EXEC_OUTPUT_FIXTURE", "1")
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	started := time.Now()
	args := []string{
		"exec", "--confirm", "--json", "--root", root, "--",
		os.Args[0], "-test.run=^TestHeadlessExecOutputLimitFixture$", "--",
	}
	code := runHeadlessCLI(args, strings.NewReader(""), &stdout, &stderr)
	var response headlessExecResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode output-limit response: %v; raw length=%d", err, stdout.Len())
	}
	if code != 1 {
		t.Logf("output-limit response state=%q truncated=%t stdout-bytes=%d detail=%q", response.State, response.Truncated, len(response.Stdout), response.Detail)
		t.Fatalf("headless exec output-limit exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
	if !response.Truncated || response.State != "failed" {
		t.Fatalf("output-limit response = %#v, want failed and truncated", response)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("output-limit process was not canceled promptly: %s", elapsed)
	}
}

func TestHeadlessExecContextCancellationStopsExternalProcess(t *testing.T) {
	t.Setenv("TEAK_HEADLESS_EXEC_CANCEL_FIXTURE", "1")
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	args := []string{
		"exec", "--confirm", "--json", "--root", root, "--",
		os.Args[0], "-test.run=^TestHeadlessExecCancellationFixture$", "--",
	}

	result := make(chan int, 1)
	go func() {
		result <- runHeadlessCLIContext(ctx, args, strings.NewReader(""), &stdout, &stderr)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case code := <-result:
		if code != 1 {
			t.Fatalf("cancelled headless exec exit code = %d, want 1; stderr=%s", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled headless exec did not stop promptly")
	}
	var response headlessExecResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode cancelled exec response: %v; stdout=%q", err, stdout.String())
	}
	if response.State != "cancelled" || response.ExitCode != -1 {
		t.Fatalf("cancelled exec response = %#v, want cancelled/-1", response)
	}
}

func TestHeadlessExecCancellationFixture(t *testing.T) {
	if os.Getenv("TEAK_HEADLESS_EXEC_CANCEL_FIXTURE") != "1" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestHeadlessExecOutputLimitFixture(t *testing.T) {
	if os.Getenv("TEAK_HEADLESS_EXEC_OUTPUT_FIXTURE") != "1" {
		return
	}
	chunk := strings.Repeat("x", 4096)
	for i := 0; i < headlessMaxCommandOutput/len(chunk); i++ {
		_, _ = os.Stdout.WriteString(chunk)
	}
	_, _ = os.Stdout.WriteString("overflow")
	for {
		time.Sleep(time.Hour)
	}
}

func TestHeadlessExecFixtureProcess(t *testing.T) {
	if os.Getenv("TEAK_HEADLESS_EXEC_FIXTURE") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString("fixture command ok\n")
}

func TestCollectHeadlessLSPStatusIncludesBuiltInServers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	response, err := collectHeadlessLSPStatus(t.TempDir())
	if err != nil {
		t.Fatalf("collectHeadlessLSPStatus() error = %v", err)
	}
	if len(response.Servers) == 0 {
		t.Fatal("headless LSP status returned no built-in servers")
	}
	foundGo := false
	for _, server := range response.Servers {
		if server.LanguageID == "go" {
			foundGo = true
			if server.Command != "gopls" || len(server.Extensions) != 1 || server.Extensions[0] != ".go" {
				t.Errorf("Go server = %#v", server)
			}
		}
	}
	if !foundGo {
		t.Fatal("headless LSP status omitted the built-in Go server")
	}
}

func TestCollectHeadlessAgentRunsObservesActiveState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := headlessAgentStorePath(root)
	manager, err := agentruntime.NewManager(agentruntime.ManagerConfig{
		Store: agentruntime.FileStore{Path: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Start(agentruntime.RunSpec{Objective: "fixture run", Workspace: root}); err != nil {
		t.Fatal(err)
	}
	response, err := collectHeadlessAgentRuns(root)
	if err != nil {
		t.Fatalf("collectHeadlessAgentRuns() error = %v", err)
	}
	if len(response.Runs) != 1 || response.Runs[0].Status != agentruntime.RunRunning {
		t.Fatalf("agent response = %#v", response)
	}
	stored, err := (agentruntime.FileStore{Path: path}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Status != agentruntime.RunRunning {
		t.Fatalf("agent store after observation = %#v, want unchanged running state", stored)
	}
}

func TestCollectHeadlessAgentRunReturnsRequestedRecord(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := headlessAgentStorePath(root)
	manager, err := agentruntime.NewManager(agentruntime.ManagerConfig{
		Store: agentruntime.FileStore{Path: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := manager.Start(agentruntime.RunSpec{Objective: "inspect this run", Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.RecordAudit(handle.ID, "terminal", "authorized", "sandbox=disabled write=false network=false"); err != nil {
		t.Fatalf("RecordAudit() error = %v", err)
	}

	response, err := collectHeadlessAgentRun(root, handle.ID)
	if err != nil {
		t.Fatalf("collectHeadlessAgentRun() error = %v", err)
	}
	if response.Workspace != root || response.Run.ID != handle.ID {
		t.Fatalf("agent response = %#v", response)
	}
	if response.Run.Status != agentruntime.RunRunning {
		t.Fatalf("agent status = %q, want %q", response.Run.Status, agentruntime.RunRunning)
	}
	if response.Run.Spec.Objective != "inspect this run" {
		t.Fatalf("agent objective = %q", response.Run.Spec.Objective)
	}
	if len(response.Run.Audit) != 1 || response.Run.Audit[0].Operation != "terminal" || response.Run.Audit[0].Outcome != "authorized" {
		t.Fatalf("agent audit = %#v, want bounded terminal authorization", response.Run.Audit)
	}
}

func TestCollectHeadlessAgentRunRejectsUnknownID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, err := collectHeadlessAgentRun(root, agentruntime.RunID("missing-run"))
	if err == nil || !errors.Is(err, agentruntime.ErrRunNotFound) {
		t.Fatalf("collectHeadlessAgentRun() error = %v, want ErrRunNotFound", err)
	}
}

func TestDevelopmentVersionFallback(t *testing.T) {
	if version == "" {
		t.Fatal("version fallback must not be empty")
	}
}

func TestResolveWorkspacePathsUsesOneAbsoluteFileIdentity(t *testing.T) {
	// Use a marker-free temp tree so project-root walking does not pick up the
	// Teak repo itself when the relative path resolves under the working tree.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "relative", "main.go")
	resolved, root := resolveWorkspacePaths(filePath)

	if !filepath.IsAbs(resolved) {
		t.Fatalf("resolved path = %q, want absolute", resolved)
	}
	if resolved != filePath {
		t.Fatalf("resolved = %q, want %q", resolved, filePath)
	}
	// No project markers in the temp tree → root is the file's parent.
	if got, want := root, filepath.Dir(resolved); got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestResolveWorkspacePathsOpensDirectoryAsWorkspace(t *testing.T) {
	dir := t.TempDir()
	// Nested folder so a wrong filepath.Dir() would point at the parent, not dir.
	workspace := filepath.Join(dir, "myproject")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	resolved, root := resolveWorkspacePaths(workspace)

	if resolved != "" {
		t.Fatalf("resolved file path = %q, want empty (directory open)", resolved)
	}
	if root != workspace {
		t.Fatalf("root = %q, want workspace directory %q", root, workspace)
	}
}

func TestResolveWorkspacePathsRelativeDirectory(t *testing.T) {
	// Create a temp dir, chdir into its parent, open the child by relative name.
	parent := t.TempDir()
	workspace := filepath.Join(parent, "target-folder")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(parent); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resolved, root := resolveWorkspacePaths("target-folder")

	if resolved != "" {
		t.Fatalf("resolved file path = %q, want empty", resolved)
	}
	// Compare via Abs of the relative name so macOS /var vs /private/var matches.
	absWant, err := filepath.Abs("target-folder")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if root != absWant {
		t.Fatalf("root = %q, want %q (not parent cwd)", root, absWant)
	}
	// Guard against the old bug: root must be the folder itself, not its parent.
	if root == parent || filepath.Base(root) != "target-folder" {
		t.Fatalf("root = %q should be the target folder, not its parent %q", root, parent)
	}
}

func TestResolveWorkspacePathsFileUsesParentAsRoot(t *testing.T) {
	// No project markers → fall back to the file's parent directory.
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved, root := resolveWorkspacePaths(file)

	if resolved != file {
		t.Fatalf("resolved = %q, want %q", resolved, file)
	}
	if root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
}

func TestResolveWorkspacePathsFileUsesProjectRoot(t *testing.T) {
	// repo/
	//   go.mod
	//   src/cmd/app/main.go  ← open this; workspace should be repo/, not src/cmd/app
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(repo, "src", "cmd", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(nested, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved, root := resolveWorkspacePaths(file)

	if resolved != file {
		t.Fatalf("resolved = %q, want %q", resolved, file)
	}
	if root != repo {
		t.Fatalf("root = %q, want project root %q (not parent %q)", root, repo, nested)
	}
}

func TestResolveWorkspacePathsFileUsesNearestProjectRoot(t *testing.T) {
	// monorepo/
	//   .git/
	//   packages/web/
	//     package.json
	//     src/index.ts  ← nearest root is packages/web, not monorepo
	monorepo := t.TempDir()
	if err := os.Mkdir(filepath.Join(monorepo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	pkg := filepath.Join(monorepo, "packages", "web")
	if err := os.MkdirAll(filepath.Join(pkg, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"web"}`), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	file := filepath.Join(pkg, "src", "index.ts")
	if err := os.WriteFile(file, []byte("export {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, root := resolveWorkspacePaths(file)

	if root != pkg {
		t.Fatalf("root = %q, want nearest package root %q", root, pkg)
	}
}

func TestResolveWorkspacePathsFileUsesGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	nested := filepath.Join(repo, "internal", "app")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(nested, "app.go")
	if err := os.WriteFile(file, []byte("package app\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, root := resolveWorkspacePaths(file)

	if root != repo {
		t.Fatalf("root = %q, want git root %q", root, repo)
	}
}

func TestResolveWorkspacePathsNewFileUsesProjectRoot(t *testing.T) {
	// Not-yet-created path still walks from its parent for project markers.
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "Cargo.toml"), []byte("[package]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
	nested := filepath.Join(repo, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	newFile := filepath.Join(nested, "lib.rs") // does not exist

	resolved, root := resolveWorkspacePaths(newFile)

	if resolved != newFile {
		t.Fatalf("resolved = %q, want %q", resolved, newFile)
	}
	if root != repo {
		t.Fatalf("root = %q, want project root %q", root, repo)
	}
}

func TestFindProjectRootStopsAtFilesystemRoot(t *testing.T) {
	// A plain temp dir with no markers should not invent a root.
	dir := t.TempDir()
	if got := findProjectRoot(dir); got != "" {
		t.Fatalf("findProjectRoot(%q) = %q, want empty", dir, got)
	}
}

func TestResolveWorkspacePathsEmptyUsesCWD(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	resolved, root := resolveWorkspacePaths("")

	if resolved != "" {
		t.Fatalf("resolved = %q, want empty", resolved)
	}
	if root != cwd {
		t.Fatalf("root = %q, want cwd %q", root, cwd)
	}
}

func TestResolveCLIWorkspaceMissingDirectoryErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir") + string(filepath.Separator)
	_, _, err := resolveCLIWorkspace([]cliTarget{{path: missing, line: -1}})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("error = %v, want missing directory", err)
	}
}

func TestResolveCLIWorkspaceMultipleFiles(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	a := filepath.Join(repo, "a.go")
	b := filepath.Join(repo, "b.go")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	files, root, err := resolveCLIWorkspace([]cliTarget{
		{path: a, line: 0, col: 0},
		{path: b, line: -1},
	})
	if err != nil {
		t.Fatalf("resolveCLIWorkspace: %v", err)
	}
	if root != repo {
		t.Fatalf("root = %q, want %q", root, repo)
	}
	if len(files) != 2 || files[0].Path != a || files[1].Path != b {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Line != 0 {
		t.Fatalf("first line = %d, want 0", files[0].Line)
	}
}

func TestResolveCLIWorkspaceRejectsDirectoryWithFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := resolveCLIWorkspace([]cliTarget{
		{path: dir, line: -1},
		{path: file, line: -1},
	})
	if err == nil || !strings.Contains(err.Error(), "together with other paths") {
		t.Fatalf("error = %v, want mix rejection", err)
	}
}

func TestParseFileLocation(t *testing.T) {
	dir := t.TempDir()
	// File whose name contains a colon should not be split when it exists.
	weird := filepath.Join(dir, "weird:name.go")
	if err := os.WriteFile(weird, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, line, _ := parseFileLocation(weird)
	if path != weird || line != -1 {
		t.Fatalf("existing colon name: path=%q line=%d, want intact", path, line)
	}

	path, line, col := parseFileLocation("main.go:12:3")
	if path != "main.go" || line != 11 || col != 2 {
		t.Fatalf("got path=%q line=%d col=%d", path, line, col)
	}

	path, line, col = parseFileLocation("main.go:12")
	if path != "main.go" || line != 11 || col != 0 {
		t.Fatalf("got path=%q line=%d col=%d", path, line, col)
	}
}

func TestTerminalStartupError(t *testing.T) {
	tests := []struct {
		name          string
		stdinIsTTY    bool
		stdoutIsTTY   bool
		term          string
		wantErrorPart string
	}{
		{name: "interactive terminal", stdinIsTTY: true, stdoutIsTTY: true, term: "xterm-256color"},
		{name: "stdin is a pipe", stdoutIsTTY: true, term: "xterm-256color", wantErrorPart: "stdin"},
		{name: "stdout is a pipe", stdinIsTTY: true, term: "xterm-256color", wantErrorPart: "stdout"},
		{name: "dumb terminal", stdinIsTTY: true, stdoutIsTTY: true, term: " dumb ", wantErrorPart: "TERM=dumb"},
		{name: "unset term remains supported", stdinIsTTY: true, stdoutIsTTY: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := terminalStartupError(tt.stdinIsTTY, tt.stdoutIsTTY, tt.term)
			if tt.wantErrorPart == "" {
				if err != nil {
					t.Fatalf("terminalStartupError() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErrorPart) {
				t.Fatalf("terminalStartupError() error = %v, want containing %q", err, tt.wantErrorPart)
			}
		})
	}
}
