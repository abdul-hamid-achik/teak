package editor

import (
	"testing"

	"teak/internal/text"
	"teak/internal/ui"
)

func menuActionLabels(items []ContextMenuItem) map[string]string {
	labels := make(map[string]string)
	for _, item := range items {
		if item.Action != "" {
			labels[item.Action] = item.Label
		}
	}
	return labels
}

// The context menu must expose the same affordances the keyboard already has:
// in-buffer find and the go-to-line dialog are available without an LSP.
func TestEditorMenuItemsIncludeFindAndGoToLine(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())

	labels := menuActionLabels(ed.buildEditorMenuItems())
	if label, ok := labels["find"]; !ok || label != "Find" {
		t.Fatalf("find item = %q (present: %t), want label %q", label, ok, "Find")
	}
	if label, ok := labels["go_to_line"]; !ok || label != "Go to Line" {
		t.Fatalf("go_to_line item = %q (present: %t), want label %q", label, ok, "Go to Line")
	}
	if _, ok := labels["format_document"]; ok {
		t.Fatal("format_document present without LSP; it must be gated on HasLSP")
	}
}

// Format Document is gated on HasLSP like the rest of the LSP trio and names
// the same chord the keyboard binding uses.
func TestEditorMenuItemsWithLSPIncludeFormatDocument(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.HasLSP = true

	var formatItem *ContextMenuItem
	items := ed.buildEditorMenuItems()
	for i, item := range items {
		if item.Action == "format_document" {
			formatItem = &items[i]
			break
		}
	}
	if formatItem == nil {
		t.Fatal("format_document missing from LSP context menu")
	}
	if formatItem.Label != "Format Document" {
		t.Fatalf("format item label = %q, want %q", formatItem.Label, "Format Document")
	}
	if formatItem.Shortcut != "Ctrl+Alt+F" {
		t.Fatalf("format item shortcut = %q, want %q", formatItem.Shortcut, "Ctrl+Alt+F")
	}
}

// The three app-routed menu actions must fall through to ContextMenuActionMsg
// so the app layer can open its dialogs, exactly like goto_definition.
func TestEditorMenuAppRoutedActionsDispatchToApp(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())

	for _, action := range []string{"find", "go_to_line", "format_document"} {
		_, cmd := ed.dispatchContextMenuAction(action)
		if cmd == nil {
			t.Fatalf("action %q returned nil command; want ContextMenuActionMsg", action)
		}
		msg := cmd()
		routed, ok := msg.(ContextMenuActionMsg)
		if !ok || routed.Action != action {
			t.Fatalf("action %q dispatched %#v; want ContextMenuActionMsg{%s}", action, msg, action)
		}
	}
}
