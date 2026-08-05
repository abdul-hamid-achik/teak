package app

import (
	"teak/internal/editor"
	"teak/internal/lsp"
)

type codeActionRequester func(filePath string, line, col int, diagnostics []lsp.Diagnostic) ([]lsp.CodeAction, error)

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
		Generation: m.documentRequests.next(kind),
	}, true
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
