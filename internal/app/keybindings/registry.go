package keybindings

import (
	"sort"
	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
	"teak/internal/app/modes"
)

// Registry manages all keyboard shortcuts
type Registry struct {
	bindings []*Binding
	lookup   map[string][]*Binding // key -> bindings
}

// NewRegistry creates a new keybinding registry
func NewRegistry() *Registry {
	return &Registry{
		bindings: make([]*Binding, 0),
		lookup:   make(map[string][]*Binding),
	}
}

// Bind registers a new keyboard shortcut
func (r *Registry) Bind(keys []string, handler HandlerFunc, desc string, opts ...Option) {
	b := &Binding{
		Keys:        keys,
		Handler:     handler,
		Description: desc,
	}

	for _, opt := range opts {
		opt(b)
	}

	r.bindings = append(r.bindings, b)

	// Index by key for fast lookup
	for _, key := range keys {
		r.lookup[key] = append(r.lookup[key], b)
	}
}

// Option configures a Binding
type Option func(*Binding)

// InContext specifies which focus areas this binding applies to
func InContext(ctxs ...app.FocusArea) Option {
	return func(b *Binding) {
		b.Contexts = ctxs
	}
}

// InModes specifies which input modes this binding applies to
func InModes(modes ...modes.ModeID) Option {
	return func(b *Binding) {
		b.Modes = modes
	}
}

// WithPriority sets the priority (higher = checked first)
func WithPriority(p int) Option {
	return func(b *Binding) {
		b.Priority = p
	}
}

// Handle processes a message and returns a command if a binding matched
func (r *Registry) Handle(msg tea.Msg, model *app.Model, focus app.FocusArea, mode modes.ModeID) tea.Cmd {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	keyStr := keyMsg.String()
	candidates := r.lookup[keyStr]

	if len(candidates) == 0 {
		return nil
	}

	// Sort by priority (descending)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority > candidates[j].Priority
	})

	// Find first matching binding
	for _, b := range candidates {
		if b.AppliesToContext(focus) && b.AppliesToMode(mode) {
			return b.Handler(model)
		}
	}

	return nil
}

// GetHelp returns all bindings for help display
func (r *Registry) GetHelp() []Binding {
	result := make([]Binding, len(r.bindings))
	for i, b := range r.bindings {
		result[i] = *b
	}
	return result
}
