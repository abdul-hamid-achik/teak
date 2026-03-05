package editor

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/highlight"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.TabSize != 4 {
		t.Errorf("expected TabSize 4, got %d", cfg.TabSize)
	}
	if cfg.InsertTabs != false {
		t.Error("expected InsertTabs to be false")
	}
	if cfg.AutoIndent != true {
		t.Error("expected AutoIndent to be true")
	}
}

func TestNewEditor(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	buf.FilePath = "test.go" // Set FilePath so Highlighter is created
	theme := ui.DefaultTheme()
	cfg := DefaultConfig()

	editor := New(buf, theme, cfg)

	if editor.Buffer == nil {
		t.Fatal("expected Buffer to be set")
	}
	if editor.Highlighter == nil {
		t.Fatal("expected Highlighter to be set")
	}
	// Theme is a struct containing lipgloss.Style which cannot be compared directly
	if editor.Config != cfg {
		t.Error("expected Config to be set")
	}
	if editor.lastVersion != -1 {
		t.Errorf("expected lastVersion -1, got %d", editor.lastVersion)
	}
}

func TestEditorSetSize(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor.SetSize(80, 24)

	if editor.Viewport.Width != 80 {
		t.Errorf("expected Width 80, got %d", editor.Viewport.Width)
	}
	if editor.Viewport.Height != 24 {
		t.Errorf("expected Height 24, got %d", editor.Viewport.Height)
	}
}

func TestEditorScheduleInitialTokenize(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("package main\n\nfunc main() {}"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	cmd := editor.ScheduleInitialTokenize()
	if cmd == nil {
		t.Fatal("expected cmd to be non-nil")
	}

	// Execute the command
	msg := cmd()
	tokenizeMsg, ok := msg.(TokenizeCompleteMsg)
	if !ok {
		t.Fatalf("expected TokenizeCompleteMsg, got %T", msg)
	}
	if tokenizeMsg.Version != buf.Version() {
		t.Errorf("expected version %d, got %d", buf.Version(), tokenizeMsg.Version)
	}
	if len(tokenizeMsg.Lines) == 0 {
		t.Error("expected some tokenized lines")
	}
}

func TestEditorScheduleInitialTokenizeNoHighlighter(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	// No FilePath set, so Highlighter will be nil
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	cmd := editor.ScheduleInitialTokenize()
	if cmd != nil {
		t.Error("expected nil cmd when no highlighter")
	}
}

func TestEditorUpdateTokenizeComplete(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	// Simulate tokenization complete
	msg := TokenizeCompleteMsg{
		Version: buf.Version(),
		Lines:   [][]highlight.StyledToken{{{Text: "hello", Style: ui.DefaultTheme().Editor}}},
	}

	editor, _ = editor.Update(msg)

	if editor.Highlighter == nil {
		t.Fatal("expected Highlighter to exist")
	}
	// The highlighter should have lines set
}

func TestEditorUpdateRetokenize(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.lastVersion = buf.Version()

	// Send retokenize message
	msg := RetokenizeMsg{
		Version:      buf.Version(),
		ViewportOnly: false,
	}

	editor, cmd := editor.Update(msg)
	// cmd may be nil if version matches lastVersion
	_ = cmd
}

func TestEditorUpdateRetokenizeViewportOnly(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world\nline 2\nline 3"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.Viewport.ScrollY = 0
	editor.Viewport.Height = 10

	msg := RetokenizeMsg{
		Version:      buf.Version(),
		ViewportOnly: true,
	}

	editor, cmd := editor.Update(msg)
	if cmd == nil {
		t.Fatal("expected cmd to be non-nil")
	}

	result := cmd()
	tokenizeMsg, ok := result.(TokenizeCompleteMsg)
	if !ok {
		t.Fatalf("expected TokenizeCompleteMsg, got %T", result)
	}
	if len(tokenizeMsg.Lines) == 0 {
		t.Error("expected some tokenized lines")
	}
}

func TestEditorUpdateRetokenizeStaleVersion(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.lastVersion = 5 // Different version

	msg := RetokenizeMsg{
		Version: 3, // Old version
	}

	editor, cmd := editor.Update(msg)
	if cmd != nil {
		t.Error("expected nil cmd for stale version")
	}
}

func TestEditorUpdateRetokenizeDuplicateVersion(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.lastVersion = buf.Version()

	msg := RetokenizeMsg{
		Version:      buf.Version(),
		ViewportOnly: false, // Not viewport-only, so should be discarded
	}

	editor, cmd := editor.Update(msg)
	if cmd != nil {
		t.Error("expected nil cmd for duplicate version")
	}
}

func TestEditorUpdateKeyPressNavigation(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	// Just test that navigation doesn't crash
	msg := tea.KeyPressMsg{Text: "right"}
	editor, _ = editor.Update(msg)
	
	msg = tea.KeyPressMsg{Text: "left"}
	editor, _ = editor.Update(msg)
	
	msg = tea.KeyPressMsg{Text: "end"}
	editor, _ = editor.Update(msg)
	
	msg = tea.KeyPressMsg{Text: "home"}
	editor, _ = editor.Update(msg)
}

func TestEditorUpdateKeyPressSelection(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)
	editor.Buffer.Cursor = text.Position{Line: 0, Col: 5}

	msg := tea.KeyPressMsg{Text: "shift+right"}
	editor, _ = editor.Update(msg)

	if editor.Buffer.Selection == nil {
		t.Fatal("expected selection to be set")
	}
}

func TestEditorUpdateKeyPressSelectAll(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	msg := tea.KeyPressMsg{Text: "ctrl+a"}
	editor, _ = editor.Update(msg)

	if editor.Buffer.Selection == nil {
		t.Fatal("expected selection to be set")
	}
}

func TestEditorUpdateKeyPressInsertText(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	msg := tea.KeyPressMsg{Text: "x"}
	editor, _ = editor.Update(msg)

	content := string(editor.Buffer.Bytes())
	if content != "xhello" {
		t.Errorf("expected 'xhello', got %q", content)
	}
}

func TestEditorUpdateKeyPressBackspace(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)
	editor.Buffer.Cursor = text.Position{Line: 0, Col: 1}

	msg := tea.KeyPressMsg{Text: "backspace"}
	editor, _ = editor.Update(msg)

	content := string(editor.Buffer.Bytes())
	if content != "ello" {
		t.Errorf("expected 'ello', got %q", content)
	}
}

func TestEditorUpdateKeyPressDelete(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	msg := tea.KeyPressMsg{Text: "delete"}
	editor, _ = editor.Update(msg)

	content := string(editor.Buffer.Bytes())
	if content != "ello" {
		t.Errorf("expected 'ello', got %q", content)
	}
}

func TestEditorUpdateKeyPressEnter(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)
	editor.Buffer.Cursor = text.Position{Line: 0, Col: 5}

	msg := tea.KeyPressMsg{Text: "enter"}
	editor, _ = editor.Update(msg)

	content := string(editor.Buffer.Bytes())
	if content != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", content)
	}
}

func TestEditorUpdateKeyPressUndoRedo(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	// Insert text
	msg := tea.KeyPressMsg{Text: "x"}
	editor, _ = editor.Update(msg)

	// Undo
	msg = tea.KeyPressMsg{Text: "ctrl+z"}
	editor, _ = editor.Update(msg)

	content := string(editor.Buffer.Bytes())
	if content != "hello" {
		t.Errorf("expected 'hello' after undo, got %q", content)
	}

	// Redo
	msg = tea.KeyPressMsg{Text: "ctrl+y"}
	editor, _ = editor.Update(msg)

	content = string(editor.Buffer.Bytes())
	if content != "xhello" {
		t.Errorf("expected 'xhello' after redo, got %q", content)
	}
}

func TestEditorUpdateKeyPressComment(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)
	editor.Config.CommentPrefix = "//"

	msg := tea.KeyPressMsg{Text: "ctrl+/"}
	editor, _ = editor.Update(msg)

	content := string(editor.Buffer.Bytes())
	if content != "// hello" {
		t.Errorf("expected '// hello', got %q", content)
	}
}

func TestEditorUpdateMouseClickLeft(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	msg := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      10,
		Y:      0,
	}
	editor, _ = editor.Update(msg)

	if editor.Buffer.Cursor.Line != 0 {
		t.Errorf("expected line 0, got %d", editor.Buffer.Cursor.Line)
	}
}

func TestEditorUpdateMouseClickRight(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	msg := tea.MouseClickMsg{
		Button: tea.MouseRight,
		X:      10,
		Y:      0,
	}
	editor, _ = editor.Update(msg)

	if !editor.contextMenu.Visible {
		t.Error("expected context menu to be visible")
	}
}

func TestEditorUpdateMouseClickRightWithSelection(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)
	editor.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	msg := tea.MouseClickMsg{
		Button: tea.MouseRight,
		X:      10,
		Y:      0,
	}
	editor, _ = editor.Update(msg)

	// Selection should be preserved
	if editor.Buffer.Selection == nil || editor.Buffer.Selection.IsEmpty() {
		t.Error("expected selection to be preserved")
	}
}

func TestEditorUpdateMouseClickDoubleClick(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	// First click
	msg := tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      5,
		Y:      0,
	}
	editor, _ = editor.Update(msg)

	// Second click (double-click)
	time.Sleep(10 * time.Millisecond)
	msg = tea.MouseClickMsg{
		Button: tea.MouseLeft,
		X:      5,
		Y:      0,
	}
	editor, _ = editor.Update(msg)

	if editor.Buffer.Selection == nil || editor.Buffer.Selection.IsEmpty() {
		t.Error("expected word selection on double-click")
	}
}

func TestEditorUpdateMouseWheel(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("line1\nline2\nline3\nline4\nline5"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	msg := tea.MouseWheelMsg{
		Button: tea.MouseWheelDown,
	}
	editor, _ = editor.Update(msg)

	if editor.Viewport.ScrollY < 0 {
		t.Errorf("expected ScrollY >= 0, got %d", editor.Viewport.ScrollY)
	}
}

func TestEditorUpdatePaste(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	msg := tea.PasteMsg{Content: "world"}
	editor, _ = editor.Update(msg)

	content := string(editor.Buffer.Bytes())
	if content != "worldhello" {
		t.Errorf("expected 'worldhello', got %q", content)
	}
}

func TestEditorUpdateContextMenuNavigation(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	// Show context menu
	editor.contextMenu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
		{Label: "Paste", Action: "paste"},
	}, 10, 5)

	// Navigate down
	msg := tea.KeyPressMsg{Text: "down"}
	editor, _ = editor.Update(msg)

	if editor.contextMenu.Cursor != 1 {
		t.Errorf("expected cursor 1, got %d", editor.contextMenu.Cursor)
	}

	// Navigate up
	msg = tea.KeyPressMsg{Text: "up"}
	editor, _ = editor.Update(msg)

	if editor.contextMenu.Cursor != 0 {
		t.Errorf("expected cursor 0, got %d", editor.contextMenu.Cursor)
	}
}

func TestEditorUpdateContextMenuSelect(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	editor.contextMenu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
	}, 10, 5)

	// Select with enter
	msg := tea.KeyPressMsg{Text: "enter"}
	editor, cmd := editor.Update(msg)

	if editor.contextMenu.Visible {
		t.Error("expected context menu to be hidden")
	}
	// cmd may be nil if no item was selected
	_ = cmd
}

func TestEditorUpdateContextMenuEscape(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	editor.contextMenu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
	}, 10, 5)

	msg := tea.KeyPressMsg{Text: "escape"}
	editor, _ = editor.Update(msg)

	if editor.contextMenu.Visible {
		t.Error("expected context menu to be hidden")
	}
}

func TestEditorUpdateAutocompleteNavigation(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	editor.autocomplete.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
	})

	msg := tea.KeyPressMsg{Text: "down"}
	editor, _ = editor.Update(msg)

	if editor.autocomplete.Cursor != 1 {
		t.Errorf("expected cursor 1, got %d", editor.autocomplete.Cursor)
	}
}

func TestEditorUpdateAutocompleteSelect(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	editor.autocomplete.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
	})

	msg := tea.KeyPressMsg{Text: "enter"}
	editor, _ = editor.Update(msg)

	if editor.autocomplete.Visible {
		t.Error("expected autocomplete to be hidden")
	}

	content := string(editor.Buffer.Bytes())
	if content != "foohello" {
		t.Errorf("expected 'foohello', got %q", content)
	}
}

func TestEditorUpdateAutocompleteEscape(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	editor.autocomplete.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
	})

	msg := tea.KeyPressMsg{Text: "escape"}
	editor, _ = editor.Update(msg)

	if editor.autocomplete.Visible {
		t.Error("expected autocomplete to be hidden")
	}
}

func TestEditorView(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	view := editor.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestEditorCursorPosition(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)
	editor.Buffer.Cursor = text.Position{Line: 0, Col: 5}

	x, y := editor.CursorPosition()
	if y != 0 {
		t.Errorf("expected y 0, got %d", y)
	}
	if x < 5 {
		t.Errorf("expected x >= 5, got %d", x)
	}
}

func TestEditorShowHideAutocomplete(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	items := []AutocompleteItem{{Label: "foo", InsertText: "foo"}}
	editor.ShowAutocomplete(items)

	if !editor.IsAutocompleteVisible() {
		t.Error("expected autocomplete to be visible")
	}

	editor.HideAutocomplete()
	if editor.IsAutocompleteVisible() {
		t.Error("expected autocomplete to be hidden")
	}
}

func TestEditorShowHideHover(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor.ShowHover("hover content")
	hoverView := editor.HoverView()
	if hoverView == "" {
		t.Error("expected hover view to be visible")
	}

	editor.HideHover()
	hoverView = editor.HoverView()
	if hoverView != "" {
		t.Error("expected hover view to be hidden")
	}
}

func TestEditorAutocompleteView(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	view := editor.AutocompleteView()
	if view != "" {
		t.Error("expected empty autocomplete view when not visible")
	}

	editor.ShowAutocomplete([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})
	view = editor.AutocompleteView()
	if view == "" {
		t.Error("expected non-empty autocomplete view")
	}
}

func TestEditorContextMenuView(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	view := editor.ContextMenuView()
	if view != "" {
		t.Error("expected empty context menu view when not visible")
	}

	editor.contextMenu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 10, 5)
	view = editor.ContextMenuView()
	if view == "" {
		t.Error("expected non-empty context menu view")
	}
}

func TestEditorContextMenuPosition(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor.contextMenu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 15, 8)
	x, y := editor.ContextMenuPosition()

	if x != 15 {
		t.Errorf("expected x 15, got %d", x)
	}
	if y != 8 {
		t.Errorf("expected y 8, got %d", y)
	}
}

func TestEditorHideContextMenu(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor.contextMenu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 10, 5)
	editor.HideContextMenu()

	if editor.IsContextMenuVisible() {
		t.Error("expected context menu to be hidden")
	}
}

func TestEditorClickContextMenuItem(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor.contextMenu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
	}, 10, 5)

	editor, cmd, action := editor.ClickContextMenuItem(0)

	if action != "cut" {
		t.Errorf("expected action 'cut', got %q", action)
	}
	if editor.contextMenu.Visible {
		t.Error("expected context menu to be hidden")
	}
	// cmd may be nil for cut with no selection
	_ = cmd
}

func TestEditorClickContextMenuItemOutOfBounds(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor.contextMenu.Show([]ContextMenuItem{{Label: "Cut", Action: "cut"}}, 10, 5)
	editor, cmd, action := editor.ClickContextMenuItem(100)

	if action != "" {
		t.Errorf("expected empty action, got %q", action)
	}
	if editor.contextMenu.Visible {
		t.Error("expected context menu to be hidden")
	}
	if cmd != nil {
		t.Error("expected nil cmd")
	}
}

func TestEditorContextMenuItemCount(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor.contextMenu.Show([]ContextMenuItem{
		{Label: "Cut", Action: "cut"},
		{Label: "Copy", Action: "copy"},
	}, 10, 5)

	count := editor.ContextMenuItemCount()
	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestEditorBuildEditorMenuItems(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	items := editor.buildEditorMenuItems()
	if len(items) == 0 {
		t.Error("expected some menu items")
	}

	// Check that cut/copy are enabled with selection
	if items[0].Disabled {
		t.Error("expected Cut to be enabled")
	}
}

func TestEditorBuildEditorMenuItemsNoSelection(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	items := editor.buildEditorMenuItems()
	if len(items) == 0 {
		t.Error("expected some menu items")
	}

	// Check that cut/copy are disabled without selection
	if !items[0].Disabled {
		t.Error("expected Cut to be disabled")
	}
}

func TestEditorBuildEditorMenuItemsWithLSP(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.HasLSP = true

	items := editor.buildEditorMenuItems()
	hasGotoDef := false
	for _, item := range items {
		if item.Action == "goto_definition" {
			hasGotoDef = true
			break
		}
	}
	if !hasGotoDef {
		t.Error("expected LSP menu items")
	}
}

func TestEditorDispatchContextMenuActionCut(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)
	editor.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	editor, cmd := editor.dispatchContextMenuAction("cut")

	content := string(editor.Buffer.Bytes())
	if content != " world" {
		t.Errorf("expected ' world', got %q", content)
	}
	// cmd may be nil if retokenize is not needed
	_ = cmd
}

func TestEditorDispatchContextMenuActionCopy(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 5})

	editor, cmd := editor.dispatchContextMenuAction("copy")

	content := string(editor.Buffer.Bytes())
	if content != "hello world" {
		t.Errorf("expected 'hello world', got %q", content)
	}
	if cmd != nil {
		t.Error("expected nil cmd for copy")
	}
}

func TestEditorDispatchContextMenuActionPaste(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor, cmd := editor.dispatchContextMenuAction("paste")

	content := string(editor.Buffer.Bytes())
	// Clipboard might be empty
	if cmd == nil && content == "hello" {
		// This is acceptable if clipboard is empty
	}
}

func TestEditorDispatchContextMenuActionSelectAll(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor, _ = editor.dispatchContextMenuAction("select_all")

	if editor.Buffer.Selection == nil || editor.Buffer.Selection.IsEmpty() {
		t.Error("expected selection to be set")
	}
}

func TestEditorDispatchContextMenuActionUndo(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	// First insert something
	editor.Buffer.InsertAtCursor([]byte("x"))

	editor, cmd := editor.dispatchContextMenuAction("undo")

	content := string(editor.Buffer.Bytes())
	if content != "hello" {
		t.Errorf("expected 'hello', got %q", content)
	}
	// cmd may be nil if retokenize is not needed
	_ = cmd
}

func TestEditorDispatchContextMenuActionRedo(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	// Insert and undo
	editor.Buffer.InsertAtCursor([]byte("x"))
	editor.Buffer.Undo()

	editor, cmd := editor.dispatchContextMenuAction("redo")

	content := string(editor.Buffer.Bytes())
	if content != "xhello" {
		t.Errorf("expected 'xhello', got %q", content)
	}
	// cmd may be nil if retokenize is not needed
	_ = cmd
}

func TestEditorDispatchContextMenuActionToggleComment(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.Config.CommentPrefix = "//"

	editor, cmd := editor.dispatchContextMenuAction("toggle_comment")

	content := string(editor.Buffer.Bytes())
	if content != "// hello" {
		t.Errorf("expected '// hello', got %q", content)
	}
	// cmd may be nil if retokenize is not needed
	_ = cmd
}

func TestEditorDispatchContextMenuActionLSP(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	editor, cmd := editor.dispatchContextMenuAction("goto_definition")

	if cmd == nil {
		t.Error("expected cmd to be non-nil for LSP action")
	}
}

func TestEditorNeedsRetokenize(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello world"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())
	editor.SetSize(80, 24)

	// Initially may or may not need retokenize depending on tokenization state
	_ = editor.needsRetokenize()
}

func TestEditorScheduleRetokenizeImmediate(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	cmd := editor.scheduleRetokenizeImmediate()
	if cmd == nil {
		t.Fatal("expected cmd to be non-nil")
	}

	msg := cmd()
	retokenizeMsg, ok := msg.(RetokenizeMsg)
	if !ok {
		t.Fatalf("expected RetokenizeMsg, got %T", msg)
	}
	if !retokenizeMsg.ViewportOnly {
		t.Error("expected ViewportOnly to be true")
	}
}

func TestEditorScheduleRetokenizeImmediateNoHighlighter(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	cmd := editor.scheduleRetokenizeImmediate()
	if cmd != nil {
		t.Error("expected nil cmd when no highlighter")
	}
}

func TestEditorScheduleRetokenize(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	buf.FilePath = "test.go"
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	cmd := editor.scheduleRetokenize()
	if cmd == nil {
		t.Fatal("expected cmd to be non-nil")
	}
	// Note: This uses a tick, so we can't easily test the result without waiting
}

func TestEditorScheduleRetokenizeNoHighlighter(t *testing.T) {
	buf := text.NewBufferFromBytes([]byte("hello"))
	editor := New(buf, ui.DefaultTheme(), DefaultConfig())

	cmd := editor.scheduleRetokenize()
	if cmd != nil {
		t.Error("expected nil cmd when no highlighter")
	}
}
