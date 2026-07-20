package app

import "teak/internal/lsp"

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
