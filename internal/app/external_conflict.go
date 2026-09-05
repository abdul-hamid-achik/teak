package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/overlay"
	"teak/internal/plugin"
	"teak/internal/text"
)

type externalConflictResolution uint8

const (
	externalConflictReload externalConflictResolution = iota
	externalConflictOverwrite
	externalConflictCancel
)

// externalConflictResolutionMsg is intentionally private: it only originates
// from the editor's confirmation dialog, never from an untrusted filesystem
// event or plugin payload.
type externalConflictResolutionMsg struct {
	Path       string
	Resolution externalConflictResolution
}

type externalFileConflict struct {
	Snapshot    *text.Rope
	Generation  uint64
	Observation uint64
	Missing     bool
	PostSave    bool
}

const maxExternalConflictBytes = 128 << 20

type externalConflictReloadPreparedMsg struct {
	Path        string
	Generation  uint64
	EditorID    uint64
	BaseVersion int
	BaseRope    *text.Rope
	Snapshot    *text.Rope
	Err         error
}

type externalFoldRegionsPreparedMsg struct {
	EditorID uint64
	Version  int
	Regions  []editor.FoldRegion
}

func prepareExternalFileChangedCmd(msg FileChangedMsg, ctx context.Context) tea.Cmd {
	path := msg.Path
	observation := msg.Observation
	needsRead := msg.NeedsRead
	data := msg.Data
	return func() tea.Msg {
		if needsRead {
			var err error
			data, err = readEditorFile(ctx, path)
			if err != nil {
				return FileLoadErrorMsg{Path: path, Err: err}
			}
		}
		var snapshot *text.Rope
		var ending text.LineEnding
		if needsRead {
			// This branch read a fresh allocation itself. Legacy FileChangedMsg
			// Data stays defensive because its sender may retain the slice.
			normalized, detected := text.NormalizeLineEndings(data)
			snapshot = text.NewOwned(normalized)
			ending = detected
		} else {
			normalized, detected := text.NormalizeLineEndings(data)
			snapshot = text.New(normalized)
			ending = detected
		}
		return FileChangedMsg{
			Path:              path,
			Snapshot:          snapshot,
			LineEnding:        ending,
			Observation:       observation,
			Missing:           msg.Missing,
			RequiresConflict:  msg.RequiresConflict,
			OwnWriteCandidate: msg.OwnWriteCandidate,
			OwnWriteSnapshot:  msg.OwnWriteSnapshot,
			OwnWriteVerified:  msg.OwnWriteVerified,
		}
	}
}

func cleanExternalConflictPath(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func (m Model) hasExternalConflict(path string) bool {
	_, ok := m.externalConflicts[cleanExternalConflictPath(path)]
	return ok
}

func (m *Model) recordExternalConflict(path string, snapshot *text.Rope, observation uint64, missing, postSave bool) {
	if m.externalConflicts == nil {
		m.externalConflicts = make(map[string]externalFileConflict)
	}
	clean := cleanExternalConflictPath(path)
	if previous, ok := m.externalConflicts[clean]; ok && previous.Snapshot != nil {
		m.externalConflictBytes -= previous.Snapshot.Len()
	}
	m.externalConflictGen++
	if snapshot != nil && snapshot.Len() <= maxExternalConflictBytes-m.externalConflictBytes {
		m.externalConflictBytes += snapshot.Len()
	} else {
		// Retain the conflict decision but not unbounded document snapshots.
		// Choosing Reload will re-read the latest disk version asynchronously.
		snapshot = nil
	}
	m.externalConflicts[clean] = externalFileConflict{
		Snapshot:    snapshot,
		Generation:  m.externalConflictGen,
		Observation: observation,
		Missing:     missing,
		PostSave:    postSave,
	}
}

func (m *Model) clearExternalConflict(path string) {
	clean := cleanExternalConflictPath(path)
	if conflict, ok := m.externalConflicts[clean]; ok && conflict.Snapshot != nil {
		m.externalConflictBytes -= conflict.Snapshot.Len()
	}
	delete(m.externalConflicts, clean)
	if m.externalConflictPrompt == clean {
		m.externalConflictPrompt = ""
		m.unsavedConfirm = nil
	}
}

func (m Model) showExternalConflictConfirmation(path string) Model {
	if m.unsavedConfirm != nil {
		return m
	}
	name := filepath.Base(path)
	title := "File Changed on Disk"
	message := fmt.Sprintf("%q has local edits and changed outside Teak.", name)
	details := []string{
		"Reload discards the local edits and uses the external version.",
		"Overwrite writes the local edits to disk.",
	}
	reloadLabel := "Reload External"
	if conflict, ok := m.externalConflicts[cleanExternalConflictPath(path)]; ok {
		switch {
		case conflict.PostSave:
			title = "Save Overlapped an External Change"
			message = fmt.Sprintf("%q changed externally while Teak was saving.", name)
			details = []string{
				"Restore External preserves the observed bytes as unsaved edits.",
				"Overwrite explicitly keeps the local saved version.",
			}
			reloadLabel = "Restore External"
		case conflict.Missing:
			title = "File Removed on Disk"
			message = fmt.Sprintf("%q was removed or renamed outside Teak.", name)
			details = []string{
				"Retry Reload checks whether an atomic replacement now exists.",
				"Overwrite explicitly recreates the path from the editor buffer.",
			}
			reloadLabel = "Retry Reload"
		}
	}
	m.unsavedConfirm = overlay.NewConfirm(
		title,
		message,
		details,
		[]overlay.Button{
			{Label: reloadLabel, Style: m.dangerButtonStyle(), Action: externalConflictResolutionMsg{Path: path, Resolution: externalConflictReload}},
			{Label: "Overwrite", Style: m.primaryButtonStyle(), Action: externalConflictResolutionMsg{Path: path, Resolution: externalConflictOverwrite}},
			{Label: "Cancel", Action: externalConflictResolutionMsg{Path: path, Resolution: externalConflictCancel}},
		},
		m.theme,
	)
	m.externalConflictPrompt = cleanExternalConflictPath(path)
	return m
}

func (m Model) handleExternalConflictResolution(msg externalConflictResolutionMsg) (tea.Model, tea.Cmd) {
	path := cleanExternalConflictPath(msg.Path)
	conflict, exists := m.externalConflicts[path]
	if !exists {
		return m, nil
	}
	m.unsavedConfirm = nil
	m.externalConflictPrompt = ""
	switch msg.Resolution {
	case externalConflictReload:
		if conflict.Snapshot == nil {
			index := m.findEditorByPath(path)
			if index < 0 {
				m.clearExternalConflict(path)
				m.status = fmt.Sprintf("Cannot reload closed file: %s", filepath.Base(path))
				return m, nil
			}
			buffer := m.editors[index].Buffer
			m.status = fmt.Sprintf("Reloading external version: %s", filepath.Base(path))
			return m, prepareExternalConflictReloadCmd(
				m.externalBackgroundContext(),
				path,
				conflict.Generation,
				m.editors[index].ID(),
				buffer.Version(),
				buffer.Rope(),
			)
		}
		m.clearExternalConflict(path)
		if conflict.PostSave {
			return m.restoreExternalSnapshotAsEdit(path, conflict.Snapshot)
		}
		return m.reloadExternalFile(path, conflict.Snapshot)
	case externalConflictOverwrite:
		for i := range m.editors {
			if cleanExternalConflictPath(m.editors[i].Buffer.FilePath) != path {
				continue
			}
			m.status = fmt.Sprintf("Overwriting external change: %s", filepath.Base(path))
			return m, m.beginSaveSnapshotForTabAuthorized(
				i,
				path,
				false,
				"",
				false,
				false,
				conflict.Generation,
			)
		}
		m.status = fmt.Sprintf("Cannot overwrite closed file: %s", filepath.Base(path))
		return m, nil
	default:
		m.status = fmt.Sprintf("Save blocked until external change is resolved: %s", filepath.Base(path))
		return m, nil
	}
}

func prepareExternalConflictReloadCmd(
	ctx context.Context,
	path string,
	generation uint64,
	editorID uint64,
	baseVersion int,
	baseRope *text.Rope,
) tea.Cmd {
	return func() tea.Msg {
		data, err := readEditorFile(ctx, path)
		if err != nil {
			return externalConflictReloadPreparedMsg{
				Path: path, Generation: generation, EditorID: editorID,
				BaseVersion: baseVersion, BaseRope: baseRope, Err: err,
			}
		}
		normalized, _ := text.NormalizeLineEndings(data)
		return externalConflictReloadPreparedMsg{
			Path: path, Generation: generation, EditorID: editorID,
			BaseVersion: baseVersion, BaseRope: baseRope, Snapshot: text.NewOwned(normalized),
		}
	}
}

func (m Model) handleExternalConflictReloadPrepared(msg externalConflictReloadPreparedMsg) (tea.Model, tea.Cmd) {
	path := cleanExternalConflictPath(msg.Path)
	conflict, ok := m.externalConflicts[path]
	if !ok || conflict.Generation != msg.Generation {
		return m, nil
	}
	index := m.editorIndexForAsyncMessage(msg.EditorID)
	if index < 0 || cleanExternalConflictPath(m.editors[index].Buffer.FilePath) != path {
		m.clearExternalConflict(path)
		return m, nil
	}
	buffer := m.editors[index].Buffer
	if buffer.Version() != msg.BaseVersion || buffer.Rope() != msg.BaseRope {
		m.status = fmt.Sprintf("Kept newer local edits; %s still conflicts with disk", filepath.Base(path))
		m = m.showExternalConflictConfirmation(path)
		return m, nil
	}
	if msg.Err != nil {
		if errors.Is(msg.Err, context.Canceled) {
			return m, nil
		}
		m.status = fmt.Sprintf("External reload failed for %s: %v", filepath.Base(path), msg.Err)
		m = m.showExternalConflictConfirmation(path)
		return m, nil
	}
	m.clearExternalConflict(path)
	if conflict.PostSave {
		return m.restoreExternalSnapshotAsEdit(path, msg.Snapshot)
	}
	return m.reloadExternalFile(path, msg.Snapshot)
}

func prepareExternalFoldRegionsCmd(editorID uint64, version int, snapshot *text.Rope) tea.Cmd {
	return func() tea.Msg {
		if snapshot == nil {
			return externalFoldRegionsPreparedMsg{EditorID: editorID, Version: version}
		}
		return externalFoldRegionsPreparedMsg{
			EditorID: editorID,
			Version:  version,
			Regions:  editor.DetectIndentRegions(snapshot.Line, snapshot.LineCount()),
		}
	}
}

func (m Model) handleExternalFoldRegionsPrepared(msg externalFoldRegionsPreparedMsg) (tea.Model, tea.Cmd) {
	index := m.editorIndexForAsyncMessage(msg.EditorID)
	if index < 0 || m.editors[index].Buffer.Version() != msg.Version {
		return m, nil
	}
	m.editors[index].Folds.SetRegions(msg.Regions)
	return m, nil
}

func (m Model) reloadExternalFile(path string, snapshot *text.Rope) (tea.Model, tea.Cmd) {
	return m.applyExternalSnapshot(path, snapshot, true)
}

// restoreExternalSnapshotAsEdit preserves an external snapshot that raced with
// a completed Teak save. The disk now contains the saved local snapshot, so
// treating the older external bytes as a clean reload would lie about the save
// baseline. Restore them as an undoable dirty edit instead.
func (m Model) restoreExternalSnapshotAsEdit(path string, snapshot *text.Rope) (tea.Model, tea.Cmd) {
	return m.applyExternalSnapshot(path, snapshot, false)
}

func (m Model) applyExternalSnapshot(path string, snapshot *text.Rope, clean bool) (tea.Model, tea.Cmd) {
	if snapshot == nil {
		return m, nil
	}
	var cmds []tea.Cmd
	for i := range m.editors {
		if cleanExternalConflictPath(m.editors[i].Buffer.FilePath) != path {
			continue
		}
		prevVersion := m.editors[i].Buffer.Version()
		prevCursor := m.editors[i].Buffer.Cursor
		m.editors[i].InvalidateClipboardPaste()
		if clean {
			m.editors[i].Buffer.LoadRopeSnapshot(snapshot, m.editors[i].Buffer.LineEnding())
		} else {
			m.editors[i].Buffer.ReplaceRopeSnapshot(snapshot, text.Position{})
		}
		if m.editors[i].Highlighter != nil {
			m.editors[i].Highlighter.Invalidate()
		}
		m.editors[i].Folds.SetRegions(nil)
		m.editors[i].SetSize(m.editors[i].Viewport.Width, m.editors[i].Viewport.Height)
		m.editors[i].EnsureCursorVisible()
		version := m.editors[i].Buffer.Version()
		editorID := m.editors[i].ID()
		cmds = append(cmds,
			m.editors[i].ScheduleInitialTokenize(),
			prepareExternalFoldRegionsCmd(editorID, version, snapshot),
			m.syncEditorStateAfterUpdate(i, prevVersion, prevCursor),
			m.triggerPluginEvents(
				m.pluginEvent(plugin.EventBufRead, path),
				m.pluginEvent(plugin.EventFileType, path),
			),
		)
	}
	if clean {
		m.status = fmt.Sprintf("Reloaded: %s (external change)", filepath.Base(path))
	} else {
		m.status = fmt.Sprintf("Restored raced external snapshot as unsaved edits: %s", filepath.Base(path))
	}
	return m, tea.Batch(cmds...)
}
