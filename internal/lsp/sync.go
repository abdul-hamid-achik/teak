package lsp

// SyncKind represents the text document sync kind from the LSP spec.
type SyncKind int

const (
	// SyncNone means documents should not be synced.
	SyncNone SyncKind = 0
	// SyncFull means documents are synced by sending the full content.
	SyncFull SyncKind = 1
	// SyncIncremental means documents are synced by sending incremental changes.
	SyncIncremental SyncKind = 2
)

// DocumentState tracks the sync state of an open document.
type DocumentState struct {
	URI        string
	LanguageID string
	Version    int
	SyncKind   SyncKind
}

// NewDocumentState creates a new document state for full sync.
func NewDocumentState(uri, languageID string) *DocumentState {
	return &DocumentState{
		URI:        uri,
		LanguageID: languageID,
		Version:    0,
		SyncKind:   SyncFull,
	}
}

// IncrementVersion bumps the document version.
func (d *DocumentState) IncrementVersion() int {
	d.Version++
	return d.Version
}
