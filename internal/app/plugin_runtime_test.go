package app

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/overlay"
	"teak/internal/plugin"
	"teak/internal/text"
)

func TestPluginRuntimeCreatesAndClosesFloat(t *testing.T) {
	model := newInputRoutingTestModel(t)
	runtime := newPluginRuntime(&model)
	id, err := runtime.NewFloat(plugin.UIFloatOptions{
		Title:   "Preview",
		Content: "hello",
		Width:   40,
		Height:  4,
	})
	if err != nil {
		t.Fatalf("NewFloat() error = %v", err)
	}
	float, ok := model.overlayStack.Top().(*overlay.Float)
	if !ok || float.ID != id {
		t.Fatalf("top float = %#v, want ID %d", model.overlayStack.Top(), id)
	}
	if err := runtime.CloseFloat(id); err != nil {
		t.Fatalf("CloseFloat() error = %v", err)
	}
	if !model.overlayStack.IsEmpty() {
		t.Fatal("CloseFloat() left an overlay on the stack")
	}
}

func TestPluginRuntimeSetsAndClearsHighlights(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer.InsertAtCursor([]byte("hello world"))
	runtime := newPluginRuntime(&model)
	request := plugin.UIHighlightRequest{
		Namespace: 7,
		Highlights: []plugin.UIHighlight{{
			Line: 0, StartCol: 6, EndCol: 11, Foreground: "#88c0d0", Bold: true,
		}},
	}
	if err := runtime.SetHighlights(request); err != nil {
		t.Fatalf("SetHighlights() error = %v", err)
	}
	ranges := model.activeEditor().PluginHighlightRanges()
	if len(ranges) != 1 || ranges[0].Namespace != 7 || ranges[0].StartCol != 6 || ranges[0].EndCol != 11 {
		t.Fatalf("PluginHighlightRanges() = %#v", ranges)
	}
	if err := runtime.ClearHighlights(7); err != nil {
		t.Fatalf("ClearHighlights() error = %v", err)
	}
	if got := model.activeEditor().PluginHighlightRanges(); len(got) != 0 {
		t.Fatalf("PluginHighlightRanges() after clear = %#v, want empty", got)
	}
}

func TestPluginAsyncRuntimeDiscardsStaleHighlights(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer.InsertAtCursor([]byte("hello"))
	runtime := newPluginAsyncRuntime(model)
	if err := runtime.SetHighlights(plugin.UIHighlightRequest{
		Namespace:  3,
		Highlights: []plugin.UIHighlight{{Line: 0, StartCol: 0, EndCol: 5}},
	}); err != nil {
		t.Fatalf("SetHighlights() error = %v", err)
	}
	model.activeEditor().Buffer.InsertAtCursor([]byte("!"))
	if cmd := runtime.apply(&model); cmd != nil {
		_ = cmd()
	}
	if got := model.activeEditor().PluginHighlightRanges(); len(got) != 0 {
		t.Fatalf("stale highlights = %#v, want empty", got)
	}
	if model.status != "Plugin highlights discarded: buffer changed while callback ran" {
		t.Fatalf("stale highlight status = %q", model.status)
	}
}

func TestPluginAsyncRuntimeAppliesHighlightsToStableTarget(t *testing.T) {
	model := newInputRoutingTestModel(t)
	model.activeEditor().Buffer.InsertAtCursor([]byte("hello"))
	runtime := newPluginAsyncRuntime(model)
	if err := runtime.SetHighlights(plugin.UIHighlightRequest{
		Namespace:  4,
		Highlights: []plugin.UIHighlight{{Line: 0, StartCol: 0, EndCol: 5, Underline: true}},
	}); err != nil {
		t.Fatalf("SetHighlights() error = %v", err)
	}
	_ = runtime.apply(&model)
	if got := model.activeEditor().PluginHighlightRanges(); len(got) != 1 || got[0].Namespace != 4 {
		t.Fatalf("stable async highlights = %#v, want namespace 4", got)
	}
	if err := newPluginRuntime(&model).ClearHighlights(0); err != nil {
		t.Fatalf("ClearHighlights(all) error = %v", err)
	}
	if got := model.activeEditor().PluginHighlightRanges(); len(got) != 0 {
		t.Fatalf("highlights after clear-all = %#v, want empty", got)
	}
}

func TestPluginRuntimeBoundsHighlightNamespacesAndTotalRanges(t *testing.T) {
	model := newInputRoutingTestModel(t)
	runtime := newPluginRuntime(&model)
	for namespace := 1; namespace <= maxPluginHighlightNamespaces; namespace++ {
		if err := runtime.SetHighlights(plugin.UIHighlightRequest{
			Namespace:  namespace,
			Highlights: []plugin.UIHighlight{{Line: 0, StartCol: 0, EndCol: 1}},
		}); err != nil {
			t.Fatalf("SetHighlights(namespace %d) error = %v", namespace, err)
		}
	}
	if err := runtime.SetHighlights(plugin.UIHighlightRequest{
		Namespace:  maxPluginHighlightNamespaces + 1,
		Highlights: []plugin.UIHighlight{{Line: 0, StartCol: 0, EndCol: 1}},
	}); err == nil {
		t.Fatal("SetHighlights() accepted too many namespaces")
	}

	model = newInputRoutingTestModel(t)
	runtime = newPluginRuntime(&model)
	for namespace := 1; namespace <= 8; namespace++ {
		ranges := make([]plugin.UIHighlight, maxPluginHighlightRanges)
		for i := range ranges {
			ranges[i] = plugin.UIHighlight{Line: 0, StartCol: i, EndCol: i + 1}
		}
		if err := runtime.SetHighlights(plugin.UIHighlightRequest{Namespace: namespace, Highlights: ranges}); err != nil {
			t.Fatalf("SetHighlights(total namespace %d) error = %v", namespace, err)
		}
	}
	if err := runtime.SetHighlights(plugin.UIHighlightRequest{
		Namespace:  9,
		Highlights: []plugin.UIHighlight{{Line: 0, StartCol: 0, EndCol: 1}},
	}); err == nil {
		t.Fatal("SetHighlights() accepted too many total ranges")
	}
}

func TestPluginRuntimeTracksRetokenizeEditorIdentity(t *testing.T) {
	model := newInputRoutingTestModel(t)
	buf := text.NewBufferFromBytes([]byte("package main\n"))
	buf.FilePath = "main.go"
	ed := editor.New(buf, model.theme, editor.DefaultConfig())
	model.editors[0] = ed

	runtime := newPluginRuntime(&model)
	if err := runtime.InsertText("// edited\n"); err != nil {
		t.Fatalf("InsertText() error = %v", err)
	}

	if got, want := runtime.retokenizeEditor, model.activeEditor().ID(); got != want {
		t.Fatalf("retokenize editor ID = %d, want %d", got, want)
	}
	if got, want := runtime.retokenizeVersion, model.activeEditor().Buffer.Version(); got != want {
		t.Fatalf("retokenize version = %d, want %d", got, want)
	}
	if cmd := runtime.command(); cmd == nil {
		t.Fatal("command() did not schedule retokenization")
	}
	if runtime.retokenizeEditor != 0 || runtime.retokenizeVersion != -1 {
		t.Fatalf("command() did not clear retokenize identity: id=%d version=%d", runtime.retokenizeEditor, runtime.retokenizeVersion)
	}
}

func TestPluginRuntimeCreatesUntitledBuffer(t *testing.T) {
	model := newInputRoutingTestModel(t)
	runtime := newPluginRuntime(&model)

	bufnr, err := runtime.NewBuffer()
	if err != nil {
		t.Fatalf("NewBuffer() error = %v", err)
	}
	if bufnr != 2 {
		t.Fatalf("NewBuffer() = %d, want 2 for the second tab", bufnr)
	}
	if len(model.editors) != 2 || model.activeTab != 1 {
		t.Fatalf("direct NewBuffer state = editors:%d active:%d, want 2/1", len(model.editors), model.activeTab)
	}
	if got := model.tabBar.Tabs[1].Label; got != "Untitled-1" {
		t.Fatalf("new tab label = %q, want Untitled-1", got)
	}
}

func TestPluginRuntimeOpenFileReportsRejectedOpen(t *testing.T) {
	model := newInputRoutingTestModel(t)
	path := filepath.Join(model.rootDir, "reserved.go")
	model.pendingSaves[1] = pendingSaveRequest{Path: path, SaveAs: true}
	runtime := newPluginRuntime(&model)

	if err := runtime.OpenFile(path); err == nil {
		t.Fatal("OpenFile() reported success for a destination reserved by Save As")
	}
	if got := len(model.editors); got != 1 {
		t.Fatalf("rejected open changed editor count to %d, want 1", got)
	}
}

func TestParseSyntheticKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKeys []string
		wantErr  bool
	}{
		{
			name:     "plain text expands to individual printable keys",
			input:    "ab",
			wantKeys: []string{"a", "b"},
		},
		{
			name:     "angle-bracket tokens mix with text",
			input:    "a<left>!",
			wantKeys: []string{"a", "left", "!"},
		},
		{
			name:     "modifier token parses without brackets",
			input:    "ctrl+s",
			wantKeys: []string{"ctrl+s"},
		},
		{
			name:     "named special token parses without brackets",
			input:    "enter",
			wantKeys: []string{"enter"},
		},
		{
			name:    "unterminated token errors",
			input:   "<ctrl+s",
			wantErr: true,
		},
		{
			name:    "unknown token errors",
			input:   "<madeup>",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, err := parseSyntheticKeys(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseSyntheticKeys() error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSyntheticKeys() error = %v", err)
			}
			if len(msgs) != len(tt.wantKeys) {
				t.Fatalf("parseSyntheticKeys() len = %d, want %d", len(msgs), len(tt.wantKeys))
			}
			for i, msg := range msgs {
				if got := tea.KeyPressMsg(msg).String(); got != tt.wantKeys[i] {
					t.Fatalf("parseSyntheticKeys()[%d] = %q, want %q", i, got, tt.wantKeys[i])
				}
			}
		})
	}
}
