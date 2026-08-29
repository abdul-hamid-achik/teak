package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/search"
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
	Opts        search.SearchOpts
}

type replacePreparedMsg struct {
	Preparation replacePreparation
	Result      *text.Rope
	Cursor      text.Position
	Matches     int
	LimitHit    bool
	Err         error
}

func (m *Model) startSearchReplace(query, replacement string, all bool, opts search.SearchOpts) tea.Cmd {
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
		Opts:        opts,
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
		pattern, err := search.CompilePattern(preparation.Query, preparation.Opts)
		if err != nil {
			return replacePreparedMsg{Preparation: preparation, Err: err}
		}
		content := string(data)

		if preparation.All {
			cursorOffset := preparation.Source.PositionToOffset(preparation.Cursor)
			replacement := preparation.Replacement
			if !preparation.Opts.Regex {
				// regexp.ExpandString interprets $-references. Escape them for
				// literal replacement mode so replacing text with "$1" remains
				// literal, while regex mode retains capture expansion.
				replacement = strings.ReplaceAll(replacement, "$", "$$")
			}
			replaced, mappedCursor, matches, ok := boundedReplaceAllRegexAtOffset(
				content, pattern, replacement, cursorOffset,
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

		cursorOffset := preparation.Source.PositionToOffset(preparation.Cursor)
		cursorOffset = min(max(0, cursorOffset), len(content))
		match := pattern.FindStringSubmatchIndex(content[cursorOffset:])
		if match == nil {
			match = pattern.FindStringSubmatchIndex(content[:cursorOffset])
			if match == nil {
				return replacePreparedMsg{Preparation: preparation}
			}
		} else {
			for index := range match {
				if match[index] >= 0 {
					match[index] += cursorOffset
				}
			}
		}
		matchOffset := match[0]
		matchEnd := match[1]
		replacement := preparation.Replacement
		if preparation.Opts.Regex {
			replacement = string(pattern.ExpandString(nil, replacement, content, match))
		}
		if err := ctx.Err(); err != nil {
			return replacePreparedMsg{Preparation: preparation, Err: err}
		}
		result := preparation.Source.Delete(matchOffset, matchEnd-matchOffset).
			Insert(matchOffset, []byte(replacement))
		cursor := result.OffsetToPosition(matchOffset + len(replacement))
		return replacePreparedMsg{
			Preparation: preparation,
			Result:      result,
			Cursor:      cursor,
			Matches:     1,
		}
	}
}

// boundedReplaceAllRegexAtOffset replaces at most maxReplaceAllMatches
// non-overlapping regexp matches and caps the resulting document before it is
// materialized. It also maps an original byte offset through replacements so
// the editor can restore the cursor after an asynchronous edit.
func boundedReplaceAllRegexAtOffset(content string, pattern *regexp.Regexp, replacement string, originalOffset int) (result string, mappedOffset, matches int, ok bool) {
	if pattern == nil {
		return "", 0, 0, false
	}
	originalOffset = min(max(0, originalOffset), len(content))
	locations := pattern.FindAllStringSubmatchIndex(content, maxReplaceAllMatches+1)
	matches = len(locations)
	if matches > maxReplaceAllMatches {
		return "", 0, matches, false
	}

	var builder strings.Builder
	last := 0
	mapped := false
	for _, location := range locations {
		if len(location) < 2 {
			continue
		}
		start, end := location[0], location[1]
		expanded := pattern.ExpandString(nil, replacement, content, location)
		pending := start - last + len(expanded)
		if pending > maxReplaceResultBytes || builder.Len() > maxReplaceResultBytes-pending {
			return "", 0, matches, false
		}
		beforeMatch := builder.Len() + start - last
		if !mapped {
			switch {
			case originalOffset <= start:
				mappedOffset = builder.Len() + originalOffset - last
				mapped = true
			case originalOffset < end:
				mappedOffset = beforeMatch + min(originalOffset-start, len(expanded))
				mapped = true
			}
		}
		builder.WriteString(content[last:start])
		_, _ = builder.Write(expanded)
		last = end
	}
	if builder.Len() > maxReplaceResultBytes-len(content[last:]) {
		return "", 0, matches, false
	}
	builder.WriteString(content[last:])
	if !mapped {
		mappedOffset = builder.Len() - (len(content) - originalOffset)
	}
	return builder.String(), mappedOffset, matches, true
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
	// The panel lists project-wide results, but replace only rewrites the
	// active editor's buffer. Say so, with the file name and match count,
	// instead of leaving the user to guess which files changed.
	name := filepath.Base(ed.Buffer.FilePath)
	if name == "." || ed.Buffer.FilePath == "" {
		name = m.tabBar.Tabs[index].Label
	}
	noun := "match"
	if msg.Matches != 1 {
		noun = "matches"
	}
	m.status = fmt.Sprintf("Replaced %d %s in %s", msg.Matches, noun, name)
	editorID, version := ed.ID(), ed.Buffer.Version()
	return m, tea.Batch(
		func() tea.Msg { return editor.RetokenizeMsg{EditorID: editorID, Version: version} },
		m.syncEditorStateAfterUpdate(index, prevVersion, prevCursor),
	)
}
