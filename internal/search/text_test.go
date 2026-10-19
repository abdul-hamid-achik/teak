package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTextSearchMissingRootReportsError guards against a missing project root
// being reported as an empty result set. Without an error the overlay would
// show "No results", which looks like a successful search over an empty tree.
func TestTextSearchMissingRootReportsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	tests := []struct {
		name string
		run  func() ([]Result, error)
	}{
		{"go walker", func() ([]Result, error) {
			return textSearchGoWalker(context.Background(), missing, "needle", SearchOpts{})
		}},
		{"text search with ripgrep fallback", func() ([]Result, error) {
			return TextSearch(missing, "needle")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := tt.run()
			if err == nil {
				t.Fatalf("expected an error for missing root %q, got nil (results=%d)", missing, len(results))
			}
		})
	}
}

// TestSearchPreviewBoundsLongLines guards against a minified or generated file
// with a very long line leaking its entire text into every result. Previews
// are copied into each Result and rendered on every frame, so they must stay
// bounded while still showing the matched text.
func TestSearchPreviewBoundsLongLines(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("a", 300000) + "needle" + strings.Repeat("b", 300000)
	if err := os.WriteFile(filepath.Join(dir, "min.js"), []byte(long+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Go walker path: the file is under the 1MB search limit.
	results, err := textSearchGoWalker(context.Background(), dir, "needle", SearchOpts{CaseSensitive: true})
	if err != nil {
		t.Fatalf("walker error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("walker results = %d, want 1", len(results))
	}
	checkBoundedPreview(t, "walker", results[0].Preview)

	// ripgrep parser path with the same long line.
	raw := fmt.Sprintf(`{"type":"match","data":{"path":{"text":"./min.js"},"lines":{"text":%q},"line_number":1,"submatches":[{"match":{"text":"needle"},"start":300000,"end":300006}]}}`, long+"\n")
	rgResults, err := parseRipgrepJSON([]byte(raw), 10)
	if err != nil {
		t.Fatalf("parseRipgrepJSON error: %v", err)
	}
	if len(rgResults) != 1 {
		t.Fatalf("ripgrep results = %d, want 1", len(rgResults))
	}
	checkBoundedPreview(t, "ripgrep", rgResults[0].Preview)
}

func checkBoundedPreview(t *testing.T, engine, preview string) {
	t.Helper()
	if len(preview) > maxPreviewBytes+2*len("…") {
		t.Errorf("%s preview is %d bytes, want at most about %d", engine, len(preview), maxPreviewBytes)
	}
	if !strings.Contains(preview, "needle") {
		t.Errorf("%s preview does not show the matched text", engine)
	}
}
