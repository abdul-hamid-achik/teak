package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/overlay"
)

func newInputRoutingTestModel(t *testing.T) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(model.cleanup)
	// Tests exercise input routing rather than the welcome screen's initial
	// tree focus.
	model.welcome = nil
	model.showTree = false
	model.focus = FocusEditor
	return model
}

func updateInputRoutingModel(t *testing.T, model Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(msg)
	// The root Bubble Tea model is currently a value Model by contract.
	result, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() model type = %T, want app.Model", updated)
	}
	return result
}

func TestInputRoutingContextMenuCapturesEditorKey(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.treeContextMenu.Show([]editor.ContextMenuItem{
		{Label: "Open", Action: "open"},
	}, 1, 1)

	updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if updated.treeContextMenu.Visible {
		t.Fatal("ordinary key did not dismiss the context menu")
	}
	if got := updated.activeEditor().Buffer.Content(); got != "" {
		t.Fatalf("editor content after context-menu key = %q, want unchanged", got)
	}
}

func TestInputRoutingModalCapturesEditorKey(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.unsavedConfirm = overlay.NewConfirm("Unsaved", "Keep this dialog open", nil, nil, model.theme)

	updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := updated.activeEditor().Buffer.Content(); got != "" {
		t.Fatalf("editor content after modal key = %q, want unchanged", got)
	}
	if updated.unsavedConfirm == nil {
		t.Fatal("modal was dismissed by a non-modal key")
	}
}

func TestInputRoutingGlobalShortcutDoesNotReachEditor(t *testing.T) {
	model := newInputRoutingTestModel(t)
	initialShowTree := model.showTree

	updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if updated.showTree == initialShowTree {
		t.Fatal("ctrl+b did not run its global action")
	}
	if got := updated.activeEditor().Buffer.Content(); got != "" {
		t.Fatalf("editor content after ctrl+b = %q, want unchanged", got)
	}
}

func TestCommandPaletteQuitWithDirtyBufferShowsConfirmation(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer.InsertAtCursor([]byte("unsaved"))
	model.tabBar.Tabs[model.activeTab].Dirty = true

	updatedAny, cmd := model.Update(commandPaletteMsg{inner: quitMsg{}})
	updated := updatedAny.(Model)

	if updated.unsavedConfirm == nil {
		t.Fatal("command palette quit did not show the unsaved changes confirmation")
	}
	if cmd != nil {
		t.Fatal("command palette quit returned a command before the user confirmed")
	}
}

func TestGlobalAndPaletteQuitUseSameUnsavedConfirmation(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer.InsertAtCursor([]byte("unsaved"))
	model.tabBar.Tabs[model.activeTab].Dirty = true

	globalAny, globalCmd, handled := model.handleGlobalKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	global := globalAny.(Model)
	if !handled {
		t.Fatal("ctrl+q was not handled")
	}
	if globalCmd != nil {
		t.Fatal("ctrl+q returned a command before the user confirmed")
	}

	paletteAny, paletteCmd := model.Update(commandPaletteMsg{inner: quitMsg{}})
	palette := paletteAny.(Model)
	if paletteCmd != nil {
		t.Fatal("command palette quit returned a command before the user confirmed")
	}

	if global.unsavedConfirm == nil || palette.unsavedConfirm == nil {
		t.Fatal("one quit entry point did not show the unsaved changes confirmation")
	}
	if global.unsavedConfirm.Title != palette.unsavedConfirm.Title ||
		global.unsavedConfirm.Message != palette.unsavedConfirm.Message ||
		len(global.unsavedConfirm.Items) != len(palette.unsavedConfirm.Items) ||
		len(global.unsavedConfirm.Buttons) != len(palette.unsavedConfirm.Buttons) {
		t.Fatal("quit entry points created different unsaved changes confirmations")
	}
	for i := range global.unsavedConfirm.Items {
		if global.unsavedConfirm.Items[i] != palette.unsavedConfirm.Items[i] {
			t.Fatal("quit entry points listed different dirty files")
		}
	}
	for i := range global.unsavedConfirm.Buttons {
		if global.unsavedConfirm.Buttons[i].Label != palette.unsavedConfirm.Buttons[i].Label {
			t.Fatal("quit entry points offered different actions")
		}
	}
}

func TestInputRoutingGlobalHelpActionReturnsFocusCommand(t *testing.T) {
	model := newInputRoutingTestModel(t)

	updatedAny, cmd, handled := model.handleGlobalKey(tea.KeyPressMsg{Code: tea.KeyF1})
	updated := updatedAny.(Model)
	if !handled {
		t.Fatal("f1 was not handled as a global key")
	}
	if !updated.showHelp {
		t.Fatal("f1 did not open the help overlay")
	}
	if cmd == nil {
		t.Fatal("f1 did not return the help focus command")
	}
}

func TestInputRoutingFoldAllAndUnfoldAllUseActualKeyStrings(t *testing.T) {
	model := newInputRoutingTestModel(t)
	ed := model.activeEditor()
	ed.Folds.Regions = []editor.FoldRegion{{StartLine: 0, EndLine: 0}, {StartLine: 1, EndLine: 1}}
	model.setEditor(model.activeTab, *ed)

	foldedAny, _, handled := model.handleGlobalKey(tea.KeyPressMsg{Code: '0', Mod: tea.ModCtrl | tea.ModShift})
	if !handled {
		t.Fatal("Ctrl+Shift+0 was not handled")
	}
	folded := foldedAny.(Model)
	for _, region := range folded.activeEditor().Folds.Regions {
		if !region.Collapsed {
			t.Fatal("Ctrl+Shift+0 did not fold every region")
		}
	}

	unfoldedAny, _, handled := folded.handleGlobalKey(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl | tea.ModShift})
	if !handled {
		t.Fatal("Ctrl+Shift+J was not handled")
	}
	unfolded := unfoldedAny.(Model)
	for _, region := range unfolded.activeEditor().Folds.Regions {
		if region.Collapsed {
			t.Fatal("Ctrl+Shift+J did not unfold every region")
		}
	}
}

func TestCommandPaletteDoesNotAdvertiseF8ForProblemsPanel(t *testing.T) {
	model := newInputRoutingTestModel(t)
	for _, command := range model.commandRegistry() {
		if command.ID != "toggle_problems" {
			continue
		}
		if command.Shortcut != "" {
			t.Fatalf("problems command shortcut = %q, want empty: F8 navigates problems", command.Shortcut)
		}
		if command.Label != "Show Problems Panel" {
			t.Fatalf("problems command label = %q, want %q", command.Label, "Show Problems Panel")
		}
		return
	}
	t.Fatal("problems command is missing from the command palette")
}

func TestInputRoutingOrdinaryEditorKeyRoutedOnce(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.tabBar.Tabs[model.activeTab].Preview = true

	updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := updated.activeEditor().Buffer.Content(); got != "x" {
		t.Fatalf("editor content = %q, want one routed key", got)
	}
	if !updated.activeEditor().Buffer.Dirty() {
		t.Fatal("editor edit did not mark the buffer dirty")
	}
	if updated.tabBar.Tabs[updated.activeTab].Preview {
		t.Fatal("editor edit did not pin the preview tab")
	}
}

func TestInputRoutingWelcomeFallthrough(t *testing.T) {
	t.Run("ordinary key dismisses and reaches editor once", func(t *testing.T) {
		model := newInputRoutingTestModel(t)
		welcome := editor.NewWelcome(model.theme)
		model.welcome = &welcome

		updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
		if updated.welcome.Active {
			t.Fatal("ordinary key did not dismiss welcome")
		}
		if got := updated.activeEditor().Buffer.Content(); got != "x" {
			t.Fatalf("editor content = %q, want welcome key routed exactly once", got)
		}
	})

	t.Run("allowed global shortcut leaves welcome active", func(t *testing.T) {
		model := newInputRoutingTestModel(t)
		welcome := editor.NewWelcome(model.theme)
		model.welcome = &welcome

		updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
		if !updated.welcome.Active {
			t.Fatal("allowed global shortcut dismissed welcome")
		}
		if !updated.showTree {
			t.Fatal("allowed global shortcut did not reach global routing")
		}
		if got := updated.activeEditor().Buffer.Content(); got != "" {
			t.Fatalf("editor content after allowed global shortcut = %q, want unchanged", got)
		}
	})
}

func TestInputRoutingPluginFeedDoesNotReenterPluginDispatch(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.pluginFeedDepth = 1
	model.pluginKeySequence = "<leader>x"

	updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if updated.pluginKeySequence != "" {
		t.Fatalf("plugin key sequence = %q, want cleared synthetic feed state", updated.pluginKeySequence)
	}
	if !updated.showTree {
		t.Fatal("synthetic feed key did not continue to global routing")
	}
}

func TestInputRoutingFocusedAgentAndSidebar(t *testing.T) {
	t.Run("agent", func(t *testing.T) {
		model := newInputRoutingTestModel(t)
		model.showAgent = true
		model.focus = FocusAgent
		_ = model.agentPanel.Focus()

		updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: 'x', Text: "x"})
		if got := updated.agentPanel.InputValue(); got != "x" {
			t.Fatalf("agent input = %q, want focused child to receive key", got)
		}
		if got := updated.activeEditor().Buffer.Content(); got != "" {
			t.Fatalf("editor content = %q, want agent key not to leak", got)
		}
	})

	t.Run("sidebar tab", func(t *testing.T) {
		model := newInputRoutingTestModel(t)
		model.showTree = true
		model.focus = FocusTree
		model.sidebarTab = SidebarFiles

		updated := updateInputRoutingModel(t, model, tea.KeyPressMsg{Code: tea.KeyTab})
		if updated.focus != FocusGitPanel || updated.sidebarTab != SidebarGit {
			t.Fatalf("sidebar tab route = (%v, %v), want (%v, %v)", updated.focus, updated.sidebarTab, FocusGitPanel, SidebarGit)
		}
	})
}
