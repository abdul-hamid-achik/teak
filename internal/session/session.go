package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var writeFile = os.WriteFile

// TabState stores the state of a single editor tab.
type TabState struct {
	FilePath   string `json:"file_path"`
	CursorLine int    `json:"cursor_line"`
	CursorCol  int    `json:"cursor_col"`
	ScrollY    int    `json:"scroll_y"`
	Pinned     bool   `json:"pinned"`
}

// State stores the full session state.
type State struct {
	Version   int        `json:"version"`
	RootDir   string     `json:"root_dir"`
	ActiveTab int        `json:"active_tab"`
	Tabs      []TabState `json:"tabs"`
}

// Path returns the session file path.
func Path() string {
	// In CI or when home dir is not available, use temp directory
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp directory for CI environments
		return filepath.Join(os.TempDir(), "teak", "session.json")
	}
	return filepath.Join(home, ".local", "state", "teak", "session.json")
}

// Save writes the session state to disk.
func Save(state State) error {
	return saveToPath(state, Path())
}

// Load reads the session state from disk.
func Load() (State, error) {
	return loadFromPath(Path())
}

func saveToPath(state State, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Remove(tempPath); err != nil {
		return err
	}

	if err := writeFile(tempPath, data, 0o644); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("rename session file: %w", err)
	}
	return nil
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
