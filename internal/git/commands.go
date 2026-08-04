package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"teak/internal/toolpath"
)

const (
	gitProbeTimeout   = 2 * time.Second
	gitReadTimeout    = 10 * time.Second
	gitMutateTimeout  = 30 * time.Second
	gitNetworkTimeout = 90 * time.Second
	gitOutputLimit    = 4 << 20 // 4 MiB prevents a hostile repository from exhausting the UI process.
)

// ErrOutputLimit means a Git command produced more output than Teak is willing
// to retain. The command's pipes are closed immediately, which also prevents
// an unbounded producer from keeping a Bubble Tea command alive indefinitely.
var ErrOutputLimit = errors.New("git command output exceeds limit")

// newGitCommand consistently makes Git commands bounded and non-interactive.
// In particular, commands issued from a TUI must never open a credential,
// pager, or editor prompt behind the user's terminal. Resolution errors are
// returned instead of falling back to a bare executable name, so a missing or
// configured Git binary cannot accidentally be selected from PATH.
func newGitCommand(ctx context.Context, rootDir string, args ...string) (*exec.Cmd, error) {
	commandArgs := append([]string{"-c", "credential.interactive=never"}, args...)
	cmd, err := toolpath.Command(ctx, "git", commandArgs...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = rootDir
	cmd.WaitDelay = time.Second
	cmd.Env = gitEnvironment(os.Environ())
	return cmd, nil
}

func gitEnvironment(base []string) []string {
	overrides := map[string]string{
		"GIT_TERMINAL_PROMPT": "GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE":     "GCM_INTERACTIVE=Never",
		"GIT_PAGER":           "GIT_PAGER=cat",
		"PAGER":               "PAGER=cat",
	}
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[key]; !overridden {
			env = append(env, entry)
		}
	}
	for _, entry := range overrides {
		env = append(env, entry)
	}
	return env
}

type limitedOutput struct {
	mu       sync.Mutex
	data     []byte
	limit    int64
	overflow bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.overflow {
		return 0, ErrOutputLimit
	}
	remaining := w.limit - int64(len(w.data))
	if remaining <= 0 || int64(len(p)) > remaining {
		if remaining > 0 {
			w.data = append(w.data, p[:remaining]...)
		}
		w.overflow = true
		return 0, ErrOutputLimit
	}
	w.data = append(w.data, p...)
	return len(p), nil
}

func (w *limitedOutput) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.data...)
}

func (w *limitedOutput) Overflowed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overflow
}

// runCommandOutput captures stdout and stderr with one shared, synchronized
// upper bound. os/exec copies both streams concurrently.
func runCommandOutput(cmd *exec.Cmd, limit int64) ([]byte, error) {
	out := &limitedOutput{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	if out.Overflowed() {
		return out.Bytes(), ErrOutputLimit
	}
	return out.Bytes(), err
}

func runGitCommand(ctx context.Context, rootDir string, limit int64, args ...string) ([]byte, error) {
	cmd, err := newGitCommand(ctx, rootDir, args...)
	if err != nil {
		return nil, err
	}
	return runCommandOutput(cmd, limit)
}

func runGitCommandTimeout(rootDir string, timeout time.Duration, limit int64, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := runGitCommand(ctx, rootDir, limit, args...)
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

// CurrentBranch returns the checked-out branch using the same bounded,
// non-interactive policy as UI commands. It is useful to callers that need a
// best-effort value outside the panel refresh flow.
func CurrentBranch(rootDir string) string {
	out, err := runGitCommandTimeout(rootDir, gitReadTimeout, 64<<10, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// StatusSnapshot is the bounded, read-only repository state consumed by
// headless tooling and the interactive panel. Entries retain Git's two status
// columns so callers can distinguish staged work from working-tree changes.
type StatusSnapshot struct {
	Branch  string
	Entries []StatusEntry
}

// StatusContext returns the current branch and porcelain status for rootDir.
// It never mutates the repository and preserves NUL-delimited paths, including
// names containing spaces, quotes, Unicode, or backslashes.
func StatusContext(ctx context.Context, rootDir string) (StatusSnapshot, error) {
	ctx, cancel := withGitTimeout(ctx, gitReadTimeout)
	defer cancel()
	branchOut, err := runGitCommand(ctx, rootDir, 64<<10, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		if ctx.Err() != nil {
			return StatusSnapshot{}, ctx.Err()
		}
		return StatusSnapshot{}, fmt.Errorf("detect git branch: %w", err)
	}
	entries, err := readStatusEntriesContext(ctx, rootDir)
	if err != nil {
		return StatusSnapshot{}, fmt.Errorf("read git status: %w", err)
	}
	return StatusSnapshot{Branch: strings.TrimSpace(string(branchOut)), Entries: entries}, nil
}

func withGitTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// DiffOutput returns a bounded, non-interactive diff for one repository
// relative path. Callers own ctx, so closing a diff tab can cancel its work.
func DiffOutput(ctx context.Context, rootDir, relPath string) ([]byte, error) {
	ctx, cancel := withGitTimeout(ctx, gitReadTimeout)
	defer cancel()
	out, err := runGitCommand(ctx, rootDir, gitOutputLimit, "diff", "--no-ext-diff", "HEAD", "--", relPath)
	if err == nil || errors.Is(err, ErrOutputLimit) || ctx.Err() != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, err
	}
	return runGitCommand(ctx, rootDir, gitOutputLimit, "diff", "--no-ext-diff", "--", relPath)
}

// RepositoryDetectedMsg reports the asynchronous repository probe performed
// during application initialization.
type RepositoryDetectedMsg struct {
	IsRepository bool
}

// DetectRepositoryCmd keeps process creation out of NewModel. A short timeout
// prevents an unusual git configuration or network filesystem from delaying
// the first rendered frame indefinitely.
func DetectRepositoryCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		if rootDir == "" {
			return RepositoryDetectedMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), gitProbeTimeout)
		defer cancel()
		_, err := runGitCommand(ctx, rootDir, 64<<10, "rev-parse", "--is-inside-work-tree")
		return RepositoryDetectedMsg{IsRepository: err == nil}
	}
}

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
		if _, err := runGitCommandTimeout(rootDir, gitMutateTimeout, gitOutputLimit, "add", "--", path); err != nil {
			return RefreshMsg{Err: fmt.Errorf("stage %s: %w", path, err)}
		}
		return refreshAfter(rootDir)
	}
}

// StageAllCmd stages all changes.
func StageAllCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		if _, err := runGitCommandTimeout(rootDir, gitMutateTimeout, gitOutputLimit, "add", "-A", "--"); err != nil {
			return RefreshMsg{Err: fmt.Errorf("stage all: %w", err)}
		}
		return refreshAfter(rootDir)
	}
}

// UnstageCmd unstages a file.
func UnstageCmd(rootDir, path string) tea.Cmd {
	return func() tea.Msg {
		if _, err := runGitCommandTimeout(rootDir, gitMutateTimeout, gitOutputLimit, "reset", "HEAD", "--", path); err != nil {
			return RefreshMsg{Err: fmt.Errorf("unstage %s: %w", path, err)}
		}
		return refreshAfter(rootDir)
	}
}

// UnstageAllCmd unstages all staged changes.
func UnstageAllCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		if _, err := runGitCommandTimeout(rootDir, gitMutateTimeout, gitOutputLimit, "reset", "HEAD", "--"); err != nil {
			return RefreshMsg{Err: fmt.Errorf("unstage all: %w", err)}
		}
		return refreshAfter(rootDir)
	}
}

// InitCmd initializes a git repository.
func InitCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		if _, err := runGitCommandTimeout(rootDir, gitMutateTimeout, gitOutputLimit, "init", "--"); err != nil {
			return RefreshMsg{Err: fmt.Errorf("git init: %w", err)}
		}
		// Return a refresh message to update the git panel
		return RefreshMsg{Branch: "", Entries: []StatusEntry{}}
	}
}

// CommitCmd creates a git commit with the given message.
func CommitCmd(rootDir, message string) tea.Cmd {
	return func() tea.Msg {
		out, err := runGitCommandTimeout(rootDir, gitMutateTimeout, gitOutputLimit, "commit", "-m", message, "--")
		return CommitResultMsg{Err: err, Out: strings.TrimSpace(string(out))}
	}
}

// PushCmd pushes to the remote.
func PushCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		out, err := runGitCommandTimeout(rootDir, gitNetworkTimeout, gitOutputLimit, "push")
		return PushResultMsg{Err: err, Out: strings.TrimSpace(string(out))}
	}
}

// PullCmd pulls from the remote.
func PullCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		out, err := runGitCommandTimeout(rootDir, gitNetworkTimeout, gitOutputLimit, "pull", "--no-edit")
		return PullResultMsg{Err: err, Out: strings.TrimSpace(string(out))}
	}
}

// ListBranchesCmd lists all branches.
func ListBranchesCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		// Get current branch
		current := ""
		if out, err := runGitCommandTimeout(rootDir, gitReadTimeout, gitOutputLimit, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			current = strings.TrimSpace(string(out))
		}

		// List all branches
		out, err := runGitCommandTimeout(rootDir, gitReadTimeout, gitOutputLimit, "branch", "-a", "--format=%(refname:short)")
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
		// git switch does not accept a pathspec separator. Git itself forbids
		// branch names beginning with '-', and rejecting one here makes the
		// command safe even if a synthetic UI message bypasses the picker.
		if branch == "" || strings.HasPrefix(branch, "-") {
			return SwitchBranchResultMsg{Branch: branch, Err: fmt.Errorf("invalid branch name %q", branch)}
		}
		if _, err := runGitCommandTimeout(rootDir, gitMutateTimeout, gitOutputLimit, "switch", branch); err != nil {
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

// ParseStatusPorcelainZ parses `git status --porcelain=v1 -z` output.
// NUL-delimited porcelain output carries paths verbatim, including spaces,
// quotes, Unicode, and backslashes. Rename and copy records are followed by a
// second NUL-delimited source path; Path is the destination used by Git's
// stage, unstage, and diff commands and OriginalPath retains the source.
func ParseStatusPorcelainZ(raw []byte) []StatusEntry {
	var entries []StatusEntry
	for len(raw) > 0 {
		recordEnd := bytes.IndexByte(raw, 0)
		if recordEnd < 0 {
			break
		}
		record := raw[:recordEnd]
		raw = raw[recordEnd+1:]
		if len(record) < 4 || record[2] != ' ' {
			continue
		}

		entry := StatusEntry{
			IndexStatus: record[0],
			WorkStatus:  record[1],
			Path:        string(record[3:]),
		}
		if entry.Path == "" {
			continue
		}
		if isRenameOrCopy(entry.IndexStatus, entry.WorkStatus) {
			sourceEnd := bytes.IndexByte(raw, 0)
			if sourceEnd < 0 {
				break
			}
			entry.OriginalPath = string(raw[:sourceEnd])
			raw = raw[sourceEnd+1:]
			if entry.OriginalPath == "" {
				continue
			}
		}
		entry.IsDir = strings.HasSuffix(entry.Path, "/")
		entry.Path = strings.TrimRight(entry.Path, "/")
		entries = append(entries, entry)
	}
	return entries
}

func isRenameOrCopy(indexStatus, workStatus byte) bool {
	return indexStatus == 'R' || indexStatus == 'C' || workStatus == 'R' || workStatus == 'C'
}

func readStatusEntriesContext(ctx context.Context, rootDir string) ([]StatusEntry, error) {
	ctx, cancel := withGitTimeout(ctx, gitReadTimeout)
	defer cancel()
	out, err := runGitCommand(ctx, rootDir, gitOutputLimit, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return ParseStatusPorcelainZ(out), nil
}

// refreshAfter runs a git status refresh and returns the result.
func refreshAfter(rootDir string) RefreshMsg {
	ctx, cancel := context.WithTimeout(context.Background(), gitReadTimeout)
	defer cancel()
	return refreshAfterContext(ctx, rootDir)
}

func refreshAfterContext(ctx context.Context, rootDir string) RefreshMsg {
	ctx, cancel := withGitTimeout(ctx, gitReadTimeout)
	defer cancel()
	branch := ""
	if out, err := runGitCommand(ctx, rootDir, 64<<10, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	entries, err := readStatusEntriesContext(ctx, rootDir)
	return RefreshMsg{Branch: branch, Entries: entries, Err: err}
}
