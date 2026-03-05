package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

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

	// Bracket matching
	BracketMatch lipgloss.Style

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

	// Replace button
	ReplaceButton lipgloss.Style

	// Scrollbar
	ScrollTrack lipgloss.Style
	ScrollThumb lipgloss.Style

	// Syntax highlighting colors
	SyntaxKeyword   color.Color
	SyntaxFunction  color.Color
	SyntaxString    color.Color
	SyntaxNumber    color.Color
	SyntaxComment   color.Color
	SyntaxType      color.Color
	SyntaxOperator  color.Color
	SyntaxTag       color.Color
	SyntaxAttribute color.Color
}

// ThemeByName returns a theme by name string. Falls back to Nord if unknown.
func ThemeByName(name string) Theme {
	switch name {
	case "nord":
		return NordTheme()
	case "dracula":
		return DraculaTheme()
	case "catppuccin":
		return CatppuccinTheme()
	case "solarized-dark":
		return SolarizedDarkTheme()
	case "one-dark":
		return OneDarkTheme()
	default:
		return NordTheme()
	}
}

// NordTheme returns the Nord-themed styles.
func NordTheme() Theme {
	return defaultNordTheme()
}

// DefaultTheme returns the Nord-themed styles.
func DefaultTheme() Theme {
	return defaultNordTheme()
}

func defaultNordTheme() Theme {
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

		// Bracket matching
		BracketMatch: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord7),

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

		// Replace button
		ReplaceButton: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6).
			Padding(0, 1),

		// Scrollbar
		ScrollTrack: lipgloss.NewStyle().
			Background(Nord1),
		ScrollThumb: lipgloss.NewStyle().
			Background(Nord3),

		// Syntax highlighting
		SyntaxKeyword:   Nord9,
		SyntaxFunction:  Nord8,
		SyntaxString:    Nord14,
		SyntaxNumber:    Nord15,
		SyntaxComment:   Nord3,
		SyntaxType:      Nord7,
		SyntaxOperator:  Nord9,
		SyntaxTag:       Nord9,
		SyntaxAttribute: Nord8,
	}
}

// palette holds the base colors for building a theme.
type palette struct {
	bg0, bg1, bg2, bg3     color.Color
	fg0, fg1, fg2          color.Color
	red, orange, yellow    color.Color
	green, cyan, blue      color.Color
	purple                 color.Color
	keyword, function      color.Color
	str, number, comment   color.Color
	typ, operator, tag     color.Color
	attribute              color.Color
	diffRemovedBg          color.Color
	diffAddedBg            color.Color
	accent                 color.Color
}

func buildTheme(p palette) Theme {
	return Theme{
		Editor: lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0),
		Gutter: lipgloss.NewStyle().Background(p.bg0).Foreground(p.bg3).PaddingRight(1),
		GutterActive: lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0).PaddingRight(1).Bold(true),
		Selection: lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		CursorLine: lipgloss.NewStyle().Background(p.bg1),
		StatusBar: lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		StatusText: lipgloss.NewStyle().Background(p.blue).Foreground(p.fg2).Padding(0, 1),
		HelpBorder: lipgloss.NewStyle().Background(p.bg1).Foreground(p.bg3),
		HelpTitle: lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		HelpKey: lipgloss.NewStyle().Foreground(p.yellow),
		TreeEntry: lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0),
		TreeCursor: lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		TreeBorder: lipgloss.NewStyle().Foreground(p.bg3),
		TabActive: lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg2).Padding(0, 1).Bold(true),
		TabInactive: lipgloss.NewStyle().Background(p.bg0).Foreground(p.bg3).Padding(0, 1),
		TabCloseActive: lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		TabCloseInactive: lipgloss.NewStyle().Background(p.bg0).Foreground(p.bg3),
		TabBar: lipgloss.NewStyle().Background(p.bg0),
		SearchBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.bg3).Background(p.bg1).Padding(1, 2),
		SearchInput: lipgloss.NewStyle().Foreground(p.fg0),
		SearchResult: lipgloss.NewStyle().Foreground(p.fg0),
		SearchActive: lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		DiagError: lipgloss.NewStyle().Foreground(p.red).Underline(true),
		DiagWarning: lipgloss.NewStyle().Foreground(p.yellow).Underline(true),
		DiagInfo: lipgloss.NewStyle().Foreground(p.cyan).Underline(true),
		DiagHint: lipgloss.NewStyle().Foreground(p.green).Underline(true),
		GutterError: lipgloss.NewStyle().Background(p.bg0).Foreground(p.red).PaddingRight(1),
		GutterWarn: lipgloss.NewStyle().Background(p.bg0).Foreground(p.yellow).PaddingRight(1),
		AutocompleteItem: lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		AutocompleteCursor: lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		AutocompleteBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.bg3).Background(p.bg1),
		HoverBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.bg3).Background(p.bg1).Foreground(p.fg0).Padding(0, 1),
		BracketMatch: lipgloss.NewStyle().Background(p.bg2).Foreground(p.cyan),
		ContextMenuDisabled: lipgloss.NewStyle().Background(p.bg1).Foreground(p.bg3),
		GitHeader: lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		GitEntry: lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0),
		GitCursor: lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		GitAdded: lipgloss.NewStyle().Foreground(p.green),
		GitModified: lipgloss.NewStyle().Foreground(p.yellow),
		GitDeleted: lipgloss.NewStyle().Foreground(p.red),
		GitUntracked: lipgloss.NewStyle().Foreground(p.bg3),
		DiffRemoved: lipgloss.NewStyle().Background(p.diffRemovedBg).Foreground(p.fg0),
		DiffAdded: lipgloss.NewStyle().Background(p.diffAddedBg).Foreground(p.fg0),
		DiffEmpty: lipgloss.NewStyle().Background(p.bg1).Foreground(p.bg3),
		DiffGutter: lipgloss.NewStyle().Background(p.bg0).Foreground(p.bg3),
		DiffBorder: lipgloss.NewStyle().Foreground(p.bg3),
		DiffHunkHeader: lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		SidebarTabActive: lipgloss.NewStyle().Background(p.bg1).Foreground(p.cyan).Bold(true).Padding(0, 1),
		SidebarTabInactive: lipgloss.NewStyle().Background(p.bg0).Foreground(p.bg3).Padding(0, 1),
		GitActionButton: lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2).Padding(0, 1),
		GitSectionHeader: lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		GitBranch: lipgloss.NewStyle().Foreground(p.purple).Bold(true),
		GitCommitInput: lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		ReplaceButton: lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2).Padding(0, 1),
		ScrollTrack: lipgloss.NewStyle().Background(p.bg0).Foreground(p.bg1),
		ScrollThumb: lipgloss.NewStyle().Background(p.bg3).Foreground(p.bg3),
		SyntaxKeyword:   p.keyword,
		SyntaxFunction:  p.function,
		SyntaxString:    p.str,
		SyntaxNumber:    p.number,
		SyntaxComment:   p.comment,
		SyntaxType:      p.typ,
		SyntaxOperator:  p.operator,
		SyntaxTag:       p.tag,
		SyntaxAttribute: p.attribute,
	}
}

// DraculaTheme returns Dracula-themed styles.
func DraculaTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#282A36"), bg1: lipgloss.Color("#343746"),
		bg2: lipgloss.Color("#44475A"), bg3: lipgloss.Color("#6272A4"),
		fg0: lipgloss.Color("#F8F8F2"), fg1: lipgloss.Color("#E0E0E0"),
		fg2: lipgloss.Color("#F8F8F2"),
		red: lipgloss.Color("#FF5555"), orange: lipgloss.Color("#FFB86C"),
		yellow: lipgloss.Color("#F1FA8C"), green: lipgloss.Color("#50FA7B"),
		cyan: lipgloss.Color("#8BE9FD"), blue: lipgloss.Color("#6272A4"),
		purple: lipgloss.Color("#BD93F9"),
		keyword: lipgloss.Color("#FF79C6"), function: lipgloss.Color("#50FA7B"),
		str: lipgloss.Color("#F1FA8C"), number: lipgloss.Color("#BD93F9"),
		comment: lipgloss.Color("#6272A4"), typ: lipgloss.Color("#8BE9FD"),
		operator: lipgloss.Color("#FF79C6"), tag: lipgloss.Color("#FF79C6"),
		attribute: lipgloss.Color("#50FA7B"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#BD93F9"),
	})
}

// CatppuccinTheme returns Catppuccin Mocha-themed styles.
func CatppuccinTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#1E1E2E"), bg1: lipgloss.Color("#313244"),
		bg2: lipgloss.Color("#45475A"), bg3: lipgloss.Color("#585B70"),
		fg0: lipgloss.Color("#CDD6F4"), fg1: lipgloss.Color("#BAC2DE"),
		fg2: lipgloss.Color("#CDD6F4"),
		red: lipgloss.Color("#F38BA8"), orange: lipgloss.Color("#FAB387"),
		yellow: lipgloss.Color("#F9E2AF"), green: lipgloss.Color("#A6E3A1"),
		cyan: lipgloss.Color("#94E2D5"), blue: lipgloss.Color("#89B4FA"),
		purple: lipgloss.Color("#CBA6F7"),
		keyword: lipgloss.Color("#CBA6F7"), function: lipgloss.Color("#89B4FA"),
		str: lipgloss.Color("#A6E3A1"), number: lipgloss.Color("#FAB387"),
		comment: lipgloss.Color("#585B70"), typ: lipgloss.Color("#94E2D5"),
		operator: lipgloss.Color("#89DCEB"), tag: lipgloss.Color("#CBA6F7"),
		attribute: lipgloss.Color("#89B4FA"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#CBA6F7"),
	})
}

// SolarizedDarkTheme returns Solarized Dark-themed styles.
func SolarizedDarkTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#002B36"), bg1: lipgloss.Color("#073642"),
		bg2: lipgloss.Color("#1A4858"), bg3: lipgloss.Color("#586E75"),
		fg0: lipgloss.Color("#839496"), fg1: lipgloss.Color("#93A1A1"),
		fg2: lipgloss.Color("#EEE8D5"),
		red: lipgloss.Color("#DC322F"), orange: lipgloss.Color("#CB4B16"),
		yellow: lipgloss.Color("#B58900"), green: lipgloss.Color("#859900"),
		cyan: lipgloss.Color("#2AA198"), blue: lipgloss.Color("#268BD2"),
		purple: lipgloss.Color("#6C71C4"),
		keyword: lipgloss.Color("#859900"), function: lipgloss.Color("#268BD2"),
		str: lipgloss.Color("#2AA198"), number: lipgloss.Color("#D33682"),
		comment: lipgloss.Color("#586E75"), typ: lipgloss.Color("#B58900"),
		operator: lipgloss.Color("#859900"), tag: lipgloss.Color("#268BD2"),
		attribute: lipgloss.Color("#B58900"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#268BD2"),
	})
}

// OneDarkTheme returns One Dark-themed styles.
func OneDarkTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#282C34"), bg1: lipgloss.Color("#2C313A"),
		bg2: lipgloss.Color("#3E4451"), bg3: lipgloss.Color("#5C6370"),
		fg0: lipgloss.Color("#ABB2BF"), fg1: lipgloss.Color("#B6BDCA"),
		fg2: lipgloss.Color("#D7DAE0"),
		red: lipgloss.Color("#E06C75"), orange: lipgloss.Color("#D19A66"),
		yellow: lipgloss.Color("#E5C07B"), green: lipgloss.Color("#98C379"),
		cyan: lipgloss.Color("#56B6C2"), blue: lipgloss.Color("#61AFEF"),
		purple: lipgloss.Color("#C678DD"),
		keyword: lipgloss.Color("#C678DD"), function: lipgloss.Color("#61AFEF"),
		str: lipgloss.Color("#98C379"), number: lipgloss.Color("#D19A66"),
		comment: lipgloss.Color("#5C6370"), typ: lipgloss.Color("#E5C07B"),
		operator: lipgloss.Color("#56B6C2"), tag: lipgloss.Color("#E06C75"),
		attribute: lipgloss.Color("#D19A66"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#61AFEF"),
	})
}
