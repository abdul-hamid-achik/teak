package ui

import "charm.land/lipgloss/v2"

// Exported Nord color palette for use by other packages.
var (
	Nord0  = lipgloss.Color("#2E3440")
	Nord1  = lipgloss.Color("#3B4252")
	Nord2  = lipgloss.Color("#434C5E")
	Nord3  = lipgloss.Color("#4C566A")
	Nord4  = lipgloss.Color("#D8DEE9")
	Nord5  = lipgloss.Color("#E5E9F0")
	Nord6  = lipgloss.Color("#ECEFF4")
	Nord7  = lipgloss.Color("#8FBCBB")
	Nord8  = lipgloss.Color("#88C0D0")
	Nord9  = lipgloss.Color("#81A1C1")
	Nord10 = lipgloss.Color("#5E81AC")
	Nord11 = lipgloss.Color("#BF616A")
	Nord12 = lipgloss.Color("#D08770")
	Nord13 = lipgloss.Color("#EBCB8B")
	Nord14 = lipgloss.Color("#A3BE8C")
	Nord15 = lipgloss.Color("#B48EAD")
)

// Theme holds lipgloss styles for the editor UI.
type Theme struct {
	Editor       lipgloss.Style
	Gutter       lipgloss.Style
	GutterActive lipgloss.Style
	Selection    lipgloss.Style
	CursorLine   lipgloss.Style
	StatusBar    lipgloss.Style
	StatusText   lipgloss.Style
	HelpBorder   lipgloss.Style
	HelpTitle    lipgloss.Style
	HelpKey      lipgloss.Style
	TreeEntry    lipgloss.Style
	TreeCursor   lipgloss.Style
	TreeBorder   lipgloss.Style

	// Tab bar
	TabActive        lipgloss.Style
	TabInactive      lipgloss.Style
	TabCloseActive   lipgloss.Style
	TabCloseInactive lipgloss.Style
	TabBar           lipgloss.Style

	// Search
	SearchBox    lipgloss.Style
	SearchInput  lipgloss.Style
	SearchResult lipgloss.Style
	SearchActive lipgloss.Style

	// Diagnostics
	DiagError   lipgloss.Style
	DiagWarning lipgloss.Style
	DiagInfo    lipgloss.Style
	DiagHint    lipgloss.Style
	GutterError lipgloss.Style
	GutterWarn  lipgloss.Style

	// Autocomplete
	AutocompleteItem   lipgloss.Style
	AutocompleteCursor lipgloss.Style
	AutocompleteBox    lipgloss.Style

	// Hover
	HoverBox lipgloss.Style

	// Context menu
	ContextMenuDisabled lipgloss.Style

	// Git panel
	GitHeader    lipgloss.Style
	GitEntry     lipgloss.Style
	GitCursor    lipgloss.Style
	GitAdded     lipgloss.Style
	GitModified  lipgloss.Style
	GitDeleted   lipgloss.Style
	GitUntracked lipgloss.Style

	// Diff view
	DiffRemoved    lipgloss.Style
	DiffAdded      lipgloss.Style
	DiffEmpty      lipgloss.Style
	DiffGutter     lipgloss.Style
	DiffBorder     lipgloss.Style
	DiffHunkHeader lipgloss.Style

	// Sidebar tabs
	SidebarTabActive   lipgloss.Style
	SidebarTabInactive lipgloss.Style

	// Git action buttons
	GitActionButton lipgloss.Style
	GitSectionHeader lipgloss.Style
	GitBranch        lipgloss.Style
	GitCommitInput   lipgloss.Style
}

// DefaultTheme returns the Nord-themed styles.
func DefaultTheme() Theme {
	return Theme{
		Editor: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord4),
		Gutter: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord3).
			PaddingRight(1),
		GutterActive: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord4).
			PaddingRight(1).
			Bold(true),
		Selection: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6),
		CursorLine: lipgloss.NewStyle().
			Background(Nord1),
		StatusBar: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord4),
		StatusText: lipgloss.NewStyle().
			Background(Nord10).
			Foreground(Nord6).
			Padding(0, 1),
		HelpBorder: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord3),
		HelpTitle: lipgloss.NewStyle().
			Foreground(Nord8).
			Bold(true),
		HelpKey: lipgloss.NewStyle().
			Foreground(Nord13),
		TreeEntry: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord4),
		TreeCursor: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6),
		TreeBorder: lipgloss.NewStyle().
			Foreground(Nord3),

		// Tab bar styles
		TabActive: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord6).
			Padding(0, 1).
			Bold(true),
		TabInactive: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord3).
			Padding(0, 1),
		TabCloseActive: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord4),
		TabCloseInactive: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord3),
		TabBar: lipgloss.NewStyle().
			Background(Nord0),

		// Search styles
		SearchBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Nord3).
			Background(Nord1).
			Padding(1, 2),
		SearchInput: lipgloss.NewStyle().
			Foreground(Nord4),
		SearchResult: lipgloss.NewStyle().
			Foreground(Nord4),
		SearchActive: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6),

		// Diagnostic styles
		DiagError: lipgloss.NewStyle().
			Foreground(Nord11).
			Underline(true),
		DiagWarning: lipgloss.NewStyle().
			Foreground(Nord13).
			Underline(true),
		DiagInfo: lipgloss.NewStyle().
			Foreground(Nord8).
			Underline(true),
		DiagHint: lipgloss.NewStyle().
			Foreground(Nord7).
			Underline(true),
		GutterError: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord11).
			PaddingRight(1),
		GutterWarn: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord13).
			PaddingRight(1),

		// Autocomplete styles
		AutocompleteItem: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord4),
		AutocompleteCursor: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6),
		AutocompleteBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Nord3).
			Background(Nord1),

		// Hover style
		HoverBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Nord3).
			Background(Nord1).
			Foreground(Nord4).
			Padding(0, 1),

		// Context menu
		ContextMenuDisabled: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord3),

		// Git panel
		GitHeader: lipgloss.NewStyle().
			Foreground(Nord8).
			Bold(true),
		GitEntry: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord4),
		GitCursor: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6),
		GitAdded: lipgloss.NewStyle().
			Foreground(Nord14),
		GitModified: lipgloss.NewStyle().
			Foreground(Nord13),
		GitDeleted: lipgloss.NewStyle().
			Foreground(Nord11),
		GitUntracked: lipgloss.NewStyle().
			Foreground(Nord3),

		// Diff view
		DiffRemoved: lipgloss.NewStyle().
			Background(lipgloss.Color("#3B2C2E")).
			Foreground(Nord4),
		DiffAdded: lipgloss.NewStyle().
			Background(lipgloss.Color("#2E3B2E")).
			Foreground(Nord4),
		DiffEmpty: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord3),
		DiffGutter: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord3),
		DiffBorder: lipgloss.NewStyle().
			Foreground(Nord3),
		DiffHunkHeader: lipgloss.NewStyle().
			Foreground(Nord8).
			Bold(true),

		// Sidebar tabs
		SidebarTabActive: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord8).
			Bold(true).
			Padding(0, 1),
		SidebarTabInactive: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord3).
			Padding(0, 1),

		// Git action buttons & sections
		GitActionButton: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6).
			Padding(0, 1),
		GitSectionHeader: lipgloss.NewStyle().
			Foreground(Nord8).
			Bold(true),
		GitBranch: lipgloss.NewStyle().
			Foreground(Nord15).
			Bold(true),
		GitCommitInput: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord4),
	}
}
