package app

import "testing"

func TestSplitLayoutPaneWidths(t *testing.T) {
	s := splitLayout{enabled: true, ratio: 0.5, secondTab: 1}

	aw := s.paneAWidth(80)
	bw := s.paneBWidth(80)
	if aw+bw+1 != 80 {
		t.Errorf("pane widths %d + %d + 1 divider != 80", aw, bw)
	}
}

func TestSplitLayoutMinimumWidth(t *testing.T) {
	s := splitLayout{enabled: true, ratio: 0.1, secondTab: 1}

	aw := s.paneAWidth(80)
	if aw < 10 {
		t.Errorf("pane A width %d below minimum 10", aw)
	}
	bw := s.paneBWidth(80)
	if bw < 10 {
		t.Errorf("pane B width %d below minimum 10", bw)
	}
}

func TestSplitLayoutDisabled(t *testing.T) {
	s := defaultSplitLayout()
	if s.enabled {
		t.Error("default split should be disabled")
	}
	if s.paneAWidth(80) != 80 {
		t.Error("disabled split should return full width for pane A")
	}
}

func TestSplitLayoutFocusedTab(t *testing.T) {
	s := splitLayout{enabled: true, ratio: 0.5, secondTab: 3, focused: 0}
	if s.focusedTab(1) != 1 {
		t.Error("focused=0 should return activeTab")
	}
	s.focused = 1
	if s.focusedTab(1) != 3 {
		t.Error("focused=1 should return secondTab")
	}
}

func TestSplitLayoutVertical(t *testing.T) {
	s := splitLayout{enabled: true, vertical: true, ratio: 0.5, secondTab: 1}

	ah := s.paneAHeight(40)
	bh := s.paneBHeight(40)
	if ah+bh+1 != 40 {
		t.Errorf("pane heights %d + %d + 1 divider != 40", ah, bh)
	}
	// Width should be full when vertical
	if s.paneAWidth(80) != 80 {
		t.Error("vertical split should return full width")
	}
}
