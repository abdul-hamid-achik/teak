package modes

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/app"
)

func TestModeIDValues(t *testing.T) {
	tests := []struct {
		mode ModeID
		want int
	}{
		{ModeNormal, 0},
		{ModeInsert, 1},
		{ModeRename, 2},
		{ModeGoToLine, 3},
		{ModeSearch, 4},
		{ModeSearchReplace, 5},
		{ModeNewFile, 6},
		{ModeNewFolder, 7},
		{ModeDeleteConfirm, 8},
		{ModeCloseConfirm, 9},
		{ModeContextMenu, 10},
		{ModeBranchPicker, 11},
		{ModeSettings, 12},
		{ModeSaveAs, 13},
		{ModeCommandPalette, 14},
		{ModeQuickOpen, 15},
		{ModePluginKey, 16},
	}

	for _, tt := range tests {
		name := func() string {
			switch tt.mode {
			case ModeNormal:
				return "ModeNormal"
			case ModeInsert:
				return "ModeInsert"
			case ModeRename:
				return "ModeRename"
			case ModeGoToLine:
				return "ModeGoToLine"
			case ModeSearch:
				return "ModeSearch"
			case ModeSearchReplace:
				return "ModeSearchReplace"
			case ModeNewFile:
				return "ModeNewFile"
			case ModeNewFolder:
				return "ModeNewFolder"
			case ModeDeleteConfirm:
				return "ModeDeleteConfirm"
			case ModeCloseConfirm:
				return "ModeCloseConfirm"
			case ModeContextMenu:
				return "ModeContextMenu"
			case ModeBranchPicker:
				return "ModeBranchPicker"
			case ModeSettings:
				return "ModeSettings"
			case ModeSaveAs:
				return "ModeSaveAs"
			case ModeCommandPalette:
				return "ModeCommandPalette"
			case ModeQuickOpen:
				return "ModeQuickOpen"
			case ModePluginKey:
				return "ModePluginKey"
			default:
				return "Unknown"
			}
		}()

		t.Run(name, func(t *testing.T) {
			if int(tt.mode) != tt.want {
				t.Errorf("ModeID = %d, want %d", tt.mode, tt.want)
			}
		})
	}
}

func TestBaseMode(t *testing.T) {
	b := &BaseMode{}

	if b.Enter(nil) != nil {
		t.Error("BaseMode.Enter should return nil")
	}

	if b.Exit(nil) != nil {
		t.Error("BaseMode.Exit should return nil")
	}

	if !b.ShouldIntercept(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"})) {
		t.Error("BaseMode.ShouldIntercept should return true")
	}
}

type testMode struct {
	BaseMode
	id          ModeID
	intercept   bool
	enterCalled bool
	exitCalled  bool
}

func (m *testMode) ID() ModeID                     { return m.id }
func (m *testMode) ShouldIntercept(_ tea.Msg) bool { return m.intercept }
func (m *testMode) Enter(_ *app.Model) tea.Cmd {
	m.enterCalled = true
	return nil
}
func (m *testMode) Exit(_ *app.Model) tea.Cmd {
	m.exitCalled = true
	return nil
}
func (m *testMode) Update(_ tea.Msg) (Mode, ModeID, []tea.Cmd) {
	return m, m.id, nil
}

func TestModeInterface(t *testing.T) {
	m := &testMode{id: ModeNormal, intercept: true}

	if m.ID() != ModeNormal {
		t.Errorf("ID() = %v, want ModeNormal", m.ID())
	}

	if !m.ShouldIntercept(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"})) {
		t.Error("ShouldIntercept should return true")
	}

	var mode Mode = m
	if mode.ID() != ModeNormal {
		t.Error("Mode interface ID() should work")
	}
}

func TestModeTransition(t *testing.T) {
	m := &testMode{id: ModeNormal}

	updated, newModeID, cmds := m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))

	if updated == nil {
		t.Error("Update should return non-nil mode")
	}
	if newModeID != ModeNormal {
		t.Errorf("newModeID = %v, want ModeNormal", newModeID)
	}
	_ = cmds // cmds may be nil or empty depending on mode implementation
}
