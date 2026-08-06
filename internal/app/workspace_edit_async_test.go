package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/text"
)

var workspaceEditPreparationSink workspaceEditPreparation

func runWorkspaceEditCommands(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		cmd, queue = queue[0], queue[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		if msg == nil {
			continue
		}
		updatedAny, next := model.Update(msg)
		model = updatedAny.(Model)
		if next != nil {
			queue = append(queue, next)
		}
	}
	return model
}

func applyWorkspaceEditAsyncForTest(t *testing.T, model Model, edit lsp.WorkspaceEdit) Model {
	t.Helper()
	updated, cmd := model.startWorkspaceEditAsync(edit, workspaceEditContinuation{})
	return runWorkspaceEditCommands(t, updated, cmd)
}

func TestWorkspaceEditPreparationAllocationBudget(t *testing.T) {
	const editCount = 1024
	rootDir := t.TempDir()
	path := filepath.Join(rootDir, "main.go")
	content := strings.Repeat("x\n", editCount)
	source := text.NewFromString(content)
	edits := make([]lsp.TextEdit, editCount)
	for line := range edits {
		edits[line] = lsp.TextEdit{StartLine: line, EndLine: line, EndCol: 1, NewText: "y"}
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	workspaceEdit := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(path): edits,
	}}
	snapshots := []workspaceEditBufferSnapshot{{
		EditorID: 1,
		Path:     path,
		Rope:     source,
	}}

	allocs := testing.AllocsPerRun(1, func() {
		preparation, err := prepareWorkspaceEdit(context.Background(), rootDir, root, workspaceEdit, snapshots)
		if err != nil {
			t.Fatalf("prepareWorkspaceEdit() error = %v", err)
		}
		workspaceEditPreparationSink = preparation
	})
	if allocs > 30_000 {
		t.Fatalf("workspace edit preparation allocations = %.0f, want <= 30000", allocs)
	}
}

func TestWorkspaceEditSequentialBatchesReusePreparedSnapshot(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	index := addDirtyEditor(t, &model, "main.go", "abc\n", "abc\n")
	path := model.editors[index].Buffer.FilePath
	model.editors[index].Buffer.SetCursor(text.Position{Line: 0, Col: 3})

	updated := applyWorkspaceEditAsyncForTest(t, model, lsp.WorkspaceEdit{
		DocumentChanges: []lsp.WorkspaceDocumentChange{
			{
				URI: lsp.FileURI(path),
				Edits: []lsp.TextEdit{{
					StartLine: 0,
					EndLine:   0,
					EndCol:    3,
					NewText:   "one\ntwo",
				}},
			},
			{
				URI: lsp.FileURI(path),
				Edits: []lsp.TextEdit{{
					StartLine: 1,
					EndLine:   1,
					EndCol:    3,
					NewText:   "THREE",
				}},
			},
		},
	})

	if got, want := updated.editors[index].Buffer.Content(), "one\nTHREE\n"; got != want {
		t.Fatalf("workspace edit content = %q, want %q", got, want)
	}
	if got, want := updated.editors[index].Buffer.Cursor, (text.Position{Line: 1, Col: 5}); got != want {
		t.Fatalf("workspace edit cursor = %+v, want %+v", got, want)
	}
}

func BenchmarkPrepareWorkspaceEditFourThousandEdits(b *testing.B) {
	const editCount = 4096
	rootDir := b.TempDir()
	path := filepath.Join(rootDir, "main.go")
	content := strings.Repeat("x\n", editCount)
	edits := make([]lsp.TextEdit, editCount)
	for line := range edits {
		edits[line] = lsp.TextEdit{StartLine: line, EndLine: line, EndCol: 1, NewText: "y"}
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		b.Fatal(err)
	}
	defer root.Close()
	workspaceEdit := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(path): edits,
	}}
	snapshots := []workspaceEditBufferSnapshot{{
		EditorID: 1,
		Path:     path,
		Rope:     text.NewFromString(content),
	}}

	b.ReportAllocs()
	for b.Loop() {
		preparation, err := prepareWorkspaceEdit(context.Background(), rootDir, root, workspaceEdit, snapshots)
		if err != nil {
			b.Fatal(err)
		}
		workspaceEditPreparationSink = preparation
	}
}

func TestWorkspaceEditAsyncDiscardsStaleLiveSnapshotAndAcknowledgesOnce(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	decisions := make(chan applyEditDecision, 2)

	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
		}},
		Respond: func(applied bool, reason string) { decisions <- applyEditDecision{applied: applied, reason: reason} },
	})
	model = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("applyEdit did not start a background command")
	}
	model.editors[idx].Buffer.InsertAtCursor([]byte("user change"))
	model = runWorkspaceEditCommands(t, model, cmd)
	if got := model.editors[idx].Buffer.Content(); got != "user changeold\n" {
		t.Fatalf("stale edit changed content to %q", got)
	}
	select {
	case decision := <-decisions:
		if decision.applied || decision.reason == "" {
			t.Fatalf("decision = %#v, want stale rejection", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("stale applyEdit was not acknowledged")
	}
	select {
	case duplicate := <-decisions:
		t.Fatalf("applyEdit acknowledged twice: %#v", duplicate)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestWorkspaceEditAsyncDoesNotApplyAfterProtocolContextExpires(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	ctx, cancel := context.WithCancel(context.Background())
	decisions := make(chan applyEditDecision, 1)
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		Context: ctx,
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
		}},
		Respond: func(applied bool, reason string) { decisions <- applyEditDecision{applied: applied, reason: reason} },
	})
	model = updatedAny.(Model)
	cancel() // models the LSP's five-second timeout winning before Update sees the result
	model = runWorkspaceEditCommands(t, model, cmd)
	if got := model.editors[idx].Buffer.Content(); got != "old\n" {
		t.Fatalf("expired request changed content to %q", got)
	}
	select {
	case decision := <-decisions:
		if decision.applied || decision.reason == "" {
			t.Fatalf("decision = %#v, want cancellation rejection", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("expired request was not rejected")
	}
}

func TestWorkspaceEditAsyncDoesNotMutateWhenClaimWasLost(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	claims := 0
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		Claim: func() bool { claims++; return false },
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
		}},
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	if claims != 1 || updated.editors[idx].Buffer.Content() != "old\n" {
		t.Fatalf("lost claim = calls %d content %q", claims, updated.editors[idx].Buffer.Content())
	}
}

func TestWorkspaceEditAsyncClaimWinsBeforeLaterContextCancellation(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claims, responses := 0, 0
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		Context: ctx,
		Claim:   func() bool { claims++; return true },
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
		}},
		Respond: func(bool, string) { responses++ },
	})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	cancel() // a timeout after Claim must not roll the accepted UI swap back
	if claims != 1 || responses != 1 || updated.editors[idx].Buffer.Content() != "new\n" {
		t.Fatalf("claimed result calls=%d responses=%d content=%q", claims, responses, updated.editors[idx].Buffer.Content())
	}
}

func TestWorkspaceEditAsyncCommitsClosedFileAfterPreparation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "closed.go")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	decisions := make(chan applyEditDecision, 1)
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
		}},
		Respond: func(applied bool, reason string) { decisions <- applyEditDecision{applied: applied, reason: reason} },
	})
	_ = runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "new\n" {
		t.Fatalf("closed file = %q, want committed edit", got)
	}
	select {
	case decision := <-decisions:
		if !decision.applied || decision.reason != "" {
			t.Fatalf("decision = %#v, want success", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("closed-file edit was not acknowledged")
	}
}

func TestWorkspaceEditAsyncSerializesQueuedRequests(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	first := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "one"}},
	}}
	second := lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "two"}},
	}}

	updatedAny, firstCmd := model.Update(lsp.ApplyEditRequestMsg{Edit: first})
	model = updatedAny.(Model)
	updatedAny, secondCmd := model.Update(lsp.ApplyEditRequestMsg{Edit: second})
	model = updatedAny.(Model)
	if secondCmd != nil || len(model.workspaceEdits.queued) != 1 {
		t.Fatalf("second workspace edit was not queued: cmd=%v queued=%d", secondCmd != nil, len(model.workspaceEdits.queued))
	}
	model = runWorkspaceEditCommands(t, model, firstCmd)
	if got := model.editors[idx].Buffer.Content(); got != "two\n" {
		t.Fatalf("serialized edits = %q, want second edit after first", got)
	}
	if model.workspaceEdits.inFlight || len(model.workspaceEdits.queued) != 0 {
		t.Fatalf("workspace state still pending: %#v", model.workspaceEdits)
	}
}

func TestWorkspaceEditAsyncRejectsClosedFileChangedBeforeCommit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "closed.go")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	decisions := make(chan applyEditDecision, 1)
	updatedAny, prepareCmd := model.Update(lsp.ApplyEditRequestMsg{
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(path): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
		}},
		Respond: func(applied bool, reason string) { decisions <- applyEditDecision{applied: applied, reason: reason} },
	})
	model = updatedAny.(Model)
	prepared, ok := prepareCmd().(workspaceEditPreparedMsg)
	if !ok || prepared.Err != nil {
		t.Fatalf("prepare = %#v, want prepared workspace edit", prepared)
	}
	updatedAny, commitCmd := model.Update(prepared)
	model = updatedAny.(Model)
	if err := os.WriteFile(path, []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = runWorkspaceEditCommands(t, model, commitCmd)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "external\n" {
		t.Fatalf("stale closed file overwritten as %q", got)
	}
	select {
	case decision := <-decisions:
		if decision.applied || decision.reason == "" {
			t.Fatalf("decision = %#v, want stale-disk rejection", decision)
		}
	case <-time.After(time.Second):
		t.Fatal("stale closed-file edit was not acknowledged")
	}
}

func TestWorkspaceEditAsyncRejectsMixedLiveAndClosedFilesBeforeMutation(t *testing.T) {
	root := t.TempDir()
	closedPath := filepath.Join(root, "closed.go")
	if err := os.WriteFile(closedPath, []byte("closed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	idx := addDirtyEditor(t, &model, "open.go", "open\n", "open\n")
	openPath := model.editors[idx].Buffer.FilePath
	before := model.editors[idx].Buffer.Content()
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(openPath):   {{StartLine: 0, EndLine: 0, EndCol: 4, NewText: "live"}},
		lsp.FileURI(closedPath): {{StartLine: 0, EndLine: 0, EndCol: 6, NewText: "disk"}},
	}}})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	if got := updated.editors[idx].Buffer.Content(); got != before {
		t.Fatalf("mixed request changed live buffer to %q", got)
	}
	data, err := os.ReadFile(closedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "closed\n" {
		t.Fatalf("mixed request changed closed file to %q", data)
	}
	if !strings.Contains(updated.status, "cannot be applied atomically") {
		t.Fatalf("status = %q, want atomicity rejection", updated.status)
	}
}

func TestWorkspaceEditAsyncRejectsMultipleClosedFilesBeforeMutation(t *testing.T) {
	root := t.TempDir()
	firstPath, secondPath := filepath.Join(root, "first.go"), filepath.Join(root, "second.go")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(firstPath):  {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "one"}},
		lsp.FileURI(secondPath): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "two"}},
	}}})
	updated := runWorkspaceEditCommands(t, updatedAny.(Model), cmd)
	for _, path := range []string{firstPath, secondPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "old\n" {
			t.Fatalf("multi-closed request changed %s to %q", filepath.Base(path), data)
		}
	}
	if !strings.Contains(updated.status, "cannot be applied atomically") {
		t.Fatalf("status = %q, want atomicity rejection", updated.status)
	}
}

func TestWorkspaceEditAsyncCancelledBeforeFileOperationCommitDoesNotCreate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "created.go")
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	updatedAny, prepareCmd := model.Update(lsp.ApplyEditRequestMsg{Edit: lsp.WorkspaceEdit{
		DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &lsp.WorkspaceFileOperation{Kind: lsp.FileOpCreate, URI: lsp.FileURI(path)}}},
	}})
	model = updatedAny.(Model)
	prepared := prepareCmd().(workspaceEditPreparedMsg)
	updatedAny, commitCmd := model.Update(prepared)
	model = updatedAny.(Model)
	model.workspaceEdits.cancel()
	_ = runWorkspaceEditCommands(t, model, commitCmd)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled file operation created target: %v", err)
	}
}

func TestWorkspaceEditAsyncCreateRejectsTargetAppearingBeforeCommit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "created.go")
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	updatedAny, prepareCmd := model.Update(lsp.ApplyEditRequestMsg{Edit: lsp.WorkspaceEdit{
		DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &lsp.WorkspaceFileOperation{Kind: lsp.FileOpCreate, URI: lsp.FileURI(path)}}},
	}})
	model = updatedAny.(Model)
	prepared := prepareCmd().(workspaceEditPreparedMsg)
	updatedAny, commitCmd := model.Update(prepared)
	model = updatedAny.(Model)
	if err := os.WriteFile(path, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := runWorkspaceEditCommands(t, model, commitCmd)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external" || !strings.Contains(updated.status, "rejected") {
		t.Fatalf("create race content=%q status=%q", data, updated.status)
	}
}

func TestWorkspaceEditAsyncRenameRejectsTargetAppearingBeforeCommit(t *testing.T) {
	root := t.TempDir()
	oldPath, newPath := filepath.Join(root, "old.go"), filepath.Join(root, "new.go")
	if err := os.WriteFile(oldPath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	updatedAny, prepareCmd := model.Update(lsp.ApplyEditRequestMsg{Edit: lsp.WorkspaceEdit{
		DocumentChanges: []lsp.WorkspaceDocumentChange{{FileOperation: &lsp.WorkspaceFileOperation{Kind: lsp.FileOpRename, OldURI: lsp.FileURI(oldPath), NewURI: lsp.FileURI(newPath)}}},
	}})
	model = updatedAny.(Model)
	prepared := prepareCmd().(workspaceEditPreparedMsg)
	updatedAny, commitCmd := model.Update(prepared)
	model = updatedAny.(Model)
	if err := os.WriteFile(newPath, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated := runWorkspaceEditCommands(t, model, commitCmd)
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("rename source disappeared: %v", err)
	}
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "external" || !strings.Contains(updated.status, "rejected") {
		t.Fatalf("rename race content=%q status=%q", data, updated.status)
	}
}

func TestWorkspaceEditAsyncRejectsQueueOverflow(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	idx := addDirtyEditor(t, &model, "main.go", "old\n", "old\n")
	path := model.editors[idx].Buffer.FilePath
	updatedAny, firstCmd := model.Update(lsp.ApplyEditRequestMsg{
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{lsp.FileURI(path): nil}},
	})
	model = updatedAny.(Model)
	for range maxQueuedWorkspaceEdits {
		updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
			Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{lsp.FileURI(path): nil}},
		})
		if cmd != nil {
			t.Fatal("queued request started work")
		}
		model = updatedAny.(Model)
	}
	responses := make(chan applyEditDecision, 1)
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		Edit:    lsp.WorkspaceEdit{},
		Respond: func(applied bool, reason string) { responses <- applyEditDecision{applied: applied, reason: reason} },
	})
	if cmd != nil || updatedAny.(Model).status == "" {
		t.Fatal("overflow did not reject synchronously")
	}
	select {
	case response := <-responses:
		if response.applied || !strings.Contains(response.reason, "too many") {
			t.Fatalf("overflow response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("overflow was not acknowledged")
	}
	_ = firstCmd
}

func TestWorkspaceEditAsyncRejectsDuplicateOpenDocumentIdentity(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	first := addDirtyEditor(t, &model, "same.go", "old\n", "old\n")
	_ = addDirtyEditor(t, &model, "same.go", "old\n", "old\n")
	responses := make(chan applyEditDecision, 1)
	updatedAny, cmd := model.Update(lsp.ApplyEditRequestMsg{
		Edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
			lsp.FileURI(model.editors[first].Buffer.FilePath): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
		}},
		Respond: func(applied bool, reason string) { responses <- applyEditDecision{applied: applied, reason: reason} },
	})
	updated := updatedAny.(Model)
	if cmd != nil || updated.workspaceEdits.inFlight {
		t.Fatal("duplicate document request started background work")
	}
	select {
	case response := <-responses:
		if response.applied || !strings.Contains(response.reason, "multiple tabs") {
			t.Fatalf("duplicate response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("duplicate document was not rejected")
	}
}

func TestWorkspaceEditAsyncAllowsDuplicateUnrelatedDocument(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	_ = addDirtyEditor(t, &model, "duplicate.go", "old\n", "old\n")
	_ = addDirtyEditor(t, &model, "duplicate.go", "old\n", "old\n")
	target := addDirtyEditor(t, &model, "target.go", "old\n", "old\n")
	targetPath := model.editors[target].Buffer.FilePath
	updated := applyWorkspaceEditAsyncForTest(t, model, lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		lsp.FileURI(targetPath): {{StartLine: 0, EndLine: 0, EndCol: 3, NewText: "new"}},
	}})
	if got := updated.editors[target].Buffer.Content(); got != "new\n" {
		t.Fatalf("unrelated duplicate prevented target edit: %q", got)
	}
}

func TestPrepareWorkspaceEditHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := prepareWorkspaceEdit(ctx, t.TempDir(), nil, lsp.WorkspaceEdit{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prepareWorkspaceEdit cancellation = %v, want context.Canceled", err)
	}
}
