package lsp

import (
	"strings"
	"testing"
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
