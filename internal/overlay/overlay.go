package overlay

import tea "charm.land/bubbletea/v2"

// Overlay is the interface for modal overlays that capture input and render
// on top of the editor. Implementations include Confirm, Picker, etc.
type Overlay interface {
	// Update handles a Bubbletea message and returns commands.
	Update(msg tea.Msg) (Overlay, tea.Cmd)

	// View renders the overlay content (without positioning — the caller
	// composites it via ui.RenderOverlay).
	View() string

	// IsDismissed returns true when the overlay has finished and should be
	// removed from the stack.
	IsDismissed() bool

	// CapturesInput returns true when the overlay should consume all
	// keyboard and mouse input, preventing it from reaching layers below.
	CapturesInput() bool
}

// Stack is an ordered collection of overlays rendered bottom-to-top.
// The topmost overlay receives input first.
type Stack struct {
	layers []Overlay
}

// Push adds an overlay to the top of the stack.
func (s *Stack) Push(o Overlay) {
	s.layers = append(s.layers, o)
}

// Pop removes and returns the topmost overlay. Returns nil if empty.
func (s *Stack) Pop() Overlay {
	n := len(s.layers)
	if n == 0 {
		return nil
	}
	top := s.layers[n-1]
	s.layers = s.layers[:n-1]
	return top
}

// Top returns the topmost overlay without removing it. Returns nil if empty.
func (s *Stack) Top() Overlay {
	if len(s.layers) == 0 {
		return nil
	}
	return s.layers[len(s.layers)-1]
}

// IsEmpty returns true when the stack has no overlays.
func (s *Stack) IsEmpty() bool {
	return len(s.layers) == 0
}

// Len returns the number of overlays on the stack.
func (s *Stack) Len() int {
	return len(s.layers)
}

// Update forwards a message to the topmost overlay. If that overlay is
// dismissed after the update, it is automatically popped.
func (s *Stack) Update(msg tea.Msg) tea.Cmd {
	if len(s.layers) == 0 {
		return nil
	}
	n := len(s.layers)
	updated, cmd := s.layers[n-1].Update(msg)
	if updated.IsDismissed() {
		s.layers = s.layers[:n-1]
	} else {
		s.layers[n-1] = updated
	}
	return cmd
}

// View renders all overlays from bottom to top. The caller is responsible
// for compositing this over the base editor content.
func (s *Stack) View() string {
	if len(s.layers) == 0 {
		return ""
	}
	// Return only the topmost overlay's view — compositing multiple
	// overlays is left to the caller if needed.
	return s.layers[len(s.layers)-1].View()
}

// CapturesInput returns true if the topmost overlay captures input.
func (s *Stack) CapturesInput() bool {
	if len(s.layers) == 0 {
		return false
	}
	return s.layers[len(s.layers)-1].CapturesInput()
}

// Clear removes all overlays from the stack.
func (s *Stack) Clear() {
	s.layers = nil
}
