package app

import (
	"context"

	"teak/internal/editor"
	"teak/internal/lsp"
)

type codeActionRequester func(context.Context, string, int, int, []lsp.Diagnostic) ([]lsp.CodeAction, error)

type documentRequestKind uint8

const (
	documentRequestDefinition documentRequestKind = iota
	documentRequestReferences
	documentRequestCodeAction
	documentRequestSymbols
	documentRequestRename
	documentRequestFolding
	documentRequestKindCount
)

func (kind documentRequestKind) String() string {
	switch kind {
	case documentRequestDefinition:
		return "definition"
	case documentRequestReferences:
		return "references"
	case documentRequestCodeAction:
		return "code-action"
	case documentRequestSymbols:
		return "symbols"
	case documentRequestRename:
		return "rename"
	case documentRequestFolding:
		return "folding"
	default:
		return "unknown"
	}
}

type documentRequestTracker struct {
	generations [documentRequestKindCount]uint64
	cancels     [documentRequestKindCount]context.CancelFunc
	filePaths   [documentRequestKindCount]string
}

func (tracker *documentRequestTracker) next(kind documentRequestKind) uint64 {
	if kind >= documentRequestKindCount {
		return 0
	}
	tracker.generations[kind]++
	return tracker.generations[kind]
}

func (tracker documentRequestTracker) current(kind documentRequestKind) uint64 {
	if kind >= documentRequestKindCount {
		return 0
	}
	return tracker.generations[kind]
}

func (kind documentRequestKind) cursorSensitive() bool {
	return kind == documentRequestDefinition ||
		kind == documentRequestReferences ||
		kind == documentRequestCodeAction ||
		kind == documentRequestRename
}

func (kind documentRequestKind) requiresActiveDocument() bool {
	return kind != documentRequestFolding
}

func (tracker *documentRequestTracker) start(kind documentRequestKind, filePath string) context.Context {
	if kind >= documentRequestKindCount {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	if tracker.cancels[kind] != nil {
		tracker.cancels[kind]()
	}
	ctx, cancel := context.WithCancel(context.Background())
	tracker.cancels[kind] = cancel
	tracker.filePaths[kind] = filePath
	return ctx
}

func (tracker *documentRequestTracker) invalidate(kind documentRequestKind) {
	if kind >= documentRequestKindCount {
		return
	}
	if tracker.cancels[kind] != nil {
		tracker.cancels[kind]()
		tracker.cancels[kind] = nil
		tracker.generations[kind]++
	}
	tracker.filePaths[kind] = ""
}

func (tracker *documentRequestTracker) invalidateEditor(filePath string, versionChanged, cursorChanged bool) {
	for kind := documentRequestKind(0); kind < documentRequestKindCount; kind++ {
		if tracker.filePaths[kind] != filePath {
			continue
		}
		if versionChanged || (cursorChanged && kind.cursorSensitive()) {
			tracker.invalidate(kind)
		}
	}
}

func (tracker *documentRequestTracker) invalidateActiveRequests() {
	for kind := documentRequestKind(0); kind < documentRequestKindCount; kind++ {
		if kind.requiresActiveDocument() {
			tracker.invalidate(kind)
		}
	}
}

func (tracker *documentRequestTracker) invalidateDocument(filePath string) {
	for kind := documentRequestKind(0); kind < documentRequestKindCount; kind++ {
		if tracker.filePaths[kind] == filePath {
			tracker.invalidate(kind)
		}
	}
}

func (tracker *documentRequestTracker) cancelAll() {
	for kind := documentRequestKind(0); kind < documentRequestKindCount; kind++ {
		if tracker.cancels[kind] != nil {
			tracker.cancels[kind]()
			tracker.cancels[kind] = nil
		}
		tracker.filePaths[kind] = ""
	}
}

func (m *Model) beginDocumentRequest(kind documentRequestKind, filePath string) (lsp.DocumentRequestMetadata, bool) {
	if filePath == "" {
		return lsp.DocumentRequestMetadata{}, false
	}
	editorIndex := m.findEditorByPath(filePath)
	if editorIndex < 0 {
		return lsp.DocumentRequestMetadata{}, false
	}
	return lsp.DocumentRequestMetadata{
		FilePath:   filePath,
		Version:    m.editors[editorIndex].Buffer.Version(),
		CursorLine: m.editors[editorIndex].Buffer.Cursor.Line,
		CursorCol:  m.editors[editorIndex].Buffer.Cursor.Col,
		Generation: m.documentRequests.next(kind),
	}, true
}

func (m *Model) beginDocumentRequestContext(kind documentRequestKind, filePath string) (lsp.DocumentRequestMetadata, context.Context, bool) {
	metadata, ok := m.beginDocumentRequest(kind, filePath)
	if !ok {
		return lsp.DocumentRequestMetadata{}, nil, false
	}
	return metadata, m.documentRequests.start(kind, filePath), true
}

func (m Model) acceptsDocumentResult(kind documentRequestKind, metadata lsp.DocumentRequestMetadata, requireActive bool) bool {
	if metadata == (lsp.DocumentRequestMetadata{}) {
		return true
	}
	if metadata.FilePath == "" || metadata.Generation == 0 {
		return false
	}

	editorIndex := m.findEditorByPath(metadata.FilePath)
	if editorIndex < 0 ||
		m.editors[editorIndex].Buffer.Version() != metadata.Version ||
		m.documentRequests.current(kind) != metadata.Generation {
		return false
	}
	if kind.cursorSensitive() &&
		(m.editors[editorIndex].Buffer.Cursor.Line != metadata.CursorLine ||
			m.editors[editorIndex].Buffer.Cursor.Col != metadata.CursorCol) {
		return false
	}
	return !requireActive || editorIndex == m.activeTab
}

// snapshotCodeActionDiagnostics converts the editor-owned diagnostics while
// still on the Bubble Tea event loop. The returned slice is safe for a tea.Cmd
// to retain while later diagnostics messages update the editor model.
func snapshotCodeActionDiagnostics(diagnostics []editor.Diagnostic, line int) []lsp.Diagnostic {
	// Most cursor lines have only a handful of diagnostics. Do not reserve the
	// entire file-wide slice when nearly all entries may be filtered out.
	snapshot := make([]lsp.Diagnostic, 0, min(len(diagnostics), 8))
	for _, diagnostic := range diagnostics {
		if line < diagnostic.StartLine || line > diagnostic.EndLine {
			continue
		}
		snapshot = append(snapshot, lsp.Diagnostic{
			Range: lsp.DiagRange{
				Start: lsp.DiagPosition{Line: diagnostic.StartLine, Character: diagnostic.StartCol},
				End:   lsp.DiagPosition{Line: diagnostic.EndLine, Character: diagnostic.EndCol},
			},
			Severity: lsp.DiagSeverity(diagnostic.Severity),
			Message:  diagnostic.Message,
		})
	}
	return snapshot
}
