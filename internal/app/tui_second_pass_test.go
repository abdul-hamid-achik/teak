package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sdk "github.com/coder/acp-go-sdk"
	"teak/internal/acp"
	"teak/internal/dap"
	"teak/internal/diff"
	"teak/internal/editor"
	"teak/internal/overlay"
	"teak/internal/problems"
	"teak/internal/search"
)

func TestTUIPasteStaysInFocusedInput(t *testing.T) {
	for _, findOpen := range []bool{false, true} {
		t.Run(map[bool]string{false: "agent", true: "agent_with_inactive_find"}[findOpen], func(t *testing.T) {
			m := newViewTestModel(t, false)
			m.showAgent = true
			m.relayout()
			if findOpen {
				m.activeEditor().ShowFind()
			}
			m.setFocus(FocusAgent)
			m.agentPanel.Focus()
			before := m.activeEditor().Buffer.Content()
			updated, _ := m.Update(tea.PasteMsg{Content: "explain café"})
			m = updated.(Model)
			if got := m.agentPanel.InputValue(); got != "explain café" {
				t.Errorf("agent input = %q", got)
			}
			if got := m.activeEditor().Buffer.Content(); got != before {
				t.Errorf("paste changed document: %q", got)
			}
			if strings.Contains(m.activeEditor().FindView(), "explain") {
				t.Error("inactive Find captured paste")
			}
		})
	}
	t.Run("workspace_search", func(t *testing.T) {
		m := newViewTestModel(t, false)
		updated, _ := m.openSearch(search.ModeText)
		m = updated.(Model)
		updated, cmd := m.Update(tea.PasteMsg{Content: "needle"})
		m = updated.(Model)
		if m.searchM.Query() != "needle" || cmd == nil {
			t.Errorf("query = %q, scheduled = %v", m.searchM.Query(), cmd != nil)
		}
	})
	t.Run("find_schedules_scan", func(t *testing.T) {
		m := newViewTestModel(t, false)
		m.activeEditor().ShowFind()
		_, cmd, _ := m.handlePastePrecedence(tea.PasteMsg{Content: "main"})
		if cmd == nil {
			t.Fatal("pasted Find query never schedules its scan")
		}
		var hasDebounce func(tea.Cmd) bool
		hasDebounce = func(cmd tea.Cmd) bool {
			if cmd == nil {
				return false
			}
			switch msg := cmd().(type) {
			case editor.FindDebounceMsg:
				return true
			case tea.BatchMsg:
				for _, child := range msg {
					if hasDebounce(child) {
						return true
					}
				}
			}
			return false
		}
		if !hasDebounce(cmd) {
			t.Error("Find paste command has no scan debounce")
		}
	})
}

func TestTUISplitWheelTargetsHoveredPane(t *testing.T) {
	for _, focused := range []int{0, 1} {
		t.Run(map[int]string{0: "hover_B", 1: "hover_A"}[focused], func(t *testing.T) {
			m := newViewTestModel(t, false)
			body := strings.Repeat("long enough line for dragging\n", 100)
			a := addDirtyEditor(t, &m, "a.txt", body, body)
			b := addDirtyEditor(t, &m, "b.txt", body, body)
			m.activateTab(a)
			m.toggleSplit()
			if focused == 1 {
				m.cycleSplitFocus()
			}
			layout := m.mouseLayout()
			x, y, target, untouched := layout.editorPaneB.x+3, 3, b, a
			if focused == 1 {
				x, target, untouched = layout.editorBody.x+3, a, b
			}
			active := m.activeTab
			updated, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown, X: x, Y: y}))
			m = updated.(Model)
			if m.editors[target].Viewport.ScrollY == 0 {
				t.Error("hovered pane did not scroll")
			}
			if m.editors[untouched].Viewport.ScrollY != 0 {
				t.Error("wheel scrolled the other pane")
			}
			if m.activeTab != active {
				t.Error("wheel stole keyboard focus")
			}
		})
	}
}

func TestTUIResizeUpdatesOpenOverlays(t *testing.T) {
	for _, surface := range []string{"search", "picker", "input", "confirm", "float"} {
		t.Run(surface, func(t *testing.T) {
			m := newViewTestModel(t, false)
			if surface == "search" {
				updated, _ := m.openSearch(search.ModeText)
				m = updated.(Model)
			} else if surface == "input" {
				i := overlay.NewInput("Value", "", m.theme)
				i.SetWidth(70)
				m.overlayStack.Push(i)
			} else if surface == "confirm" {
				c := overlay.NewConfirm("Confirm", "Proceed?", nil, []overlay.Button{{Label: "Cancel"}}, m.theme)
				c.SetWidth(70)
				m.overlayStack.Push(c)
			} else if surface == "float" {
				m.overlayStack.Push(overlay.NewFloat(1, "Details", "text", 70, 20))
			} else {
				p := overlay.NewPicker("Choose", []overlay.PickerItem{{Label: "item"}}, m.theme, "resize-test")
				p.SetSize(70, 24)
				m.overlayStack.Push(p)
			}
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 18})
			m = updated.(Model)
			var view string
			if surface != "search" {
				view = m.overlayStack.View()
			} else {
				view = m.searchM.View()
			}
			if w, h := lipgloss.Width(view), lipgloss.Height(view); w > 40 || h > 18 {
				t.Errorf("overlay = %dx%d after resize to 40x18", w, h)
			}
		})
	}
}

func TestTUISplitDragUsesOriginPaneCoordinates(t *testing.T) {
	m := newViewTestModel(t, false)
	body := strings.Repeat("abcdefghijklmnopqrstuvwxyz\n", 30)
	a := addDirtyEditor(t, &m, "a.txt", body, body)
	addDirtyEditor(t, &m, "b.txt", body, body)
	m.activateTab(a)
	m.toggleSplit()
	m.cycleSplitFocus()
	pane := m.activeEditorBodyRect()
	updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: pane.x + 10, Y: pane.y + 2}))
	m = updated.(Model)
	if !m.activeEditor().IsDragging() {
		t.Fatal("drag did not begin")
	}
	updated, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{Button: tea.MouseLeft, X: 20, Y: pane.y + 2}))
	m = updated.(Model)
	if got := m.activeEditor().Buffer.Selections.Primary().Head.Col; got != 0 {
		t.Errorf("drag crossing left edge selects col %d, want 0", got)
	}
}

func TestTUIHiddenPanelCannotKeepKeyboardFocus(t *testing.T) {
	for _, area := range []FocusArea{FocusTree, FocusGitPanel, FocusProblems, FocusDebugger, FocusTerminal, FocusAgent} {
		t.Run(fmt.Sprint(area), func(t *testing.T) {
			m := newViewTestModel(t, true)
			m.showAgent = true
			m.showTerminal = true
			m.setFocus(area)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 3})
			m = updated.(Model)
			if m.focus != FocusEditor {
				t.Errorf("invisible panel retains focus %v", m.focus)
			}
		})
	}
}

func TestTUIAgentPermissionButtonsUseScreenCoordinates(t *testing.T) {
	for _, tc := range []struct {
		name, zone string
		kind       sdk.PermissionOptionKind
	}{{"allow", "agent-perm-allow", sdk.PermissionOptionKindAllowOnce}, {"deny", "agent-perm-deny", sdk.PermissionOptionKindRejectOnce}} {
		t.Run(tc.name, func(t *testing.T) {
			m := newViewTestModel(t, true)
			m.showAgent = true
			m.agentPanel.SetConnected(true)
			m.agentPanel.AddSystemMessage("Review requested")
			m.relayout()
			responses := make(chan sdk.RequestPermissionResponse, 1)
			m.agentPanel, _ = m.agentPanel.Update(acp.AgentPermissionRequestMsg{Options: []sdk.PermissionOption{{Kind: tc.kind, OptionId: sdk.PermissionOptionId(tc.name)}}, ResponseCh: responses})
			_ = m.View()
			z := awaitDebugZone(t, tc.zone)
			if !m.mouseLayout().agentBody.contains(z.StartX, z.StartY) {
				t.Fatal("button is not in agent panel")
			}
			updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, X: z.StartX + 1, Y: z.StartY}))
			m = updated.(Model)
			if m.agentPanel.HasPermissionPending() {
				t.Error("click did not resolve permission")
			}
			select {
			case <-responses:
			default:
				t.Error("agent received no decision")
			}
		})
	}
}

func TestTUISplitContextMenuStaysInClickedPane(t *testing.T) {
	m := newViewTestModel(t, false)
	addDirtyEditor(t, &m, "b.txt", "other\n", "other\n")
	m.activateTab(0)
	m.toggleSplit()
	m.cycleSplitFocus()
	pane := m.activeEditorBodyRect()
	updated, _ := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseRight, X: pane.x + 8, Y: pane.y + 2}))
	m = updated.(Model)
	_, rect, ok := m.editorContextMenuGeometry()
	if !ok || rect.x < pane.x || rect.x+rect.width > pane.x+pane.width {
		t.Errorf("menu %#v is outside clicked pane %#v", rect, pane)
	}
}

func TestTUITerminalGeometryWithOtherPanels(t *testing.T) {
	for _, tree := range []bool{false, true} {
		for _, agent := range []bool{false, true} {
			for _, split := range []bool{false, true} {
				t.Run(fmt.Sprintf("tree_%t_agent_%t_split_%t", tree, agent, split), func(t *testing.T) {
					m := newViewTestModel(t, tree)
					m.showAgent = agent
					m.showTerminal = true
					if split {
						addDirtyEditor(t, &m, "second.txt", "second\n", "second\n")
						m.activateTab(0)
						m.toggleSplit()
					}
					m.relayout()
					lines := strings.Split(m.View().Content, "\n")
					y := m.mouseLayout().terminalBody.y
					if y >= len(lines) || !strings.Contains(lines[y], "Terminal") {
						t.Errorf("terminal header is not at its interactive row %d", y)
					}
					if !strings.Contains(lines[len(lines)-1], "F1") {
						t.Error("status bar clipped")
					}
				})
			}
		}
	}
}

func TestTUIPromptKeepsLongUnicodePathCaretVisible(t *testing.T) {
	for _, width := range []int{40, 80} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m := newViewTestModel(t, false)
			m.width = width
			m.height = 18
			m.saveAsMode = true
			path := "/" + strings.Repeat("directorio界/", 12) + "final.go"
			setPrompt(&m.saveAsInput, &m.saveAsCursor, path)
			m.relayout()
			if view := m.View().Content; !strings.Contains(view, "final.go_") {
				t.Error("end of path and caret are clipped")
			}
			m.saveAsCursor = 0
			if view := m.View().Content; !strings.Contains(view, "_/") {
				t.Error("home caret is not visible")
			}
		})
	}
}

func TestTUISidebarContentCannotPushStatusOffScreen(t *testing.T) {
	for _, tab := range []SidebarTab{SidebarFiles, SidebarGit, SidebarProblems, SidebarDebugger} {
		for _, size := range [][2]int{{40, 12}, {80, 24}, {120, 40}} {
			t.Run(fmt.Sprintf("tab_%d_%v", tab, size), func(t *testing.T) {
				m := newViewTestModel(t, true)
				m.width, m.height = size[0], size[1]
				m.sidebarTab = tab
				m.showTerminal = true
				m.debuggerPanel.SetState(dap.StateStopped)
				m.debuggerPanel.SetStackFrames([]dap.StackFrame{{Name: strings.Repeat("wide_function_", 8), Source: dap.Source{Path: "/tmp/main.go"}, Line: 1}})
				var vars []dap.Variable
				for i := range 20 {
					vars = append(vars, dap.Variable{Name: fmt.Sprint(i), Value: strings.Repeat("界", 40)})
				}
				m.debuggerPanel.SetVariables(vars)
				m.problemsPanel.SetProblems([]problems.Problem{{FilePath: "main.go", Message: strings.Repeat("diagnostic ", 40), Severity: 1}})
				m.relayout()
				lines := strings.Split(m.View().Content, "\n")
				if len(lines) > size[1] {
					t.Errorf("view has %d rows", len(lines))
				}
				if !strings.Contains(lines[len(lines)-1], "F1") {
					t.Error("sidebar pushed status off screen")
				}
				for y, line := range lines {
					if lipgloss.Width(line) > size[0] {
						t.Errorf("row %d width %d exceeds %d", y, lipgloss.Width(line), size[0])
						break
					}
				}
			})
		}
	}
}

func TestTUISplitRendersDiffInItsOwnPane(t *testing.T) {
	for _, tree := range []bool{false, true} {
		t.Run(fmt.Sprint(tree), func(t *testing.T) {
			m := newViewTestModel(t, tree)
			a := addDirtyEditor(t, &m, "a.txt", "document", "document")
			b := addDirtyEditor(t, &m, "b.txt", "", "")
			m.tabBar.Tabs[b].Kind = editor.TabDiff
			m.diffViews = map[int]diff.Model{b: diff.New("b.txt", []diff.DiffLine{{Left: "before", Right: "after", LeftNum: 1, RightNum: 1}}, m.theme)}
			m.activateTab(a)
			m.toggleSplit()
			for _, size := range [][2]int{{120, 40}, {80, 24}} {
				updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m = updated.(Model)
				if width := m.diffViews[b].Width; width != m.mouseLayout().editorPaneB.width {
					t.Errorf("diff width = %d, pane width = %d", width, m.mouseLayout().editorPaneB.width)
				}
				view := m.renderSplitPanes()
				if !strings.Contains(view, "before") || !strings.Contains(view, "after") || !strings.Contains(view, "document") {
					t.Errorf("split lost document or diff contents: %q", view)
				}
			}
		})
	}
}
