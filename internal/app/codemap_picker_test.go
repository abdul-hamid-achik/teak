package app

import (
	"path/filepath"
	"testing"

	"teak/internal/codemap"
)

func TestCodemapSymbolsToPickerItemsIncludesNavigableLocation(t *testing.T) {
	root := t.TempDir()
	items := codemapSymbolsToPickerItems([]codemap.Symbol{{
		Symbol:    "serve",
		Kind:      "function",
		File:      "internal/server.go",
		StartLine: 12,
	}}, root)

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Label != "serve" || items[0].Description != "function · internal/server.go:12" {
		t.Fatalf("item = %#v, want symbol and location", items[0])
	}
	sel, ok := items[0].Value.(codemapSymbolPickerMsg)
	if !ok || filepath.IsAbs(sel.Symbol.File) {
		t.Fatalf("picker value = %#v, want original relative symbol", items[0].Value)
	}
}

func TestCodemapSymbolsToPickerItemsBoundsLargeResults(t *testing.T) {
	symbols := make([]codemap.Symbol, maxCodemapPickerItems+50)
	items := codemapSymbolsToPickerItems(symbols, "")
	if len(items) != maxCodemapPickerItems {
		t.Fatalf("items = %d, want bounded %d", len(items), maxCodemapPickerItems)
	}
}
