package editor

import (
	"strings"
	"testing"

	"teak/internal/ui"
)

func TestNewContextMenu(t *testing.T) {
	theme := ui.DefaultTheme()
	menu := NewContextMenu(theme)

	// Theme contains lipgloss.Style which cannot be compared directly
	if menu.Visible {
		t.Error("expected Visible to be false")
	}
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
	if menu.Items != nil {
		t.Error("expected Items to be nil")
	}
	if menu.X != 0 || menu.Y != 0 {
		t.Errorf("expected position (0,0), got (%d,%d)", menu.X, menu.Y)
	}
}

func TestContextMenuShow(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	items := []ContextMenuItem{
		{Label: "Cut", Shortcut: "Ctrl+X", Action: "cut"},
		{Label: "Copy", Shortcut: "Ctrl+C", Action: "copy"},
		{Label: "Paste", Shortcut: "Ctrl+V", Action: "paste"},
	}

	menu.Show(items, 10, 20)

	if !menu.Visible {
		t.Error("expected Visible to be true")
	}
	if len(menu.Items) != 3 {
		t.Errorf("expected 3 items, got %d", len(menu.Items))
	}
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
	if menu.X != 10 || menu.Y != 20 {
		t.Errorf("expected position (10,20), got (%d,%d)", menu.X, menu.Y)
	}
}

func TestContextMenuShowEmpty(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())

	menu.Show([]ContextMenuItem{}, 10, 20)

	if menu.Visible {
		t.Error("expected Visible to be false for empty items")
	}
}

func TestContextMenuShowWithDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	items := []ContextMenuItem{
		{Label: "Cut", Action: "cut", Disabled: true},
		{Label: "Copy", Action: "copy", Disabled: true},
		{Label: "Paste", Action: "paste", Disabled: false},
	}

	menu.Show(items, 10, 20)

	// Should skip to first enabled item
	if menu.Cursor != 2 {
		t.Errorf("expected Cursor 2 (first enabled), got %d", menu.Cursor)
	}
}

func TestContextMenuShowWithSeparator(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	items := []ContextMenuItem{
		{Label: ""}, // separator
		{Label: "Cut", Action: "cut"},
	}

	menu.Show(items, 10, 20)

	// Should skip separator
	if menu.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", menu.Cursor)
	}
}

func TestContextMenuShowAllDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	items := []ContextMenuItem{
		{Label: "Cut", Action: "cut", Disabled: true},
		{Label: "Copy", Action: "copy", Disabled: true},
	}

	menu.Show(items, 10, 20)

	// Should wrap around to 0
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
}

func TestContextMenuHide(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 10, 20)

	menu.Hide()

	if menu.Visible {
		t.Error("expected Visible to be false")
	}
	if menu.Items != nil {
		t.Error("expected Items to be nil")
	}
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
}

func TestContextMenuMoveUp(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	menu.MoveUp()
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}

	menu.Cursor = 2
	menu.MoveUp()
	if menu.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", menu.Cursor)
	}
}

func TestContextMenuMoveUpSkipsDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy", Disabled: true},
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	menu.Cursor = 2
	menu.MoveUp()
	// Should skip disabled "Copy"
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
}

func TestContextMenuMoveUpSkipsSeparator(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: ""}, // separator
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	menu.Cursor = 2
	menu.MoveUp()
	// Should skip separator
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
}

func TestContextMenuMoveDown(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	menu.MoveDown()
	if menu.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", menu.Cursor)
	}

	menu.MoveDown()
	if menu.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", menu.Cursor)
	}

	// At end, should not move further
	menu.MoveDown()
	if menu.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", menu.Cursor)
	}
}

func TestContextMenuMoveDownSkipsDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy", Disabled: true},
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	menu.MoveDown()
	// Should skip disabled "Copy"
	if menu.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", menu.Cursor)
	}
}

func TestContextMenuMoveDownSkipsSeparator(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: ""}, // separator
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	menu.MoveDown()
	// Should skip separator
	if menu.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", menu.Cursor)
	}
}

func TestContextMenuSelected(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	items := []ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
	}
	menu.Show(items, 10, 20)

	item := menu.Selected()
	if item == nil {
		t.Fatal("expected item to be selected")
	}
	if item.Label != "Cut" {
		t.Errorf("expected label 'Cut', got %q", item.Label)
	}

	menu.Cursor = 1
	item = menu.Selected()
	if item.Label != "Copy" {
		t.Errorf("expected label 'Copy', got %q", item.Label)
	}
}

func TestContextMenuSelectedNotVisible(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 10, 20)
	menu.Hide()

	item := menu.Selected()
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}
}

func TestContextMenuSelectedDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut", Disabled: true},
		{Label: "Copy", Action: "copy"},
	}, 10, 20)

	// Manually set cursor to disabled item
	menu.Cursor = 0
	item := menu.Selected()
	if item != nil {
		t.Errorf("expected nil item for disabled, got %v", item)
	}
}

func TestContextMenuSelectedSeparator(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: ""}, // separator
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	// Manually set cursor to separator
	menu.Cursor = 1
	item := menu.Selected()
	if item != nil {
		t.Errorf("expected nil item for separator, got %v", item)
	}
}

func TestContextMenuSelectedOutOfBounds(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 10, 20)
	menu.Cursor = 10

	item := menu.Selected()
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}
}

func TestContextMenuItemCount(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	items := make([]ContextMenuItem, 15)
	for i := range items {
		items[i] = ContextMenuItem{Label: string(rune('a' + i)), Action: string(rune('a' + i))}
	}
	menu.Show(items, 10, 20)

	count := menu.ItemCount()
	if count != 12 {
		t.Errorf("expected count 12 (capped), got %d", count)
	}
}

func TestContextMenuItemCountSmall(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
	}, 10, 20)

	count := menu.ItemCount()
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestContextMenuSelectAt(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	item := menu.SelectAt(1)
	if item == nil {
		t.Fatal("expected item to be selected")
	}
	if item.Label != "Copy" {
		t.Errorf("expected label 'Copy', got %q", item.Label)
	}
	if menu.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", menu.Cursor)
	}
}

func TestContextMenuSelectAtOutOfBounds(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 10, 20)

	item := menu.SelectAt(100)
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}

	item = menu.SelectAt(-1)
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}
}

func TestContextMenuSelectAtDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut", Disabled: true},
		{Label: "Copy", Action: "copy"},
	}, 10, 20)

	item := menu.SelectAt(0)
	if item != nil {
		t.Errorf("expected nil item for disabled, got %v", item)
	}
}

func TestContextMenuSelectAtSeparator(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: ""}, // separator
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	item := menu.SelectAt(1)
	if item != nil {
		t.Errorf("expected nil item for separator, got %v", item)
	}
}

func TestContextMenuView(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Shortcut: "Ctrl+X", Action: "cut"},
		{Label: "Copy", Shortcut: "Ctrl+C", Action: "copy"},
	}, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "Cut") {
		t.Errorf("expected 'Cut' in view")
	}
	if !strings.Contains(view, "Ctrl+X") {
		t.Errorf("expected 'Ctrl+X' in view")
	}
}

func TestContextMenuViewNotVisible(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())

	view := menu.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestContextMenuViewEmpty(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{}, 10, 20)

	view := menu.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestContextMenuViewWithSeparator(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: ""}, // separator
		{Label: "Paste", Action: "paste"},
	}, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Separator should be rendered
}

func TestContextMenuViewWithDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut", Disabled: true},
		{Label: "Copy", Action: "copy"},
	}, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Disabled item should have different styling
}

func TestContextMenuViewCursor(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
	}, 10, 20)
	menu.Cursor = 1

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Cursor item should have different styling
}

func TestContextMenuViewWidthConstraints(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "a", Action: "a"},
	}, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should have minimum width of 20
}

func TestContextMenuViewMaxWidth(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "very_long_label_that_exceeds_fifty_characters", Action: "a"},
	}, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should be capped to max width of 50
}

func TestContextMenuViewManyItems(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	items := make([]ContextMenuItem, 20)
	for i := range items {
		items[i] = ContextMenuItem{Label: string(rune('a' + i)), Action: string(rune('a' + i))}
	}
	menu.Show(items, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should only show max 12 items
}

func TestContextMenuViewWithShortcut(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Cut", Shortcut: "Ctrl+X", Action: "cut"},
	}, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "Ctrl+X") {
		t.Errorf("expected 'Ctrl+X' in view")
	}
}

func TestContextMenuViewWithoutShortcut(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Show([]ContextMenuItem{
		{Label: "Custom Action", Action: "custom"},
	}, 10, 20)

	view := menu.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestContextMenuStructure(t *testing.T) {
	item := ContextMenuItem{
		Label:    "Test",
		Shortcut: "Ctrl+T",
		Action:   "test",
		Disabled: true,
	}

	if item.Label != "Test" {
		t.Errorf("expected Label 'Test', got %q", item.Label)
	}
	if item.Shortcut != "Ctrl+T" {
		t.Errorf("expected Shortcut 'Ctrl+T', got %q", item.Shortcut)
	}
	if item.Action != "test" {
		t.Errorf("expected Action 'test', got %q", item.Action)
	}
	if !item.Disabled {
		t.Error("expected Disabled to be true")
	}
}

func TestContextMenuActionMsg(t *testing.T) {
	msg := ContextMenuActionMsg{Action: "test_action"}
	if msg.Action != "test_action" {
		t.Errorf("expected Action 'test_action', got %q", msg.Action)
	}
}

func TestContextMenuSkipDisabledForward(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Items = []ContextMenuItem{
		{Label: "Cut", Disabled: true},
		{Label: "Copy", Disabled: true},
		{Label: "Paste", Disabled: false},
	}
	menu.Cursor = 0
	menu.skipDisabledForward()

	if menu.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", menu.Cursor)
	}
}

func TestContextMenuSkipDisabledForwardAllDisabled(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Items = []ContextMenuItem{
		{Label: "Cut", Disabled: true},
		{Label: "Copy", Disabled: true},
	}
	menu.Cursor = 0
	menu.skipDisabledForward()

	// Should wrap to 0
	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
}

func TestContextMenuSkipDisabledForwardEmpty(t *testing.T) {
	menu := NewContextMenu(ui.DefaultTheme())
	menu.Items = []ContextMenuItem{}
	menu.Cursor = 0
	menu.skipDisabledForward()

	if menu.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", menu.Cursor)
	}
}
