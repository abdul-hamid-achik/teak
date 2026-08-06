package app

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"slices"
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

func legacyPrepareFormatForTest(t testing.TB, request formatPreparationRequest, edits []lsp.TextEdit) formatPreparedMsg {
	t.Helper()
	buffer := text.NewBufferFromRope(request.Source)
	buffer.SetCursor(request.Cursor)
	buffer.RestoreSelections(request.Selections, request.Primary)
	if _, err := prepareFormattingTextEdits(context.Background(), buffer.Rope(), edits); err != nil {
		t.Fatalf("legacy format validation: %v", err)
	}
	prepared := formatPreparedMsg{
		formatPreparationRequest: request,
		Applied:                  applyTextEditsSequentiallyForTest(buffer, edits),
	}
	prepared.Result = buffer.Rope()
	prepared.ResultCursor = buffer.Cursor
	prepared.ResultSelections = append([]text.Selection(nil), buffer.Selections.All()...)
	prepared.ResultPrimary = buffer.Selections.PrimaryIndex()
	return prepared
}

func applyTextEditsSequentiallyForTest(buffer *text.Buffer, edits []lsp.TextEdit) int {
	sortedEdits := append([]lsp.TextEdit(nil), edits...)
	slices.SortFunc(sortedEdits, func(a, b lsp.TextEdit) int {
		if a.StartLine != b.StartLine {
			return b.StartLine - a.StartLine
		}
		return b.StartCol - a.StartCol
	})
	for _, edit := range sortedEdits {
		buffer.ReplaceRange(
			text.Position{Line: edit.StartLine, Col: edit.StartCol},
			text.Position{Line: edit.EndLine, Col: edit.EndCol},
			[]byte(edit.NewText),
		)
	}
	if len(sortedEdits) > 0 {
		buffer.ClampCursor()
	}
	return len(sortedEdits)
}

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

func TestPrepareFormatHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := formatPreparationRequest{
		Generation: 1,
		Source:     text.NewFromString("before\n"),
		Selections: []text.Selection{{}},
	}
	prepared := prepareFormatCmd(ctx, request, []lsp.TextEdit{{
		StartLine: 0,
		EndLine:   0,
		EndCol:    len("before"),
		NewText:   "after",
	}})().(formatPreparedMsg)
	if !errors.Is(prepared.Err, context.Canceled) {
		t.Fatalf("prepareFormatCmd() error = %v, want context.Canceled", prepared.Err)
	}
	if prepared.Result != nil {
		t.Fatal("canceled format preparation retained a result rope")
	}
}

func TestPrepareFormatSnapshotMatchesSequentialBufferEdits(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		cursor     text.Position
		selections []text.Selection
		primary    int
		edits      []lsp.TextEdit
	}{
		{
			name:    "unsorted inserts deletes and replacements",
			content: "abcdef\n",
			cursor:  text.Position{Line: 0, Col: 3},
			selections: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
				{Anchor: text.Position{Line: 0, Col: 3}, Head: text.Position{Line: 0, Col: 3}},
				{Anchor: text.Position{Line: 0, Col: 6}, Head: text.Position{Line: 0, Col: 6}},
			},
			primary: 1,
			edits: []lsp.TextEdit{
				{StartLine: 0, StartCol: 4, EndLine: 0, EndCol: 6, NewText: "Z"},
				{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 0, NewText: ">>"},
				{StartLine: 0, StartCol: 2, EndLine: 0, EndCol: 4},
			},
		},
		{
			name:    "multiline replacement maps reversed selection",
			content: "one\ntwo\nthree\n",
			cursor:  text.Position{Line: 2, Col: 2},
			selections: []text.Selection{{
				Anchor: text.Position{Line: 2, Col: 4},
				Head:   text.Position{Line: 0, Col: 1},
			}},
			edits: []lsp.TextEdit{{
				StartLine: 0,
				StartCol:  2,
				EndLine:   2,
				EndCol:    1,
				NewText:   "X\nY",
			}},
		},
		{
			name:    "adjacent edits preserve boundary semantics",
			content: "abcd",
			cursor:  text.Position{Line: 0, Col: 2},
			selections: []text.Selection{
				{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 1}},
				{Anchor: text.Position{Line: 0, Col: 2}, Head: text.Position{Line: 0, Col: 2}},
				{Anchor: text.Position{Line: 0, Col: 3}, Head: text.Position{Line: 0, Col: 3}},
			},
			primary: 1,
			edits: []lsp.TextEdit{
				{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 2, NewText: "left"},
				{StartLine: 0, StartCol: 2, EndLine: 0, EndCol: 4, NewText: "right"},
			},
		},
		{
			name:    "utf8 byte boundaries remain intact",
			content: "aéz\n",
			cursor:  text.Position{Line: 0, Col: 3},
			selections: []text.Selection{{
				Anchor: text.Position{Line: 0, Col: 1},
				Head:   text.Position{Line: 0, Col: 3},
			}},
			edits: []lsp.TextEdit{
				{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 3, NewText: "λ"},
				{StartLine: 0, StartCol: 4, EndLine: 0, EndCol: 4, NewText: "🙂"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := formatPreparationRequest{
				Generation: 1,
				Source:     text.NewFromString(tt.content),
				Cursor:     tt.cursor,
				Selections: tt.selections,
				Primary:    tt.primary,
			}
			want := legacyPrepareFormatForTest(t, request, tt.edits)
			got := prepareFormatCmd(context.Background(), request, tt.edits)().(formatPreparedMsg)
			if got.Err != nil {
				t.Fatalf("prepareFormatCmd() error = %v", got.Err)
			}
			if got.Result.String() != want.Result.String() {
				t.Errorf("result = %q, want %q", got.Result.String(), want.Result.String())
			}
			if got.ResultCursor != want.ResultCursor {
				t.Errorf("cursor = %+v, want %+v", got.ResultCursor, want.ResultCursor)
			}
			if !reflect.DeepEqual(got.ResultSelections, want.ResultSelections) {
				t.Errorf("selections = %+v, want %+v", got.ResultSelections, want.ResultSelections)
			}
			if got.ResultPrimary != want.ResultPrimary {
				t.Errorf("primary = %d, want %d", got.ResultPrimary, want.ResultPrimary)
			}
		})
	}
}

func TestPrepareFormatSnapshotMatchesSequentialBufferEditsRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(20260805))
	replacements := []string{"", "Q", "XY", "\n", "Z\nW"}
	for iteration := 0; iteration < 200; iteration++ {
		contentLen := 8 + rng.Intn(57)
		content := strings.Repeat("a", contentLen)
		edits := make([]lsp.TextEdit, 0, 8)
		position := 0
		for len(edits) < 8 && position <= contentLen {
			start := position + rng.Intn(3)
			if start > contentLen {
				break
			}
			maxDelete := min(3, contentLen-start)
			deleteLen := rng.Intn(maxDelete + 1)
			edits = append(edits, lsp.TextEdit{
				StartLine: 0,
				StartCol:  start,
				EndLine:   0,
				EndCol:    start + deleteLen,
				NewText:   replacements[rng.Intn(len(replacements))],
			})
			position = start + max(1, deleteLen)
		}
		rng.Shuffle(len(edits), func(i, j int) { edits[i], edits[j] = edits[j], edits[i] })

		selections := make([]text.Selection, 4)
		for i := range selections {
			position := text.Position{Line: 0, Col: rng.Intn(contentLen + 1)}
			selections[i] = text.Selection{Anchor: position, Head: position}
		}
		request := formatPreparationRequest{
			Generation: 1,
			Source:     text.NewFromString(content),
			Cursor:     selections[0].Head,
			Selections: selections,
			Primary:    rng.Intn(len(selections)),
		}

		want := legacyPrepareFormatForTest(t, request, edits)
		got := prepareFormatCmd(context.Background(), request, edits)().(formatPreparedMsg)
		if got.Err != nil {
			t.Fatalf("iteration %d: prepareFormatCmd() error = %v", iteration, got.Err)
		}
		if got.Result.String() != want.Result.String() ||
			got.ResultCursor != want.ResultCursor ||
			!reflect.DeepEqual(got.ResultSelections, want.ResultSelections) ||
			got.ResultPrimary != want.ResultPrimary {
			t.Fatalf(
				"iteration %d mismatch:\nresult %q / %q\ncursor %+v / %+v\nselections %+v / %+v\nprimary %d / %d",
				iteration,
				got.Result.String(), want.Result.String(),
				got.ResultCursor, want.ResultCursor,
				got.ResultSelections, want.ResultSelections,
				got.ResultPrimary, want.ResultPrimary,
			)
		}
	}
}

func TestPrepareFormattingAllocationBudget(t *testing.T) {
	const editCount = 1024
	content := strings.Repeat("x\n", editCount)
	edits := make([]lsp.TextEdit, editCount)
	for line := range edits {
		edits[line] = lsp.TextEdit{StartLine: line, EndLine: line, EndCol: 1, NewText: "y"}
	}
	request := formatPreparationRequest{
		Generation: 1,
		Source:     text.NewFromString(content),
		Selections: []text.Selection{{}},
	}
	cmd := prepareFormatCmd(context.Background(), request, edits)

	allocs := testing.AllocsPerRun(1, func() {
		prepared := cmd().(formatPreparedMsg)
		if prepared.Err != nil || prepared.Result == nil {
			t.Fatalf("format preparation = (%v, %v)", prepared.Result, prepared.Err)
		}
		benchmarkFormatPreparedSink = prepared
	})
	if allocs > 500 {
		t.Fatalf("format preparation allocations = %.0f, want <= 500", allocs)
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
