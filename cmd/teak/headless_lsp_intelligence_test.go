package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"teak/internal/lsp"
)

func TestHeadlessLSPIntelligenceQueriesFixture(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.fixture"), []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("TEAK_HEADLESS_LSP_FIXTURE", "1")
	external := filepath.Join(t.TempDir(), "external.fixture")
	if err := os.WriteFile(external, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAK_LSP_EXTERNAL_PATH", external)
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

	t.Run("symbols", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := runHeadlessCLI([]string{
			"lsp", "symbols", "--json", "--root", root, "main.fixture",
		}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("symbols exit code = %d, stderr=%s", code, stderr.String())
		}
		var response headlessLSPSymbolsResponse
		if err := json.Unmarshal([]byte(stdout.String()), &response); err != nil {
			t.Fatalf("decode symbols response: %v; stdout=%s", err, stdout.String())
		}
		if response.State != "ready" || response.RelativePath != "main.fixture" || len(response.Symbols) != 1 {
			t.Fatalf("symbols response = %#v", response)
		}
		if got := response.Symbols[0]; got.Name != "Fixture" || got.Line != 0 || got.Column != 0 || got.EndLine != 0 || got.EndColumn != 14 {
			t.Fatalf("symbol = %#v", got)
		}
	})

	t.Run("hover", func(t *testing.T) {
		var stdout, stderr strings.Builder
		code := runHeadlessCLI([]string{
			"lsp", "hover", "--json", "--line", "0", "--column", "1", "--root", root, "main.fixture",
		}, strings.NewReader(""), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("hover exit code = %d, stderr=%s", code, stderr.String())
		}
		var response headlessLSPHoverResponse
		if err := json.Unmarshal([]byte(stdout.String()), &response); err != nil {
			t.Fatalf("decode hover response: %v; stdout=%s", err, stdout.String())
		}
		if response.State != "ready" || !response.Found || response.Content != "fixture hover" {
			t.Fatalf("hover response = %#v", response)
		}
	})

	for _, operation := range []string{"definition", "references"} {
		t.Run(operation, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := runHeadlessCLI([]string{
				"lsp", operation, "--json", "--line", "0", "--column", "1", "--root", root, "main.fixture",
			}, strings.NewReader(""), &stdout, &stderr)
			if code != 0 {
				t.Fatalf("%s exit code = %d, stderr=%s", operation, code, stderr.String())
			}
			var response headlessLSPLocationsResponse
			if err := json.Unmarshal([]byte(stdout.String()), &response); err != nil {
				t.Fatalf("decode %s response: %v; stdout=%s", operation, err, stdout.String())
			}
			wantSkipped := 0
			if operation == "references" {
				wantSkipped = 1
			}
			if response.State != "ready" || len(response.Locations) != 1 || response.Skipped != wantSkipped {
				t.Fatalf("%s response = %#v", operation, response)
			}
			if got := response.Locations[0]; got.Path != "main.fixture" || got.Line != 0 || got.Column != 0 {
				t.Fatalf("%s location = %#v", operation, got)
			}
		})
	}
}

func TestHeadlessLSPIntelligenceRequiresBoundedPosition(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.fixture")
	if err := os.WriteFile(file, []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var stdout, stderr strings.Builder
	code := runHeadlessCLI([]string{
		"lsp", "hover", "--json", "--line", "0", "--root", root, "main.fixture",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--line and --column") {
		t.Fatalf("missing position code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestBoundedHeadlessLSPContent(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		truncated bool
		wantBytes int
	}{
		{name: "small", content: "hover", wantBytes: len("hover")},
		{name: "large ascii", content: strings.Repeat("x", maxHeadlessLSPHoverBytes+32), truncated: true, wantBytes: maxHeadlessLSPHoverBytes},
		{name: "large utf8", content: "x" + strings.Repeat("é", maxHeadlessLSPHoverBytes), truncated: true, wantBytes: maxHeadlessLSPHoverBytes - 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := boundedHeadlessLSPContent(tc.content)
			if truncated != tc.truncated || len(got) != tc.wantBytes || !utf8.ValidString(got) {
				t.Fatalf("bounded content = bytes:%d truncated:%t valid:%t, want bytes:%d truncated:%t", len(got), truncated, utf8.ValidString(got), tc.wantBytes, tc.truncated)
			}
		})
	}
}

func TestBoundedHeadlessLSPSymbolsTruncateCountAndDepth(t *testing.T) {
	tooMany := make([]lsp.DocumentSymbol, maxHeadlessLSPQuerySymbols+1)
	for i := range tooMany {
		tooMany[i] = lsp.DocumentSymbol{Name: fmt.Sprintf("Symbol%d", i)}
	}
	got, truncated := boundedHeadlessLSPSymbols(tooMany)
	if len(got) != maxHeadlessLSPQuerySymbols || !truncated {
		t.Fatalf("bounded symbols = len:%d truncated:%t, want %d/true", len(got), truncated, maxHeadlessLSPQuerySymbols)
	}

	deep := lsp.DocumentSymbol{Name: "root"}
	current := &deep
	for i := 0; i < maxHeadlessLSPQueryDepth+2; i++ {
		current.Children = []lsp.DocumentSymbol{{Name: fmt.Sprintf("child%d", i)}}
		current = &current.Children[0]
	}
	got, truncated = boundedHeadlessLSPSymbols([]lsp.DocumentSymbol{deep})
	if len(got) != 1 || !truncated {
		t.Fatalf("deep bounded symbols = len:%d truncated:%t, want 1/true", len(got), truncated)
	}
}

func TestBoundedHeadlessLSPLocationsSkipsInvalidTargets(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "main.fixture")
	if err := os.WriteFile(inside, []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.fixture")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locations, skipped, truncated := boundedHeadlessLSPLocations(root, []lsp.Location{
		{URI: lsp.FileURI(inside), StartLine: 0, StartCol: 0},
		{URI: lsp.FileURI(outside), StartLine: 1, StartCol: 0},
		{URI: lsp.FileURI(filepath.Join(root, "missing.fixture")), StartLine: 2, StartCol: 0},
	})
	if len(locations) != 1 || skipped != 2 || truncated {
		t.Fatalf("bounded locations = %#v skipped:%d truncated:%t, want one workspace location and two skipped", locations, skipped, truncated)
	}
}

func TestHeadlessLSPIntelligenceReportsMissingAndUnsupported(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "main.fixture")
	if err := os.WriteFile(file, []byte("fixture source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "teak")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	missingConfig := `[[lsp]]
extensions = [".fixture"]
command = "/definitely/missing/teak-intelligence-lsp"
language_id = "fixture"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(missingConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	missing, err := collectHeadlessLSPSymbolsContext(nil, root, file)
	if err != nil || missing.State != "missing" {
		t.Fatalf("missing symbols response = %#v, err=%v", missing, err)
	}

	t.Setenv("TEAK_DOCTOR_LSP_FIXTURE", "1")
	config := fmt.Sprintf(`[[lsp]]
extensions = [".fixture"]
command = %s
args = ["-test.run=^TestDoctorProtocolProbeFixtureProcess$", "--"]
language_id = "fixture"
`, strconv.Quote(os.Args[0]))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	unsupported, err := collectHeadlessLSPHoverContext(nil, root, file, 0, 1)
	if err != nil || unsupported.State != "unsupported" || !strings.Contains(unsupported.Detail, "hover") {
		t.Fatalf("unsupported hover response = %#v, err=%v", unsupported, err)
	}
}
