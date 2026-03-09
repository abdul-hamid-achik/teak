package git

import (
	"fmt"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Message types for git async operations.

// CommitResultMsg is sent after a git commit attempt.
type CommitResultMsg struct {
	Err error
	Out string
}

// PushResultMsg is sent after a git push attempt.
type PushResultMsg struct {
	Err error
	Out string
}

// PullResultMsg is sent after a git pull attempt.
type PullResultMsg struct {
	Err error
	Out string
}

// BranchListMsg is sent with the list of branches.
type BranchListMsg struct {
	Branches []string
	Current  string
	Err      error
}

// SwitchBranchMsg requests switching to a branch.
type SwitchBranchMsg struct {
	Branch string
}

// SwitchBranchResultMsg is sent after a branch switch attempt.
type SwitchBranchResultMsg struct {
	Branch string
	Err    error
}

// OpenBranchPickerMsg requests opening the branch picker modal.
type OpenBranchPickerMsg struct{}

// StageCmd stages a file.
func StageCmd(rootDir, path string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "add", "--", path)
		cmd.Dir = rootDir
		if err := cmd.Run(); err != nil {
			return RefreshMsg{Err: fmt.Errorf("stage %s: %w", path, err)}
		}
		return refreshAfter(rootDir)
	}
}

// StageAllCmd stages all changes.
func StageAllCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "add", "-A")
		cmd.Dir = rootDir
		if err := cmd.Run(); err != nil {
			return RefreshMsg{Err: fmt.Errorf("stage all: %w", err)}
		}
		return refreshAfter(rootDir)
	}
}

// UnstageCmd unstages a file.
func UnstageCmd(rootDir, path string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "reset", "HEAD", "--", path)
		cmd.Dir = rootDir
		if err := cmd.Run(); err != nil {
			return RefreshMsg{Err: fmt.Errorf("unstage %s: %w", path, err)}
		}
		return refreshAfter(rootDir)
	}
}

// UnstageAllCmd unstages all staged changes.
func UnstageAllCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "reset", "HEAD")
		cmd.Dir = rootDir
		if err := cmd.Run(); err != nil {
			return RefreshMsg{Err: fmt.Errorf("unstage all: %w", err)}
		}
		return refreshAfter(rootDir)
	}
}

// InitCmd initializes a git repository.
func InitCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "init")
		cmd.Dir = rootDir
		if err := cmd.Run(); err != nil {
			return RefreshMsg{Err: fmt.Errorf("git init: %w", err)}
		}
		// Return a refresh message to update the git panel
		return RefreshMsg{Branch: "", Entries: []StatusEntry{}}
	}
}

// CommitCmd creates a git commit with the given message.
func CommitCmd(rootDir, message string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "commit", "-m", message)
		cmd.Dir = rootDir
		out, err := cmd.CombinedOutput()
		return CommitResultMsg{Err: err, Out: strings.TrimSpace(string(out))}
	}
}

// PushCmd pushes to the remote.
func PushCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "push")
		cmd.Dir = rootDir
		out, err := cmd.CombinedOutput()
		return PushResultMsg{Err: err, Out: strings.TrimSpace(string(out))}
	}
}

// PullCmd pulls from the remote.
func PullCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "pull")
		cmd.Dir = rootDir
		out, err := cmd.CombinedOutput()
		return PullResultMsg{Err: err, Out: strings.TrimSpace(string(out))}
	}
}

// ListBranchesCmd lists all branches.
func ListBranchesCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		// Get current branch
		current := ""
		curCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
		curCmd.Dir = rootDir
		if out, err := curCmd.Output(); err == nil {
			current = strings.TrimSpace(string(out))
		}

		// List all branches
		cmd := exec.Command("git", "branch", "-a", "--format=%(refname:short)")
		cmd.Dir = rootDir
		out, err := cmd.Output()
		if err != nil {
			return BranchListMsg{Err: err}
		}

		var branches []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				branches = append(branches, line)
			}
		}
		return BranchListMsg{Branches: branches, Current: current}
	}
}

// SwitchBranchCmd switches to the given branch.
func SwitchBranchCmd(rootDir, branch string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "switch", branch)
		cmd.Dir = rootDir
		if err := cmd.Run(); err != nil {
			return SwitchBranchResultMsg{Branch: branch, Err: err}
		}
		return SwitchBranchResultMsg{Branch: branch}
	}
}

// ParseStatusLines parses the output of `git status --porcelain` into StatusEntry values.
// Each line has format "XY path" where X=index status, Y=working-tree status,
// position 2 is a space, and position 3+ is the path.
func ParseStatusLines(raw string) []StatusEntry {
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	var entries []StatusEntry
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}
		// Position 0: index status (X), position 1: work-tree status (Y),
		// position 2: space separator, position 3+: path.
		// Do NOT TrimSpace the path — it could mangle filenames with leading/trailing spaces.
		path := line[3:]
		if path == "" {
			continue
		}
		isDir := strings.HasSuffix(path, "/")
		entries = append(entries, StatusEntry{
			IndexStatus: line[0],
			WorkStatus:  line[1],
			Path:        strings.TrimRight(path, "/"),
			IsDir:       isDir,
		})
	}
	return entries
}

// refreshAfter runs a git status refresh and returns the result.
func refreshAfter(rootDir string) RefreshMsg {
	branch := ""
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = rootDir
	if out, err := branchCmd.Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}

	var entries []StatusEntry
	statusCmd := exec.Command("git", "status", "--porcelain", "-uall")
	statusCmd.Dir = rootDir
	if out, err := statusCmd.Output(); err == nil {
		entries = ParseStatusLines(strings.TrimRight(string(out), "\n"))
	}

	return RefreshMsg{Branch: branch, Entries: entries}
}
