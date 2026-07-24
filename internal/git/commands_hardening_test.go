package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStageCmdSafelyStagesDashPrefixedPath(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	path := "-notes.txt"
	if err := os.WriteFile(filepath.Join(repo, path), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}

	msg := StageCmd(repo, path)()
	refresh, ok := msg.(RefreshMsg)
	if !ok {
		t.Fatalf("StageCmd message = %T, want RefreshMsg", msg)
	}
	if refresh.Err != nil {
		t.Fatalf("StageCmd refresh error: %v", refresh.Err)
	}
	if len(refresh.Entries) != 1 || refresh.Entries[0].Path != path || !refresh.Entries[0].IsStagedChange() {
		t.Fatalf("staged entries = %+v, want staged %q", refresh.Entries, path)
	}
}

func TestSwitchBranchCmdSwitchesValidatedBranch(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "teak@example.test")
	runGit(t, repo, "config", "user.name", "Teak Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "branch", "next")

	msg := SwitchBranchCmd(repo, "next")()
	result, ok := msg.(SwitchBranchResultMsg)
	if !ok {
		t.Fatalf("SwitchBranchCmd message = %T, want SwitchBranchResultMsg", msg)
	}
	if result.Err != nil {
		t.Fatalf("SwitchBranchCmd error: %v", result.Err)
	}
}

func TestSwitchBranchCmdRejectsOptionLikeBranch(t *testing.T) {
	result, ok := SwitchBranchCmd(t.TempDir(), "--discard-changes")().(SwitchBranchResultMsg)
	if !ok {
		t.Fatal("SwitchBranchCmd did not return SwitchBranchResultMsg")
	}
	if result.Err == nil {
		t.Fatal("SwitchBranchCmd accepted an option-like branch")
	}
}

func TestCommitCmdAcceptsOptionLikeCommitMessage(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "teak@example.test")
	runGit(t, repo, "config", "user.name", "Teak Test")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "file.txt")

	msg := CommitCmd(repo, "-a message, not an option")()
	result, ok := msg.(CommitResultMsg)
	if !ok {
		t.Fatalf("CommitCmd message = %T, want CommitResultMsg", msg)
	}
	if result.Err != nil {
		t.Fatalf("CommitCmd error: %v; output: %s", result.Err, result.Out)
	}
}

func TestNewGitCommandIsNonInteractiveAndPreservesArgumentBoundary(t *testing.T) {
	cmd := newGitCommand(context.Background(), t.TempDir(), "switch", "--", "-not-an-option")
	// argv[0] is the resolved git path, so compare its basename; the argument
	// boundary after it is what this test actually guards.
	if len(cmd.Args) == 0 {
		t.Fatal("newGitCommand produced no args")
	}
	if base := filepath.Base(cmd.Args[0]); base != "git" && base != "git.exe" {
		t.Errorf("args[0] basename = %q, want git", base)
	}
	if got, want := cmd.Args[1:], []string{"-c", "credential.interactive=never", "switch", "--", "-not-an-option"}; strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args[1:] = %#v, want %#v", got, want)
	}

	env := strings.Join(cmd.Env, "\n")
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "GIT_PAGER=cat", "PAGER=cat"} {
		if !strings.Contains(env, want) {
			t.Errorf("environment does not contain %q", want)
		}
	}
}

func TestGitEnvironmentOverridesInteractiveVariablesRatherThanAppendingDuplicates(t *testing.T) {
	env := gitEnvironment([]string{"GIT_TERMINAL_PROMPT=1", "PAGER=less", "KEEP=value"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "GIT_TERMINAL_PROMPT=1") || strings.Contains(joined, "PAGER=less") {
		t.Fatalf("interactive environment was retained: %q", joined)
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "PAGER=cat", "KEEP=value"} {
		if !strings.Contains(joined, want) {
			t.Errorf("environment does not contain %q", want)
		}
	}
}

func TestRunGitOutputStopsAtOutputLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "printf '1234567890'")
	_, err := runCommandOutput(cmd, 4)
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("runCommandOutput error = %v, want ErrOutputLimit", err)
	}
}

func TestRunGitOutputReportsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 1")
	_, err := runCommandOutput(cmd, 1024)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runCommandOutput error = %v, want context.Canceled", err)
	}
}
