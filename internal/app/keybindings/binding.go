package keybindings

import (
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
	"teak/internal/app/modes"
)

// HandlerFunc is a function that handles a keybinding
type HandlerFunc func(m *app.Model) tea.Cmd

// Binding represents a single keyboard shortcut
type Binding struct {
	// Keys is the list of key sequences that trigger this binding
	Keys []string

	// Handler is called when the binding is triggered
	Handler HandlerFunc

	// Description is shown in help/cheatsheet
	Description string

	// Contexts specifies which focus areas this binding applies to
	// Empty means all contexts
	Contexts []app.FocusArea

	// Modes specifies which input modes this binding applies to
	// Empty means only ModeNormal
	Modes []modes.ModeID

	// Priority: higher numbers are checked first
	Priority int
}

// Matches returns true if the message matches this binding
func (b *Binding) Matches(msg tea.Msg, focus app.FocusArea, mode modes.ModeID) bool {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}

	keyStr := keyMsg.String()
	for _, k := range b.Keys {
		if k == keyStr {
			return true
		}
	}
	return false
}

// AppliesToContext returns true if this binding applies to the given focus area
func (b *Binding) AppliesToContext(focus app.FocusArea) bool {
	if len(b.Contexts) == 0 {
		return true // applies to all
	}
	for _, ctx := range b.Contexts {
		if ctx == focus {
			return true
		}
	}
	return false
}

// AppliesToMode returns true if this binding applies to the given mode
func (b *Binding) AppliesToMode(mode modes.ModeID) bool {
	if len(b.Modes) == 0 {
		return mode == modes.ModeNormal // default: only normal mode
	}
	for _, m := range b.Modes {
		if m == mode {
			return true
		}
	}
	return false
}
