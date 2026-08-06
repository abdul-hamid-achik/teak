package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/clipboard"
	"teak/internal/text"
	"teak/internal/ui"
)

var clipboardCopyPreparedBenchmarkSink ClipboardCopyPreparedMsg

func TestClipboardSelectionOverLimitIsRejectedBeforeMaterializing(t *testing.T) {
	content := strings.Repeat("x", clipboard.MaxClipboardBytes+1)
	ed := New(text.NewBufferFromBytes([]byte(content)), ui.DefaultTheme(), DefaultConfig())
	ed.Buffer.SelectAll()

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("oversized copy did not report a limit")
	}
	if got := updated.Buffer.Rope(); got != ed.Buffer.Rope() {
		t.Fatal("oversized copy changed the immutable document")
	}
	if msg, ok := cmd().(ClipboardOperationLimitMsg); !ok || msg.Operation != "Copy" || msg.MaxBytes != clipboard.MaxClipboardBytes {
		t.Fatalf("limit message = %#v, want copy limit", msg)
	}
}

func TestCutPreparesClipboardOffUpdateThenAppliesMatchingSnapshot(t *testing.T) {
	t.Setenv("TEAK_CLIPBOARD", "internal")
	content := strings.Repeat("a", asyncPasteThresholdBytes+1)
	ed := New(text.NewBufferFromBytes([]byte(content+"tail")), ui.DefaultTheme(), DefaultConfig())
	ed.Buffer.SetSelection(text.Position{}, text.Position{Line: 0, Col: len(content)})

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+x"})
	if got := updated.Buffer.Rope(); got != ed.Buffer.Rope() {
		t.Fatal("cut materialized or changed the buffer in Update")
	}
	prepared, ok := cmd().(ClipboardCopyPreparedMsg)
	if !ok {
		t.Fatalf("cut command = %T, want ClipboardCopyPreparedMsg", prepared)
	}
	if prepared.Err != nil || !prepared.Cut {
		t.Fatalf("prepared cut = %#v", prepared)
	}

	updated, followup := updated.Update(prepared)
	if followup == nil {
		t.Fatal("prepared cut did not schedule OS clipboard integration")
	}
	if got := updated.Buffer.Content(); got != "tail" {
		t.Fatalf("cut content = %q, want tail", got)
	}
	if got, _ := clipboard.Paste(); got != content {
		t.Fatalf("clipboard fallback = %d bytes, want %d", len(got), len(content))
	}
}

func TestLargePastePreparesOffUpdateAndDiscardsStaleResult(t *testing.T) {
	insert := strings.Repeat("p", asyncPasteThresholdBytes+1)
	ed := New(text.NewBufferFromBytes([]byte("base")), ui.DefaultTheme(), DefaultConfig())
	before := ed.Buffer.Rope()

	updated, cmd := ed.Update(tea.PasteMsg{Content: insert})
	if updated.Buffer.Rope() != before {
		t.Fatal("large terminal paste changed buffer in Update")
	}
	prepared, ok := cmd().(PastePreparedMsg)
	if !ok {
		t.Fatalf("paste command = %T, want PastePreparedMsg", prepared)
	}
	updated.Buffer.InsertAtCursor([]byte("!"))
	stale, _ := updated.Update(prepared)
	if got := stale.Buffer.Content(); got != "!base" {
		t.Fatalf("stale paste changed content to %q", got)
	}

	ed = New(text.NewBufferFromBytes([]byte("base")), ui.DefaultTheme(), DefaultConfig())
	updated, cmd = ed.Update(tea.PasteMsg{Content: insert})
	prepared = cmd().(PastePreparedMsg)
	updated, _ = updated.Update(prepared)
	if got := updated.Buffer.Rope().Len(); got != len(insert)+len("base") {
		t.Fatalf("large paste length = %d, want %d", got, len(insert)+len("base"))
	}
}

func TestLargePastePreservesUndoDirtyAndMultiCursorState(t *testing.T) {
	insert := strings.Repeat("p", asyncPasteThresholdBytes+1)
	ed := New(text.NewBufferFromBytes([]byte("one\ntwo")), ui.DefaultTheme(), DefaultConfig())
	ed.Buffer.SetSelection(text.Position{Line: 0, Col: 1}, text.Position{Line: 0, Col: 1})
	ed.Buffer.Selections.Add(text.Selection{Anchor: text.Position{Line: 1, Col: 1}, Head: text.Position{Line: 1, Col: 1}})

	updated, cmd := ed.Update(tea.PasteMsg{Content: insert})
	prepared := cmd().(PastePreparedMsg)
	updated, _ = updated.Update(prepared)
	if !updated.Buffer.Dirty() {
		t.Fatal("large paste did not mark buffer dirty")
	}
	if got := updated.Buffer.Selections.Count(); got != 2 {
		t.Fatalf("large paste selections = %d, want 2", got)
	}
	updated.Buffer.Undo()
	if got := updated.Buffer.Content(); got != "one\ntwo" {
		t.Fatalf("undo after large paste = %q", got)
	}
	updated.Buffer.Redo()
	if got := updated.Buffer.Selections.Count(); got != 1 {
		t.Fatalf("redo should use normal undo selection semantics, got %d selections", got)
	}
}

func TestCutDoesNotDeleteWhenClipboardPreparationFails(t *testing.T) {
	ed := New(text.NewBufferFromBytes([]byte{'a', 0xff, 'b'}), ui.DefaultTheme(), DefaultConfig())
	ed.Buffer.SetSelection(text.Position{}, text.Position{Line: 0, Col: 3})

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+x"})
	prepared := cmd().(ClipboardCopyPreparedMsg)
	if prepared.Err == nil {
		t.Fatal("invalid UTF-8 selection unexpectedly reached clipboard fallback")
	}
	updated, _ = updated.Update(prepared)
	if got := updated.Buffer.Rope().Len(); got != 3 {
		t.Fatalf("failed cut changed buffer length to %d", got)
	}
}

func TestPrepareClipboardCopyAvoidsTemporaryRopeAllocations(t *testing.T) {
	content := strings.Repeat("c", 64<<10)
	snapshot := text.NewFromString("prefix" + content + "suffix")
	cmd := prepareClipboardCopyCmd(
		1, 1, 1, snapshot,
		text.Position{Line: 0, Col: len("prefix")},
		text.Position{Line: 0, Col: len("prefix") + len(content)},
		len("prefix"), len("prefix")+len(content), false,
	)
	prepared := cmd().(ClipboardCopyPreparedMsg)
	if prepared.Err != nil || prepared.Content != content {
		t.Fatalf("prepared copy = (%d bytes, %v), want %d bytes", len(prepared.Content), prepared.Err, len(content))
	}

	allocs := testing.AllocsPerRun(20, func() {
		clipboardCopyPreparedBenchmarkSink = cmd().(ClipboardCopyPreparedMsg)
	})
	if allocs > 2 {
		t.Fatalf("clipboard preparation allocated %.0f times, want only content and message storage", allocs)
	}
}

func TestMultiLineCommandsRejectOversizedSelection(t *testing.T) {
	content := strings.Repeat("line\n", MaxSynchronousMultilineEditLines+1)
	ed := New(text.NewBufferFromBytes([]byte(content)), ui.DefaultTheme(), DefaultConfig())
	ed.Buffer.SelectAll()

	for _, key := range []string{"ctrl+]", "ctrl+/"} {
		updated, cmd := ed.Update(tea.KeyPressMsg{Text: key})
		if cmd == nil {
			t.Fatalf("%s did not report its multiline budget", key)
		}
		msg, ok := cmd().(MultilineEditLimitMsg)
		if !ok || msg.MaxLines != MaxSynchronousMultilineEditLines {
			t.Fatalf("%s limit message = %#v", key, msg)
		}
		if updated.Buffer.Rope() != ed.Buffer.Rope() {
			t.Fatalf("%s changed oversized selection", key)
		}
	}
}

func TestMultiLineCommandsCountAllCollapsedCursors(t *testing.T) {
	content := strings.Repeat("    line\n", MaxSynchronousMultilineEditLines+1)
	ed := New(text.NewBufferFromBytes([]byte(content)), ui.DefaultTheme(), DefaultConfig())
	selections := make([]text.Selection, MaxSynchronousMultilineEditLines+1)
	for line := range selections {
		pos := text.Position{Line: line, Col: 4}
		selections[line] = text.Selection{Anchor: pos, Head: pos}
	}
	ed.Buffer.RestoreSelections(selections, len(selections)-1)

	for _, key := range []string{"ctrl+]", "ctrl+/", "shift+tab"} {
		updated, cmd := ed.Update(tea.KeyPressMsg{Text: key})
		if cmd == nil {
			t.Fatalf("%s did not report the aggregate multiline budget", key)
		}
		msg, ok := cmd().(MultilineEditLimitMsg)
		if !ok || msg.MaxLines != MaxSynchronousMultilineEditLines {
			t.Fatalf("%s limit message = %#v", key, msg)
		}
		if updated.Buffer.Rope() != ed.Buffer.Rope() {
			t.Fatalf("%s changed a selection set above the line budget", key)
		}
	}
}

func TestToggleCommentReportsStructuralPrefixLimit(t *testing.T) {
	content := strings.Repeat(" ", text.MaxStructuralPrefixBytes+1) + "value"
	ed := New(text.NewBufferFromBytes([]byte(content)), ui.DefaultTheme(), DefaultConfig())
	ed.Config.CommentPrefix = "//"
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: len(content)})

	updated, cmd := ed.Update(tea.KeyPressMsg{Text: "ctrl+/"})
	if cmd == nil {
		t.Fatal("over-budget comment toggle did not report its structural prefix limit")
	}
	msg, ok := cmd().(StructuralEditLimitMsg)
	if !ok || msg.Operation != "Toggle comment" || msg.MaxBytes != text.MaxStructuralPrefixBytes {
		t.Fatalf("limit message = %#v, want toggle-comment prefix limit", msg)
	}
	if updated.Buffer.Rope() != ed.Buffer.Rope() || updated.Buffer.Dirty() {
		t.Fatal("over-budget comment toggle changed the buffer")
	}
}

func BenchmarkEditorLargePasteUpdate(b *testing.B) {
	content := strings.Repeat("p", asyncPasteThresholdBytes+1)
	ed := New(text.NewBufferFromBytes([]byte("base")), ui.DefaultTheme(), DefaultConfig())
	b.ResetTimer()
	for b.Loop() {
		_, _ = ed.Update(tea.PasteMsg{Content: content})
	}
}

func BenchmarkEditorLargeCopyUpdate(b *testing.B) {
	content := strings.Repeat("c", clipboard.MaxClipboardBytes)
	ed := New(text.NewBufferFromBytes([]byte(content)), ui.DefaultTheme(), DefaultConfig())
	ed.Buffer.SelectAll()
	b.ResetTimer()
	for b.Loop() {
		_, _ = ed.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	}
}

func BenchmarkPrepareClipboardCopyOneMiB(b *testing.B) {
	content := strings.Repeat("c", 1<<20)
	snapshot := text.NewFromString("prefix" + content + "suffix")
	cmd := prepareClipboardCopyCmd(
		1, 1, 1, snapshot,
		text.Position{Line: 0, Col: len("prefix")},
		text.Position{Line: 0, Col: len("prefix") + len(content)},
		len("prefix"), len("prefix")+len(content), false,
	)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		clipboardCopyPreparedBenchmarkSink = cmd().(ClipboardCopyPreparedMsg)
	}
}

func BenchmarkPrepareClipboardCopyMultiRangeOneMiB(b *testing.B) {
	content := strings.Repeat("c", 1<<20)
	snapshot := text.NewFromString("prefix" + content + "suffix")
	third := len(content) / 3
	base := len("prefix")
	selections := []text.Selection{
		{Anchor: text.Position{Col: base}, Head: text.Position{Col: base + third}},
		{Anchor: text.Position{Col: base + third}, Head: text.Position{Col: base + 2*third}},
		{Anchor: text.Position{Col: base + 2*third}, Head: text.Position{Col: base + len(content)}},
	}
	ranges := []text.ByteRange{
		{Start: base, End: base + third},
		{Start: base + third, End: base + 2*third},
		{Start: base + 2*third, End: base + len(content)},
	}
	cmd := prepareClipboardSelectionsCopyCmd(1, 1, 1, snapshot, selections, 2, ranges, false)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		clipboardCopyPreparedBenchmarkSink = cmd().(ClipboardCopyPreparedMsg)
	}
}

func BenchmarkEditorMultilineIndentBudget(b *testing.B) {
	content := strings.Repeat("line\n", MaxSynchronousMultilineEditLines)
	// Indent mutates the buffer, so each iteration needs fresh, unindented
	// content rather than reusing one buffer across b.N runs (which would
	// keep re-indenting already-indented lines and drift the measured
	// operation). A fresh *text.Buffer is cheap; editor.New() is not (it
	// also matches a chroma lexer and wires up a highlighter), so hoist the
	// Editor construction out of the loop and only rebuild the buffer.
	ed := New(text.NewBufferFromBytes([]byte(content)), ui.DefaultTheme(), DefaultConfig())
	b.ResetTimer()
	for b.Loop() {
		ed.Buffer = text.NewBufferFromBytes([]byte(content))
		ed.Buffer.SelectAll()
		updated, _ := ed.Update(tea.KeyPressMsg{Text: "ctrl+]"})
		if !updated.Buffer.Dirty() {
			b.Fatal("indent did not apply")
		}
	}
}
