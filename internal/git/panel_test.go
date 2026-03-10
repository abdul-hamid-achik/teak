package git

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// helper to create a minimal Model without running git commands.
func testModel(entries []StatusEntry) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 72
	ta := textarea.New()
	ta.SetHeight(5)
	ta.SetWidth(50)
	m := Model{
		theme:       ui.DefaultTheme(),
		rootDir:     "/tmp/fake",
		isGitRepo:   true,
		Entries:     entries,
		commitTitle: ti,
		commitBody:  ta,
		Width:       80,
		Height:      40,
	}
	m.deriveGroups()
	return m
}

// ── TestBuildTree ────────────────────────────────────────────────────────

func TestBuildTree(t *testing.T) {
	tests := []struct {
		name    string
		entries []StatusEntry
		staged  bool
		// expected checks
		wantRootCount int // number of top-level nodes
		check         func(t *testing.T, nodes []*GitTreeNode)
	}{
		{
			name: "single file at root level",
			entries: []StatusEntry{
				{Path: "go.mod", IndexStatus: 'M', WorkStatus: ' '},
			},
			staged:        true,
			wantRootCount: 1,
			check: func(t *testing.T, nodes []*GitTreeNode) {
				n := nodes[0]
				if n.Name != "go.mod" {
					t.Errorf("name = %q, want go.mod", n.Name)
				}
				if n.IsDir {
					t.Error("expected file, got dir")
				}
				if n.Depth != 0 {
					t.Errorf("depth = %d, want 0", n.Depth)
				}
				if n.Entry == nil {
					t.Error("entry is nil")
				}
				if n.Staged != true {
					t.Error("staged should be true")
				}
			},
		},
		{
			name: "single file in directory",
			entries: []StatusEntry{
				{Path: "internal/app/app.go", IndexStatus: 'A', WorkStatus: ' '},
			},
			staged:        false,
			wantRootCount: 1,
			check: func(t *testing.T, nodes []*GitTreeNode) {
				// internal(dir) -> app(dir) -> app.go(file)
				internal := nodes[0]
				if internal.Name != "internal" || !internal.IsDir {
					t.Errorf("expected dir 'internal', got name=%q isDir=%v", internal.Name, internal.IsDir)
				}
				if internal.Depth != 0 {
					t.Errorf("internal depth = %d, want 0", internal.Depth)
				}
				if len(internal.Children) != 1 {
					t.Fatalf("internal children = %d, want 1", len(internal.Children))
				}
				app := internal.Children[0]
				if app.Name != "app" || !app.IsDir {
					t.Errorf("expected dir 'app', got name=%q isDir=%v", app.Name, app.IsDir)
				}
				if app.Depth != 1 {
					t.Errorf("app depth = %d, want 1", app.Depth)
				}
				if len(app.Children) != 1 {
					t.Fatalf("app children = %d, want 1", len(app.Children))
				}
				file := app.Children[0]
				if file.Name != "app.go" || file.IsDir {
					t.Errorf("expected file 'app.go', got name=%q isDir=%v", file.Name, file.IsDir)
				}
				if file.Depth != 2 {
					t.Errorf("file depth = %d, want 2", file.Depth)
				}
				if file.Entry == nil {
					t.Error("leaf entry is nil")
				}
			},
		},
		{
			name: "multiple files in same directory",
			entries: []StatusEntry{
				{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
			},
			staged:        true,
			wantRootCount: 1,
			check: func(t *testing.T, nodes []*GitTreeNode) {
				src := nodes[0]
				if src.Name != "src" || !src.IsDir {
					t.Fatalf("expected dir 'src', got name=%q isDir=%v", src.Name, src.IsDir)
				}
				if len(src.Children) != 2 {
					t.Fatalf("src children = %d, want 2", len(src.Children))
				}
				if src.Children[0].Name != "a.go" {
					t.Errorf("first child = %q, want a.go", src.Children[0].Name)
				}
				if src.Children[1].Name != "b.go" {
					t.Errorf("second child = %q, want b.go", src.Children[1].Name)
				}
				for _, c := range src.Children {
					if c.IsDir {
						t.Errorf("%q should be a file", c.Name)
					}
				}
			},
		},
		{
			name: "files at different depths",
			entries: []StatusEntry{
				{Path: "README.md", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "internal/app/app.go", IndexStatus: 'A', WorkStatus: ' '},
				{Path: "internal/git/panel.go", IndexStatus: 'M', WorkStatus: ' '},
			},
			staged:        true,
			wantRootCount: 2, // README.md and internal/
			check: func(t *testing.T, nodes []*GitTreeNode) {
				// README.md
				if nodes[0].Name != "README.md" || nodes[0].IsDir {
					t.Errorf("first node: name=%q isDir=%v", nodes[0].Name, nodes[0].IsDir)
				}
				// internal dir
				internal := nodes[1]
				if internal.Name != "internal" || !internal.IsDir {
					t.Fatalf("second node: name=%q isDir=%v", internal.Name, internal.IsDir)
				}
				// internal should have app/ and git/ subdirs
				if len(internal.Children) != 2 {
					t.Fatalf("internal children = %d, want 2", len(internal.Children))
				}
				if internal.Children[0].Name != "app" {
					t.Errorf("first subdir = %q, want app", internal.Children[0].Name)
				}
				if internal.Children[1].Name != "git" {
					t.Errorf("second subdir = %q, want git", internal.Children[1].Name)
				}
			},
		},
		{
			name:          "empty entries",
			entries:       []StatusEntry{},
			staged:        true,
			wantRootCount: 0,
			check:         func(t *testing.T, nodes []*GitTreeNode) {},
		},
		{
			name: "directory entry with IsDir flag",
			entries: []StatusEntry{
				{Path: "internal/diff", IndexStatus: '?', WorkStatus: '?', IsDir: true},
			},
			staged:        false,
			wantRootCount: 1,
			check: func(t *testing.T, nodes []*GitTreeNode) {
				// Entry with IsDir=true should create directory nodes
				internal := nodes[0]
				if internal.Name != "internal" || !internal.IsDir {
					t.Errorf("expected dir 'internal', got name=%q isDir=%v", internal.Name, internal.IsDir)
				}
				if len(internal.Children) != 1 {
					t.Fatalf("internal children = %d, want 1", len(internal.Children))
				}
				// "diff" should also be a directory node
				diffNode := internal.Children[0]
				if diffNode.Name != "diff" {
					t.Errorf("child name = %q, want diff", diffNode.Name)
				}
				if !diffNode.IsDir {
					t.Error("diff should be a dir (entry has IsDir=true)")
				}
				if diffNode.Entry == nil {
					t.Error("dir entry should reference the StatusEntry")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := buildTree(tt.entries, tt.staged)
			if len(nodes) != tt.wantRootCount {
				t.Fatalf("root count = %d, want %d", len(nodes), tt.wantRootCount)
			}
			tt.check(t, nodes)
		})
	}
}

// ── TestFlattenTree ─────────────────────────────────────────────────────

func TestFlattenTree(t *testing.T) {
	tests := []struct {
		name      string
		entries   []StatusEntry
		collapse  func(nodes []*GitTreeNode) // mutate before flatten
		wantNames []string
	}{
		{
			name: "flat list all files no dirs",
			entries: []StatusEntry{
				{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "b.go", IndexStatus: 'M', WorkStatus: ' '},
			},
			wantNames: []string{"a.go", "b.go"},
		},
		{
			name: "nested tree depth-first order",
			entries: []StatusEntry{
				{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
			},
			wantNames: []string{"src", "a.go", "b.go"},
		},
		{
			name: "collapsed directory hides children",
			entries: []StatusEntry{
				{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
			},
			collapse: func(nodes []*GitTreeNode) {
				// Collapse the "src" directory
				nodes[0].Expanded = false
			},
			wantNames: []string{"src"},
		},
		{
			name: "mixed expanded and collapsed",
			entries: []StatusEntry{
				{Path: "pkg/x.go", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "src/a.go", IndexStatus: 'A', WorkStatus: ' '},
				{Path: "src/b.go", IndexStatus: 'A', WorkStatus: ' '},
			},
			collapse: func(nodes []*GitTreeNode) {
				// Collapse "src" (second top-level node), leave "pkg" expanded
				for _, n := range nodes {
					if n.Name == "src" {
						n.Expanded = false
					}
				}
			},
			wantNames: []string{"pkg", "x.go", "src"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := buildTree(tt.entries, true)
			if tt.collapse != nil {
				tt.collapse(nodes)
			}
			flat := flattenTree(nodes)
			if len(flat) != len(tt.wantNames) {
				names := make([]string, len(flat))
				for i, n := range flat {
					names[i] = n.Name
				}
				t.Fatalf("flat count = %d (%v), want %d (%v)", len(flat), names, len(tt.wantNames), tt.wantNames)
			}
			for i, want := range tt.wantNames {
				if flat[i].Name != want {
					t.Errorf("flat[%d].Name = %q, want %q", i, flat[i].Name, want)
				}
			}
		})
	}
}

// ── TestStatusEntry ─────────────────────────────────────────────────────

func TestStatusEntry(t *testing.T) {
	t.Run("IsStagedChange", func(t *testing.T) {
		tests := []struct {
			index byte
			work  byte
			want  bool
		}{
			{'M', ' ', true},  // modified in index
			{'A', ' ', true},  // added in index
			{'D', ' ', true},  // deleted in index
			{'R', ' ', true},  // renamed in index
			{' ', 'M', false}, // only working tree change
			{'?', '?', false}, // untracked
			{' ', ' ', false}, // no changes
		}
		for _, tt := range tests {
			e := StatusEntry{IndexStatus: tt.index, WorkStatus: tt.work}
			if got := e.IsStagedChange(); got != tt.want {
				t.Errorf("IsStagedChange(%c,%c) = %v, want %v", tt.index, tt.work, got, tt.want)
			}
		}
	})

	t.Run("IsUnstagedChange", func(t *testing.T) {
		tests := []struct {
			index byte
			work  byte
			want  bool
		}{
			{' ', 'M', true},  // modified in working tree
			{' ', 'D', true},  // deleted in working tree
			{'?', '?', true},  // untracked counts as unstaged
			{'M', ' ', false}, // staged only
			{'A', ' ', false}, // staged only
			{' ', ' ', false}, // no changes
		}
		for _, tt := range tests {
			e := StatusEntry{IndexStatus: tt.index, WorkStatus: tt.work}
			if got := e.IsUnstagedChange(); got != tt.want {
				t.Errorf("IsUnstagedChange(%c,%c) = %v, want %v", tt.index, tt.work, got, tt.want)
			}
		}
	})

	t.Run("IsUntracked", func(t *testing.T) {
		tests := []struct {
			index byte
			work  byte
			want  bool
		}{
			{'?', '?', true},
			{'M', ' ', false},
			{' ', 'M', false},
			{'?', ' ', false},
			{' ', '?', false},
		}
		for _, tt := range tests {
			e := StatusEntry{IndexStatus: tt.index, WorkStatus: tt.work}
			if got := e.IsUntracked(); got != tt.want {
				t.Errorf("IsUntracked(%c,%c) = %v, want %v", tt.index, tt.work, got, tt.want)
			}
		}
	})

	t.Run("DisplayStatus", func(t *testing.T) {
		tests := []struct {
			index  byte
			work   byte
			staged bool
			want   string
		}{
			{'M', ' ', true, "M"},  // staged modified
			{'A', ' ', true, "A"},  // staged added
			{'D', ' ', true, "D"},  // staged deleted
			{' ', 'M', false, "M"}, // unstaged modified
			{' ', 'D', false, "D"}, // unstaged deleted
			{'?', '?', false, "U"}, // untracked
			{'?', '?', true, "U"},  // untracked shown as staged (edge case)
			{'R', ' ', true, "R"},  // staged renamed
			{'C', ' ', true, "C"},  // staged copied
		}
		for _, tt := range tests {
			e := StatusEntry{IndexStatus: tt.index, WorkStatus: tt.work}
			if got := e.DisplayStatus(tt.staged); got != tt.want {
				t.Errorf("DisplayStatus(%c,%c,staged=%v) = %q, want %q", tt.index, tt.work, tt.staged, got, tt.want)
			}
		}
	})
}

// ── TestDeriveGroups ────────────────────────────────────────────────────

func TestDeriveGroups(t *testing.T) {
	tests := []struct {
		name         string
		entries      []StatusEntry
		wantStaged   int
		wantUnstaged int
	}{
		{
			name: "mixed staged and unstaged",
			entries: []StatusEntry{
				{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '}, // staged only
				{Path: "b.go", IndexStatus: ' ', WorkStatus: 'M'}, // unstaged only
				{Path: "c.go", IndexStatus: 'M', WorkStatus: 'M'}, // both
				{Path: "d.go", IndexStatus: '?', WorkStatus: '?'}, // untracked (unstaged only)
			},
			wantStaged:   2, // a.go, c.go
			wantUnstaged: 3, // b.go, c.go, d.go
		},
		{
			name:         "empty entries",
			entries:      []StatusEntry{},
			wantStaged:   0,
			wantUnstaged: 0,
		},
		{
			name: "all staged",
			entries: []StatusEntry{
				{Path: "x.go", IndexStatus: 'A', WorkStatus: ' '},
				{Path: "y.go", IndexStatus: 'M', WorkStatus: ' '},
			},
			wantStaged:   2,
			wantUnstaged: 0,
		},
		{
			name: "all unstaged",
			entries: []StatusEntry{
				{Path: "x.go", IndexStatus: ' ', WorkStatus: 'M'},
				{Path: "y.go", IndexStatus: '?', WorkStatus: '?'},
			},
			wantStaged:   0,
			wantUnstaged: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel(tt.entries)

			if len(m.Staged) != tt.wantStaged {
				t.Errorf("Staged count = %d, want %d", len(m.Staged), tt.wantStaged)
			}
			if len(m.Unstaged) != tt.wantUnstaged {
				t.Errorf("Unstaged count = %d, want %d", len(m.Unstaged), tt.wantUnstaged)
			}

			// Verify trees were built
			if tt.wantStaged > 0 && m.stagedTree == nil {
				t.Error("stagedTree is nil but expected entries")
			}
			if tt.wantUnstaged > 0 && m.unstagedTree == nil {
				t.Error("unstagedTree is nil but expected entries")
			}
		})
	}
}

// ── TestEntryAtY / TestNodeAtY ──────────────────────────────────────────

func TestEntryAtY(t *testing.T) {
	entries := []StatusEntry{
		{Path: "staged.go", IndexStatus: 'M', WorkStatus: ' '},   // staged only
		{Path: "unstaged.go", IndexStatus: ' ', WorkStatus: 'M'}, // unstaged only
	}
	m := testModel(entries)

	tests := []struct {
		name       string
		y          int
		wantEntry  bool
		wantPath   string
		wantStaged bool
	}{
		{"y=0 staged header", 0, false, "", false},
		{"y=1 first staged file", 1, true, "staged.go", true},
		{"y=2 unstaged header", 2, false, "", false},
		{"y=3 first unstaged file", 3, true, "unstaged.go", false},
		{"y=4 past end", 4, false, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, staged := m.EntryAtY(tt.y)
			if tt.wantEntry {
				if entry == nil {
					t.Fatalf("expected entry at y=%d, got nil", tt.y)
				}
				if entry.Path != tt.wantPath {
					t.Errorf("path = %q, want %q", entry.Path, tt.wantPath)
				}
				if staged != tt.wantStaged {
					t.Errorf("staged = %v, want %v", staged, tt.wantStaged)
				}
			} else {
				if entry != nil {
					t.Errorf("expected nil entry at y=%d, got %q", tt.y, entry.Path)
				}
			}
		})
	}
}

func TestEntryAtYCollapsed(t *testing.T) {
	entries := []StatusEntry{
		{Path: "staged.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "unstaged.go", IndexStatus: ' ', WorkStatus: 'M'},
	}
	m := testModel(entries)
	m.stagedCollapsed = true

	// With staged collapsed:
	// y=0: staged header
	// y=1: unstaged header (staged entries hidden)
	// y=2: first unstaged file

	entry0, _ := m.EntryAtY(0)
	if entry0 != nil {
		t.Error("y=0 should be staged header (nil)")
	}

	entry1, _ := m.EntryAtY(1)
	if entry1 != nil {
		t.Error("y=1 should be unstaged header (nil) when staged collapsed")
	}

	entry2, staged := m.EntryAtY(2)
	if entry2 == nil {
		t.Fatal("y=2 should be unstaged file when staged collapsed")
	}
	if entry2.Path != "unstaged.go" {
		t.Errorf("path = %q, want unstaged.go", entry2.Path)
	}
	if staged {
		t.Error("entry at y=2 should not be staged")
	}
}

func TestNodeAtY(t *testing.T) {
	entries := []StatusEntry{
		{Path: "src/main.go", IndexStatus: 'M', WorkStatus: ' '}, // staged: src(dir), main.go(file)
		{Path: "README.md", IndexStatus: ' ', WorkStatus: 'M'},   // unstaged: README.md(file)
	}
	m := testModel(entries)

	// Layout:
	// y=0: staged header -> nil
	// y=1: src (dir node) -> node, staged=true
	// y=2: main.go (file node) -> node, staged=true
	// y=3: unstaged header -> nil
	// y=4: README.md (file node) -> node, staged=false

	tests := []struct {
		name       string
		y          int
		wantNode   bool
		wantName   string
		wantIsDir  bool
		wantStaged bool
	}{
		{"staged header", 0, false, "", false, false},
		{"src dir", 1, true, "src", true, true},
		{"main.go file", 2, true, "main.go", false, true},
		{"unstaged header", 3, false, "", false, false},
		{"README.md file", 4, true, "README.md", false, false},
		{"past end", 5, false, "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, staged := m.NodeAtY(tt.y)
			if tt.wantNode {
				if node == nil {
					t.Fatalf("expected node at y=%d, got nil", tt.y)
				}
				if node.Name != tt.wantName {
					t.Errorf("name = %q, want %q", node.Name, tt.wantName)
				}
				if node.IsDir != tt.wantIsDir {
					t.Errorf("isDir = %v, want %v", node.IsDir, tt.wantIsDir)
				}
				if staged != tt.wantStaged {
					t.Errorf("staged = %v, want %v", staged, tt.wantStaged)
				}
			} else {
				if node != nil {
					t.Errorf("expected nil node at y=%d, got %q", tt.y, node.Name)
				}
			}
		})
	}
}

// ── TestFilesUnderDir ───────────────────────────────────────────────────

func TestFilesUnderDir(t *testing.T) {
	tests := []struct {
		name    string
		entries []StatusEntry
		dir     string
		want    []string
	}{
		{
			name: "files under known directory",
			entries: []StatusEntry{
				{Path: "src/a.go"},
				{Path: "src/b.go"},
				{Path: "pkg/c.go"},
				{Path: "src/sub/d.go"},
			},
			dir:  "src",
			want: []string{"src/a.go", "src/b.go", "src/sub/d.go"},
		},
		{
			name: "no files under nonexistent directory",
			entries: []StatusEntry{
				{Path: "src/a.go"},
			},
			dir:  "lib",
			want: nil,
		},
		{
			name:    "empty entries",
			entries: []StatusEntry{},
			dir:     "src",
			want:    nil,
		},
		{
			name: "exact prefix match only",
			entries: []StatusEntry{
				{Path: "src/a.go"},
				{Path: "srclib/b.go"}, // should NOT match "src"
			},
			dir:  "src",
			want: []string{"src/a.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilesUnderDir(tt.entries, tt.dir)
			if len(got) != len(tt.want) {
				t.Fatalf("count = %d (%v), want %d (%v)", len(got), got, len(tt.want), tt.want)
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("got[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

// ── TestParseStatusLines ────────────────────────────────────────────────

func TestParseStatusLines(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []StatusEntry
	}{
		{
			name: "empty output",
			raw:  "",
			want: nil,
		},
		{
			name: "single modified unstaged file",
			raw:  " M internal/app/app.go",
			want: []StatusEntry{
				{IndexStatus: ' ', WorkStatus: 'M', Path: "internal/app/app.go", IsDir: false},
			},
		},
		{
			name: "single staged file",
			raw:  "M  internal/app/app.go",
			want: []StatusEntry{
				{IndexStatus: 'M', WorkStatus: ' ', Path: "internal/app/app.go", IsDir: false},
			},
		},
		{
			name: "untracked file",
			raw:  "?? newfile.go",
			want: []StatusEntry{
				{IndexStatus: '?', WorkStatus: '?', Path: "newfile.go", IsDir: false},
			},
		},
		{
			name: "untracked directory with trailing slash",
			raw:  "?? internal/diff/",
			want: []StatusEntry{
				{IndexStatus: '?', WorkStatus: '?', Path: "internal/diff", IsDir: true},
			},
		},
		{
			name: "MM status (both staged and unstaged modifications)",
			raw:  "MM internal/git/panel.go",
			want: []StatusEntry{
				{IndexStatus: 'M', WorkStatus: 'M', Path: "internal/git/panel.go", IsDir: false},
			},
		},
		{
			name: "multiple lines mixed statuses",
			raw:  "M  staged.go\n M unstaged.go\n?? untracked.go\n?? newdir/",
			want: []StatusEntry{
				{IndexStatus: 'M', WorkStatus: ' ', Path: "staged.go", IsDir: false},
				{IndexStatus: ' ', WorkStatus: 'M', Path: "unstaged.go", IsDir: false},
				{IndexStatus: '?', WorkStatus: '?', Path: "untracked.go", IsDir: false},
				{IndexStatus: '?', WorkStatus: '?', Path: "newdir", IsDir: true},
			},
		},
		{
			name: "short line is skipped",
			raw:  "?? a\nXY",
			want: []StatusEntry{
				{IndexStatus: '?', WorkStatus: '?', Path: "a", IsDir: false},
			},
		},
		{
			name: "single char filename",
			raw:  "?? a",
			want: []StatusEntry{
				{IndexStatus: '?', WorkStatus: '?', Path: "a", IsDir: false},
			},
		},
		{
			name: "added file",
			raw:  "A  brand_new.go",
			want: []StatusEntry{
				{IndexStatus: 'A', WorkStatus: ' ', Path: "brand_new.go", IsDir: false},
			},
		},
		{
			name: "deleted file",
			raw:  "D  removed.go",
			want: []StatusEntry{
				{IndexStatus: 'D', WorkStatus: ' ', Path: "removed.go", IsDir: false},
			},
		},
		{
			name: "renamed file",
			raw:  "R  old.go -> new.go",
			want: []StatusEntry{
				{IndexStatus: 'R', WorkStatus: ' ', Path: "old.go -> new.go", IsDir: false},
			},
		},
		{
			name: "path with spaces is not trimmed",
			raw:  "?? path with spaces/file.go",
			want: []StatusEntry{
				{IndexStatus: '?', WorkStatus: '?', Path: "path with spaces/file.go", IsDir: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseStatusLines(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("count = %d, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.want), got, tt.want)
			}
			for i, w := range tt.want {
				g := got[i]
				if g.IndexStatus != w.IndexStatus {
					t.Errorf("[%d] IndexStatus = %c, want %c", i, g.IndexStatus, w.IndexStatus)
				}
				if g.WorkStatus != w.WorkStatus {
					t.Errorf("[%d] WorkStatus = %c, want %c", i, g.WorkStatus, w.WorkStatus)
				}
				if g.Path != w.Path {
					t.Errorf("[%d] Path = %q, want %q", i, g.Path, w.Path)
				}
				if g.IsDir != w.IsDir {
					t.Errorf("[%d] IsDir = %v, want %v", i, g.IsDir, w.IsDir)
				}
			}
		})
	}
}

// ── TestRefreshMsgUnstageFlow ───────────────────────────────────────────

func TestRefreshMsgUnstageFlow(t *testing.T) {
	t.Run("unstage single file moves it from staged to unstaged", func(t *testing.T) {
		// Initial state: one file staged (M in index, space in worktree)
		initial := []StatusEntry{
			{Path: "app.go", IndexStatus: 'M', WorkStatus: ' '},
		}
		m := testModel(initial)
		m.activeSection = SectionStaged
		m.Cursor = 0

		// Verify initial state
		if len(m.Staged) != 1 {
			t.Fatalf("initial Staged = %d, want 1", len(m.Staged))
		}
		if len(m.Unstaged) != 0 {
			t.Fatalf("initial Unstaged = %d, want 0", len(m.Unstaged))
		}

		// Simulate unstage: after `git reset HEAD -- app.go`, the file moves
		// from staged to unstaged. The refresh returns new porcelain output.
		refreshMsg := RefreshMsg{
			Branch: "main",
			Entries: []StatusEntry{
				{Path: "app.go", IndexStatus: ' ', WorkStatus: 'M'},
			},
		}
		m, _ = m.Update(refreshMsg)

		// After unstage, file should be in Unstaged only
		if len(m.Staged) != 0 {
			t.Errorf("after unstage: Staged = %d, want 0", len(m.Staged))
		}
		if len(m.Unstaged) != 1 {
			t.Errorf("after unstage: Unstaged = %d, want 1", len(m.Unstaged))
		}
		if len(m.Unstaged) > 0 && m.Unstaged[0].Path != "app.go" {
			t.Errorf("Unstaged[0].Path = %q, want app.go", m.Unstaged[0].Path)
		}

		// Active section should have moved to Unstaged since Staged is now empty
		if m.activeSection != SectionUnstaged {
			t.Errorf("activeSection = %v, want SectionUnstaged", m.activeSection)
		}

		// Tree should be rebuilt
		if len(m.stagedTree) != 0 {
			t.Errorf("stagedTree length = %d, want 0", len(m.stagedTree))
		}
		if len(m.unstagedTree) != 1 {
			t.Errorf("unstagedTree length = %d, want 1", len(m.unstagedTree))
		}
	})

	t.Run("unstage one of many staged files keeps section focus", func(t *testing.T) {
		// Initial: two staged files
		initial := []StatusEntry{
			{Path: "a.go", IndexStatus: 'A', WorkStatus: ' '},
			{Path: "b.go", IndexStatus: 'M', WorkStatus: ' '},
		}
		m := testModel(initial)
		m.activeSection = SectionStaged
		m.Cursor = 1 // cursor on b.go

		// After unstaging b.go: a.go stays staged, b.go becomes unstaged
		refreshMsg := RefreshMsg{
			Branch: "main",
			Entries: []StatusEntry{
				{Path: "a.go", IndexStatus: 'A', WorkStatus: ' '},
				{Path: "b.go", IndexStatus: ' ', WorkStatus: 'M'},
			},
		}
		m, _ = m.Update(refreshMsg)

		if len(m.Staged) != 1 {
			t.Errorf("Staged = %d, want 1", len(m.Staged))
		}
		if len(m.Unstaged) != 1 {
			t.Errorf("Unstaged = %d, want 1", len(m.Unstaged))
		}
		// Section should stay on Staged since it still has entries
		if m.activeSection != SectionStaged {
			t.Errorf("activeSection = %v, want SectionStaged", m.activeSection)
		}
		// Cursor should be clamped to valid range (was 1, now only 1 entry at index 0)
		flat := flattenTree(m.stagedTree)
		if m.Cursor >= len(flat) {
			t.Errorf("Cursor = %d, but flat length = %d", m.Cursor, len(flat))
		}
	})

	t.Run("stage moves file from unstaged to staged", func(t *testing.T) {
		// Initial: one unstaged file
		initial := []StatusEntry{
			{Path: "c.go", IndexStatus: ' ', WorkStatus: 'M'},
		}
		m := testModel(initial)
		m.activeSection = SectionUnstaged
		m.Cursor = 0

		// After staging: c.go moves to staged
		refreshMsg := RefreshMsg{
			Branch: "main",
			Entries: []StatusEntry{
				{Path: "c.go", IndexStatus: 'M', WorkStatus: ' '},
			},
		}
		m, _ = m.Update(refreshMsg)

		if len(m.Staged) != 1 {
			t.Errorf("Staged = %d, want 1", len(m.Staged))
		}
		if len(m.Unstaged) != 0 {
			t.Errorf("Unstaged = %d, want 0", len(m.Unstaged))
		}
		// Active section should move to Staged since Unstaged is now empty
		if m.activeSection != SectionStaged {
			t.Errorf("activeSection = %v, want SectionStaged", m.activeSection)
		}
	})

	t.Run("error in RefreshMsg preserves current state", func(t *testing.T) {
		initial := []StatusEntry{
			{Path: "x.go", IndexStatus: 'M', WorkStatus: ' '},
		}
		m := testModel(initial)
		m.Branch = "main"

		// Send a RefreshMsg with an error
		refreshMsg := RefreshMsg{
			Err: fmt.Errorf("git reset failed"),
		}
		m, _ = m.Update(refreshMsg)

		// State should be unchanged
		if len(m.Staged) != 1 {
			t.Errorf("Staged = %d, want 1 (preserved)", len(m.Staged))
		}
		if m.Branch != "main" {
			t.Errorf("Branch = %q, want main (preserved)", m.Branch)
		}
	})
}

// ── TestMMStatusBothSections ────────────────────────────────────────────

func TestMMStatusBothSections(t *testing.T) {
	// A file with MM status should appear in BOTH staged and unstaged sections.
	entries := []StatusEntry{
		{Path: "both.go", IndexStatus: 'M', WorkStatus: 'M'},
	}
	m := testModel(entries)

	if len(m.Staged) != 1 {
		t.Fatalf("Staged = %d, want 1", len(m.Staged))
	}
	if len(m.Unstaged) != 1 {
		t.Fatalf("Unstaged = %d, want 1", len(m.Unstaged))
	}
	if m.Staged[0].Path != "both.go" {
		t.Errorf("Staged[0].Path = %q, want both.go", m.Staged[0].Path)
	}
	if m.Unstaged[0].Path != "both.go" {
		t.Errorf("Unstaged[0].Path = %q, want both.go", m.Unstaged[0].Path)
	}

	// Both trees should have the file
	if len(m.stagedTree) != 1 {
		t.Errorf("stagedTree = %d, want 1", len(m.stagedTree))
	}
	if len(m.unstagedTree) != 1 {
		t.Errorf("unstagedTree = %d, want 1", len(m.unstagedTree))
	}
}

// ── TestDirEntryInTree ──────────────────────────────────────────────────

func TestDirEntryInTree(t *testing.T) {
	t.Run("untracked directory shows in unstaged tree", func(t *testing.T) {
		entries := []StatusEntry{
			{Path: "internal/diff", IndexStatus: '?', WorkStatus: '?', IsDir: true},
		}
		m := testModel(entries)

		// Untracked dirs are unstaged
		if len(m.Unstaged) != 1 {
			t.Fatalf("Unstaged = %d, want 1", len(m.Unstaged))
		}
		if len(m.Staged) != 0 {
			t.Fatalf("Staged = %d, want 0", len(m.Staged))
		}

		// The unstaged tree should have: internal(dir) > diff(dir)
		if len(m.unstagedTree) != 1 {
			t.Fatalf("unstagedTree root count = %d, want 1", len(m.unstagedTree))
		}
		internal := m.unstagedTree[0]
		if internal.Name != "internal" || !internal.IsDir {
			t.Errorf("root node: name=%q isDir=%v, want internal dir", internal.Name, internal.IsDir)
		}
		if len(internal.Children) != 1 {
			t.Fatalf("internal children = %d, want 1", len(internal.Children))
		}
		diff := internal.Children[0]
		if diff.Name != "diff" || !diff.IsDir {
			t.Errorf("child: name=%q isDir=%v, want diff dir", diff.Name, diff.IsDir)
		}
		if diff.Entry == nil {
			t.Error("diff node Entry should reference the StatusEntry")
		}
		if diff.Entry != nil && diff.Entry.IsUntracked() != true {
			t.Error("diff entry should be untracked")
		}
	})

	t.Run("directory entry flattens to correct nodes", func(t *testing.T) {
		entries := []StatusEntry{
			{Path: "internal/diff", IndexStatus: '?', WorkStatus: '?', IsDir: true},
		}
		tree := buildTree(entries, false)
		flat := flattenTree(tree)

		// Should be: internal(dir), diff(dir)
		if len(flat) != 2 {
			names := make([]string, len(flat))
			for i, n := range flat {
				names[i] = n.Name
			}
			t.Fatalf("flat count = %d (%v), want 2 [internal, diff]", len(flat), names)
		}
		if flat[0].Name != "internal" {
			t.Errorf("flat[0].Name = %q, want internal", flat[0].Name)
		}
		if flat[1].Name != "diff" {
			t.Errorf("flat[1].Name = %q, want diff", flat[1].Name)
		}
	})

	t.Run("mixed directory and file entries", func(t *testing.T) {
		entries := []StatusEntry{
			{Path: "internal/diff", IndexStatus: '?', WorkStatus: '?', IsDir: true},
			{Path: "README.md", IndexStatus: ' ', WorkStatus: 'M'},
		}
		m := testModel(entries)

		if len(m.Unstaged) != 2 {
			t.Fatalf("Unstaged = %d, want 2", len(m.Unstaged))
		}

		flat := flattenTree(m.unstagedTree)
		// Should be: internal(dir), diff(dir), README.md(file)
		if len(flat) != 3 {
			names := make([]string, len(flat))
			for i, n := range flat {
				names[i] = n.Name
			}
			t.Fatalf("flat count = %d (%v), want 3", len(flat), names)
		}
	})
}

// ── TestRefreshMsgCursorClamp ───────────────────────────────────────────

func TestRefreshMsgCursorClamp(t *testing.T) {
	// Start with 3 staged files, cursor at index 2
	initial := []StatusEntry{
		{Path: "a.go", IndexStatus: 'A', WorkStatus: ' '},
		{Path: "b.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "c.go", IndexStatus: 'M', WorkStatus: ' '},
	}
	m := testModel(initial)
	m.activeSection = SectionStaged
	m.Cursor = 2

	// After refresh, only 1 staged file remains
	refreshMsg := RefreshMsg{
		Branch: "main",
		Entries: []StatusEntry{
			{Path: "a.go", IndexStatus: 'A', WorkStatus: ' '},
			{Path: "b.go", IndexStatus: ' ', WorkStatus: 'M'},
			{Path: "c.go", IndexStatus: ' ', WorkStatus: 'M'},
		},
	}
	m, _ = m.Update(refreshMsg)

	// Cursor should be clamped to valid index within staged tree
	flat := m.activeFlatTree()
	if m.Cursor >= len(flat) && len(flat) > 0 {
		t.Errorf("Cursor = %d but flat length = %d", m.Cursor, len(flat))
	}
}

func TestRefreshMsgPreservesCollapsedDirectoryState(t *testing.T) {
	t.Run("preserves collapsed top-level directory", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
			{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
		})

		if len(m.stagedTree) != 1 || !m.stagedTree[0].IsDir {
			t.Fatalf("expected staged tree to contain one directory node, got %#v", m.stagedTree)
		}
		m.stagedTree[0].Expanded = false

		m, _ = m.Update(RefreshMsg{
			Branch: "main",
			Entries: []StatusEntry{
				{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "src/b.go", IndexStatus: 'M', WorkStatus: ' '},
			},
		})

		if m.stagedTree[0].Expanded {
			t.Fatal("expected src directory to stay collapsed after refresh")
		}
		if got := len(flattenTree(m.stagedTree)); got != 1 {
			t.Fatalf("expected collapsed tree to flatten to 1 row, got %d", got)
		}
	})

	t.Run("preserves nested collapsed directory", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "src/pkg/a.go", IndexStatus: 'M', WorkStatus: ' '},
			{Path: "src/other.go", IndexStatus: 'M', WorkStatus: ' '},
		})

		if len(m.stagedTree) != 1 || len(m.stagedTree[0].Children) < 1 {
			t.Fatalf("expected nested directory tree, got %#v", m.stagedTree)
		}
		src := m.stagedTree[0]
		pkg := src.Children[0]
		if !pkg.IsDir {
			t.Fatalf("expected first child to be a directory, got %#v", pkg)
		}
		pkg.Expanded = false

		m, _ = m.Update(RefreshMsg{
			Branch: "main",
			Entries: []StatusEntry{
				{Path: "src/pkg/a.go", IndexStatus: 'M', WorkStatus: ' '},
				{Path: "src/other.go", IndexStatus: 'M', WorkStatus: ' '},
			},
		})

		src = m.stagedTree[0]
		pkg = src.Children[0]
		if !src.Expanded {
			t.Fatal("expected ancestor directory to remain expanded after refresh")
		}
		if pkg.Expanded {
			t.Fatal("expected nested pkg directory to stay collapsed after refresh")
		}
	})

	t.Run("preserves staged and unstaged expansion independently", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "src/both.go", IndexStatus: 'M', WorkStatus: 'M'},
		})

		if len(m.stagedTree) != 1 || len(m.unstagedTree) != 1 {
			t.Fatalf("expected src directory in both sections, got staged=%d unstaged=%d", len(m.stagedTree), len(m.unstagedTree))
		}
		m.stagedTree[0].Expanded = false
		m.unstagedTree[0].Expanded = true

		m, _ = m.Update(RefreshMsg{
			Branch: "main",
			Entries: []StatusEntry{
				{Path: "src/both.go", IndexStatus: 'M', WorkStatus: 'M'},
			},
		})

		if m.stagedTree[0].Expanded {
			t.Fatal("expected staged src directory to stay collapsed after refresh")
		}
		if !m.unstagedTree[0].Expanded {
			t.Fatal("expected unstaged src directory to stay expanded after refresh")
		}
	})
}

func TestMouseClickOnDirectorySetsSectionCursorAndTogglesOnce(t *testing.T) {
	tests := []struct {
		name          string
		entries       []StatusEntry
		clickY        int
		wantSection   GitSection
		wantFlatCount int
	}{
		{
			name: "staged directory click",
			entries: []StatusEntry{
				{Path: "src/a.go", IndexStatus: 'M', WorkStatus: ' '},
			},
			clickY:        1,
			wantSection:   SectionStaged,
			wantFlatCount: 1,
		},
		{
			name: "unstaged directory click",
			entries: []StatusEntry{
				{Path: "src/a.go", IndexStatus: ' ', WorkStatus: 'M'},
			},
			clickY:        2,
			wantSection:   SectionUnstaged,
			wantFlatCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := testModel(tt.entries)
			m.activeSection = SectionCommitBody
			m.Cursor = 99

			updated, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{Button: tea.MouseLeft, Y: tt.clickY}))
			if cmd != nil {
				t.Fatal("expected directory click to avoid returning a command")
			}

			if updated.activeSection != tt.wantSection {
				t.Fatalf("activeSection = %v, want %v", updated.activeSection, tt.wantSection)
			}
			if updated.Cursor != 0 {
				t.Fatalf("Cursor = %d, want 0", updated.Cursor)
			}

			flat := updated.activeFlatTree()
			if got := len(flat); got != tt.wantFlatCount {
				t.Fatalf("flattened tree count = %d, want %d", got, tt.wantFlatCount)
			}
			if len(flat) == 0 {
				t.Fatal("expected directory row to remain visible")
			}
			if flat[0].Name != "src" || flat[0].Expanded {
				t.Fatalf("expected src to be collapsed after one click, got name=%q expanded=%v", flat[0].Name, flat[0].Expanded)
			}
		})
	}
}

func TestEnsureCursorVisibleUsesScrollY(t *testing.T) {
	m := testModel([]StatusEntry{
		{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "b.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "c.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "d.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "e.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "f.go", IndexStatus: 'M', WorkStatus: ' '},
	})
	m.Width = 40
	m.Height = 9 // full footer visible, tree viewport becomes 1 line
	m.activeSection = SectionStaged
	m.Cursor = 5

	m.ensureCursorVisible()

	if m.ScrollY <= 0 {
		t.Fatalf("expected ScrollY to move down for active row, got %d", m.ScrollY)
	}
	node, staged := m.NodeAtY(0)
	if node == nil {
		t.Fatal("expected visible node at y=0 after scrolling")
	}
	if !staged {
		t.Fatal("expected visible node to be in staged section")
	}
	if node.Name != "f.go" {
		t.Fatalf("visible node = %q, want f.go", node.Name)
	}
}

func TestNodeAtYWithScroll(t *testing.T) {
	m := testModel([]StatusEntry{
		{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "b.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "c.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "d.go", IndexStatus: 'M', WorkStatus: ' '},
	})
	m.Width = 40
	m.Height = 12
	m.ScrollY = 2

	node, staged := m.NodeAtY(0)
	if node == nil {
		t.Fatal("expected visible node at y=0 with scroll applied")
	}
	if !staged {
		t.Fatal("expected node at y=0 to be in staged section")
	}
	if node.Name != "b.go" {
		t.Fatalf("node at y=0 = %q, want b.go", node.Name)
	}
}

func TestMouseWheelScrollsTreeRows(t *testing.T) {
	m := testModel([]StatusEntry{
		{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "b.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "c.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "d.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "e.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "f.go", IndexStatus: 'M', WorkStatus: ' '},
	})
	m.Width = 40
	m.Height = 10
	m.activeSection = SectionStaged
	m.Cursor = 0

	updated, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown, Y: 0}))
	if updated.ScrollY <= 0 {
		t.Fatalf("expected mouse wheel to increase ScrollY, got %d", updated.ScrollY)
	}
	if updated.Cursor != 0 {
		t.Fatalf("expected mouse wheel scrolling to keep cursor, got %d", updated.Cursor)
	}
}

func TestViewWithOverflowKeepsFooterVisible(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()

	m := testModel([]StatusEntry{
		{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "b.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "c.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "d.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "e.go", IndexStatus: 'M', WorkStatus: ' '},
		{Path: "f.go", IndexStatus: 'M', WorkStatus: ' '},
	})
	m.Width = 40
	m.Height = 10

	view := m.View()
	if !strings.Contains(view, "Commit") {
		t.Fatal("expected overflowing view to keep footer actions visible")
	}
	if got := m.commitFormStartY(); got < 0 {
		t.Fatalf("expected commit form to remain visible when tree overflows, got %d", got)
	}
}

// ── TestCommitFormStartY ────────────────────────────────────────────────

func TestCommitFormStartY(t *testing.T) {
	t.Run("returns valid Y with entries", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "a.go", IndexStatus: ' ', WorkStatus: 'M'},
			{Path: "b.go", IndexStatus: ' ', WorkStatus: 'M'},
		})
		m.Width = 40
		m.Height = 30
		y := m.commitFormStartY()
		// Should be positive: staged header (1) + unstaged header (1) + 2 entries + padding
		if y < 0 {
			t.Errorf("commitFormStartY() = %d, want >= 0", y)
		}
		// Should be less than Height
		if y >= m.Height {
			t.Errorf("commitFormStartY() = %d, want < %d", y, m.Height)
		}
	})

	t.Run("returns -1 when no space", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "a.go", IndexStatus: ' ', WorkStatus: 'M'},
		})
		m.Width = 40
		m.Height = 4 // too small for form
		y := m.commitFormStartY()
		if y != -1 {
			t.Errorf("commitFormStartY() = %d, want -1 (no space)", y)
		}
	})
}

// ── TestFocusBodyAt ─────────────────────────────────────────────────────

func TestFocusBodyAt(t *testing.T) {
	t.Run("focuses body on click", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "a.go", IndexStatus: ' ', WorkStatus: 'M'},
		})
		m.Width = 40
		m.Height = 30
		m.commitBody.SetValue("hello world\nsecond line")

		formY := m.commitFormStartY()
		if formY < 0 {
			t.Fatal("form not visible")
		}

		// Click on the second body line
		m.FocusBodyAt(formY+3, 6)
		if !m.bodyFocused {
			t.Error("expected bodyFocused=true")
		}
		// Cursor positioning is now handled internally by textarea
	})

	t.Run("focuses body in valid range", func(t *testing.T) {
		m := testModel(nil)
		m.Width = 40
		m.Height = 30
		m.commitBody.SetValue("short")

		formY := m.commitFormStartY()
		if formY < 0 {
			t.Fatal("form not visible")
		}

		// Click far past end of text - should still focus body
		m.FocusBodyAt(formY+2, 50)
		if !m.bodyFocused {
			t.Error("expected bodyFocused=true")
		}
	})
}

// ── TestIsInCommitFormArea ──────────────────────────────────────────────

func TestIsInCommitFormArea(t *testing.T) {
	m := testModel([]StatusEntry{
		{Path: "a.go", IndexStatus: ' ', WorkStatus: 'M'},
	})
	m.Width = 40
	m.Height = 30

	formY := m.commitFormStartY()
	if formY < 0 {
		t.Fatal("form not visible")
	}

	// Before form area
	if m.IsInCommitFormArea(0) {
		t.Error("Y=0 should not be in commit form area")
	}
	// At form start
	if !m.IsInCommitFormArea(formY) {
		t.Errorf("Y=%d should be in commit form area", formY)
	}
	// After form start
	if !m.IsInCommitFormArea(formY + 2) {
		t.Errorf("Y=%d should be in commit form area", formY+2)
	}
}

// ── TestCommitFormHitTest ───────────────────────────────────────────────

func TestCommitFormHitTest(t *testing.T) {
	m := testModel([]StatusEntry{
		{Path: "a.go", IndexStatus: ' ', WorkStatus: 'M'},
	})
	m.Width = 40
	m.Height = 30

	formY := m.commitFormStartY()
	if formY < 0 {
		t.Fatal("form not visible")
	}

	// Before form area
	if got := m.CommitFormHitTest(0); got != "" {
		t.Errorf("Y=0: got %q, want empty", got)
	}

	// Top border line
	if got := m.CommitFormHitTest(formY); got != "" {
		t.Errorf("Y=%d (top border): got %q, want empty", formY, got)
	}

	// Title line (formY + 1)
	if got := m.CommitFormHitTest(formY + 1); got != "title" {
		t.Errorf("Y=%d (title): got %q, want %q", formY+1, got, "title")
	}

	// First body line (formY + 2)
	if got := m.CommitFormHitTest(formY + 2); got != "body" {
		t.Errorf("Y=%d (body): got %q, want %q", formY+2, got, "body")
	}

	// Second body line (formY + 3) — within default body height
	if got := m.CommitFormHitTest(formY + 3); got != "body" {
		t.Errorf("Y=%d (body line 2): got %q, want %q", formY+3, got, "body")
	}

	// Past body (formY + 2 + bodyViewHeight)
	bodyHeight := m.bodyViewHeight()
	pastBody := formY + 2 + bodyHeight
	if got := m.CommitFormHitTest(pastBody); got != "" {
		t.Errorf("Y=%d (past body): got %q, want empty", pastBody, got)
	}
}

// ── TestFocusTitleAt ────────────────────────────────────────────────────

func TestFocusTitleAt(t *testing.T) {
	t.Run("positions cursor at click offset", func(t *testing.T) {
		m := testModel(nil)
		m.Width = 40
		m.Height = 30
		m.commitTitle.SetValue("hello world")
		// SetWidth so the textinput knows its viewport size
		m.commitTitle.SetWidth(m.Width - 2)

		// Simulate a click at panel X=6 → cursor should be at position 5
		// (X=6 minus 1 for the left border char)
		m.FocusTitleAt(6)
		if !m.titleFocused {
			t.Error("expected titleFocused=true")
		}
		if got := m.commitTitle.Position(); got != 5 {
			t.Errorf("cursor position = %d, want 5", got)
		}
	})

	t.Run("clamps to value length", func(t *testing.T) {
		m := testModel(nil)
		m.Width = 40
		m.Height = 30
		m.commitTitle.SetValue("hi")
		m.commitTitle.SetWidth(m.Width - 2)

		// Click far past the value end
		m.FocusTitleAt(30)
		if got := m.commitTitle.Position(); got != 2 {
			t.Errorf("cursor position = %d, want 2 (clamped to len)", got)
		}
	})

	t.Run("click at border edge gives position 0", func(t *testing.T) {
		m := testModel(nil)
		m.Width = 40
		m.Height = 30
		m.commitTitle.SetValue("test")
		m.commitTitle.SetWidth(m.Width - 2)

		// Click at X=0 (on the border itself) → should clamp to 0
		m.FocusTitleAt(0)
		if got := m.commitTitle.Position(); got != 0 {
			t.Errorf("cursor position = %d, want 0", got)
		}
	})
}

// ── TestEnterInTitleMovesToBody ──────────────────────────────────────────

func TestEnterInTitleMovesToBody(t *testing.T) {
	m := testModel([]StatusEntry{
		{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
	})
	m.Width = 40
	m.Height = 30
	m.commitTitle.SetValue("fix: something")
	m.titleFocused = true
	m.activeSection = SectionCommitTitle

	// Simulate pressing Enter while title is focused
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	m, _ = m.Update(msg)

	// Title focus should move to body
	if m.titleFocused {
		t.Error("titleFocused should be false after Enter")
	}
	if !m.bodyFocused {
		t.Error("bodyFocused should be true after Enter")
	}
	if m.activeSection != SectionCommitBody {
		t.Errorf("activeSection = %v, want SectionCommitBody", m.activeSection)
	}
	// The commit title value should remain unchanged (no commit happened)
	if m.commitTitle.Value() != "fix: something" {
		t.Errorf("title value = %q, want %q", m.commitTitle.Value(), "fix: something")
	}
}

// ── TestDoCommitRequiresStaged ──────────────────────────────────────────

func TestDoCommitRequiresStaged(t *testing.T) {
	t.Run("refuses commit with no staged changes", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "a.go", IndexStatus: ' ', WorkStatus: 'M'}, // unstaged only
		})
		m.commitTitle.SetValue("should not commit")

		m, cmd := m.DoCommit()
		if cmd != nil {
			t.Error("DoCommit should return nil cmd when nothing is staged")
		}
		// Title should NOT be cleared
		if m.commitTitle.Value() != "should not commit" {
			t.Errorf("title was cleared unexpectedly: %q", m.commitTitle.Value())
		}
	})

	t.Run("refuses commit with empty title", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		})
		m.commitTitle.SetValue("")

		m, cmd := m.DoCommit()
		if cmd != nil {
			t.Error("DoCommit should return nil cmd when title is empty")
		}
	})

	t.Run("allows commit with staged changes and title", func(t *testing.T) {
		m := testModel([]StatusEntry{
			{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '},
		})
		m.commitTitle.SetValue("fix: bug")

		m, cmd := m.DoCommit()
		if cmd == nil {
			t.Error("DoCommit should return a cmd when staged changes and title exist")
		}
		// Title should be cleared after commit
		if m.commitTitle.Value() != "" {
			t.Errorf("title should be cleared after commit, got %q", m.commitTitle.Value())
		}
	})
}

// ── TestBodyScroll ──────────────────────────────────────────────────────
// Note: Scrolling is now handled internally by the textarea component.
// These tests verify that mouse wheel events are properly delegated.

func TestBodyScroll(t *testing.T) {
	t.Run("wheel events are delegated to textarea when body focused", func(t *testing.T) {
		m := testModel(nil)
		m.Width = 40
		m.Height = 30
		m.commitBody.SetValue("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8")
		m.bodyFocused = true

		// Textarea handles scrolling internally
		// Just verify the event is processed without error
		mouse := tea.Mouse{Button: tea.MouseWheelDown, X: 5, Y: 10}
		msg := tea.MouseWheelMsg(mouse)
		_, _ = m.Update(msg)

		// With textarea, scrolling is internal - just verify no panic
	})
}
