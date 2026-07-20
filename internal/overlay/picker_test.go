package overlay

import (
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

func init() {
	zone.NewGlobal()
}

func testItems() []PickerItem {
	return []PickerItem{
		{Label: "main.go", Value: "main.go"},
		{Label: "internal/app/app.go", Value: "app.go"},
		{Label: "internal/editor/editor.go", Value: "editor.go"},
		{Label: "internal/overlay/picker.go", Value: "picker.go"},
		{Label: "go.mod", Value: "go.mod"},
		{Label: "README.md", Value: "readme"},
	}
}

func newTestPicker() *Picker {
	return NewPicker("Test", testItems(), ui.DefaultTheme(), "test")
}

func keyDown() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyDown} }
func keyUp() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: tea.KeyUp} }
func keyEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func keyEsc() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEscape} }

func TestPickerInitialState(t *testing.T) {
	p := newTestPicker()
	if p.IsDismissed() {
		t.Error("new picker should not be dismissed")
	}
	if !p.CapturesInput() {
		t.Error("picker should capture input")
	}
	if p.FilteredCount() != 6 {
		t.Errorf("initial filtered count = %d, want 6", p.FilteredCount())
	}
	if p.Cursor() != 0 {
		t.Errorf("initial cursor = %d, want 0", p.Cursor())
	}
}

func TestPickerRefilter(t *testing.T) {
	// Test the refilter logic directly by manipulating items
	tests := []struct {
		name      string
		items     []PickerItem
		wantCount int
	}{
		{"all items", testItems(), 6},
		{"empty", nil, 0},
		{"two items", []PickerItem{{Label: "a"}, {Label: "b"}}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPicker("Test", tt.items, ui.DefaultTheme(), "test")
			if p.FilteredCount() != tt.wantCount {
				t.Errorf("FilteredCount()=%d, want %d", p.FilteredCount(), tt.wantCount)
			}
		})
	}
}

func TestPickerNavigation(t *testing.T) {
	p := newTestPicker()

	// Move down
	var o Overlay
	o, _ = p.Update(keyDown())
	p = o.(*Picker)
	if p.Cursor() != 1 {
		t.Errorf("after down: cursor=%d, want 1", p.Cursor())
	}

	// Move down again
	o, _ = p.Update(keyDown())
	p = o.(*Picker)
	if p.Cursor() != 2 {
		t.Errorf("after 2x down: cursor=%d, want 2", p.Cursor())
	}

	// Move up
	o, _ = p.Update(keyUp())
	p = o.(*Picker)
	if p.Cursor() != 1 {
		t.Errorf("after up: cursor=%d, want 1", p.Cursor())
	}

	// Don't go below 0
	o, _ = p.Update(keyUp())
	p = o.(*Picker)
	o, _ = p.Update(keyUp())
	p = o.(*Picker)
	if p.Cursor() != 0 {
		t.Errorf("should not go below 0: cursor=%d", p.Cursor())
	}
}

func TestPickerDismissOnEscape(t *testing.T) {
	p := newTestPicker()
	o, cmd := p.Update(keyEsc())
	p = o.(*Picker)
	if !p.IsDismissed() {
		t.Error("picker should be dismissed after escape")
	}
	if cmd == nil {
		t.Error("should emit PickerCloseMsg command")
	}
	msg := cmd()
	if _, ok := msg.(PickerCloseMsg); !ok {
		t.Errorf("expected PickerCloseMsg, got %T", msg)
	}
}

func TestPickerSelectOnEnter(t *testing.T) {
	p := newTestPicker()

	// Move to second item and select
	var o Overlay
	o, _ = p.Update(keyDown())
	p = o.(*Picker)
	o, cmd := p.Update(keyEnter())
	p = o.(*Picker)

	if !p.IsDismissed() {
		t.Error("picker should be dismissed after enter")
	}
	if cmd == nil {
		t.Fatal("should emit PickerSelectMsg command")
	}
	msg := cmd()
	sel, ok := msg.(PickerSelectMsg)
	if !ok {
		t.Fatalf("expected PickerSelectMsg, got %T", msg)
	}
	if sel.Item.Label != "internal/app/app.go" {
		t.Errorf("selected item label = %q, want %q", sel.Item.Label, "internal/app/app.go")
	}
}

func TestPickerEnterOnEmpty(t *testing.T) {
	p := NewPicker("Test", nil, ui.DefaultTheme(), "test")
	o, cmd := p.Update(keyEnter())
	p = o.(*Picker)
	if p.IsDismissed() {
		t.Error("enter on empty should not dismiss")
	}
	if cmd != nil {
		t.Error("enter on empty should not emit command")
	}
}

func TestPickerSetItems(t *testing.T) {
	p := newTestPicker()
	p.SetItems([]PickerItem{
		{Label: "alpha"},
		{Label: "beta"},
	})
	if p.FilteredCount() != 2 {
		t.Errorf("after SetItems: FilteredCount()=%d, want 2", p.FilteredCount())
	}
}

func TestPickerView(t *testing.T) {
	p := newTestPicker()
	p.SetSize(60, 30)
	v := p.View()
	if v == "" {
		t.Error("View() should not be empty")
	}
	if !containsStr(v, "Test") {
		t.Error("View() should contain the title")
	}
}

func TestPickerTruncatesUnicodeWithoutBreakingUTF8(t *testing.T) {
	got := truncStr("archivo-áéí-漢字", 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated label is not valid UTF-8: %q", got)
	}
	if lipgloss.Width(got) > 10 {
		t.Fatalf("truncated label width = %d, want <= 10", lipgloss.Width(got))
	}
}

func TestPickerTinyTerminalUsesPositiveInputAndContentWidths(t *testing.T) {
	p := newTestPicker()
	p.SetSize(4, 3)
	if p.input.Width() < 1 {
		t.Fatalf("input width = %d, want at least one cell", p.input.Width())
	}
	if view := p.View(); view == "" {
		t.Fatal("tiny terminal picker view must remain renderable")
	}
}

func TestPickerNavigationBounds(t *testing.T) {
	// Test with 2 items — cursor should clamp at 0 and 1
	p := NewPicker("Test", []PickerItem{
		{Label: "first"},
		{Label: "second"},
	}, ui.DefaultTheme(), "test")

	var o Overlay

	// Move down to last item
	o, _ = p.Update(keyDown())
	p = o.(*Picker)
	if p.Cursor() != 1 {
		t.Errorf("cursor=%d, want 1", p.Cursor())
	}

	// Try to move past end
	o, _ = p.Update(keyDown())
	p = o.(*Picker)
	if p.Cursor() != 1 {
		t.Errorf("cursor should clamp at 1, got %d", p.Cursor())
	}
}

func TestPickerVisibleRangeIsBoundedToRenderedItems(t *testing.T) {
	items := make([]PickerItem, 20_000)
	for i := range items {
		items[i] = PickerItem{Label: "item"}
	}
	p := NewPicker("Test", items, ui.DefaultTheme(), "picker-visible")
	p.SetSize(80, 24)
	p.scrollY = 15_000

	start, end := p.visibleRange()
	if start != 15_000 {
		t.Fatalf("visible start = %d, want 15000", start)
	}
	if got, want := end-start, p.visibleCount(); got != want {
		t.Fatalf("visible range size = %d, want %d", got, want)
	}
}

func TestPickerRefilterReusesResultCapacity(t *testing.T) {
	items := make([]PickerItem, 20_000)
	for i := range items {
		items[i] = PickerItem{Label: "package-item"}
	}
	p := NewPicker("Test", items, ui.DefaultTheme(), "picker-capacity")
	p.input.SetValue("pkg")
	p.refilter()
	if len(p.filtered) == 0 {
		t.Fatal("expected matching picker items")
	}
	firstBackingItem := &p.filtered[0]

	p.refilter()
	if len(p.filtered) == 0 {
		t.Fatal("expected matching picker items after refilter")
	}
	if &p.filtered[0] != firstBackingItem {
		t.Fatal("refilter allocated a new result buffer instead of reusing capacity")
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
