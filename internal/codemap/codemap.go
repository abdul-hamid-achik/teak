package codemap

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
	commandTimeout = 15 * time.Second
	indexTimeout   = 10 * time.Minute
	maxOutputBytes = 4 << 20
	maxImpactDepth = 10
	// FullStatus is intentionally explicit, but legacy codemap status can still
	// allocate several gigabytes. Keep the explicit path bounded by default;
	// callers with a genuinely larger project must choose a larger budget with
	// WithFullStatusBudget rather than inheriting an unbounded operation.
	defaultFullStatusRSSBudget = uint64(1 << 30)
	// Interactive preparation is also an external process and must not be
	// allowed to recreate the historical multi-gigabyte codemap spike. Keep the
	// implicit ceiling conservative; large projects can opt into a larger finite
	// budget through WithIndexRSSBudget.
	defaultIndexRSSBudget = uint64(512 << 20)
	// Older codemap binaries can open their central vector store even for an
	// apparently structural query. Keep queries bounded so a stale executable
	// cannot recreate the historical multi-gigabyte spike.
	defaultQueryRSSBudget = uint64(512 << 20)
)

var rssPollInterval = 25 * time.Millisecond

// Keep interactive indexing structural and bounded: embedding is disabled,
// and optional LSP/tips passes can otherwise start extra workers and retain a
// much larger project graph than a codemap query needs.
var memorySafeIndexArgs = []string{"index", "--no-embed", "--no-lsp", "--no-tips"}

var errOutputLimit = fmt.Errorf("codemap output exceeds %d bytes", maxOutputBytes)

// ErrStructuralManifestUnsupported means the installed codemap predates the
// bounded structural-manifest health contract. Callers must surface this as an
// explicit capability gap instead of falling back to `status`, which can load
// a legacy semantic store into several gigabytes of memory.
var ErrStructuralManifestUnsupported = errors.New("codemap structural manifest is unsupported")

// ErrFullStatusOptInRequired means a caller attempted to request graph
// statistics without explicitly authorizing the potentially memory-heavy
// legacy status path.
var ErrFullStatusOptInRequired = errors.New("codemap full status requires explicit opt-in")

// ErrFullStatusRSSBudgetExceeded means an explicitly authorized full-status
// operation exceeded its resident-memory budget and was terminated.
var ErrFullStatusRSSBudgetExceeded = errors.New("codemap full status exceeded RSS budget")

// ErrFullStatusRSSUnavailable means Teak could not sample the child process
// group's RSS. FullStatus fails closed rather than running a dangerous legacy
// status operation without its memory guard.
var ErrFullStatusRSSUnavailable = errors.New("codemap full status RSS supervision unavailable")

// ErrIndexRSSBudgetExceeded means an explicit or default memory ceiling
// stopped codemap preparation before it could finish.
var ErrIndexRSSBudgetExceeded = errors.New("codemap index exceeded RSS budget")

// ErrIndexRSSUnavailable means Teak could not sample the indexer's process-group
// RSS. The index operation fails closed instead of running without its memory
// guard.
var ErrIndexRSSUnavailable = errors.New("codemap index RSS supervision unavailable")

// ErrQueryRSSBudgetExceeded means a codemap query exceeded its process-group
// RSS ceiling and was terminated before it could return an unbounded result.
var ErrQueryRSSBudgetExceeded = errors.New("codemap query exceeded RSS budget")

// ErrQueryRSSUnavailable means Teak could not sample the query process group.
// Query execution fails closed rather than silently running without a memory
// guard.
var ErrQueryRSSUnavailable = errors.New("codemap query RSS supervision unavailable")

// ErrIndexDidNotProduceReadyManifest means Codemap reported a successful
// index operation, but its bounded structural manifest still was not complete
// and fresh when Teak rechecked it.
var ErrIndexDidNotProduceReadyManifest = errors.New("codemap index did not produce a ready structural manifest")

type fullStatusOptInKey struct{}

type fullStatusAuthorization struct {
	maxRSSBytes uint64
}

type indexRSSBudgetKey struct{}

type queryRSSBudgetKey struct{}

// WithFullStatus explicitly authorizes one FullStatus operation on ctx. The
// authorization travels with the caller's cancellation/deadline, but cannot
// be forged by using an unrelated context value key. Health, onboarding, and
// query callers should never use this helper. The default one-gigabyte RSS
// budget remains active even after explicit authorization.
func WithFullStatus(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, fullStatusOptInKey{}, fullStatusAuthorization{maxRSSBytes: defaultFullStatusRSSBudget})
}

// WithFullStatusBudget explicitly authorizes FullStatus with a chosen RSS
// budget. A zero budget selects the safe default; there is no API for an
// unbounded full-status operation.
func WithFullStatusBudget(ctx context.Context, maxRSSBytes uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxRSSBytes == 0 {
		maxRSSBytes = defaultFullStatusRSSBudget
	}
	return context.WithValue(ctx, fullStatusOptInKey{}, fullStatusAuthorization{maxRSSBytes: maxRSSBytes})
}

// WithIndexRSSBudget authorizes one shared index-preparation flight to use a
// chosen finite RSS budget. A zero budget selects the safe 512 MiB
// default. The first caller that starts a shared flight determines its budget;
// later callers join that already-bounded operation.
func WithIndexRSSBudget(ctx context.Context, maxRSSBytes uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxRSSBytes == 0 {
		maxRSSBytes = defaultIndexRSSBudget
	}
	return context.WithValue(ctx, indexRSSBudgetKey{}, maxRSSBytes)
}

func indexRSSBudgetFromContext(ctx context.Context) uint64 {
	if ctx != nil {
		if budget, ok := ctx.Value(indexRSSBudgetKey{}).(uint64); ok && budget > 0 {
			return budget
		}
	}
	return defaultIndexRSSBudget
}

// WithQueryRSSBudget authorizes one codemap query to use a chosen finite RSS
// budget. A zero budget selects the safe 512 MiB default.
func WithQueryRSSBudget(ctx context.Context, maxRSSBytes uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxRSSBytes == 0 {
		maxRSSBytes = defaultQueryRSSBudget
	}
	return context.WithValue(ctx, queryRSSBudgetKey{}, maxRSSBytes)
}

func queryRSSBudgetFromContext(ctx context.Context) uint64 {
	if ctx != nil {
		if budget, ok := ctx.Value(queryRSSBudgetKey{}).(uint64); ok && budget > 0 {
			return budget
		}
	}
	return defaultQueryRSSBudget
}

type indexFlight struct {
	done      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	err       error
	key       string
	rssBudget uint64
}

var indexFlights = struct {
	sync.Mutex
	byRoot map[string]*indexFlight
}{byRoot: make(map[string]*indexFlight)}

// Available returns true if the codemap binary can be resolved.
func Available() bool {
	return toolpath.Available("codemap")
}

// Symbol represents a code symbol from codemap.
type Symbol struct {
	Symbol    string `json:"symbol"`
	FQN       string `json:"fqn"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"` // 1-based (codemap convention)
	EndLine   int    `json:"end_line"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
}

// Line0 returns the 0-based start line (Teak convention).
func (s Symbol) Line0() int {
	if s.StartLine > 0 {
		return s.StartLine - 1
	}
	return 0
}

// ContextResult is the output of `codemap context <symbol>`.
type ContextResult struct {
	Definitions []Symbol `json:"definitions"`
	Callers     []Symbol `json:"callers"`
	Callees     []Symbol `json:"callees"`
	References  []Symbol `json:"references"`
	Tests       []Symbol `json:"tests"`
	// Older Codemap versions omit Found. Absence must not become a false
	// negative, and resolved calls must not imply complete value references.
	Found                *bool  `json:"found,omitempty"`
	CallersTotal         int    `json:"callers_total,omitempty"`
	CalleesTotal         int    `json:"callees_total,omitempty"`
	ReferencesTotal      int    `json:"references_total,omitempty"`
	ReferencesTruncated  int    `json:"references_truncated,omitempty"`
	TestsTotal           int    `json:"tests_total,omitempty"`
	ReferencesCoverage   string `json:"references_coverage,omitempty"`
	ReferencesStale      bool   `json:"references_stale,omitempty"`
	ReferencesConfidence string `json:"references_confidence,omitempty"`
	ReferencesResolution string `json:"references_resolution,omitempty"`
	CallGraph            string `json:"call_graph,omitempty"`
	Resolution           string `json:"resolution,omitempty"`
	Note                 string `json:"note,omitempty"`
	PartialErrors        []struct {
		Symbol    string `json:"symbol,omitempty"`
		Component string `json:"component"`
		Error     string `json:"error"`
	} `json:"partial_errors,omitempty"`
}

// ImpactResult is the output of `codemap impact <symbol>`.
type ImpactResult struct {
	Locations     []Symbol `json:"locations"`
	DirectCallers []Symbol `json:"direct_callers"`
	BlastRadius   []struct {
		Symbol
		Depth int `json:"depth"`
	} `json:"blast_radius"`
	Tests        []Symbol `json:"tests"`
	TestCommands []string `json:"test_commands"`
}

// StatusResult is the output of `codemap status`.
type StatusResult struct {
	// Registered is the current codemap status field. Indexed is retained for
	// compatibility with older clients and fixtures.
	Registered bool        `json:"registered"`
	Indexed    bool        `json:"indexed"`
	Nodes      int         `json:"nodes"`
	Edges      int         `json:"edges"`
	Files      int         `json:"files"`
	Stale      StaleCounts `json:"stale"`
	// Structural identifies the bounded structural-manifest projection. In
	// that mode Records is available, while Nodes/Edges/Files are intentionally
	// left unset because obtaining them may materialize the semantic store.
	Structural          bool `json:"structural,omitempty"`
	SchemaVersion       int  `json:"schema_version,omitempty"`
	ExportSchemaVersion int  `json:"export_schema_version,omitempty"`
	Records             int  `json:"records,omitempty"`
	FreshnessChecked    bool `json:"freshness_checked,omitempty"`
	Fresh               bool `json:"fresh,omitempty"`
}

// StructuralManifestResult is the lightweight output of `codemap
// structural-manifest`. Unlike StatusResult, it does not open the semantic
// vector store or materialize graph statistics. It is the preferred contract
// for health checks and orchestration paths where bounded memory matters.
type StructuralManifestResult struct {
	SchemaVersion       int                         `json:"schema_version"`
	ExportSchemaVersion int                         `json:"export_schema_version"`
	Project             string                      `json:"project"`
	ProjectKey          string                      `json:"project_key"`
	IndexFingerprint    string                      `json:"index_fingerprint"`
	TotalRecords        int                         `json:"total_records"`
	Complete            bool                        `json:"complete"`
	Freshness           StructuralManifestFreshness `json:"freshness"`
}

type StructuralManifestFreshness struct {
	Checked bool `json:"checked"`
	Fresh   bool `json:"fresh"`
	Changed int  `json:"changed"`
	New     int  `json:"new"`
	Deleted int  `json:"deleted"`
}

// StaleCounts accepts both codemap status formats: newer releases return
// integer counts while older releases returned arrays of paths. Keeping the
// normalization here makes status consumers independent of the installed CLI
// version and avoids allocating one placeholder per stale file.
type StaleCounts struct {
	Changed int `json:"changed"`
	New     int `json:"new"`
	Deleted int `json:"deleted"`
}

func (s *StaleCounts) UnmarshalJSON(data []byte) error {
	var raw struct {
		Changed json.RawMessage `json:"changed"`
		New     json.RawMessage `json:"new"`
		Deleted json.RawMessage `json:"deleted"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if s.Changed, err = decodeStaleCount(raw.Changed); err != nil {
		return fmt.Errorf("decode stale changed count: %w", err)
	}
	if s.New, err = decodeStaleCount(raw.New); err != nil {
		return fmt.Errorf("decode stale new count: %w", err)
	}
	if s.Deleted, err = decodeStaleCount(raw.Deleted); err != nil {
		return fmt.Errorf("decode stale deleted count: %w", err)
	}
	return nil
}

func decodeStaleCount(data json.RawMessage) (int, error) {
	if len(data) == 0 || string(data) == "null" {
		return 0, nil
	}
	var count int
	if err := json.Unmarshal(data, &count); err == nil {
		if count < 0 {
			return 0, fmt.Errorf("negative count %d", count)
		}
		return count, nil
	}
	var paths []json.RawMessage
	if err := json.Unmarshal(data, &paths); err != nil {
		return 0, err
	}
	return len(paths), nil
}

func parseStatus(data []byte) (StatusResult, error) {
	var status StatusResult
	if err := json.Unmarshal(data, &status); err != nil {
		return StatusResult{}, fmt.Errorf("parse codemap status: %w", err)
	}
	return status, nil
}

func parseStructuralManifest(data []byte) (StructuralManifestResult, error) {
	var manifest StructuralManifestResult
	if err := json.Unmarshal(data, &manifest); err != nil {
		return StructuralManifestResult{}, fmt.Errorf("parse codemap structural manifest: %w", err)
	}
	return manifest, nil
}

// Ready reports whether the structural index is complete and current.
func (m StructuralManifestResult) Ready() bool {
	// A partial or newer manifest must never be treated as a verified health
	// contract merely because its boolean fields happen to look healthy.
	return m.SchemaVersion == 1 && m.ExportSchemaVersion == 1 && m.Complete && m.Freshness.Checked && m.Freshness.Fresh
}

// Status reads the bounded structural project status without initializing,
// indexing, or materializing codemap's semantic store. It deliberately does
// not fall back to the legacy full-status command: a missing structural
// manifest is an unsupported health contract, not permission to risk a
// multi-gigabyte allocation.
func Status(ctx context.Context, rootDir string) (StatusResult, error) {
	manifest, err := StructuralManifest(ctx, rootDir)
	if err != nil {
		return StatusResult{}, err
	}
	return StatusResult{
		Registered: manifest.Complete,
		Indexed:    manifest.Complete,
		Stale: StaleCounts{
			Changed: manifest.Freshness.Changed,
			New:     manifest.Freshness.New,
			Deleted: manifest.Freshness.Deleted,
		},
		Structural:          true,
		SchemaVersion:       manifest.SchemaVersion,
		ExportSchemaVersion: manifest.ExportSchemaVersion,
		Records:             manifest.TotalRecords,
		FreshnessChecked:    manifest.Freshness.Checked,
		Fresh:               manifest.Freshness.Fresh,
	}, nil
}

// FullStatus is the explicit opt-in for graph statistics. Unlike Status, it
// may load codemap's semantic/vector store and can therefore use substantial
// memory on legacy binaries. Callers must wrap their context with
// WithFullStatus; keep this operation out of health and onboarding paths.
func FullStatus(ctx context.Context, rootDir string) (StatusResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	authorization, ok := ctx.Value(fullStatusOptInKey{}).(fullStatusAuthorization)
	if !ok {
		return StatusResult{}, fmt.Errorf("%w: wrap the operation with codemap.WithFullStatus", ErrFullStatusOptInRequired)
	}
	if !Available() {
		return StatusResult{}, fmt.Errorf("codemap not found: install it for code intelligence")
	}
	out, err := runWithRSSBudget(ctx, rootDir, authorization.maxRSSBytes, "status", "--full", "--json")
	if err != nil {
		return StatusResult{}, fmt.Errorf("codemap full status: %w", err)
	}
	return parseStatus(out)
}

// rssSampler is a variable so the safety contract can be tested with a
// deterministic child budget fixture. Production uses the platform
// process-group sampler from procmon, not the optional monitor executable.
var rssSampler = procmon.ProcessGroupRSS

type rssSupervisorState struct {
	mu          sync.Mutex
	peakRSS     uint64
	exceeded    bool
	unavailable error
}

func (s *rssSupervisorState) recordRSS(rss uint64) {
	s.mu.Lock()
	if rss > s.peakRSS {
		s.peakRSS = rss
	}
	s.mu.Unlock()
}

func (s *rssSupervisorState) setExceeded() {
	s.mu.Lock()
	s.exceeded = true
	s.mu.Unlock()
}

func (s *rssSupervisorState) setUnavailable(err error) {
	s.mu.Lock()
	if s.unavailable == nil {
		s.unavailable = err
	}
	s.mu.Unlock()
}

func (s *rssSupervisorState) snapshot() (peakRSS uint64, exceeded bool, unavailable error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakRSS, s.exceeded, s.unavailable
}

type rssBudgetError struct {
	sentinel error
	Budget   uint64
	Peak     uint64
}

func (e *rssBudgetError) Error() string {
	return fmt.Sprintf("%v: peak=%d bytes budget=%d bytes", e.sentinel, e.Peak, e.Budget)
}

func (e *rssBudgetError) Unwrap() error { return e.sentinel }

func runWithRSSBudget(parentCtx context.Context, rootDir string, budget uint64, args ...string) ([]byte, error) {
	return runWithRSSBudgetTimeout(
		parentCtx,
		rootDir,
		commandTimeout,
		budget,
		ErrFullStatusRSSBudgetExceeded,
		ErrFullStatusRSSUnavailable,
		args...,
	)
}

func runWithRSSBudgetTimeout(
	parentCtx context.Context,
	rootDir string,
	timeout time.Duration,
	budget uint64,
	budgetErr error,
	unavailableErr error,
	args ...string,
) ([]byte, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	if budget == 0 {
		budget = defaultFullStatusRSSBudget
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "codemap", args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = rootDir
	stdout := &boundedOutputBuffer{limit: maxOutputBytes, onLimit: cancel}
	stderr := &boundedOutputBuffer{limit: maxOutputBytes, onLimit: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	state := &rssSupervisorState{}
	supervisorCtx, stopSupervisor := context.WithCancel(context.Background())
	supervisorDone := make(chan struct{})
	processDone := make(chan struct{})
	go superviseRSS(supervisorCtx, processDone, cmd, budget, cancel, state, supervisorDone)

	err = cmd.Wait()
	close(processDone)
	stopSupervisor()
	<-supervisorDone

	peakRSS, exceeded, unavailable := state.snapshot()
	if exceeded {
		return nil, &rssBudgetError{sentinel: budgetErr, Budget: budget, Peak: peakRSS}
	}
	if unavailable != nil {
		return nil, fmt.Errorf("%w: %v", unavailableErr, unavailable)
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, errOutputLimit
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

func superviseRSS(ctx context.Context, processDone <-chan struct{}, cmd *exec.Cmd, budget uint64, cancel context.CancelFunc, state *rssSupervisorState, done chan<- struct{}) {
	defer close(done)
	consecutiveErrors := 0
	sample := func() bool {
		if cmd.Process == nil {
			return false
		}
		rss, err := rssSampler(ctx, cmd.Process.Pid)
		if err != nil {
			// A short-lived command can disappear between the sample and Wait;
			// retry a few times before declaring RSS supervision unavailable.
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

	// Sample immediately after Start. Waiting for the first ticker can miss a
	// short-lived process that exceeds its memory budget and exits before the
	// first periodic observation.
	if !sample() {
		return
	}
	ticker := time.NewTicker(rssPollInterval)
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

// StructuralManifest reads the bounded-memory identity and freshness
// preflight. Callers should prefer this over Status for health checks.
func StructuralManifest(ctx context.Context, rootDir string) (StructuralManifestResult, error) {
	if !Available() {
		return StructuralManifestResult{}, fmt.Errorf("codemap not found: install it for code intelligence")
	}
	out, err := run(ctx, rootDir, "structural-manifest", "--json")
	if err != nil {
		wrapped := fmt.Errorf("codemap structural manifest: %w", err)
		if isStructuralManifestUnsupported(err) {
			return StructuralManifestResult{}, fmt.Errorf("%w: %v", ErrStructuralManifestUnsupported, wrapped)
		}
		return StructuralManifestResult{}, wrapped
	}
	return parseStructuralManifest(out)
}

func isStructuralManifestUnsupported(err error) bool {
	message := strings.ToLower(err.Error())
	// This helper is called only for the structural-manifest invocation, so the
	// command/flag wording itself is enough; older CLIs vary in how they quote
	// or spell the requested subcommand in their diagnostic.
	return strings.Contains(message, "unknown command") ||
		strings.Contains(message, "unknown flag") ||
		strings.Contains(message, "unrecognized option") ||
		strings.Contains(message, "unsupported")
}

// Ready reports whether the index is registered and has no known stale files.
func (s StatusResult) Ready() bool { return s.ready() }

func (s StatusResult) ready() bool {
	if s.Structural && (s.SchemaVersion != 1 || s.ExportSchemaVersion != 1 || !s.FreshnessChecked || !s.Fresh) {
		return false
	}
	return (s.Registered || s.Indexed) && s.Stale.Changed == 0 &&
		s.Stale.New == 0 && s.Stale.Deleted == 0
}

// EnsureReady checks if the project is indexed and indexes it if needed.
func EnsureReady(ctx context.Context, rootDir string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !Available() {
		return fmt.Errorf("codemap not found: install it for code intelligence")
	}
	flight, leader := beginIndexFlight(rootDir, indexRSSBudgetFromContext(ctx))
	if leader {
		go runIndexFlight(rootDir, flight)
	}
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runIndexFlight(rootDir string, flight *indexFlight) {
	flight.err = ensureReadyUnshared(flight.ctx, rootDir, flight.rssBudget)
	flight.cancel()

	indexFlights.Lock()
	if indexFlights.byRoot[flight.key] == flight {
		delete(indexFlights.byRoot, flight.key)
	}
	close(flight.done)
	indexFlights.Unlock()
}

func ensureReadyUnshared(ctx context.Context, rootDir string, rssBudget uint64) error {
	manifest, err := StructuralManifest(ctx, rootDir)
	if err != nil {
		if errors.Is(err, ErrStructuralManifestUnsupported) {
			return indexCodemapWithInitFallback(ctx, rootDir, rssBudget)
		}
		if !isNotInitializedError(err) {
			return fmt.Errorf("codemap structural manifest: %w", err)
		}
		return initializeAndIndexCodemap(ctx, rootDir, rssBudget)
	}
	if manifest.Ready() {
		return nil
	}
	if _, idxErr := runIndexWithRSSBudget(ctx, rootDir, rssBudget, memorySafeIndexArgs...); idxErr != nil {
		return fmt.Errorf("codemap index: %w", idxErr)
	}
	return verifyPostIndexManifest(ctx, rootDir)
}

// indexCodemapWithInitFallback keeps explicit preparation usable with older
// codemap binaries that do not expose structural-manifest. It starts with the
// requested, structure-only index operation; init is only safe to attempt
// after that operation proves the project is absent. In particular, this must
// never fall back to legacy `status`, whose graph statistics can materialize a
// multi-gigabyte semantic store.
func indexCodemapWithInitFallback(ctx context.Context, rootDir string, rssBudget uint64) error {
	if _, err := runIndexWithRSSBudget(ctx, rootDir, rssBudget, memorySafeIndexArgs...); err == nil {
		return nil
	} else {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !isNotInitializedError(err) {
			return fmt.Errorf("codemap index: %w", err)
		}
	}
	return initializeAndIndexCodemap(ctx, rootDir, rssBudget)
}

func initializeAndIndexCodemap(ctx context.Context, rootDir string, rssBudget uint64) error {
	if _, err := runWithRSSBudgetTimeout(
		ctx,
		rootDir,
		indexTimeout,
		rssBudget,
		ErrIndexRSSBudgetExceeded,
		ErrIndexRSSUnavailable,
		"init",
	); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("codemap init: %w", err)
	}
	if _, err := runIndexWithRSSBudget(ctx, rootDir, rssBudget, memorySafeIndexArgs...); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("codemap index: %w", err)
	}
	return verifyPostIndexManifest(ctx, rootDir)
}

func verifyPostIndexManifest(ctx context.Context, rootDir string) error {
	manifest, err := StructuralManifest(ctx, rootDir)
	if err != nil {
		// Legacy binaries do not expose the structural manifest contract. Their
		// successful index operation remains the strongest contract available.
		if errors.Is(err, ErrStructuralManifestUnsupported) {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("codemap post-index manifest: %w", err)
	}
	if !manifest.Ready() {
		return fmt.Errorf("%w: schema=%d export_schema=%d complete=%t checked=%t fresh=%t changed=%d new=%d deleted=%d",
			ErrIndexDidNotProduceReadyManifest,
			manifest.SchemaVersion,
			manifest.ExportSchemaVersion,
			manifest.Complete,
			manifest.Freshness.Checked,
			manifest.Freshness.Fresh,
			manifest.Freshness.Changed,
			manifest.Freshness.New,
			manifest.Freshness.Deleted,
		)
	}
	return nil
}

func beginIndexFlight(rootDir string, rssBudget uint64) (*indexFlight, bool) {
	key := workspaceKey(rootDir)
	indexFlights.Lock()
	defer indexFlights.Unlock()
	if flight := indexFlights.byRoot[key]; flight != nil {
		return flight, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), indexTimeout)
	flight := &indexFlight{
		done:      make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
		key:       key,
		rssBudget: rssBudget,
	}
	indexFlights.byRoot[key] = flight
	return flight, true
}

// workspaceKey gives in-process lifecycle registries one identity for a
// workspace even when the user opens it through a relative path or symlink.
// The command still receives rootDir, preserving the path semantics expected
// by the installed codemap binary.
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

// CancelIndexing stops all in-flight codemap preparation. It is intended for
// workspace/app teardown, not for canceling one caller while other callers are
// still waiting on the same single-flight operation.
func CancelIndexing() {
	indexFlights.Lock()
	flights := make([]*indexFlight, 0, len(indexFlights.byRoot))
	for _, flight := range indexFlights.byRoot {
		flights = append(flights, flight)
	}
	indexFlights.Unlock()
	for _, flight := range flights {
		indexFlights.Lock()
		cancel := flight.cancel
		indexFlights.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

// WaitForIndexingShutdown waits for all currently running codemap operations
// to finish or be canceled. It keeps teardown bounded and prevents orphaned
// external indexers from surviving the editor process.
func WaitForIndexingShutdown(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		indexFlights.Lock()
		flights := make([]*indexFlight, 0, len(indexFlights.byRoot))
		for _, flight := range indexFlights.byRoot {
			flights = append(flights, flight)
		}
		indexFlights.Unlock()
		if len(flights) == 0 {
			return true
		}
		for _, flight := range flights {
			select {
			case <-flight.done:
			case <-ctx.Done():
				return false
			}
		}
	}
}

// Context returns the full context for a symbol (definitions, callers, callees, references, tests).
func Context(ctx context.Context, rootDir, symbol string) (*ContextResult, error) {
	out, err := run(ctx, rootDir, "context", symbol, "--json")
	if err != nil {
		return nil, err
	}
	var result ContextResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse codemap context: %w", err)
	}
	return &result, nil
}

// Impact returns the blast radius and covering tests for a symbol.
func Impact(ctx context.Context, rootDir, symbol string, depth int) (*ImpactResult, error) {
	if err := validateImpactDepth(depth); err != nil {
		return nil, err
	}
	out, err := run(ctx, rootDir, "impact", symbol, "--depth", fmt.Sprintf("%d", depth), "--json")
	if err != nil {
		return nil, err
	}
	var result ImpactResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse codemap impact: %w", err)
	}
	return &result, nil
}

func validateImpactDepth(depth int) error {
	if depth < 0 || depth > maxImpactDepth {
		return fmt.Errorf("codemap impact depth must be between 0 and %d", maxImpactDepth)
	}
	return nil
}

// Callers returns the call graph callers of a symbol.
func Callers(ctx context.Context, rootDir, symbol string) ([]Symbol, error) {
	out, err := run(ctx, rootDir, "callers", symbol, "--json")
	if err != nil {
		return nil, err
	}
	return parseSymbolList(out, "callers")
}

// Callees returns the call graph callees of a symbol.
func Callees(ctx context.Context, rootDir, symbol string) ([]Symbol, error) {
	out, err := run(ctx, rootDir, "callees", symbol, "--json")
	if err != nil {
		return nil, err
	}
	return parseSymbolList(out, "callees")
}

func parseSymbolList(data []byte, field string) ([]Symbol, error) {
	var symbols []Symbol
	if err := json.Unmarshal(data, &symbols); err == nil {
		return symbols, nil
	}

	var result struct {
		Callers []Symbol `json:"callers"`
		Callees []Symbol `json:"callees"`
		Results []Symbol `json:"results"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse codemap %s: %w", field, err)
	}

	var selected []Symbol
	switch field {
	case "callers":
		selected = result.Callers
	case "callees":
		selected = result.Callees
	default:
		return nil, fmt.Errorf("parse codemap %s: unsupported response field", field)
	}
	if selected != nil {
		return selected, nil
	}
	if result.Results != nil {
		return result.Results, nil
	}
	return nil, fmt.Errorf("parse codemap %s: response missing %q or %q", field, field, "results")
}

// SymbolAt resolves the symbol at a file:line position.
func SymbolAt(ctx context.Context, rootDir, file string, line int) (*Symbol, error) {
	out, err := run(ctx, rootDir, "symbol-at", fmt.Sprintf("%s:%d", file, line+1), "--json")
	if err != nil {
		return nil, err
	}
	var symbol Symbol
	if err := json.Unmarshal(out, &symbol); err != nil {
		return nil, fmt.Errorf("parse codemap symbol-at: %w", err)
	}
	return &symbol, nil
}

// Find searches for symbols by name across the project.
func Find(ctx context.Context, rootDir, name string) ([]Symbol, error) {
	out, err := run(ctx, rootDir, "find", name, "--json")
	if err != nil {
		return nil, err
	}
	var symbols []Symbol
	if err := json.Unmarshal(out, &symbols); err != nil {
		return nil, fmt.Errorf("parse codemap find: %w", err)
	}
	return symbols, nil
}

// Symbols lists the symbols defined in one indexed file. The caller is
// responsible for validating that file is workspace-relative before passing
// it to the external tool.
func Symbols(ctx context.Context, rootDir, file string) ([]Symbol, error) {
	out, err := run(ctx, rootDir, "symbols", file, "--json")
	if err != nil {
		return nil, err
	}
	var symbols []Symbol
	if err := json.Unmarshal(out, &symbols); err == nil {
		return symbols, nil
	}
	var envelope struct {
		Symbols []Symbol `json:"symbols"`
		Results []Symbol `json:"results"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return nil, fmt.Errorf("parse codemap symbols: %w", err)
	}
	if envelope.Symbols != nil {
		return envelope.Symbols, nil
	}
	if envelope.Results != nil {
		return envelope.Results, nil
	}
	return nil, errors.New("parse codemap symbols: response missing symbols")
}

func run(ctx context.Context, rootDir string, args ...string) ([]byte, error) {
	return runWithRSSBudgetTimeout(
		ctx,
		rootDir,
		commandTimeout,
		queryRSSBudgetFromContext(ctx),
		ErrQueryRSSBudgetExceeded,
		ErrQueryRSSUnavailable,
		args...,
	)
}

func runIndexWithRSSBudget(ctx context.Context, rootDir string, budget uint64, args ...string) ([]byte, error) {
	return runWithRSSBudgetTimeout(
		ctx,
		rootDir,
		indexTimeout,
		budget,
		ErrIndexRSSBudgetExceeded,
		ErrIndexRSSUnavailable,
		args...,
	)
}

func runWithTimeout(ctx context.Context, rootDir string, timeout time.Duration, args ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "codemap", args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = rootDir
	stdout := &boundedOutputBuffer{limit: maxOutputBytes, onLimit: cancel}
	stderr := &boundedOutputBuffer{limit: maxOutputBytes, onLimit: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, errOutputLimit
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

type boundedOutputBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
	onLimit  func()
}

func (b *boundedOutputBuffer) Write(p []byte) (int, error) {
	if b.Len() >= b.limit {
		b.markExceeded()
		return 0, errOutputLimit
	}
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.markExceeded()
		return remaining, errOutputLimit
	}
	return b.Buffer.Write(p)
}

func (b *boundedOutputBuffer) WriteString(s string) (int, error) {
	return b.Write([]byte(s))
}

// ReadFrom overrides the ReadFrom method promoted by bytes.Buffer. os/exec
// uses io.Copy for pipe output, and without this override it would bypass
// Write and the codemap output cap entirely.
func (b *boundedOutputBuffer) ReadFrom(r io.Reader) (int64, error) {
	if b.Len() >= b.limit {
		b.markExceeded()
		return 0, errOutputLimit
	}
	remaining := int64(b.limit - b.Len())
	data, err := io.ReadAll(io.LimitReader(r, remaining+1))
	if int64(len(data)) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		b.markExceeded()
		return int64(len(data)), errOutputLimit
	}
	if len(data) > 0 {
		_, _ = b.Buffer.Write(data)
	}
	return int64(len(data)), err
}

func (b *boundedOutputBuffer) markExceeded() {
	if b.exceeded {
		return
	}
	b.exceeded = true
	if b.onLimit != nil {
		b.onLimit()
	}
}

func isNotInitializedError(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not initialized") ||
		strings.Contains(s, "no index") ||
		strings.Contains(s, "not registered") ||
		strings.Contains(s, "not a codemap project")
}
