package ui

import (
	"image/color"
	"math"

	"charm.land/bubbles/v2/textinput"
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

// Theme is an immutable, small handle to the Lipgloss styles used by the UI.
//
// Components store themes by value. Keeping the styles behind a shared pointer
// prevents a single cursor movement from copying tens of KiB of style state
// through every nested editor overlay. Theme constructors allocate a fresh
// immutable themeStyles graph; copied Theme values deliberately share it.
type Theme struct {
	*themeStyles
}

// ApplyTextInputTheme gives Bubbles inputs explicit foregrounds so light
// Teak themes remain readable even when the terminal profile itself is dark.
func ApplyTextInputTheme(input *textinput.Model, theme Theme) {
	styles := input.Styles()
	textStyle := lipgloss.NewStyle().Foreground(theme.Editor.GetForeground())
	mutedStyle := lipgloss.NewStyle().Foreground(theme.Gutter.GetForeground())
	accentStyle := lipgloss.NewStyle().Foreground(theme.PromptAccent.GetForeground())
	styles.Focused.Text = textStyle
	styles.Focused.Placeholder = mutedStyle
	styles.Focused.Suggestion = mutedStyle
	styles.Focused.Prompt = accentStyle
	styles.Blurred.Text = textStyle
	styles.Blurred.Placeholder = mutedStyle
	styles.Blurred.Suggestion = mutedStyle
	styles.Blurred.Prompt = mutedStyle
	styles.Cursor.Color = theme.PromptAccent.GetForeground()
	input.SetStyles(styles)
}

// ThemeVariant identifies whether a theme is intended for a dark or light
// terminal background.
type ThemeVariant string

const (
	ThemeDark  ThemeVariant = "dark"
	ThemeLight ThemeVariant = "light"
)

// ThemeOption describes a theme available to the UI. The catalog is kept in
// display order so settings and other consumers present the same choices.
type ThemeOption struct {
	ID          string
	Name        string
	Variant     ThemeVariant
	Constructor func() Theme
}

// themeStyles is private to keep construction centralized. Theme values are
// treated as immutable after construction; callers must not assign to promoted
// style fields because copied handles intentionally share this backing graph.
type themeStyles struct {
	Editor             lipgloss.Style
	Gutter             lipgloss.Style
	GutterActive       lipgloss.Style
	Selection          lipgloss.Style
	SecondarySelection lipgloss.Style
	FindMatch          lipgloss.Style
	FindMatchCurrent   lipgloss.Style
	CursorLine         lipgloss.Style
	StatusBar          lipgloss.Style
	StatusText         lipgloss.Style
	HelpBorder         lipgloss.Style
	HelpTitle          lipgloss.Style
	HelpKey            lipgloss.Style
	TreeEntry          lipgloss.Style
	TreeCursor         lipgloss.Style
	TreeBorder         lipgloss.Style

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
	GitActionButton   lipgloss.Style
	GitCommitButton   lipgloss.Style
	GitPushPullButton lipgloss.Style
	GitSectionHeader  lipgloss.Style
	GitBranch         lipgloss.Style
	GitCommitInput    lipgloss.Style

	// Replace button
	ReplaceButton lipgloss.Style

	// Scrollbar
	ScrollTrack lipgloss.Style
	ScrollThumb lipgloss.Style

	// Agent panel
	AgentHeader        lipgloss.Style
	AgentUserMsg       lipgloss.Style
	AgentBorder        lipgloss.Style
	AgentBorderFocused lipgloss.Style
	AgentInput         lipgloss.Style
	AgentToolCall      lipgloss.Style
	AgentPermission    lipgloss.Style

	// Debug/Breakpoint styles (pre-cached for gutter rendering)
	BreakpointActive   lipgloss.Style
	BreakpointDisabled lipgloss.Style
	ExecLineMarker     lipgloss.Style
	FoldCollapsed      lipgloss.Style
	FoldExpanded       lipgloss.Style

	IndentGuide  lipgloss.Style
	TrailingWS   lipgloss.Style
	Ruler        lipgloss.Style
	GitGutterAdd lipgloss.Style
	GitGutterMod lipgloss.Style
	GitGutterDel lipgloss.Style
	PromptAccent lipgloss.Style
	PromptMuted  lipgloss.Style
	PromptDanger lipgloss.Style

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

var themeOptions = []ThemeOption{
	{ID: "nord", Name: "Nord", Variant: ThemeDark, Constructor: NordTheme},
	{ID: "dracula", Name: "Dracula", Variant: ThemeDark, Constructor: DraculaTheme},
	{ID: "catppuccin", Name: "Catppuccin Mocha", Variant: ThemeDark, Constructor: CatppuccinTheme},
	{ID: "solarized-dark", Name: "Solarized Dark", Variant: ThemeDark, Constructor: SolarizedDarkTheme},
	{ID: "one-dark", Name: "One Dark", Variant: ThemeDark, Constructor: OneDarkTheme},
	{ID: "github-dark", Name: "GitHub Dark", Variant: ThemeDark, Constructor: GitHubDarkTheme},
	{ID: "github-light", Name: "GitHub Light", Variant: ThemeLight, Constructor: GitHubLightTheme},
	{ID: "tokyo-night", Name: "Tokyo Night", Variant: ThemeDark, Constructor: TokyoNightTheme},
	{ID: "ayu-mirage", Name: "Ayu Mirage", Variant: ThemeDark, Constructor: AyuMirageTheme},
	{ID: "solarized-light", Name: "Solarized Light", Variant: ThemeLight, Constructor: SolarizedLightTheme},
	{ID: "catppuccin-latte", Name: "Catppuccin Latte", Variant: ThemeLight, Constructor: CatppuccinLatteTheme},
	{ID: "gruvbox-dark", Name: "Gruvbox Dark", Variant: ThemeDark, Constructor: GruvboxDarkTheme},
	{ID: "monokai", Name: "Monokai", Variant: ThemeDark, Constructor: MonokaiTheme},
	{ID: "night-owl", Name: "Night Owl", Variant: ThemeDark, Constructor: NightOwlTheme},
	{ID: "material-palenight", Name: "Material Palenight", Variant: ThemeDark, Constructor: MaterialPalenightTheme},
}

// ThemeOptions returns a copy of the supported themes in display order.
func ThemeOptions() []ThemeOption {
	return append([]ThemeOption(nil), themeOptions...)
}

// ThemeIDs returns a copy of the supported theme IDs in display order.
func ThemeIDs() []string {
	ids := make([]string, len(themeOptions))
	for i, option := range themeOptions {
		ids[i] = option.ID
	}
	return ids
}

// HasTheme reports whether name is a supported theme ID.
func HasTheme(name string) bool {
	for _, option := range themeOptions {
		if option.ID == name {
			return true
		}
	}
	return false
}

// ThemeByName returns a theme by name string. Falls back to Nord if unknown.
func ThemeByName(name string) Theme {
	for _, option := range themeOptions {
		if option.ID == name {
			return option.Constructor()
		}
	}
	return NordTheme()
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
	return Theme{themeStyles: &themeStyles{
		Editor: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord4),
		Gutter: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(readableTextOnColor(Nord0, Nord3, Nord4)).
			PaddingRight(1),
		GutterActive: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(Nord4).
			PaddingRight(1).
			Bold(true),
		Selection: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6),
		SecondarySelection: lipgloss.NewStyle().
			Background(Nord10).
			Foreground(readableTextOnColor(Nord10, Nord6)),
		FindMatch: lipgloss.NewStyle().
			Background(Nord3).
			Foreground(Nord6),
		FindMatchCurrent: lipgloss.NewStyle().
			Background(Nord13).
			Foreground(Nord0),
		CursorLine: lipgloss.NewStyle().
			Background(Nord1),
		StatusBar: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord4),
		StatusText: lipgloss.NewStyle().
			Background(Nord10).
			Foreground(textOnColor(Nord10)).
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
			Foreground(readableTextOnColor(Nord0, Nord3, Nord4)).
			Padding(0, 1),
		TabCloseActive: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord4),
		TabCloseInactive: lipgloss.NewStyle().
			Background(Nord0).
			Foreground(readableTextOnColor(Nord0, Nord3, Nord4)),
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
			Foreground(readableTextOnColor(Nord0, Nord3, Nord4)),
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
			Foreground(readableTextOnColor(Nord0, Nord3, Nord4)).
			Padding(0, 1),

		// Git action buttons & sections
		GitActionButton: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord6).
			Padding(0, 1),
		GitCommitButton: lipgloss.NewStyle().
			Background(Nord14).
			Foreground(Nord0).
			Padding(0, 1).
			Bold(true),
		GitPushPullButton: lipgloss.NewStyle().
			Background(Nord10).
			Foreground(textOnColor(Nord10)).
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

		// Agent panel
		AgentHeader: lipgloss.NewStyle().
			Foreground(Nord8).
			Bold(true),
		AgentUserMsg: lipgloss.NewStyle().
			Foreground(Nord8).
			Bold(true),
		AgentBorder: lipgloss.NewStyle().
			Foreground(Nord3),
		AgentBorderFocused: lipgloss.NewStyle().
			Foreground(Nord8),
		AgentInput: lipgloss.NewStyle().
			Background(Nord1).
			Foreground(Nord4),
		AgentToolCall: lipgloss.NewStyle().
			Foreground(Nord4),
		AgentPermission: lipgloss.NewStyle().
			Foreground(Nord12),

		// Debug/Breakpoint styles (pre-cached for gutter rendering)
		BreakpointActive: lipgloss.NewStyle().
			Foreground(Nord11), // red
		BreakpointDisabled: lipgloss.NewStyle().
			Foreground(Nord3), // grey
		ExecLineMarker: lipgloss.NewStyle().
			Background(Nord3).
			Foreground(Nord13), // yellow on dark
		FoldCollapsed: lipgloss.NewStyle().
			Foreground(Nord13), // yellow
		FoldExpanded: lipgloss.NewStyle().
			Foreground(Nord3), // dim
		IndentGuide: lipgloss.NewStyle().
			Foreground(Nord3),
		TrailingWS: lipgloss.NewStyle().
			Background(Nord11).
			Foreground(Nord6),
		Ruler: lipgloss.NewStyle().
			Background(Nord2).
			Foreground(Nord3),
		GitGutterAdd: lipgloss.NewStyle().
			Foreground(Nord14),
		GitGutterMod: lipgloss.NewStyle().
			Foreground(Nord13),
		GitGutterDel: lipgloss.NewStyle().
			Foreground(Nord11),
		PromptAccent: lipgloss.NewStyle().
			Foreground(Nord8).
			Bold(true),
		PromptMuted: lipgloss.NewStyle().
			Foreground(Nord4),
		PromptDanger: lipgloss.NewStyle().
			Foreground(Nord11),

		// Syntax highlighting
		SyntaxKeyword:   Nord9,
		SyntaxFunction:  Nord8,
		SyntaxString:    Nord14,
		SyntaxNumber:    Nord15,
		SyntaxComment:   readableTextOnColor(Nord0, Nord3, Nord4),
		SyntaxType:      Nord7,
		SyntaxOperator:  Nord9,
		SyntaxTag:       Nord9,
		SyntaxAttribute: Nord8,
	}}
}

// palette holds the base colors for building a theme.
type palette struct {
	bg0, bg1, bg2, bg3   color.Color
	fg0, fg1, fg2        color.Color
	red, orange, yellow  color.Color
	green, cyan, blue    color.Color
	purple               color.Color
	keyword, function    color.Color
	str, number, comment color.Color
	typ, operator, tag   color.Color
	attribute            color.Color
	diffRemovedBg        color.Color
	diffAddedBg          color.Color
	accent               color.Color
}

// textOnColor chooses the higher-contrast black or white text for a filled
// control, using relative sRGB luminance. A theme's normal foreground can be
// unreadable on its accent color even when it works on the editor background.
func textOnColor(background color.Color) color.Color {
	luminance := relativeLuminance(background)
	if (luminance+0.05)/0.05 >= 1.05/(luminance+0.05) {
		return color.Black
	}
	return color.White
}

// Preserve the palette's text unless its fill makes that text hard to read.
func readableTextOnColor(background color.Color, candidates ...color.Color) color.Color {
	bg := relativeLuminance(background)
	for _, candidate := range candidates {
		fg := relativeLuminance(candidate)
		if (max(fg, bg)+0.05)/(min(fg, bg)+0.05) >= 4.5 {
			return candidate
		}
	}
	return textOnColor(background)
}

func relativeLuminance(c color.Color) float64 {
	linear := func(v uint32) float64 {
		x := float64(v) / 65535
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	r, g, b, _ := c.RGBA()
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

func buildTheme(p palette) Theme {
	onBlue := textOnColor(p.blue)
	return Theme{themeStyles: &themeStyles{
		Editor:              lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0),
		Gutter:              lipgloss.NewStyle().Background(p.bg0).Foreground(readableTextOnColor(p.bg0, p.bg3, p.fg1)).PaddingRight(1),
		GutterActive:        lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0).PaddingRight(1).Bold(true),
		Selection:           lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		SecondarySelection:  lipgloss.NewStyle().Background(p.bg3).Foreground(readableTextOnColor(p.bg3, p.fg1)),
		FindMatch:           lipgloss.NewStyle().Background(p.bg3).Foreground(readableTextOnColor(p.bg3, p.fg0)),
		FindMatchCurrent:    lipgloss.NewStyle().Background(p.yellow).Foreground(readableTextOnColor(p.yellow, p.bg0)),
		CursorLine:          lipgloss.NewStyle().Background(p.bg1),
		StatusBar:           lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		StatusText:          lipgloss.NewStyle().Background(p.blue).Foreground(onBlue).Padding(0, 1),
		HelpBorder:          lipgloss.NewStyle().Background(p.bg1).Foreground(p.bg3),
		HelpTitle:           lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		HelpKey:             lipgloss.NewStyle().Foreground(p.yellow),
		TreeEntry:           lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0),
		TreeCursor:          lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		TreeBorder:          lipgloss.NewStyle().Foreground(p.bg3),
		TabActive:           lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg2).Padding(0, 1).Bold(true),
		TabInactive:         lipgloss.NewStyle().Background(p.bg0).Foreground(readableTextOnColor(p.bg0, p.bg3, p.fg1)).Padding(0, 1),
		TabCloseActive:      lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		TabCloseInactive:    lipgloss.NewStyle().Background(p.bg0).Foreground(readableTextOnColor(p.bg0, p.bg3, p.fg1)),
		TabBar:              lipgloss.NewStyle().Background(p.bg0),
		SearchBox:           lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.bg3).Background(p.bg1).Padding(1, 2),
		SearchInput:         lipgloss.NewStyle().Foreground(p.fg0),
		SearchResult:        lipgloss.NewStyle().Foreground(p.fg0),
		SearchActive:        lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		DiagError:           lipgloss.NewStyle().Foreground(p.red).Underline(true),
		DiagWarning:         lipgloss.NewStyle().Foreground(p.yellow).Underline(true),
		DiagInfo:            lipgloss.NewStyle().Foreground(p.cyan).Underline(true),
		DiagHint:            lipgloss.NewStyle().Foreground(p.green).Underline(true),
		GutterError:         lipgloss.NewStyle().Background(p.bg0).Foreground(p.red).PaddingRight(1),
		GutterWarn:          lipgloss.NewStyle().Background(p.bg0).Foreground(p.yellow).PaddingRight(1),
		AutocompleteItem:    lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		AutocompleteCursor:  lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		AutocompleteBox:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.bg3).Background(p.bg1),
		HoverBox:            lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.bg3).Background(p.bg1).Foreground(p.fg0).Padding(0, 1),
		BracketMatch:        lipgloss.NewStyle().Background(p.bg2).Foreground(p.cyan),
		ContextMenuDisabled: lipgloss.NewStyle().Background(p.bg1).Foreground(p.bg3),
		GitHeader:           lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		GitEntry:            lipgloss.NewStyle().Background(p.bg0).Foreground(p.fg0),
		GitCursor:           lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2),
		GitAdded:            lipgloss.NewStyle().Foreground(p.green),
		GitModified:         lipgloss.NewStyle().Foreground(p.yellow),
		GitDeleted:          lipgloss.NewStyle().Foreground(p.red),
		GitUntracked:        lipgloss.NewStyle().Foreground(p.bg3),
		DiffRemoved:         lipgloss.NewStyle().Background(p.diffRemovedBg).Foreground(readableTextOnColor(p.diffRemovedBg, p.fg0)),
		DiffAdded:           lipgloss.NewStyle().Background(p.diffAddedBg).Foreground(readableTextOnColor(p.diffAddedBg, p.fg0)),
		DiffEmpty:           lipgloss.NewStyle().Background(p.bg1).Foreground(p.bg3),
		DiffGutter:          lipgloss.NewStyle().Background(p.bg0).Foreground(readableTextOnColor(p.bg0, p.bg3, p.fg1)),
		DiffBorder:          lipgloss.NewStyle().Foreground(p.bg3),
		DiffHunkHeader:      lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		SidebarTabActive:    lipgloss.NewStyle().Background(p.bg1).Foreground(p.cyan).Bold(true).Padding(0, 1),
		SidebarTabInactive:  lipgloss.NewStyle().Background(p.bg0).Foreground(readableTextOnColor(p.bg0, p.bg3, p.fg1)).Padding(0, 1),
		GitActionButton:     lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2).Padding(0, 1),
		GitCommitButton:     lipgloss.NewStyle().Background(p.green).Foreground(readableTextOnColor(p.green, p.bg0)).Padding(0, 1).Bold(true),
		GitPushPullButton:   lipgloss.NewStyle().Background(p.blue).Foreground(onBlue).Padding(0, 1),
		GitSectionHeader:    lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		GitBranch:           lipgloss.NewStyle().Foreground(p.purple).Bold(true),
		GitCommitInput:      lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		ReplaceButton:       lipgloss.NewStyle().Background(p.bg2).Foreground(p.fg2).Padding(0, 1),
		ScrollTrack:         lipgloss.NewStyle().Background(p.bg0).Foreground(p.bg1),
		ScrollThumb:         lipgloss.NewStyle().Background(p.bg3).Foreground(p.bg3),
		AgentHeader:         lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		AgentUserMsg:        lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		AgentBorder:         lipgloss.NewStyle().Foreground(p.bg3),
		AgentBorderFocused:  lipgloss.NewStyle().Foreground(p.cyan),
		AgentInput:          lipgloss.NewStyle().Background(p.bg1).Foreground(p.fg0),
		AgentToolCall:       lipgloss.NewStyle().Foreground(p.fg0),
		AgentPermission:     lipgloss.NewStyle().Foreground(p.orange),
		BreakpointActive:    lipgloss.NewStyle().Foreground(p.red),
		BreakpointDisabled:  lipgloss.NewStyle().Foreground(p.bg3),
		ExecLineMarker:      lipgloss.NewStyle().Background(p.bg3).Foreground(p.yellow),
		FoldCollapsed:       lipgloss.NewStyle().Foreground(p.yellow),
		FoldExpanded:        lipgloss.NewStyle().Foreground(p.bg3),
		IndentGuide:         lipgloss.NewStyle().Foreground(p.bg3),
		TrailingWS:          lipgloss.NewStyle().Background(p.red).Foreground(p.fg2),
		Ruler:               lipgloss.NewStyle().Background(p.bg2).Foreground(p.bg3),
		GitGutterAdd:        lipgloss.NewStyle().Foreground(p.green),
		GitGutterMod:        lipgloss.NewStyle().Foreground(p.yellow),
		GitGutterDel:        lipgloss.NewStyle().Foreground(p.red),
		PromptAccent:        lipgloss.NewStyle().Foreground(p.cyan).Bold(true),
		PromptMuted:         lipgloss.NewStyle().Foreground(p.fg1),
		PromptDanger:        lipgloss.NewStyle().Foreground(p.red),
		SyntaxKeyword:       p.keyword,
		SyntaxFunction:      p.function,
		SyntaxString:        p.str,
		SyntaxNumber:        p.number,
		SyntaxComment:       readableTextOnColor(p.bg0, p.comment, p.fg1),
		SyntaxType:          p.typ,
		SyntaxOperator:      p.operator,
		SyntaxTag:           p.tag,
		SyntaxAttribute:     p.attribute,
	}}
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
		purple:  lipgloss.Color("#BD93F9"),
		keyword: lipgloss.Color("#FF79C6"), function: lipgloss.Color("#50FA7B"),
		str: lipgloss.Color("#F1FA8C"), number: lipgloss.Color("#BD93F9"),
		comment: lipgloss.Color("#6272A4"), typ: lipgloss.Color("#8BE9FD"),
		operator: lipgloss.Color("#FF79C6"), tag: lipgloss.Color("#FF79C6"),
		attribute:     lipgloss.Color("#50FA7B"),
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
		purple:  lipgloss.Color("#CBA6F7"),
		keyword: lipgloss.Color("#CBA6F7"), function: lipgloss.Color("#89B4FA"),
		str: lipgloss.Color("#A6E3A1"), number: lipgloss.Color("#FAB387"),
		comment: lipgloss.Color("#585B70"), typ: lipgloss.Color("#94E2D5"),
		operator: lipgloss.Color("#89DCEB"), tag: lipgloss.Color("#CBA6F7"),
		attribute:     lipgloss.Color("#89B4FA"),
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
		purple:  lipgloss.Color("#6C71C4"),
		keyword: lipgloss.Color("#859900"), function: lipgloss.Color("#268BD2"),
		str: lipgloss.Color("#2AA198"), number: lipgloss.Color("#D33682"),
		comment: lipgloss.Color("#586E75"), typ: lipgloss.Color("#B58900"),
		operator: lipgloss.Color("#859900"), tag: lipgloss.Color("#268BD2"),
		attribute:     lipgloss.Color("#B58900"),
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
		purple:  lipgloss.Color("#C678DD"),
		keyword: lipgloss.Color("#C678DD"), function: lipgloss.Color("#61AFEF"),
		str: lipgloss.Color("#98C379"), number: lipgloss.Color("#D19A66"),
		comment: lipgloss.Color("#5C6370"), typ: lipgloss.Color("#E5C07B"),
		operator: lipgloss.Color("#56B6C2"), tag: lipgloss.Color("#E06C75"),
		attribute:     lipgloss.Color("#D19A66"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#61AFEF"),
	})
}

// GitHubDarkTheme returns the GitHub Dark Default palette.
func GitHubDarkTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#0D1117"), bg1: lipgloss.Color("#161B22"),
		bg2: lipgloss.Color("#21262D"), bg3: lipgloss.Color("#484F58"),
		fg0: lipgloss.Color("#C9D1D9"), fg1: lipgloss.Color("#8B949E"),
		fg2: lipgloss.Color("#F0F6FC"),
		red: lipgloss.Color("#F85149"), orange: lipgloss.Color("#DB6D28"),
		yellow: lipgloss.Color("#D29922"), green: lipgloss.Color("#3FB950"),
		cyan: lipgloss.Color("#39C5CF"), blue: lipgloss.Color("#58A6FF"),
		purple:  lipgloss.Color("#BC8CFF"),
		keyword: lipgloss.Color("#FF7B72"), function: lipgloss.Color("#D2A8FF"),
		str: lipgloss.Color("#A5D6FF"), number: lipgloss.Color("#79C0FF"),
		comment: lipgloss.Color("#8B949E"), typ: lipgloss.Color("#FFA657"),
		operator: lipgloss.Color("#FF7B72"), tag: lipgloss.Color("#7EE787"),
		attribute:     lipgloss.Color("#79C0FF"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#58A6FF"),
	})
}

// GitHubLightTheme returns the GitHub Light Default palette.
func GitHubLightTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#FFFFFF"), bg1: lipgloss.Color("#F6F8FA"),
		bg2: lipgloss.Color("#EAECEF"), bg3: lipgloss.Color("#8C959F"),
		fg0: lipgloss.Color("#24292F"), fg1: lipgloss.Color("#57606A"),
		fg2: lipgloss.Color("#1F2328"),
		red: lipgloss.Color("#CF222E"), orange: lipgloss.Color("#BC4C00"),
		yellow: lipgloss.Color("#9A6700"), green: lipgloss.Color("#1A7F37"),
		cyan: lipgloss.Color("#0A7B83"), blue: lipgloss.Color("#0969DA"),
		purple:  lipgloss.Color("#8250DF"),
		keyword: lipgloss.Color("#CF222E"), function: lipgloss.Color("#8250DF"),
		str: lipgloss.Color("#0A3069"), number: lipgloss.Color("#0550AE"),
		comment: lipgloss.Color("#57606A"), typ: lipgloss.Color("#953800"),
		operator: lipgloss.Color("#CF222E"), tag: lipgloss.Color("#116329"),
		attribute:     lipgloss.Color("#0550AE"),
		diffRemovedBg: lipgloss.Color("#FFEBE9"), diffAddedBg: lipgloss.Color("#DAFBE1"),
		accent: lipgloss.Color("#0969DA"),
	})
}

// TokyoNightTheme returns the Tokyo Night Storm-inspired palette.
func TokyoNightTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#1A1B26"), bg1: lipgloss.Color("#16161E"),
		bg2: lipgloss.Color("#292E42"), bg3: lipgloss.Color("#565F89"),
		fg0: lipgloss.Color("#A9B1D6"), fg1: lipgloss.Color("#787C99"),
		fg2: lipgloss.Color("#C0CAF5"),
		red: lipgloss.Color("#F7768E"), orange: lipgloss.Color("#FF9E64"),
		yellow: lipgloss.Color("#E0AF68"), green: lipgloss.Color("#73DACA"),
		cyan: lipgloss.Color("#7DCFFF"), blue: lipgloss.Color("#7AA2F7"),
		purple:  lipgloss.Color("#BB9AF7"),
		keyword: lipgloss.Color("#BB9AF7"), function: lipgloss.Color("#7AA2F7"),
		str: lipgloss.Color("#9ECE6A"), number: lipgloss.Color("#FF9E64"),
		comment: lipgloss.Color("#565F89"), typ: lipgloss.Color("#2AC3DE"),
		operator: lipgloss.Color("#89DDFF"), tag: lipgloss.Color("#F7768E"),
		attribute:     lipgloss.Color("#E0AF68"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#7AA2F7"),
	})
}

// AyuMirageTheme returns the Ayu Mirage palette.
func AyuMirageTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#1F2430"), bg1: lipgloss.Color("#242936"),
		bg2: lipgloss.Color("#343E4F"), bg3: lipgloss.Color("#5C6773"),
		fg0: lipgloss.Color("#CBCCC6"), fg1: lipgloss.Color("#A3A6A4"),
		fg2: lipgloss.Color("#E6E1CF"),
		red: lipgloss.Color("#F28779"), orange: lipgloss.Color("#FFAD66"),
		yellow: lipgloss.Color("#FFCC66"), green: lipgloss.Color("#D5FF80"),
		cyan: lipgloss.Color("#95E6CB"), blue: lipgloss.Color("#80D4FF"),
		purple:  lipgloss.Color("#D4BFFF"),
		keyword: lipgloss.Color("#FFAD66"), function: lipgloss.Color("#FFD580"),
		str: lipgloss.Color("#D5FF80"), number: lipgloss.Color("#DFBFFF"),
		comment: lipgloss.Color("#5C6773"), typ: lipgloss.Color("#73D0FF"),
		operator: lipgloss.Color("#F29E74"), tag: lipgloss.Color("#5CCFE6"),
		attribute:     lipgloss.Color("#FFCC66"),
		diffRemovedBg: lipgloss.Color("#3B2C2E"), diffAddedBg: lipgloss.Color("#2E3B2E"),
		accent: lipgloss.Color("#FFCC66"),
	})
}

// SolarizedLightTheme returns the canonical Solarized Light palette.
func SolarizedLightTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#FDF6E3"), bg1: lipgloss.Color("#EEE8D5"),
		bg2: lipgloss.Color("#E0DBC8"), bg3: lipgloss.Color("#93A1A1"),
		fg0: lipgloss.Color("#657B83"), fg1: lipgloss.Color("#586E75"),
		fg2: lipgloss.Color("#002B36"),
		red: lipgloss.Color("#DC322F"), orange: lipgloss.Color("#CB4B16"),
		yellow: lipgloss.Color("#B58900"), green: lipgloss.Color("#859900"),
		cyan: lipgloss.Color("#2AA198"), blue: lipgloss.Color("#268BD2"),
		purple:  lipgloss.Color("#6C71C4"),
		keyword: lipgloss.Color("#859900"), function: lipgloss.Color("#268BD2"),
		str: lipgloss.Color("#2AA198"), number: lipgloss.Color("#D33682"),
		comment: lipgloss.Color("#93A1A1"), typ: lipgloss.Color("#B58900"),
		operator: lipgloss.Color("#859900"), tag: lipgloss.Color("#268BD2"),
		attribute:     lipgloss.Color("#B58900"),
		diffRemovedBg: lipgloss.Color("#FDECEC"), diffAddedBg: lipgloss.Color("#EAF6EA"),
		accent: lipgloss.Color("#268BD2"),
	})
}

// CatppuccinLatteTheme returns the Catppuccin Latte palette.
func CatppuccinLatteTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#EFF1F5"), bg1: lipgloss.Color("#E6E9EF"),
		bg2: lipgloss.Color("#DCE0E8"), bg3: lipgloss.Color("#9CA0B0"),
		fg0: lipgloss.Color("#4C4F69"), fg1: lipgloss.Color("#6C6F85"),
		fg2: lipgloss.Color("#1E1E2E"),
		red: lipgloss.Color("#D20F39"), orange: lipgloss.Color("#FE640B"),
		yellow: lipgloss.Color("#DF8E1D"), green: lipgloss.Color("#40A02B"),
		cyan: lipgloss.Color("#179299"), blue: lipgloss.Color("#1E66F5"),
		purple:  lipgloss.Color("#8839EF"),
		keyword: lipgloss.Color("#8839EF"), function: lipgloss.Color("#1E66F5"),
		str: lipgloss.Color("#40A02B"), number: lipgloss.Color("#FE640B"),
		comment: lipgloss.Color("#9CA0B0"), typ: lipgloss.Color("#179299"),
		operator: lipgloss.Color("#04A5E5"), tag: lipgloss.Color("#D20F39"),
		attribute:     lipgloss.Color("#1E66F5"),
		diffRemovedBg: lipgloss.Color("#FDEBEC"), diffAddedBg: lipgloss.Color("#E8F5E4"),
		accent: lipgloss.Color("#1E66F5"),
	})
}

// GruvboxDarkTheme returns the classic Gruvbox Dark palette with readable UI
// neutrals for terminal chrome.
func GruvboxDarkTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#282828"), bg1: lipgloss.Color("#32302F"),
		bg2: lipgloss.Color("#504945"), bg3: lipgloss.Color("#A89984"),
		fg0: lipgloss.Color("#EBDBB2"), fg1: lipgloss.Color("#D5C4A1"),
		fg2: lipgloss.Color("#FBF1C7"),
		red: lipgloss.Color("#FB4934"), orange: lipgloss.Color("#FE8019"),
		yellow: lipgloss.Color("#FABD2F"), green: lipgloss.Color("#B8BB26"),
		cyan: lipgloss.Color("#8EC07C"), blue: lipgloss.Color("#83A598"),
		purple:  lipgloss.Color("#D3869B"),
		keyword: lipgloss.Color("#FB4934"), function: lipgloss.Color("#B8BB26"),
		str: lipgloss.Color("#B8BB26"), number: lipgloss.Color("#D3869B"),
		comment: lipgloss.Color("#A89984"), typ: lipgloss.Color("#FABD2F"),
		operator: lipgloss.Color("#FE8019"), tag: lipgloss.Color("#83A598"),
		attribute:     lipgloss.Color("#8EC07C"),
		diffRemovedBg: lipgloss.Color("#442E2D"), diffAddedBg: lipgloss.Color("#334032"),
		accent: lipgloss.Color("#83A598"),
	})
}

// MonokaiTheme returns the classic open-source Monokai palette bundled by
// Code OSS, not the separate commercial Monokai Pro product.
func MonokaiTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#272822"), bg1: lipgloss.Color("#2D2E27"),
		bg2: lipgloss.Color("#49483E"), bg3: lipgloss.Color("#A09F8D"),
		fg0: lipgloss.Color("#F8F8F2"), fg1: lipgloss.Color("#DADACF"),
		fg2: lipgloss.Color("#FFFFFF"),
		red: lipgloss.Color("#F92672"), orange: lipgloss.Color("#FD971F"),
		yellow: lipgloss.Color("#E6DB74"), green: lipgloss.Color("#A6E22E"),
		cyan: lipgloss.Color("#66D9EF"), blue: lipgloss.Color("#66D9EF"),
		purple:  lipgloss.Color("#AE81FF"),
		keyword: lipgloss.Color("#F92672"), function: lipgloss.Color("#A6E22E"),
		str: lipgloss.Color("#E6DB74"), number: lipgloss.Color("#AE81FF"),
		comment: lipgloss.Color("#A09F8D"), typ: lipgloss.Color("#66D9EF"),
		operator: lipgloss.Color("#F92672"), tag: lipgloss.Color("#F92672"),
		attribute:     lipgloss.Color("#A6E22E"),
		diffRemovedBg: lipgloss.Color("#3F2830"), diffAddedBg: lipgloss.Color("#303A27"),
		accent: lipgloss.Color("#F92672"),
	})
}

// NightOwlTheme returns the accessible dark Night Owl palette.
func NightOwlTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#011627"), bg1: lipgloss.Color("#0B253A"),
		bg2: lipgloss.Color("#12344D"), bg3: lipgloss.Color("#8BADC1"),
		fg0: lipgloss.Color("#D6DEEB"), fg1: lipgloss.Color("#B3C5D3"),
		fg2: lipgloss.Color("#FFFFFF"),
		red: lipgloss.Color("#EF5350"), orange: lipgloss.Color("#F78C6C"),
		yellow: lipgloss.Color("#ECC48D"), green: lipgloss.Color("#ADDB67"),
		cyan: lipgloss.Color("#7FDBCA"), blue: lipgloss.Color("#82AAFF"),
		purple:  lipgloss.Color("#C792EA"),
		keyword: lipgloss.Color("#C792EA"), function: lipgloss.Color("#82AAFF"),
		str: lipgloss.Color("#ECC48D"), number: lipgloss.Color("#F78C6C"),
		comment: lipgloss.Color("#8BADC1"), typ: lipgloss.Color("#7FDBCA"),
		operator: lipgloss.Color("#C792EA"), tag: lipgloss.Color("#7FDBCA"),
		attribute:     lipgloss.Color("#ADDB67"),
		diffRemovedBg: lipgloss.Color("#321C2B"), diffAddedBg: lipgloss.Color("#15352F"),
		accent: lipgloss.Color("#82AAFF"),
	})
}

// MaterialPalenightTheme returns the MIT-licensed Palenight palette, an open
// Material-inspired theme distinct from commercial Material Theme products.
func MaterialPalenightTheme() Theme {
	return buildTheme(palette{
		bg0: lipgloss.Color("#292D3E"), bg1: lipgloss.Color("#252938"),
		bg2: lipgloss.Color("#3A3F58"), bg3: lipgloss.Color("#959DCB"),
		fg0: lipgloss.Color("#A6ACCD"), fg1: lipgloss.Color("#C3CAE3"),
		fg2: lipgloss.Color("#FFFFFF"),
		red: lipgloss.Color("#F07178"), orange: lipgloss.Color("#F78C6C"),
		yellow: lipgloss.Color("#FFCB6B"), green: lipgloss.Color("#C3E88D"),
		cyan: lipgloss.Color("#89DDFF"), blue: lipgloss.Color("#82AAFF"),
		purple:  lipgloss.Color("#C792EA"),
		keyword: lipgloss.Color("#C792EA"), function: lipgloss.Color("#82AAFF"),
		str: lipgloss.Color("#C3E88D"), number: lipgloss.Color("#F78C6C"),
		comment: lipgloss.Color("#959DCB"), typ: lipgloss.Color("#FFCB6B"),
		operator: lipgloss.Color("#89DDFF"), tag: lipgloss.Color("#F07178"),
		attribute:     lipgloss.Color("#82AAFF"),
		diffRemovedBg: lipgloss.Color("#442B38"), diffAddedBg: lipgloss.Color("#304033"),
		accent: lipgloss.Color("#82AAFF"),
	})
}
