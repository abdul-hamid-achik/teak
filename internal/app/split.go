package app

// splitLayout manages a two-pane editor split.
type splitLayout struct {
	enabled   bool
	vertical  bool // false = side-by-side (|), true = stacked (-)
	ratio     float64
	firstTab  int // editors[] index shown in pane A; -1 = none
	secondTab int // editors[] index shown in pane B; -1 = none
	focused   int // 0 = pane A, 1 = pane B
}

// paneTab returns the editor index displayed in the given pane, or -1.
func (s splitLayout) paneTab(pane int) int {
	if pane == 1 {
		return s.secondTab
	}
	return s.firstTab
}

func defaultSplitLayout() splitLayout {
	return splitLayout{
		ratio:     0.5,
		firstTab:  -1,
		secondTab: -1,
	}
}

// splitWidth computes the width of pane A given the total editor width.
func (s splitLayout) paneAWidth(totalWidth int) int {
	if !s.enabled || s.vertical {
		return totalWidth
	}
	w := int(float64(totalWidth) * s.ratio)
	if w < 10 {
		w = 10
	}
	if totalWidth-w < 10 {
		w = totalWidth - 10
	}
	if w < 1 {
		w = 1
	}
	return w
}

// paneBWidth computes the width of pane B.
func (s splitLayout) paneBWidth(totalWidth int) int {
	if !s.enabled || s.vertical {
		return totalWidth
	}
	a := s.paneAWidth(totalWidth)
	b := totalWidth - a - 1 // -1 for divider
	if b < 1 {
		b = 1
	}
	return b
}

// paneAHeight computes the height of pane A given the total editor height.
func (s splitLayout) paneAHeight(totalHeight int) int {
	if !s.enabled || !s.vertical {
		return totalHeight
	}
	h := int(float64(totalHeight) * s.ratio)
	if h < 3 {
		h = 3
	}
	if totalHeight-h < 3 {
		h = totalHeight - 3
	}
	if h < 1 {
		h = 1
	}
	return h
}

// paneBHeight computes the height of pane B.
func (s splitLayout) paneBHeight(totalHeight int) int {
	if !s.enabled || !s.vertical {
		return totalHeight
	}
	a := s.paneAHeight(totalHeight)
	b := totalHeight - a - 1 // -1 for divider
	if b < 1 {
		b = 1
	}
	return b
}

// focusedTab returns the editors[] index of the focused pane.
func (s splitLayout) focusedTab(activeTab int) int {
	if !s.enabled || s.focused == 0 {
		return activeTab
	}
	if s.secondTab >= 0 {
		return s.secondTab
	}
	return activeTab
}

// toggleSplit enables or disables the split view. When enabling, the current
// active tab is shown in pane A and the next tab (if any) in pane B.
func (m *Model) toggleSplit() {
	if m.split.enabled {
		m.unsplit()
		return
	}
	if len(m.editors) < 2 {
		m.status = "Need at least 2 open tabs to split"
		return
	}
	m.split.enabled = true
	m.split.focused = 0
	// Pane A keeps the current tab and pane B shows the next one. Pane A's tab
	// is recorded explicitly: deriving it from activeTab made both panes render
	// the same buffer as soon as focus moved to pane B.
	m.split.firstTab = m.activeTab
	m.split.secondTab = (m.activeTab + 1) % len(m.editors)
	m.relayout()
	m.status = "Split view (F6 to switch panes, Ctrl+Shift+\\ to close)"
}

// focusSplitPaneAt moves split focus to whichever pane contains the point. It
// is a no-op when the split is off or the point is not over a pane.
func (m *Model) focusSplitPaneAt(x, y int) {
	if !m.split.enabled {
		return
	}
	pane := 0
	if m.mouseLayout().inPaneB(x, y) {
		pane = 1
	}
	if pane == m.split.focused {
		return
	}
	m.split.focused = pane
	if tab := m.split.paneTab(pane); tab >= 0 && tab < len(m.editors) {
		m.activateTab(tab)
	}
}

// unsplit disables the split view.
func (m *Model) unsplit() {
	if !m.split.enabled {
		return
	}
	m.split.enabled = false
	m.split.firstTab = -1
	m.split.secondTab = -1
	m.split.focused = 0
	m.relayout()
}

// reconcileSplitAfterClose repairs split pane references once the editor at
// closedIdx has been removed from editors. Pane tabs are editors[] indices,
// so indices above the closed tab shift down with the slice. A pane that
// showed the closed tab (or that would duplicate the other pane) collapses
// the split exactly like unsplit instead of silently reassigning contents.
func (m *Model) reconcileSplitAfterClose(closedIdx int) {
	if !m.split.enabled {
		return
	}
	adjust := func(tab int) int {
		switch {
		case tab < 0:
			return tab
		case tab == closedIdx:
			return -1
		case tab > closedIdx:
			return tab - 1
		default:
			return tab
		}
	}
	first := adjust(m.split.firstTab)
	second := adjust(m.split.secondTab)
	if first < 0 || second < 0 || first == second ||
		first >= len(m.editors) || second >= len(m.editors) {
		m.unsplit()
		return
	}
	m.split.firstTab = first
	m.split.secondTab = second
}

// cycleSplitFocus switches focus between pane A and pane B.
func (m *Model) cycleSplitFocus() {
	if !m.split.enabled {
		return
	}
	// activeTab follows the focused pane in both directions. Only moving it
	// towards pane B left pane A pointing at pane B's tab on the way back.
	if m.split.focused == 0 {
		m.split.focused = 1
	} else {
		m.split.focused = 0
	}
	if tab := m.split.paneTab(m.split.focused); tab >= 0 && tab < len(m.editors) {
		m.activateTab(tab)
	}
}
