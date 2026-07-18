package hittest

import (
	"testing"

	"teak/internal/git"
)

func TestSectionValues(t *testing.T) {
	tests := []struct {
		section Section
		want    int
	}{
		{SectionNone, 0},
		{SectionStaged, 1},
		{SectionUnstaged, 2},
		{SectionCommitTitle, 3},
		{SectionCommitBody, 4},
	}

	for _, tt := range tests {
		name := func() string {
			switch tt.section {
			case SectionNone:
				return "SectionNone"
			case SectionStaged:
				return "SectionStaged"
			case SectionUnstaged:
				return "SectionUnstaged"
			case SectionCommitTitle:
				return "SectionCommitTitle"
			case SectionCommitBody:
				return "SectionCommitBody"
			default:
				return "Unknown"
			}
		}()

		t.Run(name, func(t *testing.T) {
			if int(tt.section) != tt.want {
				t.Errorf("Section = %d, want %d", tt.section, tt.want)
			}
		})
	}
}

func TestNewHitTester(t *testing.T) {
	ht := New(10, 20)

	if ht == nil {
		t.Fatal("expected non-nil HitTester")
	}
	if ht.layout.treeHeight != 10 {
		t.Errorf("treeHeight = %d, want 10", ht.layout.treeHeight)
	}
	if ht.layout.bodyHeight != 20 {
		t.Errorf("bodyHeight = %d, want 20", ht.layout.bodyHeight)
	}
}

func TestUpdateLayout(t *testing.T) {
	ht := New(10, 20)

	ht.UpdateLayout(30, 40)

	if ht.layout.treeHeight != 30 {
		t.Errorf("treeHeight = %d, want 30", ht.layout.treeHeight)
	}
	if ht.layout.bodyHeight != 40 {
		t.Errorf("bodyHeight = %d, want 40", ht.layout.bodyHeight)
	}
}

func TestRowHitAtY(t *testing.T) {
	ht := New(10, 10)

	rows := []RowHit{
		{Kind: 1},
		{Kind: 2},
		{Kind: 3},
	}

	tests := []struct {
		name     string
		y        int
		wantKind int
	}{
		{"y=0", 0, 1},
		{"y=1", 1, 2},
		{"y=2", 2, 3},
		{"negative y", -1, 0},
		{"y beyond length", 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit := ht.RowHitAtY(tt.y, rows)
			if hit.Kind != tt.wantKind {
				t.Errorf("Kind = %d, want %d", hit.Kind, tt.wantKind)
			}
		})
	}
}

func TestRowHitAtYEmptyRows(t *testing.T) {
	ht := New(10, 10)

	hit := ht.RowHitAtY(0, nil)
	if hit.Kind != 0 {
		t.Errorf("expected Kind 0 for empty rows, got %d", hit.Kind)
	}
}

func TestEntryAtY(t *testing.T) {
	ht := New(10, 10)

	node := &git.GitTreeNode{
		Entry: &git.StatusEntry{Path: "test.go"},
	}

	rows := []RowHit{
		{Kind: 1}, // header
		{Kind: 2, Node: node, Section: git.SectionStaged},
	}

	tests := []struct {
		name       string
		y          int
		wantEntry  bool
		wantStaged bool
	}{
		{"y=0 is header", 0, false, false},
		{"y=1 is staged node", 1, true, true},
		{"y=2 doesn't exist", 2, false, false},
		{"negative y", -1, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, staged := ht.EntryAtY(tt.y, rows)
			if tt.wantEntry && entry == nil {
				t.Error("expected entry, got nil")
			}
			if !tt.wantEntry && entry != nil {
				t.Errorf("expected nil, got entry")
			}
			if staged != tt.wantStaged {
				t.Errorf("staged = %v, want %v", staged, tt.wantStaged)
			}
		})
	}
}

func TestNodeAtY(t *testing.T) {
	ht := New(10, 10)

	node := &git.GitTreeNode{}

	rows := []RowHit{
		{Kind: 1},
		{Kind: 2, Node: node, Section: git.SectionUnstaged},
	}

	tests := []struct {
		name       string
		y          int
		wantNode   bool
		wantStaged bool
	}{
		{"y=1 is unstaged node", 1, true, false}, // unstaged returns false for "staged"
		{"y=0 is header", 0, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, unstaged := ht.NodeAtY(tt.y, rows)
			if tt.wantNode && n == nil {
				t.Error("expected node, got nil")
			}
			if unstaged != tt.wantStaged {
				t.Errorf("unstaged = %v, want %v", unstaged, tt.wantStaged)
			}
		})
	}
}

func TestSectionAtY(t *testing.T) {
	ht := New(10, 10)

	tests := []struct {
		name    string
		y       int
		scrollY int
		want    Section
	}{
		{"y=0 scroll=0", 0, 0, SectionStaged},
		{"negative y", -1, 0, SectionNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ht.SectionAtY(tt.y, tt.scrollY, 10)
			if got != tt.want {
				t.Errorf("SectionAtY() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommitFormHit(t *testing.T) {
	ht := New(10, 5)

	tests := []struct {
		name   string
		panelY int
		formY  int
		want   string
	}{
		{"title hit", 1, 0, "title"},
		{"body hit", 2, 0, "body"},
		{"body hit with offset", 4, 1, "body"},
		{"above form", 0, 0, ""},
		{"negative panelY", -1, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ht.CommitFormHit(tt.panelY, tt.formY)
			if got != tt.want {
				t.Errorf("CommitFormHit() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaxScroll(t *testing.T) {
	ht := New(10, 0)

	tests := []struct {
		name     string
		rowCount int
		want     int
	}{
		{"rows less than height", 5, 0},
		{"rows equal to height", 10, 0},
		{"rows greater than height", 20, 10},
		{"tree height zero", 20, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ht.layout.treeHeight = 10
			if tt.name == "tree height zero" {
				ht.layout.treeHeight = 0
			}
			got := ht.MaxScroll(tt.rowCount)
			if got != tt.want {
				t.Errorf("MaxScroll() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNormalizeScroll(t *testing.T) {
	ht := New(10, 0)
	ht.layout.treeHeight = 10

	tests := []struct {
		name     string
		scrollY  int
		rowCount int
		want     int
	}{
		{"normal scroll", 5, 20, 5},
		{"negative scroll", -5, 20, 0},
		{"scroll beyond max", 15, 20, 10},
		{"negative with small rows", -5, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ht.NormalizeScroll(tt.scrollY, tt.rowCount)
			if got != tt.want {
				t.Errorf("NormalizeScroll() = %d, want %d", got, tt.want)
			}
		})
	}
}
