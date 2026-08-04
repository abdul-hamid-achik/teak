package overlay

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

func TestInputSubmitsTextAndMapsResult(t *testing.T) {
	input := NewInput("Branch name", "feature/", ui.DefaultTheme())
	var got InputResult
	input.SetResultAction(func(result InputResult) tea.Msg {
		got = result
		return result
	})
	if cmd := input.Focus(); cmd == nil {
		t.Fatal("Focus() should return a blink command")
	}
	o, cmd := input.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("submit should emit a result command")
	}
	if result, ok := cmd().(InputResult); !ok || result.Value != "feature/" || !result.Accepted {
		t.Fatalf("submit command result = %#v, want accepted feature/", cmd())
	}
	if got.Value != "feature/" || !got.Accepted {
		t.Fatalf("mapped result = %#v, want accepted feature/", got)
	}
	if !o.(*Input).IsDismissed() {
		t.Fatal("input should be dismissed after submit")
	}
}

func TestInputEscapeCancelsWithoutReturningText(t *testing.T) {
	input := NewInput("Branch name", "feature/", ui.DefaultTheme())
	input.SetResultAction(func(result InputResult) tea.Msg { return result })
	o, cmd := input.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("cancel should emit a result command")
	}
	result, ok := cmd().(InputResult)
	if !ok || result.Value != "" || result.Accepted {
		t.Fatalf("cancel result = %#v, want empty rejected result", result)
	}
	if !o.(*Input).IsDismissed() {
		t.Fatal("input should be dismissed after Escape")
	}
}

func TestInputViewAndCancelAction(t *testing.T) {
	input := NewInput("Prompt", "initial", ui.DefaultTheme())
	input.SetWidth(32)
	input.SetResultAction(func(result InputResult) tea.Msg { return result })
	if got := input.Value(); got != "initial" {
		t.Fatalf("Value() = %q, want initial", got)
	}
	if got := input.CancelAction().(InputResult); got.Value != "" || got.Accepted {
		t.Fatalf("CancelAction() = %#v, want rejected empty result", got)
	}
	if view := input.View(); view == "" {
		t.Fatal("View() returned empty prompt")
	}
	if !input.CapturesInput() {
		t.Fatal("input should capture input")
	}
}
