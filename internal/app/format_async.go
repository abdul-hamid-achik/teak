package app

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/text"
)

const maxConcurrentFormatPreparations = 16

type formatPreparationTracker struct {
	next uint64
	jobs map[uint64]context.CancelFunc
}

func (t *formatPreparationTracker) begin() (uint64, context.Context, bool) {
	if len(t.jobs) >= maxConcurrentFormatPreparations {
		return 0, nil, false
	}
	if t.jobs == nil {
		t.jobs = make(map[uint64]context.CancelFunc)
	}
	t.next++
	ctx, cancel := context.WithCancel(context.Background())
	t.jobs[t.next] = cancel
	return t.next, ctx, true
}

func (t *formatPreparationTracker) take(generation uint64) bool {
	cancel, ok := t.jobs[generation]
	if !ok {
		return false
	}
	delete(t.jobs, generation)
	cancel()
	return true
}

func (t *formatPreparationTracker) cancelAll() {
	for generation, cancel := range t.jobs {
		cancel()
		delete(t.jobs, generation)
	}
}

type formatPreparationRequest struct {
	Generation  uint64
	RequestID   int
	FilePath    string
	EditorID    uint64
	BaseVersion int
	Source      *text.Rope
	Cursor      text.Position
	Selections  []text.Selection
	Primary     int
}

type formatPreparedMsg struct {
	formatPreparationRequest
	Result           *text.Rope
	ResultCursor     text.Position
	ResultSelections []text.Selection
	ResultPrimary    int
	Applied          int
	Err              error
}

func prepareFormatCmd(ctx context.Context, request formatPreparationRequest, edits []lsp.TextEdit) tea.Cmd {
	return func() tea.Msg {
		prepared := formatPreparedMsg{formatPreparationRequest: request}
		if err := ctx.Err(); err != nil {
			prepared.Err = err
			return prepared
		}

		buffer := text.NewBufferFromRope(request.Source)
		buffer.SetCursor(request.Cursor)
		buffer.RestoreSelections(request.Selections, request.Primary)
		if err := validateFormattingTextEdits(buffer, edits); err != nil {
			prepared.Err = fmt.Errorf("invalid formatting edits: %w", err)
			return prepared
		}
		if err := ctx.Err(); err != nil {
			prepared.Err = err
			return prepared
		}

		prepared.Applied = applyTextEditsToBuffer(buffer, edits)
		if err := ctx.Err(); err != nil {
			prepared.Err = err
			return prepared
		}
		prepared.Result = buffer.Rope()
		prepared.ResultCursor = buffer.Cursor
		if buffer.Selections != nil {
			prepared.ResultSelections = append([]text.Selection(nil), buffer.Selections.All()...)
			prepared.ResultPrimary = buffer.Selections.PrimaryIndex()
		}
		return prepared
	}
}

func (m Model) handleFormatPrepared(msg formatPreparedMsg) (tea.Model, tea.Cmd) {
	if !m.formatPreparations.take(msg.Generation) {
		return m, nil
	}
	idx := m.editorIndexForAsyncMessage(msg.EditorID)
	if idx < 0 || m.editors[idx].Buffer.Version() != msg.BaseVersion || m.editors[idx].Buffer.Rope() != msg.Source {
		return m.discardPreparedFormat(msg.RequestID)
	}
	if msg.Err != nil {
		m.status = fmt.Sprintf("Formatting result rejected: %v", msg.Err)
		return m.finishFormatResult(lsp.FormatResultMsg{
			RequestID: msg.RequestID,
			FilePath:  msg.FilePath,
			Status:    lsp.FormatError,
			Err:       msg.Err,
		}, idx, nil)
	}

	status := lsp.FormatNoOp
	var postMutationCmd tea.Cmd
	if msg.Applied > 0 && msg.Result != nil && msg.Result != msg.Source {
		ed := &m.editors[idx]
		prevVersion, prevCursor := ed.Buffer.Version(), ed.Buffer.Cursor
		ed.Buffer.ReplaceRopeSnapshot(msg.Result, msg.ResultCursor)
		ed.Buffer.RestoreSelections(msg.ResultSelections, msg.ResultPrimary)
		if ed.Highlighter != nil {
			ed.Highlighter.Invalidate()
		}
		ed.SetSize(ed.Viewport.Width, ed.Viewport.Height)
		ed.EnsureCursorVisible()
		editorID, version := ed.ID(), ed.Buffer.Version()
		postMutationCmd = tea.Batch(
			func() tea.Msg { return editor.RetokenizeMsg{EditorID: editorID, Version: version} },
			m.syncEditorStateAfterUpdate(idx, prevVersion, prevCursor),
		)
		m.status = "Document formatted"
		status = lsp.FormatApplied
	}
	return m.finishFormatResult(lsp.FormatResultMsg{
		RequestID: msg.RequestID,
		FilePath:  msg.FilePath,
		Status:    status,
	}, idx, postMutationCmd)
}

func (m Model) discardPreparedFormat(requestID int) (tea.Model, tea.Cmd) {
	if requestID == 0 {
		m.status = "Formatting result discarded; newer edits remain unsaved"
		return m, nil
	}
	req, ok := m.pendingSaves[requestID]
	delete(m.pendingSaves, requestID)
	if ok && req.QueuedSnapshot != nil {
		m.status = "Formatting result discarded; formatting newer edits"
		return m, m.startQueuedSave(requestID, req)
	}
	m.status = "Formatting result discarded; newer edits remain unsaved"
	if ok && req.QuitAfter {
		m.cancelQuitAfterSaves()
	}
	return m, nil
}

func (m Model) finishFormatResult(msg lsp.FormatResultMsg, idx int, postMutationCmd tea.Cmd) (tea.Model, tea.Cmd) {
	if msg.RequestID == 0 {
		switch msg.Status {
		case lsp.FormatApplied:
			if idx >= 0 && idx == m.activeTab {
				m.setEditor(m.activeTab, m.editors[idx])
			}
		case lsp.FormatNoOp:
			m.status = "No formatting changes"
		case lsp.FormatUnsupported:
			m.status = "Formatting not supported"
		case lsp.FormatError:
			if msg.Err != nil {
				m.status = fmt.Sprintf("Formatting failed: %v", msg.Err)
			} else {
				m.status = "Formatting failed"
			}
		}
		return m, postMutationCmd
	}

	if msg.Status == lsp.FormatApplied && idx >= 0 {
		req := m.pendingSaves[msg.RequestID]
		req.Snapshot = m.editors[idx].Buffer.Rope()
		req.SnapshotVersion = m.editors[idx].Buffer.Version()
		req.LineEnding = m.editors[idx].Buffer.LineEnding()
		m.pendingSaves[msg.RequestID] = req
	}
	m.setPendingSaveNote(msg.RequestID, formatResultNote(msg.Status, msg.Err))
	return m, tea.Batch(postMutationCmd, m.startSaveRequest(msg.RequestID))
}
