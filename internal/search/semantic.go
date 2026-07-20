package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

const (
	maxSemanticOutputBytes = 4 << 20
	maxSemanticErrorBytes  = 64 << 10
)

var errSemanticOutputLimit = errors.New("vecgrep output exceeds limit")

// vecgrepReady caches per-rootDir whether vecgrep has been initialized+indexed.
var vecgrepReady sync.Map

type semanticSetupFlight struct {
	done chan struct{}
	err  error
}

var semanticSetupFlights = struct {
	mu      sync.Mutex
	flights map[string]*semanticSetupFlight
}{
	flights: make(map[string]*semanticSetupFlight),
}

// runSemanticSetup ensures that exactly one indexing setup can run per
// workspace. Concurrent semantic searches wait for the leader and receive the
// same outcome instead of starting competing vecgrep init/index processes.
func runSemanticSetup(rootDir string, setup func() error) error {
	return runSemanticSetupContext(context.Background(), rootDir, func(context.Context) error {
		return setup()
	})
}

// runSemanticSetupContext is the cancellable form of runSemanticSetup. A
// canceled waiter returns immediately instead of remaining tied to another
// query's indexing process. If an older leader is canceled while the current
// caller is still live, the current caller retries as the next leader.
func runSemanticSetupContext(ctx context.Context, rootDir string, setup func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		semanticSetupFlights.mu.Lock()
		if flight, ok := semanticSetupFlights.flights[rootDir]; ok {
			semanticSetupFlights.mu.Unlock()
			select {
			case <-flight.done:
				if flight.err != nil &&
					(errors.Is(flight.err, context.Canceled) || errors.Is(flight.err, context.DeadlineExceeded)) &&
					ctx.Err() == nil {
					continue
				}
				return flight.err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		flight := &semanticSetupFlight{done: make(chan struct{})}
		semanticSetupFlights.flights[rootDir] = flight
		semanticSetupFlights.mu.Unlock()

		err := setup(ctx)

		semanticSetupFlights.mu.Lock()
		flight.err = err
		delete(semanticSetupFlights.flights, rootDir)
		close(flight.done)
		semanticSetupFlights.mu.Unlock()
		return err
	}
}

// InvalidateSemanticIndex clears the cached ready state for a workspace so the
// next semantic search rechecks and reindexes vecgrep as needed.
func InvalidateSemanticIndex(rootDir string) {
	if rootDir == "" {
		return
	}
	vecgrepReady.Delete(rootDir)
}

// SemanticSearch performs a semantic code search using vecgrep.
func SemanticSearch(rootDir, query string) ([]Result, error) {
	return SemanticSearchContext(context.Background(), rootDir, query)
}

// SemanticSearchContext performs a cancellable semantic code search.
func SemanticSearchContext(ctx context.Context, rootDir, query string) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, err := exec.LookPath("vecgrep")
	if err != nil {
		return nil, fmt.Errorf("vecgrep not found: install it for semantic search")
	}

	// Ensure the project is initialized and indexed
	if err := ensureVecgrepReadyContext(ctx, rootDir); err != nil {
		return nil, fmt.Errorf("vecgrep setup failed: %w", err)
	}

	out, err := runVecgrep(ctx, rootDir, "search", query, "--format", "json", "--limit", "20")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, errSemanticOutputLimit) {
			return nil, err
		}
		// Try without --format flag for older versions
		out2, err2 := runVecgrep(ctx, rootDir, "search", query)
		if err2 != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("vecgrep search failed: %w", err)
		}
		return parsePlainOutput(out2), nil
	}

	return parseJSONOutput(out)
}

func ensureVecgrepReadyContext(ctx context.Context, rootDir string) error {
	if _, ok := vecgrepReady.Load(rootDir); ok {
		return nil
	}
	return runSemanticSetupContext(ctx, rootDir, func(ctx context.Context) error {
		// A concurrent caller may have completed setup before this leader got
		// scheduled, so recheck the ready cache inside the single-flight path.
		if _, ok := vecgrepReady.Load(rootDir); ok {
			return nil
		}
		return setupVecgrepContext(ctx, rootDir)
	})
}

func setupVecgrepContext(ctx context.Context, rootDir string) error {
	// Check current status
	out, err := runVecgrep(ctx, rootDir, "status")
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	if err != nil || !isIndexed(string(out)) {
		// Initialize the project
		initCmd := exec.CommandContext(ctx, "vecgrep", "init", rootDir)
		initCmd.Dir = rootDir
		if initErr := initCmd.Run(); initErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("vecgrep init failed: %w", initErr)
		}

		// Index the project
		indexCmd := exec.CommandContext(ctx, "vecgrep", "index")
		indexCmd.Dir = rootDir
		if indexErr := indexCmd.Run(); indexErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("vecgrep index failed: %w", indexErr)
		}
	}

	vecgrepReady.Store(rootDir, true)
	return nil
}

type boundedCommandBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedCommandBuffer) Write(p []byte) (int, error) {
	if b.limit <= b.Len() {
		b.exceeded = true
		return 0, errSemanticOutputLimit
	}
	remaining := b.limit - b.Len()
	if len(p) <= remaining {
		return b.Buffer.Write(p)
	}
	n, _ := b.Buffer.Write(p[:remaining])
	b.exceeded = true
	return n, errSemanticOutputLimit
}

func runVecgrep(ctx context.Context, rootDir string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	stdout := &boundedCommandBuffer{limit: maxSemanticOutputBytes}
	stderr := &boundedCommandBuffer{limit: maxSemanticErrorBytes}
	cmd := exec.CommandContext(ctx, "vecgrep", args...)
	cmd.Dir = rootDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, errSemanticOutputLimit
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// isIndexed checks vecgrep status output to determine if the project is indexed.
func isIndexed(statusOutput string) bool {
	s := strings.ToLower(statusOutput)
	// If status contains indicators of a healthy index, consider it ready
	if strings.Contains(s, "indexed") || strings.Contains(s, "files:") || strings.Contains(s, "ready") {
		return true
	}
	// If status mentions it's not initialized or has no index, it's not ready
	if strings.Contains(s, "not initialized") || strings.Contains(s, "no index") || strings.Contains(s, "not found") {
		return false
	}
	// If we got valid output, assume it's okay
	return len(strings.TrimSpace(statusOutput)) > 0
}

type vecgrepResult struct {
	File         string  `json:"file"`
	FilePath     string  `json:"file_path"`
	RelativePath string  `json:"relative_path"`
	Line         int     `json:"line"`
	StartLine    int     `json:"start_line"`
	Col          int     `json:"col"`
	Preview      string  `json:"preview"`
	Score        float64 `json:"score"`
	Text         string  `json:"text"`
	Content      string  `json:"content"`
}

func parseJSONOutput(data []byte) ([]Result, error) {
	// Try array of results
	var results []vecgrepResult
	if err := json.Unmarshal(data, &results); err != nil {
		// Try line-delimited JSON
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		for _, line := range lines {
			var r vecgrepResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				continue
			}
			results = append(results, r)
		}
	}

	var out []Result
	for _, r := range results {
		// Resolve file path: prefer relative_path > file_path > file
		filePath := r.RelativePath
		if filePath == "" {
			filePath = r.FilePath
		}
		if filePath == "" {
			filePath = r.File
		}

		// Resolve line number: prefer start_line > line
		line := r.StartLine
		if line == 0 && r.Line > 0 {
			line = r.Line
		}

		// Resolve preview: prefer preview > first line of content > text
		preview := r.Preview
		if preview == "" && r.Content != "" {
			// Use first non-empty line of content as preview
			for _, l := range strings.SplitN(r.Content, "\n", 5) {
				trimmed := strings.TrimSpace(l)
				if trimmed != "" {
					preview = trimmed
					break
				}
			}
		}
		if preview == "" {
			preview = r.Text
		}

		out = append(out, Result{
			FilePath: filePath,
			Line:     line,
			Col:      r.Col,
			Preview:  strings.TrimSpace(preview),
			Score:    r.Score,
		})
	}
	return out, nil
}

func parsePlainOutput(data []byte) []Result {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var results []Result
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Try to parse "file:line: text" format
		parts := strings.SplitN(line, ":", 3)
		if len(parts) >= 2 {
			lineNum := 0
			if _, err := fmt.Sscanf(parts[1], "%d", &lineNum); err != nil {
				lineNum = 0
			}
			preview := ""
			if len(parts) >= 3 {
				preview = strings.TrimSpace(parts[2])
			}
			results = append(results, Result{
				FilePath: parts[0],
				Line:     lineNum,
				Preview:  preview,
			})
		}
	}
	return results
}
