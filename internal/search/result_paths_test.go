package search

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateResultsNormalizesRelativePathsWithoutMutatingInput(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := []Result{{FilePath: "./main.go", Line: 2, Col: 3}}
	got, err := ValidateResults(root, input)
	if err != nil {
		t.Fatalf("ValidateResults() error = %v", err)
	}
	if got[0].FilePath != "main.go" || input[0].FilePath != "./main.go" {
		t.Fatalf("normalized = %#v, input = %#v; want copied relative path", got, input)
	}
}

func TestValidateResultsRejectsOutsideAndMissingFiles(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]Result{
		"outside":       {FilePath: filepath.Join(outside, "secret.go")},
		"missing":       {FilePath: "missing.go"},
		"negative line": {FilePath: "missing.go", Line: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateResults(root, []Result{result}); !errors.Is(err, ErrInvalidResult) {
				t.Fatalf("ValidateResults() error = %v, want ErrInvalidResult", err)
			}
		})
	}
}

func TestValidateResultsRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(target, []byte("package secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.go")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ValidateResults(root, []Result{{FilePath: "link.go"}}); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("ValidateResults() error = %v, want ErrInvalidResult", err)
	}
}
