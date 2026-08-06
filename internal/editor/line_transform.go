package editor

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"teak/internal/text"
)

const maxQueuedLineTransforms = 32

func isLineTransformKey(key string) bool {
	switch key {
	case "alt+up", "alt+down", "alt+shift+up", "alt+shift+down", "ctrl+shift+k":
		return true
	default:
		return false
	}
}

// LineTransformPreparedMsg carries a non-local line edit prepared from one
// immutable buffer snapshot. EditorID, Generation, Version, Snapshot, and the
// selection snapshot together prevent a late result from rewriting a buffer
// after typing, navigation, reload, or a newer structural command.
type LineTransformPreparedMsg struct {
	EditorID         uint64
	Generation       uint64
	Version          int
	Snapshot         *text.Rope
	Rope             *text.Rope
	SourceSelections []text.Selection
	SourcePrimary    int
	Selections       []text.Selection
	Primary          int
	Transform        text.LineTransform
	Changed          bool
	Err              error
}

func prepareLineTransformCmd(
	ctx context.Context,
	editorID, generation uint64,
	version int,
	snapshot *text.Rope,
	selections []text.Selection,
	primary int,
	transform text.LineTransform,
) tea.Cmd {
	return func() tea.Msg {
		result := LineTransformPreparedMsg{
			EditorID: editorID, Generation: generation, Version: version,
			Snapshot: snapshot, Transform: transform,
			SourceSelections: append([]text.Selection(nil), selections...), SourcePrimary: primary,
		}
		prepared, err := text.PrepareLineTransform(ctx, snapshot, selections, primary, transform)
		if err != nil {
			result.Err = err
			return result
		}
		result.Rope = prepared.Rope
		result.Selections = prepared.Selections
		result.Primary = prepared.Primary
		result.Changed = prepared.Changed
		return result
	}
}

func (e *Editor) requestLineTransform(transform text.LineTransform) tea.Cmd {
	if e.Buffer == nil {
		return nil
	}
	if e.lineTransformPending {
		if len(e.lineTransformQueue) < maxQueuedLineTransforms {
			e.lineTransformQueue = append(e.lineTransformQueue, transform)
		}
		return nil
	}
	if e.Buffer.Selections == nil || e.Buffer.Selections.Count() == 0 {
		e.Buffer.RestoreSelections([]text.Selection{{Anchor: e.Buffer.Cursor, Head: e.Buffer.Cursor}}, 0)
	}
	e.Buffer.Selections.Normalize()
	selections := append([]text.Selection(nil), e.Buffer.Selections.All()...)
	primary := e.Buffer.Selections.PrimaryIndex()
	snapshot := e.Buffer.Rope()
	e.lineTransformGeneration++
	e.lineTransformPending = true
	ctx, cancel := context.WithCancel(context.Background())
	e.lineTransformCancel = cancel
	return prepareLineTransformCmd(ctx, e.id, e.lineTransformGeneration, e.Buffer.Version(), snapshot, selections, primary, transform)
}

func (e *Editor) cancelLineTransforms() {
	if e.lineTransformCancel != nil {
		e.lineTransformCancel()
	}
	e.lineTransformCancel = nil
	e.lineTransformPending = false
	e.lineTransformQueue = nil
	e.lineTransformGeneration++
}

func (e *Editor) handleLineTransformPrepared(msg LineTransformPreparedMsg) (Editor, tea.Cmd) {
	if msg.EditorID != e.id || !e.lineTransformPending || msg.Generation != e.lineTransformGeneration {
		return *e, nil
	}
	e.lineTransformPending = false
	e.lineTransformCancel = nil
	if msg.Err != nil || msg.Version != e.Buffer.Version() || msg.Snapshot != e.Buffer.Rope() ||
		msg.Rope == nil || msg.Primary < 0 || msg.Primary >= len(msg.Selections) ||
		!e.matchesSelectionSnapshot(msg.SourceSelections, msg.SourcePrimary) {
		e.lineTransformQueue = nil
		return *e, nil
	}

	changed := msg.Changed && msg.Rope != msg.Snapshot
	if changed {
		e.Buffer.ReplaceRopeSnapshot(msg.Rope, msg.Selections[msg.Primary].Head)
		e.Buffer.RestoreSelections(msg.Selections, msg.Primary)
		e.refreshWordWrapAfterBufferChange()
		e.EnsureCursorVisible()
		if e.Highlighter != nil {
			e.Highlighter.Invalidate()
		}
	}

	var next tea.Cmd
	if len(e.lineTransformQueue) > 0 {
		nextTransform := e.lineTransformQueue[0]
		e.lineTransformQueue = e.lineTransformQueue[1:]
		next = e.requestLineTransform(nextTransform)
	}
	if changed {
		return *e, tea.Batch(e.scheduleRetokenize(), next)
	}
	return *e, next
}
