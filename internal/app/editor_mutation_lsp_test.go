package app

import (
	"testing"

	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/text"
)

func TestNotifyLSPChangeSkipsDocumentBeforeDidOpen(t *testing.T) {
	buffer := text.NewBuffer()
	buffer.FilePath = "/workspace/main.go"
	ed := &editor.Editor{Buffer: buffer}
	client := &lsp.Client{}

	m := testModel(modelState{})
	if cmd := (&m).notifyLSPChange(client, ed); cmd != nil {
		t.Fatal("didChange command was scheduled before didOpen recorded the document")
	}
}
