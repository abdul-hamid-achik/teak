package overlays

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

var autocompleteBenchmarkCmd tea.Cmd

func TestNewAutocomplete(t *testing.T) {
	theme := ui.DefaultTheme()
	ac := NewAutocomplete(theme)

	if ac.Visible {
		t.Error("expected Visible to be false")
	}
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
	if ac.Items != nil {
		t.Error("expected Items to be nil")
	}
}

func TestAutocompleteShow(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	items := []AutocompleteItem{
		{Label: "foo", Detail: "func", InsertText: "foo()"},
		{Label: "bar", Detail: "var", InsertText: "bar"},
	}

	ac.Show(items)

	if !ac.Visible {
		t.Error("expected Visible to be true")
	}
	if len(ac.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(ac.Items))
	}
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteShowEmpty(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())

	ac.Show([]AutocompleteItem{})

	if ac.Visible {
		t.Error("expected Visible to be false for empty items")
	}
}

func TestAutocompleteHide(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})

	ac.Hide()

	if ac.Visible {
		t.Error("expected Visible to be false")
	}
	if ac.Items != nil {
		t.Error("expected Items to be nil")
	}
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteDismissalRejectsPreparedItems(t *testing.T) {
	autocomplete := NewAutocomplete(ui.DefaultTheme())
	generation := autocomplete.BeginLoading()
	if view := ansi.Strip(autocomplete.View()); !strings.Contains(view, "Loading") {
		t.Fatalf("loading view = %q", view)
	}
	autocomplete.Hide()
	if cmd := autocomplete.InstallItems(7, generation, []AutocompleteItem{{Label: "late"}}, ""); cmd != nil {
		t.Fatal("dismissed loading generation accepted late items")
	}
	if autocomplete.Visible {
		t.Fatal("dismissed autocomplete became visible again")
	}
}

func TestAutocompleteAsyncFilterRejectsSupersededResult(t *testing.T) {
	autocomplete := NewAutocomplete(ui.DefaultTheme())
	autocomplete.Show([]AutocompleteItem{{Label: "apple"}, {Label: "banana"}})
	staleCmd := autocomplete.ScheduleFilter(9, "app")
	currentCmd := autocomplete.ScheduleFilter(9, "ban")

	stale := staleCmd().(AutocompleteFilterReadyMsg)
	if !errors.Is(stale.Err, context.Canceled) {
		t.Fatalf("superseded filter error = %v, want context cancellation", stale.Err)
	}
	if item := autocomplete.ApplyFilter(stale); item != nil {
		t.Fatalf("superseded filter selected %#v", item)
	}
	current := currentCmd().(AutocompleteFilterReadyMsg)
	autocomplete.ApplyFilter(current)
	if len(autocomplete.Items) != 1 || autocomplete.Items[0].Label != "banana" {
		t.Fatalf("current items = %+v, want banana", autocomplete.Items)
	}
}

func TestAutocompleteInstallItemsFiltersCurrentPrefix(t *testing.T) {
	autocomplete := NewAutocomplete(ui.DefaultTheme())
	if cmd := autocomplete.ScheduleFilter(1, "app"); cmd != nil {
		t.Fatal("hidden autocomplete scheduled a filter")
	}
	generation := autocomplete.BeginLoading()
	if !autocomplete.ItemsLoading() {
		t.Fatal("BeginLoading did not report item preparation")
	}
	if cmd := autocomplete.ScheduleFilter(1, "app"); cmd != nil {
		t.Fatal("item-loading autocomplete scheduled a premature filter")
	}
	if cmd := autocomplete.InstallItems(1, generation+1, []AutocompleteItem{{Label: "apple"}}, "app"); cmd != nil {
		t.Fatal("wrong item generation scheduled a filter")
	}
	cmd := autocomplete.InstallItems(1, generation, []AutocompleteItem{{Label: "apple"}, {Label: "banana"}}, "app")
	if cmd == nil || !autocomplete.Pending() || autocomplete.ItemsLoading() {
		t.Fatalf("installed items pending/loading = %t/%t, want true/false", autocomplete.Pending(), autocomplete.ItemsLoading())
	}
	autocomplete.ApplyFilter(cmd().(AutocompleteFilterReadyMsg))
	if len(autocomplete.Items) != 1 || autocomplete.Items[0].Label != "apple" {
		t.Fatalf("installed items = %+v, want apple", autocomplete.Items)
	}

	emptyGeneration := autocomplete.BeginLoading()
	if cmd := autocomplete.InstallItems(1, emptyGeneration, nil, ""); cmd != nil {
		t.Fatal("empty item preparation scheduled a filter")
	}
	if autocomplete.Visible {
		t.Fatal("empty prepared collection left autocomplete visible")
	}
}

func TestAutocompleteSynchronousFilterCompatibility(t *testing.T) {
	autocomplete := NewAutocomplete(ui.DefaultTheme())
	autocomplete.Filter("ignored while hidden")
	autocomplete.Show([]AutocompleteItem{{Label: "apple"}, {Label: "Apply"}, {Label: "banana"}})
	autocomplete.Filter("app")
	if len(autocomplete.Items) != 2 {
		t.Fatalf("prefix filter items = %d, want 2", len(autocomplete.Items))
	}
	autocomplete.Filter("")
	if len(autocomplete.Items) != 3 {
		t.Fatalf("empty-prefix items = %d, want 3", len(autocomplete.Items))
	}
	autocomplete.allItems = nil
	autocomplete.Filter("ban")
	if len(autocomplete.Items) != 1 || autocomplete.Items[0].Label != "banana" {
		t.Fatalf("fallback source items = %+v, want banana", autocomplete.Items)
	}
	autocomplete.Show([]AutocompleteItem{{Label: "apple"}})
	autocomplete.Filter("zzz")
	if autocomplete.Visible {
		t.Fatal("no-match synchronous filter remained visible")
	}
}

func TestAutocompletePendingSelectionUsesCurrentProjection(t *testing.T) {
	autocomplete := NewAutocomplete(ui.DefaultTheme())
	autocomplete.RequestSelection() // no pending work: deliberately inert
	autocomplete.Show([]AutocompleteItem{{Label: "apple"}, {Label: "banana"}})
	cmd := autocomplete.ScheduleFilter(4, "ban")
	autocomplete.RequestSelection()
	autocomplete.MoveUp()
	autocomplete.MoveDown()
	if selected := autocomplete.Selected(); selected != nil {
		t.Fatalf("pending Selected() = %#v, want nil", selected)
	}
	if selected := autocomplete.SelectAt(0); selected != nil {
		t.Fatalf("pending SelectAt() = %#v, want nil", selected)
	}
	selected := autocomplete.ApplyFilter(cmd().(AutocompleteFilterReadyMsg))
	if selected == nil || selected.Label != "banana" {
		t.Fatalf("pending selection = %#v, want banana", selected)
	}
	if autocomplete.Visible {
		t.Fatal("autocomplete remained visible after pending selection")
	}
}

func TestAutocompleteCurrentFilterErrorClosesPopup(t *testing.T) {
	autocomplete := NewAutocomplete(ui.DefaultTheme())
	autocomplete.Show([]AutocompleteItem{{Label: "apple"}})
	cmd := autocomplete.ScheduleFilter(5, "app")
	ready := cmd().(AutocompleteFilterReadyMsg)
	ready.Err = errors.New("projection failed")
	autocomplete.ApplyFilter(ready)
	if autocomplete.Visible {
		t.Fatal("failed current filter left popup visible")
	}
}

func TestFilterAutocompleteItemsContextEdges(t *testing.T) {
	items := []AutocompleteItem{{Label: "Alpha"}, {Label: "beta"}}
	all, err := filterAutocompleteItemsContext(nil, items, "")
	if err != nil || len(all) != len(items) {
		t.Fatalf("empty-prefix projection = %d, %v", len(all), err)
	}
	filtered, err := filterAutocompleteItemsContext(context.Background(), items, "AL")
	if err != nil || len(filtered) != 1 || filtered[0].Label != "Alpha" {
		t.Fatalf("case-insensitive projection = %+v, %v", filtered, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	filtered, err = filterAutocompleteItemsContext(ctx, items, "a")
	if !errors.Is(err, context.Canceled) || filtered != nil {
		t.Fatalf("cancelled projection = %+v, %v", filtered, err)
	}
}

func BenchmarkAutocompleteFilterDispatchTwentyThousand(b *testing.B) {
	items := make([]AutocompleteItem, 20_000)
	for i := range items {
		items[i] = AutocompleteItem{Label: fmt.Sprintf("completion-%05d", i)}
	}
	autocomplete := NewAutocomplete(ui.DefaultTheme())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		autocomplete.Show(items)
		cmd := autocomplete.ScheduleFilter(17, "completion-19999")
		if cmd == nil {
			b.Fatal("filter dispatch returned no command")
		}
		autocompleteBenchmarkCmd = cmd
	}
	autocomplete.Hide()
	autocompleteBenchmarkCmd = nil
}

func BenchmarkAutocompleteFilterProjectionTwentyThousand(b *testing.B) {
	items := make([]AutocompleteItem, 20_000)
	for i := range items {
		items[i] = AutocompleteItem{Label: fmt.Sprintf("completion-%05d", i)}
	}
	b.ReportAllocs()
	for b.Loop() {
		filtered, err := filterAutocompleteItemsContext(b.Context(), items, "completion-19999")
		if err != nil || len(filtered) != 1 {
			b.Fatalf("filtered items = %d, %v", len(filtered), err)
		}
	}
}

func TestAutocompleteMoveUp(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
		{Label: "baz", InsertText: "baz"},
	})

	ac.MoveUp()
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}

	ac.Cursor = 2
	ac.MoveUp()
	if ac.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", ac.Cursor)
	}
}

func TestAutocompleteMoveDown(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
		{Label: "baz", InsertText: "baz"},
	})

	ac.MoveDown()
	if ac.Cursor != 1 {
		t.Errorf("expected Cursor 1, got %d", ac.Cursor)
	}

	ac.Cursor = 1
	ac.MoveDown()
	if ac.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", ac.Cursor)
	}

	ac.MoveDown()
	if ac.Cursor != 2 {
		t.Errorf("expected Cursor 2, got %d", ac.Cursor)
	}
}

func TestAutocompleteScrollsToKeepSelectedItemVisible(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	items := make([]AutocompleteItem, 20)
	for i := range items {
		items[i] = AutocompleteItem{Label: fmt.Sprintf("item-%02d", i), InsertText: "x"}
	}
	ac.Show(items)
	for i := 0; i < 15; i++ {
		ac.MoveDown()
	}

	if got, want := ac.visibleStart(), 6; got != want {
		t.Fatalf("visibleStart() = %d, want %d", got, want)
	}
	view := ansi.Strip(ac.View())
	if !strings.Contains(view, "item-15") {
		t.Fatalf("selected item is missing from popup: %q", view)
	}
	if strings.Contains(view, "item-00") {
		t.Fatalf("popup still renders items before visible window: %q", view)
	}

	selected := ac.SelectAt(0)
	if selected == nil || selected.Label != "item-06" {
		t.Fatalf("SelectAt(0) = %#v, want item-06 at visible window start", selected)
	}
}

func TestAutocompleteSelected(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	items := []AutocompleteItem{
		{Label: "foo", InsertText: "foo"},
		{Label: "bar", InsertText: "bar"},
	}
	ac.Show(items)

	item := ac.Selected()
	if item == nil {
		t.Fatal("expected item to be selected")
	}
	if item.Label != "foo" {
		t.Errorf("expected label 'foo', got %q", item.Label)
	}

	ac.Cursor = 1
	item = ac.Selected()
	if item.Label != "bar" {
		t.Errorf("expected label 'bar', got %q", item.Label)
	}
}

func TestAutocompleteSelectedNotVisible(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})
	ac.Hide()

	item := ac.Selected()
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}
}

func TestAutocompleteSelectedOutOfBounds(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})
	ac.Cursor = 10

	item := ac.Selected()
	if item != nil {
		t.Errorf("expected nil item, got %v", item)
	}
}

func TestAutocompleteView(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", Detail: "func", InsertText: "foo()"},
		{Label: "bar", Detail: "var", InsertText: "bar"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "foo") {
		t.Errorf("expected 'foo' in view, got %q", view)
	}
	if !strings.Contains(view, "bar") {
		t.Errorf("expected 'bar' in view, got %q", view)
	}
}

func TestAutocompleteViewNotVisible(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())

	view := ac.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestAutocompleteViewEmpty(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{})

	view := ac.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestAutocompleteItemStructure(t *testing.T) {
	item := AutocompleteItem{
		Label:      "test",
		Detail:     "detail",
		InsertText: "insert",
	}

	if item.Label != "test" {
		t.Errorf("expected Label 'test', got %q", item.Label)
	}
	if item.Detail != "detail" {
		t.Errorf("expected Detail 'detail', got %q", item.Detail)
	}
	if item.InsertText != "insert" {
		t.Errorf("expected InsertText 'insert', got %q", item.InsertText)
	}
}

func TestAutocompleteMoveUpFromZero(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})

	ac.MoveUp()
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteMoveDownSingleItem(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "foo", InsertText: "foo"}})

	ac.MoveDown()
	if ac.Cursor != 0 {
		t.Errorf("expected Cursor 0, got %d", ac.Cursor)
	}
}

func TestAutocompleteViewWidthConstraints(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{{Label: "a", InsertText: "a"}})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestAutocompleteViewMaxWidth(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "this_is_a_very_long_label_that_exceeds_sixty_characters", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestAutocompleteViewManyItems(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	items := make([]AutocompleteItem, 20)
	for i := range items {
		items[i] = AutocompleteItem{
			Label:      string(rune('a' + i)),
			InsertText: string(rune('a' + i)),
		}
	}
	ac.Show(items)

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestAutocompleteViewLongLabels(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "very_long_label_that_exceeds_max_width", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestAutocompleteViewWithDetail(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", Detail: "func foo() string", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "foo") {
		t.Errorf("expected 'foo' in view")
	}
}

func TestAutocompleteViewWithLongDetail(t *testing.T) {
	ac := NewAutocomplete(ui.DefaultTheme())
	ac.Show([]AutocompleteItem{
		{Label: "foo", Detail: "very long detail text that should be truncated", InsertText: "foo"},
	})

	view := ac.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestHoverNew(t *testing.T) {
	theme := ui.DefaultTheme()
	h := NewHover(theme)

	if h.Visible {
		t.Error("expected Visible to be false")
	}
	if h.Content != "" {
		t.Errorf("expected empty Content, got %q", h.Content)
	}
}

func TestHoverShow(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	h.Show("test content")

	if !h.Visible {
		t.Error("expected Visible to be true")
	}
	if h.Content != "test content" {
		t.Errorf("expected Content 'test content', got %q", h.Content)
	}
}

func TestHoverShowEmpty(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	h.Show("")

	if h.Visible {
		t.Error("expected Visible to be false for empty content")
	}
}

func TestHoverHide(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	h.Show("test content")

	h.Hide()

	if h.Visible {
		t.Error("expected Visible to be false")
	}
	if h.Content != "" {
		t.Errorf("expected empty Content, got %q", h.Content)
	}
}

func TestHoverView(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	h.Show("test content")

	view := h.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "test content") {
		t.Errorf("expected 'test content' in view")
	}
}

func TestHoverViewNotVisible(t *testing.T) {
	h := NewHover(ui.DefaultTheme())

	view := h.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestHoverViewEmpty(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	h.Show("")

	view := h.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestHoverViewLongContent(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	longContent := strings.Repeat("x", 100)
	h.Show(longContent)

	view := h.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestHoverViewManyLines(t *testing.T) {
	h := NewHover(ui.DefaultTheme())
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "line content"
	}
	h.Show(strings.Join(lines, "\n"))

	view := h.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestSignatureHelpNew(t *testing.T) {
	theme := ui.DefaultTheme()
	s := NewSignatureHelp(theme)

	if s.Visible {
		t.Error("expected Visible to be false")
	}
	if s.Help != nil {
		t.Error("expected Help to be nil")
	}
	if s.maxWidth != 70 {
		t.Errorf("expected maxWidth 70, got %d", s.maxWidth)
	}
	if s.maxHeight != 8 {
		t.Errorf("expected maxHeight 8, got %d", s.maxHeight)
	}
}

func TestSignatureHelpShow(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	data := &SignatureData{
		Signatures: []SignatureInfo{
			{Label: "foo(a int, b string)", Documentation: "foo function"},
		},
		ActiveSignature: 0,
		ActiveParameter: 0,
	}

	s.Show(data)

	if !s.Visible {
		t.Error("expected Visible to be true")
	}
	if s.Help == nil {
		t.Fatal("expected Help to be set")
	}
	if len(s.Help.Signatures) != 1 {
		t.Errorf("expected 1 signature, got %d", len(s.Help.Signatures))
	}
}

func TestSignatureHelpShowNil(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(nil)

	if s.Visible {
		t.Error("expected Visible to be false for nil data")
	}
}

func TestSignatureHelpShowEmpty(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{})

	if s.Visible {
		t.Error("expected Visible to be false for empty signatures")
	}
}

func TestSignatureHelpHide(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{{Label: "foo()"}},
	})

	s.Hide()

	if s.Visible {
		t.Error("expected Visible to be false")
	}
	if s.Help != nil {
		t.Error("expected Help to be nil")
	}
}

func TestSignatureHelpView(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{
			{Label: "foo(a int, b string)", Documentation: "foo function"},
		},
		ActiveSignature: 0,
		ActiveParameter: 0,
	})

	view := s.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
	if !strings.Contains(view, "foo") {
		t.Errorf("expected 'foo' in view")
	}
}

func TestSignatureHelpViewNotVisible(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())

	view := s.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestSignatureHelpViewNilHelp(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Help = nil
	s.Visible = true

	view := s.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestSignatureHelpViewEmptySignatures(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Help = &SignatureData{}
	s.Visible = true

	view := s.View()
	if view != "" {
		t.Errorf("expected empty view, got %q", view)
	}
}

func TestSignatureHelpViewActiveParameter(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{
			{
				Label:      "foo(a int, b string)",
				Parameters: []ParameterInfo{{Label: "a"}, {Label: "b"}},
			},
		},
		ActiveSignature: 0,
		ActiveParameter: 1,
	})

	view := s.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestSignatureHelpViewNegativeActiveParameterDoesNotPanic(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{{
			Label:      "f(a)",
			Parameters: []ParameterInfo{{Label: "a"}},
		}},
		ActiveParameter: -1,
	})

	if got := s.View(); got == "" {
		t.Fatal("expected visible signature help")
	}
}

func TestSignatureHelpUpdateActiveParameterClampsToActiveSignature(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{{
			Label:      "f(a)",
			Parameters: []ParameterInfo{{Label: "a"}},
		}},
	})

	s.UpdateActiveParameter(-1)
	if got := s.Help.ActiveParameter; got != 0 {
		t.Fatalf("negative parameter index was not clamped: got %d", got)
	}
}

func TestSignatureHelpUpdateActiveParameter(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{
			{
				Label:      "foo(a int, b string)",
				Parameters: []ParameterInfo{{Label: "a"}, {Label: "b"}},
			},
		},
		ActiveSignature: 0,
		ActiveParameter: 0,
	})

	s.UpdateActiveParameter(1)
	if s.Help.ActiveParameter != 1 {
		t.Errorf("expected ActiveParameter 1, got %d", s.Help.ActiveParameter)
	}
}

func TestSignatureHelpUpdateActiveParameterNilHelp(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Help = nil

	s.UpdateActiveParameter(1)
}

func TestSignatureHelpViewWithDocumentation(t *testing.T) {
	s := NewSignatureHelp(ui.DefaultTheme())
	s.Show(&SignatureData{
		Signatures: []SignatureInfo{
			{Label: "foo()", Documentation: "This is a long documentation string that should be truncated when displayed"},
		},
		ActiveSignature: 0,
		ActiveParameter: 0,
	})

	view := s.View()
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestSignatureDataStructure(t *testing.T) {
	data := SignatureData{
		Signatures: []SignatureInfo{
			{
				Label:         "test(a int, b string)",
				Documentation: "test function",
				Parameters: []ParameterInfo{
					{Label: "a", Documentation: "first param"},
					{Label: "b", Documentation: "second param"},
				},
			},
		},
		ActiveSignature: 0,
		ActiveParameter: 1,
	}

	if len(data.Signatures) != 1 {
		t.Errorf("expected 1 signature, got %d", len(data.Signatures))
	}
	if data.ActiveSignature != 0 {
		t.Errorf("expected ActiveSignature 0, got %d", data.ActiveSignature)
	}
	if data.ActiveParameter != 1 {
		t.Errorf("expected ActiveParameter 1, got %d", data.ActiveParameter)
	}
	if len(data.Signatures[0].Parameters) != 2 {
		t.Errorf("expected 2 parameters, got %d", len(data.Signatures[0].Parameters))
	}
}

func TestSignatureInfoStructure(t *testing.T) {
	info := SignatureInfo{
		Label:         "foo(x int)",
		Documentation: "foo function",
		Parameters: []ParameterInfo{
			{Label: "x", Documentation: "an integer"},
		},
	}

	if info.Label != "foo(x int)" {
		t.Errorf("expected Label 'foo(x int)', got %q", info.Label)
	}
	if info.Documentation != "foo function" {
		t.Errorf("expected Documentation 'foo function', got %q", info.Documentation)
	}
	if len(info.Parameters) != 1 {
		t.Errorf("expected 1 parameter, got %d", len(info.Parameters))
	}
}

func TestParameterInfoStructure(t *testing.T) {
	param := ParameterInfo{
		Label:         "x",
		Documentation: "an integer",
	}

	if param.Label != "x" {
		t.Errorf("expected Label 'x', got %q", param.Label)
	}
	if param.Documentation != "an integer" {
		t.Errorf("expected Documentation 'an integer', got %q", param.Documentation)
	}
}
