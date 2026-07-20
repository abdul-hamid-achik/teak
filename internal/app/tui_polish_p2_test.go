package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/search"
	"teak/internal/text"
)

func TestViewHidesNativeCursorForCapturingSurfaces(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Model)
	}{
		{"settings", func(m *Model) { m.showSettings = true }},
		{"branch picker", func(m *Model) { m.showBranchPicker = true }},
		{"go to line", func(m *Model) { m.goToLineMode = true }},
		{"save as", func(m *Model) { m.saveAsMode = true }},
		{"delete confirmation", func(m *Model) { m.deleteConfirm = true }},
		{"editor context menu", func(m *Model) {
			ed := m.editors[m.activeTab]
			ed, _ = ed.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: 1, Y: 1}))
			m.setEditor(m.activeTab, ed)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newViewTestModel(t, false)
			tt.apply(&m)
			if got := m.View().Cursor; got != nil {
				t.Fatalf("native cursor = %#v, want hidden while %s captures input", got, tt.name)
			}
		})
	}
}

func TestScrollIndicatorUsesWrappedVisualRows(t *testing.T) {
	m := newViewTestModel(t, false)
	ed := m.activeEditor()
	ed.Buffer = text.NewBufferFromBytes([]byte(strings.Repeat("abcdefghij", 12)))
	ed.Config.WordWrap = true
	ed.SetSize(20, 3)
	if ed.Wrap == nil {
		t.Fatal("word wrap layout was not initialized")
	}
	if rows := ed.Wrap.LineRows(0); rows <= ed.Viewport.Height {
		t.Fatalf("wrapped visual rows = %d, want more than viewport height %d", rows, ed.Viewport.Height)
	}
	ed.Viewport.WrapScrollY = 3
	ed.Viewport.ScrollY = 0 // Deliberately stale: wrapped scrolling owns status.

	totalRows := ed.Wrap.TotalRows()
	maxScroll := totalRows - ed.Viewport.Height
	want := fmt.Sprintf("%d%%", ed.Viewport.WrapScrollY*100/maxScroll)
	if got := m.scrollIndicator(); got != want {
		t.Fatalf("scrollIndicator() = %q, want %q from visual wrapped rows", got, want)
	}
}

func TestStatusBarCenterUsesUnicodeCellWidth(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width = 44
	m.status = "已保存 ✓"
	bar := m.renderStatusBar()
	lines := strings.Split(bar, "\n")
	if len(lines) != 2 {
		t.Fatalf("status rows = %d, want 2", len(lines))
	}
	if got := ansi.StringWidth(lines[1]); got != m.width {
		t.Fatalf("status width = %d, want %d terminal cells", got, m.width)
	}
}

func TestLipglossWidthUsesUnicodeDisplayCells(t *testing.T) {
	if got := lipglossWidth("保存 ✓"); got != ansi.StringWidth("保存 ✓") {
		t.Fatalf("lipglossWidth() = %d, want terminal-cell width %d", got, ansi.StringWidth("保存 ✓"))
	}
}

func TestEditorContextMenuClampsAndHitTestsSharedGeometry(t *testing.T) {
	m := newViewTestModel(t, true)
	m.width, m.height = 40, 8
	m.relayout()
	body := m.mouseLayout().editorBody

	openedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		Button: tea.MouseRight,
		X:      body.x + body.width - 1,
		Y:      body.y + body.height - 1,
	}))
	opened := openedAny.(Model)
	view, rect, ok := opened.editorContextMenuGeometry()
	if !ok {
		t.Fatal("editor context menu was not visible")
	}
	if !body.contains(rect.x, rect.y) || rect.x+rect.width > body.x+body.width || rect.y+rect.height > body.y+body.height {
		t.Fatalf("menu rect %+v escapes editor body %+v", rect, body)
	}
	if got := contextMenuRect(rect.x, rect.y, view); got != rect {
		t.Fatalf("render rect %+v and hit-test rect %+v disagree", got, rect)
	}

	// The first menu item is below the top border. Its click must be accepted
	// at the same clamped coordinates used for rendering.
	clickedAny, _ := opened.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: rect.x + 1, Y: rect.y + 1}))
	clicked := clickedAny.(Model)
	if clicked.activeEditor().IsContextMenuVisible() {
		t.Fatal("click inside clamped menu did not dismiss/select it")
	}
}

func TestCapturingContextMenuCancelsEditorDrag(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width, m.height = 80, 24
	m.relayout()
	body := m.mouseLayout().editorBody

	pressedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: body.x + 6, Y: body.y + 1}))
	pressed := pressedAny.(Model)
	if !pressed.activeEditor().IsDragging() {
		t.Fatal("editor drag did not start")
	}

	openedAny, _ := pressed.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: body.x + 6, Y: body.y + 1}))
	opened := openedAny.(Model)
	if opened.activeEditor().IsDragging() {
		t.Fatal("context menu left editor selection drag active")
	}
}

func TestOpeningModalCancelsEditorDrag(t *testing.T) {
	m := newViewTestModel(t, false)
	m.width, m.height = 80, 24
	m.relayout()
	body := m.mouseLayout().editorBody

	pressedAny, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: body.x + 6, Y: body.y + 1}))
	pressed := pressedAny.(Model)
	if !pressed.activeEditor().IsDragging() {
		t.Fatal("editor drag did not start")
	}

	openedAny, _ := pressed.openSearch(search.ModeText)
	opened := openedAny.(Model)
	if !opened.showSearch || opened.activeEditor().IsDragging() {
		t.Fatal("search modal did not take input ownership from selection drag")
	}
}
