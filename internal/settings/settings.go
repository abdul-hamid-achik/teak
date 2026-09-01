package settings

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"teak/internal/config"
	"teak/internal/ui"
)

// SettingType represents the type of a setting.
type SettingType int

const (
	TypeBool SettingType = iota
	TypeInt
	TypeString
	TypeStringList
)

// Setting represents a single configuration setting.
type Setting struct {
	ID           string
	Label        string
	Description  string
	Type         SettingType
	Value        interface{}
	DefaultValue interface{}
	Category     string
}

// Category represents a group of settings.
type Category struct {
	ID       string
	Name     string
	Settings []Setting
}

// Model represents the settings editor state.
type Model struct {
	categories       []Category
	selectedCategory int
	selectedSetting  int
	scrollY          int
	width            int
	height           int
	theme            ui.Theme
	configPath       string
	baseConfig       config.Config
	dirty            bool
	status           string
}

// GetCategories returns the default settings categories.
func GetCategories(cfg config.Config) []Category {
	return []Category{
		{
			ID:   "editor",
			Name: "Editor",
			Settings: []Setting{
				{
					ID:           "editor.tab_size",
					Label:        "Tab Size",
					Description:  "Number of spaces per tab",
					Type:         TypeInt,
					Value:        cfg.Editor.TabSize,
					DefaultValue: 4,
					Category:     "editor",
				},
				{
					ID:           "editor.format_on_save",
					Label:        "Format on Save",
					Description:  "Ask the language server to format before saving",
					Type:         TypeBool,
					Value:        cfg.Editor.FormatOnSave,
					DefaultValue: false,
					Category:     "editor",
				},
				{
					ID:           "editor.word_wrap",
					Label:        "Word Wrap",
					Description:  "Wrap long lines to the editor width",
					Type:         TypeBool,
					Value:        cfg.Editor.WordWrap,
					DefaultValue: false,
					Category:     "editor",
				},
				{
					ID:           "editor.insert_tabs",
					Label:        "Insert Tabs",
					Description:  "Insert tab character instead of spaces",
					Type:         TypeBool,
					Value:        cfg.Editor.InsertTabs,
					DefaultValue: false,
					Category:     "editor",
				},
				{
					ID:           "editor.auto_indent",
					Label:        "Auto Indent",
					Description:  "Automatically indent new lines",
					Type:         TypeBool,
					Value:        cfg.Editor.AutoIndent,
					DefaultValue: true,
					Category:     "editor",
				},
				{
					ID:           "editor.scroll_margin",
					Label:        "Scroll Margin",
					Description:  "Rows kept visible above and below the cursor while scrolling",
					Type:         TypeInt,
					Value:        cfg.Editor.ScrollMargin,
					DefaultValue: 2,
					Category:     "editor",
				},
				{
					ID:           "editor.insert_final_newline",
					Label:        "Insert Final Newline",
					Description:  "Append a newline when saving a file that does not end with one",
					Type:         TypeBool,
					Value:        cfg.Editor.InsertFinalNewline,
					DefaultValue: false,
					Category:     "editor",
				},
				{
					ID:           "editor.git_gutter",
					Label:        "Git Gutter",
					Description:  "Show added, modified, and deleted lines in the gutter",
					Type:         TypeBool,
					Value:        cfg.Editor.GitGutter,
					DefaultValue: true,
					Category:     "editor",
				},
				{
					ID:           "editor.indent_guides",
					Label:        "Indent Guides",
					Description:  "Draw faint guides at indent columns",
					Type:         TypeBool,
					Value:        cfg.Editor.IndentGuides,
					DefaultValue: true,
					Category:     "editor",
				},
				{
					ID:           "editor.highlight_trailing_whitespace",
					Label:        "Highlight Trailing Whitespace",
					Description:  "Mark spaces and tabs at the end of lines",
					Type:         TypeBool,
					Value:        cfg.Editor.HighlightTrailingWS,
					DefaultValue: true,
					Category:     "editor",
				},
				{
					ID:           "editor.ruler_column",
					Label:        "Ruler Column",
					Description:  "Draw a column guide (0 disables)",
					Type:         TypeInt,
					Value:        cfg.Editor.RulerColumn,
					DefaultValue: 0,
					Category:     "editor",
				},
			},
		},
		{
			ID:   "ui",
			Name: "User Interface",
			Settings: []Setting{
				{
					ID:           "ui.theme",
					Label:        "Theme",
					Description:  "Color theme; saved now and applied after restarting Teak",
					Type:         TypeString,
					Value:        cfg.UI.Theme,
					DefaultValue: "nord",
					Category:     "ui",
				},
				{
					ID:           "ui.show_tree",
					Label:        "Show File Tree",
					Description:  "Show file tree sidebar on startup",
					Type:         TypeBool,
					Value:        cfg.UI.ShowTree,
					DefaultValue: true,
					Category:     "ui",
				},
				{
					ID:           "ui.tree_width",
					Label:        "Tree Width",
					Description:  "Sidebar width in columns (0 keeps the default)",
					Type:         TypeInt,
					Value:        cfg.UI.TreeWidth,
					DefaultValue: 0,
					Category:     "ui",
				},
			},
		},
		{
			ID:   "lsp",
			Name: "Language Server",
			Settings: []Setting{
				{
					ID:           "lsp.config",
					Label:        "LSP Configuration",
					Description:  "LSP servers are configured in config.toml",
					Type:         TypeString,
					Value:        "Edit config file to customize",
					DefaultValue: "",
					Category:     "lsp",
				},
			},
		},
	}
}

// New creates a new settings model.
func New(theme ui.Theme, cfg config.Config, configPath string) Model {
	return Model{
		categories: GetCategories(cfg),
		theme:      theme,
		configPath: configPath,
		baseConfig: cfg,
	}
}

// SetSize sets the model dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = max(1, width)
	m.height = max(1, height)
	m.ensureSelectedVisible()
}

// SelectedCategory returns the currently selected category.
func (m *Model) SelectedCategory() *Category {
	if len(m.categories) == 0 {
		return nil
	}
	if m.selectedCategory >= len(m.categories) {
		m.selectedCategory = len(m.categories) - 1
	}
	return &m.categories[m.selectedCategory]
}

// SelectedSetting returns the currently selected setting.
func (m *Model) SelectedSetting() *Setting {
	cat := m.SelectedCategory()
	if cat == nil || len(cat.Settings) == 0 {
		return nil
	}
	if m.selectedSetting >= len(cat.Settings) {
		m.selectedSetting = len(cat.Settings) - 1
	}
	return &cat.Settings[m.selectedSetting]
}

// SelectNextCategory moves to the next category.
func (m *Model) SelectNextCategory() {
	if len(m.categories) == 0 {
		return
	}
	m.selectedCategory = (m.selectedCategory + 1) % len(m.categories)
	m.selectedSetting = 0
	m.scrollY = 0
}

// SelectPrevCategory moves to the previous category.
func (m *Model) SelectPrevCategory() {
	if len(m.categories) == 0 {
		return
	}
	m.selectedCategory = (m.selectedCategory - 1 + len(m.categories)) % len(m.categories)
	m.selectedSetting = 0
	m.scrollY = 0
}

// SelectNextSetting moves to the next setting.
func (m *Model) SelectNextSetting() {
	cat := m.SelectedCategory()
	if cat == nil {
		return
	}
	if len(cat.Settings) > 0 {
		m.selectedSetting = (m.selectedSetting + 1) % len(cat.Settings)
		m.ensureSelectedVisible()
	}
}

// SelectPrevSetting moves to the previous setting.
func (m *Model) SelectPrevSetting() {
	cat := m.SelectedCategory()
	if cat == nil {
		return
	}
	if len(cat.Settings) > 0 {
		m.selectedSetting = (m.selectedSetting - 1 + len(cat.Settings)) % len(cat.Settings)
		m.ensureSelectedVisible()
	}
}

// ToggleBoolValue toggles a boolean setting value.
func (m *Model) ToggleBoolValue() {
	setting := m.SelectedSetting()
	if setting == nil || setting.Type != TypeBool {
		return
	}
	if val, ok := setting.Value.(bool); ok {
		setting.Value = !val
		m.markDirty()
	}
}

// intSettingBounds returns the stepper range for an integer setting.
func intSettingBounds(id string) (int, int) {
	switch id {
	case "editor.tab_size":
		return 1, 8
	case "editor.scroll_margin":
		return 0, 50
	case "ui.tree_width":
		return 0, 120
	default:
		return 1, 100
	}
}

// IncrementIntValue increments an integer setting value.
func (m *Model) IncrementIntValue() {
	setting := m.SelectedSetting()
	if setting == nil || setting.Type != TypeInt {
		return
	}
	_, maxVal := intSettingBounds(setting.ID)
	if val, ok := setting.Value.(int); ok && val < maxVal {
		setting.Value = val + 1
		m.markDirty()
	}
}

// DecrementIntValue decrements an integer setting value.
func (m *Model) DecrementIntValue() {
	setting := m.SelectedSetting()
	if setting == nil || setting.Type != TypeInt {
		return
	}
	minVal, _ := intSettingBounds(setting.ID)
	if val, ok := setting.Value.(int); ok && val > minVal {
		setting.Value = val - 1
		m.markDirty()
	}
}

// ResetCurrentValue resets the current setting to its default value.
func (m *Model) ResetCurrentValue() {
	setting := m.SelectedSetting()
	if setting == nil {
		return
	}
	setting.Value = setting.DefaultValue
	m.markDirty()
}

// CycleStringValue advances a setting with a fixed, validated list of values.
// Currently the only editable string is the configured theme.
func (m *Model) CycleStringValue() {
	setting := m.SelectedSetting()
	if setting == nil || setting.ID != "ui.theme" {
		return
	}
	current, ok := setting.Value.(string)
	if !ok {
		return
	}
	themes := config.KnownThemes()
	for i, theme := range themes {
		if theme == current {
			setting.Value = themes[(i+1)%len(themes)]
			m.markDirty()
			return
		}
	}
	setting.Value = themes[0]
	m.markDirty()
}

// HandleMouseClick applies a click in Settings content coordinates (that is,
// after the containing modal border and padding). It returns true whenever the
// click lands on an interactive tab or setting row. Keeping the geometry here
// makes the rendered rows and their hit targets evolve together.
func (m *Model) HandleMouseClick(x, y int) bool {
	if y == settingsCategoryRow {
		for i := range m.categories {
			start, end := m.categoryTabBounds(i)
			if x >= start && x < end {
				m.selectedCategory = i
				m.selectedSetting = 0
				m.scrollY = 0
				return true
			}
		}
		return false
	}

	row := y - settingsFirstRow
	if row < 0 || row >= m.visibleSettings() {
		return false
	}
	idx := m.scrollY + row
	cat := m.SelectedCategory()
	if cat == nil || idx >= len(cat.Settings) {
		return false
	}
	m.selectedSetting = idx

	// A label click selects the row. The bracketed value/control region changes
	// it, which avoids accidental edits when merely navigating with the mouse.
	setting := m.SelectedSetting()
	if setting != nil && x >= m.settingControlStart(setting) {
		switch setting.Type {
		case TypeBool:
			m.ToggleBoolValue()
		case TypeInt:
			if x <= m.settingControlStart(setting)+2 {
				m.DecrementIntValue()
			} else {
				m.IncrementIntValue()
			}
		case TypeString:
			m.CycleStringValue()
		}
	}
	m.ensureSelectedVisible()
	return true
}

func (m *Model) markDirty() {
	m.dirty = true
	m.status = "Unsaved changes — press Ctrl+S to save"
}

// Dirty reports whether the displayed values differ from the last saved values.
func (m *Model) Dirty() bool { return m.dirty }

// Status returns the user-visible outcome of the last settings action.
func (m *Model) Status() string { return m.status }

// SetStatus reports a recoverable error or progress update without discarding
// the values currently being edited.
func (m *Model) SetStatus(status string) { m.status = status }

// MarkSaved records a successful persistence operation. It deliberately keeps
// the current selection and values intact.
func (m *Model) MarkSaved(cfg config.Config, status string) {
	m.baseConfig = cfg
	m.dirty = false
	m.status = status
}

// Config returns the full application configuration represented by the
// editable Settings UI. Settings not exposed by the overlay are retained from
// the configuration that opened it.
func (m *Model) Config() (config.Config, error) {
	cfg := m.baseConfig
	for _, category := range m.categories {
		for _, setting := range category.Settings {
			switch setting.ID {
			case "editor.tab_size":
				value, ok := setting.Value.(int)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid tab size setting")
				}
				cfg.Editor.TabSize = value
			case "editor.insert_tabs":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid insert tabs setting")
				}
				cfg.Editor.InsertTabs = value
			case "editor.auto_indent":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid auto indent setting")
				}
				cfg.Editor.AutoIndent = value
			case "editor.format_on_save":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid format on save setting")
				}
				cfg.Editor.FormatOnSave = value
			case "editor.word_wrap":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid word wrap setting")
				}
				cfg.Editor.WordWrap = value
			case "editor.scroll_margin":
				value, ok := setting.Value.(int)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid scroll margin setting")
				}
				cfg.Editor.ScrollMargin = value
			case "editor.insert_final_newline":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid insert final newline setting")
				}
				cfg.Editor.InsertFinalNewline = value
			case "editor.git_gutter":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid git gutter setting")
				}
				cfg.Editor.GitGutter = value
			case "editor.indent_guides":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid indent guides setting")
				}
				cfg.Editor.IndentGuides = value
			case "editor.highlight_trailing_whitespace":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid trailing whitespace setting")
				}
				cfg.Editor.HighlightTrailingWS = value
			case "editor.ruler_column":
				value, ok := setting.Value.(int)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid ruler column setting")
				}
				cfg.Editor.RulerColumn = value
			case "ui.theme":
				value, ok := setting.Value.(string)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid theme setting")
				}
				cfg.UI.Theme = value
			case "ui.show_tree":
				value, ok := setting.Value.(bool)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid show tree setting")
				}
				cfg.UI.ShowTree = value
			case "ui.tree_width":
				value, ok := setting.Value.(int)
				if !ok {
					return config.Config{}, fmt.Errorf("invalid tree width setting")
				}
				cfg.UI.TreeWidth = value
			}
		}
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

// View renders the settings UI.
func (m *Model) View() string {
	if len(m.categories) == 0 {
		return m.theme.Editor.Render("No settings available")
	}

	var sb strings.Builder

	// Title (centered)
	title := m.theme.HelpTitle.Render("Settings")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Config path hint
	configHint := fmt.Sprintf("Config: %s", m.configPath)
	if available := m.width - 6; available > 0 && lipgloss.Width(configHint) > available {
		configHint = truncateDisplay(configHint, available)
	}
	configHint = m.theme.Gutter.Render(configHint)
	sb.WriteString(configHint)
	sb.WriteString("\n\n")

	// Categories tabs
	sb.WriteString(m.renderCategoryTabs())
	sb.WriteString("\n\n")

	// Settings list with fixed height
	sb.WriteString(m.renderSettingsList(m.visibleSettings()))
	if m.status != "" {
		sb.WriteString("\n\n")
		sb.WriteString(m.theme.Gutter.Render(m.status))
	}

	return sb.String()
}

// renderCategoryTabs renders the category selection tabs.
func (m *Model) renderCategoryTabs() string {
	var tabs []string
	for i, cat := range m.categories {
		style := m.theme.SidebarTabInactive
		if i == m.selectedCategory {
			style = m.theme.SidebarTabActive
		}
		tabs = append(tabs, style.Render(cat.Name))
	}
	return strings.Join(tabs, "  ")
}

// renderSettingsList renders the settings for the current category.
func (m *Model) renderSettingsList(maxLines int) string {
	cat := m.SelectedCategory()
	if cat == nil {
		return ""
	}

	var sb strings.Builder
	startIdx := m.scrollY
	endIdx := min(startIdx+maxLines, len(cat.Settings))

	for i := startIdx; i < endIdx; i++ {
		if i > startIdx {
			sb.WriteString("\n")
		}
		sb.WriteString(m.renderSetting(&cat.Settings[i], i == m.selectedSetting))
	}

	return sb.String()
}

// renderSetting renders a single setting row.
func (m *Model) renderSetting(setting *Setting, isSelected bool) string {
	var valueStr string
	switch setting.Type {
	case TypeBool:
		if val, ok := setting.Value.(bool); ok {
			if val {
				valueStr = lipgloss.NewStyle().Foreground(ui.Nord14).Render("[✓ Enabled]")
			} else {
				valueStr = m.theme.Gutter.Render("[✗ Disabled]")
			}
		}
	case TypeInt:
		if val, ok := setting.Value.(int); ok {
			valueStr = fmt.Sprintf("[- %d +]", val)
		}
	case TypeString:
		if val, ok := setting.Value.(string); ok {
			if setting.ID == "ui.theme" {
				valueStr = "[" + val + " >]"
			} else {
				valueStr = val
			}
		}
	case TypeStringList:
		if val, ok := setting.Value.([]string); ok {
			valueStr = strings.Join(val, ", ")
		}
	}

	// Build the line
	labelStyle := m.theme.TreeEntry
	if isSelected {
		labelStyle = m.theme.TreeCursor
	}

	line := fmt.Sprintf("  %s  %s",
		labelStyle.Render(setting.Label+":"),
		m.theme.Gutter.Render(valueStr),
	)

	if isSelected {
		line = lipgloss.NewStyle().Background(ui.Nord2).Render(line)
	}

	return line
}

const (
	settingsCategoryRow = 4
	settingsFirstRow    = 6
)

func (m *Model) visibleSettings() int {
	// The outer app modal reserves two rows for its border/padding, while this
	// model needs title, config hint, categories, and a compact footer. On a
	// tiny terminal still expose one row rather than returning an invalid size.
	visible := m.height - 10
	if visible < 1 {
		return 1
	}
	return visible
}

func (m *Model) ensureSelectedVisible() {
	cat := m.SelectedCategory()
	if cat == nil || len(cat.Settings) == 0 {
		m.scrollY = 0
		return
	}
	if m.selectedSetting < 0 {
		m.selectedSetting = 0
	}
	if m.selectedSetting >= len(cat.Settings) {
		m.selectedSetting = len(cat.Settings) - 1
	}
	visible := m.visibleSettings()
	if m.selectedSetting < m.scrollY {
		m.scrollY = m.selectedSetting
	}
	if m.selectedSetting >= m.scrollY+visible {
		m.scrollY = m.selectedSetting - visible + 1
	}
	maxScroll := len(cat.Settings) - visible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollY > maxScroll {
		m.scrollY = maxScroll
	}
}

func (m *Model) categoryTabBounds(index int) (int, int) {
	start := 0
	for i, cat := range m.categories {
		style := m.theme.SidebarTabInactive
		if i == m.selectedCategory {
			style = m.theme.SidebarTabActive
		}
		width := lipgloss.Width(style.Render(cat.Name))
		if i == index {
			return start, start + width
		}
		start += width + 2
	}
	return 0, 0
}

func (m *Model) settingControlStart(setting *Setting) int {
	// Two leading spaces, the label and colon, then two spaces before the
	// bracketed control. lipgloss.Width handles labels containing non-ASCII.
	return 2 + lipgloss.Width(setting.Label) + 1 + 2
}

func truncateDisplay(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 3 {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return lipgloss.NewStyle().MaxWidth(width-3).Render(s) + "..."
}

// PreviewTOML generates a TOML preview of current settings.
func (m *Model) PreviewTOML() string {
	var sb strings.Builder

	sb.WriteString("# Teak Configuration\n")
	sb.WriteString("# Generated from Settings UI\n\n")

	// Editor section
	sb.WriteString("[editor]\n")
	for _, cat := range m.categories {
		if cat.ID == "editor" {
			for _, s := range cat.Settings {
				sb.WriteString(m.settingToTOML(s))
			}
		}
	}
	sb.WriteString("\n")

	// UI section
	sb.WriteString("[ui]\n")
	for _, cat := range m.categories {
		if cat.ID == "ui" {
			for _, s := range cat.Settings {
				sb.WriteString(m.settingToTOML(s))
			}
		}
	}
	sb.WriteString("\n")

	// LSP section (note)
	sb.WriteString("# LSP servers are configured separately\n")
	sb.WriteString("# See documentation for available options\n")

	return sb.String()
}

// settingToTOML converts a setting to TOML format.
func (m *Model) settingToTOML(setting Setting) string {
	key := strings.TrimPrefix(setting.ID, "editor.")
	key = strings.TrimPrefix(key, "ui.")
	key = strings.TrimPrefix(key, "lsp.")

	switch setting.Type {
	case TypeBool:
		if val, ok := setting.Value.(bool); ok {
			return fmt.Sprintf("%s = %v\n", key, val)
		}
	case TypeInt:
		if val, ok := setting.Value.(int); ok {
			return fmt.Sprintf("%s = %d\n", key, val)
		}
	case TypeString:
		if val, ok := setting.Value.(string); ok {
			return fmt.Sprintf("%s = %q\n", key, val)
		}
	case TypeStringList:
		if val, ok := setting.Value.([]string); ok {
			quoted := make([]string, len(val))
			for i, item := range val {
				quoted[i] = strconv.Quote(item)
			}
			return fmt.Sprintf("%s = [%s]\n", key, strings.Join(quoted, ", "))
		}
	}
	return ""
}

// CategoryCount returns the number of categories.
func (m *Model) CategoryCount() int {
	return len(m.categories)
}

// SettingCount returns the number of settings in the current category.
func (m *Model) SettingCount() int {
	cat := m.SelectedCategory()
	if cat == nil {
		return 0
	}
	return len(cat.Settings)
}

// ConfigPath returns the configuration file path.
func (m *Model) ConfigPath() string {
	return m.configPath
}
