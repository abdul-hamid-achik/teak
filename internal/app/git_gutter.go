package app

import (
	"context"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/git"
)

type gitGutterReadyMsg struct {
	Path       string
	Generation uint64
	Marks      map[int]editor.GitLineKind
	Err        error
}

func mapGitLineMarks(src map[int]git.LineKind) map[int]editor.GitLineKind {
	if len(src) == 0 {
		return nil
	}
	out := make(map[int]editor.GitLineKind, len(src))
	for line, kind := range src {
		out[line] = editor.GitLineKind(kind)
	}
	return out
}

func (m *Model) refreshGitGutterCmd() tea.Cmd {
	ed := m.activeEditor()
	if ed == nil {
		return nil
	}
	return m.refreshGitGutterForPath(ed.Buffer.FilePath)
}

func (m *Model) refreshGitGutterForPath(path string) tea.Cmd {
	if m.modelState == nil || !m.appCfg.Editor.GitGutter || m.rootDir == "" || path == "" {
		return nil
	}
	cleaned := filepath.Clean(path)
	if m.gitGutterGeneration == nil {
		m.gitGutterGeneration = make(map[string]uint64)
	}
	m.gitGutterGeneration[cleaned]++
	generation := m.gitGutterGeneration[cleaned]
	root := m.rootDir
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		marks, err := git.DiffLinesAgainstHEAD(ctx, root, cleaned)
		return gitGutterReadyMsg{
			Path:       cleaned,
			Generation: generation,
			Marks:      mapGitLineMarks(marks),
			Err:        err,
		}
	}
}

func (m Model) handleGitGutterReady(msg gitGutterReadyMsg) (tea.Model, tea.Cmd) {
	if m.gitGutterGeneration == nil || m.gitGutterGeneration[msg.Path] != msg.Generation {
		return m, nil
	}
	if !m.appCfg.Editor.GitGutter {
		return m, nil
	}
	for i := range m.editors {
		if filepath.Clean(m.editors[i].Buffer.FilePath) == msg.Path {
			if msg.Err != nil {
				m.editors[i].GitLines = nil
				continue
			}
			m.editors[i].GitLines = msg.Marks
		}
	}
	return m, nil
}

func (m *Model) clearGitGutterMarks() {
	for i := range m.editors {
		m.editors[i].GitLines = nil
	}
}
