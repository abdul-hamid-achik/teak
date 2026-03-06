package overlay

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// mockOverlay is a minimal Overlay implementation for testing the Stack.
type mockOverlay struct {
	id        string
	dismissed bool
	captures  bool
	viewText  string
}

func (m *mockOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if kp, ok := msg.(tea.KeyPressMsg); ok && kp.String() == "esc" {
		m.dismissed = true
	}
	return m, nil
}
func (m *mockOverlay) View() string       { return m.viewText }
func (m *mockOverlay) IsDismissed() bool   { return m.dismissed }
func (m *mockOverlay) CapturesInput() bool { return m.captures }

func TestStackEmpty(t *testing.T) {
	var s Stack
	if !s.IsEmpty() {
		t.Error("new stack should be empty")
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
	if s.Top() != nil {
		t.Error("Top() on empty stack should return nil")
	}
	if s.Pop() != nil {
		t.Error("Pop() on empty stack should return nil")
	}
	if s.View() != "" {
		t.Errorf("View() on empty stack should be empty, got %q", s.View())
	}
	if s.CapturesInput() {
		t.Error("CapturesInput() on empty stack should be false")
	}
}

func TestStackPushPopLen(t *testing.T) {
	var s Stack
	a := &mockOverlay{id: "a", viewText: "view-a", captures: true}
	b := &mockOverlay{id: "b", viewText: "view-b", captures: false}

	s.Push(a)
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1", s.Len())
	}
	if s.IsEmpty() {
		t.Error("should not be empty after Push")
	}

	s.Push(b)
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2", s.Len())
	}

	// Top returns last pushed
	top := s.Top()
	if top.(*mockOverlay).id != "b" {
		t.Errorf("Top() id = %q, want %q", top.(*mockOverlay).id, "b")
	}

	// Pop returns last pushed
	popped := s.Pop()
	if popped.(*mockOverlay).id != "b" {
		t.Errorf("Pop() id = %q, want %q", popped.(*mockOverlay).id, "b")
	}
	if s.Len() != 1 {
		t.Errorf("after Pop: Len() = %d, want 1", s.Len())
	}

	// Pop again
	popped = s.Pop()
	if popped.(*mockOverlay).id != "a" {
		t.Errorf("Pop() id = %q, want %q", popped.(*mockOverlay).id, "a")
	}
	if !s.IsEmpty() {
		t.Error("should be empty after popping all")
	}
}

func TestStackClear(t *testing.T) {
	var s Stack
	s.Push(&mockOverlay{id: "a"})
	s.Push(&mockOverlay{id: "b"})
	s.Push(&mockOverlay{id: "c"})
	if s.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", s.Len())
	}

	s.Clear()
	if !s.IsEmpty() {
		t.Error("should be empty after Clear")
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
}

func TestStackViewReturnsTopmost(t *testing.T) {
	var s Stack
	s.Push(&mockOverlay{viewText: "bottom"})
	s.Push(&mockOverlay{viewText: "top"})

	if s.View() != "top" {
		t.Errorf("View() = %q, want %q", s.View(), "top")
	}
}

func TestStackCapturesInputFromTop(t *testing.T) {
	var s Stack
	s.Push(&mockOverlay{captures: false})
	if s.CapturesInput() {
		t.Error("should reflect top overlay's CapturesInput (false)")
	}

	s.Push(&mockOverlay{captures: true})
	if !s.CapturesInput() {
		t.Error("should reflect top overlay's CapturesInput (true)")
	}
}

func TestStackUpdateForwardsToTop(t *testing.T) {
	var s Stack
	bottom := &mockOverlay{id: "bottom"}
	top := &mockOverlay{id: "top"}
	s.Push(bottom)
	s.Push(top)

	// Send a non-esc key — should not dismiss
	s.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2 (no dismiss)", s.Len())
	}
}

func TestStackUpdateAutoPopsOnDismiss(t *testing.T) {
	var s Stack
	bottom := &mockOverlay{id: "bottom"}
	top := &mockOverlay{id: "top"}
	s.Push(bottom)
	s.Push(top)

	// Send esc to dismiss top
	s.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after dismiss", s.Len())
	}
	if s.Top().(*mockOverlay).id != "bottom" {
		t.Error("bottom overlay should now be on top")
	}
}

func TestStackUpdateOnEmpty(t *testing.T) {
	var s Stack
	cmd := s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("Update on empty stack should return nil cmd")
	}
}

func TestStackMultiplePushPop(t *testing.T) {
	var s Stack
	for i := 0; i < 10; i++ {
		s.Push(&mockOverlay{id: itoa(i)})
	}
	if s.Len() != 10 {
		t.Fatalf("Len() = %d, want 10", s.Len())
	}
	for i := 9; i >= 0; i-- {
		o := s.Pop()
		if o.(*mockOverlay).id != itoa(i) {
			t.Errorf("Pop() id = %q, want %q", o.(*mockOverlay).id, itoa(i))
		}
	}
}
