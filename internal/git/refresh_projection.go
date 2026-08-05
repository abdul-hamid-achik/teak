package git

import (
	"sync"

	tea "charm.land/bubbletea/v2"
)

// expansionState is shared by value-copied panel models and background refresh
// commands. Update writes one path at a time; commands take a short snapshot
// before doing any work proportional to the repository status.
type expansionState struct {
	mu         sync.RWMutex
	generation uint64
	staged     map[string]bool
	unstaged   map[string]bool
}

type expansionSnapshot struct {
	generation uint64
	staged     map[string]bool
	unstaged   map[string]bool
}

func newExpansionState() *expansionState {
	return &expansionState{
		staged:   make(map[string]bool),
		unstaged: make(map[string]bool),
	}
}

func (s *expansionState) record(section GitSection, path string, expanded bool) {
	if s == nil || path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.unstaged
	if section == SectionStaged {
		target = s.staged
	}
	if expanded {
		delete(target, path)
	} else {
		target[path] = false
	}
	s.generation++
}

// toggle records the next desired visibility without mutating the prepared
// tree currently shared by value-copied Bubble Tea models. Repeated input that
// arrives before a projection completes toggles the pending value, rather than
// repeatedly deriving the same value from the old tree node.
func (s *expansionState) toggle(section GitSection, path string, current bool) {
	if s == nil || path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.unstaged
	if section == SectionStaged {
		target = s.staged
	}
	desired := !current
	if pending, ok := target[path]; ok {
		desired = !pending
	}
	if desired {
		delete(target, path)
	} else {
		target[path] = false
	}
	s.generation++
}

func (s *expansionState) snapshot() expansionSnapshot {
	if s == nil {
		return expansionSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return expansionSnapshot{
		generation: s.generation,
		staged:     cloneExpansionMap(s.staged),
		unstaged:   cloneExpansionMap(s.unstaged),
	}
}

func (s *expansionState) currentGeneration() uint64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

func cloneExpansionMap(source map[string]bool) map[string]bool {
	cloned := make(map[string]bool, len(source))
	for path, expanded := range source {
		cloned[path] = expanded
	}
	return cloned
}

type refreshProjection struct {
	entries      []StatusEntry
	staged       []StatusEntry
	unstaged     []StatusEntry
	stagedTree   []*GitTreeNode
	unstagedTree []*GitTreeNode
	rowCache     *treeRowCache
	gitStatus    map[string]string
}

type treeProjection struct {
	stagedTree   []*GitTreeNode
	unstagedTree []*GitTreeNode
	rowCache     *treeRowCache
}

// PreparedRefreshMsg contains the repository-status projection built outside
// Bubble Tea's Update loop. RequestGeneration rejects an older Git result;
// ExpansionGeneration rejects a projection built before a directory toggle.
type PreparedRefreshMsg struct {
	Branch              string
	Generation          int
	RequestGeneration   uint64
	ExpansionGeneration uint64
	SectionGeneration   uint64
	GitStatus           map[string]string
	projection          *refreshProjection
}

// PreparedTreeProjectionMsg carries a visibility-only projection built after
// a directory or section toggle. StatusGeneration prevents an interaction
// based on old repository data from replacing a newer refresh.
type PreparedTreeProjectionMsg struct {
	StatusGeneration    uint64
	ExpansionGeneration uint64
	SectionGeneration   uint64
	projection          *treeProjection
}

func prepareRefreshCmd(msg RefreshMsg, requestGeneration uint64, state *expansionState, stagedCollapsed, unstagedCollapsed bool, sectionGeneration uint64) tea.Cmd {
	return func() tea.Msg {
		expansion := state.snapshot()
		projection := buildRefreshProjection(msg.Entries, expansion, stagedCollapsed, unstagedCollapsed)
		return PreparedRefreshMsg{
			Branch:              msg.Branch,
			Generation:          msg.Generation,
			RequestGeneration:   requestGeneration,
			ExpansionGeneration: expansion.generation,
			SectionGeneration:   sectionGeneration,
			GitStatus:           projection.gitStatus,
			projection:          projection,
		}
	}
}

func buildRefreshProjection(entries []StatusEntry, expansion expansionSnapshot, stagedCollapsed, unstagedCollapsed bool) *refreshProjection {
	projection := &refreshProjection{
		entries:   entries,
		gitStatus: make(map[string]string, len(entries)),
	}
	for _, entry := range entries {
		if entry.IsStagedChange() {
			projection.staged = append(projection.staged, entry)
		}
		if entry.IsUnstagedChange() {
			projection.unstaged = append(projection.unstaged, entry)
			projection.gitStatus[entry.Path] = entry.DisplayStatus(false)
		} else if entry.IsStagedChange() {
			projection.gitStatus[entry.Path] = entry.DisplayStatus(true)
		}
	}
	projection.stagedTree = buildTree(projection.staged, true)
	projection.unstagedTree = buildTree(projection.unstaged, false)
	restoreExpandedDirs(projection.stagedTree, expansion.staged)
	restoreExpandedDirs(projection.unstagedTree, expansion.unstaged)
	projection.rowCache = buildTreeRowCache(projection.stagedTree, projection.unstagedTree, stagedCollapsed, unstagedCollapsed)
	return projection
}

func prepareTreeProjectionCmd(stagedTree, unstagedTree []*GitTreeNode, state *expansionState, stagedCollapsed, unstagedCollapsed bool, statusGeneration, sectionGeneration uint64) tea.Cmd {
	return func() tea.Msg {
		expansion := state.snapshot()
		projection := buildTreeProjection(stagedTree, unstagedTree, expansion, stagedCollapsed, unstagedCollapsed)
		return PreparedTreeProjectionMsg{
			StatusGeneration:    statusGeneration,
			ExpansionGeneration: expansion.generation,
			SectionGeneration:   sectionGeneration,
			projection:          projection,
		}
	}
}

func buildTreeProjection(stagedTree, unstagedTree []*GitTreeNode, expansion expansionSnapshot, stagedCollapsed, unstagedCollapsed bool) *treeProjection {
	projection := &treeProjection{
		stagedTree:   projectTreeExpansion(stagedTree, expansion.staged),
		unstagedTree: projectTreeExpansion(unstagedTree, expansion.unstaged),
	}
	projection.rowCache = buildTreeRowCache(projection.stagedTree, projection.unstagedTree, stagedCollapsed, unstagedCollapsed)
	return projection
}

// projectTreeExpansion applies desired directory visibility with structural
// sharing. It traverses outside Update, clones only changed nodes and their
// ancestors, and returns the original root slice when the snapshot is already
// represented by the tree.
func projectTreeExpansion(nodes []*GitTreeNode, expanded map[string]bool) []*GitTreeNode {
	projected, _ := projectTreeExpansionChanged(nodes, expanded)
	return projected
}

func projectTreeExpansionChanged(nodes []*GitTreeNode, expanded map[string]bool) ([]*GitTreeNode, bool) {
	var projected []*GitTreeNode
	for i, node := range nodes {
		if node == nil || !node.IsDir {
			continue
		}

		desired := true
		if value, ok := expanded[node.Path]; ok {
			desired = value
		}
		children, childrenChanged := projectTreeExpansionChanged(node.Children, expanded)
		if desired == node.Expanded && !childrenChanged {
			continue
		}

		if projected == nil {
			projected = append([]*GitTreeNode(nil), nodes...)
		}
		clone := *node
		clone.Expanded = desired
		clone.Children = children
		projected[i] = &clone
	}
	if projected == nil {
		return nodes, false
	}
	return projected, true
}
