package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
