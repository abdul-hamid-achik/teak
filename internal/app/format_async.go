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

		preparedEdits, err := prepareFormattingTextEdits(ctx, request.Source, edits)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				prepared.Err = ctxErr
				return prepared
			}
			prepared.Err = fmt.Errorf("invalid formatting edits: %w", err)
			return prepared
		}

		result, err := applyPreparedTextEdits(ctx, request.Source, preparedEdits)
		if err != nil {
			prepared.Err = err
			return prepared
		}
		resultCursor, resultSelections, resultPrimary, err := mapFormattingSelections(
			ctx,
			request.Source,
			result,
			request.Cursor,
			request.Selections,
			request.Primary,
			preparedEdits,
		)
		if err != nil {
			prepared.Err = err
			return prepared
		}

		prepared.Applied = len(preparedEdits)
		prepared.Result = result
		prepared.ResultCursor = resultCursor
		prepared.ResultSelections = resultSelections
		prepared.ResultPrimary = resultPrimary
		return prepared
	}
}

func applyPreparedTextEdits(ctx context.Context, source *text.Rope, edits []preparedTextEdit) (*text.Rope, error) {
	if len(edits) == 0 {
		return source, ctx.Err()
	}

	finalLen := int64(source.Len())
	for i, edit := range edits {
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		finalLen += int64(len(edit.newText)) - int64(edit.end-edit.start)
	}
	maxInt := int64(^uint(0) >> 1)
	if finalLen < 0 || finalLen > maxInt {
		return nil, fmt.Errorf("edited document size is not representable")
	}

	sourceBytes, err := source.BytesContext(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 0, int(finalLen))
	previousEnd := 0
	for i, edit := range edits {
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		result = append(result, sourceBytes[previousEnd:edit.start]...)
		result = append(result, edit.newText...)
		previousEnd = edit.end
	}
	result = append(result, sourceBytes[previousEnd:]...)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return text.NewOwned(result), nil
}

func mapFormattingSelections(
	ctx context.Context,
	source, result *text.Rope,
	cursor text.Position,
	selections []text.Selection,
	primary int,
	edits []preparedTextEdit,
) (text.Position, []text.Selection, int, error) {
	base := text.NewBufferFromRope(source)
	base.SetCursor(cursor)
	base.RestoreSelections(selections, primary)
	baseSelections := base.Selections.All()
	mapped := make([]text.Selection, len(baseSelections))
	for i, selection := range baseSelections {
		if i&255 == 0 {
			if err := ctx.Err(); err != nil {
				return text.Position{}, nil, 0, err
			}
		}
		mapped[i] = text.Selection{
			Anchor: mapPositionThroughTextEdits(source, result, selection.Anchor, edits),
			Head:   mapPositionThroughTextEdits(source, result, selection.Head, edits),
		}
	}

	normalized := text.NewBufferFromRope(result)
	normalized.RestoreSelections(mapped, base.Selections.PrimaryIndex())
	return normalized.Cursor,
		append([]text.Selection(nil), normalized.Selections.All()...),
		normalized.Selections.PrimaryIndex(),
		ctx.Err()
}

func mapPositionThroughTextEdits(source, result *text.Rope, position text.Position, edits []preparedTextEdit) text.Position {
	offset := source.PositionToOffset(position)
	delta := 0
	for _, edit := range edits {
		if offset < edit.start {
			break
		}
		if offset < edit.end {
			return result.OffsetToPosition(edit.start + delta + len(edit.newText))
		}
		delta += len(edit.newText) - (edit.end - edit.start)
	}
	return result.OffsetToPosition(offset + delta)
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
