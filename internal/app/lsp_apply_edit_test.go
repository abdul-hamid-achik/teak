package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"teak/internal/config"
	"teak/internal/lsp"
)

type applyEditDecision struct {
	applied bool
	reason  string
}

func TestServerApplyEditRunsInUpdateAndAcknowledgesValidatedEdit(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	decision := make(chan applyEditDecision, 1)

	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		RequestID: 7,
		Label:     "server fix",
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{
				StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, NewText: "new",
			}},
		}},
		Respond: func(applied bool, reason string) {
			decision <- applyEditDecision{applied: applied, reason: reason}
		},
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)

	if got := updated.editors[idx].Buffer.Content(); got != "new\n" {
		t.Fatalf("buffer = %q, want server edit", got)
	}
	select {
	case got := <-decision:
		if !got.applied || got.reason != "" {
			t.Fatalf("decision = %#v, want successful acknowledgement", got)
		}
	case <-time.After(time.Second):
		t.Fatal("server apply edit was not acknowledged")
	}
}

func TestServerApplyEditRejectsMismatchedDocumentVersion(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	wrongVersion := model.editors[idx].Buffer.Version() + 1
	decision := make(chan applyEditDecision, 1)

	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		RequestID: 8,
		Edit: lsp.WorkspaceEdit{DocumentChanges: []lsp.WorkspaceDocumentChange{{
			URI:     lsp.FileURI(path),
			Version: &wrongVersion,
			Edits: []lsp.TextEdit{{
				StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, NewText: "new",
			}},
		}}},
		Respond: func(applied bool, reason string) {
			decision <- applyEditDecision{applied: applied, reason: reason}
		},
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)

	if got := updated.editors[idx].Buffer.Content(); got != "old\n" {
		t.Fatalf("mismatched version changed buffer to %q", got)
	}
	select {
	case got := <-decision:
		if got.applied || got.reason == "" {
			t.Fatalf("decision = %#v, want rejected acknowledgement", got)
		}
	case <-time.After(time.Second):
		t.Fatal("version mismatch was not acknowledged")
	}
}

func TestServerApplyEditPreflightPreventsPartialMutation(t *testing.T) {
	root := t.TempDir()
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	outside := filepath.Join(filepath.Dir(root), "outside.go")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	decision := make(chan applyEditDecision, 1)

	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		RequestID: 9,
		Edit: lsp.WorkspaceEdit{DocumentChanges: []lsp.WorkspaceDocumentChange{
			{
				URI: lsp.FileURI(path),
				Edits: []lsp.TextEdit{{
					StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, NewText: "new",
				}},
			},
			{
				URI: lsp.FileURI(outside),
				Edits: []lsp.TextEdit{{
					StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 7, NewText: "changed",
				}},
			},
		}},
		Respond: func(applied bool, reason string) {
			decision <- applyEditDecision{applied: applied, reason: reason}
		},
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)

	if got := updated.editors[idx].Buffer.Content(); got != "old\n" {
		t.Fatalf("first document changed to %q even though preflight failed later", got)
	}
	select {
	case got := <-decision:
		if got.applied || got.reason == "" {
			t.Fatalf("decision = %#v, want rejected acknowledgement", got)
		}
	case <-time.After(time.Second):
		t.Fatal("failed preflight was not acknowledged")
	}
}

func TestServerApplyEditRejectsInvalidUTF8ByteRange(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "unicode.go", "aéz\n", "aéz\n")
	path := model.editors[idx].Buffer.FilePath
	decision := make(chan applyEditDecision, 1)

	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		RequestID: 10,
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{
				StartLine: 0, StartCol: 2, EndLine: 0, EndCol: 3, NewText: "x",
			}},
		}},
		Respond: func(applied bool, reason string) {
			decision <- applyEditDecision{applied: applied, reason: reason}
		},
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)

	if got := updated.editors[idx].Buffer.Content(); got != "aéz\n" {
		t.Fatalf("invalid UTF-8 byte range changed buffer to %q", got)
	}
	select {
	case got := <-decision:
		if got.applied || got.reason == "" {
			t.Fatalf("decision = %#v, want rejected acknowledgement", got)
		}
	case <-time.After(time.Second):
		t.Fatal("invalid UTF-8 range was not acknowledged")
	}
}

func TestServerApplyEditRejectsNonAtomicFileOperationSequence(t *testing.T) {
	root := t.TempDir()
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	renamedPath := filepath.Join(root, "renamed.go")
	decision := make(chan applyEditDecision, 1)

	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		RequestID: 11,
		Edit: lsp.WorkspaceEdit{DocumentChanges: []lsp.WorkspaceDocumentChange{
			{
				FileOperation: &lsp.WorkspaceFileOperation{
					Kind:   lsp.FileOpRename,
					OldURI: lsp.FileURI(path),
					NewURI: lsp.FileURI(renamedPath),
				},
			},
			{
				URI: lsp.FileURI(renamedPath),
				Edits: []lsp.TextEdit{{
					StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, NewText: "new",
				}},
			},
		}},
		Respond: func(applied bool, reason string) {
			decision <- applyEditDecision{applied: applied, reason: reason}
		},
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)

	if got := updated.editors[idx].Buffer.Content(); got != "old\n" {
		t.Fatalf("non-atomic sequence changed buffer to %q", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("source file changed during rejected sequence: %v", err)
	}
	if _, err := os.Stat(renamedPath); !os.IsNotExist(err) {
		t.Fatalf("rename target exists after rejected sequence: %v", err)
	}
	select {
	case got := <-decision:
		if got.applied || got.reason == "" {
			t.Fatalf("decision = %#v, want rejected acknowledgement", got)
		}
	case <-time.After(time.Second):
		t.Fatal("non-atomic sequence was not acknowledged")
	}
}
