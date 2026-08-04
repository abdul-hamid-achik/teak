package app

import (
	"os"
	"path/filepath"
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

func TestPluginAsyncRuntimeQueuesNewBufferEffect(t *testing.T) {
	model := newInputRoutingTestModel(t)
	runtime := newPluginAsyncRuntime(model)

	bufnr, err := runtime.NewBuffer()
	if err != nil {
		t.Fatalf("NewBuffer() error = %v", err)
	}
	if bufnr != 2 {
		t.Fatalf("NewBuffer() = %d, want 2 for the second tab", bufnr)
	}
	if got := len(model.editors); got != 1 {
		t.Fatalf("async NewBuffer mutated model before apply: editors=%d", got)
	}

	runtime.apply(&model)
	if got := len(model.editors); got != 2 {
		t.Fatalf("editor count after NewBuffer() = %d, want 2", got)
	}
	if got := model.activeTab; got != 1 {
		t.Fatalf("active tab after NewBuffer() = %d, want 1", got)
	}
	if got := model.tabBar.Tabs[1].Label; got != "Untitled-1" {
		t.Fatalf("new tab label = %q, want Untitled-1", got)
	}
	if got := model.editors[1].Buffer.FilePath; got != "" {
		t.Fatalf("new buffer filepath = %q, want empty", got)
	}
}

func TestPluginAsyncRuntimeAppliesEditsToTabSelectedByCallback(t *testing.T) {
	model := newInputRoutingTestModel(t)

	first := text.NewBufferFromBytes([]byte("first"))
	first.FilePath = "first.go"
	model.editors[0] = editor.New(first, model.theme, editor.DefaultConfig())
	model.tabBar.Tabs[0].FilePath = first.FilePath

	second := text.NewBufferFromBytes([]byte("second"))
	second.FilePath = "second.go"
	model.editors = append(model.editors, editor.New(second, model.theme, editor.DefaultConfig()))
	model.tabBar.AddTab("second.go", second.FilePath)

	runtime := newPluginAsyncRuntime(model)
	if err := runtime.SetActiveTab(1); err != nil {
		t.Fatalf("SetActiveTab() error = %v", err)
	}
	if err := runtime.SetBufferText("second edited"); err != nil {
		t.Fatalf("SetBufferText() error = %v", err)
	}

	runtime.apply(&model)

	if got := model.editors[0].Buffer.Content(); got != "first" {
		t.Fatalf("first tab content = %q, want unchanged first tab", got)
	}
	if got := model.editors[1].Buffer.Content(); got != "second edited" {
		t.Fatalf("selected tab content = %q, want second edited", got)
	}
	if got := model.activeTab; got != 1 {
		t.Fatalf("active tab = %d, want 1", got)
	}
}

func TestPluginAsyncRuntimeAppliesEditsToNewBufferCreatedByCallback(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.editors[0].Buffer.SetCursor(text.Position{})
	original := model.editors[0].Buffer.Content()

	runtime := newPluginAsyncRuntime(model)
	if _, err := runtime.NewBuffer(); err != nil {
		t.Fatalf("NewBuffer() error = %v", err)
	}
	if err := runtime.SetBufferText("new content"); err != nil {
		t.Fatalf("SetBufferText() error = %v", err)
	}

	runtime.apply(&model)

	if got := model.editors[0].Buffer.Content(); got != original {
		t.Fatalf("original tab content = %q, want %q", got, original)
	}
	if got := len(model.editors); got != 2 {
		t.Fatalf("editor count = %d, want 2", got)
	}
	if got := model.editors[1].Buffer.Content(); got != "new content" {
		t.Fatalf("new buffer content = %q, want new content", got)
	}
	if got := model.activeTab; got != 1 {
		t.Fatalf("active tab = %d, want 1", got)
	}
}

func TestPluginAsyncRuntimeAppliesEditsToFileOpenedByCallback(t *testing.T) {
	model := newInputRoutingTestModel(t)
	path := filepath.Join(t.TempDir(), "opened.go")
	if err := os.WriteFile(path, []byte("loaded later"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	runtime := newPluginAsyncRuntime(model)
	if err := runtime.OpenFile(path); err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	if err := runtime.SetBufferText("plugin content"); err != nil {
		t.Fatalf("SetBufferText() after OpenFile() error = %v", err)
	}

	cmd := runtime.apply(&model)
	if cmd == nil {
		t.Fatal("OpenFile() did not retain the asynchronous file-load command")
	}
	if got := model.activeEditor().Buffer.FilePath; got != path {
		t.Fatalf("opened filepath = %q, want %q", got, path)
	}
	if got := model.activeEditor().Buffer.Content(); got != "plugin content" {
		t.Fatalf("opened buffer content = %q, want plugin content", got)
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
