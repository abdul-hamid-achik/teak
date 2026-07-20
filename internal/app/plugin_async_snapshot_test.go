package app

import (
	"testing"

	"teak/internal/editor"
	"teak/internal/text"
)

func TestPluginAsyncRuntimeCapturesImmutableRopeWithoutMaterializing(t *testing.T) {
	model := newInputRoutingTestModel(t)
	buf := text.NewBufferFromBytes([]byte("package main\n"))
	buf.FilePath = "main.go"
	model.editors[0] = editor.New(buf, model.theme, editor.DefaultConfig())

	source := model.activeEditor().Buffer.Rope()
	runtime := newPluginAsyncRuntime(model)

	if runtime.buffer.Rope() != source {
		t.Fatal("newPluginAsyncRuntime materialized the document instead of sharing its immutable rope")
	}
}

func TestPluginAsyncRuntimeAppliesPreparedRopeWithoutMaterializing(t *testing.T) {
	model := newInputRoutingTestModel(t)
	buf := text.NewBufferFromBytes([]byte("package main\n"))
	buf.FilePath = "main.go"
	model.editors[0] = editor.New(buf, model.theme, editor.DefaultConfig())

	runtime := newPluginAsyncRuntime(model)
	if err := runtime.InsertText("// generated\n"); err != nil {
		t.Fatalf("InsertText() error = %v", err)
	}
	prepared := runtime.buffer.Rope()
	beforeVersion := model.activeEditor().Buffer.Version()

	cmd := runtime.apply(&model)

	if model.activeEditor().Buffer.Rope() != prepared {
		t.Fatal("plugin result was materialized instead of applying the prepared immutable rope")
	}
	if got, want := model.activeEditor().Buffer.Version(), beforeVersion+1; got != want {
		t.Fatalf("buffer version = %d, want %d", got, want)
	}
	if got, want := model.activeEditor().Buffer.Cursor, runtime.buffer.Cursor; got != want {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	if cmd == nil {
		t.Fatal("plugin edit did not schedule downstream editor synchronization")
	}
}

func TestPluginAsyncRuntimeInvalidatesHighlightAndRebuildsWordWrap(t *testing.T) {
	model := newInputRoutingTestModel(t)
	buf := text.NewBufferFromBytes([]byte("package main\n"))
	buf.FilePath = "main.go"
	cfg := editor.DefaultConfig()
	cfg.WordWrap = true
	model.editors[0] = editor.New(buf, model.theme, cfg)
	model.editors[0].SetSize(16, 4)
	model.editors[0].Highlighter.Tokenize(buf.Bytes())
	beforeBuild := model.editors[0].Wrap.BuildCount()

	runtime := newPluginAsyncRuntime(model)
	if err := runtime.SetBufferText("package main\n// this replacement is deliberately wider than the viewport\n"); err != nil {
		t.Fatalf("SetBufferText() error = %v", err)
	}
	runtime.apply(&model)

	if !model.editors[0].Highlighter.IsDirty() {
		t.Fatal("plugin snapshot retained syntax tokens from the previous rope")
	}
	if got := model.editors[0].Wrap.BuildCount(); got <= beforeBuild {
		t.Fatalf("word-wrap build count = %d, want greater than %d after snapshot replacement", got, beforeBuild)
	}
	if got := model.editors[0].Wrap.LineRows(1); got <= 1 {
		t.Fatalf("wrapped rows for replacement line = %d, want a rebuilt multi-row line", got)
	}
}

func TestPluginAsyncRuntimeAppliesToOwningInactiveTab(t *testing.T) {
	model := newInputRoutingTestModel(t)

	first := text.NewBufferFromBytes([]byte("first"))
	first.FilePath = "first.go"
	model.editors[0] = editor.New(first, model.theme, editor.DefaultConfig())
	model.tabBar.Tabs[0].FilePath = first.FilePath
	model.tabBar.Tabs[0].Preview = true

	runtime := newPluginAsyncRuntime(model)
	if err := runtime.InsertText(" edited"); err != nil {
		t.Fatalf("InsertText() error = %v", err)
	}

	second := text.NewBufferFromBytes([]byte("second"))
	second.FilePath = "second.go"
	model.editors = append(model.editors, editor.New(second, model.theme, editor.DefaultConfig()))
	model.tabBar.AddTab("second.go", second.FilePath)
	model.activeTab = 1
	model.tabBar.ActiveIdx = 1

	runtime.apply(&model)

	if model.activeTab != 1 || model.tabBar.ActiveIdx != 1 {
		t.Fatalf("active tab changed to model=%d tabbar=%d, want 1/1", model.activeTab, model.tabBar.ActiveIdx)
	}
	if got, want := model.editors[0].Buffer.Content(), " editedfirst"; got != want {
		t.Fatalf("owning tab content = %q, want %q", got, want)
	}
	if got := model.editors[1].Buffer.Content(); got != "second" {
		t.Fatalf("unrelated active tab content = %q, want unchanged", got)
	}
	if !model.tabBar.Tabs[0].Dirty || model.tabBar.Tabs[0].Preview {
		t.Fatalf("owning tab state = dirty:%v preview:%v, want dirty and pinned", model.tabBar.Tabs[0].Dirty, model.tabBar.Tabs[0].Preview)
	}
}

func TestPluginAsyncRuntimeDiscardsStaleSnapshotWithoutSideEffects(t *testing.T) {
	model := newInputRoutingTestModel(t)
	buf := text.NewBufferFromBytes([]byte("base"))
	buf.FilePath = "main.go"
	model.editors[0] = editor.New(buf, model.theme, editor.DefaultConfig())
	model.editors[0].Highlighter.Tokenize(buf.Bytes())

	runtime := newPluginAsyncRuntime(model)
	if err := runtime.InsertText("plugin "); err != nil {
		t.Fatalf("InsertText() error = %v", err)
	}
	model.activeEditor().Buffer.InsertAtCursor([]byte("user "))
	current := model.activeEditor().Buffer.Rope()

	cmd := runtime.apply(&model)

	if model.activeEditor().Buffer.Rope() != current {
		t.Fatal("stale plugin snapshot replaced a newer live rope")
	}
	if model.activeEditor().Highlighter.IsDirty() {
		t.Fatal("stale plugin snapshot invalidated the live highlighter")
	}
	if cmd != nil {
		t.Fatal("stale plugin snapshot scheduled downstream side effects")
	}
}

func TestPluginAsyncSnapshotPreservesUndoRedoAndCollapsesMulticursor(t *testing.T) {
	model := newInputRoutingTestModel(t)
	buf := text.NewBufferFromBytes([]byte("alpha beta"))
	buf.FilePath = "main.go"
	buf.SetCursor(text.Position{Col: 5})
	buf.Selections.Add(text.Selection{
		Anchor: text.Position{Col: 6},
		Head:   text.Position{Col: 10},
	})
	model.editors[0] = editor.New(buf, model.theme, editor.DefaultConfig())
	source, sourceCursor := buf.Rope(), buf.Cursor

	runtime := newPluginAsyncRuntime(model)
	if err := runtime.InsertText("B"); err != nil {
		t.Fatalf("InsertText() error = %v", err)
	}
	prepared, preparedCursor := runtime.buffer.Rope(), runtime.buffer.Cursor
	runtime.apply(&model)

	live := model.activeEditor().Buffer
	if got := live.Selections.Count(); got != 1 {
		t.Fatalf("selection count after full-document plugin edit = %d, want 1", got)
	}
	live.Undo()
	if live.Rope() != source || live.Cursor != sourceCursor || live.Dirty() {
		t.Fatalf("undo state = rope:%v cursor:%+v dirty:%v, want original clean snapshot", live.Rope() == source, live.Cursor, live.Dirty())
	}
	live.Redo()
	if live.Rope() != prepared || live.Cursor != preparedCursor || !live.Dirty() {
		t.Fatalf("redo state = rope:%v cursor:%+v dirty:%v, want prepared dirty snapshot", live.Rope() == prepared, live.Cursor, live.Dirty())
	}
}
