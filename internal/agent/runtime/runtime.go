// Package runtime provides the durable, bounded lifecycle for Teak agent runs.
//
// It intentionally does not execute a model or a shell command. The package
// owns the security and bookkeeping contract that an ACP adapter must satisfy:
// runs have stable identities, children inherit a capability intersection,
// budgets are bounded, cancellation is hierarchical, and completion requires
// a validated handoff.
package runtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"teak/internal/atomicfile"
)

const (
	storeVersion = 1

	defaultMaxDepth           = 1
	defaultMaxConcurrentChild = 3
	defaultMaxOutputBytes     = 4 << 20
	defaultRunTimeout         = 10 * time.Minute
	externalSyncInterval      = 250 * time.Millisecond

	// ManagerConfig is embeddable, so defaults alone are not enough: a host
	// must not be able to accidentally turn one run into an unbounded fan-out,
	// output retention, or lifetime. These are hard ceilings; normal Teak runs
	// use the smaller defaults above.
	maxManagerDepth              = 4
	maxManagerConcurrentChildren = 16
	maxManagerOutputBytes        = 16 << 20
	maxManagerTimeout            = time.Hour

	maxObjectiveBytes         = 16 << 10
	maxAgentMetadataBytes     = 4 << 10
	maxDoneCriteria           = 64
	maxManifestItems          = 4_096
	maxPathBytes              = 32 << 10
	maxHandoffBytes           = 256 << 10
	maxVerificationItems      = 128
	maxVerificationItemBytes  = 4 << 10
	maxVerificationTotalBytes = 64 << 10
	maxArtifacts              = 256
	maxArtifactBytes          = 64 << 20
	maxRunEvents              = 32
	maxRunEventDetailBytes    = 2 << 10
	maxRunAuditEvents         = 32
	maxRunAuditTokenBytes     = 64
	maxRunAuditDetailBytes    = 512
)

var (
	ErrRunNotFound      = errors.New("agent run not found")
	ErrParentNotActive  = errors.New("agent parent run is not active")
	ErrChildLimit       = errors.New("agent child limit reached")
	ErrActiveChildren   = errors.New("agent run has active children")
	ErrRunTerminal      = errors.New("agent run is already terminal")
	ErrInvalidSpec      = errors.New("invalid agent run specification")
	ErrInvalidHandoff   = errors.New("invalid agent handoff")
	ErrBudget           = errors.New("agent run budget rejected")
	ErrCapabilityDenied = errors.New("agent capability denied")
	ErrRunContext       = errors.New("agent run context unavailable")
)

// RunID is stable across persistence and process restarts.
type RunID string

// RunStatus is the lifecycle state persisted for a run.
type RunStatus string

const (
	RunRunning     RunStatus = "running"
	RunCompleted   RunStatus = "completed"
	RunFailed      RunStatus = "failed"
	RunCancelled   RunStatus = "cancelled"
	RunTimedOut    RunStatus = "timed_out"
	RunInterrupted RunStatus = "interrupted"
)

func (s RunStatus) terminal() bool {
	return s == RunCompleted || s == RunFailed || s == RunCancelled ||
		s == RunTimedOut || s == RunInterrupted
}

// Capabilities are monotonic permissions. A child can never gain a capability
// its parent did not have.
type Capabilities struct {
	Read     bool `json:"read"`
	Write    bool `json:"write"`
	Shell    bool `json:"shell"`
	Network  bool `json:"network"`
	Dispatch bool `json:"dispatch"`
}

func (c Capabilities) any() bool {
	return c.Read || c.Write || c.Shell || c.Network || c.Dispatch
}

func (c Capabilities) normalized() Capabilities {
	if !c.any() {
		// Zero-value requests are intentionally read-only rather than an
		// accidental request for unrestricted execution.
		c.Read = true
	}
	return c
}

func (c Capabilities) intersect(parent Capabilities) Capabilities {
	return Capabilities{
		Read:     c.Read && parent.Read,
		Write:    c.Write && parent.Write,
		Shell:    c.Shell && parent.Shell,
		Network:  c.Network && parent.Network,
		Dispatch: c.Dispatch && parent.Dispatch,
	}
}

// ReadOnlyCapabilities returns the safe default for a new run.
func ReadOnlyCapabilities() Capabilities { return Capabilities{Read: true} }

// Budget contains resource limits requested by a run. Zero values receive the
// manager's safe defaults; values above those defaults are rejected.
type Budget struct {
	Timeout        time.Duration `json:"timeout"`
	MaxChildren    int           `json:"max_children"`
	MaxOutputBytes int64         `json:"max_output_bytes"`
}

// RunSpec is the immutable dispatch contract recorded for a run.
type RunSpec struct {
	Objective             string       `json:"objective"`
	DoneCriteria          []string     `json:"done_criteria,omitempty"`
	Workspace             string       `json:"workspace"`
	Manifest              []string     `json:"manifest,omitempty"`
	ParentID              RunID        `json:"parent_id,omitempty"`
	RequestedCapabilities Capabilities `json:"requested_capabilities"`
	ModelRoute            string       `json:"model_route,omitempty"`
	ReasoningEffort       string       `json:"reasoning_effort,omitempty"`
	Budget                Budget       `json:"budget"`
}

// Artifact is a file or report produced by a run. Digests make handoffs
// independently checkable by the caller.
type Artifact struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

// Handoff is the only accepted completion payload. Verified must be set by a
// caller that actually ran the relevant checks; the runtime does not pretend
// that a textual summary is proof of completion.
type Handoff struct {
	Summary           string     `json:"summary"`
	CompletedCriteria []string   `json:"completed_criteria,omitempty"`
	Artifacts         []Artifact `json:"artifacts,omitempty"`
	Verification      []string   `json:"verification,omitempty"`
	Verified          bool       `json:"verified"`
}

// RunEvent is a bounded lifecycle entry retained with a durable run. It is
// deliberately limited to runtime-owned transitions; arbitrary tool output
// belongs in the bounded ACP/terminal streams, not in the run store.
type RunEvent struct {
	Type   string    `json:"type"`
	At     time.Time `json:"at"`
	Detail string    `json:"detail,omitempty"`
}

// OperationAuditEvent records a bounded runtime-owned operation outcome. It
// intentionally contains no command, arguments, paths, stdout, or model
// content; adapters should emit only stable classifications and short policy
// metadata here.
type OperationAuditEvent struct {
	Operation string    `json:"operation"`
	Outcome   string    `json:"outcome"`
	At        time.Time `json:"at"`
	Detail    string    `json:"detail,omitempty"`
}

// RunRecord is the durable public view of one run.
type RunRecord struct {
	ID                    RunID        `json:"id"`
	ParentID              RunID        `json:"parent_id,omitempty"`
	Depth                 int          `json:"depth"`
	Status                RunStatus    `json:"status"`
	Spec                  RunSpec      `json:"spec"`
	EffectiveCapabilities Capabilities `json:"effective_capabilities"`
	CreatedAt             time.Time    `json:"created_at"`
	StartedAt             time.Time    `json:"started_at"`
	// LastHeartbeatAt is updated by the adapter while work is still active.
	// It is an observation signal, not a replacement for the absolute run
	// timeout. Older stores may omit it; stale recovery falls back to
	// StartedAt for those records.
	LastHeartbeatAt time.Time             `json:"last_heartbeat_at,omitempty"`
	FinishedAt      time.Time             `json:"finished_at,omitempty"`
	Error           string                `json:"error,omitempty"`
	Handoff         *Handoff              `json:"handoff,omitempty"`
	Events          []RunEvent            `json:"events,omitempty"`
	Audit           []OperationAuditEvent `json:"audit,omitempty"`
}

// Store persists the complete run set. Implementations must replace the
// snapshot atomically so a crash cannot leave half a JSON document. FileStore
// additionally serializes cross-process readers/writers and merges independent
// run IDs while preserving terminal lifecycle states.
type Store interface {
	Load() ([]RunRecord, error)
	Save([]RunRecord) error
}

// FileStore persists runs in a private JSON file.
type FileStore struct {
	Path string
}

// WorkspaceStorePath returns the stable local store location for one
// workspace. Callers provide the state home so the runtime remains independent
// from the session package and can be embedded by headless or interactive
// adapters alike.
func WorkspaceStorePath(root, stateHome string) string {
	checksum := sha256.Sum256([]byte(canonicalWorkspacePath(root)))
	return filepath.Join(stateHome, "agent-runs", hex.EncodeToString(checksum[:]), "runs.json")
}

// LegacyWorkspaceStorePath returns the pre-canonicalization store path. It is
// exported for adapters that need to discover state written before workspace
// symlink aliases were unified.
func LegacyWorkspaceStorePath(root, stateHome string) string {
	checksum := sha256.Sum256([]byte(legacyWorkspacePath(root)))
	return filepath.Join(stateHome, "agent-runs", hex.EncodeToString(checksum[:]), "runs.json")
}

// ResolveWorkspaceStorePath prefers the canonical store and falls back to an
// existing legacy store. New writes therefore use one identity, while runs
// created by older Teak versions remain observable instead of disappearing
// after an upgrade.
func ResolveWorkspaceStorePath(root, stateHome string) string {
	canonical := WorkspaceStorePath(root, stateHome)
	if _, err := os.Lstat(canonical); err == nil {
		return canonical
	} else if !os.IsNotExist(err) {
		return canonical
	}
	legacy := LegacyWorkspaceStorePath(root, stateHome)
	if legacy != canonical {
		if _, err := os.Lstat(legacy); err == nil {
			return legacy
		}
	}
	return canonical
}

// canonicalWorkspacePath gives all adapters the same durable workspace key.
// RunSpec normalization already resolves symlinks before a run starts, but
// store-path construction happens before that normalization in interactive and
// headless adapters. Falling back to the absolute lexical path keeps this
// helper useful for callers that derive a path before a workspace exists.
func canonicalWorkspacePath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved)
	}
	return abs
}

func legacyWorkspacePath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return filepath.Clean(abs)
}

type storedState struct {
	Version int         `json:"version"`
	Runs    []RunRecord `json:"runs"`
}

func (s FileStore) Load() ([]RunRecord, error) {
	if strings.TrimSpace(s.Path) == "" {
		return nil, fmt.Errorf("agent runtime store path is empty")
	}
	if _, err := os.Lstat(s.Path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	lock, err := acquireStoreLock(s.Path)
	if err != nil {
		return nil, err
	}
	records, loadErr := s.loadLocked()
	unlockErr := lock.Unlock()
	if loadErr != nil {
		return nil, loadErr
	}
	if unlockErr != nil {
		return nil, fmt.Errorf("release agent runtime store lock: %w", unlockErr)
	}
	return records, nil
}

func (s FileStore) loadLocked() ([]RunRecord, error) {
	root, err := os.OpenRoot(filepath.Dir(s.Path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Base(s.Path)
	info, err := root.Lstat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("agent runtime store is a symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("agent runtime store is not a regular file")
	}
	if info.Size() > maxHandoffBytes*4 {
		return nil, fmt.Errorf("agent runtime store exceeds size limit")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxHandoffBytes*4+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxHandoffBytes*4 {
		return nil, fmt.Errorf("agent runtime store exceeds size limit")
	}
	var state storedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode agent runtime store: %w", err)
	}
	if state.Version != storeVersion {
		return nil, fmt.Errorf("unsupported agent runtime store version %d", state.Version)
	}
	return cloneRecords(state.Runs), nil
}

func (s FileStore) Save(records []RunRecord) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("agent runtime store path is empty")
	}
	lock, err := acquireStoreLock(s.Path)
	if err != nil {
		return err
	}
	existing, loadErr := s.loadLocked()
	if loadErr != nil {
		_ = lock.Unlock()
		return loadErr
	}
	merged := mergeRunRecords(existing, records)
	state := storedState{Version: storeVersion, Runs: merged}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		_ = lock.Unlock()
		return fmt.Errorf("encode agent runtime store: %w", err)
	}
	if len(data) > maxHandoffBytes*4 {
		_ = lock.Unlock()
		return fmt.Errorf("encoded agent runtime store exceeds size limit")
	}
	if err := atomicfile.Write(s.Path, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	}); err != nil {
		_ = lock.Unlock()
		return fmt.Errorf("save agent runtime store: %w", err)
	}
	if err := lock.Unlock(); err != nil {
		return fmt.Errorf("release agent runtime store lock: %w", err)
	}
	return nil
}

func mergeRunRecords(existing, incoming []RunRecord) []RunRecord {
	merged := cloneRecords(existing)
	indexes := make(map[RunID]int, len(merged))
	for i, record := range merged {
		indexes[record.ID] = i
	}
	for _, candidate := range incoming {
		if index, ok := indexes[candidate.ID]; ok {
			merged[index] = mergeRunRecord(merged[index], candidate)
			continue
		}
		indexes[candidate.ID] = len(merged)
		merged = append(merged, cloneRecord(candidate))
	}
	return merged
}

func mergeRunRecord(existing, candidate RunRecord) RunRecord {
	var merged RunRecord
	if existing.Status.terminal() && !candidate.Status.terminal() {
		merged = cloneRecord(existing)
	} else if !existing.Status.terminal() && candidate.Status.terminal() {
		merged = cloneRecord(candidate)
	} else if existing.Status.terminal() && candidate.Status.terminal() {
		// Terminal is a one-way boundary. A later save from a stale manager
		// must not replace cancellation, failure, or completion merely because
		// its wall clock is newer.
		merged = cloneRecord(existing)
	} else if candidateLast := runRecordHeartbeat(candidate); candidateLast.After(runRecordHeartbeat(existing)) {
		merged = cloneRecord(candidate)
	} else {
		merged = cloneRecord(existing)
	}
	merged.Audit = mergeOperationAudit(existing.Audit, candidate.Audit)
	return merged
}

func mergeOperationAudit(existing, candidate []OperationAuditEvent) []OperationAuditEvent {
	combined := make([]OperationAuditEvent, 0, len(existing)+len(candidate))
	combined = append(combined, existing...)
	combined = append(combined, candidate...)

	seen := make(map[string]struct{}, len(combined))
	merged := make([]OperationAuditEvent, 0, len(combined))
	for _, event := range combined {
		event.Operation, _ = normalizeAuditToken(event.Operation, "operation")
		event.Outcome, _ = normalizeAuditToken(event.Outcome, "outcome")
		event.Detail, _ = normalizeAuditDetail(event.Detail)
		if event.Operation == "" || event.Outcome == "" {
			continue
		}
		key := event.Operation + "\x00" + event.Outcome + "\x00" + event.At.UTC().Format(time.RFC3339Nano) + "\x00" + event.Detail
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, event)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].At.Before(merged[j].At)
	})
	if len(merged) > maxRunAuditEvents {
		merged = merged[len(merged)-maxRunAuditEvents:]
	}
	return append([]OperationAuditEvent(nil), merged...)
}

func runRecordHeartbeat(record RunRecord) time.Time {
	if !record.LastHeartbeatAt.IsZero() {
		return record.LastHeartbeatAt
	}
	return record.StartedAt
}

func auditEventsEqual(left, right []OperationAuditEvent) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// ManagerConfig controls safe global limits.
type ManagerConfig struct {
	Store                 Store
	MaxDepth              int
	MaxConcurrentChildren int
	MaxOutputBytes        int64
	MaxTimeout            time.Duration
	Now                   func() time.Time
	// SkipRecovery keeps persisted running records observable without changing
	// them. Read-only inspectors use this mode; normal managers recover active
	// records as interrupted because they own the lifecycle after a restart.
	SkipRecovery bool
}

type runHandle struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// RunHandle exposes cancellation observation without exposing the cancel
// function. Cancellation must go through Manager so terminal state is durable.
type RunHandle struct {
	ID      RunID
	Context context.Context
	Done    <-chan struct{}
}

// Manager owns all mutable lifecycle state and serializes persistence.
type Manager struct {
	mu                    sync.RWMutex
	store                 Store
	maxDepth              int
	maxConcurrentChildren int
	maxOutputBytes        int64
	maxTimeout            time.Duration
	now                   func() time.Time
	runs                  map[RunID]RunRecord
	handles               map[RunID]runHandle
}

// NewManager creates a manager and recovers active records from its store as
// interrupted. A process restart cannot safely claim that an in-flight model
// completed, so callers receive an explicit recovery state.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = defaultMaxDepth
	}
	if cfg.MaxConcurrentChildren <= 0 {
		cfg.MaxConcurrentChildren = defaultMaxConcurrentChild
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}
	if cfg.MaxTimeout <= 0 {
		cfg.MaxTimeout = defaultRunTimeout
	}
	if cfg.MaxDepth > maxManagerDepth {
		return nil, fmt.Errorf("%w: max depth exceeds %d", ErrBudget, maxManagerDepth)
	}
	if cfg.MaxConcurrentChildren > maxManagerConcurrentChildren {
		return nil, fmt.Errorf("%w: max concurrent children exceeds %d", ErrBudget, maxManagerConcurrentChildren)
	}
	if cfg.MaxOutputBytes > maxManagerOutputBytes {
		return nil, fmt.Errorf("%w: max output exceeds %d bytes", ErrBudget, maxManagerOutputBytes)
	}
	if cfg.MaxTimeout > maxManagerTimeout {
		return nil, fmt.Errorf("%w: max timeout exceeds %s", ErrBudget, maxManagerTimeout)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	m := &Manager{
		store:                 cfg.Store,
		maxDepth:              cfg.MaxDepth,
		maxConcurrentChildren: cfg.MaxConcurrentChildren,
		maxOutputBytes:        cfg.MaxOutputBytes,
		maxTimeout:            cfg.MaxTimeout,
		now:                   cfg.Now,
		runs:                  make(map[RunID]RunRecord),
		handles:               make(map[RunID]runHandle),
	}
	if cfg.Store == nil {
		return m, nil
	}
	records, err := cfg.Store.Load()
	if err != nil {
		return nil, err
	}
	changed := false
	for _, record := range records {
		if record.ID == "" {
			return nil, fmt.Errorf("%w: stored run has empty id", ErrInvalidSpec)
		}
		if _, exists := m.runs[record.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate stored run %q", ErrInvalidSpec, record.ID)
		}
		if record.Status == RunRunning && !cfg.SkipRecovery {
			record.Status = RunInterrupted
			record.Error = "runtime restarted before the run reached a terminal state"
			record.FinishedAt = m.now()
			appendRunEvent(&record, "recovered_interrupted", record.Error, m.now())
			changed = true
		}
		m.runs[record.ID] = cloneRecord(record)
	}
	if changed {
		if err := m.persistLocked(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// Start creates a running record and returns its cancellable handle.
func (m *Manager) Start(spec RunSpec) (*RunHandle, error) {
	if spec.ParentID != "" {
		if err := m.reconcileExternalTransition(spec.ParentID); err != nil {
			return nil, fmt.Errorf("reconcile agent parent: %w", err)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	normalized, err := m.normalizeSpecLocked(spec)
	if err != nil {
		return nil, err
	}
	effectiveCapabilities := normalized.RequestedCapabilities
	depth := 0
	var parent RunRecord
	if normalized.ParentID != "" {
		var ok bool
		parent, ok = m.runs[normalized.ParentID]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrRunNotFound, normalized.ParentID)
		}
		if parent.Status.terminal() {
			return nil, fmt.Errorf("%w: %q", ErrParentNotActive, normalized.ParentID)
		}
		depth = parent.Depth + 1
		if depth > m.maxDepth {
			return nil, fmt.Errorf("%w: maximum depth is %d", ErrBudget, m.maxDepth)
		}
		active := 0
		for _, record := range m.runs {
			if record.ParentID == normalized.ParentID && !record.Status.terminal() {
				active++
			}
		}
		limit := normalized.Budget.MaxChildren
		if parent.Spec.Budget.MaxChildren < limit {
			limit = parent.Spec.Budget.MaxChildren
		}
		if m.maxConcurrentChildren < limit {
			limit = m.maxConcurrentChildren
		}
		if active >= limit {
			return nil, fmt.Errorf("%w: parent allows %d active children", ErrChildLimit, limit)
		}
		effectiveCapabilities = normalized.RequestedCapabilities.intersect(parent.EffectiveCapabilities)
		if parent.Spec.Budget.MaxOutputBytes < normalized.Budget.MaxOutputBytes {
			normalized.Budget.MaxOutputBytes = parent.Spec.Budget.MaxOutputBytes
		}
	}
	parentContext := context.Background()
	parentDeadline, hasParentDeadline := parentContext.Deadline()
	if normalized.ParentID != "" {
		parentHandle, ok := m.handles[normalized.ParentID]
		if !ok {
			return nil, fmt.Errorf("%w: parent handle is unavailable", ErrParentNotActive)
		}
		parentContext = parentHandle.ctx
		parentDeadline, hasParentDeadline = parentContext.Deadline()
	}

	id, err := newRunID()
	if err != nil {
		return nil, fmt.Errorf("create agent run id: %w", err)
	}
	ctx := parentContext
	var cancel context.CancelFunc
	if normalized.Budget.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, normalized.Budget.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	deadline, hasDeadline := ctx.Deadline()
	ownsTimeout := hasDeadline && (!hasParentDeadline || deadline.Before(parentDeadline))
	now := m.now()
	record := RunRecord{
		ID:                    id,
		ParentID:              normalized.ParentID,
		Depth:                 depth,
		Status:                RunRunning,
		Spec:                  normalized,
		EffectiveCapabilities: effectiveCapabilities,
		CreatedAt:             now,
		StartedAt:             now,
		LastHeartbeatAt:       now,
	}
	appendRunEvent(&record, "started", "run started", now)
	m.runs[id] = record
	done := make(chan struct{})
	m.handles[id] = runHandle{ctx: ctx, cancel: cancel, done: done}
	if err := m.persistLocked(); err != nil {
		delete(m.runs, id)
		delete(m.handles, id)
		cancel()
		return nil, fmt.Errorf("persist agent start: %w", err)
	}

	go m.watchTimeout(id, ctx, ownsTimeout)
	go m.watchExternalTransition(id, ctx)
	return &RunHandle{ID: id, Context: ctx, Done: done}, nil
}

// Heartbeat records that an adapter is still making progress on a run. It is
// intentionally separate from completion: a heartbeat cannot extend the
// absolute context deadline and cannot revive a terminal run.
func (m *Manager) Heartbeat(id RunID) error {
	if err := m.reconcileExternalTransition(id); err != nil {
		return fmt.Errorf("reconcile agent heartbeat: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return fmt.Errorf("%w: %q", ErrRunTerminal, id)
	}
	previous := cloneRecord(record)
	record.LastHeartbeatAt = m.now()
	m.runs[id] = record
	if err := m.persistLocked(); err != nil {
		m.runs[id] = previous
		return fmt.Errorf("persist agent heartbeat: %w", err)
	}
	return nil
}

// Authorize verifies that an active run still owns every requested
// capability. Adapters call this at the boundary where a capability is
// exercised; persisted capability metadata is therefore an enforceable
// contract rather than an informational label. Terminal runs are rejected
// separately so cancellation and cross-process reaping cannot race a later
// tool request back into an active state.
func (m *Manager) Authorize(id RunID, required Capabilities) error {
	if err := m.reconcileExternalTransition(id); err != nil {
		return fmt.Errorf("reconcile agent authorization: %w", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	record, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return fmt.Errorf("%w: %q", ErrRunTerminal, id)
	}
	missing := Capabilities{
		Read:     required.Read && !record.EffectiveCapabilities.Read,
		Write:    required.Write && !record.EffectiveCapabilities.Write,
		Shell:    required.Shell && !record.EffectiveCapabilities.Shell,
		Network:  required.Network && !record.EffectiveCapabilities.Network,
		Dispatch: required.Dispatch && !record.EffectiveCapabilities.Dispatch,
	}
	if missing.any() {
		return fmt.Errorf("%w: required=%#v effective=%#v", ErrCapabilityDenied, required, record.EffectiveCapabilities)
	}
	return nil
}

// RecordAudit appends one bounded operation outcome to an active run. It is a
// persistence boundary: a terminal run cannot receive a late audit entry, and
// a store failure is returned without exposing a partially updated record.
func (m *Manager) RecordAudit(id RunID, operation, outcome, detail string) error {
	operation, err := normalizeAuditToken(operation, "operation")
	if err != nil {
		return err
	}
	outcome, err = normalizeAuditToken(outcome, "outcome")
	if err != nil {
		return err
	}
	detail, err = normalizeAuditDetail(detail)
	if err != nil {
		return err
	}
	if err := m.reconcileExternalTransition(id); err != nil {
		return fmt.Errorf("reconcile agent audit: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return fmt.Errorf("%w: %q", ErrRunTerminal, id)
	}
	previous := cloneRecord(record)
	appendOperationAudit(&record, operation, outcome, detail, m.now())
	m.runs[id] = record
	if err := m.persistLocked(); err != nil {
		m.runs[id] = previous
		return fmt.Errorf("persist agent audit: %w", err)
	}
	return nil
}

// EffectiveCapabilities returns the active run's immutable capability
// snapshot. Execution adapters use it to translate an already-authorized run
// into OS policy (for example, whether a terminal may write in the workspace
// or open network connections). A terminal run cannot reuse stale capability
// metadata after cancellation.
func (m *Manager) EffectiveCapabilities(id RunID) (Capabilities, error) {
	if err := m.reconcileExternalTransition(id); err != nil {
		return Capabilities{}, fmt.Errorf("reconcile agent capabilities: %w", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	record, ok := m.runs[id]
	if !ok {
		return Capabilities{}, fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return Capabilities{}, fmt.Errorf("%w: %q", ErrRunTerminal, id)
	}
	return record.EffectiveCapabilities, nil
}

// ActiveContext returns a cancellation-only view of an active run context.
// Adapters use it to bind descendants such as terminal processes to the run's
// lifecycle. The returned context cannot be used to mutate or complete the
// run; cancellation remains owned by Manager.Cancel, timeout, or stale-run
// recovery.
func (m *Manager) ActiveContext(id RunID) (context.Context, error) {
	if err := m.reconcileExternalTransition(id); err != nil {
		return nil, fmt.Errorf("reconcile agent context: %w", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.runs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return nil, fmt.Errorf("%w: %q", ErrRunTerminal, id)
	}
	handle, ok := m.handles[id]
	if !ok || handle.ctx == nil {
		return nil, fmt.Errorf("%w: %q", ErrRunContext, id)
	}
	return handle.ctx, nil
}

// OutputLimit returns the active run's normalized output budget. Adapters use
// it to cap captured tool output at the boundary where bytes enter their
// process, while still applying their own stricter transport limit. Terminal
// runs are rejected so a caller cannot use a stale budget after cancellation.
func (m *Manager) OutputLimit(id RunID) (int64, error) {
	if err := m.reconcileExternalTransition(id); err != nil {
		return 0, fmt.Errorf("reconcile agent output budget: %w", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	record, ok := m.runs[id]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return 0, fmt.Errorf("%w: %q", ErrRunTerminal, id)
	}
	if record.Spec.Budget.MaxOutputBytes <= 0 {
		return 0, fmt.Errorf("%w: output limit is not positive", ErrBudget)
	}
	return record.Spec.Budget.MaxOutputBytes, nil
}

// ReapStale marks active runs whose heartbeat is older than maxSilence as
// interrupted. A stale parent also cancels its active descendants. The
// operation is explicit so embedders can choose an appropriate supervision
// cadence without adding a hidden goroutine to the runtime.
//
// The returned IDs are the stale roots that were transitioned directly. A
// child included in a returned root's cancellation subtree is not returned a
// second time.
func (m *Manager) ReapStale(maxSilence time.Duration) ([]RunID, error) {
	if maxSilence <= 0 {
		return nil, fmt.Errorf("%w: heartbeat silence must be positive", ErrBudget)
	}
	if err := m.reconcileExternalTransitions(); err != nil {
		return nil, fmt.Errorf("reconcile stale agent runs: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	candidates := make([]RunRecord, 0)
	for _, record := range m.runs {
		if record.Status.terminal() {
			continue
		}
		last := record.LastHeartbeatAt
		if last.IsZero() {
			last = record.StartedAt
		}
		if last.IsZero() || now.Before(last) || now.Sub(last) <= maxSilence {
			continue
		}
		candidates = append(candidates, record)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Depth == candidates[j].Depth {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Depth < candidates[j].Depth
	})

	reaped := make([]RunID, 0, len(candidates))
	for _, candidate := range candidates {
		current, ok := m.runs[candidate.ID]
		if !ok || current.Status.terminal() {
			continue
		}
		message := fmt.Sprintf("agent heartbeat expired after %s", now.Sub(candidate.LastHeartbeatAt))
		if candidate.LastHeartbeatAt.IsZero() {
			message = fmt.Sprintf("agent heartbeat expired after %s", now.Sub(candidate.StartedAt))
		}
		if err := m.finishTreeLocked(candidate.ID, RunInterrupted, message); err != nil {
			return reaped, err
		}
		reaped = append(reaped, candidate.ID)
	}
	return reaped, nil
}

func (m *Manager) watchTimeout(id RunID, ctx context.Context, ownsTimeout bool) {
	<-ctx.Done()
	status := RunCancelled
	if ownsTimeout && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = RunTimedOut
	}
	_ = m.finishTree(id, status, ctx.Err().Error())
}

// watchExternalTransition mirrors terminal transitions written by another
// manager process into this manager's in-memory lifecycle. The durable store
// is the authority for cross-process cancellation; without this small poller,
// an ACP adapter that owns a live handle would keep running after a headless
// supervisor marked the run terminal.
func (m *Manager) watchExternalTransition(id RunID, ctx context.Context) {
	ticker := time.NewTicker(externalSyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = m.reconcileExternalTransition(id)
		}
	}
}

func (m *Manager) reconcileExternalTransition(id RunID) error {
	if m.store == nil {
		return nil
	}
	records, err := m.store.Load()
	if err != nil {
		return err
	}
	var external RunRecord
	found := false
	for _, record := range records {
		if record.ID == id {
			external = cloneRecord(record)
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	local, ok := m.runs[id]
	if !ok {
		return nil
	}
	merged := mergeRunRecord(local, external)
	if local.Status == merged.Status &&
		runRecordHeartbeat(local).Equal(runRecordHeartbeat(merged)) &&
		auditEventsEqual(local.Audit, merged.Audit) {
		return nil
	}
	m.runs[id] = merged
	if merged.Status.terminal() {
		m.closeHandleLocked(id)
	}
	return nil
}

// reconcileExternalTransitions refreshes every locally active record before a
// supervisor operation such as stale reaping. A manager created in
// SkipRecovery mode may not own handles, but its mutating APIs still need to
// observe terminal state written by another process. Active records are also
// refreshed when another process has persisted a newer heartbeat; otherwise a
// supervisor with a stale in-memory snapshot could interrupt a live run.
func (m *Manager) reconcileExternalTransitions() error {
	if m.store == nil {
		return nil
	}
	records, err := m.store.Load()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, external := range records {
		local, ok := m.runs[external.ID]
		if !ok {
			continue
		}
		merged := mergeRunRecord(local, external)
		if local.Status == merged.Status &&
			runRecordHeartbeat(local).Equal(runRecordHeartbeat(merged)) &&
			auditEventsEqual(local.Audit, merged.Audit) {
			continue
		}
		m.runs[external.ID] = merged
		if merged.Status.terminal() {
			m.closeHandleLocked(external.ID)
		}
	}
	return nil
}

// Cancel requests hierarchical cancellation and makes the whole active
// subtree terminal in one serialized transition. Repeated calls are
// idempotent.
func (m *Manager) Cancel(id RunID) error {
	if err := m.reconcileExternalTransition(id); err != nil {
		return fmt.Errorf("reconcile agent cancellation: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finishTreeLocked(id, RunCancelled, context.Canceled.Error())
}

// Complete records a verified handoff. Terminal transitions are idempotent
// only for repeated cancellation; a second completion is rejected.
func (m *Manager) Complete(id RunID, handoff Handoff) error {
	if err := m.reconcileExternalTransition(id); err != nil {
		return fmt.Errorf("reconcile agent completion: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return fmt.Errorf("%w: %q", ErrRunTerminal, id)
	}
	for _, child := range m.runs {
		if child.ParentID == id && !child.Status.terminal() {
			return fmt.Errorf("%w: child %q is still active", ErrActiveChildren, child.ID)
		}
	}
	if err := validateHandoff(record.Spec, handoff); err != nil {
		return err
	}
	return m.finishLocked(id, RunCompleted, "", &handoff)
}

// Fail records a non-successful terminal result.
func (m *Manager) Fail(id RunID, runErr error) error {
	if runErr == nil {
		runErr = errors.New("agent run failed")
	}
	if err := m.reconcileExternalTransition(id); err != nil {
		return fmt.Errorf("reconcile agent failure: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finishTreeLocked(id, RunFailed, runErr.Error())
}

func (m *Manager) finishLocked(id RunID, status RunStatus, message string, handoff *Handoff) error {
	record, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return nil
	}
	previous := cloneRecord(record)
	record.Status = status
	record.Error = message
	record.Handoff = nil
	if handoff != nil {
		record.Handoff = cloneHandoff(handoff)
	}
	record.FinishedAt = m.now()
	appendRunEvent(&record, string(status), message, record.FinishedAt)
	m.runs[id] = record
	if err := m.persistLocked(); err != nil {
		m.runs[id] = previous
		return fmt.Errorf("persist agent completion: %w", err)
	}
	m.closeHandleLocked(id)
	return nil
}

// finishTree marks an active run and all of its active descendants terminal.
// It is used by cancellation, failure, and timeout paths so a parent cannot
// become terminal while a concurrently-started child remains running.
func (m *Manager) finishTree(id RunID, status RunStatus, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.finishTreeLocked(id, status, message)
}

func (m *Manager) finishTreeLocked(id RunID, status RunStatus, message string) error {
	record, ok := m.runs[id]
	if !ok {
		return fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	if record.Status.terminal() {
		return nil
	}
	ids := m.activeDescendantsLocked(id)
	previous := make(map[RunID]RunRecord, len(ids))
	now := m.now()
	for _, runID := range ids {
		record := m.runs[runID]
		previous[runID] = cloneRecord(record)
		record.Status = RunCancelled
		record.Error = "parent run ended"
		if runID == id {
			record.Status = status
			record.Error = message
		}
		record.Handoff = nil
		record.FinishedAt = now
		if runID == id {
			appendRunEvent(&record, string(status), message, now)
		} else {
			appendRunEvent(&record, "parent_ended", "parent run ended", now)
		}
		m.runs[runID] = record
	}
	if err := m.persistLocked(); err != nil {
		for runID, previousRecord := range previous {
			m.runs[runID] = previousRecord
		}
		return fmt.Errorf("persist agent completion: %w", err)
	}
	for _, runID := range ids {
		m.closeHandleLocked(runID)
	}
	return nil
}

func (m *Manager) activeDescendantsLocked(id RunID) []RunID {
	ids := []RunID{id}
	for i := 0; i < len(ids); i++ {
		for childID, child := range m.runs {
			if child.ParentID == ids[i] && !child.Status.terminal() {
				ids = append(ids, childID)
			}
		}
	}
	return ids
}

func (m *Manager) closeHandleLocked(id RunID) {
	if handle, ok := m.handles[id]; ok {
		if handle.cancel != nil {
			handle.cancel()
		}
		close(handle.done)
		delete(m.handles, id)
	}
}

// Get returns an immutable copy of a run record.
func (m *Manager) Get(id RunID) (RunRecord, error) {
	if err := m.reconcileExternalTransition(id); err != nil {
		return RunRecord{}, fmt.Errorf("reconcile agent run: %w", err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.runs[id]
	if !ok {
		return RunRecord{}, fmt.Errorf("%w: %q", ErrRunNotFound, id)
	}
	return cloneRecord(record), nil
}

// ListWithError returns records in creation order and reports a failure while
// refreshing the durable store. Read-only control planes should prefer this
// method so a permissions, I/O, or decode error cannot be mistaken for a
// trustworthy cached snapshot.
func (m *Manager) ListWithError() ([]RunRecord, error) {
	if err := m.reconcileExternalTransitions(); err != nil {
		return nil, fmt.Errorf("reconcile agent runs: %w", err)
	}
	return m.listRecords(), nil
}

func (m *Manager) listRecords() []RunRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]RunRecord, 0, len(m.runs))
	for _, record := range m.runs {
		result = append(result, cloneRecord(record))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func (m *Manager) normalizeSpecLocked(spec RunSpec) (RunSpec, error) {
	// Normalization belongs to the manager. Copy caller-owned slices before
	// trimming or rebuilding them so Start cannot mutate an adapter's request
	// while validating it, and the stored record owns its immutable snapshot.
	spec.DoneCriteria = append([]string(nil), spec.DoneCriteria...)
	spec.Manifest = append([]string(nil), spec.Manifest...)
	spec.Objective = strings.TrimSpace(spec.Objective)
	if spec.Objective == "" || len(spec.Objective) > maxObjectiveBytes {
		return RunSpec{}, fmt.Errorf("%w: objective is empty or too large", ErrInvalidSpec)
	}
	spec.ModelRoute = strings.TrimSpace(spec.ModelRoute)
	if len(spec.ModelRoute) > maxAgentMetadataBytes {
		return RunSpec{}, fmt.Errorf("%w: model route is too large", ErrInvalidSpec)
	}
	spec.ReasoningEffort = strings.TrimSpace(spec.ReasoningEffort)
	if len(spec.ReasoningEffort) > maxAgentMetadataBytes {
		return RunSpec{}, fmt.Errorf("%w: reasoning effort is too large", ErrInvalidSpec)
	}
	if len(spec.DoneCriteria) > maxDoneCriteria {
		return RunSpec{}, fmt.Errorf("%w: too many done criteria", ErrInvalidSpec)
	}
	for i := range spec.DoneCriteria {
		spec.DoneCriteria[i] = strings.TrimSpace(spec.DoneCriteria[i])
		if spec.DoneCriteria[i] == "" || len(spec.DoneCriteria[i]) > maxObjectiveBytes {
			return RunSpec{}, fmt.Errorf("%w: invalid done criterion %d", ErrInvalidSpec, i)
		}
	}
	workspace, err := filepath.Abs(spec.Workspace)
	if err != nil {
		return RunSpec{}, fmt.Errorf("%w: resolve workspace: %v", ErrInvalidSpec, err)
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return RunSpec{}, fmt.Errorf("%w: workspace: %v", ErrInvalidSpec, err)
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return RunSpec{}, fmt.Errorf("%w: resolve workspace symlinks: %v", ErrInvalidSpec, err)
	}
	spec.Workspace = filepath.Clean(workspace)
	if len(spec.Manifest) > maxManifestItems {
		return RunSpec{}, fmt.Errorf("%w: manifest is too large", ErrInvalidSpec)
	}
	manifest := make([]string, 0, len(spec.Manifest))
	seen := make(map[string]struct{}, len(spec.Manifest))
	for _, item := range spec.Manifest {
		item = filepath.Clean(strings.TrimSpace(item))
		if item == "." || filepath.IsAbs(item) || strings.HasPrefix(item, ".."+string(filepath.Separator)) || item == ".." || len(item) > maxPathBytes {
			return RunSpec{}, fmt.Errorf("%w: manifest path %q escapes workspace", ErrInvalidSpec, item)
		}
		if _, exists := seen[item]; exists {
			return RunSpec{}, fmt.Errorf("%w: duplicate manifest path %q", ErrInvalidSpec, item)
		}
		if err := validateWorkspaceManifestPath(spec.Workspace, item); err != nil {
			return RunSpec{}, fmt.Errorf("%w: manifest path %q: %v", ErrInvalidSpec, item, err)
		}
		seen[item] = struct{}{}
		manifest = append(manifest, filepath.ToSlash(item))
	}
	spec.Manifest = manifest
	if spec.Budget.Timeout <= 0 {
		spec.Budget.Timeout = m.maxTimeout
	}
	if spec.Budget.Timeout > m.maxTimeout {
		return RunSpec{}, fmt.Errorf("%w: timeout exceeds %s", ErrBudget, m.maxTimeout)
	}
	if spec.Budget.MaxChildren <= 0 {
		spec.Budget.MaxChildren = m.maxConcurrentChildren
	}
	if spec.Budget.MaxChildren > m.maxConcurrentChildren {
		return RunSpec{}, fmt.Errorf("%w: max children exceeds %d", ErrBudget, m.maxConcurrentChildren)
	}
	if spec.Budget.MaxOutputBytes <= 0 {
		spec.Budget.MaxOutputBytes = m.maxOutputBytes
	}
	if spec.Budget.MaxOutputBytes > m.maxOutputBytes {
		return RunSpec{}, fmt.Errorf("%w: output limit exceeds %d bytes", ErrBudget, m.maxOutputBytes)
	}
	spec.RequestedCapabilities = spec.RequestedCapabilities.normalized()
	return spec, nil
}

func validateHandoff(spec RunSpec, handoff Handoff) error {
	if !handoff.Verified {
		return fmt.Errorf("%w: verification is required", ErrInvalidHandoff)
	}
	handoff.Summary = strings.TrimSpace(handoff.Summary)
	if handoff.Summary == "" || len(handoff.Summary) > maxHandoffBytes {
		return fmt.Errorf("%w: summary is empty or too large", ErrInvalidHandoff)
	}
	if len(handoff.Verification) == 0 {
		return fmt.Errorf("%w: at least one verification is required", ErrInvalidHandoff)
	}
	if len(handoff.Verification) > maxVerificationItems {
		return fmt.Errorf("%w: too many verification entries", ErrInvalidHandoff)
	}
	verificationBytes := 0
	for index, verification := range handoff.Verification {
		if strings.TrimSpace(verification) == "" || len(verification) > maxVerificationItemBytes {
			return fmt.Errorf("%w: invalid verification entry %d", ErrInvalidHandoff, index)
		}
		verificationBytes += len(verification) + 1
		if verificationBytes > maxVerificationTotalBytes {
			return fmt.Errorf("%w: verification entries are too large", ErrInvalidHandoff)
		}
	}
	allowed := make(map[string]struct{}, len(spec.DoneCriteria))
	for _, criterion := range spec.DoneCriteria {
		allowed[criterion] = struct{}{}
	}
	if len(handoff.CompletedCriteria) > len(spec.DoneCriteria) {
		return fmt.Errorf("%w: too many completed criteria", ErrInvalidHandoff)
	}
	seenCriteria := make(map[string]struct{}, len(handoff.CompletedCriteria))
	for _, criterion := range handoff.CompletedCriteria {
		if _, ok := allowed[criterion]; !ok {
			return fmt.Errorf("%w: criterion %q was not requested", ErrInvalidHandoff, criterion)
		}
		if _, duplicate := seenCriteria[criterion]; duplicate {
			return fmt.Errorf("%w: duplicate criterion %q", ErrInvalidHandoff, criterion)
		}
		seenCriteria[criterion] = struct{}{}
	}
	for _, criterion := range spec.DoneCriteria {
		if _, ok := seenCriteria[criterion]; !ok {
			return fmt.Errorf("%w: criterion %q was not completed", ErrInvalidHandoff, criterion)
		}
	}
	if len(handoff.Artifacts) > maxArtifacts {
		return fmt.Errorf("%w: too many artifacts", ErrInvalidHandoff)
	}
	seenArtifacts := make(map[string]struct{}, len(handoff.Artifacts))
	for _, artifact := range handoff.Artifacts {
		if len(artifact.Kind) > maxAgentMetadataBytes {
			return fmt.Errorf("%w: artifact kind is too large", ErrInvalidHandoff)
		}
		path := filepath.Clean(strings.TrimSpace(artifact.Path))
		if path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || len(path) > maxPathBytes {
			return fmt.Errorf("%w: artifact path %q escapes workspace", ErrInvalidHandoff, artifact.Path)
		}
		if _, duplicate := seenArtifacts[path]; duplicate {
			return fmt.Errorf("%w: duplicate artifact %q", ErrInvalidHandoff, path)
		}
		seenArtifacts[path] = struct{}{}
		if len(artifact.SHA256) != 64 {
			return fmt.Errorf("%w: artifact %q has no SHA-256", ErrInvalidHandoff, path)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return fmt.Errorf("%w: artifact %q has invalid SHA-256", ErrInvalidHandoff, path)
		}
		if err := validateArtifactFile(spec.Workspace, path, artifact.SHA256); err != nil {
			return fmt.Errorf("%w: artifact %q: %v", ErrInvalidHandoff, path, err)
		}
	}
	return nil
}

func validateWorkspaceManifestPath(workspace, relative string) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return fmt.Errorf("open workspace root: %w", err)
	}
	defer func() { _ = root.Close() }()
	relative = filepath.ToSlash(filepath.Clean(relative))
	if _, err := root.Stat(relative); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("path escapes workspace or cannot be inspected: %w", err)
	}
	parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(relative)))
	if parent == "" {
		parent = "."
	}
	parentInfo, err := root.Stat(parent)
	if err != nil {
		return err
	}
	if !parentInfo.IsDir() {
		return errors.New("manifest parent is not a directory")
	}
	return nil
}

func validateArtifactFile(workspace, relative, expectedDigest string) error {
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return fmt.Errorf("open workspace root: %w", err)
	}
	defer func() { _ = root.Close() }()
	relative = filepath.ToSlash(filepath.Clean(relative))
	info, err := root.Stat(relative)
	if err != nil {
		return fmt.Errorf("stat artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("artifact is not a regular file")
	}
	if info.Size() > maxArtifactBytes {
		return fmt.Errorf("artifact exceeds %d-byte limit", maxArtifactBytes)
	}
	file, err := root.Open(relative)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxArtifactBytes+1))
	if err != nil {
		return fmt.Errorf("hash artifact: %w", err)
	}
	if written > maxArtifactBytes {
		return fmt.Errorf("artifact exceeds %d-byte limit", maxArtifactBytes)
	}
	actualDigest := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actualDigest, expectedDigest) {
		return fmt.Errorf("digest mismatch: got %s", actualDigest)
	}
	return nil
}

func (m *Manager) persistLocked() error {
	if m.store == nil {
		return nil
	}
	records := make([]RunRecord, 0, len(m.runs))
	for _, record := range m.runs {
		records = append(records, cloneRecord(record))
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	return m.store.Save(records)
}

func newRunID() (RunID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return RunID("run_" + hex.EncodeToString(raw[:])), nil
}

func cloneRecords(records []RunRecord) []RunRecord {
	result := make([]RunRecord, 0, len(records))
	for _, record := range records {
		result = append(result, cloneRecord(record))
	}
	return result
}

// List returns records in creation order. It preserves the historical
// best-effort API for embedders that cannot handle an error; new adapters that
// present durable state to users should use ListWithError instead.
func (m *Manager) List() []RunRecord {
	// Keep the old best-effort behavior: consumers that cannot handle an
	// error still receive the last in-memory snapshot when the external store
	// is temporarily unavailable.
	_ = m.reconcileExternalTransitions()
	return m.listRecords()
}

func cloneRecord(record RunRecord) RunRecord {
	record.Spec.DoneCriteria = append([]string(nil), record.Spec.DoneCriteria...)
	record.Spec.Manifest = append([]string(nil), record.Spec.Manifest...)
	record.Handoff = cloneHandoff(record.Handoff)
	record.Events = cloneRunEvents(record.Events)
	record.Audit = cloneOperationAudit(record.Audit)
	return record
}

func cloneRunEvents(events []RunEvent) []RunEvent {
	if len(events) == 0 {
		return nil
	}
	start := 0
	if len(events) > maxRunEvents {
		start = len(events) - maxRunEvents
	}
	cloned := make([]RunEvent, 0, len(events)-start)
	for _, event := range events[start:] {
		event.Type = strings.TrimSpace(event.Type)
		if len(event.Detail) > maxRunEventDetailBytes {
			event.Detail = event.Detail[:maxRunEventDetailBytes]
		}
		cloned = append(cloned, event)
	}
	return cloned
}

func appendRunEvent(record *RunRecord, eventType, detail string, at time.Time) {
	if record == nil || strings.TrimSpace(eventType) == "" {
		return
	}
	detail = strings.TrimSpace(detail)
	if len(detail) > maxRunEventDetailBytes {
		detail = detail[:maxRunEventDetailBytes]
	}
	if len(record.Events) >= maxRunEvents {
		copy(record.Events, record.Events[len(record.Events)-maxRunEvents+1:])
		record.Events = record.Events[:maxRunEvents-1]
	}
	record.Events = append(record.Events, RunEvent{
		Type:   eventType,
		At:     at,
		Detail: detail,
	})
}

func cloneOperationAudit(audit []OperationAuditEvent) []OperationAuditEvent {
	if len(audit) == 0 {
		return nil
	}
	start := 0
	if len(audit) > maxRunAuditEvents {
		start = len(audit) - maxRunAuditEvents
	}
	cloned := make([]OperationAuditEvent, 0, len(audit)-start)
	for _, event := range audit[start:] {
		event.Operation, _ = normalizeAuditToken(event.Operation, "operation")
		event.Outcome, _ = normalizeAuditToken(event.Outcome, "outcome")
		event.Detail, _ = normalizeAuditDetail(event.Detail)
		if event.Operation == "" || event.Outcome == "" {
			continue
		}
		cloned = append(cloned, event)
	}
	return cloned
}

func appendOperationAudit(record *RunRecord, operation, outcome, detail string, at time.Time) {
	if record == nil || operation == "" || outcome == "" {
		return
	}
	if len(record.Audit) >= maxRunAuditEvents {
		copy(record.Audit, record.Audit[len(record.Audit)-maxRunAuditEvents+1:])
		record.Audit = record.Audit[:maxRunAuditEvents-1]
	}
	record.Audit = append(record.Audit, OperationAuditEvent{
		Operation: operation,
		Outcome:   outcome,
		At:        at,
		Detail:    detail,
	})
}

func normalizeAuditToken(value, name string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", fmt.Errorf("%s is empty", name)
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("%s contains invalid UTF-8", name)
	}
	if len(value) > maxRunAuditTokenBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", name, maxRunAuditTokenBytes)
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.' {
			return "", fmt.Errorf("%s contains unsupported characters", name)
		}
	}
	return value, nil
}

func normalizeAuditDetail(detail string) (string, error) {
	detail = strings.TrimSpace(detail)
	if !utf8.ValidString(detail) {
		return "", fmt.Errorf("audit detail contains invalid UTF-8")
	}
	if len(detail) > maxRunAuditDetailBytes {
		detail = detail[:maxRunAuditDetailBytes]
		for len(detail) > 0 && !utf8.ValidString(detail) {
			detail = detail[:len(detail)-1]
		}
	}
	if strings.ContainsAny(detail, "\x00\r\n") {
		return "", fmt.Errorf("audit detail contains unsupported control characters")
	}
	return detail, nil
}

func cloneHandoff(handoff *Handoff) *Handoff {
	if handoff == nil {
		return nil
	}
	copyHandoff := *handoff
	copyHandoff.CompletedCriteria = append([]string(nil), handoff.CompletedCriteria...)
	copyHandoff.Verification = append([]string(nil), handoff.Verification...)
	copyHandoff.Artifacts = append([]Artifact(nil), handoff.Artifacts...)
	return &copyHandoff
}
