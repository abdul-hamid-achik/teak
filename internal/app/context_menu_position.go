package app

import "teak/internal/editor"

// editorContextMenuScreenPosition is shared by render and hit testing. Compact
// height hides the sidebar, so its horizontal offset must disappear from both
// paths at the same threshold.
func (m Model) editorContextMenuScreenPosition() (int, int) {
	if m.activeEditor() == nil {
		return 0, 0
	}
	x, y := m.activeEditor().ContextMenuPosition()
	body := m.activeEditorBodyRect()
	return body.x + x, body.y + y
}

// editorContextMenuGeometry is the single geometry source for both rendering
// and mouse hit-testing. It first clips an oversized popup to the editor body
// (tiny PTYs cannot fit a full menu), then clamps its anchor so neither its
// visible cells nor its clickable rectangle can cover the tab bar/status bar.
func (m Model) editorContextMenuGeometry() (string, mouseRect, bool) {
	ed := m.activeEditor()
	if ed == nil || !ed.IsContextMenuVisible() {
		return "", mouseRect{}, false
	}
	body := m.activeEditorBodyRect()
	if body.width < 1 || body.height < 1 {
		return "", mouseRect{}, false
	}
	view := clipViewRows(clipViewLines(ed.ContextMenuView(), body.width), body.height)
	if view == "" {
		return "", mouseRect{}, false
	}
	menuRect := contextMenuRect(0, 0, view)
	rawX, rawY := m.editorContextMenuScreenPosition()
	x := min(max(rawX, body.x), body.x+max(0, body.width-menuRect.width))
	y := min(max(rawY, body.y), body.y+max(0, body.height-menuRect.height))
	return view, contextMenuRect(x, y, view), true
}

// clampActiveEditorContextMenu stores the shared geometry back in editor-local
// coordinates immediately after opening. That keeps public position queries,
// a resize-free render, and the following click on exactly the same cells.
func (m Model) clampActiveEditorContextMenu() {
	ed := m.activeEditor()
	if ed == nil || !ed.IsContextMenuVisible() {
		return
	}
	_, rect, ok := m.editorContextMenuGeometry()
	if !ok {
		return
	}
	body := m.activeEditorBodyRect()
	ed.SetContextMenuPosition(rect.x-body.x, rect.y-body.y)
}

func (m Model) cancelActiveEditorDrag() {
	if ed := m.activeEditor(); ed != nil {
		ed.CancelDrag()
	}
}

// showContextMenu places a menu entirely within the interactive content area.
// Rendering already clips overlays, but clamping first keeps hit-testing aligned
// with what the user can actually see and avoids invisible actions below the
// status bar on a short terminal.
func (m Model) showContextMenu(menu *editor.ContextMenu, items []editor.ContextMenuItem, x, y int) {
	m.cancelActiveEditorDrag()
	menu.Show(items, x, y)
	rect := contextMenuRect(0, 0, menu.View())
	maxX := max(0, m.width-rect.width)
	maxY := max(0, m.height-2-rect.height)
	menu.X = min(max(0, x), maxX)
	menu.Y = min(max(0, y), maxY)
}
