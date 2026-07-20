package app

import (
	"runtime/debug"
	"testing"
	"unsafe"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
)

// The Bubble Tea program stores the root model in a tea.Model interface after
// every Update. Keep the public value small: otherwise each return m copies
// the entire UI graph while boxing the interface.
func TestModelIsSmallValueHandle(t *testing.T) {
	const maxModelHandleBytes = 64
	if size := unsafe.Sizeof(Model{}); size > maxModelHandleBytes {
		t.Fatalf("Model is %d bytes; want at most %d-byte handle", size, maxModelHandleBytes)
	}
}

// testModel builds an isolated root-state owner for tests that previously used
// a Model struct literal. Production models are constructed by NewModel.
func testModel(state modelState) Model {
	return Model{modelState: &state}
}

func TestModelStateOwnersAreIndependent(t *testing.T) {
	first := testModel(modelState{})
	second := testModel(modelState{})

	updated, _ := first.Update(tea.WindowSizeMsg{Width: 91, Height: 27})
	got := updated.(Model)
	if got.modelState == second.modelState {
		t.Fatal("independent models unexpectedly share modelState")
	}
	if second.width != 0 || second.height != 0 {
		t.Fatalf("second model was mutated: %dx%d", second.width, second.height)
	}
}

func TestZeroModelUpdateInitializesAnOwnedState(t *testing.T) {
	updated, _ := (Model{}).Update(modelHandleNoopMsg{})
	m, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want app.Model", updated)
	}
	if m.modelState == nil {
		t.Fatal("zero Model Update returned a nil state handle")
	}
}

func TestZeroModelCanRenderAfterWindowSizeUpdate(t *testing.T) {
	updated, _ := (Model{}).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want app.Model", updated)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("View() panicked after zero-model initialization: %v\n%s", recovered, debug.Stack())
		}
	}()
	_ = m.View()
}

func BenchmarkModelUpdateNoop(b *testing.B) {
	m := testModel(modelState{})
	msg := modelHandleNoopMsg{}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
}

func BenchmarkModelUpdateCursorInput(b *testing.B) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel("", "", cfg)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(m.cleanup)
	m.welcome = nil
	m.focus = FocusEditor
	msg := tea.KeyPressMsg{Code: tea.KeyRight}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
}

type modelHandleNoopMsg struct{}
