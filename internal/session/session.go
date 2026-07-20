package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"teak/internal/atomicfile"
)

const (
	maxSessionBytes     = 4 << 20
	maxSessionTabs      = 10_000
	maxSessionPathBytes = 32 << 10
	maxSessionVersion   = 1_000_000
)

// writeSessionData is a narrow test seam for simulating a failure before the
// atomic replacement step. Production always writes directly to the already
// private temporary file.
var writeSessionData = func(file *os.File, data []byte) error {
	_, err := file.Write(data)
	return err
}

// TabState stores the state of a single editor tab.
type TabState struct {
	FilePath    string `json:"file_path"`
	CursorLine  int    `json:"cursor_line"`
	CursorCol   int    `json:"cursor_col"`
	ScrollY     int    `json:"scroll_y"`
	WrapScrollY int    `json:"wrap_scroll_y,omitempty"`
	Pinned      bool   `json:"pinned"`
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
	if dir := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "teak", "session.json")
	}
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
	return LoadContext(context.Background())
}

// LoadContext reads the persisted session while checking ctx between file
// reads. Startup uses this form so a superseded session restore can stop
// before parsing or filtering additional state.
func LoadContext(ctx context.Context) (State, error) {
	return loadFromPathContext(ctx, Path())
}

func saveToPath(state State, path string) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("invalid session state: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session state: %w", err)
	}
	if len(data) > maxSessionBytes {
		return fmt.Errorf("encoded session exceeds %d-byte limit", maxSessionBytes)
	}
	if err := atomicfile.Write(path, func(file *os.File) error {
		return writeSessionData(file, data)
	}); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func loadFromPath(path string) (State, error) {
	return loadFromPathContext(context.Background(), path)
}

func loadFromPathContext(ctx context.Context, path string) (State, error) {
	var state State
	if err := ctx.Err(); err != nil {
		return state, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return state, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return state, fmt.Errorf("session path is not a regular file")
	}
	if info.Size() > maxSessionBytes {
		return state, fmt.Errorf("session file exceeds %d-byte limit", maxSessionBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(sessionContextReader{ctx: ctx, reader: file}, maxSessionBytes+1))
	if err != nil {
		return state, err
	}
	if len(data) > maxSessionBytes {
		return state, fmt.Errorf("session file exceeds %d-byte limit", maxSessionBytes)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if err := state.Validate(); err != nil {
		return state, fmt.Errorf("invalid session state: %w", err)
	}
	return state, nil
}

type sessionContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r sessionContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// Validate bounds session data before it is persisted or restored. Session
// files are user-controlled input, so keeping these limits here prevents a
// malformed file from allocating unbounded editor state during restoration.
func (s State) Validate() error {
	if s.Version < 0 || s.Version > maxSessionVersion {
		return fmt.Errorf("version must be between 0 and %d", maxSessionVersion)
	}
	if len(s.RootDir) > maxSessionPathBytes || strings.IndexByte(s.RootDir, 0) >= 0 {
		return fmt.Errorf("root_dir is invalid")
	}
	if len(s.Tabs) > maxSessionTabs {
		return fmt.Errorf("tabs exceeds %d entries", maxSessionTabs)
	}
	if s.ActiveTab < -1 || (len(s.Tabs) == 0 && s.ActiveTab > 0) || (len(s.Tabs) > 0 && s.ActiveTab >= len(s.Tabs)) {
		return fmt.Errorf("active_tab is outside the saved tabs")
	}
	for i, tab := range s.Tabs {
		if tab.FilePath == "" || len(tab.FilePath) > maxSessionPathBytes || strings.IndexByte(tab.FilePath, 0) >= 0 {
			return fmt.Errorf("tabs[%d].file_path is invalid", i)
		}
		if tab.CursorLine < 0 || tab.CursorCol < 0 || tab.ScrollY < 0 || tab.WrapScrollY < 0 {
			return fmt.Errorf("tabs[%d] has a negative cursor or scroll position", i)
		}
	}
	return nil
}
