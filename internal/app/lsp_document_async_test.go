package app

import (
	"os"
	"path/filepath"
	"testing"

	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/overlay"
)

func TestDocumentResultRejectsStaleRequestIdentity(t *testing.T) {
	kinds := []documentRequestKind{
		documentRequestDefinition,
		documentRequestReferences,
		documentRequestCodeAction,
		documentRequestSymbols,
		documentRequestRename,
		documentRequestFolding,
	}
	scenarios := []struct {
		name   string
		mutate func(*Model, documentRequestKind, *lsp.DocumentRequestMetadata)
		want   bool
	}{
		{name: "matching request", want: true},
		{
			name: "different document",
			mutate: func(_ *Model, _ documentRequestKind, metadata *lsp.DocumentRequestMetadata) {
				metadata.FilePath = "/workspace/other.go"
			},
		},
		{
			name: "edited document",
			mutate: func(model *Model, _ documentRequestKind, _ *lsp.DocumentRequestMetadata) {
				model.activeEditor().Buffer.InsertAtCursor([]byte("x"))
			},
		},
		{
			name: "superseded request",
			mutate: func(model *Model, kind documentRequestKind, _ *lsp.DocumentRequestMetadata) {
				model.beginDocumentRequest(kind, model.activeEditor().Buffer.FilePath)
			},
		},
	}

	for _, kind := range kinds {
		for _, scenario := range scenarios {
			t.Run(kind.String()+"/"+scenario.name, func(t *testing.T) {
				model := newOverlayRequestTestModel(t)
				metadata, ok := model.beginDocumentRequest(kind, model.activeEditor().Buffer.FilePath)
				if !ok {
					t.Fatal("beginDocumentRequest() ok = false")
				}
				if scenario.mutate != nil {
					scenario.mutate(&model, kind, &metadata)
				}
				requireActive := kind != documentRequestFolding
				if got := model.acceptsDocumentResult(kind, metadata, requireActive); got != scenario.want {
					t.Fatalf("acceptsDocumentResult() = %t, want %t; metadata = %#v", got, scenario.want, metadata)
				}
			})
		}
	}
}

func TestStaleCodeActionDoesNotMutateDocument(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	metadata, ok := model.beginDocumentRequest(documentRequestCodeAction, model.activeEditor().Buffer.FilePath)
	if !ok {
		t.Fatal("beginDocumentRequest() ok = false")
	}
	model.activeEditor().Buffer.InsertAtCursor([]byte("current"))
	before := model.activeEditor().Buffer.Content()

	updatedAny, cmd := model.Update(lsp.CodeActionResultMsg{
		DocumentRequestMetadata: metadata,
		Actions: []lsp.CodeAction{{
			Title: "stale edit",
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				lsp.FileURI(model.activeEditor().Buffer.FilePath): {{NewText: "stale"}},
			}},
		}},
	})
	updated := updatedAny.(Model)
	if cmd != nil {
		t.Fatal("stale code action returned a command")
	}
	if got := updated.activeEditor().Buffer.Content(); got != before {
		t.Fatalf("stale code action changed content to %q, want %q", got, before)
	}
}

func newCodeActionPickerTestModel(t *testing.T) Model {
	t.Helper()

	root := t.TempDir()
	filePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(filePath, nil, 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel(filePath, root, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(model.cleanup)
	return model
}

func TestCodeActionResultShowsPickerWithoutApplyingFirstAction(t *testing.T) {
	model := newCodeActionPickerTestModel(t)
	metadata, ok := model.beginDocumentRequest(documentRequestCodeAction, model.activeEditor().Buffer.FilePath)
	if !ok {
		t.Fatal("beginDocumentRequest() ok = false")
	}
	before := model.activeEditor().Buffer.Content()

	updatedAny, cmd := model.Update(lsp.CodeActionResultMsg{
		DocumentRequestMetadata: metadata,
		Actions: []lsp.CodeAction{
			{
				Title: "first",
				Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
					lsp.FileURI(metadata.FilePath): {{NewText: "first"}},
				}},
			},
			{
				Title: "second",
				Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
					lsp.FileURI(metadata.FilePath): {{NewText: "second"}},
				}},
			},
		},
	})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("code action result did not focus its picker")
	}
	if got := updated.activeEditor().Buffer.Content(); got != before {
		t.Fatalf("code action result changed content to %q before selection", got)
	}
	if updated.overlayStack.Len() != 1 {
		t.Fatalf("overlay stack length = %d, want 1", updated.overlayStack.Len())
	}
	if picker, ok := updated.overlayStack.Top().(*overlay.Picker); !ok || picker.FilteredCount() != 2 {
		t.Fatalf("top overlay = %T with unexpected item count", updated.overlayStack.Top())
	}
}

func TestCodeActionPickerAppliesSelectedAction(t *testing.T) {
	model := newCodeActionPickerTestModel(t)
	metadata, ok := model.beginDocumentRequest(documentRequestCodeAction, model.activeEditor().Buffer.FilePath)
	if !ok {
		t.Fatal("beginDocumentRequest() ok = false")
	}
	action := lsp.CodeAction{
		Title: "chosen",
		Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(metadata.FilePath): {{NewText: "chosen"}},
		}},
	}

	updatedAny, cmd := model.Update(overlay.PickerSelectMsg{Item: overlay.PickerItem{
		Value: lspCodeActionPickerMsg{Action: action, Metadata: metadata},
	}})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	if got := updated.activeEditor().Buffer.Content(); got != "chosen" {
		t.Fatalf("selected code action content = %q, want %q", got, "chosen")
	}
	if got := updated.status; got != "Applied: chosen" {
		t.Fatalf("status = %q, want applied confirmation", got)
	}
}

func TestCodeActionPickerRejectsSelectionAfterDocumentChanges(t *testing.T) {
	model := newCodeActionPickerTestModel(t)
	metadata, ok := model.beginDocumentRequest(documentRequestCodeAction, model.activeEditor().Buffer.FilePath)
	if !ok {
		t.Fatal("beginDocumentRequest() ok = false")
	}
	action := lsp.CodeAction{
		Title: "stale",
		Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(metadata.FilePath): {{NewText: "stale"}},
		}},
	}
	model.activeEditor().Buffer.InsertAtCursor([]byte("current"))

	updatedAny, cmd := model.Update(overlay.PickerSelectMsg{Item: overlay.PickerItem{
		Value: lspCodeActionPickerMsg{Action: action, Metadata: metadata},
	}})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	if cmd != nil {
		t.Fatal("stale picker selection returned a command")
	}
	if got := updated.activeEditor().Buffer.Content(); got != "current" {
		t.Fatalf("stale selection changed content to %q", got)
	}
	if got := updated.status; got != "Code action expired; request it again" {
		t.Fatalf("status = %q, want expiration message", got)
	}
}

func TestCodeActionPickerRunsExplicitServerCommandAfterApplyingItsEdit(t *testing.T) {
	model := newCodeActionPickerTestModel(t)
	metadata, ok := model.beginDocumentRequest(documentRequestCodeAction, model.activeEditor().Buffer.FilePath)
	if !ok {
		t.Fatal("beginDocumentRequest() ok = false")
	}
	action := lsp.CodeAction{
		Title: "fix and organize",
		Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(metadata.FilePath): {{NewText: "fixed"}},
		}},
		Command: &struct {
			Title     string `json:"title"`
			Command   string `json:"command"`
			Arguments []any  `json:"arguments,omitempty"`
		}{Title: "organize", Command: "gopls.organize_imports"},
	}

	updatedAny, cmd := model.Update(overlay.PickerSelectMsg{Item: overlay.PickerItem{
		Value: lspCodeActionPickerMsg{Action: action, Metadata: metadata},
	}})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	if got := updated.activeEditor().Buffer.Content(); got != "fixed" {
		t.Fatalf("edit was not applied before command dispatch: %q", got)
	}
	if got := updated.status; got != "Running code action: fix and organize" {
		t.Fatalf("status = %q, want running command", got)
	}
	if cmd == nil {
		t.Fatal("combined edit and command returned no asynchronous command")
	}
}

func TestCodeActionCommandResultIsDiscardedAfterDocumentChanges(t *testing.T) {
	model := newCodeActionPickerTestModel(t)
	metadata, ok := model.beginDocumentRequest(documentRequestCodeAction, model.activeEditor().Buffer.FilePath)
	if !ok {
		t.Fatal("beginDocumentRequest() ok = false")
	}
	action := lsp.CodeAction{
		Title: "server command",
		Command: &struct {
			Title     string `json:"title"`
			Command   string `json:"command"`
			Arguments []any  `json:"arguments,omitempty"`
		}{Title: "run", Command: "gopls.run"},
	}
	updatedAny, _ := model.Update(overlay.PickerSelectMsg{Item: overlay.PickerItem{
		Value: lspCodeActionPickerMsg{Action: action, Metadata: metadata},
	}})
	updated := updatedAny.(Model)
	commandMetadata := lsp.DocumentRequestMetadata{
		FilePath:   updated.activeEditor().Buffer.FilePath,
		Version:    updated.activeEditor().Buffer.Version(),
		Generation: updated.documentRequests.current(documentRequestCodeAction),
	}
	updated.activeEditor().Buffer.InsertAtCursor([]byte("changed"))
	updated.status = "newer editor state"

	finalAny, cmd := updated.Update(lspCodeActionCommandResultMsg{
		Generation: updated.codeActionCommandGen,
		Metadata:   commandMetadata,
		Title:      action.Title,
		Err:        nil,
	})
	final := finalAny.(Model)
	if cmd != nil {
		t.Fatal("stale code action command result returned a command")
	}
	if got := final.status; got != "newer editor state" {
		t.Fatalf("stale command result changed status to %q", got)
	}
}
