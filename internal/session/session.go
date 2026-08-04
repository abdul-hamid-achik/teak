package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"teak/internal/atomicfile"
)

const (
	maxSessionBytes     = 4 << 20
	maxSessionTabs      = 10_000
	maxSessionPathBytes = 32 << 10
	maxSessionVersion   = 1_000_000
	maxSessionNameBytes = 128
	maxHealthIssues     = 1_000
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

// TabHealth describes why a saved tab can no longer be restored cleanly.
type TabHealth struct {
	FilePath string `json:"file_path"`
	State    string `json:"state"`
}

// NamedHealth is a bounded, read-only assessment of a named session.
// "stale" means at least one tab is missing, inaccessible, or outside the
// workspace; "invalid" means the session file itself cannot be loaded.
type NamedHealth struct {
	Name   string      `json:"name"`
	Path   string      `json:"path"`
	State  string      `json:"state"`
	Tabs   int         `json:"tabs"`
	Issues []TabHealth `json:"issues,omitempty"`
	Detail string      `json:"detail,omitempty"`
}

// StateHome returns the Teak state directory following the XDG Base Directory
// Specification:
//
//	$XDG_STATE_HOME/teak          when XDG_STATE_HOME is an absolute path
//	$HOME/.local/state/teak       default on Unix-like systems
//	$TMPDIR/teak                  last-resort fallback (e.g. CI without a home)
func StateHome() string {
	if dir := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "teak")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "teak")
	}
	return filepath.Join(home, ".local", "state", "teak")
}

// PathForRoot returns the per-workspace session file path:
//
//	$XDG_STATE_HOME/teak/sessions/<sha256(abs root)>/session.json
//
// Each workspace keeps its own tabs so switching projects no longer overwrites
// another project's session.
func PathForRoot(rootDir string) string {
	return filepath.Join(StateHome(), "sessions", rootKey(rootDir), "session.json")
}

// LegacyPath returns the pre-per-workspace session path
// ($XDG_STATE_HOME/teak/session.json). LoadContextForRoot still reads it once
// for migration when the per-root file is missing.
func LegacyPath() string {
	return filepath.Join(StateHome(), "session.json")
}

// Path returns LegacyPath for backward compatibility with older callers and
// tests. Prefer PathForRoot for new code.
func Path() string {
	return LegacyPath()
}

// NamedPath returns the path for a user-named session in one workspace. Names
// are deliberately a single safe path component so a headless caller cannot
// turn session management into arbitrary file access.
func NamedPath(rootDir, name string) (string, error) {
	name, err := normalizeName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(StateHome(), "sessions", rootKey(rootDir), "named", name+".json"), nil
}

func legacyNamedPath(rootDir, name string) (string, error) {
	name, err := normalizeName(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(StateHome(), "sessions", legacyRootKey(rootDir), "named", name+".json"), nil
}

// SaveNamed stores a validated workspace session under name.
func SaveNamed(state State, name string) error {
	path, err := NamedPath(state.RootDir, name)
	if err != nil {
		return err
	}
	return saveToPath(state, path)
}

// LoadNamed loads a named session and verifies that it belongs to rootDir.
func LoadNamed(ctx context.Context, rootDir, name string) (State, error) {
	path, err := NamedPath(rootDir, name)
	if err != nil {
		return State{}, err
	}
	state, err := loadFromPathContext(ctx, path)
	if os.IsNotExist(err) {
		legacyPath, legacyErr := legacyNamedPath(rootDir, name)
		if legacyErr != nil {
			return State{}, legacyErr
		}
		if legacyPath != path {
			state, err = loadFromPathContext(ctx, legacyPath)
		}
	}
	if err != nil {
		return State{}, err
	}
	if !rootsMatch(state.RootDir, rootDir) {
		return State{}, fmt.Errorf("named session belongs to a different workspace")
	}
	return state, nil
}

// ListNamed returns the safe names stored for rootDir. Symlinks and malformed
// filenames are ignored rather than followed or exposed as paths.
func ListNamed(rootDir string) ([]string, error) {
	dirs := []string{
		filepath.Join(StateHome(), "sessions", rootKey(rootDir), "named"),
		filepath.Join(StateHome(), "sessions", legacyRootKey(rootDir), "named"),
	}
	namesByName := make(map[string]struct{})
	for index, dir := range dirs {
		if index == 1 && dir == dirs[0] {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			if filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			if _, err := normalizeName(name); err != nil {
				continue
			}
			namesByName[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(namesByName))
	for name := range namesByName {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// CheckNamed reports whether a named session can be restored in rootDir. It
// never mutates the session or the workspace and bounds the returned issue
// collection even when a user-controlled session contains many tabs.
func CheckNamed(ctx context.Context, rootDir, name string) (NamedHealth, error) {
	path, err := NamedPath(rootDir, name)
	if err != nil {
		return NamedHealth{}, err
	}
	health := NamedHealth{Name: name, Path: path, State: "missing"}
	state, err := LoadNamed(ctx, rootDir, name)
	if err != nil {
		if os.IsNotExist(err) {
			health.Detail = "no saved session"
			return health, nil
		}
		health.State = "invalid"
		health.Detail = err.Error()
		return health, nil
	}
	health.Tabs = len(state.Tabs)
	for _, tab := range state.Tabs {
		if err := ctx.Err(); err != nil {
			return NamedHealth{}, err
		}
		status := inspectTabPath(ctx, rootDir, tab.FilePath)
		if status == "present" {
			continue
		}
		if len(health.Issues) < maxHealthIssues {
			health.Issues = append(health.Issues, TabHealth{FilePath: tab.FilePath, State: status})
		}
	}
	if len(health.Issues) == 0 {
		health.State = "healthy"
	} else {
		health.State = "stale"
	}
	return health, nil
}

// CheckNamedAll assesses every safe regular named session for rootDir in
// deterministic name order.
func CheckNamedAll(ctx context.Context, rootDir string) ([]NamedHealth, error) {
	names, err := ListNamed(rootDir)
	if err != nil {
		return nil, err
	}
	health := make([]NamedHealth, 0, len(names))
	for _, name := range names {
		entry, err := CheckNamed(ctx, rootDir, name)
		if err != nil {
			return nil, err
		}
		health = append(health, entry)
	}
	return health, nil
}

// RemoveNamed removes one validated named session. It refuses symlinks and
// non-regular files so cleanup cannot follow an attacker-controlled path.
// Missing files are treated as success to make explicit cleanup idempotent.
func RemoveNamed(rootDir, name string) error {
	path, err := NamedPath(rootDir, name)
	if err != nil {
		return err
	}
	legacyPath, err := legacyNamedPath(rootDir, name)
	if err != nil {
		return err
	}
	paths := []string{path}
	if legacyPath != path {
		paths = append(paths, legacyPath)
	}
	for _, candidate := range paths {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("named session path is not a regular file")
		}
		if err := os.Remove(candidate); err != nil {
			return err
		}
	}
	return nil
}

func inspectTabPath(ctx context.Context, rootDir, filePath string) string {
	if err := ctx.Err(); err != nil {
		return "unavailable"
	}
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return "unavailable"
	}
	target := filePath
	if !filepath.IsAbs(target) {
		target = filepath.Join(rootAbs, target)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "unavailable"
	}
	if !pathWithin(rootAbs, targetAbs) {
		return "outside"
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "unavailable"
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "unavailable"
	}
	if !pathWithin(resolvedRoot, resolvedTarget) {
		return "outside"
	}
	info, err := os.Stat(resolvedTarget)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "unavailable"
	}
	if !info.Mode().IsRegular() {
		return "unavailable"
	}
	return "present"
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || len(name) > maxSessionNameBytes ||
		strings.ContainsAny(name, `/\\`) || filepath.Base(name) != name {
		return "", fmt.Errorf("invalid session name %q", name)
	}
	return name, nil
}

// rootKey returns a stable filesystem-safe key for a workspace root.
func rootKey(rootDir string) string {
	abs := canonicalRootPath(rootDir)
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

// legacyRootKey reproduces the pre-canonicalization workspace key so state
// written by older Teak versions remains discoverable during migration.
func legacyRootKey(rootDir string) string {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		abs = filepath.Clean(rootDir)
	} else {
		abs = filepath.Clean(abs)
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])
}

// canonicalRootPath keeps session identity consistent when the same project
// is opened through a symlink and through its real filesystem path. If the
// root does not exist yet, retain an absolute lexical fallback so callers can
// still derive a deterministic path before creating it.
func canonicalRootPath(rootDir string) string {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return filepath.Clean(rootDir)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}
	return abs
}

// Save writes the session state to the per-workspace path for state.RootDir.
func Save(state State) error {
	path := PathForRoot(state.RootDir)
	if state.RootDir == "" {
		// Empty root still needs a stable location; keep the legacy path so
		// tests and edge cases without a workspace do not scatter files.
		path = LegacyPath()
	}
	return saveToPath(state, path)
}

// Load reads the legacy single-file session. Prefer LoadContextForRoot.
func Load() (State, error) {
	return LoadContext(context.Background())
}

// LoadContext reads the legacy single-file session path.
func LoadContext(ctx context.Context) (State, error) {
	return loadFromPathContext(ctx, LegacyPath())
}

// LoadContextForRoot loads the session for a workspace root. When the
// per-workspace file is missing it falls back to the legacy global session
// only if that file's RootDir refers to the same directory (one-shot
// migration from older Teak installs).
func LoadContextForRoot(ctx context.Context, rootDir string) (State, error) {
	if rootDir == "" {
		return LoadContext(ctx)
	}
	path := PathForRoot(rootDir)
	state, err := loadFromPathContext(ctx, path)
	if err == nil {
		return state, nil
	}
	if !os.IsNotExist(err) {
		return state, err
	}
	legacyPath := filepath.Join(StateHome(), "sessions", legacyRootKey(rootDir), "session.json")
	if legacyPath != path {
		state, legacyErr := loadFromPathContext(ctx, legacyPath)
		if legacyErr == nil {
			return state, nil
		}
		if !os.IsNotExist(legacyErr) {
			return State{}, legacyErr
		}
	}
	legacy, legErr := loadFromPathContext(ctx, LegacyPath())
	if legErr != nil {
		return State{}, err
	}
	if !rootsMatch(legacy.RootDir, rootDir) {
		return State{}, os.ErrNotExist
	}
	return legacy, nil
}

// rootsMatch reports whether two path spellings refer to the same directory.
func rootsMatch(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if absA, errA := filepath.Abs(a); errA == nil {
		if absB, errB := filepath.Abs(b); errB == nil {
			if filepath.Clean(absA) == filepath.Clean(absB) {
				return true
			}
		}
	}
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && infoA.IsDir() && infoB.IsDir() && os.SameFile(infoA, infoB)
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
