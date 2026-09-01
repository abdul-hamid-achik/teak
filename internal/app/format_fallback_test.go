package app

import (
	"context"
	"strings"
	"testing"

	"teak/internal/text"
	"teak/internal/toolpath"
)

func TestFallbackFormatterByExtension(t *testing.T) {
	tests := []struct {
		path string
		name string
	}{
		{"main.go", "gofmt"},
		{"app.ts", "prettier"},
		{"styles.css", "prettier"},
		{"readme.md", "prettier"},
		{"notes.txt", ""},
	}
	for _, tt := range tests {
		name, _ := fallbackFormatter(tt.path)
		if name != tt.name {
			t.Errorf("fallbackFormatter(%q) = %q, want %q", tt.path, name, tt.name)
		}
	}
}

func TestFallbackFormatDocumentGofmt(t *testing.T) {
	if _, err := toolpath.Command(context.Background(), "gofmt"); err != nil {
		t.Skip("gofmt is not installed")
	}
	src := text.NewFromString("package main\nfunc main() {x:=1}\n")
	edits, used, err := fallbackFormatDocument(context.Background(), "main.go", src)
	if err != nil {
		t.Fatalf("fallbackFormatDocument: %v", err)
	}
	if !used {
		t.Fatal("gofmt fallback was not used")
	}
	if len(edits) != 1 {
		t.Fatalf("edits = %d, want a whole-document rewrite", len(edits))
	}
	if !strings.Contains(edits[0].NewText, "x := 1") {
		t.Fatalf("formatted text = %q, want gofmt spacing", edits[0].NewText)
	}
}
