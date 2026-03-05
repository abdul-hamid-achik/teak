package filetree

import (
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// OpenFileMsg is sent when a file is selected in the tree (single-click = preview).
type OpenFileMsg struct {
	Path string
}

// PinFileMsg is sent when a file should be opened permanently (double-click or enter).
type PinFileMsg struct {
	Path string
}

// DirExpandedMsg is sent when a directory's children have been read asynchronously.
type DirExpandedMsg struct {
	Path     string
	Children []Entry
}

// Entry represents a file or directory in the tree.
type Entry struct {
	Name     string
	Path     string
	IsDir    bool
	Children []Entry
	Expanded bool
	Loading  bool // true while async directory read is in progress
	Depth    int
}

// Model is a file tree sidebar sub-model.
type Model struct {
	Root    string
	Entries []Entry
	Cursor  int
	ScrollY int
	Width       int
	Height      int
	theme          ui.Theme
	cachedFlat     []Entry
	diagnostics    map[string]int // path → worst severity (1=error, 2=warn, 3=info, 4=hint)
	lastClickPath  string
	lastClickTime  time.Time
}

// SetDiagnostics sets the diagnostics map (file paths + directory paths → worst severity).
func (m *Model) SetDiagnostics(diags map[string]int) {
	m.diagnostics = diags
}

// New creates a new file tree model rooted at the given directory.
// Only reads the first level synchronously for fast startup.
func New(root string, theme ui.Theme) Model {
	m := Model{
		Root:  root,
		theme: theme,
	}
	m.Entries = readDirEntries(root, 0)
	return m
}

// RefreshDir re-reads a directory's children synchronously and updates the tree.
// If the directory is the root, it refreshes the top-level entries.
func (m *Model) RefreshDir(dir string) {
	if dir == m.Root {
		m.Entries = readDirEntries(m.Root, 0)
		m.cachedFlat = nil
		return
	}
	refreshInSlice(m.Entries, dir)
	m.cachedFlat = nil
}

func refreshInSlice(entries []Entry, dir string) bool {
	for i := range entries {
		if entries[i].Path == dir && entries[i].IsDir {
			entries[i].Children = readDirEntries(dir, entries[i].Depth+1)
			entries[i].Loading = false
			return true
		}
		if entries[i].Expanded && entries[i].Children != nil {
			if refreshInSlice(entries[i].Children, dir) {
				return true
			}
		}
	}
	return false
}

// Update handles input for the file tree.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyPress(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case DirExpandedMsg:
		return m.handleDirExpanded(msg)
	}
	return m, nil
}

func (m Model) handleKeyPress(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	flat := m.flatEntries()
	switch msg.String() {
	case "up":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down":
		if m.Cursor < len(flat)-1 {
			m.Cursor++
		}
	case "enter":
		if m.Cursor < len(flat) {
			entry := flat[m.Cursor]
			if entry.IsDir {
				return m.toggleDir(entry.Path)
			}
			// Enter pins the file (not a preview)
			return m, func() tea.Msg {
				return PinFileMsg{Path: entry.Path}
			}
		}
	}
	m.ensureCursorVisible()
	return m, nil
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	idx := m.ScrollY + mouse.Y
	flat := m.flatEntries()
	if idx < 0 || idx >= len(flat) {
		return m, nil
	}
	m.Cursor = idx
	entry := flat[idx]
	if entry.IsDir {
		return m.toggleDir(entry.Path)
	}

	// Detect double-click: same path within 400ms
	now := time.Now()
	isDoubleClick := entry.Path == m.lastClickPath && now.Sub(m.lastClickTime) < 400*time.Millisecond
	m.lastClickPath = entry.Path
	m.lastClickTime = now

	if isDoubleClick {
		return m, func() tea.Msg {
			return PinFileMsg{Path: entry.Path}
		}
	}
	return m, func() tea.Msg {
		return OpenFileMsg{Path: entry.Path}
	}
}

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (Model, tea.Cmd) {
	mouse := msg.Mouse()
	flat := m.flatEntries()
	maxScroll := len(flat) - m.Height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if mouse.Button == tea.MouseWheelUp {
		m.ScrollY -= 3
		if m.ScrollY < 0 {
			m.ScrollY = 0
		}
	} else if mouse.Button == tea.MouseWheelDown {
		m.ScrollY += 3
		if m.ScrollY > maxScroll {
			m.ScrollY = maxScroll
		}
	}
	return m, nil
}

func (m Model) handleDirExpanded(msg DirExpandedMsg) (Model, tea.Cmd) {
	setChildrenInSlice(m.Entries, msg.Path, msg.Children)
	m.cachedFlat = nil
	return m, nil
}

// EntryAtY returns the entry at the given screen Y position, or nil.
func (m Model) EntryAtY(y int) *Entry {
	flat := m.flatEntries()
	idx := m.ScrollY + y
	if idx < 0 || idx >= len(flat) {
		return nil
	}
	return &flat[idx]
}

// ToggleEntry toggles the expand state of a directory entry by path.
func (m *Model) ToggleEntry(path string) (Model, tea.Cmd) {
	return m.toggleDir(path)
}

// toggleDir toggles a directory's expanded state.
// If expanding and children aren't loaded, starts an async read.
func (m *Model) toggleDir(path string) (Model, tea.Cmd) {
	cmd := toggleInSlice(m.Entries, path)
	m.cachedFlat = nil
	return *m, cmd
}

// toggleInSlice toggles expansion and returns a command if async loading is needed.
func toggleInSlice(entries []Entry, path string) tea.Cmd {
	for i := range entries {
		if entries[i].Path == path && entries[i].IsDir {
			entries[i].Expanded = !entries[i].Expanded
			if entries[i].Expanded && entries[i].Children == nil && !entries[i].Loading {
				// Start async directory read
				entries[i].Loading = true
				dirPath := entries[i].Path
				depth := entries[i].Depth + 1
				return func() tea.Msg {
					children := readDirEntries(dirPath, depth)
					return DirExpandedMsg{Path: dirPath, Children: children}
				}
			}
			return nil
		}
		if entries[i].Expanded && entries[i].Children != nil {
			if cmd := toggleInSlice(entries[i].Children, path); cmd != nil {
				return cmd
			}
		}
	}
	return nil
}

// setChildrenInSlice finds the entry by path and sets its children.
func setChildrenInSlice(entries []Entry, path string, children []Entry) bool {
	for i := range entries {
		if entries[i].Path == path && entries[i].IsDir {
			entries[i].Children = children
			entries[i].Loading = false
			return true
		}
		if entries[i].Expanded && entries[i].Children != nil {
			if setChildrenInSlice(entries[i].Children, path, children) {
				return true
			}
		}
	}
	return false
}

func (m *Model) ensureCursorVisible() {
	if m.Cursor < m.ScrollY {
		m.ScrollY = m.Cursor
	}
	if m.Cursor >= m.ScrollY+m.Height {
		m.ScrollY = m.Cursor - m.Height + 1
	}
}

func (m Model) flatEntries() []Entry {
	if m.cachedFlat != nil {
		return m.cachedFlat
	}
	var flat []Entry
	flattenEntries(m.Entries, &flat)
	m.cachedFlat = flat
	return flat
}

func flattenEntries(entries []Entry, flat *[]Entry) {
	for _, e := range entries {
		*flat = append(*flat, e)
		if e.IsDir && e.Expanded && e.Children != nil {
			flattenEntries(e.Children, flat)
		}
	}
}

func readDirEntries(path string, depth int) []Entry {
	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}

	var dirs, files []Entry
	for _, de := range dirEntries {
		name := de.Name()
		// skip dotfiles
		if strings.HasPrefix(name, ".") {
			continue
		}
		entry := Entry{
			Name:  name,
			Path:  filepath.Join(path, name),
			IsDir: de.IsDir(),
			Depth: depth,
		}
		if de.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	return append(dirs, files...)
}

// View renders the file tree.
func (m Model) View() string {
	flat := m.flatEntries()
	var sb strings.Builder

	for i := range m.Height {
		idx := m.ScrollY + i
		if i > 0 {
			sb.WriteByte('\n')
		}
		if idx < len(flat) {
			entry := flat[idx]
			isCursor := idx == m.Cursor
			icon, iconColor := iconForEntry(entry)

			// Determine background based on cursor state
			var bg color.Color
			var baseStyle lipgloss.Style
			if isCursor {
				bg = m.theme.TreeCursor.GetBackground()
				baseStyle = m.theme.TreeCursor
			} else {
				bg = m.theme.TreeEntry.GetBackground()
				baseStyle = m.theme.TreeEntry
			}

			// Build plain text parts to calculate widths accurately
			indent := " " + strings.Repeat("  ", entry.Depth)
			const iconWidth = 2 // Nerd Font icons are typically 2 cells
			nameStr := entry.Name

			// Calculate used width: indent + icon + space + name
			usedWidth := len(indent) + iconWidth + 1 + len(nameStr)

			// Diagnostic dot
			hasDiag := false
			var diagColor color.Color
			if m.diagnostics != nil {
				if sev, ok := m.diagnostics[entry.Path]; ok && sev > 0 {
					hasDiag = true
					diagColor = ui.Nord13
					if sev == 1 {
						diagColor = ui.Nord11
					}
					usedWidth += 2 // " ●"
				}
			}

			// Truncate name if needed
			maxNameWidth := m.Width - (len(indent) + iconWidth + 1)
			if hasDiag {
				maxNameWidth -= 2
			}
			if maxNameWidth > 0 && len(nameStr) > maxNameWidth {
				nameStr = nameStr[:maxNameWidth]
				usedWidth = m.Width
			}

			// Render parts with consistent background
			styledIcon := lipgloss.NewStyle().Foreground(iconColor).Background(bg).Render(icon)
			styledName := lipgloss.NewStyle().Foreground(baseStyle.GetForeground()).Background(bg).Render(nameStr)

			var diagPart string
			if hasDiag {
				diagPart = lipgloss.NewStyle().Foreground(diagColor).Background(bg).Render(" ●")
			}

			// Calculate padding needed
			contentWidth := len(indent) + iconWidth + 1 + len(nameStr)
			if hasDiag {
				contentWidth += 2
			}
			padWidth := m.Width - contentWidth
			if padWidth < 0 {
				padWidth = 0
			}
			padding := strings.Repeat(" ", padWidth)

			// Assemble: indent + icon + space + name + diag + padding
			// Render indent and padding with background too
			indentStyled := lipgloss.NewStyle().Background(bg).Render(indent)
			spaceStyled := lipgloss.NewStyle().Background(bg).Render(" ")
			padStyled := lipgloss.NewStyle().Background(bg).Render(padding)

			line := indentStyled + styledIcon + spaceStyled + styledName + diagPart + padStyled
			sb.WriteString(line)
		} else {
			emptyLine := lipgloss.NewStyle().
				Background(m.theme.TreeEntry.GetBackground()).
				Render(strings.Repeat(" ", m.Width))
			sb.WriteString(emptyLine)
		}
	}

	return sb.String()
}

// SetSize updates the tree dimensions.
func (m *Model) SetSize(width, height int) {
	m.Width = width
	m.Height = height
}
