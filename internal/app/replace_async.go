package app

import (
	"bytes"
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/text"
)

type replaceAsyncState struct {
	generation uint64
	cancel     context.CancelFunc
}

type replacePreparation struct {
	Generation  uint64
	EditorID    uint64
	Version     int
	Source      *text.Rope
	Cursor      text.Position
	Query       string
	Replacement string
	All         bool
}

type replacePreparedMsg struct {
	Preparation replacePreparation
	Result      *text.Rope
	Cursor      text.Position
	Matches     int
	LimitHit    bool
	Err         error
}

func (m *Model) startSearchReplace(query, replacement string, all bool) tea.Cmd {
	ed := m.activeEditor()
	if ed == nil || ed.Buffer == nil || query == "" {
		return nil
	}
	if all && ed.Buffer.Rope().Len() > maxReplaceAllBytes {
		m.status = "Replace All is limited to files up to 8 MiB"
		return nil
	}
	if m.replaces.cancel != nil {
		m.replaces.cancel()
	}
	m.replaces.generation++
	ctx, cancel := context.WithCancel(context.Background())
	m.replaces.cancel = cancel
	preparation := replacePreparation{
		Generation:  m.replaces.generation,
		EditorID:    ed.ID(),
		Version:     ed.Buffer.Version(),
		Source:      ed.Buffer.Rope(),
		Cursor:      ed.Buffer.Cursor,
		Query:       query,
		Replacement: replacement,
		All:         all,
	}
	return prepareReplaceCmd(ctx, preparation)
}

func prepareReplaceCmd(ctx context.Context, preparation replacePreparation) tea.Cmd {
	return func() tea.Msg {
		data, err := preparation.Source.BytesContext(ctx)
		if err != nil {
			return replacePreparedMsg{Preparation: preparation, Err: err}
		}
		if err := ctx.Err(); err != nil {
			return replacePreparedMsg{Preparation: preparation, Err: err}
		}

		if preparation.All {
			cursorOffset := preparation.Source.PositionToOffset(preparation.Cursor)
			replaced, mappedCursor, matches, ok := boundedReplaceAllAtOffset(
				string(data),
				preparation.Query,
				preparation.Replacement,
				cursorOffset,
			)
			if !ok {
				return replacePreparedMsg{Preparation: preparation, Matches: matches, LimitHit: true}
			}
			if matches == 0 {
				return replacePreparedMsg{Preparation: preparation}
			}
			result := text.NewFromString(replaced)
			cursor := result.OffsetToPosition(mappedCursor)
			return replacePreparedMsg{
				Preparation: preparation,
				Result:      result,
				Cursor:      cursor,
				Matches:     matches,
			}
		}

		query := []byte(preparation.Query)
		cursorOffset := preparation.Source.PositionToOffset(preparation.Cursor)
		matchOffset := bytes.Index(data[cursorOffset:], query)
		if matchOffset < 0 {
			matchOffset = bytes.Index(data[:cursorOffset], query)
			if matchOffset < 0 {
				return replacePreparedMsg{Preparation: preparation}
			}
		} else {
			matchOffset += cursorOffset
		}
		if err := ctx.Err(); err != nil {
			return replacePreparedMsg{Preparation: preparation, Err: err}
		}
		result := preparation.Source.Delete(matchOffset, len(query)).
			Insert(matchOffset, []byte(preparation.Replacement))
		cursor := result.OffsetToPosition(matchOffset + len(preparation.Replacement))
		return replacePreparedMsg{
			Preparation: preparation,
			Result:      result,
			Cursor:      cursor,
			Matches:     1,
		}
	}
}

func (m Model) handleReplacePrepared(msg replacePreparedMsg) (tea.Model, tea.Cmd) {
	if msg.Preparation.Generation != m.replaces.generation {
		return m, nil
	}
	if m.replaces.cancel != nil {
		m.replaces.cancel()
		m.replaces.cancel = nil
	}
	if msg.Err != nil {
		if !errors.Is(msg.Err, context.Canceled) {
			m.status = "Replace failed: " + msg.Err.Error()
		}
		return m, nil
	}
	if msg.LimitHit {
		m.status = "Replace All exceeded its match or 64 MiB result limit"
		return m, nil
	}
	if msg.Matches == 0 || msg.Result == nil {
		return m, nil
	}

	index := m.editorIndexForAsyncMessage(msg.Preparation.EditorID)
	if index < 0 {
		return m, nil
	}
	ed := &m.editors[index]
	if ed.Buffer.Version() != msg.Preparation.Version || ed.Buffer.Rope() != msg.Preparation.Source {
		m.status = "Replace discarded: buffer changed while it was prepared"
		return m, nil
	}

	prevVersion, prevCursor := ed.Buffer.Version(), ed.Buffer.Cursor
	ed.Buffer.ReplaceRopeSnapshot(msg.Result, msg.Cursor)
	if ed.Highlighter != nil {
		ed.Highlighter.Invalidate()
	}
	ed.SetSize(ed.Viewport.Width, ed.Viewport.Height)
	ed.EnsureCursorVisible()
	editorID, version := ed.ID(), ed.Buffer.Version()
	return m, tea.Batch(
		func() tea.Msg { return editor.RetokenizeMsg{EditorID: editorID, Version: version} },
		m.syncEditorStateAfterUpdate(index, prevVersion, prevCursor),
	)
}
