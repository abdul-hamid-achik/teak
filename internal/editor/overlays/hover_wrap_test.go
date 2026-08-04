package overlays

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func TestHoverWrapsLongLinesInsteadOfTruncating(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	long := "func DoSomethingVeryLongIndeed(param1 int, param2 string) (result bool, err error)"
	h.Show(long)

	view := h.View()
	if strings.Contains(view, "...") {
		t.Fatalf("hover still hard-truncates long lines:\n%s", view)
	}
	// The wrapped view must keep the whole signature, split across rows.
	for _, part := range []string{"func", "DoSomethingVeryLongIndeed", "param2", "error"} {
		if !strings.Contains(view, part) {
			t.Fatalf("wrapped hover lost %q:\n%s", part, view)
		}
	}
	if !strings.Contains(view, "\n") {
		t.Fatalf("long line did not wrap to multiple rows:\n%s", view)
	}
}

func TestHoverBoundsVeryLongContent(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("line of documentation text\n")
	}
	h.Show(sb.String())

	view := h.View()
	lines := strings.Split(view, "\n")
	// The popup must stay bounded even for enormous hover payloads.
	if len(lines) > 40 {
		t.Fatalf("hover rendered %d lines, want a bounded popup", len(lines))
	}
}

func TestHoverKeepsShortContentUnchanged(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	h.Show("short doc")
	if view := h.View(); !strings.Contains(view, "short doc") {
		t.Fatalf("hover view = %q, want the content rendered", view)
	}
}
