package settings

import (
	"fmt"
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
			},
		},
		{
			ID:   "ui",
			Name: "User Interface",
			Settings: []Setting{
				{
					ID:           "ui.theme",
					Label:        "Theme",
					Description:  "Color theme (nord, dracula, catppuccin, solarized-dark, one-dark)",
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
	}
}

// SetSize sets the model dimensions.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
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
	m.selectedCategory = (m.selectedCategory + 1) % len(m.categories)
	m.selectedSetting = 0
}

// SelectPrevCategory moves to the previous category.
func (m *Model) SelectPrevCategory() {
	m.selectedCategory = (m.selectedCategory - 1 + len(m.categories)) % len(m.categories)
	m.selectedSetting = 0
}

// SelectNextSetting moves to the next setting.
func (m *Model) SelectNextSetting() {
	cat := m.SelectedCategory()
	if cat == nil {
		return
	}
	if len(cat.Settings) > 0 {
		m.selectedSetting = (m.selectedSetting + 1) % len(cat.Settings)
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
	}
}

// IncrementIntValue increments an integer setting value.
func (m *Model) IncrementIntValue() {
	setting := m.SelectedSetting()
	if setting == nil || setting.Type != TypeInt {
		return
	}
	if val, ok := setting.Value.(int); ok {
		setting.Value = val + 1
	}
}

// DecrementIntValue decrements an integer setting value.
func (m *Model) DecrementIntValue() {
	setting := m.SelectedSetting()
	if setting == nil || setting.Type != TypeInt {
		return
	}
	if val, ok := setting.Value.(int); ok && val > 1 {
		setting.Value = val - 1
	}
}

// ResetCurrentValue resets the current setting to its default value.
func (m *Model) ResetCurrentValue() {
	setting := m.SelectedSetting()
	if setting == nil {
		return
	}
	setting.Value = setting.DefaultValue
}

// View renders the settings UI.
func (m *Model) View() string {
	if len(m.categories) == 0 {
		return m.theme.Editor.Render("No settings available")
	}

	// Fixed modal height
	modalHeight := 18

	var sb strings.Builder

	// Title (centered)
	title := m.theme.HelpTitle.Render("Settings")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	// Config path hint
	configHint := fmt.Sprintf("Config: %s", m.configPath)
	configHint = m.theme.Gutter.Render(configHint)
	sb.WriteString(configHint)
	sb.WriteString("\n\n")

	// Categories tabs
	sb.WriteString(m.renderCategoryTabs())
	sb.WriteString("\n\n")

	// Settings list with fixed height
	sb.WriteString(m.renderSettingsList(modalHeight - 8))

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
				valueStr = lipgloss.NewStyle().Foreground(ui.Nord14).Render("✓ Enabled")
			} else {
				valueStr = m.theme.Gutter.Render("✗ Disabled")
			}
		}
	case TypeInt:
		if val, ok := setting.Value.(int); ok {
			valueStr = fmt.Sprintf("%d", val)
		}
	case TypeString:
		if val, ok := setting.Value.(string); ok {
			valueStr = val
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
			return fmt.Sprintf("%s = [%q]\n", key, strings.Join(val, "\", \""))
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
