package app

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/lsp"
	"teak/internal/text"
)

var benchmarkFormatUpdateSink Model
var benchmarkFormatPreparedSink formatPreparedMsg

func completeFormatPreparation(t testing.TB, model Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("format result did not schedule background preparation")
	}
	msg := cmd()
	if _, ok := msg.(formatPreparedMsg); !ok {
		t.Fatalf("format preparation returned %T, want formatPreparedMsg", msg)
	}
	updatedAny, next := model.Update(msg)
	return updatedAny.(Model), next
}

func TestFormatResultDefersPreparationOutsideUpdate(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	index := addDirtyEditor(t, &model, "format.go", "before\n", "before\n")
	path := model.editors[index].Buffer.FilePath
	version := model.editors[index].Buffer.Version()

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		FilePath:       path,
		BaseVersion:    version,
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
		Edits: []lsp.TextEdit{{
			StartLine: 0,
			StartCol:  0,
			EndLine:   0,
			EndCol:    len("before"),
			NewText:   "after",
		}},
	})
	updated := updatedAny.(Model)

	if got := updated.editors[index].Buffer.Content(); got != "before\n" {
		t.Fatalf("FormatResultMsg mutated buffer inside Update: got %q", got)
	}
	if cmd == nil {
		t.Fatal("FormatResultMsg did not schedule background preparation")
	}
}

func TestPreparedFormatDoesNotOverwriteNewerEdit(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	index := addDirtyEditor(t, &model, "format.go", "before\n", "before\n")
	path := model.editors[index].Buffer.FilePath
	version := model.editors[index].Buffer.Version()

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		FilePath:       path,
		BaseVersion:    version,
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
		Edits: []lsp.TextEdit{{
			StartLine: 0,
			EndLine:   0,
			EndCol:    len("before"),
			NewText:   "formatted",
		}},
	})
	updated := updatedAny.(Model)
	prepared := cmd()

	updated.editors[index].Buffer.SelectAll()
	updated.editors[index].Buffer.InsertAtCursor([]byte("newer\n"))
	finalAny, next := updated.Update(prepared)
	final := finalAny.(Model)

	if next != nil {
		t.Fatal("stale prepared format scheduled post-edit work")
	}
	if got, want := final.editors[index].Buffer.Content(), "newer\n"; got != want {
		t.Fatalf("stale prepared format changed content: got %q, want %q", got, want)
	}
	if got, want := final.status, "Formatting result discarded; newer edits remain unsaved"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestPreparedFormatRestoresMappedSelections(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	index := addDirtyEditor(t, &model, "format.go", "ab\ncd\n", "ab\ncd\n")
	buffer := model.editors[index].Buffer
	buffer.RestoreSelections([]text.Selection{
		{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
		{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
	}, 1)

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		FilePath:       buffer.FilePath,
		BaseVersion:    buffer.Version(),
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
		Edits:          []lsp.TextEdit{{NewText: "XX"}},
	})
	updated, _ := completeFormatPreparation(t, updatedAny.(Model), cmd)

	if got, want := updated.editors[index].Buffer.Content(), "XXab\ncd\n"; got != want {
		t.Fatalf("formatted content = %q, want %q", got, want)
	}
	selections := updated.editors[index].Buffer.Selections
	if got, want := selections.PrimaryIndex(), 1; got != want {
		t.Fatalf("primary selection = %d, want %d", got, want)
	}
	want := []text.Selection{
		{Anchor: text.Position{Line: 0, Col: 3}, Head: text.Position{Line: 0, Col: 3}},
		{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}},
	}
	got := selections.All()
	if len(got) != len(want) {
		t.Fatalf("selection count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("selection %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got, want := updated.editors[index].Buffer.Cursor, (text.Position{Line: 1, Col: 1}); got != want {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
}

func TestPreparedFormatWithoutChangesFinishesNoOp(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	index := addDirtyEditor(t, &model, "format.go", "same\n", "same\n")
	buffer := model.editors[index].Buffer

	updatedAny, cmd := model.Update(lsp.FormatResultMsg{
		FilePath:       buffer.FilePath,
		BaseVersion:    buffer.Version(),
		HasBaseVersion: true,
		Status:         lsp.FormatApplied,
	})
	updated, next := completeFormatPreparation(t, updatedAny.(Model), cmd)

	if next != nil {
		t.Fatal("no-op format scheduled post-edit work")
	}
	if got, want := updated.status, "No formatting changes"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func BenchmarkFormatResultDispatchFourThousandEdits(b *testing.B) {
	const editCount = 4096
	content := strings.Repeat("x\n", editCount)
	edits := make([]lsp.TextEdit, editCount)
	for line := range edits {
		edits[line] = lsp.TextEdit{
			StartLine: line,
			EndLine:   line,
			EndCol:    1,
			NewText:   "y",
		}
	}

	model := newSaveFlowModel(b, config.DefaultConfig(), b.TempDir())
	index := addDirtyEditor(b, &model, "format.go", content, content)
	path := model.editors[index].Buffer.FilePath
	theme := model.theme
	cfg := model.editors[index].Config

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		buf := text.NewBufferFromBytes([]byte(content))
		buf.FilePath = path
		ed := editor.New(buf, theme, cfg)
		ed.SetSize(80, 24)
		model.editors[index] = ed
		model.tabBar.Tabs[index].Dirty = false
		msg := lsp.FormatResultMsg{
			FilePath:       path,
			BaseVersion:    buf.Version(),
			HasBaseVersion: true,
			Status:         lsp.FormatApplied,
			Edits:          edits,
		}
		b.StartTimer()
		updatedAny, _ := model.Update(msg)
		b.StopTimer()
		updated := updatedAny.(Model)
		benchmarkFormatUpdateSink = updated
		updated.formatPreparations.cancelAll()
		b.StartTimer()
	}
}

func BenchmarkPrepareFormattingFourThousandEdits(b *testing.B) {
	const editCount = 4096
	content := strings.Repeat("x\n", editCount)
	edits := make([]lsp.TextEdit, editCount)
	for line := range edits {
		edits[line] = lsp.TextEdit{
			StartLine: line,
			EndLine:   line,
			EndCol:    1,
			NewText:   "y",
		}
	}
	request := formatPreparationRequest{
		Generation: 1,
		Source:     text.NewFromString(content),
		Selections: []text.Selection{{}},
	}
	cmd := prepareFormatCmd(context.Background(), request, edits)

	b.ReportAllocs()
	for b.Loop() {
		benchmarkFormatPreparedSink = cmd().(formatPreparedMsg)
	}
}
