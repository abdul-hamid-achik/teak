package editor

import (
	"testing"

	"teak/internal/ui"
)

func TestSignatureHelpShow(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	help := &SignatureData{
		Signatures: []SignatureInfo{
			{
				Label:         "func Foo(a int, b string)",
				Documentation: "Foo does something",
				Parameters: []ParameterInfo{
					{Label: "a int", Documentation: "First param"},
					{Label: "b string", Documentation: "Second param"},
				},
			},
		},
		ActiveSignature: 0,
		ActiveParameter: 0,
	}

	sh.Show(help)

	if !sh.Visible {
		t.Error("SignatureHelp should be visible after Show()")
	}
	if sh.Help == nil {
		t.Fatal("SignatureHelp.Help should not be nil")
	}
	if len(sh.Help.Signatures) != 1 {
		t.Errorf("expected 1 signature, got %d", len(sh.Help.Signatures))
	}
}

func TestSignatureHelpHide(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	sh.Show(&SignatureData{
		Signatures: []SignatureInfo{{Label: "func Foo()"}},
	})
	sh.Hide()

	if sh.Visible {
		t.Error("SignatureHelp should not be visible after Hide()")
	}
	if sh.Help != nil {
		t.Error("SignatureHelp.Help should be nil after Hide()")
	}
}

func TestSignatureHelpView(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	// View should be empty when not visible
	if view := sh.View(); view != "" {
		t.Errorf("View() should be empty when not visible, got %q", view)
	}

	// View should render when visible
	sh.Show(&SignatureData{
		Signatures: []SignatureInfo{
			{Label: "func Foo(a int)"},
		},
	})

	view := sh.View()
	if view == "" {
		t.Error("View() should not be empty when visible")
	}
}

func TestSignatureHelpViewLongLabel(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	// Create a very long signature
	longLabel := "func VeryLongFunctionNameWithManyParameters(param1 int, param2 string, param3 bool, param4 float64)"
	sh.Show(&SignatureData{
		Signatures: []SignatureInfo{{Label: longLabel}},
	})

	view := sh.View()
	if view == "" {
		t.Error("View() should render even with long labels")
	}
}

func TestSignatureHelpViewMultiLine(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	sh.Show(&SignatureData{
		Signatures: []SignatureInfo{
			{
				Label:         "func Foo()",
				Documentation: "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
			},
		},
	})

	view := sh.View()
	if view == "" {
		t.Error("View() should render multi-line documentation")
	}
}

func TestSignatureHelpUpdateActiveParameter(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	sh.Show(&SignatureData{
		Signatures:      []SignatureInfo{{Label: "func Foo(a, b, c)"}},
		ActiveParameter: 0,
	})

	sh.UpdateActiveParameter(1)
	if sh.Help.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %d, want 1", sh.Help.ActiveParameter)
	}

	sh.UpdateActiveParameter(2)
	if sh.Help.ActiveParameter != 2 {
		t.Errorf("ActiveParameter = %d, want 2", sh.Help.ActiveParameter)
	}
}

func TestSignatureHelpHideIdempotent(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	// Hide when already hidden should not panic
	sh.Hide()
	sh.Hide()

	// Show and hide multiple times
	sh.Show(&SignatureData{Signatures: []SignatureInfo{{Label: "func Foo()"}}})
	sh.Hide()
	sh.Hide()
}

func TestSignatureHelpShowNil(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	// Show with nil should not panic
	sh.Show(nil)

	if sh.Visible {
		t.Error("SignatureHelp should not be visible when showing nil")
	}
}

func TestSignatureHelpShowEmptySignatures(t *testing.T) {
	theme := ui.DefaultTheme()
	sh := NewSignatureHelp(theme)

	// Show with empty signatures should not be visible
	sh.Show(&SignatureData{Signatures: []SignatureInfo{}})

	if sh.Visible {
		t.Error("SignatureHelp should not be visible with empty signatures")
	}
}
