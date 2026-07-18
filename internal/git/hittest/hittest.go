package hittest

import "teak/internal/git"

// Section represents which section of the panel was hit
type Section int

const (
	SectionNone Section = iota
	SectionStaged
	SectionUnstaged
	SectionCommitTitle
	SectionCommitBody
)

// RowHit represents a hit test result for a tree row
type RowHit struct {
	Kind    int // treeRowKind: 0=none, 1=staged header, 2=node, 3=unstaged header
	Section git.GitSection
	Index   int
	Node    *git.GitTreeNode
}

// HitTester provides hit testing for the git panel
type HitTester struct {
	layout panelLayout
}

// panelLayout describes the panel layout
type panelLayout struct {
	treeHeight int
	bodyHeight int
}

// New creates a new HitTester with the given dimensions
func New(treeHeight, bodyHeight int) *HitTester {
	return &HitTester{
		layout: panelLayout{
			treeHeight: treeHeight,
			bodyHeight: bodyHeight,
		},
	}
}

// UpdateLayout updates the layout dimensions
func (h *HitTester) UpdateLayout(treeHeight, bodyHeight int) {
	h.layout.treeHeight = treeHeight
	h.layout.bodyHeight = bodyHeight
}

// RowHitAtY returns the row hit at the given Y coordinate relative to tree start
func (h *HitTester) RowHitAtY(y int, rows []RowHit) RowHit {
	if y < 0 || y >= h.layout.treeHeight || len(rows) == 0 {
		return RowHit{}
	}

	if y >= len(rows) {
		return RowHit{}
	}
	return rows[y]
}

// EntryAtY returns the status entry at the given panel Y coordinate
func (h *HitTester) EntryAtY(y int, rows []RowHit) (*git.StatusEntry, bool) {
	hit := h.RowHitAtY(y, rows)
	if hit.Kind != 2 || hit.Node == nil || hit.Node.Entry == nil { // treeRowNode
		return nil, false
	}
	return hit.Node.Entry, hit.Section == git.SectionStaged
}

// NodeAtY returns the tree node at the given panel Y coordinate
func (h *HitTester) NodeAtY(y int, rows []RowHit) (*git.GitTreeNode, bool) {
	hit := h.RowHitAtY(y, rows)
	if hit.Kind != 2 || hit.Node == nil {
		return nil, false
	}
	return hit.Node, hit.Section == git.SectionStaged
}

// SectionAtY returns which section was hit at the given Y
func (h *HitTester) SectionAtY(y int, scrollY int, rowCount int) Section {
	// Calculate which row index this Y corresponds to
	rowIdx := scrollY + y
	if rowIdx < 0 {
		return SectionNone
	}

	// Check if in staged section (after staged header)
	if rowIdx == 0 {
		return SectionStaged // staged header
	}

	// Check staged section (rowIdx 1 to stagedCount)
	// This requires knowing the staged count - would need to be passed in

	return SectionNone
}

// CommitFormHit checks if panelY falls on the title or body of the commit form
func (h *HitTester) CommitFormHit(panelY, formY int) string {
	if formY < 0 {
		return ""
	}
	titleY := formY + 1
	if panelY == titleY {
		return "title"
	}
	bodyStartY := formY + 2
	if panelY >= bodyStartY && panelY < bodyStartY+h.layout.bodyHeight {
		return "body"
	}
	return ""
}

// MaxScroll calculates the maximum scroll value for a tree
func (h *HitTester) MaxScroll(rowCount int) int {
	if h.layout.treeHeight <= 0 || rowCount <= h.layout.treeHeight {
		return 0
	}
	return rowCount - h.layout.treeHeight
}

// NormalizeScroll normalizes a scroll value to be within valid bounds
func (h *HitTester) NormalizeScroll(scrollY, rowCount int) int {
	maxScroll := h.MaxScroll(rowCount)
	if scrollY < 0 {
		scrollY = 0
	}
	if scrollY > maxScroll {
		scrollY = maxScroll
	}
	return scrollY
}
