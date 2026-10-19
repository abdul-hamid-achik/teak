package filetree

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// fgEscape returns the SGR parameters lipgloss emits for a foreground style, so a
// rendered row can be searched for the exact colour a role resolved to.
func fgEscape(style lipgloss.Style) string {
	rendered := style.Render("x")
	prefix := rendered[:strings.Index(rendered, "x")]
	return strings.TrimSuffix(strings.TrimPrefix(prefix, "\x1b["), "m")
}

func rowContaining(view, needle string) string {
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, needle) {
			return row
		}
	}
	return ""
}

func TestGitIgnoredEntriesUseMutedRole(t *testing.T) {
	for _, theme := range []ui.Theme{ui.NordTheme(), ui.DraculaTheme()} {
		m := New(t.TempDir(), theme)
		if got, want := m.cachedStyles.gitIgnoredColor, theme.Muted.GetForeground(); got != want {
			t.Fatalf("New ignored colour = %v, want Muted %v", got, want)
		}
		m.SetTheme(theme)
		if got, want := m.cachedStyles.gitIgnoredColor, theme.Muted.GetForeground(); got != want {
			t.Fatalf("SetTheme ignored colour = %v, want Muted %v", got, want)
		}
	}
}

func TestViewRendersIgnoredAndUntrackedDistinctly(t *testing.T) {
	theme := ui.NordTheme()
	root := t.TempDir()
	m := New(root, theme)
	m.SetSize(40, 6)
	m.Entries = []Entry{
		{Name: "debug.log", Path: filepath.Join(root, "debug.log"), IsGitIgnored: true},
		{Name: "NOTES.md", Path: filepath.Join(root, "NOTES.md")},
		{Name: "plain.go", Path: filepath.Join(root, "plain.go")},
	}
	m.SetGitStatus(map[string]string{"NOTES.md": "U"})
	m.invalidateFlatCache()
	view := m.View()

	muted := fgEscape(lipgloss.NewStyle().Foreground(theme.Muted.GetForeground()))
	untracked := fgEscape(lipgloss.NewStyle().Foreground(theme.GitUntracked.GetForeground()))
	if muted == untracked {
		t.Fatalf("Muted and GitUntracked resolve to the same colour %q", muted)
	}

	ignoredRow := rowContaining(view, "debug.log")
	if !strings.Contains(ignoredRow, muted) {
		t.Errorf("ignored row lacks Muted colour %q: %q", muted, ignoredRow)
	}
	if strings.Contains(ignoredRow, untracked) {
		t.Errorf("ignored row uses the untracked colour: %q", ignoredRow)
	}

	untrackedRow := rowContaining(view, "NOTES.md")
	if !strings.Contains(untrackedRow, untracked) {
		t.Errorf("untracked row lacks GitUntracked colour %q: %q", untracked, untrackedRow)
	}
	if strings.Contains(untrackedRow, muted) {
		t.Errorf("untracked row uses the muted colour: %q", untrackedRow)
	}
	if !strings.Contains(untrackedRow, "U") {
		t.Errorf("untracked row lacks its U marker: %q", untrackedRow)
	}

	if row := rowContaining(view, "plain.go"); strings.Contains(row, muted) || strings.Contains(row, untracked) {
		t.Errorf("normal row should not use the muted or untracked colour: %q", row)
	}
}

func makeGitWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".git/objects", "pkg/.git", "pkg/sub", ".github"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{".gitignore", "main.go", "pkg/sub/a.go", "pkg/.git/config", ".github/ci.yml"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func entryNames(entries []Entry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	return names
}

func TestDotGitIsNeverListed(t *testing.T) {
	for _, showHidden := range []bool{true, false} {
		root := makeGitWorkspace(t)
		m := New(root, ui.NordTheme())
		m.SetShowHidden(showHidden)
		m.SetSize(40, 20)

		names := entryNames(m.flatEntries())
		if slices.Contains(names, ".git") {
			t.Fatalf("showHidden=%v: .git listed: %v", showHidden, names)
		}
		if showHidden {
			for _, want := range []string{".gitignore", ".github", "main.go"} {
				if !slices.Contains(names, want) {
					t.Errorf("showHidden=true: %s missing from %v", want, names)
				}
			}
		}
	}
}

func TestDotGitStaysHiddenAfterRefreshAndNested(t *testing.T) {
	root := makeGitWorkspace(t)
	m := New(root, ui.NordTheme())
	m.SetSize(40, 20)

	// Nested repositories (submodules, vendored clones) are hidden too.
	pkg := filepath.Join(root, "pkg")
	children := readDirEntries(root, pkg, 1, nil)
	if slices.Contains(entryNames(children), ".git") {
		t.Fatalf("nested .git listed: %v", entryNames(children))
	}

	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.RefreshDir(root)
	m.invalidateFlatCache()
	flat := m.flatEntries()
	if slices.Contains(entryNames(flat), ".git") {
		t.Fatalf(".git reappeared after refresh: %v", entryNames(flat))
	}
	if m.Cursor < 0 || m.Cursor >= len(flat) {
		t.Fatalf("cursor %d outside %d rows", m.Cursor, len(flat))
	}
}

func TestDotGitIsNotMatchedByFilter(t *testing.T) {
	root := makeGitWorkspace(t)
	m := New(root, ui.NordTheme())
	m.SetSize(40, 20)
	m.filter = ".git"
	m.invalidateFlatCache()
	names := entryNames(m.flatEntries())
	if slices.Contains(names, ".git") {
		t.Fatalf("filter surfaced .git: %v", names)
	}
}
