package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestActiveContextTracksManagerCancellation(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}

	ctx, err := m.ActiveContext(handle.ID)
	if err != nil {
		t.Fatalf("ActiveContext() error = %v", err)
	}
	select {
	case <-ctx.Done():
		t.Fatal("active run context is already cancelled")
	default:
	}

	if _, err := m.ActiveContext(RunID("missing")); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("ActiveContext(missing) error = %v, want ErrRunNotFound", err)
	}
	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("cancelled context error = %v, want context.Canceled", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("ActiveContext() did not observe manager cancellation")
	}
	if _, err := m.ActiveContext(handle.ID); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("ActiveContext(terminal) error = %v, want ErrRunTerminal", err)
	}
}

func TestValidateWorkspaceManifestPathConfinesExistingAndNewPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, relative := range []string{"src/main.go", "src/new.go", "src/../missing.go"} {
		if err := validateWorkspaceManifestPath(root, relative); err != nil {
			t.Fatalf("validateWorkspaceManifestPath(%q) error = %v", relative, err)
		}
	}
	for _, tt := range []struct {
		relative string
		wantPart string
	}{
		{relative: "missing/new.go", wantPart: "no such file"},
		{relative: "src/main.go/child", wantPart: "not a directory"},
	} {
		t.Run(tt.relative, func(t *testing.T) {
			err := validateWorkspaceManifestPath(root, tt.relative)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.wantPart) {
				t.Fatalf("validateWorkspaceManifestPath(%q) error = %v, want %q", tt.relative, err, tt.wantPart)
			}
		})
	}
	for _, relative := range []string{"../outside.go", "/tmp/outside.go"} {
		if err := validateWorkspaceManifestPath(root, relative); err == nil {
			t.Fatalf("validateWorkspaceManifestPath(%q) accepted an escaping path", relative)
		}
	}
}

func testSpec(root string) RunSpec {
	return RunSpec{
		Objective:    "inspect the project",
		Workspace:    root,
		DoneCriteria: []string{"report findings"},
	}
}

func verifiedHandoff() Handoff {
	return Handoff{
		Summary:           "inspection completed",
		CompletedCriteria: []string{"report findings"},
		Verification:      []string{"fixture assertion"},
		Verified:          true,
	}
}

func TestStartDefaultsToReadOnlyAndRecordsImmutableRequest(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunRunning {
		t.Fatalf("status = %q, want running", record.Status)
	}
	if len(record.Events) != 1 || record.Events[0].Type != "started" {
		t.Fatalf("start events = %#v, want one started event", record.Events)
	}
	if !record.Spec.RequestedCapabilities.Read || record.Spec.RequestedCapabilities.Write {
		t.Fatalf("requested capabilities = %#v, want read-only", record.Spec.RequestedCapabilities)
	}
	if !record.EffectiveCapabilities.Read || record.EffectiveCapabilities.Write {
		t.Fatalf("effective capabilities = %#v, want read-only", record.EffectiveCapabilities)
	}
	if record.Spec.Budget.MaxChildren != defaultMaxConcurrentChild || record.Spec.Budget.MaxOutputBytes != defaultMaxOutputBytes {
		t.Fatalf("budget = %#v, want safe defaults", record.Spec.Budget)
	}

	if err := m.Complete(handle.ID, verifiedHandoff()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	select {
	case <-handle.Done:
	case <-time.After(time.Second):
		t.Fatal("run handle did not close after completion")
	}
	completed, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != RunCompleted || completed.Handoff == nil || !completed.Handoff.Verified {
		t.Fatalf("completed record = %#v", completed)
	}
	if len(completed.Events) != 2 || completed.Events[1].Type != "completed" {
		t.Fatalf("completion events = %#v, want started/completed", completed.Events)
	}
	if err := m.Complete(handle.ID, verifiedHandoff()); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("second Complete() error = %v, want ErrRunTerminal", err)
	}
}

func TestRunEventsAreBoundedAndCopied(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Events) != 2 || record.Events[1].Type != string(RunCancelled) {
		t.Fatalf("cancel events = %#v, want started/cancelled", record.Events)
	}
	record.Events[0].Type = "caller mutation"
	again, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Events[0].Type != "started" {
		t.Fatalf("Get() exposed mutable events: %#v", again.Events)
	}

	for i := 0; i < maxRunEvents+8; i++ {
		appendRunEvent(&again, "synthetic", strings.Repeat("x", maxRunEventDetailBytes+10), time.Unix(int64(i), 0))
	}
	if len(again.Events) != maxRunEvents {
		t.Fatalf("bounded events = %d, want %d", len(again.Events), maxRunEvents)
	}
	if len(again.Events[len(again.Events)-1].Detail) > maxRunEventDetailBytes {
		t.Fatalf("event detail length = %d, want <= %d", len(again.Events[len(again.Events)-1].Detail), maxRunEventDetailBytes)
	}
}

func TestOperationAuditIsBoundedAndCopied(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RecordAudit(handle.ID, "terminal", "started", "sandbox=disabled"); err != nil {
		t.Fatalf("RecordAudit() error = %v", err)
	}
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Audit) != 1 || record.Audit[0].Operation != "terminal" || record.Audit[0].Outcome != "started" {
		t.Fatalf("audit = %#v, want one terminal start entry", record.Audit)
	}
	record.Audit[0].Detail = "caller mutation"
	again, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Audit[0].Detail != "sandbox=disabled" {
		t.Fatalf("Get() exposed mutable audit = %#v", again.Audit)
	}

	for i := 0; i < maxRunAuditEvents+8; i++ {
		if err := m.RecordAudit(handle.ID, "terminal", "step", strings.Repeat("x", maxRunAuditDetailBytes+10)); err != nil {
			t.Fatalf("RecordAudit(%d) error = %v", i, err)
		}
	}
	again, err = m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Audit) != maxRunAuditEvents {
		t.Fatalf("bounded audit = %d, want %d", len(again.Audit), maxRunAuditEvents)
	}
	if len(again.Audit[len(again.Audit)-1].Detail) > maxRunAuditDetailBytes {
		t.Fatalf("audit detail length = %d, want <= %d", len(again.Audit[len(again.Audit)-1].Detail), maxRunAuditDetailBytes)
	}
	detail, err := normalizeAuditDetail(strings.Repeat("🙂", 200))
	if err != nil {
		t.Fatalf("normalizeAuditDetail() error = %v", err)
	}
	if len(detail) > maxRunAuditDetailBytes || !utf8.ValidString(detail) {
		t.Fatalf("normalized UTF-8 detail length=%d valid=%t", len(detail), utf8.ValidString(detail))
	}
	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordAudit(handle.ID, "terminal", "late", "must not record"); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("RecordAudit(terminal) error = %v, want ErrRunTerminal", err)
	}
}

func TestMergeRunRecordPreservesDistinctOperationAudit(t *testing.T) {
	base := time.Unix(100, 0)
	existing := RunRecord{
		ID:     "run_audit_merge",
		Status: RunRunning,
		Audit:  []OperationAuditEvent{{Operation: "terminal", Outcome: "started", At: base}},
	}
	candidate := RunRecord{
		ID:     existing.ID,
		Status: RunRunning,
		Audit: []OperationAuditEvent{
			{Operation: "terminal", Outcome: "started", At: base},
			{Operation: "terminal", Outcome: "exited", At: base.Add(time.Second), Detail: "exit_code=0"},
		},
	}

	merged := mergeRunRecord(existing, candidate)
	if len(merged.Audit) != 2 {
		t.Fatalf("merged audit = %#v, want duplicate-free union", merged.Audit)
	}
	if merged.Audit[0].Outcome != "started" || merged.Audit[1].Outcome != "exited" {
		t.Fatalf("merged audit order = %#v, want started then exited", merged.Audit)
	}
}

func TestManagerRefreshesAuditFromAnotherWriter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "runs.json")
	first, err := NewManager(ManagerConfig{Store: FileStore{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := first.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(ManagerConfig{Store: FileStore{Path: path}, SkipRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.RecordAudit(handle.ID, "terminal", "started", "sandbox=disabled"); err != nil {
		t.Fatal(err)
	}
	record, err := second.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Audit) != 1 || record.Audit[0].Outcome != "started" {
		t.Fatalf("refreshed audit = %#v, want audit from another writer", record.Audit)
	}
}

func TestStartDoesNotMutateCallerSpec(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	doneCriteria := []string{"  report findings  "}
	spec := testSpec(root)
	spec.DoneCriteria = doneCriteria
	handle, err := m.Start(spec)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if doneCriteria[0] != "  report findings  " {
		t.Fatalf("Start() mutated caller criteria to %q", doneCriteria[0])
	}
	doneCriteria[0] = "mutated after Start"
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := record.Spec.DoneCriteria[0]; got != "report findings" {
		t.Fatalf("stored criteria = %q, want normalized manager-owned copy", got)
	}
	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteCopiesHandoffSlices(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}

	completedCriteria := []string{"report findings"}
	verification := []string{"go test ./..."}
	handoff := Handoff{
		Summary:           "inspection completed",
		CompletedCriteria: completedCriteria,
		Verification:      verification,
		Verified:          true,
	}
	if err := m.Complete(handle.ID, handoff); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	// Complete accepts a value, but its slices are still caller-owned unless
	// the runtime clones them before storing the terminal record.
	completedCriteria[0] = "caller mutated criteria"
	verification[0] = "caller mutated verification"

	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := record.Handoff.CompletedCriteria[0]; got != "report findings" {
		t.Fatalf("stored completed criterion = %q, want original value", got)
	}
	if got := record.Handoff.Verification[0]; got != "go test ./..." {
		t.Fatalf("stored verification = %q, want original value", got)
	}
}

func TestCompleteRejectsOversizedVerificationList(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}

	verification := make([]string, 129)
	for i := range verification {
		verification[i] = "check"
	}
	handoff := verifiedHandoff()
	handoff.Verification = verification
	if err := m.Complete(handle.ID, handoff); !errors.Is(err, ErrInvalidHandoff) {
		t.Fatalf("Complete() error = %v, want ErrInvalidHandoff", err)
	}
}

func TestStartRejectsOversizedRoutingMetadata(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		set  func(*RunSpec)
		want string
	}{
		{
			name: "model route",
			set: func(spec *RunSpec) {
				spec.ModelRoute = strings.Repeat("x", (4<<10)+1)
			},
			want: "model route",
		},
		{
			name: "reasoning effort",
			set: func(spec *RunSpec) {
				spec.ReasoningEffort = strings.Repeat("x", (4<<10)+1)
			},
			want: "reasoning effort",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := testSpec(root)
			tt.set(&spec)
			_, err := m.Start(spec)
			if !errors.Is(err, ErrInvalidSpec) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Start() error = %v, want invalid %s metadata", err, tt.want)
			}
		})
	}
}

func TestCompleteRejectsOversizedArtifactMetadataAndCriteria(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}

	tooManyCriteria := verifiedHandoff()
	tooManyCriteria.CompletedCriteria = make([]string, maxDoneCriteria+1)
	for i := range tooManyCriteria.CompletedCriteria {
		tooManyCriteria.CompletedCriteria[i] = "report findings"
	}
	if err := m.Complete(handle.ID, tooManyCriteria); !errors.Is(err, ErrInvalidHandoff) || !strings.Contains(err.Error(), "too many completed criteria") {
		t.Fatalf("Complete() criteria error = %v, want bounded criteria rejection", err)
	}

	tooManyMetadata := verifiedHandoff()
	tooManyMetadata.Artifacts = []Artifact{{
		Path:   "report.json",
		Kind:   strings.Repeat("x", (4<<10)+1),
		SHA256: strings.Repeat("a", 64),
	}}
	if err := m.Complete(handle.ID, tooManyMetadata); !errors.Is(err, ErrInvalidHandoff) || !strings.Contains(err.Error(), "artifact kind") {
		t.Fatalf("Complete() artifact metadata error = %v, want bounded kind rejection", err)
	}

	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNewManagerRejectsUnboundedGlobalLimits(t *testing.T) {
	tests := []struct {
		name string
		cfg  ManagerConfig
	}{
		{name: "depth", cfg: ManagerConfig{MaxDepth: maxManagerDepth + 1}},
		{name: "children", cfg: ManagerConfig{MaxConcurrentChildren: maxManagerConcurrentChildren + 1}},
		{name: "output", cfg: ManagerConfig{MaxOutputBytes: maxManagerOutputBytes + 1}},
		{name: "timeout", cfg: ManagerConfig{MaxTimeout: maxManagerTimeout + time.Nanosecond}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewManager(tt.cfg)
			if !errors.Is(err, ErrBudget) {
				t.Fatalf("NewManager(%#v) error = %v, want ErrBudget", tt.cfg, err)
			}
			if manager != nil {
				t.Fatal("NewManager returned a manager for an unbounded configuration")
			}
		})
	}
}

func TestNewManagerAcceptsHardGlobalLimits(t *testing.T) {
	manager, err := NewManager(ManagerConfig{
		MaxDepth:              maxManagerDepth,
		MaxConcurrentChildren: maxManagerConcurrentChildren,
		MaxOutputBytes:        maxManagerOutputBytes,
		MaxTimeout:            maxManagerTimeout,
	})
	if err != nil {
		t.Fatalf("NewManager() at hard limits error = %v", err)
	}
	if manager == nil {
		t.Fatal("NewManager() at hard limits returned nil manager")
	}
}

func TestReadOnlyCapabilitiesAndWorkspaceStorePathAreStable(t *testing.T) {
	if got := ReadOnlyCapabilities(); got != (Capabilities{Read: true}) {
		t.Fatalf("ReadOnlyCapabilities() = %#v, want read-only capability", got)
	}
	stateHome := t.TempDir()
	first := WorkspaceStorePath(filepath.Join("/tmp", "project", "."), stateHome)
	second := WorkspaceStorePath("/tmp/project", stateHome)
	if first != second || !strings.HasPrefix(first, filepath.Join(stateHome, "agent-runs")) || !strings.HasSuffix(first, filepath.Join("runs.json")) {
		t.Fatalf("WorkspaceStorePath() = %q and %q, want stable state-home path", first, second)
	}
}

func TestWorkspaceStorePathCanonicalizesSymlinkAliases(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "project")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "project-link")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stateHome := filepath.Join(base, "state")
	realPath := WorkspaceStorePath(realRoot, stateHome)
	aliasPath := WorkspaceStorePath(alias, stateHome)
	if realPath != aliasPath {
		t.Fatalf("WorkspaceStorePath(real) = %q, alias = %q; want one durable workspace identity", realPath, aliasPath)
	}
}

func TestResolveWorkspaceStorePathFallsBackToLegacyAlias(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "project")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "project-link")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	stateHome := filepath.Join(base, "state")
	legacyPath := LegacyWorkspaceStorePath(alias, stateHome)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := ResolveWorkspaceStorePath(alias, stateHome), legacyPath; got != want {
		t.Fatalf("ResolveWorkspaceStorePath() = %q, want legacy %q", got, want)
	}
}

func TestListReturnsCreationOrderAndImmutableCopies(t *testing.T) {
	root := t.TempDir()
	current := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	m, err := NewManager(ManagerConfig{Now: func() time.Time {
		value := current
		current = current.Add(time.Millisecond)
		return value
	}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	records := m.List()
	if len(records) != 2 || records[0].ID != first.ID || records[1].ID != second.ID {
		t.Fatalf("List() = %#v, want creation order [%q, %q]", records, first.ID, second.ID)
	}
	records[0].Spec.DoneCriteria[0] = "mutated outside manager"
	records = m.List()
	if records[0].Spec.DoneCriteria[0] != "report findings" {
		t.Fatal("List() exposed mutable nested run data")
	}
	if err := m.Cancel(first.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(second.ID); err != nil {
		t.Fatal(err)
	}
}

func TestListWithErrorReportsExternalStoreFailure(t *testing.T) {
	root := t.TempDir()
	store := &failingStore{}
	m, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(testSpec(root)); err != nil {
		t.Fatal(err)
	}
	store.setLoadFail(true)

	if _, err := m.ListWithError(); err == nil || !strings.Contains(err.Error(), "fixture load failure") {
		t.Fatalf("ListWithError() error = %v, want fixture load failure", err)
	}
	// Preserve the historical best-effort API for existing embedders.
	if got := m.List(); len(got) != 1 || got[0].Status != RunRunning {
		t.Fatalf("List() after external failure = %#v, want cached running record", got)
	}
}

func TestFailTransitionsRunAndClosesHandle(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Fail(handle.ID, errors.New("fixture failed")); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	select {
	case <-handle.Done:
	case <-time.After(time.Second):
		t.Fatal("failed run handle did not close")
	}
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunFailed || record.Error != "fixture failed" || record.FinishedAt.IsZero() {
		t.Fatalf("failed record = %#v, want terminal error", record)
	}
	if err := m.Fail(handle.ID, errors.New("second failure")); err != nil {
		t.Fatalf("repeated Fail() error = %v, want idempotent terminal transition", err)
	}
	if err := m.Fail(RunID("missing"), errors.New("missing")); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("Fail(missing) error = %v, want ErrRunNotFound", err)
	}
}

func TestChildCapabilitiesAreIntersectedAndChildrenAreBounded(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{MaxConcurrentChildren: 2})
	if err != nil {
		t.Fatal(err)
	}
	parentSpec := testSpec(root)
	parentSpec.RequestedCapabilities = Capabilities{Read: true, Write: true, Dispatch: true}
	parentSpec.Budget.MaxChildren = 1
	parent, err := m.Start(parentSpec)
	if err != nil {
		t.Fatal(err)
	}

	childSpec := RunSpec{
		Objective:             "inspect child scope",
		Workspace:             root,
		ParentID:              parent.ID,
		RequestedCapabilities: Capabilities{Read: true, Write: true, Shell: true},
	}
	child, err := m.Start(childSpec)
	if err != nil {
		t.Fatalf("child Start() error = %v", err)
	}
	childRecord, err := m.Get(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !childRecord.Spec.RequestedCapabilities.Shell || childRecord.EffectiveCapabilities.Shell {
		t.Fatalf("child capabilities = spec=%#v effective=%#v", childRecord.Spec.RequestedCapabilities, childRecord.EffectiveCapabilities)
	}
	if !childRecord.EffectiveCapabilities.Read || !childRecord.EffectiveCapabilities.Write {
		t.Fatalf("child lost inherited read/write capability: %#v", childRecord.EffectiveCapabilities)
	}

	if _, err := m.Start(childSpec); !errors.Is(err, ErrChildLimit) {
		t.Fatalf("second child error = %v, want ErrChildLimit", err)
	}
	grandchild := childSpec
	grandchild.ParentID = child.ID
	if _, err := m.Start(grandchild); !errors.Is(err, ErrBudget) {
		t.Fatalf("grandchild error = %v, want depth ErrBudget", err)
	}

	if err := m.Cancel(parent.ID); err != nil {
		t.Fatalf("Cancel(parent) error = %v", err)
	}
	parentRecord, _ := m.Get(parent.ID)
	childRecord, _ = m.Get(child.ID)
	if parentRecord.Status != RunCancelled || childRecord.Status != RunCancelled {
		t.Fatalf("cancelled statuses = parent=%q child=%q", parentRecord.Status, childRecord.Status)
	}
}

func TestChildOutputBudgetCannotExceedParent(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	parentSpec := testSpec(root)
	parentSpec.Budget.MaxOutputBytes = 128
	parent, err := m.Start(parentSpec)
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.Start(RunSpec{
		Objective: "child output",
		Workspace: root,
		ParentID:  parent.ID,
		Budget:    Budget{MaxOutputBytes: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := m.Get(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := record.Spec.Budget.MaxOutputBytes; got != 128 {
		t.Fatalf("child output budget = %d, want inherited parent ceiling 128", got)
	}
	if got, err := m.OutputLimit(child.ID); err != nil || got != 128 {
		t.Fatalf("child OutputLimit() = %d, %v; want 128, nil", got, err)
	}
	if err := m.Cancel(parent.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeRequiresActiveEffectiveCapabilities(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec(root)
	spec.RequestedCapabilities = Capabilities{Read: true, Write: true}
	handle, err := m.Start(spec)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.Authorize(handle.ID, Capabilities{Read: true}); err != nil {
		t.Fatalf("Authorize(read) error = %v", err)
	}
	if err := m.Authorize(handle.ID, Capabilities{Shell: true}); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("Authorize(shell) error = %v, want ErrCapabilityDenied", err)
	}

	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Authorize(handle.ID, Capabilities{Read: true}); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("Authorize after cancellation error = %v, want ErrRunTerminal", err)
	}
}

func TestEffectiveCapabilitiesReturnsActiveSnapshotAndRejectsTerminalRun(t *testing.T) {
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(RunSpec{
		Objective:             "inspect capabilities",
		Workspace:             t.TempDir(),
		RequestedCapabilities: Capabilities{Read: true, Shell: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	caps, err := m.EffectiveCapabilities(handle.ID)
	if err != nil {
		t.Fatalf("EffectiveCapabilities() error = %v", err)
	}
	if !caps.Read || !caps.Shell || caps.Write || caps.Network {
		t.Fatalf("EffectiveCapabilities() = %#v, want read/shell only", caps)
	}
	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EffectiveCapabilities(handle.ID); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("EffectiveCapabilities() after cancel = %v, want ErrRunTerminal", err)
	}
}

func TestOutputLimitReturnsNormalizedBudgetAndRejectsTerminalRun(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec(root)
	spec.Budget.MaxOutputBytes = 128
	handle, err := m.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := m.OutputLimit(handle.ID); err != nil || got != 128 {
		t.Fatalf("OutputLimit() = %d, %v; want 128, nil", got, err)
	}
	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.OutputLimit(handle.ID); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("OutputLimit() after cancellation error = %v, want ErrRunTerminal", err)
	}
}

func TestParentCannotCompleteWithActiveChildren(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.Start(RunSpec{Objective: "child", Workspace: root, ParentID: parent.ID, DoneCriteria: []string{"report findings"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Complete(parent.ID, verifiedHandoff()); !errors.Is(err, ErrActiveChildren) {
		t.Fatalf("parent completion error = %v, want ErrActiveChildren", err)
	}
	if err := m.Complete(child.ID, verifiedHandoff()); err != nil {
		t.Fatalf("child completion error = %v", err)
	}
	if err := m.Complete(parent.ID, verifiedHandoff()); err != nil {
		t.Fatalf("parent completion after child error = %v", err)
	}
}

func TestTimeoutBecomesTerminalAndClosesHandle(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{MaxTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec(root)
	spec.Budget.Timeout = 10 * time.Millisecond
	handle, err := m.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-handle.Done:
	case <-time.After(time.Second):
		t.Fatal("timed-out run did not close handle")
	}
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunTimedOut {
		t.Fatalf("status = %q, want timed_out", record.Status)
	}
}

func TestHeartbeatUpdatesPersistedLiveness(t *testing.T) {
	root := t.TempDir()
	store := &failingStore{}
	current := time.Unix(100, 0)
	m, err := NewManager(ManagerConfig{
		Store: store,
		Now:   func() time.Time { return current },
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	started, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !started.LastHeartbeatAt.Equal(current) {
		t.Fatalf("start heartbeat = %v, want %v", started.LastHeartbeatAt, current)
	}

	current = current.Add(15 * time.Second)
	if err := m.Heartbeat(handle.ID); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	updated, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.LastHeartbeatAt.Equal(current) {
		t.Fatalf("updated heartbeat = %v, want %v", updated.LastHeartbeatAt, current)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || !loaded[0].LastHeartbeatAt.Equal(current) {
		t.Fatalf("persisted heartbeat = %#v, want %v", loaded, current)
	}
	_ = m.Cancel(handle.ID)
}

func TestHeartbeatPersistenceFailureRollsBack(t *testing.T) {
	root := t.TempDir()
	current := time.Unix(200, 0)
	store := &failingStore{}
	m, err := NewManager(ManagerConfig{
		Store: store,
		Now:   func() time.Time { return current },
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	before, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(time.Minute)
	store.setFail(true)
	if err := m.Heartbeat(handle.ID); err == nil {
		t.Fatal("Heartbeat() unexpectedly succeeded with a failing store")
	}
	after, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastHeartbeatAt.Equal(before.LastHeartbeatAt) {
		t.Fatalf("heartbeat after failed persistence = %v, want rollback to %v", after.LastHeartbeatAt, before.LastHeartbeatAt)
	}
	store.setFail(false)
	_ = m.Cancel(handle.ID)
}

func TestFileStoreSavePreservesRunsWrittenByAnotherProcess(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runs.json")
	store := FileStore{Path: path}
	first := RunRecord{ID: "run_first", Status: RunRunning, StartedAt: time.Unix(10, 0)}
	second := RunRecord{ID: "run_second", Status: RunRunning, StartedAt: time.Unix(20, 0)}
	if err := store.Save([]RunRecord{first}); err != nil {
		t.Fatal(err)
	}
	if err := (FileStore{Path: path}).Save([]RunRecord{second}); err != nil {
		t.Fatal(err)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ID != first.ID || records[1].ID != second.ID {
		t.Fatalf("merged records = %#v, want both independent runs", records)
	}
}

func TestFileStoreSaveDoesNotReopenTerminalRunFromStaleSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runs.json")
	store := FileStore{Path: path}
	started := RunRecord{
		ID:              "run_terminal",
		Status:          RunRunning,
		StartedAt:       time.Unix(10, 0),
		LastHeartbeatAt: time.Unix(10, 0),
	}
	if err := store.Save([]RunRecord{started}); err != nil {
		t.Fatal(err)
	}
	completed := started
	completed.Status = RunCompleted
	completed.FinishedAt = time.Unix(30, 0)
	if err := store.Save([]RunRecord{completed}); err != nil {
		t.Fatal(err)
	}
	staleRunning := started
	staleRunning.LastHeartbeatAt = time.Unix(40, 0)
	if err := store.Save([]RunRecord{staleRunning}); err != nil {
		t.Fatal(err)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != RunCompleted || !records[0].FinishedAt.Equal(completed.FinishedAt) {
		t.Fatalf("terminal record after stale save = %#v, want completed at %v", records, completed.FinishedAt)
	}
}

func TestStaleManagerHeartbeatCannotReopenCancelledFileStoreRun(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Path: filepath.Join(t.TempDir(), "runtime", "runs.json")}
	owner, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := owner.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewManager(ManagerConfig{Store: store, SkipRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	if err := observer.Heartbeat(handle.ID); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("stale observer Heartbeat() error = %v, want ErrRunTerminal", err)
	}
	observed, err := observer.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Status != RunCancelled {
		t.Fatalf("stale observer status = %q, want cancelled", observed.Status)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != RunCancelled {
		t.Fatalf("store after stale observer heartbeat = %#v, want cancelled run", records)
	}
}

func TestStaleManagerCannotAuthorizeOrCompleteExternallyCancelledRun(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Path: filepath.Join(t.TempDir(), "runtime", "runs.json")}
	owner, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := owner.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewManager(ManagerConfig{Store: store, SkipRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	if err := observer.Authorize(handle.ID, Capabilities{Read: true}); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("stale observer Authorize() error = %v, want ErrRunTerminal", err)
	}
	if err := observer.Complete(handle.ID, verifiedHandoff()); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("stale observer Complete() error = %v, want ErrRunTerminal", err)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != RunCancelled {
		t.Fatalf("store after stale observer completion = %#v, want cancelled run", records)
	}
}

func TestListReconcilesExternallyTerminalRuns(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Path: filepath.Join(t.TempDir(), "runtime", "runs.json")}
	owner, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := owner.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewManager(ManagerConfig{Store: store, SkipRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	listed := observer.List()
	if len(listed) != 1 || listed[0].Status != RunCancelled {
		t.Fatalf("stale observer List() = %#v, want cancelled run", listed)
	}
}

func TestFileStoreTerminalTransitionIsImmutable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runs.json")
	store := FileStore{Path: path}
	started := RunRecord{ID: "run_terminal_immutable", Status: RunRunning, StartedAt: time.Unix(10, 0)}
	if err := store.Save([]RunRecord{started}); err != nil {
		t.Fatal(err)
	}
	cancelled := started
	cancelled.Status = RunCancelled
	cancelled.Error = "cancelled by supervisor"
	cancelled.FinishedAt = time.Unix(20, 0)
	if err := store.Save([]RunRecord{cancelled}); err != nil {
		t.Fatal(err)
	}
	staleCompleted := started
	staleCompleted.Status = RunCompleted
	staleCompleted.FinishedAt = time.Unix(30, 0)
	if err := store.Save([]RunRecord{staleCompleted}); err != nil {
		t.Fatal(err)
	}
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != RunCancelled || records[0].Error != cancelled.Error {
		t.Fatalf("terminal record after later stale transition = %#v, want immutable cancellation", records)
	}
}

func TestExternalManagerCancellationReachesOwningHandle(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Path: filepath.Join(t.TempDir(), "runtime", "runs.json")}
	owner, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := owner.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewManager(ManagerConfig{Store: store, SkipRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Cancel(handle.ID); err != nil {
		t.Fatalf("observer Cancel() error = %v", err)
	}

	select {
	case <-handle.Context.Done():
	case <-time.After(4 * externalSyncInterval):
		t.Fatal("owning handle context was not canceled after external manager cancellation")
	}
	select {
	case <-handle.Done:
	case <-time.After(externalSyncInterval):
		t.Fatal("owning handle Done channel was not closed after external manager cancellation")
	}
	record, err := owner.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunCancelled {
		t.Fatalf("owner record status = %q, want cancelled", record.Status)
	}
}

func TestFileStoreConcurrentSavesPreserveAllRuns(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "runs.json")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := FileStore{Path: path}
			if err := store.Save([]RunRecord{
				{ID: RunID(fmt.Sprintf("run_%d", i)), Status: RunRunning, StartedAt: time.Unix(int64(i+1), 0)},
			}); err != nil {
				t.Errorf("concurrent Save(%d) error = %v", i, err)
			}
		}()
	}
	wg.Wait()
	records, err := (FileStore{Path: path}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 8 {
		t.Fatalf("concurrent save records = %d, want 8: %#v", len(records), records)
	}
}

func TestReapStaleCancelsStaleSubtreeAndLeavesFreshRuns(t *testing.T) {
	root := t.TempDir()
	current := time.Unix(300, 0)
	m, err := NewManager(ManagerConfig{
		Now: func() time.Time { return current },
	})
	if err != nil {
		t.Fatal(err)
	}
	staleParent, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	staleChild, err := m.Start(RunSpec{
		Objective: "stale child",
		Workspace: root,
		ParentID:  staleParent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	current = current.Add(time.Minute)
	if err := m.Heartbeat(fresh.ID); err != nil {
		t.Fatal(err)
	}

	reaped, err := m.ReapStale(30 * time.Second)
	if err != nil {
		t.Fatalf("ReapStale() error = %v", err)
	}
	if len(reaped) != 1 || reaped[0] != staleParent.ID {
		t.Fatalf("reaped = %v, want only stale parent %q", reaped, staleParent.ID)
	}
	parentRecord, _ := m.Get(staleParent.ID)
	childRecord, _ := m.Get(staleChild.ID)
	freshRecord, _ := m.Get(fresh.ID)
	if parentRecord.Status != RunInterrupted || childRecord.Status != RunCancelled {
		t.Fatalf("stale statuses = parent=%q child=%q, want interrupted/cancelled", parentRecord.Status, childRecord.Status)
	}
	if freshRecord.Status != RunRunning {
		t.Fatalf("fresh status = %q, want running", freshRecord.Status)
	}
	if !strings.Contains(parentRecord.Error, "heartbeat") {
		t.Fatalf("stale parent error = %q, want heartbeat explanation", parentRecord.Error)
	}
	_ = m.Cancel(fresh.ID)
}

func TestReapStaleReportsStartedAtSilenceForLegacyRun(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "runtime", "runs.json")
	current := time.Unix(300, 0)
	store := FileStore{Path: path}
	if err := store.Save([]RunRecord{{
		ID:        "legacy-run",
		Status:    RunRunning,
		StartedAt: time.Unix(200, 0),
		Spec:      testSpec(root),
	}}); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(ManagerConfig{
		Store:        store,
		Now:          func() time.Time { return current },
		SkipRecovery: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	reaped, err := m.ReapStale(50 * time.Second)
	if err != nil {
		t.Fatalf("ReapStale() error = %v", err)
	}
	if len(reaped) != 1 || reaped[0] != "legacy-run" {
		t.Fatalf("reaped = %v, want legacy-run", reaped)
	}
	record, err := m.Get("legacy-run")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunInterrupted {
		t.Fatalf("legacy run status = %q, want interrupted", record.Status)
	}
	if !strings.Contains(record.Error, "1m40s") {
		t.Fatalf("legacy run error = %q, want StartedAt-based silence", record.Error)
	}
}

func TestReapStaleRefreshesHeartbeatWrittenByAnotherManager(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "runtime", "runs.json")
	current := time.Unix(300, 0)
	store := FileStore{Path: path}
	owner, err := NewManager(ManagerConfig{
		Store: store,
		Now:   func() time.Time { return current },
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := owner.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}

	observer, err := NewManager(ManagerConfig{
		Store:        store,
		Now:          func() time.Time { return current },
		SkipRecovery: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	current = current.Add(time.Minute)
	if err := owner.Heartbeat(handle.ID); err != nil {
		t.Fatal(err)
	}
	reaped, err := observer.ReapStale(30 * time.Second)
	if err != nil {
		t.Fatalf("ReapStale() error = %v", err)
	}
	if len(reaped) != 0 {
		t.Fatalf("reaped = %v, want no live run reaped", reaped)
	}
	record, err := observer.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunRunning {
		t.Fatalf("observer status = %q, want running", record.Status)
	}
	if !record.LastHeartbeatAt.Equal(current) {
		t.Fatalf("observer heartbeat = %v, want %v", record.LastHeartbeatAt, current)
	}
	if err := owner.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
}

func TestParentTimeoutCancelsActiveChildren(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{MaxTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	parentSpec := testSpec(root)
	parentSpec.Budget.Timeout = 10 * time.Millisecond
	parent, err := m.Start(parentSpec)
	if err != nil {
		t.Fatal(err)
	}
	child, err := m.Start(RunSpec{
		Objective:    "wait for parent",
		Workspace:    root,
		ParentID:     parent.ID,
		Budget:       Budget{Timeout: time.Second},
		DoneCriteria: []string{"report findings"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, handle := range []*RunHandle{parent, child} {
		select {
		case <-handle.Done:
		case <-time.After(time.Second):
			t.Fatalf("run %s did not become terminal", handle.ID)
		}
	}
	parentRecord, err := m.Get(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	childRecord, err := m.Get(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parentRecord.Status != RunTimedOut {
		t.Fatalf("parent status = %q, want timed_out", parentRecord.Status)
	}
	if childRecord.Status != RunCancelled {
		t.Fatalf("child status = %q, want cancelled", childRecord.Status)
	}
}

type failingStore struct {
	mu       sync.RWMutex
	records  []RunRecord
	fail     bool
	loadFail bool
}

func (s *failingStore) Load() ([]RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.loadFail {
		return nil, errors.New("fixture load failure")
	}
	return cloneRecords(s.records), nil
}

func (s *failingStore) Save(records []RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("fixture store failure")
	}
	s.records = cloneRecords(records)
	return nil
}

func (s *failingStore) setFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = fail
}

func (s *failingStore) setLoadFail(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadFail = fail
}

func TestTerminalPersistenceFailureRollsBackState(t *testing.T) {
	root := t.TempDir()
	store := &failingStore{}
	m, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	store.setFail(true)
	if err := m.Cancel(handle.ID); err == nil {
		t.Fatal("Cancel() unexpectedly succeeded with a failing store")
	}
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunRunning {
		t.Fatalf("status after failed persistence = %q, want running", record.Status)
	}
	select {
	case <-handle.Done:
		t.Fatal("run handle closed after failed persistence")
	default:
	}
}

func TestHandoffRequiresVerificationAndConfinedDigests(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	spec := testSpec(root)
	handle, err := m.Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Complete(handle.ID, Handoff{Summary: "not verified"}); !errors.Is(err, ErrInvalidHandoff) {
		t.Fatalf("unverified handoff error = %v", err)
	}
	bad := verifiedHandoff()
	bad.Artifacts = []Artifact{{Path: "../outside.txt", SHA256: strings.Repeat("a", 64)}}
	if err := m.Complete(handle.ID, bad); !errors.Is(err, ErrInvalidHandoff) {
		t.Fatalf("outside artifact error = %v", err)
	}
	bad = verifiedHandoff()
	bad.Artifacts = []Artifact{{Path: "report.json", SHA256: "not-a-digest"}}
	if err := m.Complete(handle.ID, bad); !errors.Is(err, ErrInvalidHandoff) {
		t.Fatalf("bad digest error = %v", err)
	}
	valid := verifiedHandoff()
	valid.CompletedCriteria = []string{"report findings"}
	report := []byte(`{"ok":true}`)
	if err := os.WriteFile(filepath.Join(root, "report.json"), report, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(report)
	valid.Artifacts = []Artifact{{Path: "report.json", Kind: "report", SHA256: hex.EncodeToString(digest[:])}}
	if err := m.Complete(handle.ID, valid); err != nil {
		t.Fatalf("valid handoff error = %v", err)
	}
}

func TestHandoffRejectsArtifactOutsideWorkspaceThroughSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "report.json")
	contents := []byte(`{"outside":true}`)
	if err := os.WriteFile(outsidePath, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(root, "report.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	digest := sha256.Sum256(contents)
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	handoff := verifiedHandoff()
	handoff.Artifacts = []Artifact{{Path: "report.json", SHA256: hex.EncodeToString(digest[:])}}
	if err := m.Complete(handle.ID, handoff); !errors.Is(err, ErrInvalidHandoff) {
		t.Fatalf("outside symlink error = %v, want ErrInvalidHandoff", err)
	}
}

func TestHandoffAcceptsArtifactSymlinkThatStaysInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	contents := []byte(`{"inside":true}`)
	if err := os.WriteFile(filepath.Join(root, "actual.json"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("actual.json", filepath.Join(root, "report.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	digest := sha256.Sum256(contents)
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	handoff := verifiedHandoff()
	handoff.Artifacts = []Artifact{{Path: "report.json", SHA256: hex.EncodeToString(digest[:])}}
	if err := m.Complete(handle.ID, handoff); err != nil {
		t.Fatalf("inside symlink handoff error = %v", err)
	}
}

func TestHandoffRejectsArtifactWithWrongDigest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "report.json"), []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	handoff := verifiedHandoff()
	handoff.Artifacts = []Artifact{{Path: "report.json", SHA256: strings.Repeat("a", 64)}}
	if err := m.Complete(handle.ID, handoff); !errors.Is(err, ErrInvalidHandoff) {
		t.Fatalf("wrong digest error = %v, want ErrInvalidHandoff", err)
	}
}

func TestStaleCompletionAfterCancellationCannotReopenRun(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := m.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete(handle.ID, verifiedHandoff()); !errors.Is(err, ErrRunTerminal) {
		t.Fatalf("stale completion error = %v, want ErrRunTerminal", err)
	}
	record, err := m.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunCancelled {
		t.Fatalf("status after stale completion = %q, want cancelled", record.Status)
	}
}

func TestFileStoreRecoversRunningRunsAsInterrupted(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Path: filepath.Join(t.TempDir(), "runtime", "runs.json")}
	first, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := first.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}

	second, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	record, err := second.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunInterrupted {
		t.Fatalf("recovered status = %q, want interrupted", record.Status)
	}
	if !strings.Contains(record.Error, "restarted") {
		t.Fatalf("recovered error = %q", record.Error)
	}
	if len(record.Events) < 2 || record.Events[len(record.Events)-1].Type != "recovered_interrupted" {
		t.Fatalf("recovered events = %#v, want recovery event", record.Events)
	}
	if _, err := os.Stat(store.Path); err != nil {
		t.Fatalf("runtime store was not persisted: %v", err)
	}
	_ = first.Cancel(handle.ID)
}

func TestFileStoreLoadRejectsSymlinkStore(t *testing.T) {
	stateDir := t.TempDir()
	outside := t.TempDir()
	outsidePath := filepath.Join(outside, "runs.json")
	if err := os.WriteFile(outsidePath, []byte(`{"version":1,"runs":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(stateDir, "runs.json")
	if err := os.Symlink(outsidePath, storePath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := (FileStore{Path: storePath}).Load()
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Load() error = %v, want symlink rejection", err)
	}
}

func TestReadOnlyManagerObservesRunningRunsWithoutRecovery(t *testing.T) {
	root := t.TempDir()
	store := FileStore{Path: filepath.Join(t.TempDir(), "runtime", "runs.json")}
	first, err := NewManager(ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := first.Start(testSpec(root))
	if err != nil {
		t.Fatal(err)
	}

	observer, err := NewManager(ManagerConfig{Store: store, SkipRecovery: true})
	if err != nil {
		t.Fatal(err)
	}
	record, err := observer.Get(handle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunRunning {
		t.Fatalf("observer status = %q, want running", record.Status)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Status != RunRunning {
		t.Fatalf("store after read-only observation = %#v, want unchanged running record", loaded)
	}
	if err := first.Cancel(handle.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentChildStartsRespectLimit(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(ManagerConfig{MaxConcurrentChildren: 3})
	if err != nil {
		t.Fatal(err)
	}
	parentSpec := testSpec(root)
	parentSpec.Budget.MaxChildren = 3
	parent, err := m.Start(parentSpec)
	if err != nil {
		t.Fatal(err)
	}

	childSpec := RunSpec{Objective: "parallel child", Workspace: root, ParentID: parent.ID}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var children []*RunHandle
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handle, startErr := m.Start(childSpec)
			if startErr == nil {
				mu.Lock()
				children = append(children, handle)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(children) != 3 {
		t.Fatalf("successful children = %d, want 3", len(children))
	}
	_ = m.Cancel(parent.ID)
}
