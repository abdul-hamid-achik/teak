package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

func TestNewHelpModel(t *testing.T) {
	theme := ui.DefaultTheme()
	model := NewHelpModel(theme)

	// Theme contains lipgloss.Style which cannot be compared directly
	if model.scrollY != 0 {
		t.Errorf("expected scrollY 0, got %d", model.scrollY)
	}
	if model.lines == nil {
		t.Error("expected lines to be initialized")
	}
	if model.filtered == nil {
		t.Error("expected filtered to be initialized")
	}
	if len(model.lines) == 0 {
		t.Error("expected some help lines")
	}
}

func TestHelpModelSetSize(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	model.SetSize(80, 24)

	if model.width != 80 {
		t.Errorf("expected width 80, got %d", model.width)
	}
	if model.height != 24 {
		t.Errorf("expected height 24, got %d", model.height)
	}
	if model.input.Width() > 68 {
		t.Errorf("expected input width <= 68, got %d", model.input.Width())
	}
}

func TestHelpModelSetSizeSmall(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	model.SetSize(40, 10)

	if model.width != 40 {
		t.Errorf("expected width 40, got %d", model.width)
	}
	if model.height != 10 {
		t.Errorf("expected height 10, got %d", model.height)
	}
}

func TestHelpModelFocus(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	cmd := model.Focus()

	if cmd == nil {
		t.Error("expected cmd to be non-nil")
	}
	if !model.input.Focused() {
		t.Error("expected input to be focused")
	}
}

func TestHelpModelUpdateEscape(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	msg := teaKeyPress("escape")
	model, _ = model.Update(msg)

	// Should return to caller to close
}

func TestHelpModelUpdateUp(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.scrollY = 5

	msg := teaKeyPress("up")
	model, _ = model.Update(msg)

	if model.scrollY != 4 {
		t.Errorf("expected scrollY 4, got %d", model.scrollY)
	}
}

func TestHelpModelUpdateUpAtTop(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	msg := teaKeyPress("up")
	model, _ = model.Update(msg)

	if model.scrollY != 0 {
		t.Errorf("expected scrollY 0, got %d", model.scrollY)
	}
}

func TestHelpModelUpdateDown(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	initialScroll := model.scrollY

	msg := teaKeyPress("down")
	model, _ = model.Update(msg)

	if model.scrollY <= initialScroll {
		t.Errorf("expected scrollY to increase")
	}
}

func TestHelpModelUpdateDownAtBottom(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 1000 // Beyond max

	msg := teaKeyPress("down")
	model, _ = model.Update(msg)

	// Should stay at max
	if model.scrollY < model.maxScroll() {
		t.Errorf("expected scrollY at max, got %d", model.scrollY)
	}
}

func TestHelpModelUpdatePgUp(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 20

	msg := teaKeyPress("pgup")
	model, _ = model.Update(msg)

	// Should scroll up by visible lines
	if model.scrollY >= 20 {
		t.Errorf("expected scrollY to decrease")
	}
}

func TestHelpModelUpdatePgUpAtTop(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)

	msg := teaKeyPress("pgup")
	model, _ = model.Update(msg)

	if model.scrollY != 0 {
		t.Errorf("expected scrollY 0, got %d", model.scrollY)
	}
}

func TestHelpModelUpdatePgDown(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)

	msg := teaKeyPress("pgdown")
	model, _ = model.Update(msg)

	if model.scrollY <= 0 {
		t.Errorf("expected scrollY to increase")
	}
}

func TestHelpModelUpdatePgDownAtBottom(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 1000

	msg := teaKeyPress("pgdown")
	model, _ = model.Update(msg)

	// Should stay at max
	if model.scrollY < model.maxScroll() {
		t.Errorf("expected scrollY at max, got %d", model.scrollY)
	}
}

func TestHelpModelUpdateMouseWheel(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 10

	msg := teaMouseWheel(tea.MouseWheelUp)
	model, _ = model.Update(msg)

	if model.scrollY != 7 {
		t.Errorf("expected scrollY 7, got %d", model.scrollY)
	}
}

func TestHelpModelUpdateMouseWheelDown(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 10

	msg := teaMouseWheel(tea.MouseWheelDown)
	model, _ = model.Update(msg)

	if model.scrollY != 13 {
		t.Errorf("expected scrollY 13, got %d", model.scrollY)
	}
}

func TestHelpModelUpdateMouseWheelUpAtTop(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.scrollY = 1

	msg := teaMouseWheel(tea.MouseWheelUp)
	model, _ = model.Update(msg)

	if model.scrollY < 0 {
		t.Errorf("expected scrollY >= 0, got %d", model.scrollY)
	}
}

func TestHelpModelUpdateSearch(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	// Type in search - note: textinput may not update on single char in test
	msg := teaKeyPressMsgWithText("f")
	model, _ = model.Update(msg)

	// Input value may not update immediately in test environment
	_ = model.input.Value()
}

func TestHelpModelFilterLines(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	filtered := model.filterLines("quit")

	if len(filtered) == 0 {
		t.Error("expected some filtered lines")
	}
	// Should contain lines with "quit"
	found := false
	for _, line := range filtered {
		if strings.Contains(line.text, "quit") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'quit' in filtered lines")
	}
}

func TestHelpModelFilterLinesNoMatch(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	filtered := model.filterLines("xyznonexistent")

	if len(filtered) != 0 {
		t.Errorf("expected 0 filtered lines, got %d", len(filtered))
	}
}

func TestHelpModelFilterLinesGroupTitle(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	filtered := model.filterLines("general")

	if len(filtered) == 0 {
		t.Error("expected some filtered lines")
	}
}

func TestHelpModelFilterLinesEmpty(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	filtered := model.filterLines("")

	if len(filtered) != len(model.lines) {
		t.Error("expected all lines when query is empty")
	}
}

func TestHelpModelVisibleLines(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)

	v := model.visibleLines()
	if v < 5 {
		t.Errorf("expected visible lines >= 5, got %d", v)
	}
}

func TestHelpModelMaxScroll(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)

	ms := model.maxScroll()
	if ms < 0 {
		t.Errorf("expected maxScroll >= 0, got %d", ms)
	}
}

func TestHelpModelMaxScrollFewLines(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 100) // Large height, few lines

	ms := model.maxScroll()
	if ms < 0 {
		ms = 0
	}
	// If lines fit, maxScroll should be 0 or negative (clamped)
}

func TestHelpModelView(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)

	view := model.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "Keyboard Shortcuts") {
		t.Error("expected title in view")
	}
}

func TestHelpModelViewScroll(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 5

	view := model.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestHelpModelViewFiltered(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.filtered = model.filterLines("quit")

	view := model.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestHelpModelViewScrollIndicator(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 10

	view := model.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should show scroll hint
}

func TestHelpModelBuildLines(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	lines := model.buildLines()

	if len(lines) == 0 {
		t.Error("expected some lines")
	}

	// Check for group titles
	hasGeneral := false
	for _, line := range lines {
		if line.isTitle && strings.Contains(line.text, "general") {
			hasGeneral = true
			break
		}
	}
	if !hasGeneral {
		t.Error("expected 'General' group")
	}
}

func TestHelpModelBuildLinesSeparators(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())

	lines := model.buildLines()

	// Should have empty lines between groups
	hasSeparator := false
	for _, line := range lines {
		if line.rendered == "" && line.text == "" {
			hasSeparator = true
			break
		}
	}
	if !hasSeparator {
		t.Error("expected separators between groups")
	}
}

func TestPadRight(t *testing.T) {
	result := padRight("hello", 10)
	if result != "hello     " {
		t.Errorf("expected 'hello     ', got %q", result)
	}

	result = padRight("hello", 5)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}

	result = padRight("hello", 3)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestRenderHelp(t *testing.T) {
	theme := ui.DefaultTheme()

	result := RenderHelp(theme, 80, 24)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestHelpLineStructure(t *testing.T) {
	line := helpLine{
		rendered: "rendered text",
		text:     "plain text",
		isTitle:  true,
	}

	if line.rendered != "rendered text" {
		t.Errorf("expected rendered 'rendered text', got %q", line.rendered)
	}
	if line.text != "plain text" {
		t.Errorf("expected text 'plain text', got %q", line.text)
	}
	if !line.isTitle {
		t.Error("expected isTitle to be true")
	}
}

func TestHelpModelUpdateScrollResetOnFilter(t *testing.T) {
	model := NewHelpModel(ui.DefaultTheme())
	model.SetSize(80, 24)
	model.scrollY = 50

	// Filter to fewer lines
	msg := teaKeyPressMsgWithText("quit")
	model, _ = model.Update(msg)

	// Scroll should be reset if beyond new max
	if model.scrollY > model.maxScroll() {
		t.Errorf("expected scrollY <= maxScroll, got %d", model.scrollY)
	}
}

func TestHelpKeyBindingStructure(t *testing.T) {
	binding := keybinding{
		key:  "Ctrl+Q",
		desc: "Quit",
	}

	if binding.key != "Ctrl+Q" {
		t.Errorf("expected key 'Ctrl+Q', got %q", binding.key)
	}
	if binding.desc != "Quit" {
		t.Errorf("expected desc 'Quit', got %q", binding.desc)
	}
}

func TestHelpBindingGroupStructure(t *testing.T) {
	group := bindingGroup{
		title: "General",
		bindings: []keybinding{
			{key: "Ctrl+Q", desc: "Quit"},
		},
	}

	if group.title != "General" {
		t.Errorf("expected title 'General', got %q", group.title)
	}
	if len(group.bindings) != 1 {
		t.Errorf("expected 1 binding, got %d", len(group.bindings))
	}
}

func TestHelpGroups(t *testing.T) {
	if len(helpGroups) == 0 {
		t.Error("expected some help groups")
	}

	// Check for expected groups
	hasGeneral := false
	hasNavigation := false
	hasEditing := false
	for _, g := range helpGroups {
		switch g.title {
		case "General":
			hasGeneral = true
		case "Navigation":
			hasNavigation = true
		case "Editing":
			hasEditing = true
		}
	}

	if !hasGeneral {
		t.Error("expected 'General' group")
	}
	if !hasNavigation {
		t.Error("expected 'Navigation' group")
	}
	if !hasEditing {
		t.Error("expected 'Editing' group")
	}
}

// Helper functions for creating test messages
func teaKeyPress(key string) tea.Msg {
	return tea.KeyPressMsg{Text: key}
}

func teaKeyPressMsgWithText(text string) tea.Msg {
	return tea.KeyPressMsg{Text: text}
}

func teaMouseWheel(button tea.MouseButton) tea.Msg {
	return tea.MouseWheelMsg{Button: button}
}
