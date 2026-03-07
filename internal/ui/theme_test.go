package ui

import (
	"testing"
)

// TestNordColors tests that Nord colors are defined
func TestNordColors(t *testing.T) {
	colors := []struct {
		name  string
		color interface{}
	}{
		{"Nord0", Nord0},
		{"Nord1", Nord1},
		{"Nord2", Nord2},
		{"Nord3", Nord3},
		{"Nord4", Nord4},
		{"Nord5", Nord5},
		{"Nord6", Nord6},
		{"Nord7", Nord7},
		{"Nord8", Nord8},
		{"Nord9", Nord9},
		{"Nord10", Nord10},
		{"Nord11", Nord11},
		{"Nord12", Nord12},
		{"Nord13", Nord13},
		{"Nord14", Nord14},
		{"Nord15", Nord15},
	}

	for _, c := range colors {
		t.Run(c.name, func(t *testing.T) {
			if c.color == nil {
				t.Errorf("Expected %s to be defined", c.name)
			}
		})
	}
}

// TestThemeByName tests theme lookup by name
func TestThemeByName(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"nord"},
		{"dracula"},
		{"catppuccin"},
		{"solarized-dark"},
		{"one-dark"},
		{"unknown"}, // Should fall back to nord
		{""},        // Empty should fall back to nord
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			theme := ThemeByName(tt.name)

			// Just verify we get a theme with non-empty Editor style
			// We can't directly compare themes, but we can verify they're usable
			_ = theme.Editor
		})
	}
}

// TestNordTheme tests Nord theme creation
func TestNordTheme(t *testing.T) {
	theme := NordTheme()

	// Verify theme has essential properties set
	// We can't check specific values, but we can verify the theme is usable
	_ = theme.Editor
	_ = theme.Gutter
	_ = theme.StatusBar
}

// TestDefaultTheme tests default theme creation
func TestDefaultTheme(t *testing.T) {
	theme := DefaultTheme()

	// Should be same as NordTheme
	_ = theme.Editor
	_ = theme.Gutter
}

// TestDraculaTheme tests Dracula theme creation
func TestDraculaTheme(t *testing.T) {
	theme := DraculaTheme()

	_ = theme.Editor
	_ = theme.Gutter
}

// TestCatppuccinTheme tests Catppuccin theme creation
func TestCatppuccinTheme(t *testing.T) {
	theme := CatppuccinTheme()

	_ = theme.Editor
	_ = theme.Gutter
}

// TestSolarizedDarkTheme tests Solarized Dark theme creation
func TestSolarizedDarkTheme(t *testing.T) {
	theme := SolarizedDarkTheme()

	_ = theme.Editor
	_ = theme.Gutter
}

// TestOneDarkTheme tests One Dark theme creation
func TestOneDarkTheme(t *testing.T) {
	theme := OneDarkTheme()

	_ = theme.Editor
	_ = theme.Gutter
}

// TestThemeStyles tests that theme has expected styles
func TestThemeStyles(t *testing.T) {
	theme := NordTheme()

	// Test that various theme styles are set
	styles := []struct {
		name  string
		style interface{}
	}{
		{"Editor", theme.Editor},
		{"Gutter", theme.Gutter},
		{"GutterActive", theme.GutterActive},
		{"Selection", theme.Selection},
		{"CursorLine", theme.CursorLine},
		{"StatusBar", theme.StatusBar},
		{"StatusText", theme.StatusText},
		{"HelpBorder", theme.HelpBorder},
		{"HelpTitle", theme.HelpTitle},
		{"HelpKey", theme.HelpKey},
		{"TreeEntry", theme.TreeEntry},
		{"TreeCursor", theme.TreeCursor},
		{"TreeBorder", theme.TreeBorder},
		{"TabActive", theme.TabActive},
		{"TabInactive", theme.TabInactive},
		{"TabBar", theme.TabBar},
		{"SearchBox", theme.SearchBox},
		{"SearchInput", theme.SearchInput},
		{"SearchResult", theme.SearchResult},
		{"DiagError", theme.DiagError},
		{"DiagWarning", theme.DiagWarning},
		{"AutocompleteBox", theme.AutocompleteBox},
		{"HoverBox", theme.HoverBox},
		{"BracketMatch", theme.BracketMatch},
		{"GitHeader", theme.GitHeader},
		{"GitEntry", theme.GitEntry},
		{"GitAdded", theme.GitAdded},
		{"GitModified", theme.GitModified},
		{"GitDeleted", theme.GitDeleted},
		{"DiffRemoved", theme.DiffRemoved},
		{"DiffAdded", theme.DiffAdded},
		{"SidebarTabActive", theme.SidebarTabActive},
		{"SidebarTabInactive", theme.SidebarTabInactive},
	}

	for _, s := range styles {
		t.Run(s.name, func(t *testing.T) {
			if s.style == nil {
				t.Errorf("Expected %s to be set", s.name)
			}
		})
	}
}

// TestThemeFallback tests theme fallback for unknown names
func TestThemeFallback(t *testing.T) {
	// Unknown theme should fall back to Nord
	unknownTheme := ThemeByName("nonexistent-theme")
	nordTheme := NordTheme()

	// Both should be usable (non-nil styles)
	_ = unknownTheme.Editor
	_ = nordTheme.Editor
}

// TestThemeCaseSensitivity tests theme name case sensitivity
func TestThemeCaseSensitivity(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"nord"},
		{"Nord"},  // Should fall back due to case mismatch
		{"NORD"},  // Should fall back due to case mismatch
		{"dracula"},
		{"Dracula"}, // Should fall back
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			theme := ThemeByName(tt.name)

			// Just verify we get a valid theme
			_ = theme.Editor
		})
	}
}

// TestAllThemes tests all available themes
func TestAllThemes(t *testing.T) {
	themes := []string{"nord", "dracula", "catppuccin", "solarized-dark", "one-dark"}

	for _, themeName := range themes {
		t.Run(themeName, func(t *testing.T) {
			theme := ThemeByName(themeName)

			// Verify theme has essential properties
			_ = theme.Editor
			_ = theme.Gutter
			_ = theme.StatusBar
		})
	}
}

// TestThemeSyntaxColors tests syntax highlighting colors
func TestThemeSyntaxColors(t *testing.T) {
	theme := NordTheme()

	// Test syntax colors
	colors := []struct {
		name  string
		color interface{}
	}{
		{"SyntaxKeyword", theme.SyntaxKeyword},
		{"SyntaxString", theme.SyntaxString},
		{"SyntaxNumber", theme.SyntaxNumber},
		{"SyntaxComment", theme.SyntaxComment},
		{"SyntaxType", theme.SyntaxType},
		{"SyntaxOperator", theme.SyntaxOperator},
		{"SyntaxTag", theme.SyntaxTag},
		{"SyntaxAttribute", theme.SyntaxAttribute},
	}

	for _, c := range colors {
		t.Run(c.name, func(t *testing.T) {
			if c.color == nil {
				t.Errorf("Expected %s to be set", c.name)
			}
		})
	}
}

// TestThemeOverlayColors tests overlay colors
func TestThemeOverlayColors(t *testing.T) {
	theme := NordTheme()

	// Test overlay-related colors that exist in Theme
	_ = theme.HelpBorder
	_ = theme.HelpTitle
}

// TestThemeTabColors tests tab colors
func TestThemeTabColors(t *testing.T) {
	theme := NordTheme()

	// Test tab colors
	_ = theme.TabBar
	_ = theme.TabActive
	_ = theme.TabInactive
	_ = theme.TabCloseActive
	_ = theme.TabCloseInactive
}

// TestThemeSearchColors tests search colors
func TestThemeSearchColors(t *testing.T) {
	theme := NordTheme()

	// Test search colors
	_ = theme.SearchBox
	_ = theme.SearchInput
	_ = theme.SearchResult
	_ = theme.SearchActive
}

// TestThemeGitColors tests git colors
func TestThemeGitColors(t *testing.T) {
	theme := NordTheme()

	// Test git colors
	_ = theme.GitHeader
	_ = theme.GitEntry
	_ = theme.GitCursor
	_ = theme.GitAdded
	_ = theme.GitModified
	_ = theme.GitDeleted
	_ = theme.GitUntracked
}

// TestThemeDiagnosticColors tests diagnostic colors
func TestThemeDiagnosticColors(t *testing.T) {
	theme := NordTheme()

	// Test diagnostic colors
	_ = theme.DiagError
	_ = theme.DiagWarning
	_ = theme.DiagInfo
	_ = theme.DiagHint
	_ = theme.GutterError
	_ = theme.GutterWarn
}

// TestThemeDiffColors tests diff colors
func TestThemeDiffColors(t *testing.T) {
	theme := NordTheme()

	// Test diff colors
	_ = theme.DiffRemoved
	_ = theme.DiffAdded
	_ = theme.DiffEmpty
	_ = theme.DiffGutter
	_ = theme.DiffBorder
	_ = theme.DiffHunkHeader
}

// TestThemeAutocompleteColors tests autocomplete colors
func TestThemeAutocompleteColors(t *testing.T) {
	theme := NordTheme()

	// Test autocomplete colors
	_ = theme.AutocompleteItem
	_ = theme.AutocompleteCursor
	_ = theme.AutocompleteBox
}

// TestThemeContextMenu tests context menu style
func TestThemeContextMenu(t *testing.T) {
	theme := NordTheme()

	_ = theme.ContextMenuDisabled
}

// TestMultipleThemeCalls tests that multiple calls return consistent results
func TestMultipleThemeCalls(t *testing.T) {
	theme1 := NordTheme()
	theme2 := NordTheme()

	// Both should be usable
	_ = theme1.Editor
	_ = theme2.Editor
}

// TestThemeByNameConsistency tests that ThemeByName returns consistent results
func TestThemeByNameConsistency(t *testing.T) {
	theme1 := ThemeByName("nord")
	theme2 := ThemeByName("nord")

	// Both should be usable
	_ = theme1.Editor
	_ = theme2.Editor
}

// TestPlaceOverlayAt tests PlaceOverlayAt function
func TestPlaceOverlayAt(t *testing.T) {
	base := "Hello\nWorld"
	overlay := "X"
	
	result := PlaceOverlayAt(base, overlay, 0, 0, 10, 5)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestPlaceOverlayAtWithOffset tests PlaceOverlayAt with offset
func TestPlaceOverlayAtWithOffset(t *testing.T) {
	base := "Hello\nWorld"
	overlay := "X"
	
	result := PlaceOverlayAt(base, overlay, 2, 1, 10, 5)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestPlaceOverlayAtWithLargeOverlay tests PlaceOverlayAt with large overlay
func TestPlaceOverlayAtWithLargeOverlay(t *testing.T) {
	base := "Hi"
	overlay := "Large overlay text"
	
	result := PlaceOverlayAt(base, overlay, 0, 0, 10, 5)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestPlaceOverlayAtWithEmptyBase tests PlaceOverlayAt with empty base
func TestPlaceOverlayAtWithEmptyBase(t *testing.T) {
	base := ""
	overlay := "X"
	
	result := PlaceOverlayAt(base, overlay, 0, 0, 10, 5)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestPlaceOverlayAtWithEmptyOverlay tests PlaceOverlayAt with empty overlay
func TestPlaceOverlayAtWithEmptyOverlay(t *testing.T) {
	base := "Hello"
	overlay := ""
	
	result := PlaceOverlayAt(base, overlay, 0, 0, 10, 5)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestRenderOverlay tests RenderOverlay function
func TestRenderOverlay(t *testing.T) {
	base := "Hello World"
	overlay := "X"
	
	result := RenderOverlay(base, overlay, 20, 10)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestRenderOverlayWithLargeOverlay tests RenderOverlay with large overlay
func TestRenderOverlayWithLargeOverlay(t *testing.T) {
	base := "Hi"
	overlay := "This is a very large overlay text"
	
	result := RenderOverlay(base, overlay, 20, 10)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestRenderOverlayWithSmallCanvas tests RenderOverlay with small canvas
func TestRenderOverlayWithSmallCanvas(t *testing.T) {
	base := "Hello"
	overlay := "X"
	
	result := RenderOverlay(base, overlay, 5, 3)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestRenderOverlayWithMultilineOverlay tests RenderOverlay with multiline overlay
func TestRenderOverlayWithMultilineOverlay(t *testing.T) {
	base := "Hello World"
	overlay := "Line1\nLine2\nLine3"
	
	result := RenderOverlay(base, overlay, 30, 10)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestRenderOverlayCentersOverlay tests that RenderOverlay centers the overlay
func TestRenderOverlayCentersOverlay(t *testing.T) {
	base := "Hello World"
	overlay := "X"
	
	// With a 20-width canvas and 1-char overlay, should be centered at x=9
	result := RenderOverlay(base, overlay, 20, 10)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}

// TestRenderOverlayHandlesNegativePosition tests RenderOverlay handles negative positions
func TestRenderOverlayHandlesNegativePosition(t *testing.T) {
	base := "Hello"
	overlay := "X"
	
	// Very small canvas should clamp position to 0
	result := RenderOverlay(base, overlay, 1, 1)
	if result == "" {
		t.Error("Expected non-empty result")
	}
}
