package ui

import (
	"testing"
	"unsafe"
)

// Themes are copied into every TUI component. Keep the public value a small,
// immutable handle so editor input does not copy all Lipgloss style state.
func TestThemeIsSmallImmutableHandle(t *testing.T) {
	const maxThemeHandleBytes = 64
	if size := unsafe.Sizeof(Theme{}); size > maxThemeHandleBytes {
		t.Fatalf("Theme is %d bytes; want at most %d-byte handle", size, maxThemeHandleBytes)
	}
}

func TestThemeConstructorsCreateIndependentImmutableGraphs(t *testing.T) {
	first := NordTheme()
	second := NordTheme()
	if first.themeStyles == nil || second.themeStyles == nil {
		t.Fatal("theme constructor returned a nil backing graph")
	}
	if first.themeStyles == second.themeStyles {
		t.Fatal("separate theme constructors unexpectedly share mutable backing")
	}
	if copied := first; copied.themeStyles != first.themeStyles {
		t.Fatal("copied Theme did not retain its immutable backing graph")
	}
}
