package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/overlay"
	"teak/internal/text"
)

var benchmarkCodeActionDiagnosticsSink []lsp.Diagnostic

func BenchmarkCodeActionDiagnosticIndexedProjectionHundredThousand(b *testing.B) {
	diagnostics := make([]editor.Diagnostic, 100_000)
	for i := range diagnostics {
		diagnostics[i] = editor.Diagnostic{StartLine: i, EndLine: i, Message: "diagnostic"}
	}
	var ed editor.Editor
	ed.InstallDiagnostics(diagnostics)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkCodeActionDiagnosticsSink = snapshotCodeActionDiagnostics(ed.DiagnosticsIntersecting(50_000, 50_000), 50_000)
	}
}

func TestCodeActionDiagnosticsAreSnapshottedBeforeCommand(t *testing.T) {
	diagnostics := []editor.Diagnostic{
		{StartLine: 2, StartCol: 1, EndLine: 4, EndCol: 3, Severity: 2, Message: "original"},
		{StartLine: 8, EndLine: 8, Message: "outside cursor line"},
	}

	got := snapshotCodeActionDiagnostics(diagnostics, 3)
	diagnostics[0].Message = "mutated after request"
	diagnostics[0].StartLine = 99

	if len(got) != 1 {
		t.Fatalf("snapshot length = %d, want 1", len(got))
	}
	if got[0].Message != "original" || got[0].Range.Start.Line != 2 || got[0].Range.End.Line != 4 {
		t.Fatalf("snapshot changed with editor diagnostics: %#v", got[0])
	}
}

func TestRequestCodeActionsSnapshotsDiagnosticsBeforeCommand(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	model.activeEditor().InstallDiagnostics([]editor.Diagnostic{{
		StartLine: 0,
		StartCol:  1,
		EndLine:   0,
		EndCol:    4,
		Severity:  1,
		Message:   "original",
	}})

	var received []lsp.Diagnostic
	model.codeActionRequester = func(_ context.Context, _ string, _, _ int, diagnostics []lsp.Diagnostic) ([]lsp.CodeAction, error) {
		received = diagnostics
		return nil, nil
	}
	_, cmd := model.requestCodeActions()
	if cmd == nil {
		t.Fatal("requestCodeActions() command = nil")
	}

	model.activeEditor().Diagnostics[0].Message = "new publication"
	model.activeEditor().Diagnostics[0].StartCol = 99
	_ = cmd()

	if len(received) != 1 || received[0].Message != "original" || received[0].Range.Start.Character != 1 {
		t.Fatalf("request observed diagnostics after Update returned: %#v", received)
	}
}

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

func TestDocumentResultCursorSensitivityMatchesRequestKind(t *testing.T) {
	tests := []struct {
		kind documentRequestKind
		want bool
	}{
		{documentRequestDefinition, false},
		{documentRequestReferences, false},
		{documentRequestCodeAction, false},
		{documentRequestRename, false},
		{documentRequestSymbols, true},
		{documentRequestFolding, true},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			model := newOverlayRequestTestModel(t)
			model.activeEditor().Buffer.LoadContent([]byte("alpha beta"))
			model.activeEditor().Buffer.SetCursor(text.Position{Col: 1})
			metadata, ok := model.beginDocumentRequest(tt.kind, model.activeEditor().Buffer.FilePath)
			if !ok {
				t.Fatal("beginDocumentRequest() ok = false")
			}
			model.activeEditor().Buffer.SetCursor(text.Position{Col: 7})

			if got := model.acceptsDocumentResult(tt.kind, metadata, tt.kind != documentRequestFolding); got != tt.want {
				t.Fatalf("acceptsDocumentResult() = %t, want %t after cursor move", got, tt.want)
			}
		})
	}
}

func TestBeginDocumentRequestContextCancelsSupersededRequest(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	path := model.activeEditor().Buffer.FilePath
	first, firstContext, ok := model.beginDocumentRequestContext(documentRequestDefinition, path)
	if !ok {
		t.Fatal("first beginDocumentRequestContext() ok = false")
	}
	second, secondContext, ok := model.beginDocumentRequestContext(documentRequestDefinition, path)
	if !ok {
		t.Fatal("second beginDocumentRequestContext() ok = false")
	}

	select {
	case <-firstContext.Done():
	default:
		t.Fatal("superseded definition request context remains active")
	}
	select {
	case <-secondContext.Done():
		t.Fatal("current definition request context was canceled")
	default:
	}
	if second.Generation != first.Generation+1 {
		t.Fatalf("generations = %d then %d, want consecutive", first.Generation, second.Generation)
	}
}

func TestCursorMoveCancelsOnlyCursorSensitiveDocumentRequests(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	model.activeEditor().Buffer.LoadContent([]byte("alpha beta"))
	model.activeEditor().Buffer.SetCursor(text.Position{Col: 5})
	path := model.activeEditor().Buffer.FilePath
	definitionMetadata, definitionContext, ok := model.beginDocumentRequestContext(documentRequestDefinition, path)
	if !ok {
		t.Fatal("begin definition request = false")
	}
	symbolMetadata, symbolContext, ok := model.beginDocumentRequestContext(documentRequestSymbols, path)
	if !ok {
		t.Fatal("begin symbols request = false")
	}

	updatedAny, _ := model.forwardToEditor(tea.KeyPressMsg{Code: tea.KeyLeft})
	updated := updatedAny.(Model)
	select {
	case <-definitionContext.Done():
	default:
		t.Fatal("cursor move left definition request running")
	}
	select {
	case <-symbolContext.Done():
		t.Fatal("cursor-independent symbol request was canceled")
	default:
	}
	if updated.acceptsDocumentResult(documentRequestDefinition, definitionMetadata, true) {
		t.Fatal("canceled definition generation remained valid")
	}
	if !updated.acceptsDocumentResult(documentRequestSymbols, symbolMetadata, true) {
		t.Fatal("cursor-independent symbol result became stale")
	}
}

func TestDocumentEditCancelsCursorIndependentRequest(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	path := model.activeEditor().Buffer.FilePath
	metadata, requestContext, ok := model.beginDocumentRequestContext(documentRequestFolding, path)
	if !ok {
		t.Fatal("begin folding request = false")
	}

	updatedAny, _ := model.forwardToEditor(tea.KeyPressMsg{Code: 'x', Text: "x"})
	updated := updatedAny.(Model)
	select {
	case <-requestContext.Done():
	default:
		t.Fatal("document edit left folding request running")
	}
	if updated.acceptsDocumentResult(documentRequestFolding, metadata, false) {
		t.Fatal("document edit left canceled folding generation valid")
	}
}

func TestTabSwitchCancelsActiveDocumentAndOverlayRequests(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	firstPath := model.activeEditor().Buffer.FilePath
	model.rootDir = t.TempDir()
	second := addDirtyEditor(t, &model, "other.go", "package other\n", "package other\n")
	if !model.activateTab(0) {
		t.Fatal("activate first tab = false")
	}
	_, overlayContext, ok := model.beginOverlayRequestContext(overlayRequestHover)
	if !ok {
		t.Fatal("begin hover request = false")
	}
	_, documentContext, ok := model.beginDocumentRequestContext(documentRequestSymbols, firstPath)
	if !ok {
		t.Fatal("begin symbols request = false")
	}

	if !model.activateTab(second) {
		t.Fatal("activate second tab = false")
	}
	for name, requestContext := range map[string]context.Context{
		"hover":   overlayContext,
		"symbols": documentContext,
	} {
		select {
		case <-requestContext.Done():
		default:
			t.Fatalf("tab switch left %s request running", name)
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
	picker, ok := updated.overlayStack.Top().(*overlay.Picker)
	if !ok || !picker.FilterPending() || picker.FilteredCount() != 0 {
		t.Fatalf("top overlay = %T with unexpected pending item state", updated.overlayStack.Top())
	}
	ready := executePickerItemsPreparation(t, cmd)
	updated = installPreparedPickerItems(t, updated, ready)
	if picker, ok := updated.overlayStack.Top().(*overlay.Picker); !ok || picker.FilterPending() || picker.FilteredCount() != 2 {
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
