package overlay

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFloatDismissesWithID(t *testing.T) {
	float := NewFloat(7, "Preview", "hello", 40, 5)
	o, cmd := float.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("Escape should emit FloatCloseMsg")
	}
	msg, ok := cmd().(FloatCloseMsg)
	if !ok || msg.ID != 7 {
		t.Fatalf("close message = %#v, want FloatCloseMsg{ID: 7}", msg)
	}
	if !o.(*Float).IsDismissed() {
		t.Fatal("float should be dismissed")
	}
}

func TestFloatViewAndInputCapture(t *testing.T) {
	float := NewFloat(3, "Preview", "hello\nworld", 40, 5)
	if view := float.View(); view == "" {
		t.Fatal("View() returned empty float")
	}
	if !float.CapturesInput() {
		t.Fatal("float should capture input")
	}
}
