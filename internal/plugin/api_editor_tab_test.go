package plugin

import (
	"testing"
)

// editorTabCloseRecorder records the tab index each close_tab call forwards
// to the runtime, so tests can tell an explicit tab from the active-tab
// sentinel used internally by the runtime.
type editorTabCloseRecorder struct {
	apiRuntimeStub
	closedTabs []int
}

func (r *editorTabCloseRecorder) CloseTab(tab int) error {
	r.closedTabs = append(r.closedTabs, tab)
	return nil
}

func TestLuaEditorCloseTabRejectsInvalidTabNumbers(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"zero is not a 1-based tab", "editor.close_tab(0)"},
		{"negative tab", "editor.close_tab(-1)"},
		{"negative tab below sentinel", "editor.close_tab(-2)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := newLuaStateFactory().Get()
			defer L.Close()
			registerTestModule(L, "editor", registerEditorAPI)

			runtime := &editorTabCloseRecorder{apiRuntimeStub: apiRuntimeStub{tabCount: 2, activeTab: 1}}
			setRuntimeForState(L, runtime)
			defer clearRuntimeForState(L)

			if err := L.DoString(tt.src); err == nil {
				t.Fatalf("%s: expected an error, got none", tt.src)
			}
			if len(runtime.closedTabs) != 0 {
				t.Fatalf("%s: runtime closed tab index %v; invalid Lua tab number must not close any tab", tt.src, runtime.closedTabs)
			}
		})
	}
}

func TestLuaEditorCloseTabWithoutArgumentClosesActiveTab(t *testing.T) {
	L := newLuaStateFactory().Get()
	defer L.Close()
	registerTestModule(L, "editor", registerEditorAPI)

	runtime := &editorTabCloseRecorder{apiRuntimeStub: apiRuntimeStub{tabCount: 2, activeTab: 1}}
	setRuntimeForState(L, runtime)
	defer clearRuntimeForState(L)

	if err := L.DoString("editor.close_tab()"); err != nil {
		t.Fatalf("close_tab() failed: %v", err)
	}
	if len(runtime.closedTabs) != 1 || runtime.closedTabs[0] != -1 {
		t.Fatalf("close_tab() forwarded %v, want the active-tab sentinel [-1]", runtime.closedTabs)
	}
}

func TestLuaEditorCloseTabOneBasedTabIsZeroBasedAtBoundary(t *testing.T) {
	L := newLuaStateFactory().Get()
	defer L.Close()
	registerTestModule(L, "editor", registerEditorAPI)

	runtime := &editorTabCloseRecorder{apiRuntimeStub: apiRuntimeStub{tabCount: 2, activeTab: 1}}
	setRuntimeForState(L, runtime)
	defer clearRuntimeForState(L)

	if err := L.DoString("editor.close_tab(2)"); err != nil {
		t.Fatalf("close_tab(2) failed: %v", err)
	}
	if len(runtime.closedTabs) != 1 || runtime.closedTabs[0] != 1 {
		t.Fatalf("close_tab(2) forwarded %v, want zero-based [1]", runtime.closedTabs)
	}
}
