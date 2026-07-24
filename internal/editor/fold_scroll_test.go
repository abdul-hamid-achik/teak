package editor

import (
	"strings"
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

// foldedEditor returns an editor over `lines` lines with [foldStart, foldEnd]
// collapsed, and the cursor placed below the fold.
func foldedEditor(t *testing.T, lines, foldStart, foldEnd int) Editor {
	t.Helper()
	var sb strings.Builder
	for i := range lines {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", 1+i%5))
		sb.WriteString("\n")
	}
	buf := text.NewBufferFromBytes([]byte(sb.String()))
	buf.FilePath = "main.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(80, 20)
	ed.Folds.SetRegions([]FoldRegion{{StartLine: foldStart, EndLine: foldEnd, Collapsed: true}})
	return ed
}

func TestCursorScrollStaysWithinCollapsedDocument(t *testing.T) {
	const totalLines = 100
	ed := foldedEditor(t, totalLines, 5, 90)

	visible := ed.Folds.TotalVisibleLines(totalLines)
	if visible >= totalLines {
		t.Fatalf("setup: fold did not hide anything (%d visible of %d)", visible, totalLines)
	}

	// Clicking below the fold lands on a high buffer line; the next cursor move
	// used to push ScrollY past the end of the collapsed document, leaving the
	// viewport blank with no way back except manual scrolling.
	ed.Buffer.SetCursor(text.Position{Line: 95, Col: 0})
	ed.EnsureCursorVisible()

	maxScroll := max(visible-ed.Viewport.Height, 0)
	if ed.Viewport.ScrollY > maxScroll {
		t.Errorf("ScrollY = %d, want at most %d (%d visible rows, height %d)",
			ed.Viewport.ScrollY, maxScroll, visible, ed.Viewport.Height)
	}
	if ed.Viewport.ScrollY < 0 {
		t.Errorf("ScrollY = %d, want >= 0", ed.Viewport.ScrollY)
	}
}

func TestCursorScrollUsesVisualRowsWhenFolded(t *testing.T) {
	const totalLines = 100
	ed := foldedEditor(t, totalLines, 5, 50)

	// Line 95 sits far enough below the fold that its visual row is off screen,
	// so this genuinely exercises scrolling rather than passing because the
	// cursor already happened to be visible.
	ed.Buffer.SetCursor(text.Position{Line: 95, Col: 0})
	ed.EnsureCursorVisible()

	visualRow := ed.Folds.BufferLineToVisual(95, totalLines)
	if visualRow < ed.Viewport.Height {
		t.Fatalf("setup: visual row %d is already on screen; test would pass vacuously", visualRow)
	}
	if visualRow < ed.Viewport.ScrollY || visualRow >= ed.Viewport.ScrollY+ed.Viewport.Height {
		t.Errorf("cursor visual row %d not visible in [%d, %d)",
			visualRow, ed.Viewport.ScrollY, ed.Viewport.ScrollY+ed.Viewport.Height)
	}
}

func TestCursorScrollUnaffectedWithoutCollapsedRegions(t *testing.T) {
	const totalLines = 100
	ed := foldedEditor(t, totalLines, 5, 50)
	// Expanding must restore plain buffer-line scrolling. SetRegions cannot be
	// used here: it deliberately preserves the collapsed state of matching
	// ranges so a fresh set of LSP fold ranges does not undo the user's folds.
	ed.Folds.UnfoldAll()
	if ed.Folds.HasCollapsedRegions() {
		t.Fatal("setup: regions still collapsed after UnfoldAll")
	}

	ed.Buffer.SetCursor(text.Position{Line: 60, Col: 0})
	ed.EnsureCursorVisible()

	if 60 < ed.Viewport.ScrollY || 60 >= ed.Viewport.ScrollY+ed.Viewport.Height {
		t.Errorf("cursor line 60 not visible in [%d, %d) with nothing folded",
			ed.Viewport.ScrollY, ed.Viewport.ScrollY+ed.Viewport.Height)
	}
}

func TestHasCollapsedRegions(t *testing.T) {
	tests := []struct {
		name    string
		regions []FoldRegion
		want    bool
	}{
		{"no regions", nil, false},
		{"all expanded", []FoldRegion{{StartLine: 1, EndLine: 5}}, false},
		{"one collapsed", []FoldRegion{{StartLine: 1, EndLine: 5, Collapsed: true}}, true},
		{"mixed", []FoldRegion{{StartLine: 1, EndLine: 5}, {StartLine: 10, EndLine: 20, Collapsed: true}}, true},
		{"collapsed but empty range", []FoldRegion{{StartLine: 3, EndLine: 3, Collapsed: true}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var fs FoldState
			fs.SetRegions(tc.regions)
			if got := fs.HasCollapsedRegions(); got != tc.want {
				t.Errorf("HasCollapsedRegions() = %v, want %v", got, tc.want)
			}
		})
	}
}
