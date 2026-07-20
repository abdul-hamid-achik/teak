package app

// splitLayout manages a two-pane editor split.
type splitLayout struct {
	enabled   bool
	vertical  bool // false = side-by-side (|), true = stacked (-)
	ratio     float64
	secondTab int // editors[] index shown in pane B; -1 = none
	focused   int // 0 = pane A (activeTab), 1 = pane B
}

func defaultSplitLayout() splitLayout {
	return splitLayout{
		ratio:     0.5,
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
	// Pane B shows the next tab
	next := (m.activeTab + 1) % len(m.editors)
	m.split.secondTab = next
	m.relayout()
	m.status = "Split view (F6 to switch panes, Ctrl+Shift+\\ to close)"
}

// unsplit disables the split view.
func (m *Model) unsplit() {
	if !m.split.enabled {
		return
	}
	m.split.enabled = false
	m.split.secondTab = -1
	m.split.focused = 0
	m.relayout()
}

// cycleSplitFocus switches focus between pane A and pane B.
func (m *Model) cycleSplitFocus() {
	if !m.split.enabled {
		return
	}
	if m.split.focused == 0 {
		m.split.focused = 1
		if m.split.secondTab >= 0 && m.split.secondTab < len(m.editors) {
			m.activeTab = m.split.secondTab
		}
	} else {
		m.split.focused = 0
	}
	m.tabBar.SetActive(m.activeTab)
}
