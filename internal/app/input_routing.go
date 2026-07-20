package app

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/agent"
	"teak/internal/editor"
	"teak/internal/overlay"
	"teak/internal/search"
	"teak/internal/text"
)

// Input routing is intentionally kept in the app package: Model remains the
// single owner of the state that decides precedence between overlays, global
// actions, and focused children.

// handleKeyPressPrecedence handles the root-level stages that always capture
// keyboard input. A false handled result intentionally lets ordinary keys
// continue into the global shortcut stage and then the focused child.
func (m Model) handleKeyPressPrecedence(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	if m.unsavedConfirm != nil {
		updated, cmd := m.unsavedConfirm.Update(msg)
		if updated.IsDismissed() {
			m.unsavedConfirm = nil
			switch msg.String() {
			case "esc", "escape":
				m.cancelSaveAsDestinationPrompt(m.saveAsDestinationPromptID)
			}
		} else {
			m.unsavedConfirm = updated.(*overlay.Confirm)
		}
		return m, cmd, true
	}

	if !m.overlayStack.IsEmpty() {
		return m, m.overlayStack.Update(msg), true
	}
	if m.showBranchPicker {
		model, cmd := m.updateBranchPicker(msg)
		return model.(Model), cmd, true
	}
	if m.showSearch {
		model, cmd := m.updateSearch(msg)
		return model.(Model), cmd, true
	}
	if m.goToLineMode {
		model, cmd := m.handleGoToLineInput(msg)
		return model.(Model), cmd, true
	}
	if m.renameMode {
		model, cmd := m.handleRenameInput(msg)
		return model.(Model), cmd, true
	}
	if m.saveAsMode {
		model, cmd := m.handleSaveAsInput(msg)
		return model.(Model), cmd, true
	}
	if m.newFileMode || m.newFolderMode {
		model, cmd := m.handleNewItemInput(msg)
		return model.(Model), cmd, true
	}
	if m.deleteConfirm {
		model, cmd := m.handleDeleteConfirm(msg)
		return model.(Model), cmd, true
	}

	if m.treeContextMenu.Visible {
		switch msg.String() {
		case "up":
			m.treeContextMenu.MoveUp()
			return m, nil, true
		case "down":
			m.treeContextMenu.MoveDown()
			return m, nil, true
		case "enter":
			if item := m.treeContextMenu.Selected(); item != nil {
				action := item.Action
				m.treeContextMenu.Hide()
				model, cmd := m.handleTreeContextMenuAction(action)
				return model.(Model), cmd, true
			}
			m.treeContextMenu.Hide()
			return m, nil, true
		default:
			m.treeContextMenu.Hide()
			return m, nil, true
		}
	}

	if m.gitContextMenu.Visible {
		switch msg.String() {
		case "up":
			m.gitContextMenu.MoveUp()
			return m, nil, true
		case "down":
			m.gitContextMenu.MoveDown()
			return m, nil, true
		case "enter":
			if item := m.gitContextMenu.Selected(); item != nil {
				action := item.Action
				m.gitContextMenu.Hide()
				model, cmd := m.handleGitContextMenuAction(action)
				return model.(Model), cmd, true
			}
			m.gitContextMenu.Hide()
			return m, nil, true
		default:
			m.gitContextMenu.Hide()
			return m, nil, true
		}
	}

	if m.showHelp {
		key := msg.String()
		if key == "esc" || key == "escape" || key == "f1" {
			m.showHelp = false
			return m, nil, true
		}
		var cmd tea.Cmd
		m.helpM, cmd = m.helpM.Update(msg)
		return m, cmd, true
	}
	if m.showSettings {
		model, cmd := m.updateSettings(msg)
		return model.(Model), cmd, true
	}

	if m.pluginFeedDepth == 0 {
		if model, cmd, handled := m.handlePluginKey(msg); handled {
			return model.(Model), cmd, true
		}
	} else {
		m.pluginKeySequence = ""
	}

	gitInputFocused := m.focus == FocusGitPanel && (m.gitPanel.IsTitleFocused() || m.gitPanel.IsBodyFocused())
	if m.welcome != nil && m.welcome.Active && !gitInputFocused {
		switch msg.String() {
		case "ctrl+q", "ctrl+b", "ctrl+f", "ctrl+shift+f", "ctrl+h", "f1":
			// Global shortcuts intentionally pass through.
		default:
			m.welcome.Dismiss()
		}
	}

	return m, nil, false
}

// handleGlobalKey performs root-level keyboard actions after modal and plugin
// routing have declined the key. It deliberately reports false for an unknown
// key so the focused child receives it exactly once.
func (m Model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+q":
		updated, cmd := m.requestQuit()
		return updated, cmd, true
	case "ctrl+s":
		if m.activeEditor() == nil {
			return m, nil, true
		}
		buf := m.activeEditor().Buffer
		if buf.FilePath == "" {
			m.cancelActiveEditorDrag()
			m.saveAsMode = true
			m.saveAsInput = filepath.Join(m.rootDir, "") + "/"
			return m, nil, true
		}
		return m, m.beginSaveForTab(m.activeTab, false, false), true
	case "ctrl+shift+s":
		if m.activeEditor() == nil {
			return m, nil, true
		}
		m.cancelActiveEditorDrag()
		m.saveAsMode = true
		if m.activeEditor().Buffer.FilePath != "" {
			m.saveAsInput = m.activeEditor().Buffer.FilePath
		} else {
			m.saveAsInput = filepath.Join(m.rootDir, "") + "/"
		}
		return m, nil, true
	case "f1":
		m.cancelActiveEditorDrag()
		m.showHelp = true
		m.helpM = editor.NewHelpModel(m.theme)
		m.helpM.SetSize(m.width, m.height-2)
		return m, m.helpM.Focus(), true
	case "ctrl+b":
		m.showTree = !m.showTree
		if m.showTree && !m.showHelp {
			m.focus = FocusTree
		} else {
			m.focus = FocusEditor
		}
		m.relayout()
		return m, nil, true
	case "ctrl+\\":
		m.toggleSplit()
		return m, nil, true
	case "ctrl+shift+\\":
		m.unsplit()
		return m, nil, true
	case "f6":
		m.cycleSplitFocus()
		return m, nil, true
	case "ctrl+f":
		if ed := m.activeEditor(); ed != nil {
			ed.ShowFind()
		}
		return m, nil, true
	case "ctrl+h":
		model, cmd := m.openSearchReplace()
		return model, cmd, true
	case "ctrl+shift+f":
		model, cmd := m.openSearch(search.ModeText)
		return model, cmd, true
	case "ctrl+space":
		model, cmd := m.requestCompletion()
		return model, cmd, true
	case "alt+k":
		if m.focus == FocusEditor {
			model, cmd := m.requestHover()
			return model, cmd, true
		}
		return m, nil, true
	case "ctrl+k":
		if m.focus == FocusEditor {
			model, cmd := m.requestCodeActions()
			return model, cmd, true
		}
		return m, nil, true
	case "f12":
		model, cmd := m.requestDefinition()
		return model, cmd, true
	case "ctrl+shift+[":
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.Fold(ed.Buffer.Cursor.Line)
			m.setEditor(m.activeTab, *ed)
		}
		return m, nil, true
	case "ctrl+shift+]":
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.Unfold(ed.Buffer.Cursor.Line)
			m.setEditor(m.activeTab, *ed)
		}
		return m, nil, true
	case "ctrl+shift+0":
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.FoldAll()
			m.setEditor(m.activeTab, *ed)
			m.status = "All regions folded"
		}
		return m, nil, true
	case "ctrl+shift+j":
		if ed := m.activeEditor(); ed != nil {
			ed.Folds.UnfoldAll()
			m.setEditor(m.activeTab, *ed)
			m.status = "All regions unfolded"
		}
		return m, nil, true
	case "ctrl+alt+f":
		if m.focus == FocusEditor {
			ed := m.activeEditor()
			if ed == nil || ed.Buffer.FilePath == "" {
				return m, nil, true
			}
			return m, m.requestFormatting(ed.Buffer.FilePath, ed.Config, 0), true
		}
		return m, nil, true
	case "ctrl+shift+o":
		if m.focus == FocusEditor {
			model, cmd := m.requestDocumentSymbols()
			return model, cmd, true
		}
		return m, nil, true
	case "f5":
		model, cmd := m.handleDebugStart()
		return model, cmd, true
	case "shift+f5":
		model, cmd := m.handleDebugStop("Debugging stopped")
		return model, cmd, true
	case "f9":
		if ed := m.activeEditor(); ed != nil && ed.Buffer.FilePath != "" {
			return m, m.toggleBreakpoint(ed.Buffer.FilePath, ed.Buffer.Cursor.Line), true
		}
		return m, nil, true
	case "ctrl+w":
		model, cmd := m.closeCurrentTabSafe()
		return model, cmd, true
	case "ctrl+shift+t":
		if len(m.closedTabs) > 0 {
			lastClosed := m.closedTabs[len(m.closedTabs)-1]
			m.closedTabs = m.closedTabs[:len(m.closedTabs)-1]
			model, cmd := m.openFilePinned(lastClosed.FilePath)
			return model, cmd, true
		}
		return m, nil, true
	case "f3":
		if ed := m.activeEditor(); ed != nil && ed.IsFindVisible() {
			ed.UpdateFind(msg)
			return m, nil, true
		}
		model, cmd := m.findNext()
		return model, cmd, true
	case "shift+f3":
		if ed := m.activeEditor(); ed != nil && ed.IsFindVisible() {
			ed.UpdateFind(msg)
			return m, nil, true
		}
		model, cmd := m.findPrev()
		return model, cmd, true
	case "ctrl+n":
		model, cmd := m.newUntitledTab()
		return model, cmd, true
	case "ctrl+g":
		m.cancelActiveEditorDrag()
		m.goToLineMode = true
		m.goToLineInput = ""
		return m, nil, true
	case "ctrl+shift+g":
		if m.gitPanel.IsGitRepo() {
			m.showTree = true
			m.sidebarTab = SidebarGit
			m.focus = FocusGitPanel
			m.relayout()
		}
		return m, nil, true
	case "ctrl+p":
		model, cmd := m.openQuickOpen()
		return model, cmd, true
	case "ctrl+shift+p":
		model, cmd := m.openCommandPalette()
		return model, cmd, true
	case "ctrl+tab":
		if len(m.editors) > 1 {
			m.activateTab((m.activeTab + 1) % len(m.editors))
		}
		return m, nil, true
	case "ctrl+shift+tab":
		if len(m.editors) > 1 {
			m.activateTab((m.activeTab - 1 + len(m.editors)) % len(m.editors))
		}
		return m, nil, true
	case "ctrl+j":
		return m, m.toggleAgentPanel(), true
	case "ctrl+'":
		if m.showAgent {
			if m.focus == FocusAgent {
				m.focus = FocusEditor
				m.agentPanel.Blur()
			} else {
				m.focus = FocusAgent
				return m, m.agentPanel.Focus(), true
			}
		}
		return m, nil, true
	case "ctrl+,":
		m.openSettingsOverlay()
		return m, nil, true
	case "f8":
		if m.problemsPanel.ProblemCount() > 0 {
			m.problemsPanel.SelectNext()
			if prob := m.problemsPanel.SelectedProblem(); prob != nil {
				pos := text.Position{Line: prob.Line, Col: prob.Col}
				m.setPendingCursor(prob.FilePath, pos)
				model, cmd := m.openFile(prob.FilePath)
				updated := model.(Model)
				updated.status = fmt.Sprintf("Problem %d/%d", updated.problemsPanel.SelectedIndex()+1, updated.problemsPanel.ProblemCount())
				return updated, cmd, true
			}
		}
		return m, nil, true
	case "shift+f8":
		if m.problemsPanel.ProblemCount() > 0 {
			m.problemsPanel.SelectPrev()
			if prob := m.problemsPanel.SelectedProblem(); prob != nil {
				pos := text.Position{Line: prob.Line, Col: prob.Col}
				m.setPendingCursor(prob.FilePath, pos)
				model, cmd := m.openFile(prob.FilePath)
				updated := model.(Model)
				updated.status = fmt.Sprintf("Problem %d/%d", updated.problemsPanel.SelectedIndex()+1, updated.problemsPanel.ProblemCount())
				return updated, cmd, true
			}
		}
		return m, nil, true
	default:
		return m, nil, false
	}
}

// routeFocusedInput gives the focused child the final opportunity to handle a
// message after every root-level message category has declined it. The handled
// result deliberately distinguishes "there is no active recipient" from a
// child that consumed a message without scheduling a command.
func (m Model) routeFocusedInput(msg tea.Msg) (Model, tea.Cmd, bool) {
	if m.showAgent && m.focus == FocusAgent {
		if kp, ok := msg.(tea.KeyPressMsg); ok {
			if m.agentPanel.HasPendingWrite() {
				var cmd tea.Cmd
				m.agentPanel, cmd = m.agentPanel.Update(kp)
				if cmd == nil {
					return m, nil, true
				}
				result := cmd()
				if decision, ok := result.(agent.WriteDecisionMsg); ok {
					model, nextCmd := m.handleAgentWriteDecision(decision)
					return model.(Model), nextCmd, true
				}
				return m, func() tea.Msg { return result }, true
			}
			switch kp.String() {
			case "esc", "escape":
				m.focus = FocusEditor
				m.agentPanel.Blur()
				return m, nil, true
			case "enter":
				newM, cmd, handled := m.handleAgentEnter()
				if handled {
					return newM, cmd, true
				}
				// Not a slash command — let the panel add the message, then send it.
				input := strings.TrimSpace(m.agentPanel.InputValue())
				if input != "" {
					var panelCmd tea.Cmd
					m.agentPanel, panelCmd = m.agentPanel.Update(kp)
					return m, tea.Batch(panelCmd, m.sendAgentPrompt(input)), true
				}
				return m, nil, true
			case "ctrl+c":
				if m.acpMgr != nil {
					m.acpMgr.Cancel()
				}
				return m, nil, true
			default:
				var cmd tea.Cmd
				m.agentPanel, cmd = m.agentPanel.Update(kp)
				return m, cmd, true
			}
		}
		if wm, ok := msg.(tea.MouseWheelMsg); ok {
			var cmd tea.Cmd
			m.agentPanel, cmd = m.agentPanel.Update(wm)
			return m, cmd, true
		}
	}

	if m.showTree && m.focus == FocusTree {
		if kp, ok := msg.(tea.KeyPressMsg); ok && kp.String() == "tab" {
			switch m.sidebarTab {
			case SidebarFiles:
				m.sidebarTab = SidebarGit
				m.focus = FocusGitPanel
			case SidebarGit:
				m.sidebarTab = SidebarProblems
				m.focus = FocusProblems
			case SidebarProblems:
				m.sidebarTab = SidebarDebugger
				m.focus = FocusDebugger
			default:
				m.sidebarTab = SidebarFiles
				m.focus = FocusTree
			}
			return m, nil, true
		}
		var cmd tea.Cmd
		m.tree, cmd = m.tree.Update(msg)
		return m, cmd, true
	}
	if m.focus == FocusGitPanel {
		var cmd tea.Cmd
		m.gitPanel, cmd = m.gitPanel.Update(msg)
		return m, cmd, true
	}
	if m.focus == FocusProblems {
		model, cmd := m.updateProblems(msg)
		return model.(Model), cmd, true
	}
	if m.focus == FocusDebugger {
		model, cmd := m.updateDebugger(msg)
		return model.(Model), cmd, true
	}

	if m.isActiveDiffTab() {
		if dv, ok := m.diffViews[m.activeTab]; ok {
			var cmd tea.Cmd
			dv, cmd = dv.Update(msg)
			m.diffViews[m.activeTab] = dv
			return m, cmd, true
		}
		return m, nil, true
	}

	if m.activeEditor() == nil {
		return m, nil, false
	}

	var cmd tea.Cmd
	ed := *m.activeEditor()
	if ed.Buffer.FilePath != "" {
		ed.HasLSP = m.lspMgr.ClientForFile(ed.Buffer.FilePath) != nil
	}
	prevVersion := ed.Buffer.Version()
	prevCursor := ed.Buffer.Cursor
	ed, cmd = ed.Update(msg)
	m.setEditor(m.activeTab, ed)

	return m, tea.Batch(cmd, m.syncEditorStateAfterUpdate(m.activeTab, prevVersion, prevCursor)), true
}
