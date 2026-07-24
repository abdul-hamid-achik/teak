package highlight

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func tokenizedHighlighter(t *testing.T, lines int) (*Highlighter, []byte) {
	t.Helper()
	var sb strings.Builder
	for i := range lines {
		sb.WriteString("func f")
		sb.WriteString(string(rune('a' + i%26)))
		sb.WriteString("() error { return nil }\n")
	}
	content := []byte(sb.String())
	h := New("main.go", ui.DefaultTheme())
	h.Tokenize(content)
	return h, content
}

func TestInvalidateEditedKeepsTokensOutsideTheEdit(t *testing.T) {
	h, _ := tokenizedHighlighter(t, 20)

	if len(h.Line(0)) == 0 || len(h.Line(10)) == 0 {
		t.Fatal("setup: expected tokens on lines 0 and 10")
	}

	// Typing one character on line 5 changes no line count.
	h.InvalidateEdited(5, 5, 0)

	// Clearing the whole cache made the entire viewport flash to plain text for
	// the ~150ms until the async pass landed.
	if len(h.Line(0)) == 0 {
		t.Error("line 0 lost its tokens after an edit on line 5")
	}
	if len(h.Line(10)) == 0 {
		t.Error("line 10 lost its tokens after an edit on line 5")
	}
	if len(h.Line(5)) != 0 {
		t.Error("the edited line kept tokens that no longer describe it")
	}
}

func TestInvalidateEditedStillSchedulesRetokenization(t *testing.T) {
	h, _ := tokenizedHighlighter(t, 20)

	if !h.CoversRange(0, 20) {
		t.Fatal("setup: expected the document to be covered")
	}

	h.InvalidateEdited(5, 5, 0)

	// Coverage is what tells the editor a range is fresh. Retaining it would
	// leave stale tokens on screen permanently instead of briefly.
	if h.CoversRange(0, 20) {
		t.Error("coverage survived the edit; no retokenization would be scheduled")
	}
	if !h.IsDirty() {
		t.Error("highlighter is not marked dirty after an edit")
	}
}

func TestInvalidateEditedShiftsTokensWhenLinesAreInserted(t *testing.T) {
	h, _ := tokenizedHighlighter(t, 20)
	before := h.Line(10)
	if len(before) == 0 {
		t.Fatal("setup: expected tokens on line 10")
	}
	firstText := before[0].Text

	// Pressing Enter on line 2 pushes everything below it down by one.
	h.InvalidateEdited(2, 2, 1)

	after := h.Line(11)
	if len(after) == 0 {
		t.Fatalf("tokens for the line that moved from 10 to 11 were lost")
	}
	if after[0].Text != firstText {
		t.Errorf("line 11 first token = %q, want %q from the line that moved down",
			after[0].Text, firstText)
	}
}

func TestInvalidateEditedShiftsTokensWhenLinesAreDeleted(t *testing.T) {
	h, _ := tokenizedHighlighter(t, 20)
	before := h.Line(10)
	if len(before) == 0 {
		t.Fatal("setup: expected tokens on line 10")
	}
	firstText := before[0].Text

	// Deleting a selection spanning lines 2-3 pulls everything below up by one.
	h.InvalidateEdited(2, 3, -1)

	after := h.Line(9)
	if len(after) == 0 {
		t.Fatal("tokens for the line that moved from 10 to 9 were lost")
	}
	if after[0].Text != firstText {
		t.Errorf("line 9 first token = %q, want %q from the line that moved up",
			after[0].Text, firstText)
	}
}

func TestInvalidateEditedOnEmptyCacheFallsBackToFullInvalidate(t *testing.T) {
	h := New("main.go", ui.DefaultTheme())

	h.InvalidateEdited(0, 0, 0)

	if !h.IsDirty() {
		t.Error("highlighter should be dirty")
	}
	if h.Line(0) != nil {
		t.Error("expected no tokens from an untokenized highlighter")
	}
}

func TestInvalidateEditedHandlesOutOfRangeLines(t *testing.T) {
	// A stale or malformed change must not panic or corrupt the cache.
	tests := []struct {
		name              string
		start, end, delta int
	}{
		{"negative start", -5, 2, 0},
		{"end before start", 10, 3, 0},
		{"beyond document", 500, 600, 0},
		{"delta larger than document", 0, 0, -1000},
		{"large positive delta", 1, 1, 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := tokenizedHighlighter(t, 20)
			h.InvalidateEdited(tc.start, tc.end, tc.delta)
			// Reading around the document must stay safe afterwards.
			for line := range 30 {
				_ = h.Line(line)
			}
		})
	}
}

func TestInvalidateEditedShiftsSparseLines(t *testing.T) {
	h := New("main.go", ui.DefaultTheme())
	h.sparseLines = map[int][]StyledToken{
		1:  {{Text: "one"}},
		10: {{Text: "ten"}},
	}
	h.lineCount = 20

	h.InvalidateEdited(5, 5, 2)

	if got := h.Line(1); len(got) == 0 || got[0].Text != "one" {
		t.Errorf("sparse line above the edit was not preserved: %+v", got)
	}
	if got := h.Line(12); len(got) == 0 || got[0].Text != "ten" {
		t.Errorf("sparse line 10 did not shift to 12: %+v", got)
	}
}
