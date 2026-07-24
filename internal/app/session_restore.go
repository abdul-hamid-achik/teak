package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/session"
	"teak/internal/text"
)

// A restored workspace should remain immediately usable even when a previous
// session contained a large number of generated files. This is deliberately
// lower than the on-disk session format's safety limit.
const (
	maxStartupSessionTabs  = 100
	maxStartupSessionBytes = 128 << 20
)

var (
	// loadSessionForRestore loads the per-workspace session for rootDir.
	loadSessionForRestore  = session.LoadContextForRoot
	readSessionRestoreFile = readSessionRestoreFileFromRoot
)

var errSessionRestoreWorkspaceChanged = errors.New("workspace changed while restoring session")

// restoredSessionFile contains a rope prepared while a workspace root handle
// was pinned. It intentionally carries no descriptor or raw file bytes across
// Bubble Tea messages: all handles are closed by the command before it
// returns, and each raw read is released before the next tab is read. This
// keeps session restoration below roughly one aggregate document budget plus
// one in-flight file, rather than retaining the aggregate bytes and then
// building a second aggregate of ropes in Batch commands.
type restoredSessionFile struct {
	Tab      session.TabState
	Snapshot *text.Rope
}

type sessionRestoreResultMsg struct {
	Generation uint64
	State      session.State
	Files      []restoredSessionFile
	Err        error
}

// sessionRestoreCmd performs all session I/O and per-file existence checks off
// the Bubble Tea update path. The generation is checked again by Update before
// it is allowed to create any tabs.
func sessionRestoreCmd(ctx context.Context, generation uint64, rootDir string) tea.Cmd {
	return func() tea.Msg {
		if err := ctx.Err(); err != nil {
			return sessionRestoreResultMsg{Generation: generation, Err: err}
		}
		if !sessionRestorePinnedRootSupported() {
			// os.Root cannot retain a directory identity across a rename on
			// Plan 9 and documents a symlink-validation TOCTOU limitation on
			// js. Skipping persisted restoration is safer than opening an
			// attacker-replaced workspace on those targets.
			return sessionRestoreResultMsg{Generation: generation}
		}
		root, err := os.OpenRoot(rootDir)
		if err != nil {
			return sessionRestoreResultMsg{Generation: generation, Err: err}
		}
		defer func() { _ = root.Close() }()
		rootInfo, err := root.Stat(".")
		if err != nil || !rootInfo.IsDir() {
			return sessionRestoreResultMsg{Generation: generation, Err: err}
		}
		state, err := loadSessionForRestore(ctx, rootDir)
		if err != nil {
			return sessionRestoreResultMsg{Generation: generation, Err: err}
		}
		// Compare the saved workspace against the *opened* root rather than
		// reopening rootDir after validation. A name can be swapped at any
		// point; the descriptor is the identity that governs all subsequent
		// reads.
		if !sessionWorkspaceMatchesRoot(state.RootDir, rootInfo) {
			return sessionRestoreResultMsg{Generation: generation}
		}
		filtered, files, err := readSessionRestoreFiles(ctx, root, rootDir, state, maxStartupSessionBytes)
		if err != nil {
			releaseRestoredSessionFiles(files)
			return sessionRestoreResultMsg{Generation: generation, Err: err}
		}
		if err := ctx.Err(); err != nil {
			releaseRestoredSessionFiles(files)
			return sessionRestoreResultMsg{Generation: generation, Err: err}
		}
		// The descriptor retained the original directory across a rename. Do
		// not attach those bytes to lexical paths if the model's workspace name
		// now resolves somewhere else.
		if !sessionWorkspaceMatchesRoot(rootDir, rootInfo) {
			releaseRestoredSessionFiles(files)
			return sessionRestoreResultMsg{Generation: generation, Err: errSessionRestoreWorkspaceChanged}
		}
		return sessionRestoreResultMsg{Generation: generation, State: filtered, Files: files}
	}
}

// readSessionRestoreFileFromRoot reads a single path using the root handle
// captured at the start of session restoration. os.Root ensures all parent
// links remain beneath that root even if a link is swapped concurrently.
func readSessionRestoreFileFromRoot(ctx context.Context, root *os.Root, name string, limit int64) ([]byte, os.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	file, err := openSessionRestoreInput(root, name)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	return readOpenedEditorFile(ctx, file, name, limit)
}

// readSessionRestoreFiles validates only lexical path spelling and delegates
// link traversal to os.Root. This avoids EvalSymlinks/Stat-by-name TOCTOU
// windows and keeps all restored data within both per-file and aggregate
// memory budgets before a UI tab is ever created.
func readSessionRestoreFiles(ctx context.Context, root *os.Root, rootDir string, state session.State, budget int64) (session.State, []restoredSessionFile, error) {
	filtered := session.State{Version: state.Version, RootDir: state.RootDir, ActiveTab: -1}
	if budget <= 0 {
		return filtered, nil, nil
	}
	savedRootPath, err := filepath.Abs(state.RootDir)
	if err != nil {
		return session.State{}, nil, err
	}
	displayRootPath, err := filepath.Abs(rootDir)
	if err != nil {
		return session.State{}, nil, err
	}
	byRelativePath := make(map[string]int, min(len(state.Tabs), maxStartupSessionTabs))
	var identities []os.FileInfo
	var total int64
	var files []restoredSessionFile
	for savedIndex, saved := range state.Tabs {
		if err := ctx.Err(); err != nil {
			releaseRestoredSessionFiles(files)
			return session.State{}, nil, err
		}
		if len(filtered.Tabs) >= maxStartupSessionTabs || total >= budget {
			break
		}
		relative, displayPath, ok := sessionRestoreRelativePath(savedRootPath, displayRootPath, saved.FilePath)
		if !ok {
			continue
		}
		if existingIndex, duplicate := byRelativePath[relative]; duplicate {
			if savedIndex == state.ActiveTab {
				filtered.ActiveTab = existingIndex
			}
			continue
		}
		remaining := budget - total
		limit := min(maxEditorFileBytes, remaining)
		data, info, err := readSessionRestoreFile(ctx, root, relative, limit)
		if ctxErr := ctx.Err(); ctxErr != nil {
			releaseRestoredSessionFiles(files)
			return session.State{}, nil, ctxErr
		}
		if err != nil {
			// Persisted sessions can legitimately reference deleted, oversized,
			// non-regular, or now-untrusted link targets. Skip just that tab.
			continue
		}
		duplicate := -1
		for index, identity := range identities {
			if os.SameFile(identity, info) {
				duplicate = index
				break
			}
		}
		if duplicate >= 0 {
			if savedIndex == state.ActiveTab {
				filtered.ActiveTab = duplicate
			}
			continue
		}
		saved.FilePath = displayPath
		if savedIndex == state.ActiveTab {
			filtered.ActiveTab = len(filtered.Tabs)
		}
		byRelativePath[relative] = len(filtered.Tabs)
		filtered.Tabs = append(filtered.Tabs, saved)
		identities = append(identities, info)
		// Build the immutable rope while this command still owns the one raw
		// file read. NewOwned transfers that allocation into the restored
		// document snapshot.
		size := int64(len(data))
		snapshot := text.NewOwned(data)
		files = append(files, restoredSessionFile{Tab: saved, Snapshot: snapshot})
		total += size
	}
	return filtered, files, nil
}

// sessionRestoreRelativePath translates a persisted spelling under savedRoot
// into the current model spelling under displayRoot. The two roots were
// already identity-checked with os.SameFile, so this supports a workspace
// opened through a symlink without resolving any candidate path by name.
func sessionRestoreRelativePath(savedRoot, displayRoot, path string) (relative, displayPath string, ok bool) {
	if path == "" {
		return "", "", false
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(savedRoot, candidate)
	}
	relative, err := filepath.Rel(savedRoot, filepath.Clean(candidate))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", false
	}
	return filepath.Clean(relative), filepath.Clean(filepath.Join(displayRoot, relative)), true
}

func sessionWorkspaceMatchesRoot(path string, rootInfo os.FileInfo) bool {
	if path == "" || rootInfo == nil || !rootInfo.IsDir() {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir() && os.SameFile(info, rootInfo)
}

func releaseRestoredSessionFiles(files []restoredSessionFile) {
	for index := range files {
		files[index].Snapshot = nil
	}
}

func (m *Model) cancelSessionRestore() {
	if !m.sessionRestoreEligible {
		return
	}
	m.sessionRestoreEligible = false
	m.sessionRestoreGeneration++
	if m.sessionRestoreCancel != nil {
		m.sessionRestoreCancel()
		m.sessionRestoreCancel = nil
	}
}

func (m *Model) applySessionRestore(result sessionRestoreResultMsg) tea.Cmd {
	if !m.sessionRestoreEligible || result.Generation != m.sessionRestoreGeneration {
		releaseRestoredSessionFiles(result.Files)
		return nil
	}
	m.sessionRestoreEligible = false
	if m.sessionRestoreCancel != nil {
		m.sessionRestoreCancel()
	}
	m.sessionRestoreCancel = nil
	if result.Err != nil || len(result.State.Tabs) == 0 || len(result.State.Tabs) != len(result.Files) {
		releaseRestoredSessionFiles(result.Files)
		return nil
	}
	return m.restoreSessionFromPinnedRead(result.State, result.Files)
}

// restoreSessionFromPinnedRead rebuilds placeholders and sends already
// verified rope snapshots through the normal FileLoadedMsg path. It preserves the usual
// highlighter, watcher, LSP, plugin, cursor, and stale-editor safeguards
// without scheduling a second path-based disk read after validation.
//
// When the editor was started with CLI files (startupFiles), those paths are
// merged into the restored tabs: session tabs come back, missing CLI files are
// appended, CLI line:col wins via startupCursors, and the first CLI file becomes
// active.
func (m *Model) restoreSessionFromPinnedRead(state session.State, files []restoredSessionFile) tea.Cmd {
	if len(state.Tabs) != len(files) {
		releaseRestoredSessionFiles(files)
		return nil
	}
	if m.welcome != nil {
		m.welcome.Dismiss()
	}
	// Drop placeholder CLI tabs; session rebuild recreates the full set.
	m.editors = nil
	m.tabBar.Tabs = nil
	m.activeTab = -1
	cmds := make([]tea.Cmd, 0, len(files)+len(m.startupFiles))
	restoredPaths := make(map[string]struct{}, len(files))
	for savedIndex, restored := range files {
		tab := restored.Tab
		buf := text.NewBuffer()
		buf.FilePath = tab.FilePath
		cfg := editor.DefaultConfig()
		cfg.TabSize = m.appCfg.Editor.TabSize
		cfg.InsertTabs = m.appCfg.Editor.InsertTabs
		cfg.AutoIndent = m.appCfg.Editor.AutoIndent
		cfg.CommentPrefix = editor.CommentPrefixForFile(tab.FilePath)
		ed := editor.New(buf, m.theme, cfg)
		m.editors = append(m.editors, ed)
		idx := len(m.editors) - 1
		m.tabBar.AddTab(filepath.Base(tab.FilePath), tab.FilePath)
		if tab.Pinned {
			m.tabBar.PinTab(idx)
		}
		if savedIndex == state.ActiveTab {
			m.activeTab = idx
		}
		restoredPaths[filepath.Clean(tab.FilePath)] = struct{}{}
		m.nextFileLoadID++
		requestID := m.nextFileLoadID
		if m.pendingFileLoads == nil {
			m.pendingFileLoads = make(map[uint64]pendingFileLoad)
		}
		loadCtx, cancel := context.WithCancel(context.Background())
		tabCopy := tab
		m.pendingFileLoads[requestID] = pendingFileLoad{
			ID:          requestID,
			Path:        tab.FilePath,
			EditorID:    ed.ID(),
			BaseVersion: ed.Buffer.Version(),
			BaseRope:    ed.Buffer.Rope(),
			Session:     &tabCopy,
			Cancel:      cancel,
		}
		path, snapshot, editorID := tab.FilePath, restored.Snapshot, ed.ID()
		restored.Snapshot = nil // transfer ownership to the command closure
		cmds = append(cmds, func() tea.Msg {
			if err := loadCtx.Err(); err != nil {
				return FileLoadErrorMsg{Path: path, EditorID: editorID, RequestID: requestID, Err: err}
			}
			return FileLoadedMsg{Path: path, Snapshot: snapshot, EditorID: editorID, RequestID: requestID}
		})
	}
	releaseRestoredSessionFiles(files)

	// Append CLI-opened files that were not part of the saved session.
	for _, sf := range m.startupFiles {
		if sf.Path == "" {
			continue
		}
		clean := filepath.Clean(sf.Path)
		if _, ok := restoredPaths[clean]; ok {
			continue
		}
		if m.tabBar.FindTab(sf.Path) >= 0 || m.tabBar.FindTab(clean) >= 0 {
			continue
		}
		buf := text.NewBuffer()
		buf.FilePath = sf.Path
		cfg := editor.DefaultConfig()
		cfg.TabSize = m.appCfg.Editor.TabSize
		cfg.InsertTabs = m.appCfg.Editor.InsertTabs
		cfg.AutoIndent = m.appCfg.Editor.AutoIndent
		cfg.CommentPrefix = editor.CommentPrefixForFile(sf.Path)
		ed := editor.New(buf, m.theme, cfg)
		m.editors = append(m.editors, ed)
		idx := len(m.editors) - 1
		m.tabBar.AddTab(filepath.Base(sf.Path), sf.Path)
		m.tabBar.PinTab(idx)
		if sf.Line >= 0 {
			if m.startupCursors == nil {
				m.startupCursors = make(map[string]text.Position)
			}
			m.startupCursors[clean] = text.Position{Line: sf.Line, Col: max(0, sf.Col)}
		}
		cmds = append(cmds, m.startFileLoad(sf.Path, ed, false, nil))
	}

	// Prefer the first CLI file as the active tab when the user launched with paths.
	if len(m.startupFiles) > 0 && m.startupFiles[0].Path != "" {
		want := filepath.Clean(m.startupFiles[0].Path)
		for i, ed := range m.editors {
			if filepath.Clean(ed.Buffer.FilePath) == want {
				m.activeTab = i
				break
			}
		}
	}

	if len(m.editors) > 0 {
		if m.activeTab < 0 || m.activeTab >= len(m.editors) {
			m.activeTab = 0
		}
		m.activateTab(m.activeTab)
		m.focus = FocusEditor
	}
	return tea.Batch(cmds...)
}
