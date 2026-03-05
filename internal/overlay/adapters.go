package overlay

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/git"
	"teak/internal/search"
)

// SearchOverlay wraps search.Model to implement the Overlay interface.
// Close is detected by intercepting Escape; result messages (OpenResultMsg,
// ReplaceOneMsg, etc.) flow through as normal tea.Cmd returns.
type SearchOverlay struct {
	Model     search.Model
	dismissed bool
}

func NewSearchOverlay(m search.Model) *SearchOverlay {
	return &SearchOverlay{Model: m}
}

func (s *SearchOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		key := kp.String()
		if key == "esc" || key == "escape" {
			s.dismissed = true
			return s, nil
		}
	}
	updated, cmd := s.Model.Update(msg)
	s.Model = updated
	return s, cmd
}

func (s *SearchOverlay) View() string       { return s.Model.View() }
func (s *SearchOverlay) IsDismissed() bool   { return s.dismissed }
func (s *SearchOverlay) CapturesInput() bool { return true }

// HelpOverlay wraps editor.HelpModel to implement the Overlay interface.
type HelpOverlay struct {
	Model     editor.HelpModel
	dismissed bool
}

func NewHelpOverlay(m editor.HelpModel) *HelpOverlay {
	return &HelpOverlay{Model: m}
}

func (h *HelpOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		key := kp.String()
		if key == "esc" || key == "escape" || key == "f1" {
			h.dismissed = true
			return h, nil
		}
	}
	updated, cmd := h.Model.Update(msg)
	h.Model = updated
	return h, cmd
}

func (h *HelpOverlay) View() string       { return h.Model.View() }
func (h *HelpOverlay) IsDismissed() bool   { return h.dismissed }
func (h *HelpOverlay) CapturesInput() bool { return true }

// BranchPickerOverlay wraps git.BranchPickerModel to implement the Overlay interface.
type BranchPickerOverlay struct {
	Model     git.BranchPickerModel
	dismissed bool
}

func NewBranchPickerOverlay(m git.BranchPickerModel) *BranchPickerOverlay {
	return &BranchPickerOverlay{Model: m}
}

func (b *BranchPickerOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if kp, ok := msg.(tea.KeyPressMsg); ok {
		key := kp.String()
		if key == "esc" || key == "escape" {
			b.dismissed = true
			return b, nil
		}
	}
	updated, cmd := b.Model.Update(msg)
	b.Model = updated
	return b, cmd
}

func (b *BranchPickerOverlay) View() string       { return b.Model.View() }
func (b *BranchPickerOverlay) IsDismissed() bool   { return b.dismissed }
func (b *BranchPickerOverlay) CapturesInput() bool { return true }
