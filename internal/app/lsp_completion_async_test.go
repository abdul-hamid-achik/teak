package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/editor/overlays"
	"teak/internal/lsp"
	"teak/internal/text"
	"teak/internal/ui"
)

var completionBenchmarkCmd tea.Cmd

func TestLSPCompletionItemPreparationPreservesEditAndCancellation(t *testing.T) {
	source := []lsp.CompletionItem{{
		Label: "Println", Detail: "func", InsertText: "Println($0)", HasEdit: true,
		Edit: lsp.CompletionEdit{StartLine: 3, StartCol: 4, EndLine: 3, EndCol: 7},
	}}
	items, err := lspCompletionItemsContext(context.Background(), source)
	if err != nil || len(items) != 1 {
		t.Fatalf("prepared items = %d, %v", len(items), err)
	}
	item := items[0]
	if item.Label != source[0].Label || item.Detail != source[0].Detail || item.InsertText != source[0].InsertText || !item.HasEdit {
		t.Fatalf("prepared item lost completion fields: %+v", item)
	}
	if item.Edit.StartLine != 3 || item.Edit.StartCol != 4 || item.Edit.EndLine != 3 || item.Edit.EndCol != 7 {
		t.Fatalf("prepared edit = %+v, want source range", item.Edit)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items, err = lspCompletionItemsContext(ctx, make([]lsp.CompletionItem, 20_000))
	if !errors.Is(err, context.Canceled) || items != nil {
		t.Fatalf("cancelled preparation = %d items, %v", len(items), err)
	}
}

func TestCompletionResultPreparesItemsOutsideUpdate(t *testing.T) {
	buffer := text.NewBufferFromBytes([]byte("pri"))
	buffer.FilePath = "/workspace/main.go"
	ed := editor.New(buffer, ui.DefaultTheme(), editor.DefaultConfig())
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 3})
	model := testModel(modelState{
		theme:     ui.DefaultTheme(),
		editors:   []editor.Editor{ed},
		activeTab: 0,
	})

	updatedAny, cmd := model.handleCompletionResult(lsp.CompletionResultMsg{
		Items: []lsp.CompletionItem{
			{Label: "Println", InsertText: "Println"},
			{Label: "Printf", InsertText: "Printf"},
		},
	})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("completion result returned no preparation command")
	}
	view := updated.activeEditor().AutocompleteView()
	if !strings.Contains(view, "Loading") {
		t.Fatalf("initial autocomplete view = %q, want loading state", view)
	}
	if strings.Contains(view, "Println") || strings.Contains(view, "Printf") {
		t.Fatalf("completion items were installed synchronously: %q", view)
	}

	prepared := cmd()
	filteringAny, filterCmd := updated.Update(prepared)
	filtering := filteringAny.(Model)
	if filterCmd == nil {
		t.Fatal("prepared completion items returned no filtering command")
	}
	if view := filtering.activeEditor().AutocompleteView(); !strings.Contains(view, "Filtering") {
		t.Fatalf("view after item preparation = %q, want filtering state", view)
	}
	filteredAny, _ := filtering.Update(filterCmd())
	filtered := filteredAny.(Model)
	view = filtered.activeEditor().AutocompleteView()
	if !strings.Contains(view, "Println") || !strings.Contains(view, "Printf") {
		t.Fatalf("final autocomplete view = %q, want both prepared matches", view)
	}
}

func TestCompletionPreparationDoesNotReopenDismissedAutocomplete(t *testing.T) {
	buffer := text.NewBufferFromBytes([]byte("pri"))
	buffer.FilePath = "/workspace/main.go"
	ed := editor.New(buffer, ui.DefaultTheme(), editor.DefaultConfig())
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 3})
	model := testModel(modelState{theme: ui.DefaultTheme(), editors: []editor.Editor{ed}})

	updatedAny, cmd := model.handleCompletionResult(lsp.CompletionResultMsg{
		Items: []lsp.CompletionItem{{Label: "Println", InsertText: "Println"}},
	})
	updated := updatedAny.(Model)
	updated.activeEditor().HideAutocomplete()
	preparedAny, next := updated.Update(cmd())
	prepared := preparedAny.(Model)
	if next != nil {
		t.Fatal("dismissed autocomplete scheduled late filtering work")
	}
	if view := prepared.activeEditor().AutocompleteView(); view != "" {
		t.Fatalf("dismissed autocomplete reopened with view %q", view)
	}
}

func TestSwitchTabCancelsAutocompleteWorkOwnedByDepartingEditor(t *testing.T) {
	first := editor.New(text.NewBufferFromBytes([]byte("first")), ui.DefaultTheme(), editor.DefaultConfig())
	second := editor.New(text.NewBufferFromBytes([]byte("second")), ui.DefaultTheme(), editor.DefaultConfig())
	first.BeginAutocompleteLoading()
	model := testModel(modelState{editors: []editor.Editor{first, second}, activeTab: 0})

	if !model.activateTab(1) {
		t.Fatal("activateTab(1) rejected a valid editor")
	}
	if view := model.editors[0].AutocompleteView(); view != "" {
		t.Fatalf("departing editor retained autocomplete view %q", view)
	}
}

func TestPendingAutocompleteSelectionSynchronizesRootEditorState(t *testing.T) {
	buffer := text.NewBufferFromBytes([]byte("ap\n"))
	ed := editor.New(buffer, ui.DefaultTheme(), editor.DefaultConfig())
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 2})
	ed.ShowAutocomplete([]overlays.AutocompleteItem{
		{Label: "apple", InsertText: "apple"},
		{Label: "banana", InsertText: "banana"},
	})
	ed, filterCmd := ed.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	ed, _ = ed.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	tabBar := editor.NewTabBar(ui.DefaultTheme())
	tabBar.AddTab("main.go", "")
	tabBar.Tabs[0].Preview = true
	model := testModel(modelState{editors: []editor.Editor{ed}, tabBar: tabBar})
	updatedAny, _ := model.Update(autocompleteFilterMessage(t, filterCmd))
	updated := updatedAny.(Model)
	if got := string(updated.editors[0].Buffer.Bytes()); got != "appapple\n" {
		t.Fatalf("buffer = %q, want pending completion applied", got)
	}
	if !updated.tabBar.Tabs[0].Dirty || updated.tabBar.Tabs[0].Preview {
		t.Fatalf("tab dirty/preview = %t/%t, want true/false", updated.tabBar.Tabs[0].Dirty, updated.tabBar.Tabs[0].Preview)
	}
}

func autocompleteFilterMessage(t *testing.T, cmd tea.Cmd) overlays.AutocompleteFilterReadyMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("autocomplete filtering command is nil")
	}
	var find func(tea.Msg) (overlays.AutocompleteFilterReadyMsg, bool)
	find = func(msg tea.Msg) (overlays.AutocompleteFilterReadyMsg, bool) {
		if ready, ok := msg.(overlays.AutocompleteFilterReadyMsg); ok {
			return ready, true
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				if child == nil {
					continue
				}
				if ready, ok := find(child()); ok {
					return ready, true
				}
			}
		}
		return overlays.AutocompleteFilterReadyMsg{}, false
	}
	ready, ok := find(cmd())
	if !ok {
		t.Fatal("command tree contained no autocomplete filter result")
	}
	return ready
}

func BenchmarkLSPCompletionDispatchTwentyThousand(b *testing.B) {
	items := make([]lsp.CompletionItem, 20_000)
	for i := range items {
		label := fmt.Sprintf("completion-%05d", i)
		items[i] = lsp.CompletionItem{Label: label, Detail: "func", InsertText: label}
	}
	buffer := text.NewBufferFromBytes([]byte("comp"))
	buffer.FilePath = "/workspace/main.go"
	ed := editor.New(buffer, ui.DefaultTheme(), editor.DefaultConfig())
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: 4})
	state := &modelState{
		theme:     ui.DefaultTheme(),
		editors:   []editor.Editor{ed},
		activeTab: 0,
	}
	model := Model{modelState: state}
	msg := lsp.CompletionResultMsg{Items: items}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		updatedAny, cmd := model.handleCompletionResult(msg)
		updated := updatedAny.(Model)
		if updated.activeEditor() == nil {
			b.Fatal("completion dispatch lost the active editor")
		}
		completionBenchmarkCmd = cmd
	}
	completionBenchmarkCmd = nil
	state.overlayRequests.cancelAll()
}

func BenchmarkLSPCompletionPreparationTwentyThousand(b *testing.B) {
	items := make([]lsp.CompletionItem, 20_000)
	for i := range items {
		label := fmt.Sprintf("completion-%05d", i)
		items[i] = lsp.CompletionItem{Label: label, Detail: "func", InsertText: label}
	}
	b.ReportAllocs()
	for b.Loop() {
		prepared, err := lspCompletionItemsContext(b.Context(), items)
		if err != nil || len(prepared) != len(items) {
			b.Fatalf("prepared completion items = %d, %v", len(prepared), err)
		}
	}
}
