package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
)

type externalFileReader func(context.Context, string) ([]byte, error)

type externalReadState struct {
	ctx      context.Context
	cancel   context.CancelFunc
	inFlight bool
	current  FileChangedMsg
	pending  map[string]FileChangedMsg
	order    []string
}

type externalFileReadPreparedMsg struct {
	Change        FileChangedMsg
	Snapshot      *text.Rope
	LineEnding    text.LineEnding
	Err           error
	OwnWriteMatch bool
}

func (m *Model) externalBackgroundContext() context.Context {
	if m.externalReads.ctx == nil {
		m.externalReads.ctx, m.externalReads.cancel = context.WithCancel(context.Background())
	}
	return m.externalReads.ctx
}

func newerExternalChange(candidate, current FileChangedMsg) bool {
	if candidate.Observation == 0 || current.Observation == 0 {
		return true
	}
	return candidate.Observation >= current.Observation
}

// enqueueExternalFileRead keeps only the newest unread observation per open
// path and starts at most one bounded (64 MiB) editor read. This prevents a
// watcher overflow or snapshot-budget fallback from launching N large reads in
// parallel through tea.Batch.
func (m *Model) enqueueExternalFileRead(change FileChangedMsg) tea.Cmd {
	path := cleanExternalConflictPath(change.Path)
	if path == "" || m.findEditorByPath(path) < 0 {
		return nil
	}
	change.Path = path
	change.Data = nil
	change.Snapshot = nil
	change.NeedsRead = true
	if m.externalReads.pending == nil {
		m.externalReads.pending = make(map[string]FileChangedMsg)
	}
	if previous, exists := m.externalReads.pending[path]; exists {
		if newerExternalChange(change, previous) {
			m.externalReads.pending[path] = change
		}
		return m.startNextExternalFileRead()
	}
	m.externalReads.pending[path] = change
	m.externalReads.order = append(m.externalReads.order, path)
	return m.startNextExternalFileRead()
}

func (m *Model) startNextExternalFileRead() tea.Cmd {
	if m.externalReads.inFlight {
		return nil
	}
	for len(m.externalReads.order) > 0 {
		path := m.externalReads.order[0]
		m.externalReads.order = m.externalReads.order[1:]
		change, ok := m.externalReads.pending[path]
		if !ok {
			continue
		}
		delete(m.externalReads.pending, path)
		if m.findEditorByPath(path) < 0 {
			continue
		}
		m.externalReads.inFlight = true
		m.externalReads.current = change
		ctx := m.externalBackgroundContext()
		reader := m.externalFileReader
		if reader == nil {
			reader = readEditorFile
		}
		return func() tea.Msg {
			data, err := reader(ctx, path)
			result := externalFileReadPreparedMsg{Change: change, Err: err}
			if err == nil {
				// The serialized reader transfers its freshly-read bytes directly
				// to the immutable snapshot. Normalize CRLF first: buffer
				// snapshots are LF-only, and the own-write comparison below
				// matches against a normalized saved snapshot.
				normalized, ending := text.NormalizeLineEndings(data)
				result.Snapshot = text.NewOwned(normalized)
				result.LineEnding = ending
				if change.OwnWriteSnapshot != nil {
					result.OwnWriteMatch = change.OwnWriteSnapshot.EqualBytes(normalized)
				}
			}
			return result
		}
	}
	return nil
}

func (m Model) handleExternalFileReadPrepared(msg externalFileReadPreparedMsg) (tea.Model, tea.Cmd) {
	path := cleanExternalConflictPath(msg.Change.Path)
	current := m.externalReads.current
	if !m.externalReads.inFlight ||
		cleanExternalConflictPath(current.Path) != path ||
		current.Observation != msg.Change.Observation {
		return m, nil
	}
	m.externalReads.inFlight = false
	m.externalReads.current = FileChangedMsg{}

	// A newer observation for this path arrived while the read was in flight.
	// Never flash/apply the stale snapshot; advance directly to the latest one.
	if pending, ok := m.externalReads.pending[path]; ok && newerExternalChange(pending, msg.Change) {
		return m, m.startNextExternalFileRead()
	}

	change := msg.Change
	if msg.OwnWriteMatch {
		// An atomic save can report Remove/Rename before Create. Once the
		// delayed re-read proves that disk contains the exact bytes Teak wrote,
		// this observation is not an external conflict even though its watcher
		// order is newer than the save watermark.
		change.RequiresConflict = false
		change.OwnWriteVerified = true
	}
	if change.Observation != 0 {
		// Ordering can advance while the serialized read is blocked. Recheck at
		// completion so an older result (including an unreadable one) cannot
		// replace or obscure a newer applied observation, and so a save-covered
		// result is always presented as an explicit post-save conflict.
		if change.Observation <= m.externalChangeObserved[path] {
			return m, m.startNextExternalFileRead()
		}
		if !msg.OwnWriteMatch && change.Observation <= m.lastSaveWatcherWatermarks[path] {
			change.RequiresConflict = true
		}
	}

	var applyCmd tea.Cmd
	switch {
	case msg.Err == nil:
		change.Snapshot = msg.Snapshot
		change.LineEnding = msg.LineEnding
		change.NeedsRead = false
		change.Missing = false
		updated, cmd := m.applyPreparedExternalFileChange(change)
		m = updated.(Model)
		applyCmd = cmd
	case errors.Is(msg.Err, context.Canceled):
		// Shutdown owns cancellation; no status or conflict should be created.
	case errors.Is(msg.Err, os.ErrNotExist):
		change.NeedsRead = false
		change.Missing = true
		updated, cmd := m.applyPreparedExternalFileChange(change)
		m = updated.(Model)
		applyCmd = cmd
	default:
		if change.Observation != 0 {
			m.externalChangeObserved[path] = max(
				m.externalChangeObserved[path],
				change.Observation,
			)
		}
		if m.findEditorByPath(path) >= 0 {
			m.recordExternalConflict(path, nil, change.Observation, false, change.RequiresConflict)
			m = m.showExternalConflictConfirmation(path)
			m.status = fmt.Sprintf(
				"Could not verify external change for %s: %v",
				filepath.Base(path),
				msg.Err,
			)
		}
	}

	return m, tea.Batch(applyCmd, m.startNextExternalFileRead())
}
