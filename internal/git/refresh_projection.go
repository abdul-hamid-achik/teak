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
