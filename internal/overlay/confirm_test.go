package overlay

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

func init() {
	zone.NewGlobal()
}

func testButtons() []Button {
	return []Button{
		{Label: "Save", Action: ButtonAction{Label: "save"}},
		{Label: "Discard", Action: ButtonAction{Label: "discard"}},
		{Label: "Cancel", Action: ButtonAction{Label: "cancel"}},
	}
}

func newTestConfirm() *Confirm {
	return NewConfirm(
		"Unsaved Changes",
		"You have unsaved changes.",
		[]string{"file1.go", "file2.go"},
		testButtons(),
		ui.DefaultTheme(),
	)
}

func TestConfirmInitialState(t *testing.T) {
	c := newTestConfirm()
	if c.IsDismissed() {
		t.Error("new confirm should not be dismissed")
	}
	if !c.CapturesInput() {
		t.Error("confirm should capture input")
	}
	if c.Result() != nil {
		t.Error("result should be nil initially")
	}
}

func TestConfirmDismissOnEscape(t *testing.T) {
	c := newTestConfirm()
	o, cmd := c.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	c = o.(*Confirm)
	if !c.IsDismissed() {
		t.Error("should be dismissed after escape")
	}
	if cmd != nil {
		t.Error("escape should not emit a command")
	}
	if c.Result() != nil {
		t.Error("result should be nil after escape dismiss")
	}
}

func TestConfirmNavigateButtons(t *testing.T) {
	tests := []struct {
		name       string
		keys       []tea.KeyPressMsg
		wantCursor int
	}{
		{
			name:       "initial cursor at 0",
			keys:       nil,
			wantCursor: 0,
		},
		{
			name: "right moves cursor",
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyRight},
			},
			wantCursor: 1,
		},
		{
			name: "right twice",
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyRight},
				{Code: tea.KeyRight},
			},
			wantCursor: 2,
		},
		{
			name: "right clamps at end",
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyRight},
				{Code: tea.KeyRight},
				{Code: tea.KeyRight},
				{Code: tea.KeyRight},
			},
			wantCursor: 2,
		},
		{
			name: "left from 0 stays at 0",
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyLeft},
			},
			wantCursor: 0,
		},
		{
			name: "right then left",
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyRight},
				{Code: tea.KeyRight},
				{Code: tea.KeyLeft},
			},
			wantCursor: 1,
		},
		{
			name: "tab moves right",
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyTab},
			},
			wantCursor: 1,
		},
		{
			name: "shift+tab moves left",
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyTab},
				{Code: tea.KeyTab, Mod: tea.ModShift},
			},
			wantCursor: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestConfirm()
			var o Overlay = c
			for _, k := range tt.keys {
				o, _ = o.Update(k)
			}
			got := o.(*Confirm).cursor
			if got != tt.wantCursor {
				t.Errorf("cursor = %d, want %d", got, tt.wantCursor)
			}
		})
	}
}

func TestConfirmSelectButton(t *testing.T) {
	tests := []struct {
		name      string
		navKeys   []tea.KeyPressMsg
		wantLabel string
	}{
		{
			name:      "select first button",
			navKeys:   nil,
			wantLabel: "save",
		},
		{
			name:      "select second button",
			navKeys:   []tea.KeyPressMsg{{Code: tea.KeyRight}},
			wantLabel: "discard",
		},
		{
			name: "select third button",
			navKeys: []tea.KeyPressMsg{
				{Code: tea.KeyRight},
				{Code: tea.KeyRight},
			},
			wantLabel: "cancel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestConfirm()
			var o Overlay = c
			for _, k := range tt.navKeys {
				o, _ = o.Update(k)
			}
			o, cmd := o.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			c = o.(*Confirm)

			if !c.IsDismissed() {
				t.Error("should be dismissed after enter")
			}
			if cmd == nil {
				t.Fatal("should emit a command")
			}
			msg := cmd()
			action, ok := msg.(ButtonAction)
			if !ok {
				t.Fatalf("expected ButtonAction, got %T", msg)
			}
			if action.Label != tt.wantLabel {
				t.Errorf("action label = %q, want %q", action.Label, tt.wantLabel)
			}
			result, ok := c.Result().(ButtonAction)
			if !ok {
				t.Fatalf("Result() should be ButtonAction, got %T", c.Result())
			}
			if result.Label != tt.wantLabel {
				t.Errorf("Result().Label = %q, want %q", result.Label, tt.wantLabel)
			}
		})
	}
}

func TestConfirmEnterWithNoButtons(t *testing.T) {
	c := NewConfirm("Title", "Msg", nil, nil, ui.DefaultTheme())
	o, cmd := c.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if o.(*Confirm).IsDismissed() {
		t.Error("enter with no buttons should not dismiss")
	}
	if cmd != nil {
		t.Error("enter with no buttons should not emit command")
	}
}

func TestConfirmView(t *testing.T) {
	c := newTestConfirm()
	v := c.View()
	if v == "" {
		t.Error("View() should not be empty")
	}
	// Should contain title and button labels
	for _, want := range []string{"Unsaved Changes", "Save", "Discard", "Cancel"} {
		if !containsStr(v, want) {
			t.Errorf("View() should contain %q", want)
		}
	}
	// Should contain items
	for _, item := range []string{"file1.go", "file2.go"} {
		if !containsStr(v, item) {
			t.Errorf("View() should contain item %q", item)
		}
	}
}

func TestConfirmViewNoMessage(t *testing.T) {
	c := NewConfirm("Title", "", nil, testButtons(), ui.DefaultTheme())
	v := c.View()
	if !containsStr(v, "Title") {
		t.Error("View() should contain the title")
	}
}

func TestConfirmSetWidth(t *testing.T) {
	c := newTestConfirm()
	c.SetWidth(80)
	if c.width != 80 {
		t.Errorf("width = %d, want 80", c.width)
	}
}

func TestConfirmUnhandledMessage(t *testing.T) {
	c := newTestConfirm()
	o, cmd := c.Update(tea.FocusMsg{})
	if o.(*Confirm).IsDismissed() {
		t.Error("unhandled message should not dismiss")
	}
	if cmd != nil {
		t.Error("unhandled message should return nil cmd")
	}
}
