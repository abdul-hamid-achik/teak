package lsp

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"teak/internal/toolpath"
)

func TestMergeEnvironmentOverridesWithoutDuplicateKeys(t *testing.T) {
	base := []string{"PATH=/usr/bin", "PATH=/duplicate", "UNCHANGED=yes"}
	merged := mergeEnvironment(base, map[string]string{
		"PATH":     "=/custom/bin",
		"NEW_FLAG": "enabled",
	})

	if strings.Join(base, "\x00") != "PATH=/usr/bin\x00PATH=/duplicate\x00UNCHANGED=yes" {
		t.Fatalf("mergeEnvironment mutated base = %#v", base)
	}
	counts := make(map[string]int)
	values := make(map[string]string)
	for _, entry := range merged {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		counts[name]++
		values[name] = value
	}
	if counts["PATH"] != 1 || values["PATH"] != "=/custom/bin" {
		t.Fatalf("merged PATH = %q with %d entries, want one overridden entry", values["PATH"], counts["PATH"])
	}
	if counts["NEW_FLAG"] != 1 || values["NEW_FLAG"] != "enabled" {
		t.Fatalf("merged NEW_FLAG = %q with %d entries, want one new entry", values["NEW_FLAG"], counts["NEW_FLAG"])
	}
}

func TestLanguageServerEnvironmentIncludesToolchainFallbacks(t *testing.T) {
	t.Setenv("PATH", "/minimal/bin")
	for _, tt := range []struct {
		name      string
		overrides map[string]string
		explicit  bool
	}{
		{"inherited", nil, false}, {"configured PATH", map[string]string{"PATH": "/chosen/sdk/bin"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := languageServerEnvironment(tt.overrides)
			paths := []string{}
			for _, entry := range env {
				if value, ok := strings.CutPrefix(entry, "PATH="); ok {
					paths = append(paths, value)
				}
			}
			if len(paths) != 1 {
				t.Fatalf("got %d PATH entries", len(paths))
			}
			if tt.explicit {
				if paths[0] != "/chosen/sdk/bin" {
					t.Fatalf("explicit SDK PATH changed: %q", paths[0])
				}
				return
			}
			dirs := filepath.SplitList(paths[0])
			if dirs[0] != "/minimal/bin" {
				t.Fatal("inherited PATH precedence changed")
			}
			for _, fallback := range toolpath.Default().SearchPath() {
				if !slices.Contains(dirs, fallback) {
					t.Fatalf("missing toolchain directory %q", fallback)
				}
			}
		})
	}
}
