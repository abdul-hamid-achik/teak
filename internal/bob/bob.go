package bob

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"teak/internal/toolpath"
)

const (
	commandTimeout = 15 * time.Second
	maxOutputBytes = 4 << 20
)

// Available returns true if the bob binary can be resolved.
func Available() bool {
	return toolpath.Available("bob")
}

// PlanAction represents a single file action in a bob plan.
type PlanAction struct {
	Path   string `json:"path"`
	Action string `json:"action"` // create, adopt, unchanged, update, conflict
	Seed   bool   `json:"seed"`
}

// PlanResult is the output of `bob plan --json`.
type PlanResult struct {
	Actions []PlanAction `json:"actions"`
	Digest  string       `json:"digest"`
}

// CheckResult is the output of `bob check --json`.
type CheckResult struct {
	OK        bool     `json:"ok"`
	Drifted   []string `json:"drifted"`
	Conflicts []string `json:"conflicts"`
}

// Plan returns the current bob plan for the workspace.
func Plan(ctx context.Context, rootDir string) (*PlanResult, error) {
	out, err := run(ctx, rootDir, "plan", "--json")
	if err != nil {
		return nil, err
	}
	var result PlanResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse bob plan: %w", err)
	}
	return &result, nil
}

// Check runs the drift check.
func Check(ctx context.Context, rootDir string) (*CheckResult, error) {
	out, err := run(ctx, rootDir, "check", "--json")
	if err != nil {
		// Exit code 3 = drift detected, still parse output
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
			var result CheckResult
			if json.Unmarshal(out, &result) == nil {
				return &result, nil
			}
		}
		return nil, err
	}
	var result CheckResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse bob check: %w", err)
	}
	return &result, nil
}

// HasManifest returns true if a bob.yaml exists in the workspace.
func HasManifest(rootDir string) bool {
	out, err := run(context.Background(), rootDir, "path", "bob.yaml", "--json")
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "bob.yaml")
}

func run(ctx context.Context, rootDir string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "bob", args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = rootDir
	out, stderr, err := toolpath.RunBounded(cmd, maxOutputBytes, maxOutputBytes)
	if err != nil {
		detail := strings.TrimSpace(string(stderr))
		if detail != "" {
			return out, fmt.Errorf("%w: %s", err, detail)
		}
		return out, err
	}
	return out, nil
}
