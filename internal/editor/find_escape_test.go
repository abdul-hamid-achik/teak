package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"teak/internal/text"
	"teak/internal/ui"
)

// findUXEditor returns a sized editor over content with the cursor at cursor.
func findUXEditor(content string, cursor text.Position) Editor {
	buf := text.NewBufferFromBytes([]byte(content))
	ed := New(buf, ui.DefaultTheme(), DefaultConfig())
	ed.SetSize(80, 24)
	ed.Buffer.SetCursor(cursor)
	return ed
}

// drainFindScan drives the debounce tick and scan command to completion so the
// editor holds the matches a real event loop would have installed.
func drainFindScan(t *testing.T, ed Editor) Editor {
	t.Helper()
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
	return updated
}

// --- F5a: ShowFind seeds the query from the primary selection ---

func TestShowFindSeedsQueryFromSelection(t *testing.T) {
	ed := findUXEditor("foo bar foo\nfoo baz", text.Position{Line: 0, Col: 0})
	// Select the first "foo"; the head lands at 0:3.
	ed.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 0, Col: 3})

	ed.ShowFind()

	if got := ed.find.input.Value(); got != "foo" {
		t.Fatalf("find input after ShowFind = %q, want seeded %q", got, "foo")
	}
	if got := ed.find.MatchCount(); got != 3 {
		t.Fatalf("matches after seeding = %d, want 3 computed by the initial scan", got)
	}
	// The cursor jumps to the first match at or after the selection head, as it
	// does when results arrive after typing.
	if got, want := ed.Buffer.Cursor, (text.Position{Line: 0, Col: 8}); got != want {
		t.Fatalf("cursor after seeding = %+v, want first match at %+v", got, want)
	}
}

func TestShowFindWithoutSelectionKeepsEmptyQuery(t *testing.T) {
	ed := findUXEditor("foo bar", text.Position{Line: 0, Col: 4})

	ed.ShowFind()

	if got := ed.find.input.Value(); got != "" {
		t.Fatalf("find input without selection = %q, want empty", got)
	}
	if got := ed.find.MatchCount(); got != 0 {
		t.Fatalf("matches without a seeded query = %d, want 0", got)
	}
}

func TestShowFindDoesNotSeedMultilineSelection(t *testing.T) {
	ed := findUXEditor("alpha beta\ngamma delta", text.Position{Line: 0, Col: 0})
	ed.Buffer.SetSelection(text.Position{Line: 0, Col: 0}, text.Position{Line: 1, Col: 5})

	ed.ShowFind()

	if got := ed.find.input.Value(); got != "" {
		t.Fatalf("find input after multiline selection = %q, want empty (per-line scan cannot match it)", got)
	}
}

// --- F5b: Esc restores the pre-find position only without navigation ---

func TestEscapeFromFindRestoresPositionWithoutNavigation(t *testing.T) {
	origin := text.Position{Line: 1, Col: 2}
	ed := findUXEditor("one two\nthree four\nthree five", origin)

	ed.ShowFind()
	for _, ch := range "three" {
		ed.UpdateFind(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	ed = drainFindScan(t, ed)
	// The cursor trailed the current match while typing; that is not navigation.
	if ed.Buffer.Cursor == origin {
		t.Fatal("typing the query did not move the cursor to a match; test setup is broken")
	}

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})

	if ed.IsFindVisible() {
		t.Fatal("escape left the find widget visible")
	}
	if got := ed.Buffer.Cursor; got != origin {
		t.Fatalf("cursor after escape = %+v, want restored to pre-find %+v", got, origin)
	}
	if got := ed.Buffer.Selections.Count(); got != 1 {
		t.Fatalf("selection count after escape = %d, want 1", got)
	}
	if sel := ed.Buffer.Selections.Primary(); sel.Head != origin || sel.Anchor != origin {
		t.Fatalf("primary selection after escape = %#v, want collapsed caret at %+v", sel, origin)
	}
}

func TestEscapeFromFindRestoresSelectionWithoutNavigation(t *testing.T) {
	ed := findUXEditor("one two\none three", text.Position{Line: 0, Col: 0})
	wantSel := text.Selection{
		Anchor: text.Position{Line: 0, Col: 0},
		Head:   text.Position{Line: 0, Col: 3},
	}
	ed.Buffer.SetSelection(wantSel.Anchor, wantSel.Head)

	ed.ShowFind()
	// Seeding jumps the cursor to the next match; Esc must still put the
	// original selection back because no match was navigated to.
	if ed.Buffer.Cursor == wantSel.Head {
		t.Fatal("seeding did not move the cursor; test setup is broken")
	}

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})

	if got := ed.Buffer.Selections.Primary(); got != wantSel {
		t.Fatalf("primary selection after escape = %#v, want restored %#v", got, wantSel)
	}
	if got, want := ed.Buffer.Cursor, wantSel.Head; got != want {
		t.Fatalf("cursor after escape = %+v, want selection head %+v", got, want)
	}
}

func TestEscapeFromFindKeepsCursorAfterNavigation(t *testing.T) {
	ed := findUXEditor("needle here\nneedle there", text.Position{Line: 0, Col: 0})

	ed.ShowFind()
	for _, ch := range "needle" {
		ed.UpdateFind(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	ed = drainFindScan(t, ed)

	// Enter steps to the next match: that is navigation.
	ed.UpdateFind(tea.KeyPressMsg{Code: tea.KeyEnter})
	want := text.Position{Line: 1, Col: 0}
	if ed.Buffer.Cursor != want {
		t.Fatalf("cursor before escape = %+v, want %+v; test setup is broken", ed.Buffer.Cursor, want)
	}

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})

	if ed.IsFindVisible() {
		t.Fatal("escape left the find widget visible")
	}
	if got := ed.Buffer.Cursor; got != want {
		t.Fatalf("cursor after navigated escape = %+v, want to stay at the match %+v", got, want)
	}
}

func TestEscapeFromFindAfterBackwardNavigationKeepsCursor(t *testing.T) {
	ed := findUXEditor("needle here\nneedle there", text.Position{Line: 1, Col: 2})

	ed.ShowFind()
	for _, ch := range "needle" {
		ed.UpdateFind(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	ed = drainFindScan(t, ed)
	ed.UpdateFind(tea.KeyPressMsg{Code: tea.KeyF3, Mod: tea.ModShift})
	// The cursor sits past both match starts, so the scan lands on match 0 and
	// shift+F3 wraps backwards to the last match on line 1.
	want := text.Position{Line: 1, Col: 0}
	if ed.Buffer.Cursor != want {
		t.Fatalf("cursor before escape = %+v, want %+v; test setup is broken", ed.Buffer.Cursor, want)
	}

	ed, _ = ed.Update(tea.KeyPressMsg{Text: "escape"})

	if got := ed.Buffer.Cursor; got != want {
		t.Fatalf("cursor after backward-navigated escape = %+v, want to stay at the match %+v", got, want)
	}
}
