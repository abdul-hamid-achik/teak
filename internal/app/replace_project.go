package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/search"
	"teak/internal/text"
)

type projectReplaceTarget struct {
	Path     string
	Open     bool
	EditorID uint64
	Version  int
	Source   *text.Rope
	Cursor   text.Position
	Ending   text.LineEnding
}

type projectReplaceOpenResult struct {
	Target  projectReplaceTarget
	Result  *text.Rope
	Cursor  text.Position
	Matches int
}

type projectReplaceClosedResult struct {
	Path    string
	Matches int
	Err     error
}

type projectReplacePreparedMsg struct {
	Generation uint64
	Open       []projectReplaceOpenResult
	Closed     []projectReplaceClosedResult
	LimitHit   bool
	Err        error
}

func uniqueSearchResultPaths(results []search.Result) []string {
	seen := make(map[string]struct{}, len(results))
	var paths []string
	for _, result := range results {
		if result.FilePath == "" {
			continue
		}
		if _, ok := seen[result.FilePath]; ok {
			continue
		}
		seen[result.FilePath] = struct{}{}
		paths = append(paths, result.FilePath)
	}
	return paths
}

func (m *Model) startProjectReplace(query, replacement string, opts search.SearchOpts, paths []string) tea.Cmd {
	if query == "" || len(paths) == 0 {
		return nil
	}
	if m.replaces.cancel != nil {
		m.replaces.cancel()
	}
	m.replaces.generation++
	ctx, cancel := context.WithCancel(context.Background())
	m.replaces.cancel = cancel
	generation := m.replaces.generation

	openByPath := make(map[string]int, len(m.editors))
	for i, ed := range m.editors {
		if ed.Buffer != nil && ed.Buffer.FilePath != "" {
			openByPath[ed.Buffer.FilePath] = i
		}
	}

	targets := make([]projectReplaceTarget, 0, len(paths))
	for _, path := range paths {
		if index, ok := openByPath[path]; ok {
			ed := m.editors[index]
			if ed.Buffer == nil || ed.Buffer.Rope() == nil {
				continue
			}
			if ed.Buffer.Rope().Len() > maxReplaceAllBytes {
				m.status = "Replace All is limited to files up to 8 MiB"
				return nil
			}
			targets = append(targets, projectReplaceTarget{
				Path:     path,
				Open:     true,
				EditorID: ed.ID(),
				Version:  ed.Buffer.Version(),
				Source:   ed.Buffer.Rope(),
				Cursor:   ed.Buffer.Cursor,
				Ending:   ed.Buffer.LineEnding(),
			})
			continue
		}
		targets = append(targets, projectReplaceTarget{Path: path})
	}
	return prepareProjectReplaceCmd(ctx, generation, query, replacement, opts, targets)
}

func prepareProjectReplaceCmd(ctx context.Context, generation uint64, query, replacement string, opts search.SearchOpts, targets []projectReplaceTarget) tea.Cmd {
	return func() tea.Msg {
		pattern, err := search.CompilePattern(query, opts)
		if err != nil {
			return projectReplacePreparedMsg{Generation: generation, Err: err}
		}
		literalReplacement := replacement
		if !opts.Regex {
			literalReplacement = strings.ReplaceAll(replacement, "$", "$$")
		}

		var open []projectReplaceOpenResult
		var closed []projectReplaceClosedResult
		for _, target := range targets {
			if err := ctx.Err(); err != nil {
				return projectReplacePreparedMsg{Generation: generation, Err: err}
			}
			if target.Open {
				data, err := target.Source.BytesContext(ctx)
				if err != nil {
					return projectReplacePreparedMsg{Generation: generation, Err: err}
				}
				cursorOffset := target.Source.PositionToOffset(target.Cursor)
				replaced, mappedCursor, matches, ok := boundedReplaceAllRegexAtOffset(
					string(data), pattern, literalReplacement, cursorOffset,
				)
				if !ok {
					return projectReplacePreparedMsg{Generation: generation, LimitHit: true}
				}
				if matches == 0 {
					continue
				}
				result := text.NewFromString(replaced)
				open = append(open, projectReplaceOpenResult{
					Target:  target,
					Result:  result,
					Cursor:  result.OffsetToPosition(mappedCursor),
					Matches: matches,
				})
				continue
			}

			raw, err := os.ReadFile(target.Path)
			if err != nil {
				closed = append(closed, projectReplaceClosedResult{Path: target.Path, Err: err})
				continue
			}
			if len(raw) > maxReplaceAllBytes {
				return projectReplacePreparedMsg{Generation: generation, LimitHit: true}
			}
			normalized, ending, prepErr := text.PrepareLoadedBytes(raw)
			if prepErr != nil {
				closed = append(closed, projectReplaceClosedResult{Path: target.Path, Err: prepErr})
				continue
			}
			replaced, _, matches, ok := boundedReplaceAllRegexAtOffset(
				string(normalized), pattern, literalReplacement, 0,
			)
			if !ok {
				return projectReplacePreparedMsg{Generation: generation, LimitHit: true}
			}
			if matches == 0 {
				continue
			}
			if err := text.WriteRopeAtomicallyWithLineEnding(target.Path, text.NewFromString(replaced), ending); err != nil {
				closed = append(closed, projectReplaceClosedResult{Path: target.Path, Err: err})
				continue
			}
			closed = append(closed, projectReplaceClosedResult{Path: target.Path, Matches: matches})
		}
		return projectReplacePreparedMsg{Generation: generation, Open: open, Closed: closed}
	}
}

func (m Model) handleProjectReplacePrepared(msg projectReplacePreparedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.replaces.generation {
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

	var cmds []tea.Cmd
	total := 0
	files := 0
	for _, item := range msg.Open {
		index := m.editorIndexForAsyncMessage(item.Target.EditorID)
		if index < 0 {
			continue
		}
		ed := &m.editors[index]
		if ed.Buffer.Version() != item.Target.Version || ed.Buffer.Rope() != item.Target.Source {
			m.status = "Replace discarded: buffer changed while it was prepared"
			return m, nil
		}
		if item.Result == nil || item.Matches == 0 {
			continue
		}
		prevVersion, prevCursor := ed.Buffer.Version(), ed.Buffer.Cursor
		ed.Buffer.ReplaceRopeSnapshot(item.Result, item.Cursor)
		if ed.Highlighter != nil {
			ed.Highlighter.Invalidate()
		}
		ed.SetSize(ed.Viewport.Width, ed.Viewport.Height)
		ed.EnsureCursorVisible()
		total += item.Matches
		files++
		editorID, version := ed.ID(), ed.Buffer.Version()
		cmds = append(cmds,
			func() tea.Msg { return editor.RetokenizeMsg{EditorID: editorID, Version: version} },
			m.syncEditorStateAfterUpdate(index, prevVersion, prevCursor),
		)
	}
	for _, item := range msg.Closed {
		if item.Err != nil {
			m.status = fmt.Sprintf("Replace failed in %s: %v", filepath.Base(item.Path), item.Err)
			return m, tea.Batch(cmds...)
		}
		if item.Matches == 0 {
			continue
		}
		total += item.Matches
		files++
	}
	if total == 0 {
		m.status = "No matches to replace"
		return m, tea.Batch(cmds...)
	}
	noun := "match"
	if total != 1 {
		noun = "matches"
	}
	fileNoun := "file"
	if files != 1 {
		fileNoun = "files"
	}
	m.status = fmt.Sprintf("Replaced %d %s in %d %s", total, noun, files, fileNoun)
	return m, tea.Batch(cmds...)
}
