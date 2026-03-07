package settings

import (
	"strings"
	"testing"

	"teak/internal/config"
	"teak/internal/ui"
)

// TestSettingType tests SettingType constants
func TestSettingType(t *testing.T) {
	if TypeBool != 0 {
		t.Errorf("TypeBool should be 0, got %d", TypeBool)
	}
	if TypeInt != 1 {
		t.Errorf("TypeInt should be 1, got %d", TypeInt)
	}
	if TypeString != 2 {
		t.Errorf("TypeString should be 2, got %d", TypeString)
	}
	if TypeStringList != 3 {
		t.Errorf("TypeStringList should be 3, got %d", TypeStringList)
	}
}

// TestSettingStruct tests Setting struct
func TestSettingStruct(t *testing.T) {
	setting := Setting{
		ID:           "test.id",
		Label:        "Test Label",
		Description:  "Test Description",
		Type:         TypeBool,
		Value:        true,
		DefaultValue: false,
		Category:     "test",
	}

	if setting.ID != "test.id" {
		t.Errorf("Expected ID 'test.id', got %q", setting.ID)
	}
	if setting.Label != "Test Label" {
		t.Errorf("Expected Label 'Test Label', got %q", setting.Label)
	}
	if setting.Description != "Test Description" {
		t.Errorf("Expected Description 'Test Description', got %q", setting.Description)
	}
	if setting.Type != TypeBool {
		t.Errorf("Expected Type TypeBool, got %d", setting.Type)
	}
	if setting.Value != true {
		t.Errorf("Expected Value true, got %v", setting.Value)
	}
	if setting.DefaultValue != false {
		t.Errorf("Expected DefaultValue false, got %v", setting.DefaultValue)
	}
	if setting.Category != "test" {
		t.Errorf("Expected Category 'test', got %q", setting.Category)
	}
}

// TestCategoryStruct tests Category struct
func TestCategoryStruct(t *testing.T) {
	category := Category{
		ID:   "test",
		Name: "Test Category",
		Settings: []Setting{
			{ID: "test.setting1", Label: "Setting 1"},
			{ID: "test.setting2", Label: "Setting 2"},
		},
	}

	if category.ID != "test" {
		t.Errorf("Expected ID 'test', got %q", category.ID)
	}
	if category.Name != "Test Category" {
		t.Errorf("Expected Name 'Test Category', got %q", category.Name)
	}
	if len(category.Settings) != 2 {
		t.Errorf("Expected 2 settings, got %d", len(category.Settings))
	}
}

// TestGetCategories tests GetCategories function
func TestGetCategories(t *testing.T) {
	cfg := config.DefaultConfig()
	categories := GetCategories(cfg)

	if len(categories) < 3 {
		t.Errorf("Expected at least 3 categories, got %d", len(categories))
	}

	// Check for expected categories
	foundCategories := make(map[string]bool)
	for _, cat := range categories {
		foundCategories[cat.ID] = true
	}

	expectedCategories := []string{"editor", "ui", "lsp"}
	for _, expected := range expectedCategories {
		if !foundCategories[expected] {
			t.Errorf("Expected category %q not found", expected)
		}
	}
}

// TestGetCategoriesWithCustomConfig tests GetCategories with custom config
func TestGetCategoriesWithCustomConfig(t *testing.T) {
	cfg := config.Config{
		Editor: config.EditorConfig{
			TabSize:    2,
			InsertTabs: true,
			AutoIndent: false,
		},
		UI: config.UIConfig{
			Theme:    "dracula",
			ShowTree: false,
		},
	}

	categories := GetCategories(cfg)

	// Find editor category and check tab size setting
	for _, cat := range categories {
		if cat.ID == "editor" {
			for _, setting := range cat.Settings {
				if setting.ID == "editor.tab_size" {
					if setting.Value != 2 {
						t.Errorf("Expected tab_size 2, got %v", setting.Value)
					}
				}
				if setting.ID == "editor.insert_tabs" {
					if setting.Value != true {
						t.Errorf("Expected insert_tabs true, got %v", setting.Value)
					}
				}
			}
		}
		if cat.ID == "ui" {
			for _, setting := range cat.Settings {
				if setting.ID == "ui.theme" {
					if setting.Value != "dracula" {
						t.Errorf("Expected theme 'dracula', got %v", setting.Value)
					}
				}
			}
		}
	}
}

// TestSettingsModelCreation tests New function
func TestSettingsModelCreation(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()

	model := New(theme, cfg, "/test/config.toml")

	if model.configPath != "/test/config.toml" {
		t.Errorf("Expected configPath '/test/config.toml', got %q", model.configPath)
	}
	if len(model.categories) == 0 {
		t.Error("Expected categories to be initialized")
	}
}

// TestSettingsSetSize tests SetSize method
func TestSettingsSetSize(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	model.SetSize(100, 30)

	if model.width != 100 {
		t.Errorf("Expected width 100, got %d", model.width)
	}
	if model.height != 30 {
		t.Errorf("Expected height 30, got %d", model.height)
	}
}

// TestSettingsSelectedCategory tests SelectedCategory method
func TestSettingsSelectedCategory(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	cat := model.SelectedCategory()
	if cat == nil {
		t.Fatal("Expected non-nil category")
	}
	if cat.ID != "editor" {
		t.Errorf("Expected first category 'editor', got %q", cat.ID)
	}
}

// TestSettingsSelectedSetting tests SelectedSetting method
func TestSettingsSelectedSetting(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	setting := model.SelectedSetting()
	if setting == nil {
		t.Fatal("Expected non-nil setting")
	}
}

// TestSettingsSelectNextCategory tests SelectNextCategory method
func TestSettingsSelectNextCategory(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	initialCategory := model.selectedCategory
	model.SelectNextCategory()

	if model.selectedCategory <= initialCategory {
		t.Error("Expected category to change after SelectNextCategory")
	}
}

// TestSettingsSelectPrevCategory tests SelectPrevCategory method
func TestSettingsSelectPrevCategory(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	// Move to second category first
	model.SelectNextCategory()
	initialCategory := model.selectedCategory

	model.SelectPrevCategory()

	if model.selectedCategory >= initialCategory {
		t.Error("Expected category to decrease after SelectPrevCategory")
	}
}

// TestSettingsSelectNextSetting tests SelectNextSetting method
func TestSettingsSelectNextSetting(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	initialSetting := model.selectedSetting
	model.SelectNextSetting()

	if model.selectedSetting <= initialSetting {
		t.Error("Expected setting to change after SelectNextSetting")
	}
}

// TestSettingsSelectPrevSetting tests SelectPrevSetting method
func TestSettingsSelectPrevSetting(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	// Move to second setting first
	model.SelectNextSetting()
	initialSetting := model.selectedSetting

	model.SelectPrevSetting()

	if model.selectedSetting >= initialSetting {
		t.Error("Expected setting to decrease after SelectPrevSetting")
	}
}

// TestSettingsToggleBoolValue tests ToggleBoolValue method
func TestSettingsToggleBoolValue(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	// Find a bool setting (insert_tabs)
	for i, cat := range model.categories {
		if cat.ID == "editor" {
			for j, setting := range cat.Settings {
				if setting.ID == "editor.insert_tabs" {
					model.selectedCategory = i
					model.selectedSetting = j

					initialValue := setting.Value.(bool)
					model.ToggleBoolValue()

					newValue := model.SelectedSetting().Value.(bool)
					if newValue == initialValue {
						t.Error("Expected bool value to toggle")
					}
					return
				}
			}
		}
	}
	t.Error("Could not find bool setting to test")
}

// TestSettingsIncrementIntValue tests IncrementIntValue method
func TestSettingsIncrementIntValue(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	// Find an int setting (tab_size)
	for i, cat := range model.categories {
		if cat.ID == "editor" {
			for j, setting := range cat.Settings {
				if setting.ID == "editor.tab_size" {
					model.selectedCategory = i
					model.selectedSetting = j

					initialValue := setting.Value.(int)
					model.IncrementIntValue()

					newValue := model.SelectedSetting().Value.(int)
					if newValue != initialValue+1 {
						t.Errorf("Expected value to increment from %d to %d, got %d", initialValue, initialValue+1, newValue)
					}
					return
				}
			}
		}
	}
	t.Error("Could not find int setting to test")
}

// TestSettingsDecrementIntValue tests DecrementIntValue method
func TestSettingsDecrementIntValue(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	// Find an int setting (tab_size)
	for i, cat := range model.categories {
		if cat.ID == "editor" {
			for j, setting := range cat.Settings {
				if setting.ID == "editor.tab_size" {
					model.selectedCategory = i
					model.selectedSetting = j

					// Increment first
					model.IncrementIntValue()
					initialValue := model.SelectedSetting().Value.(int)

					model.DecrementIntValue()

					newValue := model.SelectedSetting().Value.(int)
					if newValue != initialValue-1 {
						t.Errorf("Expected value to decrement from %d to %d, got %d", initialValue, initialValue-1, newValue)
					}
					return
				}
			}
		}
	}
	t.Error("Could not find int setting to test")
}

// TestSettingsResetCurrentValue tests ResetCurrentValue method
func TestSettingsResetCurrentValue(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	// Find a setting with different default
	for i, cat := range model.categories {
		if cat.ID == "editor" {
			for j, setting := range cat.Settings {
				if setting.ID == "editor.tab_size" {
					model.selectedCategory = i
					model.selectedSetting = j

					// Change value
					model.IncrementIntValue()
					changedValue := model.SelectedSetting().Value.(int)

					// Reset
					model.ResetCurrentValue()
					resetValue := model.SelectedSetting().Value.(int)

					if resetValue != setting.DefaultValue.(int) {
						t.Errorf("Expected reset value %d, got %d (changed was %d)", setting.DefaultValue, resetValue, changedValue)
					}
					return
				}
			}
		}
	}
	t.Error("Could not find setting to test")
}

// TestSettingsView tests View method
func TestSettingsView(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	model.width = 80
	model.height = 24

	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
	if !strings.Contains(view, "Settings") {
		t.Error("Expected view to contain 'Settings' title")
	}
}

// TestSettingsCategoryCount tests CategoryCount method
func TestSettingsCategoryCount(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	count := model.CategoryCount()
	if count < 3 {
		t.Errorf("Expected at least 3 categories, got %d", count)
	}
}

// TestSettingsSettingCount tests SettingCount method
func TestSettingsSettingCount(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	count := model.SettingCount()
	if count == 0 {
		t.Error("Expected non-zero setting count")
	}
}

// TestSettingsConfigPath tests ConfigPath method
func TestSettingsConfigPath(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	path := model.ConfigPath()
	if path != "/test/config.toml" {
		t.Errorf("Expected config path '/test/config.toml', got %q", path)
	}
}

// TestSettingsPreviewTOML tests PreviewTOML method
func TestSettingsPreviewTOML(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()
	model := New(theme, cfg, "/test/config.toml")

	toml := model.PreviewTOML()
	if toml == "" {
		t.Error("Expected non-empty TOML preview")
	}
	if !strings.Contains(toml, "[editor]") {
		t.Error("Expected TOML to contain [editor] section")
	}
	if !strings.Contains(toml, "[ui]") {
		t.Error("Expected TOML to contain [ui] section")
	}
}

// TestSettingsWithEmptyConfigPath tests settings with empty config path
func TestSettingsWithEmptyConfigPath(t *testing.T) {
	theme := ui.DefaultTheme()
	cfg := config.DefaultConfig()

	model := New(theme, cfg, "")

	if model.configPath != "" {
		t.Errorf("Expected empty configPath, got %q", model.configPath)
	}
}
