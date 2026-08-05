package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"teak/internal/procmon"
	"teak/internal/toolpath"
)

const (
	maxSemanticOutputBytes = 4 << 20
	maxSemanticErrorBytes  = 64 << 10
	// Setup commands normally emit progress rather than query data. Keep the
	// retained stream small and terminate a noisy producer instead of allowing
	// an indexing tool to fill pipes or leave descendants running.
	maxSemanticSetupOutputBytes = 64 << 10
	// MaxSemanticSearchResults is the bound passed to vecgrep for each query.
	MaxSemanticSearchResults = 20
	// Interactive and explicit vecgrep indexing must remain bounded even when
	// the installed embedding/indexer implementation grows its resident set.
	// Large projects can opt into a larger finite value with
	// WithSemanticIndexRSSBudget.
	defaultSemanticIndexRSSBudget = uint64(512 << 20)
	// Semantic queries can load a large vector/index representation even when
	// they do not mutate it. Keep the query process group under the same
	// conservative default; callers may choose a larger finite budget.
	defaultSemanticQueryRSSBudget = uint64(512 << 20)
	// Legacy status can load the full vector graph in older vecgrep releases.
	// Keep its compatibility escape hatch finite and explicit, just like
	// codemap's FullStatus path.
	defaultVecgrepLegacyStatusRSSBudget = uint64(512 << 20)
)

// Interactive indexing must outlive one query context, but it must not be
// allowed to live forever when the external tool hangs or loses progress. It
// is a variable so lifecycle tests can exercise the timeout without waiting
// ten minutes.
var semanticIndexTimeout = 10 * time.Minute

// Legacy status is an inspection escape hatch, not an interactive indexing
// operation. A shorter default deadline prevents an old binary from keeping
// a health or diagnostic request alive indefinitely.
var semanticStatusTimeout = 30 * time.Second

// It is a variable so lifecycle tests can prove that a short-lived process is
// sampled before the first periodic observation.
var semanticRSSPollInterval = 25 * time.Millisecond

// Production uses the platform process-group sampler from procmon. Keeping this injectable
// makes the hard memory contract deterministic in lifecycle tests without an
// optional monitor executable.
var semanticRSSSampler = procmon.ProcessGroupRSS

var errSemanticOutputLimit = errors.New("vecgrep output exceeds limit")

// errSemanticIndexBuildStopped distinguishes the index build's own lifetime
// ending (hard timeout or CancelIndexing) from a single setup caller being
// canceled. Existing waiters may retry the latter as a new single-flight
// leader, but must share the former terminal outcome instead of resurrecting
// an index process that Teak deliberately stopped.
var errSemanticIndexBuildStopped = errors.New("vecgrep index build stopped")

const (
	// Keep readiness observations bounded across workspaces. The cache avoids
	// repeating a status probe for every interactive query while the TTL keeps
	// an external edit or index replacement from remaining invisible forever.
	maxSemanticReadyCacheEntries = 64
	semanticReadyCacheTTL        = 30 * time.Second
)

// ErrVecgrepLightweightUnsupported means the installed vecgrep cannot provide
// a vector-free health report. Health callers must surface this state instead
// of falling back to the legacy status command, which may load the full HNSW
// graph. Explicit indexing may still use the compatibility path below.
var ErrVecgrepLightweightUnsupported = errors.New("vecgrep lightweight status is unsupported")

// ErrVecgrepLegacyStatusOptInRequired means a caller attempted to use the
// compatibility status path without explicitly authorizing its bounded RSS
// cost. Normal health and control-plane callers must use the vector-free API.
var ErrVecgrepLegacyStatusOptInRequired = errors.New("vecgrep legacy status requires explicit opt-in")

// ErrVecgrepLegacyStatusRSSBudgetExceeded means a legacy status process (or
// one of its descendants) crossed the caller's resident-memory ceiling.
var ErrVecgrepLegacyStatusRSSBudgetExceeded = errors.New("vecgrep legacy status exceeded RSS budget")

// ErrVecgrepLegacyStatusRSSUnavailable means Teak could not supervise the
// legacy process group's RSS. The compatibility escape hatch fails closed
// rather than running an unbounded graph-loading command.
var ErrVecgrepLegacyStatusRSSUnavailable = errors.New("vecgrep legacy status RSS supervision unavailable")

// ErrSemanticIndexNotReady is returned by read-only semantic search when the
// workspace has no current vecgrep index. Callers can use it to offer an
// explicit indexing action without silently starting a potentially expensive
// build.
var ErrSemanticIndexNotReady = errors.New("vecgrep semantic index is not ready")

// ErrSemanticIndexRSSBudgetExceeded means vecgrep indexing was terminated
// after crossing its resident-memory ceiling.
var ErrSemanticIndexRSSBudgetExceeded = errors.New("vecgrep index exceeded RSS budget")

// ErrSemanticIndexRSSUnavailable means Teak could not sample the indexer's
// process-group RSS. Indexing fails closed instead of running without its
// memory guard.
var ErrSemanticIndexRSSUnavailable = errors.New("vecgrep index RSS supervision unavailable")

// ErrSemanticQueryRSSBudgetExceeded means a vecgrep search process crossed
// its resident-memory ceiling and was terminated before returning results.
var ErrSemanticQueryRSSBudgetExceeded = errors.New("vecgrep query exceeded RSS budget")

// ErrSemanticQueryRSSUnavailable means Teak could not sample a vecgrep query
// process group. Queries fail closed instead of running without supervision.
var ErrSemanticQueryRSSUnavailable = errors.New("vecgrep query RSS supervision unavailable")

type semanticIndexRSSBudgetKey struct{}
type semanticQueryRSSBudgetKey struct{}

type vecgrepLegacyStatusOptInKey struct{}

type vecgrepLegacyStatusAuthorization struct {
	maxRSSBytes uint64
}

// WithSemanticIndexRSSBudget authorizes one vecgrep index-preparation flight
// to use maxRSSBytes. A zero value restores the conservative default. The
// first caller to join a shared setup flight determines its bound, so callers
// that need a larger project-specific ceiling should wrap their context before
// starting the operation.
func WithSemanticIndexRSSBudget(ctx context.Context, maxRSSBytes uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxRSSBytes == 0 {
		maxRSSBytes = defaultSemanticIndexRSSBudget
	}
	return context.WithValue(ctx, semanticIndexRSSBudgetKey{}, maxRSSBytes)
}

func semanticIndexRSSBudgetFromContext(ctx context.Context) uint64 {
	if ctx != nil {
		if budget, ok := ctx.Value(semanticIndexRSSBudgetKey{}).(uint64); ok && budget > 0 {
			return budget
		}
	}
	return defaultSemanticIndexRSSBudget
}

// WithSemanticQueryRSSBudget authorizes semantic queries made with ctx to use
// maxRSSBytes. A zero value restores the conservative default; there is no
// unbounded query API.
func WithSemanticQueryRSSBudget(ctx context.Context, maxRSSBytes uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxRSSBytes == 0 {
		maxRSSBytes = defaultSemanticQueryRSSBudget
	}
	return context.WithValue(ctx, semanticQueryRSSBudgetKey{}, maxRSSBytes)
}

func semanticQueryRSSBudgetFromContext(ctx context.Context) uint64 {
	if ctx != nil {
		if budget, ok := ctx.Value(semanticQueryRSSBudgetKey{}).(uint64); ok && budget > 0 {
			return budget
		}
	}
	return defaultSemanticQueryRSSBudget
}

// WithVecgrepLegacyStatus explicitly authorizes the compatibility status path
// with a finite process-group RSS budget. A zero budget selects the safe
// 512 MiB default; there is no API for an unbounded legacy status operation.
func WithVecgrepLegacyStatus(ctx context.Context, maxRSSBytes uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxRSSBytes == 0 {
		maxRSSBytes = defaultVecgrepLegacyStatusRSSBudget
	}
	return context.WithValue(ctx, vecgrepLegacyStatusOptInKey{}, vecgrepLegacyStatusAuthorization{maxRSSBytes: maxRSSBytes})
}

// VecgrepStatus describes the on-disk semantic index without starting an
// indexing operation. It is used by diagnostics and machine-facing clients.
type VecgrepStatus struct {
	IndexFresh     bool
	FreshnessKnown bool
	FilesKnown     bool
	Files          int
	PendingChanges int
}

// Ready reports whether a semantic query can run without changing the
// workspace. Older vecgrep status formats may not expose file statistics, so
// an otherwise fresh response remains usable when FilesKnown is false.
func (s VecgrepStatus) Ready() bool {
	return s.FreshnessKnown && s.IndexFresh && s.PendingChanges == 0 &&
		(s.Files > 0 || !s.FilesKnown)
}

// VecgrepStatusContext reads vecgrep's status without initializing or indexing
// the workspace. A missing vecgrep project is returned as an error so callers
// can distinguish it from a fresh index.
func VecgrepStatusContext(ctx context.Context, rootDir string) (VecgrepStatus, error) {
	return VecgrepLightweightStatusContext(ctx, rootDir)
}

// VecgrepLegacyStatusContext uses the compatibility status forms needed by
// older vecgrep releases. It is deliberately separate from VecgrepStatusContext
// because old `status` may load the complete HNSW graph. Callers must opt in
// with WithVecgrepLegacyStatus, and every legacy command is bounded by both a
// deadline and process-group RSS supervision.
func VecgrepLegacyStatusContext(ctx context.Context, rootDir string) (VecgrepStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	authorization, ok := ctx.Value(vecgrepLegacyStatusOptInKey{}).(vecgrepLegacyStatusAuthorization)
	if !ok || authorization.maxRSSBytes == 0 {
		return VecgrepStatus{}, ErrVecgrepLegacyStatusOptInRequired
	}
	if _, err := toolpath.Resolve("vecgrep"); err != nil {
		return VecgrepStatus{}, err
	}
	statusCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		statusCtx, cancel = context.WithTimeout(ctx, semanticStatusTimeout)
		defer cancel()
	}
	out, err := runVecgrepLegacyStatus(statusCtx, rootDir, authorization.maxRSSBytes)
	if err != nil {
		if errors.Is(err, ErrSemanticIndexRSSBudgetExceeded) {
			return VecgrepStatus{}, fmt.Errorf("%w: %v", ErrVecgrepLegacyStatusRSSBudgetExceeded, err)
		}
		if errors.Is(err, ErrSemanticIndexRSSUnavailable) {
			return VecgrepStatus{}, fmt.Errorf("%w: %v", ErrVecgrepLegacyStatusRSSUnavailable, err)
		}
		return VecgrepStatus{}, err
	}
	return parseVecgrepStatus(out)
}

// VecgrepLightweightStatusContext reads only the vector-free health manifest.
// It intentionally does not use the legacy compatibility fallback because
// this function is used by health checks and read-only control-plane paths.
func VecgrepLightweightStatusContext(ctx context.Context, rootDir string) (VecgrepStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := toolpath.Resolve("vecgrep"); err != nil {
		return VecgrepStatus{}, err
	}
	out, err := runVecgrep(ctx, rootDir, "status", "--lightweight", "--format", "json")
	if err != nil {
		if isLightweightStatusUnsupported(err) {
			return VecgrepStatus{}, fmt.Errorf("%w: upgrade vecgrep or use an explicit index operation: %v", ErrVecgrepLightweightUnsupported, err)
		}
		return VecgrepStatus{}, err
	}
	return parseVecgrepLightweightStatus(out)
}

func parseVecgrepStatus(out []byte) (VecgrepStatus, error) {
	var status vecgrepStatusJSON
	if err := json.Unmarshal(out, &status); err != nil {
		return VecgrepStatus{IndexFresh: isIndexed(string(out)), FreshnessKnown: true}, nil
	}
	freshnessKnown := !status.Lightweight || status.Freshness.State == "fresh" || status.Freshness.State == "stale"
	return VecgrepStatus{
		IndexFresh:     status.IndexFresh,
		FreshnessKnown: freshnessKnown,
		FilesKnown:     true,
		Files:          status.Stats.Files,
		PendingChanges: status.PendingChanges.TotalPending,
	}, nil
}

func parseVecgrepLightweightStatus(out []byte) (VecgrepStatus, error) {
	var status vecgrepStatusJSON
	if err := json.Unmarshal(bytes.TrimSpace(out), &status); err != nil || !status.Lightweight {
		return VecgrepStatus{}, fmt.Errorf("%w: response did not confirm vector-free status", ErrVecgrepLightweightUnsupported)
	}
	return parseVecgrepStatus(out)
}

// runVecgrepLegacyStatus first probes the vector-free form and only then runs
// legacy status forms under the caller's RSS budget. The first command is
// still bounded by runVecgrep's output cap and the shared context deadline.
func runVecgrepLegacyStatus(ctx context.Context, rootDir string, maxRSSBytes uint64) ([]byte, error) {
	out, err := runVecgrep(ctx, rootDir, "status", "--lightweight", "--format", "json")
	if err == nil {
		// A zero exit status is not enough evidence that the command was the
		// vector-free contract. Wrappers and older releases may accept unknown
		// flags while returning the legacy status payload. Require the explicit
		// marker before allowing this process to escape the RSS-supervised
		// compatibility path.
		if _, parseErr := parseVecgrepLightweightStatus(out); parseErr == nil {
			return out, nil
		}
		err = fmt.Errorf("%w: response did not confirm vector-free status", ErrVecgrepLightweightUnsupported)
	}
	if !isLightweightStatusUnsupported(err) && !errors.Is(err, ErrVecgrepLightweightUnsupported) {
		return out, err
	}

	// Older releases may reject both --lightweight and --format. Try the
	// structured legacy form first, then the plain status output used by the
	// oldest supported releases. This path is only for callers that explicitly
	// request the compatibility status API; indexing setup uses index directly.
	out, err = runVecgrepRSSCommand(ctx, rootDir, maxRSSBytes, maxSemanticSetupOutputBytes, "status", "--format", "json")
	if err == nil || !isLightweightStatusUnsupported(err) {
		return out, err
	}
	return runVecgrepRSSCommand(ctx, rootDir, maxRSSBytes, maxSemanticSetupOutputBytes, "status")
}

func isLightweightStatusUnsupported(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unknown flag") ||
		strings.Contains(s, "flag provided but not defined") ||
		strings.Contains(s, "unknown option") ||
		strings.Contains(s, "unrecognized option") ||
		strings.Contains(s, "invalid option") ||
		strings.Contains(s, "unknown command")
}

type semanticReadyCacheEntry struct {
	value    any
	storedAt time.Time
}

// semanticReadyCache caches per-workspace readiness without allowing a long
// editor session that visits many projects to retain one entry per workspace
// forever. It deliberately exposes the small Load/Store/Delete surface used
// by the lifecycle code and tests, rather than leaking a general-purpose map.
type semanticReadyCache struct {
	mu      sync.Mutex
	entries map[string]semanticReadyCacheEntry
	now     func() time.Time
}

func newSemanticReadyCache(now func() time.Time) *semanticReadyCache {
	if now == nil {
		now = time.Now
	}
	return &semanticReadyCache{entries: make(map[string]semanticReadyCacheEntry), now: now}
}

func (c *semanticReadyCache) Load(key string) (any, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if c.now().Sub(entry.storedAt) >= semanticReadyCacheTTL {
		delete(c.entries, key)
		return nil, false
	}
	return entry.value, true
}

func (c *semanticReadyCache) Store(key string, value any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]semanticReadyCacheEntry)
	}
	now := c.now()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= maxSemanticReadyCacheEntries {
		var oldestKey string
		var oldest time.Time
		for candidate, entry := range c.entries {
			if oldestKey == "" || entry.storedAt.Before(oldest) {
				oldestKey = candidate
				oldest = entry.storedAt
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.entries[key] = semanticReadyCacheEntry{value: value, storedAt: now}
}

func (c *semanticReadyCache) Delete(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func (c *semanticReadyCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for key, entry := range c.entries {
		if now.Sub(entry.storedAt) >= semanticReadyCacheTTL {
			delete(c.entries, key)
		}
	}
	return len(c.entries)
}

var vecgrepReady = newSemanticReadyCache(time.Now)

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
func beginIndexBuild(rootDir string, rssBudget uint64) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), semanticIndexTimeout)
	ctx = WithSemanticIndexRSSBudget(ctx, rssBudget)
	build := &indexBuild{cancel: cancel, done: make(chan struct{})}
	key := workspaceKey(rootDir)

	indexBuilds.mu.Lock()
	indexBuilds.byDir[key] = build
	indexBuilds.mu.Unlock()

	finish := func() {
		indexBuilds.mu.Lock()
		if indexBuilds.byDir[key] == build {
			delete(indexBuilds.byDir, key)
		}
		indexBuilds.mu.Unlock()
		cancel()
		close(build.done)
	}
	return ctx, finish
}

// runIndexBuild runs setupVecgrepContext under a context scoped to the index
// build's own lifetime rather than the caller's. See indexBuilds.
func runIndexBuild(rootDir string, rssBudget uint64) error {
	ctx, finish := beginIndexBuild(rootDir, rssBudget)
	defer finish()
	err := setupVecgrepContext(ctx, rootDir)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: %w", errSemanticIndexBuildStopped, ctxErr)
	}
	return err
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
	if ctx == nil {
		ctx = context.Background()
	}
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
		key := workspaceKey(rootDir)
		if err := ctx.Err(); err != nil {
			return err
		}

		semanticSetupFlights.mu.Lock()
		if flight, ok := semanticSetupFlights.flights[key]; ok {
			semanticSetupFlights.mu.Unlock()
			select {
			case <-flight.done:
				if flight.err != nil &&
					(errors.Is(flight.err, context.Canceled) || errors.Is(flight.err, context.DeadlineExceeded)) &&
					!errors.Is(flight.err, errSemanticIndexBuildStopped) &&
					ctx.Err() == nil {
					continue
				}
				return flight.err
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		flight := &semanticSetupFlight{done: make(chan struct{})}
		semanticSetupFlights.flights[key] = flight
		semanticSetupFlights.mu.Unlock()

		err := setup(ctx)

		semanticSetupFlights.mu.Lock()
		flight.err = err
		delete(semanticSetupFlights.flights, key)
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
	vecgrepReady.Delete(workspaceKey(rootDir))
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
		return nil, fmt.Errorf("vecgrep not found: install it for semantic search: %w", err)
	}

	// Ensure the project is initialized and indexed
	if err := ensureVecgrepReadyContext(ctx, rootDir); err != nil {
		return nil, fmt.Errorf("vecgrep setup failed: %w", err)
	}

	return SemanticSearchIndexedContext(ctx, rootDir, query)
}

// SemanticSearchIndexContext explicitly checks/builds the semantic index under
// the caller's context before querying it. One-shot callers such as the
// headless control plane should use this variant so their deadline cancels the
// indexing subprocess; the interactive search path intentionally keeps its
// index build alive across per-keystroke query cancellation.
func SemanticSearchIndexContext(ctx context.Context, rootDir, query string) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := toolpath.Resolve("vecgrep"); err != nil {
		return nil, fmt.Errorf("vecgrep not found: install it for semantic search: %w", err)
	}
	if err := runSemanticSetupContext(ctx, rootDir, func(buildCtx context.Context) error {
		return setupVecgrepContext(buildCtx, rootDir)
	}); err != nil {
		return nil, fmt.Errorf("vecgrep setup failed: %w", err)
	}
	return SemanticSearchIndexedContext(ctx, rootDir, query)
}

// SemanticSearchReadyContext performs a read-only semantic search. It checks
// vecgrep's lightweight status and refuses to query a stale or uninitialized
// index; callers that want to build it must opt into SemanticSearchContext.
func SemanticSearchReadyContext(ctx context.Context, rootDir, query string) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	status, err := VecgrepLightweightStatusContext(ctx, rootDir)
	if err != nil {
		return nil, err
	}
	if !status.Ready() {
		return nil, fmt.Errorf("%w: fresh=%t pending=%d files=%d", ErrSemanticIndexNotReady,
			status.IndexFresh, status.PendingChanges, status.Files)
	}
	return SemanticSearchIndexedContext(ctx, rootDir, query)
}

// SemanticSearchIndexedContext queries an index that the caller has already
// verified. It never runs vecgrep status, init, or index and is therefore safe
// for a read-only control plane after an explicit readiness check.
func SemanticSearchIndexedContext(ctx context.Context, rootDir, query string) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := toolpath.Resolve("vecgrep"); err != nil {
		return nil, fmt.Errorf("vecgrep not found: install it for semantic search: %w", err)
	}
	results, err := runSemanticQueryContext(ctx, rootDir, query)
	if err != nil {
		return nil, err
	}
	return ValidateResults(rootDir, results)
}

func runSemanticQueryContext(ctx context.Context, rootDir, query string) ([]Result, error) {
	limit := fmt.Sprintf("%d", MaxSemanticSearchResults)
	budget := semanticQueryRSSBudgetFromContext(ctx)
	out, err := runVecgrepRSSQueryCommand(ctx, rootDir, budget, "search", query, "--format", "json-envelope", "--limit", limit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, errSemanticOutputLimit) ||
			errors.Is(err, ErrSemanticQueryRSSBudgetExceeded) ||
			errors.Is(err, ErrSemanticQueryRSSUnavailable) {
			return nil, err
		}
		// Fallback: try plain json for older versions
		out2, err2 := runVecgrepRSSQueryCommand(ctx, rootDir, budget, "search", query, "--format", "json", "--limit", limit)
		if err2 != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			if errors.Is(err2, ErrSemanticQueryRSSBudgetExceeded) || errors.Is(err2, ErrSemanticQueryRSSUnavailable) {
				return nil, err2
			}
			return nil, fmt.Errorf("vecgrep search failed: %w", err)
		}
		return parseJSONOutput(out2)
	}

	return parseEnvelopeOutput(out)
}

func ensureVecgrepReadyContext(ctx context.Context, rootDir string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	key := workspaceKey(rootDir)
	if _, ok := vecgrepReady.Load(key); ok {
		return nil
	}
	return runSemanticSetupContext(ctx, rootDir, func(context.Context) error {
		// A concurrent caller may have completed setup before this leader got
		// scheduled, so recheck the ready cache inside the single-flight path.
		if _, ok := vecgrepReady.Load(key); ok {
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
		return runIndexBuild(rootDir, semanticIndexRSSBudgetFromContext(ctx))
	})
}

func setupVecgrepContext(ctx context.Context, rootDir string) error {
	// Health and explicit indexing share the vector-free status contract when
	// it exists. Do not use runVecgrepStatus here: its compatibility fallback
	// intentionally reaches legacy `status`, which can load the full HNSW graph
	// before an explicit index operation has even started.
	status, err := VecgrepLightweightStatusContext(ctx, rootDir)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	if err != nil {
		if !errors.Is(err, ErrVecgrepLightweightUnsupported) && !isVecgrepProjectMissing(err) {
			return fmt.Errorf("vecgrep lightweight status failed: %w", err)
		}
		if indexErr := indexVecgrepWithInitFallback(ctx, rootDir); indexErr != nil {
			return indexErr
		}
		vecgrepReady.Store(workspaceKey(rootDir), true)
		return nil
	}

	if !status.Ready() {
		if indexErr := indexVecgrepWithInitFallback(ctx, rootDir); indexErr != nil {
			return indexErr
		}
	}

	vecgrepReady.Store(workspaceKey(rootDir), true)
	return nil
}

// indexVecgrepWithInitFallback keeps explicit indexing usable when an older
// vecgrep has no vector-free status command. It tries the operation that the
// caller explicitly requested first; init is only attempted when index itself
// proves that the project is absent. This avoids the legacy status path that
// can materialize the full semantic graph merely to answer a preflight query.
func indexVecgrepWithInitFallback(ctx context.Context, rootDir string) error {
	indexErr := runVecgrepSetupCommand(ctx, rootDir, "index")
	if indexErr == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if !isVecgrepProjectMissing(indexErr) {
		return indexErr
	}
	if initErr := runVecgrepSetupCommand(ctx, rootDir, "init", rootDir); initErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("vecgrep init failed: %w", initErr)
	}
	if indexErr = runVecgrepSetupCommand(ctx, rootDir, "index"); indexErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return indexErr
	}
	return nil
}

// workspaceKey gives in-process setup flights and readiness state one identity
// for a workspace even when it is opened through a symlink or relative path.
// External commands still receive rootDir so their configured project path is
// unchanged.
func workspaceKey(rootDir string) string {
	if rootDir == "" {
		return ""
	}
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return filepath.Clean(rootDir)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(real)
	}
	return filepath.Clean(abs)
}

func runVecgrepSetupCommand(ctx context.Context, rootDir string, args ...string) error {
	_, err := runVecgrepRSSCommand(ctx, rootDir, semanticIndexRSSBudgetFromContext(ctx), maxSemanticSetupOutputBytes, args...)
	return err
}

// runVecgrepRSSCommand runs a bounded external command and supervises the
// complete process group. It is shared by index setup and the explicitly
// authorized legacy status path so descendants cannot escape the memory
// contract while the direct process is still alive.
func runVecgrepRSSCommand(ctx context.Context, rootDir string, budget uint64, outputLimit int, args ...string) ([]byte, error) {
	return runVecgrepRSSCommandWithErrors(ctx, rootDir, budget, outputLimit,
		ErrSemanticIndexRSSBudgetExceeded, ErrSemanticIndexRSSUnavailable, args...)
}

func runVecgrepRSSQueryCommand(ctx context.Context, rootDir string, budget uint64, args ...string) ([]byte, error) {
	return runVecgrepRSSCommandWithErrors(ctx, rootDir, budget, maxSemanticOutputBytes,
		ErrSemanticQueryRSSBudgetExceeded, ErrSemanticQueryRSSUnavailable, args...)
}

func runVecgrepRSSCommandWithErrors(ctx context.Context, rootDir string, budget uint64, outputLimit int, budgetErr, unavailableErr error, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd, err := toolpath.Command(runCtx, "vecgrep", args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = rootDir
	stdout := &boundedCommandBuffer{limit: outputLimit, onLimit: cancel}
	stderr := &boundedCommandBuffer{limit: maxSemanticErrorBytes, onLimit: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	state := &semanticRSSSupervisorState{}
	supervisorCtx, stopSupervisor := context.WithCancel(context.Background())
	supervisorDone := make(chan struct{})
	processDone := make(chan struct{})
	go superviseSemanticRSS(supervisorCtx, processDone, cmd, budget, cancel, state, supervisorDone)

	runErr := cmd.Wait()
	close(processDone)
	stopSupervisor()
	<-supervisorDone

	peakRSS, exceeded, unavailable := state.snapshot()
	if exceeded {
		return nil, &semanticRSSBudgetError{sentinel: budgetErr, budget: budget, peak: peakRSS}
	}
	if unavailable != nil {
		return nil, fmt.Errorf("%w: %v", unavailableErr, unavailable)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, errSemanticOutputLimit
	}
	if runErr == nil {
		return stdout.Bytes(), nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = strings.TrimSpace(stdout.String())
	}
	if detail != "" {
		return nil, fmt.Errorf("vecgrep %s failed: %w: %s", args[0], runErr, detail)
	}
	return nil, fmt.Errorf("vecgrep %s failed: %w", args[0], runErr)
}

type semanticRSSSupervisorState struct {
	mu          sync.Mutex
	peakRSS     uint64
	exceeded    bool
	unavailable error
}

func (s *semanticRSSSupervisorState) recordRSS(rss uint64) {
	s.mu.Lock()
	if rss > s.peakRSS {
		s.peakRSS = rss
	}
	s.mu.Unlock()
}

func (s *semanticRSSSupervisorState) setExceeded() {
	s.mu.Lock()
	s.exceeded = true
	s.mu.Unlock()
}

func (s *semanticRSSSupervisorState) setUnavailable(err error) {
	s.mu.Lock()
	if s.unavailable == nil {
		s.unavailable = err
	}
	s.mu.Unlock()
}

func (s *semanticRSSSupervisorState) snapshot() (peakRSS uint64, exceeded bool, unavailable error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakRSS, s.exceeded, s.unavailable
}

type semanticRSSBudgetError struct {
	sentinel error
	budget   uint64
	peak     uint64
}

func (e *semanticRSSBudgetError) Error() string {
	return fmt.Sprintf("%v: peak=%d bytes budget=%d bytes", e.sentinel, e.peak, e.budget)
}

func (e *semanticRSSBudgetError) Unwrap() error { return e.sentinel }

func superviseSemanticRSS(ctx context.Context, processDone <-chan struct{}, cmd *exec.Cmd, budget uint64, cancel context.CancelFunc, state *semanticRSSSupervisorState, done chan<- struct{}) {
	defer close(done)
	consecutiveErrors := 0
	sample := func() bool {
		if cmd.Process == nil {
			return false
		}
		rss, err := semanticRSSSampler(ctx, cmd.Process.Pid)
		if err != nil {
			// A short-lived command can disappear between the sample and Wait;
			// retry transient process-exit observations before failing closed.
			if errors.Is(err, context.Canceled) || os.IsNotExist(err) {
				return true
			}
			consecutiveErrors++
			if consecutiveErrors < 3 {
				return true
			}
			state.setUnavailable(err)
			_ = toolpath.TerminateCommand(cmd)
			cancel()
			return false
		}
		consecutiveErrors = 0
		state.recordRSS(rss)
		if rss > budget {
			state.setExceeded()
			_ = toolpath.TerminateCommand(cmd)
			cancel()
			return false
		}
		return true
	}

	// Sample immediately after Start so a short-lived process cannot exceed
	// its budget and exit before the first periodic observation.
	if !sample() {
		return
	}
	ticker := time.NewTicker(semanticRSSPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-processDone:
			return
		case <-ticker.C:
		}
		if !sample() {
			return
		}
	}
}

func isVecgrepProjectMissing(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not in a vecgrep project") ||
		strings.Contains(s, "not initialized") ||
		strings.Contains(s, "no vecgrep project") ||
		strings.Contains(s, "project not registered")
}

type boundedCommandBuffer struct {
	bytes.Buffer
	limit         int
	exceeded      bool
	onLimit       func()
	limitNotified bool
}

func (b *boundedCommandBuffer) Write(p []byte) (int, error) {
	if b.limit <= b.Len() {
		b.markExceeded()
		return 0, errSemanticOutputLimit
	}
	remaining := b.limit - b.Len()
	if len(p) <= remaining {
		return b.Buffer.Write(p)
	}
	n, _ := b.Buffer.Write(p[:remaining])
	b.markExceeded()
	return n, errSemanticOutputLimit
}

func (b *boundedCommandBuffer) WriteString(s string) (int, error) {
	return b.Write([]byte(s))
}

// ReadFrom overrides the ReadFrom method promoted by bytes.Buffer. io.Copy
// prefers that fast path for command pipes, so enforcing the cap only in
// Write would allow a vecgrep/rg process to bypass it completely.
func (b *boundedCommandBuffer) ReadFrom(r io.Reader) (int64, error) {
	if b.limit <= b.Len() {
		b.markExceeded()
		return 0, errSemanticOutputLimit
	}
	remaining := int64(b.limit - b.Len())
	data, err := io.ReadAll(io.LimitReader(r, remaining+1))
	if int64(len(data)) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		b.markExceeded()
		return int64(len(data)), errSemanticOutputLimit
	}
	if len(data) > 0 {
		_, _ = b.Buffer.Write(data)
	}
	return int64(len(data)), err
}

func (b *boundedCommandBuffer) markExceeded() {
	if b.exceeded {
		return
	}
	b.exceeded = true
	if !b.limitNotified {
		b.limitNotified = true
		if b.onLimit != nil {
			b.onLimit()
		}
	}
}

func runVecgrep(ctx context.Context, rootDir string, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := &boundedCommandBuffer{limit: maxSemanticOutputBytes, onLimit: cancel}
	stderr := &boundedCommandBuffer{limit: maxSemanticErrorBytes, onLimit: cancel}
	cmd, cmdErr := toolpath.Command(runCtx, "vecgrep", args...)
	if cmdErr != nil {
		return nil, cmdErr
	}
	toolpath.ConfigureCommand(cmd)
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
	IndexFresh  bool `json:"index_fresh"`
	Lightweight bool `json:"lightweight"`
	Stats       struct {
		Files int `json:"files"`
	} `json:"stats"`
	PendingChanges struct {
		TotalPending int `json:"total_pending"`
	} `json:"pending_changes"`
	Freshness struct {
		State string `json:"state"`
	} `json:"freshness"`
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
		strings.Contains(s, "not indexed") || strings.Contains(s, "unindexed") ||
		strings.Contains(s, "not ready") || strings.Contains(s, "not found") ||
		strings.Contains(s, "not in a vecgrep project") {
		return false
	}
	// If status contains indicators of a healthy index, consider it ready
	if strings.Contains(s, "fresh") || strings.Contains(s, "indexed") || strings.Contains(s, "files:") || strings.Contains(s, "ready") {
		return true
	}
	// Unknown prose is not evidence that the index is usable. Setup callers
	// will run the explicit init/index path rather than trusting a future or
	// localized status message.
	return false
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
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("vecgrep returned empty output")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err == nil {
		if _, ok := raw["hits"]; !ok {
			return nil, errors.New("vecgrep response is missing hits")
		}
	}
	var envelope vecgrepEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		// Fall back to plain JSON array parsing
		return parseJSONOutput(data)
	}
	if len(envelope.Hits) == 0 {
		return nil, nil
	}
	results := make([]vecgrepResult, 0, len(envelope.Hits))
	for _, hit := range envelope.Hits {
		if hasVecgrepFile(hit) {
			results = append(results, hit)
		}
	}
	if len(results) == 0 {
		return nil, errors.New("vecgrep response contains no usable hits")
	}
	return mapVecgrepResults(boundSemanticResults(results)), nil
}

func parseJSONOutput(data []byte) ([]Result, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("vecgrep returned empty output")
	}
	// Try array of results
	var results []vecgrepResult
	if err := json.Unmarshal(trimmed, &results); err != nil {
		// Try line-delimited JSON
		lines := strings.Split(string(trimmed), "\n")
		invalid := 0
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var r vecgrepResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				invalid++
				continue
			}
			if !hasVecgrepFile(r) {
				invalid++
				continue
			}
			results = append(results, r)
		}
		if invalid > 0 || len(results) == 0 {
			return nil, errors.New("vecgrep response is not valid result JSON")
		}
	} else if len(results) == 0 {
		if string(trimmed) == "[]" {
			return nil, nil
		}
		return nil, errors.New("vecgrep response contains no usable results")
	} else {
		usable := results[:0]
		for _, result := range results {
			if hasVecgrepFile(result) {
				usable = append(usable, result)
			}
		}
		if len(usable) == 0 {
			return nil, errors.New("vecgrep response contains no usable results")
		}
		results = usable
	}
	results = boundSemanticResults(results)

	var out []Result
	for _, r := range results {
		out = append(out, mapVecgrepResult(r))
	}
	return out, nil
}

func boundSemanticResults(results []vecgrepResult) []vecgrepResult {
	if len(results) > MaxSemanticSearchResults {
		return results[:MaxSemanticSearchResults]
	}
	return results
}

func hasVecgrepFile(result vecgrepResult) bool {
	return result.RelativePath != "" || result.FilePath != "" || result.File != ""
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

	// Vecgrep reports source lines as 1-based, while Teak's text/editor
	// position contract is 0-based. A missing start_line falls back to the
	// legacy line field, but both fields use the same external convention.
	line := r.StartLine
	if line == 0 && r.Line > 0 {
		line = r.Line
	}
	if line > 0 {
		line--
	}
	endLine := r.EndLine
	if endLine > 0 {
		endLine--
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
		EndLine:    endLine,
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
