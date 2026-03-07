package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// TabState stores the state of a single editor tab.
type TabState struct {
	FilePath string `json:"file_path"`
	CursorLine int  `json:"cursor_line"`
	CursorCol  int  `json:"cursor_col"`
	ScrollY    int  `json:"scroll_y"`
	Pinned     bool `json:"pinned"`
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
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads the session state from disk.
func Load() (State, error) {
	var state State
	data, err := os.ReadFile(Path())
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}
