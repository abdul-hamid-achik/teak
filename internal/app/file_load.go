package app

import (
	"context"
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/session"
	"teak/internal/text"
)

// pendingNavigation is scoped to a requested path, rather than the whole UI.
// It is transferred to the concrete file-load request as soon as a placeholder
// editor has been created.
type pendingNavigation struct {
	Path             string
	Position         text.Position
	ProtocolEncoding string
}

type pendingFileLoad struct {
	ID             uint64
	Path           string
	EditorID       uint64
	BaseVersion    int
	BaseRope       *text.Rope
	Cursor         *text.Position
	CursorEncoding string
	Session        *session.TabState
	Cancel         context.CancelFunc
}

type pendingDiffLoad struct {
	ID       uint64
	Path     string
	EditorID uint64
	Cancel   context.CancelFunc
}

func (m *Model) setPendingCursor(path string, pos text.Position) {
	m.pendingCursor = &pendingNavigation{Path: filepath.Clean(path), Position: pos}
}

func (m *Model) setPendingLSPCursor(path string, pos text.Position, encoding string) {
	m.pendingCursor = &pendingNavigation{
		Path:             filepath.Clean(path),
		Position:         pos,
		ProtocolEncoding: encoding,
	}
}

func (m *Model) takePendingNavigation(path string) *pendingNavigation {
	if m.pendingCursor == nil || filepath.Clean(path) != m.pendingCursor.Path {
		return nil
	}
	navigation := *m.pendingCursor
	m.pendingCursor = nil
	return &navigation
}

func resolvePendingLSPPosition(content string, position text.Position, encoding string) (text.Position, error) {
	if encoding == "" {
		return position, nil
	}
	converted, err := lsp.PositionFromProtocol(content, encoding, lsp.Position{
		Line:      position.Line,
		Character: position.Col,
	})
	if err != nil {
		return text.Position{}, err
	}
	return text.Position{Line: converted.Line, Col: converted.Character}, nil
}

func resolvePendingLSPPositionRope(rope *text.Rope, position text.Position, encoding string) (text.Position, error) {
	if encoding == "" {
		return position, nil
	}
	if rope == nil || position.Line < 0 || position.Line >= rope.LineCount() {
		return text.Position{}, fmt.Errorf("line %d is outside document", position.Line)
	}
	column, err := lsp.PositionFromProtocolLine(rope.Line(position.Line), encoding, position.Col)
	if err != nil {
		return text.Position{}, fmt.Errorf("line %d: %w", position.Line, err)
	}
	return text.Position{Line: position.Line, Col: column}, nil
}

func (m *Model) startFileLoad(path string, ed editor.Editor, forceNew bool, restored *session.TabState) tea.Cmd {
	m.nextFileLoadID++
	requestID := m.nextFileLoadID
	ctx, cancel := context.WithCancel(context.Background())
	navigation := m.takePendingNavigation(path)
	request := pendingFileLoad{
		ID:          requestID,
		Path:        path,
		EditorID:    ed.ID(),
		BaseVersion: ed.Buffer.Version(),
		BaseRope:    ed.Buffer.Rope(),
		Session:     restored,
		Cancel:      cancel,
	}
	if navigation != nil {
		position := navigation.Position
		request.Cursor = &position
		request.CursorEncoding = navigation.ProtocolEncoding
	}
	if m.pendingFileLoads == nil {
		m.pendingFileLoads = make(map[uint64]pendingFileLoad)
	}
	m.pendingFileLoads[requestID] = request
	return loadFileCmd(ctx, path, ed.ID(), requestID, forceNew)
}

func (m *Model) startDiffLoad(relPath, status string, ed editor.Editor) tea.Cmd {
	m.nextDiffLoadID++
	requestID := m.nextDiffLoadID
	ctx, cancel := context.WithCancel(context.Background())
	if m.pendingDiffLoads == nil {
		m.pendingDiffLoads = make(map[uint64]pendingDiffLoad)
	}
	m.pendingDiffLoads[requestID] = pendingDiffLoad{ID: requestID, Path: relPath, EditorID: ed.ID(), Cancel: cancel}
	return loadDiffCmd(ctx, m.rootDir, relPath, status, ed.ID(), requestID)
}

func (m *Model) latestFileLoadRequest() pendingFileLoad {
	return m.pendingFileLoads[m.nextFileLoadID]
}

func (m *Model) removeLoadsForEditor(editorID uint64) {
	for id, request := range m.pendingFileLoads {
		if request.EditorID == editorID {
			request.Cancel()
			delete(m.pendingFileLoads, id)
		}
	}
	for id, request := range m.pendingDiffLoads {
		if request.EditorID == editorID {
			request.Cancel()
			delete(m.pendingDiffLoads, id)
		}
	}
}

func (m Model) editorIndexForLoad(editorID uint64, path string) int {
	for index := range m.editors {
		if m.editors[index].ID() == editorID && m.editors[index].Buffer.FilePath == path {
			return index
		}
	}
	// A safe fallback is allowed only for a unique matching path. This helps
	// callers that recreated the UI model, while never guessing between tabs.
	match := -1
	for index := range m.editors {
		if m.editors[index].Buffer.FilePath == path {
			if match >= 0 {
				return -1
			}
			match = index
		}
	}
	return match
}

func (m Model) editorIndexForDiffLoad(editorID uint64, relPath string) int {
	diffPath := "diff://" + relPath
	for index := range m.editors {
		if m.editors[index].ID() == editorID && index < len(m.tabBar.Tabs) && m.tabBar.Tabs[index].FilePath == diffPath {
			return index
		}
	}
	match := -1
	for index := range m.tabBar.Tabs {
		if m.tabBar.Tabs[index].FilePath == diffPath {
			if match >= 0 {
				return -1
			}
			match = index
		}
	}
	return match
}
