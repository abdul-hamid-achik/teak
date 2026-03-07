package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTabState tests TabState struct
func TestTabState(t *testing.T) {
	tab := TabState{
		FilePath:   "/test.go",
		CursorLine: 10,
		CursorCol:  5,
		ScrollY:    100,
		Pinned:     true,
	}

	if tab.FilePath != "/test.go" {
		t.Errorf("Expected FilePath '/test.go', got %q", tab.FilePath)
	}
	if tab.CursorLine != 10 {
		t.Errorf("Expected CursorLine 10, got %d", tab.CursorLine)
	}
	if tab.CursorCol != 5 {
		t.Errorf("Expected CursorCol 5, got %d", tab.CursorCol)
	}
	if tab.ScrollY != 100 {
		t.Errorf("Expected ScrollY 100, got %d", tab.ScrollY)
	}
	if !tab.Pinned {
		t.Error("Expected Pinned to be true")
	}
}

// TestState tests State struct
func TestState(t *testing.T) {
	state := State{
		Version:   1,
		RootDir:   "/project",
		ActiveTab: 2,
		Tabs: []TabState{
			{FilePath: "/test1.go", CursorLine: 0},
			{FilePath: "/test2.go", CursorLine: 10},
		},
	}

	if state.Version != 1 {
		t.Errorf("Expected Version 1, got %d", state.Version)
	}
	if state.RootDir != "/project" {
		t.Errorf("Expected RootDir '/project', got %q", state.RootDir)
	}
	if state.ActiveTab != 2 {
		t.Errorf("Expected ActiveTab 2, got %d", state.ActiveTab)
	}
	if len(state.Tabs) != 2 {
		t.Errorf("Expected 2 tabs, got %d", len(state.Tabs))
	}
}

// TestPath tests session file path generation
func TestPath(t *testing.T) {
	path := Path()

	if path == "" {
		t.Error("Expected non-empty path")
	}

	// Should end with session.json
	if filepath.Base(path) != "session.json" {
		t.Errorf("Expected path to end with 'session.json', got %q", path)
	}
}

// TestSaveAndLoad tests saving and loading session
func TestSaveAndLoad(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "session.json")

	state := State{
		Version:   1,
		RootDir:   "/test",
		ActiveTab: 0,
		Tabs: []TabState{
			{FilePath: "/test.go", CursorLine: 10, Pinned: true},
		},
	}

	// Save to temp location
	err := saveToPath(state, testPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load from temp location
	loaded, err := loadFromPath(testPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify loaded state
	if loaded.Version != state.Version {
		t.Errorf("Version mismatch: %d != %d", loaded.Version, state.Version)
	}
	if loaded.RootDir != state.RootDir {
		t.Errorf("RootDir mismatch: %q != %q", loaded.RootDir, state.RootDir)
	}
	if len(loaded.Tabs) != len(state.Tabs) {
		t.Errorf("Tab count mismatch: %d != %d", len(loaded.Tabs), len(state.Tabs))
	}
}

// TestSaveToNonExistentDirectory tests saving to directory that doesn't exist
func TestSaveToNonExistentDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "subdir", "session.json")

	state := State{
		Version:   1,
		RootDir:   "/test",
		ActiveTab: 0,
		Tabs:      []TabState{},
	}

	// Should create directory automatically
	err := saveToPath(state, testPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		t.Error("Expected file to be created")
	}
}

// TestLoadNonExistentFile tests loading from non-existent file
func TestLoadNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "nonexistent.json")

	_, err := loadFromPath(testPath)
	if err == nil {
		t.Error("Expected error when loading non-existent file")
	}
}

// TestEmptyState tests empty state
func TestEmptyState(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "session.json")

	state := State{
		Version:   0,
		RootDir:   "",
		ActiveTab: 0,
		Tabs:      []TabState{},
	}

	err := saveToPath(state, testPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := loadFromPath(testPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.Version != 0 {
		t.Errorf("Expected Version 0, got %d", loaded.Version)
	}
	if loaded.RootDir != "" {
		t.Errorf("Expected empty RootDir, got %q", loaded.RootDir)
	}
	if len(loaded.Tabs) != 0 {
		t.Errorf("Expected 0 tabs, got %d", len(loaded.Tabs))
	}
}

// TestMultipleTabs tests state with multiple tabs
func TestMultipleTabs(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "session.json")

	state := State{
		Version:   1,
		RootDir:   "/project",
		ActiveTab: 2,
		Tabs: []TabState{
			{FilePath: "/file1.go", CursorLine: 0, CursorCol: 0, ScrollY: 0, Pinned: false},
			{FilePath: "/file2.go", CursorLine: 10, CursorCol: 5, ScrollY: 100, Pinned: true},
			{FilePath: "/file3.go", CursorLine: 20, CursorCol: 10, ScrollY: 200, Pinned: false},
		},
	}

	err := saveToPath(state, testPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := loadFromPath(testPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loaded.Tabs) != 3 {
		t.Errorf("Expected 3 tabs, got %d", len(loaded.Tabs))
	}
	if loaded.ActiveTab != 2 {
		t.Errorf("Expected ActiveTab 2, got %d", loaded.ActiveTab)
	}
}

// TestTabStateWithSpecialCharacters tests tab state with special characters in path
func TestTabStateWithSpecialCharacters(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "session.json")

	state := State{
		Version:   1,
		RootDir:   "/project with spaces",
		ActiveTab: 0,
		Tabs: []TabState{
			{FilePath: "/file with spaces.go", CursorLine: 0},
			{FilePath: "/file-with-dashes.go", CursorLine: 0},
			{FilePath: "/file_with_underscores.go", CursorLine: 0},
		},
	}

	err := saveToPath(state, testPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := loadFromPath(testPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.RootDir != state.RootDir {
		t.Errorf("RootDir mismatch: %q != %q", loaded.RootDir, state.RootDir)
	}
}

// TestStateJSONFormat tests that saved JSON is properly formatted
func TestStateJSONFormat(t *testing.T) {
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "session.json")

	state := State{
		Version:   1,
		RootDir:   "/test",
		ActiveTab: 0,
		Tabs:      []TabState{},
	}

	err := saveToPath(state, testPath)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read raw file content
	data, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Should contain indentation (formatted JSON)
	if len(data) < 50 {
		t.Error("Expected formatted JSON with indentation")
	}
}

// Helper functions for testing with custom paths
func saveToPath(state State, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadFromPath(path string) (State, error) {
	var state State
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}
