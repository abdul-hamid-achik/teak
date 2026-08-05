package app

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
	"teak/internal/overlay"
	"teak/internal/ui"
)

func TestLSPPickerResultsPrepareItemsOutsideUpdate(t *testing.T) {
	locations := []lsp.Location{
		{URI: "file:///workspace/one.go", StartLine: 1},
		{URI: "file:///workspace/two.go", StartLine: 2},
	}
	tests := []struct {
		name string
		want int
		run  func(Model) (tea.Model, tea.Cmd)
	}{
		{
			name: "definitions",
			want: len(locations),
			run: func(model Model) (tea.Model, tea.Cmd) {
				return model.handleDefinitionResult(lsp.DefinitionResultMsg{Locations: locations})
			},
		},
		{
			name: "references",
			want: len(locations),
			run: func(model Model) (tea.Model, tea.Cmd) {
				return model.handleReferencesResult(lsp.ReferencesResultMsg{Locations: locations})
			},
		},
		{
			name: "document symbols",
			want: 3,
			run: func(model Model) (tea.Model, tea.Cmd) {
				return model.handleDocumentSymbolResult(lsp.DocumentSymbolResultMsg{Symbols: []lsp.DocumentSymbol{
					{Name: "Parent", Children: []lsp.DocumentSymbol{{Name: "Child"}, {Name: "Other"}}},
				}})
			},
		},
		{
			name: "code actions",
			want: 2,
			run: func(model Model) (tea.Model, tea.Cmd) {
				return model.handleCodeActionResult(lsp.CodeActionResultMsg{Actions: []lsp.CodeAction{
					{Title: "first"},
					{Title: "second"},
				}})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := Model{modelState: &modelState{rootDir: "/workspace", theme: ui.DefaultTheme()}}
			updatedAny, prepareCmd := tt.run(model)
			updated := updatedAny.(Model)
			picker, ok := updated.overlayStack.Top().(*overlay.Picker)
			if !ok {
				t.Fatalf("top overlay = %T, want picker", updated.overlayStack.Top())
			}
			if !picker.FilterPending() || picker.FilteredCount() != 0 {
				t.Fatalf("initial picker pending/count = %t/%d, want true/0", picker.FilterPending(), picker.FilteredCount())
			}
			ready := executePickerItemsPreparation(t, prepareCmd)
			if len(ready.Items) != tt.want {
				t.Fatalf("prepared items = %d, want %d", len(ready.Items), tt.want)
			}
			final := installPreparedPickerItems(t, updated, ready)
			finalPicker := final.overlayStack.Top().(*overlay.Picker)
			if finalPicker.FilterPending() || finalPicker.FilteredCount() != tt.want {
				t.Fatalf("final picker pending/count = %t/%d, want false/%d", finalPicker.FilterPending(), finalPicker.FilteredCount(), tt.want)
			}
		})
	}
}

func installPreparedPickerItems(t *testing.T, model Model, ready overlay.PickerItemsReadyMsg) Model {
	t.Helper()
	filteredAny, filterCmd := model.Update(ready)
	if filterCmd == nil {
		t.Fatal("installing prepared items returned no filter command")
	}
	filterMsg := filterCmd()
	filterReady, ok := filterMsg.(overlay.PickerFilterReadyMsg)
	if !ok {
		t.Fatalf("filter command returned %T, want PickerFilterReadyMsg", filterMsg)
	}
	finalAny, _ := filteredAny.(Model).Update(filterReady)
	return finalAny.(Model)
}

func executePickerItemsPreparation(t *testing.T, cmd tea.Cmd) overlay.PickerItemsReadyMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("picker result returned no preparation command")
	}
	msg := cmd()
	if ready, ok := msg.(overlay.PickerItemsReadyMsg); ok {
		return ready
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("preparation command returned %T, want batch", msg)
	}
	for _, child := range batch {
		if ready, ok := child().(overlay.PickerItemsReadyMsg); ok {
			return ready
		}
	}
	t.Fatal("preparation batch contained no PickerItemsReadyMsg")
	return overlay.PickerItemsReadyMsg{}
}

func BenchmarkLSPReferencesPickerDispatchTwentyThousand(b *testing.B) {
	locations := make([]lsp.Location, 20_000)
	for i := range locations {
		locations[i] = lsp.Location{URI: "file:///workspace/main.go", StartLine: i}
	}
	msg := lsp.ReferencesResultMsg{Locations: locations}
	state := &modelState{rootDir: "/workspace", theme: ui.DefaultTheme()}
	model := Model{modelState: state}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		state.overlayStack.Clear()
		updatedAny, cmd := model.handleReferencesResult(msg)
		updated := updatedAny.(Model)
		picker, ok := updated.overlayStack.Top().(*overlay.Picker)
		if !ok || !picker.FilterPending() || cmd == nil {
			b.Fatalf("picker = %T pending=%t cmd=nil=%t", updated.overlayStack.Top(), ok && picker.FilterPending(), cmd == nil)
		}
	}
	state.overlayStack.Clear()
}

func BenchmarkLSPReferencePickerPreparationTwentyThousand(b *testing.B) {
	locations := make([]lsp.Location, 20_000)
	for i := range locations {
		locations[i] = lsp.Location{URI: "file:///workspace/" + fmt.Sprintf("file-%05d.go", i), StartLine: i}
	}
	b.ReportAllocs()
	for b.Loop() {
		items := lspLocationsToPickerItems(locations, "/workspace")
		if len(items) != len(locations) {
			b.Fatalf("items = %d, want %d", len(items), len(locations))
		}
	}
}
