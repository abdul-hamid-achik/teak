package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"teak/internal/toolpath"
)

const (
	commandTimeout = 30 * time.Second
	maxOutputBytes = 4 << 20
)

// Available returns true if the fcheap binary can be resolved.
func Available() bool {
	return toolpath.Available("fcheap")
}

// StashResult is the JSON output of `fcheap save --json`.
type StashResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Files  int    `json:"files"`
}

// StashEntry is a single stash in the list output.
type StashEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Created string `json:"created"`
	Files   int    `json:"files"`
	Tool    string `json:"tool"`
}

// StashFile snapshots a file or directory to the vault.
func StashFile(ctx context.Context, path string) (*StashResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := runFcheap(ctx, "save", path, "--json", "--tool", "teak", "--tag", "teak")
	if err != nil {
		return nil, fmt.Errorf("fcheap save failed: %w", err)
	}

	var result StashResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse fcheap output: %w", err)
	}
	return &result, nil
}

// ListStashes returns recent stashes created by teak.
func ListStashes(ctx context.Context, limit int) ([]StashEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := runFcheap(ctx, "list", "--tool", "teak", "--json", "--limit", fmt.Sprintf("%d", limit))
	if err != nil {
		return nil, fmt.Errorf("fcheap list failed: %w", err)
	}

	var entries []StashEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parse fcheap list: %w", err)
	}
	return entries, nil
}

// RestoreStash extracts a stash to a temporary directory and returns the path.
func RestoreStash(ctx context.Context, id string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	out, err := runFcheap(ctx, "restore", id, "--json")
	if err != nil {
		return "", fmt.Errorf("fcheap restore failed: %w", err)
	}

	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse fcheap restore: %w", err)
	}
	return result.Path, nil
}

func runFcheap(ctx context.Context, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd, err := toolpath.Command(ctx, "fcheap", args...)
	if err != nil {
		return nil, err
	}
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
