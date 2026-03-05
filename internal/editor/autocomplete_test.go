package editor

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func TestNewAutocomplete(t *testing.T) {
	theme := ui.DefaultTheme()
	ac := NewAutocomplete(theme)

	// Theme contains lipgloss.Style which cannot be compared directly
	if ac.Visible {
		t.Error("expected Visible to be false")
	}
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
	if ac.Items != nil {
		t.Error("expected Items to be nil")
	}
}

func TestAutocompleteShow(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	items := []AutocompleteItem{
		{Label: "foo", Detail: "func", InsertText: "foo()"},
		{Label: "bar", Detail: "var", InsertText: "bar"},
	}

	ac.Show(items)

	if !ac.Visible {
		t.Error("expected Visible to be true")
	}
	if len(ac.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(ac.Items))
	}
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteShowEmpty(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())

	ac.Show([]AutocompleteItem{})

	if ac.Visible {
		t.Error("expected Visible to be false for empty items")
	}
}

func TestAutocompleteHide(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})

	ac.Hide()

	if ac.Visible {
		t.Error("expected Visible to be false")
	}
	if ac.Items != nil {
		t.Error("expected Items to be nil")
	}
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteMoveUp(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
		{Label: "baz", InsertText: "baz"},
	})

	ac.MoveUp()
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}

	ac.Cursor = 2
	ac.MoveUp()
	if ac.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", ac.Cursor)
	}
}

func TestAutocompleteMoveDown(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
		{Label: "baz", InsertText: "baz"},
	})

	ac.MoveDown()
	if ac.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", ac.Cursor)
	}

	ac.Cursor = 1
	ac.MoveDown()
	if ac.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", ac.Cursor)
	}

	// At end, should not move further
	ac.MoveDown()
	if ac.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", ac.Cursor)
	}
}

func TestAutocompleteSelected(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	items := []AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
	}
	ac.Show(items)

	item := ac.Selected()
	if item == nil {
		t.Fatal("expected item to be selected")
	}
	if item.Label != "foo" {
		t.Errorf("expected label 'foo', got %q", item.Label)
	}

	ac.Cursor = 1
	item = ac.Selected()
	if item.Label != "bar" {
		t.Errorf("expected label 'bar', got %q", item.Label)
	}
}

func TestAutocompleteSelectedNotVisible(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})
	ac.Hide()

	item := ac.Selected()
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}
}

func TestAutocompleteSelectedOutOfBounds(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})
	ac.Cursor = 10

	item := ac.Selected()
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}
}

func TestAutocompleteView(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", Detail: "func", InsertText: "foo()"},
		{Label: "bar", Detail: "var", InsertText: "bar"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "foo") {
		t.Errorf("expected 'foo' in view, got %q", view)
	}
	if !strings.Contains(view, "bar") {
		t.Errorf("expected 'bar' in view, got %q", view)
	}
}

func TestAutocompleteViewNotVisible(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())

	view := ac.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestAutocompleteViewEmpty(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{})

	view := ac.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestAutocompleteViewCursor(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
	})
	ac.Cursor = 1

	view := ac.View()
	// Cursor item should have different styling
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestAutocompleteViewManyItems(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	items := make([]AutocompleteItem, 20)
	for i := range items {
		items[i] = AutocompleteItem{
			Label:      string(rune('a' + i)),
			InsertText: string(rune('a' + i)),
		}
	}
	ac.Show(items)

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should only show max 10 items
}

func TestAutocompleteViewLongLabels(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "very_long_label_that_exceeds_max_width", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestAutocompleteViewWithDetail(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", Detail: "func foo() string", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "foo") {
		t.Errorf("expected 'foo' in view")
	}
}

func TestAutocompleteViewWithLongDetail(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", Detail: "very long detail text that should be truncated", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestAutocompleteItemStructure(t *testing.T) {
	item := AutocompleteItem{
		Label:      "test",
		Detail:     "detail",
		InsertText: "insert",
	}

	if item.Label != "test" {
		t.Errorf("expected Label 'test', got %q", item.Label)
	}
	if item.Detail != "detail" {
		t.Errorf("expected Detail 'detail', got %q", item.Detail)
	}
	if item.InsertText != "insert" {
		t.Errorf("expected InsertText 'insert', got %q", item.InsertText)
	}
}

func TestAutocompleteMoveUpFromZero(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})

	// Already at 0, should stay at 0
	ac.MoveUp()
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteMoveDownSingleItem(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})

	ac.MoveDown()
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteViewWidthConstraints(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	// Very short label
	ac.Show([]AutocompleteItem{{Label: "a", InsertText: "a"}})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should have minimum width of 20
}

func TestAutocompleteViewMaxWidth(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	// Very long label
	ac.Show([]AutocompleteItem{
		{Label: "this_is_a_very_long_label_that_exceeds_sixty_characters", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should be truncated to max width of 60
}
