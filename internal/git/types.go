package git

// GitSection identifies which section has focus within the git panel.
type GitSection int

const (
	SectionUnstaged GitSection = iota
	SectionStaged
	SectionCommitTitle
	SectionCommitBody
)

// StatusEntry represents a file with git status.
type StatusEntry struct {
	Path        string
	IndexStatus byte // X column from porcelain (staged state)
	WorkStatus  byte // Y column from porcelain (working tree state)
	IsDir       bool // true if this was a directory entry (trailing / in porcelain)
}

// IsStagedChange returns true if this entry has a staged change.
func (e StatusEntry) IsStagedChange() bool {
	return e.IndexStatus != ' ' && e.IndexStatus != '?'
}

// IsUnstagedChange returns true if this entry has an unstaged change.
func (e StatusEntry) IsUnstagedChange() bool {
	return e.WorkStatus != ' ' || e.IndexStatus == '?'
}

// IsUntracked returns true if this is an untracked file.
func (e StatusEntry) IsUntracked() bool {
	return e.IndexStatus == '?' && e.WorkStatus == '?'
}

// DisplayStatus returns a human-readable status character.
func (e StatusEntry) DisplayStatus(staged bool) string {
	if staged {
		return displayChar(e.IndexStatus)
	}
	if e.IsUntracked() {
		return "U"
	}
	return displayChar(e.WorkStatus)
}

func displayChar(b byte) string {
	switch b {
	case 'M':
		return "M"
	case 'A':
		return "A"
	case 'D':
		return "D"
	case 'R':
		return "R"
	case 'C':
		return "C"
	case '?':
		return "U"
	default:
		return string(b)
	}
}

// GitTreeNode represents a file or directory in the git changed-files tree.
type GitTreeNode struct {
	Name     string       // display name (just the basename)
	Path     string       // full relative path
	IsDir    bool         // true for directories
	Depth    int          // nesting depth
	Entry    *StatusEntry // nil for directories
	Staged   bool         // whether this entry is in staged section
	Children []*GitTreeNode
	Expanded bool
}
