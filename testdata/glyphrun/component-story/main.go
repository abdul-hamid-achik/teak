// A deterministic visual fixture for Glyphrun. These are production component
// renderers with small, fixed inputs; no LSP, Git, search or ACP process starts.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/acp"
	"teak/internal/agent"
	"teak/internal/dap"
	"teak/internal/debugger"
	"teak/internal/diff"
	"teak/internal/editor"
	"teak/internal/editor/overlays"
	"teak/internal/git"
	"teak/internal/problems"
	"teak/internal/search"
	"teak/internal/text"
	"teak/internal/ui"
)

var stages = []string{"editor", "completion-loading", "completion-ready", "search-empty", "search-loading", "search-error", "search-results", "git-empty", "git-changes", "agent-empty", "agent-loading", "agent-error", "debugger-empty", "debugger-stopped", "problems", "diff"}

type model struct {
	stage, width, height int
	theme                ui.Theme
	themeID              string
}

func (m model) Init() tea.Cmd { return nil }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			m.stage = (m.stage + 1) % len(stages)
		case "home":
			m.stage = 0
		case "q", "ctrl+q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	w, h := max(1, m.width), max(1, m.height-2)
	body := m.component(w, h)
	header := "Stage: " + stages[m.stage]
	if lipgloss.Width(body) > w || lipgloss.Height(body) > h {
		header += fmt.Sprintf(" OVERFLOW %dx%d", lipgloss.Width(body), lipgloss.Height(body))
	}
	frame := m.theme.Editor.Width(w).MaxWidth(w).Height(h).MaxHeight(h).Render(body)
	v := tea.NewView(zone.Scan(m.theme.StatusBar.Width(w).MaxWidth(w).Render(header) + "\n" + frame + "\n" + m.theme.StatusBar.Width(w).MaxWidth(w).Render(m.themeID+" | Enter next | Home first")))
	v.ForegroundColor = m.theme.Editor.GetForeground()
	v.BackgroundColor = m.theme.Editor.GetBackground()
	v.AltScreen = true
	return v
}

func (m model) component(w, h int) string {
	name, theme := stages[m.stage], m.theme
	switch {
	case name == "editor":
		content := "// Compute the total: café\nfunc total(items []int) int {\n\treturn items[0] + 42\n}\n"
		buf := text.NewBufferFromBytes([]byte(content))
		buf.FilePath = "sample.go"
		e := editor.New(buf, theme, editor.DefaultConfig())
		e.SetSize(w, h-1)
		e.Highlighter.Tokenize([]byte(content))
		buf.RestoreSelections([]text.Selection{
			{Anchor: text.Position{Line: 1, Col: 5}, Head: text.Position{Line: 1, Col: 10}},
			{Anchor: text.Position{Line: 2, Col: 8}, Head: text.Position{Line: 2, Col: 13}},
		}, 0)
		tabs := editor.NewTabBar(theme)
		tabs.Width = w
		tabs.AddTab("sample.go", "sample.go")
		tabs.AddTab("notes.md", "notes.md")
		return tabs.View() + "\n" + e.View()
	case strings.HasPrefix(name, "completion"):
		a := overlays.NewAutocomplete(theme)
		if name == "completion-loading" {
			a.BeginLoading()
		} else {
			a.Show([]overlays.AutocompleteItem{{Label: "total", Detail: "func([]int) int"}, {Label: "items", Detail: "[]int"}, {Label: "itemCount", Detail: "int"}})
		}
		return a.View()
	case strings.HasPrefix(name, "search"):
		s := search.New(theme, "/fixture", search.ModeText)
		s.SetSize(w, h)
		_ = s.Focus()
		if name != "search-empty" {
			s, _ = s.Update(tea.KeyPressMsg{Code: 't', Text: "total"})
			switch name {
			case "search-error":
				s, _ = s.Update(search.SearchResultsMsg{Generation: 1, Err: errors.New("Search tool unavailable")})
			case "search-results":
				s, _ = s.Update(search.SearchResultsMsg{Generation: 1, Results: []search.Result{{FilePath: "/fixture/sample.go", Line: 1, Col: 5, Preview: "func total(items []int) int {"}}})
			}
		}
		return s.View()
	case strings.HasPrefix(name, "git"):
		g := git.New("/fixture", theme)
		g.SetIsGitRepo(true)
		g.SetSize(w, h)
		if name == "git-changes" {
			var cmd tea.Cmd
			g, cmd = g.Update(git.RefreshMsg{Branch: "feature/readability", Entries: []git.StatusEntry{{Path: "sample.go", IndexStatus: 'M', WorkStatus: ' '}, {Path: "notes.md", IndexStatus: '?', WorkStatus: '?'}}})
			if cmd != nil {
				g, _ = g.Update(cmd())
			} // Pure tree projection, no Git command.
			_ = g.FocusTitle()
			g, _ = g.Update(tea.KeyPressMsg{Code: 'r', Text: "Readable selections"})
		}
		return g.View()
	case strings.HasPrefix(name, "agent"):
		a := agent.New(theme)
		a.SetSize(w, h)
		a, _ = a.Update(acp.AgentStartedMsg{})
		_ = a.Focus()
		if name != "agent-empty" {
			a, _ = a.Update(tea.KeyPressMsg{Code: 'e', Text: "Explain this function"})
			a, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if name == "agent-loading" {
				a, _ = a.Update(acp.AgentTextMsg{Text: "The function computes a total."})
			} else {
				var cmd tea.Cmd
				a, cmd = a.Update(acp.AgentPromptResponseMsg{Err: errors.New("Agent connection lost")})
				if cmd != nil {
					a, _ = a.Update(cmd())
				} // Pure response finalization.
			}
		}
		return a.View()
	case strings.HasPrefix(name, "debugger"):
		d := debugger.New(theme)
		d.SetSize(w, h)
		if name == "debugger-stopped" {
			d.SetState(dap.StateStopped)
			d.SetStackFrames([]dap.StackFrame{{Id: 1, Name: "main.total", Source: dap.Source{Path: "/fixture/sample.go"}, Line: 3}})
			d.SetVariables([]dap.Variable{{Name: "items", Value: "[1, 2, 3]", Type: "[]int"}, {Name: "total", Value: "42", Type: "int"}})
			d.SetBreakpoints([]debugger.Breakpoint{{FilePath: "sample.go", Line: 2, Enabled: true, Verified: true}})
		}
		return d.View()
	case name == "problems":
		p := problems.New(theme, "/fixture")
		p.SetSize(w, h)
		p.SetProblems([]problems.Problem{{FilePath: "/fixture/sample.go", Line: 2, Severity: 1, Message: "Index may be out of range", Source: "gopls"}, {FilePath: "/fixture/sample.go", Line: 1, Severity: 2, Message: "Unused parameter", Source: "gopls"}})
		return p.View()
	case name == "diff":
		d := diff.New("sample.txt", []diff.DiffLine{{Left: "return items[0]", Right: "return total(items)", LeftNum: 3, RightNum: 3, LeftKind: diff.KindRemoved, RightKind: diff.KindAdded}, {Left: "}", Right: "}", LeftNum: 4, RightNum: 4}}, theme)
		d.SetSize(w, h)
		return d.View()
	}
	return ""
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--themes" {
		fmt.Println(strings.Join(ui.ThemeIDs(), " "))
		return
	}
	themeID := os.Getenv("TEAK_STORY_THEME")
	if themeID == "" {
		themeID = "nord"
	}
	if !ui.HasTheme(themeID) {
		fmt.Fprintln(os.Stderr, "unknown story theme:", themeID)
		os.Exit(1)
	}
	zone.NewGlobal()
	defer zone.Close()
	if _, err := tea.NewProgram(model{theme: ui.ThemeByName(themeID), themeID: themeID, width: 80, height: 24}, tea.WithColorProfile(colorprofile.TrueColor)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
