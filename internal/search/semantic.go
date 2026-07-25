package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"teak/internal/toolpath"
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

// indexBuild tracks the subprocess lifetime of one workspace's `vecgrep
// init`/`vecgrep index` run, independent of any single caller's context.
type indexBuild struct {
	cancel context.CancelFunc
	done   chan struct{} // closed once the build's subprocess(es) have returned
}

// indexBuilds is the process-wide registry of in-flight index builds, keyed
// by rootDir. A search's per-query context (recreated on every keystroke,
// Tab, Ctrl+R, and cancelled on Escape) must never be the context that an
// index build's exec.CommandContext runs under: doing so kills `vecgrep
// index` the moment the user who happened to trigger it types again. Instead
// every build gets its own context here, so it survives query edits and mode
// switches and is only stopped by CancelIndexing (workspace/app teardown).
var indexBuilds = struct {
	mu    sync.Mutex
	byDir map[string]*indexBuild
}{byDir: make(map[string]*indexBuild)}

// beginIndexBuild registers a new index build for rootDir, returning a
// context scoped to the build's own lifetime and a finish function that must
// be called exactly once, when the build completes (success or failure), to
// release it from the registry and unblock WaitForIndexingShutdown.
func beginIndexBuild(rootDir string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	build := &indexBuild{cancel: cancel, done: make(chan struct{})}

	indexBuilds.mu.Lock()
	indexBuilds.byDir[rootDir] = build
	indexBuilds.mu.Unlock()

	finish := func() {
		indexBuilds.mu.Lock()
		if indexBuilds.byDir[rootDir] == build {
			delete(indexBuilds.byDir, rootDir)
		}
		indexBuilds.mu.Unlock()
		cancel()
		close(build.done)
	}
	return ctx, finish
}

// runIndexBuild runs setupVecgrepContext under a context scoped to the index
// build's own lifetime rather than the caller's. See indexBuilds.
func runIndexBuild(rootDir string) error {
	ctx, finish := beginIndexBuild(rootDir)
	defer finish()
	return setupVecgrepContext(ctx, rootDir)
}

// CancelIndexing cancels every in-flight vecgrep index build across all
// workspaces without waiting for their subprocesses to exit. It is safe to
// call multiple times and from any goroutine. Application shutdown should
// call this (mirroring lsp.Client.Shutdown) before the process exits so a
// long-running `vecgrep index` is not orphaned as an untracked child process.
func CancelIndexing() {
	indexBuilds.mu.Lock()
	builds := make([]*indexBuild, 0, len(indexBuilds.byDir))
	for _, b := range indexBuilds.byDir {
		builds = append(builds, b)
	}
	indexBuilds.mu.Unlock()

	for _, b := range builds {
		b.cancel()
	}
}

// WaitForIndexingShutdown blocks until every index build that was in flight
// at the time of the call has released its subprocess, or ctx is done. It
// mirrors lsp.Client.WaitForShutdown: callers should invoke CancelIndexing
// first, then bound this with a deadline before the process terminates.
func WaitForIndexingShutdown(ctx context.Context) bool {
	indexBuilds.mu.Lock()
	dones := make([]chan struct{}, 0, len(indexBuilds.byDir))
	for _, b := range indexBuilds.byDir {
		dones = append(dones, b.done)
	}
	indexBuilds.mu.Unlock()

	for _, done := range dones {
		select {
		case <-done:
		case <-ctx.Done():
			return false
		}
	}
	return true
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
	if _, err := toolpath.Resolve("vecgrep"); err != nil {
		return nil, fmt.Errorf("vecgrep not found: install it for semantic search")
	}

	// Ensure the project is initialized and indexed
	if err := ensureVecgrepReadyContext(ctx, rootDir); err != nil {
		return nil, fmt.Errorf("vecgrep setup failed: %w", err)
	}

	out, err := runVecgrep(ctx, rootDir, "search", query, "--format", "json-envelope", "--limit", "20")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, errSemanticOutputLimit) {
			return nil, err
		}
		// Fallback: try plain json for older versions
		out2, err2 := runVecgrep(ctx, rootDir, "search", query, "--format", "json", "--limit", "20")
		if err2 != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, fmt.Errorf("vecgrep search failed: %w", err)
		}
		return parseJSONOutput(out2)
	}

	return parseEnvelopeOutput(out)
}

func ensureVecgrepReadyContext(ctx context.Context, rootDir string) error {
	if _, ok := vecgrepReady.Load(rootDir); ok {
		return nil
	}
	return runSemanticSetupContext(ctx, rootDir, func(context.Context) error {
		// A concurrent caller may have completed setup before this leader got
		// scheduled, so recheck the ready cache inside the single-flight path.
		if _, ok := vecgrepReady.Load(rootDir); ok {
			return nil
		}
		// Deliberately ignore the leader's own ctx here: it is a per-query
		// context that search.go cancels and replaces on every keystroke,
		// Tab, Ctrl+R toggle, and Escape (see replaceSearchContext /
		// cancelSearch). If this particular caller happens to become the
		// single-flight leader for a first-time index build, its own edits
		// must not kill the `vecgrep index` subprocess out from under every
		// other caller waiting on the same flight. runIndexBuild gives the
		// build its own lifetime instead; see indexBuilds.
		return runIndexBuild(rootDir)
	})
}

func setupVecgrepContext(ctx context.Context, rootDir string) error {
	// Check current status. --format json is requested so isIndexedFromStatus
	// can read the structured freshness fields instead of guessing from
	// prose; see isIndexedFromStatus for why that matters.
	out, err := runVecgrep(ctx, rootDir, "status", "--format", "json")
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	if err != nil || !isIndexedFromStatus(out) {
		// Initialize the project
		initCmd, cmdErr := toolpath.Command(ctx, "vecgrep", "init", rootDir)
		if cmdErr != nil {
			return cmdErr
		}
		initCmd.Dir = rootDir
		if initErr := initCmd.Run(); initErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("vecgrep init failed: %w", initErr)
		}

		// Index the project
		indexCmd, cmdErr := toolpath.Command(ctx, "vecgrep", "index")
		if cmdErr != nil {
			return cmdErr
		}
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
	cmd, cmdErr := toolpath.Command(ctx, "vecgrep", args...)
	if cmdErr != nil {
		return nil, cmdErr
	}
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

// vecgrepStatusJSON mirrors the fields of `vecgrep status --format json`
// that determine whether the on-disk index still reflects the workspace's
// current files. index_fresh and pending_changes are vecgrep's own drift
// tracking, computed from file hashes/mtimes it has already checked, so
// trusting them is both more correct and cheaper than teak recomputing
// staleness itself.
type vecgrepStatusJSON struct {
	IndexFresh bool `json:"index_fresh"`
	Stats      struct {
		Files int `json:"files"`
	} `json:"stats"`
	PendingChanges struct {
		TotalPending int `json:"total_pending"`
	} `json:"pending_changes"`
}

// isIndexedFromStatus decides whether `vecgrep index` needs to run again,
// preferring the structured `status --format json` fields and falling back
// to prose matching (isIndexed) for older vecgrep releases that don't
// support --format json for status.
func isIndexedFromStatus(statusOutput []byte) bool {
	var st vecgrepStatusJSON
	if err := json.Unmarshal(statusOutput, &st); err == nil {
		return st.IndexFresh && st.Stats.Files > 0 && st.PendingChanges.TotalPending == 0
	}
	return isIndexed(string(statusOutput))
}

// isIndexed checks vecgrep's default (non-JSON) status output to determine
// whether the project is indexed. It is a fallback for vecgrep releases
// whose `status` command doesn't support --format json; prefer
// isIndexedFromStatus, which reads the structured freshness fields.
//
// "stale" is checked first and unconditionally returns false: vecgrep's
// default-format status output always prints an "Index statistics:" block
// with a "Files:" line whether or not the index is current, and a project
// that has drifted (a file changed since the last `vecgrep index`) still
// says "Reindex status: Freshness: stale (...)" further down. An earlier
// version of this function checked "files:"/"indexed" first, which matched
// unconditionally and made InvalidateSemanticIndex (called on every save,
// external file change, and tree change) a no-op in practice: the status
// check always reported "already indexed" and the stale index was never
// rebuilt.
func isIndexed(statusOutput string) bool {
	s := strings.ToLower(statusOutput)
	if strings.Contains(s, "stale") {
		return false
	}
	// If status mentions it's not initialized or has no index, it's not ready
	if strings.Contains(s, "not initialized") || strings.Contains(s, "no index") ||
		strings.Contains(s, "not found") || strings.Contains(s, "not in a vecgrep project") {
		return false
	}
	// If status contains indicators of a healthy index, consider it ready
	if strings.Contains(s, "fresh") || strings.Contains(s, "indexed") || strings.Contains(s, "files:") || strings.Contains(s, "ready") {
		return true
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
	EndLine      int     `json:"end_line"`
	Col          int     `json:"col"`
	Preview      string  `json:"preview"`
	Score        float64 `json:"score"`
	Text         string  `json:"text"`
	Content      string  `json:"content"`
	SymbolName   string  `json:"symbol_name"`
	ChunkType    string  `json:"chunk_type"`
}

// vecgrepEnvelope is the json-envelope output format.
type vecgrepEnvelope struct {
	SchemaVersion int `json:"schema_version"`
	Index         struct {
		Indexed bool `json:"indexed"`
		Fresh   bool `json:"fresh"`
		Chunks  int  `json:"chunks"`
	} `json:"index"`
	Hits     []vecgrepResult `json:"hits"`
	Warnings []string        `json:"warnings"`
}

func parseEnvelopeOutput(data []byte) ([]Result, error) {
	var envelope vecgrepEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		// Fall back to plain JSON array parsing
		return parseJSONOutput(data)
	}
	return mapVecgrepResults(envelope.Hits), nil
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
		out = append(out, mapVecgrepResult(r))
	}
	return out, nil
}

func mapVecgrepResults(results []vecgrepResult) []Result {
	var out []Result
	for _, r := range results {
		out = append(out, mapVecgrepResult(r))
	}
	return out
}

func mapVecgrepResult(r vecgrepResult) Result {
	filePath := r.RelativePath
	if filePath == "" {
		filePath = r.FilePath
	}
	if filePath == "" {
		filePath = r.File
	}

	line := r.StartLine
	if line == 0 && r.Line > 0 {
		line = r.Line
	}

	preview := r.Preview
	if preview == "" && r.Content != "" {
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

	return Result{
		FilePath:   filePath,
		Line:       line,
		Col:        r.Col,
		Preview:    strings.TrimSpace(preview),
		Score:      r.Score,
		SymbolName: r.SymbolName,
		ChunkType:  r.ChunkType,
		EndLine:    r.EndLine,
	}
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
