package git

import (
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// StatusEntry represents a file with git status.
type StatusEntry struct {
	Path   string
	Status string // "M", "A", "D", "??"
}

// RefreshMsg is sent when git status data has been fetched.
type RefreshMsg struct {
	Branch  string
	Entries []StatusEntry
	Err     error
}

// Model is the git sidebar panel model.
type Model struct {
	Branch    string
	Entries   []StatusEntry
	Cursor    int
	ScrollY   int
	Width     int
	Height    int
	Collapsed bool
	theme     ui.Theme
	rootDir   string
	isGitRepo bool
}

// New creates a new git panel model.
func New(rootDir string, theme ui.Theme) Model {
	m := Model{
		theme:   theme,
		rootDir: rootDir,
	}
	// Check if inside a git repo
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = rootDir
	if err := cmd.Run(); err == nil {
		m.isGitRepo = true
	}
	return m
}

// IsGitRepo returns whether the root dir is inside a git repository.
func (m Model) IsGitRepo() bool {
	return m.isGitRepo
}

// Refresh returns a command that fetches git branch and status asynchronously.
func (m Model) Refresh() tea.Cmd {
	if !m.isGitRepo {
		return nil
	}
	rootDir := m.rootDir
	return func() tea.Msg {
		branch := ""
		branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		branchCmd.Dir = rootDir
		if out, err := branchCmd.Output(); err == nil {
			branch = strings.TrimSpace(string(out))
		}

		var entries []StatusEntry
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = rootDir
		if out, err := statusCmd.Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range lines {
				if len(line) < 4 {
					continue
				}
				status := strings.TrimSpace(line[:2])
				path := strings.TrimSpace(line[3:])
				entries = append(entries, StatusEntry{Path: path, Status: status})
			}
		}

		return RefreshMsg{Branch: branch, Entries: entries}
	}
}

// Update handles messages for the git panel.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case RefreshMsg:
		if msg.Err == nil {
			m.Branch = msg.Branch
			m.Entries = msg.Entries
			if m.Cursor >= len(m.Entries) {
				m.Cursor = max(0, len(m.Entries)-1)
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "up":
			if m.Cursor > 0 {
				m.Cursor--
			}
			return m, nil
		case "down":
			if m.Cursor < len(m.Entries)-1 {
				m.Cursor++
			}
			return m, nil
		case "enter":
			// Toggle collapsed state when no entries, or could open file
			m.Collapsed = !m.Collapsed
			return m, nil
		}
	}
	return m, nil
}

// ToggleCollapsed toggles the collapsed state.
func (m *Model) ToggleCollapsed() {
	m.Collapsed = !m.Collapsed
}

// SetSize sets the panel dimensions.
func (m *Model) SetSize(w, h int) {
	m.Width = w
	m.Height = h
}

// View renders the git panel.
func (m Model) View() string {
	if !m.isGitRepo || m.Width == 0 || m.Height == 0 {
		return ""
	}

	var sb strings.Builder

	// Header line
	branchName := m.Branch
	if branchName == "" {
		branchName = "HEAD"
	}
	arrow := "▾"
	if m.Collapsed {
		arrow = "▸"
	}
	header := m.theme.GitHeader.Render(fmt.Sprintf(" %s %s", branchName, arrow))
	sb.WriteString(header)

	if m.Collapsed {
		// Pad remaining height with empty lines
		for i := 1; i < m.Height; i++ {
			sb.WriteByte('\n')
		}
		return sb.String()
	}

	sb.WriteByte('\n')

	// Changes section
	if len(m.Entries) > 0 {
		changesHeader := m.theme.GitHeader.Render(fmt.Sprintf("CHANGES (%d)", len(m.Entries)))
		sb.WriteString(changesHeader)
		sb.WriteByte('\n')

		// Visible entries (leave room for header lines)
		maxVisible := m.Height - 3 // header + changes header + padding
		if maxVisible < 1 {
			maxVisible = 1
		}

		// Scroll offset
		startIdx := m.ScrollY
		if m.Cursor >= startIdx+maxVisible {
			startIdx = m.Cursor - maxVisible + 1
		}
		if m.Cursor < startIdx {
			startIdx = m.Cursor
		}
		m.ScrollY = startIdx

		endIdx := min(startIdx+maxVisible, len(m.Entries))
		for i := startIdx; i < endIdx; i++ {
			e := m.Entries[i]
			statusStyle := m.statusStyle(e.Status)
			statusStr := statusStyle.Render(fmt.Sprintf(" %2s", e.Status))
			pathStr := " " + truncPath(e.Path, m.Width-5)

			line := statusStr + pathStr
			if i == m.Cursor {
				line = m.theme.GitCursor.Width(m.Width).Render(line)
			} else {
				line = m.theme.GitEntry.Width(m.Width).Render(line)
			}
			sb.WriteString(line)
			if i < endIdx-1 {
				sb.WriteByte('\n')
			}
		}
	} else {
		sb.WriteString(m.theme.GitEntry.Render(" No changes"))
	}

	return sb.String()
}

func (m Model) statusStyle(status string) lipgloss.Style {
	switch {
	case status == "??":
		return m.theme.GitUntracked
	case strings.Contains(status, "A"):
		return m.theme.GitAdded
	case strings.Contains(status, "M"):
		return m.theme.GitModified
	case strings.Contains(status, "D"):
		return m.theme.GitDeleted
	default:
		return m.theme.GitEntry
	}
}

func truncPath(path string, maxLen int) string {
	if maxLen <= 0 || len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}
