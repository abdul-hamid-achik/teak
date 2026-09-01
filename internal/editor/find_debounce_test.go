package editor

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/search"
	"teak/internal/text"
	"teak/internal/ui"
)

func findTestEditor(t *testing.T, lines int) Editor {
	t.Helper()
	var sb strings.Builder
	for range lines {
		sb.WriteString("needle in a haystack line\n")
	}
	buf := text.NewBufferFromBytes([]byte(sb.String()))
	buf.FilePath = "main.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(100, 30)
	ed.ShowFind()
	return ed
}

func TestFindTypingDoesNotScanSynchronously(t *testing.T) {
	ed := findTestEditor(t, 200)

	cmd := ed.UpdateFind(tea.KeyPressMsg{Code: 'n', Text: "n"})

	// The scan used to run inside Update, blocking the event loop for as long as
	// it took to walk the whole document on every keystroke.
	if ed.find.MatchCount() != 0 {
		t.Errorf("matches computed during Update: got %d, want 0 until the scan runs", ed.find.MatchCount())
	}
	if cmd == nil {
		t.Fatal("no command returned; the debounced scan would never be scheduled")
	}
}

func TestFindDebouncedScanProducesMatches(t *testing.T) {
	ed := findTestEditor(t, 50)
	ed.UpdateFind(tea.KeyPressMsg{Code: 'n', Text: "n"})

	// Drive the two async hops the real loop would: debounce tick, then scan.
	tick := FindDebounceMsg{EditorID: ed.id, Generation: ed.find.Generation()}
	updated, scanCmd := ed.Update(tick)
	if scanCmd == nil {
		t.Fatal("debounce tick did not produce a scan command")
	}
	results, ok := scanCmd().(FindResultsMsg)
	if !ok {
		t.Fatalf("scan returned %T, want FindResultsMsg", scanCmd())
	}
	updated, _ = updated.Update(results)

	if updated.find.MatchCount() == 0 {
		t.Error("no matches after the debounced scan completed")
	}
}

func TestFindDiscardsSupersededResults(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.UpdateFind(tea.KeyPressMsg{Code: 'n', Text: "n"})
	stale := ed.find.Generation()

	// The user keeps typing, which supersedes the in-flight scan.
	ed.UpdateFind(tea.KeyPressMsg{Code: 'e', Text: "e"})

	updated, _ := ed.Update(FindResultsMsg{
		EditorID:   ed.id,
		Generation: stale,
		Matches:    []FindMatch{{Start: text.Position{Line: 1}, End: text.Position{Line: 1, Col: 1}}},
	})

	if updated.find.MatchCount() != 0 {
		t.Error("results from a superseded query overwrote the newer state")
	}
}

func TestFindHideInvalidatesInFlightScan(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.UpdateFind(tea.KeyPressMsg{Code: 'n', Text: "n"})
	generation := ed.find.Generation()
	ed.Buffer.SetCursor(text.Position{Line: 10, Col: 3})
	ed.HideFind()

	updated, _ := ed.Update(FindResultsMsg{
		EditorID:   ed.id,
		Generation: generation,
		Matches:    []FindMatch{{Start: text.Position{Line: 1}, End: text.Position{Line: 1, Col: 1}}},
	})

	if got, want := updated.Buffer.Cursor, (text.Position{Line: 10, Col: 3}); got != want {
		t.Fatalf("late hidden-find result moved cursor to %+v, want %+v", got, want)
	}
	if updated.find.MatchCount() != 0 {
		t.Fatalf("late hidden-find result installed %d matches, want none", updated.find.MatchCount())
	}
}

func TestFindHideCancelsScanContext(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.UpdateFind(tea.KeyPressMsg{Code: 'n', Text: "n"})
	ctx := ed.find.scanContext
	if ctx == nil {
		t.Fatal("query change did not create a cancellable scan context")
	}

	ed.HideFind()

	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("scan context error = %v, want context.Canceled", ctx.Err())
		}
	default:
		t.Fatal("HideFind did not cancel scan context")
	}
}

func TestFindMatchesContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := findMatchesContext(ctx, text.NewFromString(strings.Repeat("needle\n", 10_000)), "needle", search.SearchOpts{}, text.Position{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("findMatchesContext() error = %v, want context.Canceled", err)
	}
}

func TestFindQueryChangeClearsStaleMatchesWithoutMovingCursor(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.find.input.SetValue("n")
	ed.find.query = "n"
	ed.find.matches = []FindMatch{{Start: text.Position{Line: 1}, End: text.Position{Line: 1, Col: 1}}}
	ed.Buffer.SetCursor(text.Position{Line: 10, Col: 3})

	cmd := ed.UpdateFind(tea.KeyPressMsg{Code: 'e', Text: "e"})

	if cmd == nil {
		t.Fatal("query change did not schedule a replacement scan")
	}
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 10, Col: 3}); got != want {
		t.Fatalf("query change moved cursor to stale match %+v, want %+v", got, want)
	}
	if ed.find.MatchCount() != 0 {
		t.Fatalf("query change retained %d stale matches", ed.find.MatchCount())
	}
	view := ed.FindView()
	if !strings.Contains(view, "Searching") || strings.Contains(view, "No matches") {
		t.Fatalf("query-change view = %q, want a pending-search state without a false no-match result", view)
	}
}

func TestFindInvalidRegexSurfacesScanError(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.find.regex = true
	ed.UpdateFind(tea.KeyPressMsg{Code: '[', Text: "["})

	tick := FindDebounceMsg{EditorID: ed.id, Generation: ed.find.Generation()}
	updated, scanCmd := ed.Update(tick)
	if scanCmd == nil {
		t.Fatal("invalid regex did not start the asynchronous validation scan")
	}
	result, ok := scanCmd().(FindResultsMsg)
	if !ok {
		t.Fatalf("scan returned %T, want FindResultsMsg", scanCmd())
	}
	if result.Err == nil {
		t.Fatal("invalid regex scan error = nil")
	}
	updated, _ = updated.Update(result)
	if view := updated.FindView(); !strings.Contains(view, "invalid pattern") {
		t.Fatalf("find view = %q, want invalid-regex feedback", view)
	}
}

func TestFindReopenRescansPreservedQuery(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.find.input.SetValue("needle")
	ed.find.query = "needle"
	ed.HideFind()

	cmd := ed.ShowFind()

	if cmd == nil {
		t.Fatal("reopening a preserved query did not schedule a fresh scan")
	}
	if !ed.IsFindVisible() {
		t.Fatal("ShowFind did not reopen the widget")
	}
}

func TestFindIgnoresResultsForAnotherEditor(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.UpdateFind(tea.KeyPressMsg{Code: 'n', Text: "n"})

	updated, _ := ed.Update(FindResultsMsg{
		EditorID:   ed.id + 1,
		Generation: ed.find.Generation(),
		Matches:    []FindMatch{{Start: text.Position{Line: 1}, End: text.Position{Line: 1, Col: 1}}},
	})

	if updated.find.MatchCount() != 0 {
		t.Error("applied a scan result addressed to a different editor")
	}
}

func TestFindClearingQueryClearsMatchesImmediately(t *testing.T) {
	ed := findTestEditor(t, 20)
	ed.find.matches = []FindMatch{{Start: text.Position{Line: 1}, End: text.Position{Line: 1, Col: 1}}}
	ed.find.input.SetValue("")

	ed.find.scheduleScan()

	// An empty query has no matches; waiting for a scan would leave stale
	// highlights on screen for the debounce interval.
	if ed.find.MatchCount() != 0 {
		t.Errorf("matches = %d after clearing the query, want 0", ed.find.MatchCount())
	}
}

func BenchmarkFindKeystroke50kLines(b *testing.B) {
	var sb strings.Builder
	for range 50_000 {
		sb.WriteString("needle in a haystack line\n")
	}
	buf := text.NewBufferFromBytes([]byte(sb.String()))
	buf.FilePath = "main.go"
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(100, 30)
	ed.ShowFind()

	key := tea.KeyPressMsg{Code: 'n', Text: "n"}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ed.UpdateFind(key)
	}
}
