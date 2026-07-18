package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/acp"
	"teak/internal/agent"
	"teak/internal/config"
)

func runAgentWriteDecision(t *testing.T, model Model, decision agent.WriteDecisionMsg) (Model, error) {
	t.Helper()

	updatedAny, cmd := model.Update(decision)
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("write decision command = nil")
	}

	result := cmd()
	if result == nil {
		t.Fatal("write decision result = nil")
	}
	finalAny, _ := updated.Update(result)
	final := finalAny.(Model)

	select {
	case err := <-decision.Proposal.ResponseCh:
		select {
		case duplicate := <-decision.Proposal.ResponseCh:
			t.Fatalf("write decision responded more than once; duplicate = %v", duplicate)
		default:
		}
		return final, err
	default:
		t.Fatal("write decision did not respond")
		return final, nil
	}
}

func TestAgentWriteDecisionAcceptsWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	responseCh := make(chan error, 1)
	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "accepted.txt",
			Content:    "accepted content",
			ResponseCh: responseCh,
		},
	}

	model, err := runAgentWriteDecision(t, Model{rootDir: root}, decision)
	if err != nil {
		t.Fatalf("accepted write error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "accepted.txt"))
	if err != nil {
		t.Fatalf("ReadFile(accepted.txt) error = %v", err)
	}
	if got := string(data); got != decision.Proposal.Content {
		t.Fatalf("written content = %q, want %q", got, decision.Proposal.Content)
	}
	if model.status != "Agent wrote accepted.txt" {
		t.Fatalf("status = %q, want successful agent-write status", model.status)
	}
}

func TestAgentWriteDecisionAcceptsNewNestedWorkspaceFile(t *testing.T) {
	root := t.TempDir()
	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "new/nested/accepted.txt",
			Content:    "nested content",
			ResponseCh: make(chan error, 1),
		},
	}

	_, err := runAgentWriteDecision(t, Model{rootDir: root}, decision)
	if err != nil {
		t.Fatalf("nested accepted write error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "new", "nested", "accepted.txt"))
	if err != nil {
		t.Fatalf("ReadFile(nested accepted.txt) error = %v", err)
	}
	if got := string(data); got != decision.Proposal.Content {
		t.Fatalf("written content = %q, want %q", got, decision.Proposal.Content)
	}
}

func TestAgentWriteDecisionsExecuteInDecisionOrder(t *testing.T) {
	root := t.TempDir()
	first := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "ordered.txt",
			Content:    "first",
			ResponseCh: make(chan error, 1),
		},
	}
	second := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "ordered.txt",
			Content:    "second",
			ResponseCh: make(chan error, 1),
		},
	}

	firstModelAny, firstCmd := Model{rootDir: root}.Update(first)
	firstModel := firstModelAny.(Model)
	if firstCmd == nil {
		t.Fatal("first write command = nil")
	}

	queuedModelAny, secondCmd := firstModel.Update(second)
	queuedModel := queuedModelAny.(Model)
	if secondCmd != nil {
		t.Fatal("second write started before the first result was handled")
	}

	firstResult := firstCmd()
	afterFirstAny, promotedCmd := queuedModel.Update(firstResult)
	afterFirst := afterFirstAny.(Model)
	if promotedCmd == nil {
		t.Fatal("queued second write was not started after the first completed")
	}
	if err := <-first.Proposal.ResponseCh; err != nil {
		t.Fatalf("first response error = %v", err)
	}

	secondResult := promotedCmd()
	finalAny, extraCmd := afterFirst.Update(secondResult)
	if extraCmd != nil {
		t.Fatal("unexpected command after final queued write")
	}
	if err := <-second.Proposal.ResponseCh; err != nil {
		t.Fatalf("second response error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "ordered.txt"))
	if err != nil {
		t.Fatalf("ReadFile(ordered.txt) error = %v", err)
	}
	if got := string(data); got != "second" {
		t.Fatalf("final content = %q, want second decision content", got)
	}
	_ = finalAny.(Model)
}

func TestAgentWritePanelDecisionsAreSerializedBeforeCommandScheduling(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(model.cleanup)

	first := acp.AgentWriteFileMsg{
		Path:       "scheduled.txt",
		Content:    "first",
		ResponseCh: make(chan error, 1),
	}
	second := acp.AgentWriteFileMsg{
		Path:       "scheduled.txt",
		Content:    "second",
		ResponseCh: make(chan error, 1),
	}
	firstPendingAny, _ := model.handleACPMsg(acpMsg{msg: first})
	firstPending := firstPendingAny.(Model)
	secondPendingAny, _ := firstPending.handleACPMsg(acpMsg{msg: second})
	secondPending := secondPendingAny.(Model)

	firstDecidedAny, firstWriteCmd := secondPending.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	firstDecided := firstDecidedAny.(Model)
	if firstWriteCmd == nil || !firstDecided.agentWrites.inFlight {
		t.Fatal("first panel decision did not synchronously enter the serialized write pipeline")
	}

	secondDecidedAny, prematureCmd := firstDecided.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	secondDecided := secondDecidedAny.(Model)
	if prematureCmd != nil {
		t.Fatal("second panel decision started before the first write result")
	}
	if got := len(secondDecided.agentWrites.queued); got != 1 {
		t.Fatalf("queued decisions = %d, want 1", got)
	}

	afterFirstAny, secondWriteCmd := secondDecided.Update(firstWriteCmd())
	afterFirst := afterFirstAny.(Model)
	if secondWriteCmd == nil {
		t.Fatal("second write command was not released after the first result")
	}
	finalAny, _ := afterFirst.Update(secondWriteCmd())
	final := finalAny.(Model)

	if err := <-first.ResponseCh; err != nil {
		t.Fatalf("first response error = %v", err)
	}
	if err := <-second.ResponseCh; err != nil {
		t.Fatalf("second response error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "scheduled.txt"))
	if err != nil {
		t.Fatalf("ReadFile(scheduled.txt) error = %v", err)
	}
	if got := string(data); got != "second" {
		t.Fatalf("final content = %q, want second", got)
	}
	if final.agentWrites.inFlight || len(final.agentWrites.queued) != 0 {
		t.Fatalf("final write state = %#v, want idle", final.agentWrites)
	}
}

func TestAgentWriteDecisionDoesNotApplyCancelledProposal(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "cancelled.txt",
			Content:    "must not be written",
			ResponseCh: make(chan error, 1),
			Context:    ctx,
		},
	}

	_, err := runAgentWriteDecision(t, Model{rootDir: root}, decision)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write error = %v, want context.Canceled", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "cancelled.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled target exists; Stat error = %v", statErr)
	}
}

func TestAgentWriteAtomicRemainsConfinedAfterParentSwap(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(root, "safe"), 0o755); err != nil {
		t.Fatalf("MkdirAll(safe) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}

	relativePath, err := validatePathStrict(root, "safe/result.txt")
	if err != nil {
		t.Fatalf("validatePathStrict() error = %v", err)
	}
	if err := os.Rename(filepath.Join(root, "safe"), filepath.Join(root, "safe-old")); err != nil {
		t.Fatalf("Rename(safe) error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "safe")); err != nil {
		t.Fatalf("Symlink(outside) error = %v", err)
	}

	err = writeAgentFileAtomic(root, relativePath, []byte("escaped"))
	if err == nil {
		t.Fatal("writeAgentFileAtomic() error = nil after parent symlink swap")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "result.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("outside target exists; Stat error = %v", statErr)
	}
}

func TestAgentWriteUsesPinnedWorkspaceRootAfterRootPathSwap(t *testing.T) {
	base := t.TempDir()
	rootPath := filepath.Join(base, "workspace")
	anchoredPath := filepath.Join(base, "workspace-moved")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspace) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	pinnedRoot, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatalf("OpenRoot(workspace) error = %v", err)
	}
	defer func() {
		_ = pinnedRoot.Close()
	}()

	if err := os.Rename(rootPath, anchoredPath); err != nil {
		t.Fatalf("Rename(workspace) error = %v", err)
	}
	if err := os.Symlink(outside, rootPath); err != nil {
		t.Fatalf("Symlink(outside) error = %v", err)
	}

	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "pinned.txt",
			Content:    "anchored",
			ResponseCh: make(chan error, 1),
		},
	}
	_, err = runAgentWriteDecision(t, Model{
		rootDir:        rootPath,
		agentWriteRoot: pinnedRoot,
	}, decision)
	if err != nil {
		t.Fatalf("pinned-root write error = %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(outside, "pinned.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("outside target exists; Stat error = %v", statErr)
	}
	data, err := os.ReadFile(filepath.Join(anchoredPath, "pinned.txt"))
	if err != nil {
		t.Fatalf("ReadFile(anchored pinned.txt) error = %v", err)
	}
	if got := string(data); got != "anchored" {
		t.Fatalf("anchored content = %q, want anchored", got)
	}
}

func TestAgentWriteDecisionRejectsWithoutChangingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile(existing.txt) error = %v", err)
	}

	decision := agent.WriteDecisionMsg{
		Accepted: false,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "existing.txt",
			Content:    "replacement",
			ResponseCh: make(chan error, 1),
		},
	}
	_, err := runAgentWriteDecision(t, Model{rootDir: root}, decision)
	if err == nil {
		t.Fatal("rejected write error = nil")
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile(existing.txt) error = %v", readErr)
	}
	if got := string(data); got != "original" {
		t.Fatalf("content after rejection = %q, want original", got)
	}
}

func TestAgentWriteDecisionRejectsWorkspaceEscapes(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspace) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}

	outsideFile := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(outsideFile, []byte("sentinel"), 0o644); err != nil {
		t.Fatalf("WriteFile(sentinel) error = %v", err)
	}

	symlinkPath := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, symlinkPath); err != nil {
		t.Fatalf("Symlink(outside) error = %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "empty path", path: ""},
		{name: "absolute path", path: outsideFile},
		{name: "parent traversal", path: "../outside/sentinel.txt"},
		{name: "symlink parent", path: "outside-link/sentinel.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := agent.WriteDecisionMsg{
				Accepted: true,
				Proposal: acp.AgentWriteFileMsg{
					Path:       tt.path,
					Content:    "escaped",
					ResponseCh: make(chan error, 1),
				},
			}
			_, err := runAgentWriteDecision(t, Model{rootDir: root}, decision)
			if err == nil {
				t.Fatalf("escaped write %q error = nil", tt.path)
			}

			data, readErr := os.ReadFile(outsideFile)
			if readErr != nil {
				t.Fatalf("ReadFile(sentinel) error = %v", readErr)
			}
			if got := string(data); got != "sentinel" {
				t.Fatalf("outside content = %q, want sentinel", got)
			}
		})
	}
}

func TestAgentWriteDecisionDoesNotCreateNestedPathThroughSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "workspace")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspace) error = %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll(outside) error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outside-link")); err != nil {
		t.Fatalf("Symlink(outside) error = %v", err)
	}

	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "outside-link/new/nested.txt",
			Content:    "escaped",
			ResponseCh: make(chan error, 1),
		},
	}
	_, err := runAgentWriteDecision(t, Model{rootDir: root}, decision)
	if err == nil {
		t.Fatal("nested write through symlink error = nil")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(statErr) {
		t.Fatalf("outside nested directory was created; Stat error = %v", statErr)
	}
}

func TestAgentWriteDecisionDoesNotFollowPredictableTempSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("sentinel"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "accepted.txt.tmp")); err != nil {
		t.Fatalf("Symlink(temp path) error = %v", err)
	}

	decision := agent.WriteDecisionMsg{
		Accepted: true,
		Proposal: acp.AgentWriteFileMsg{
			Path:       "accepted.txt",
			Content:    "accepted content",
			ResponseCh: make(chan error, 1),
		},
	}
	_, err := runAgentWriteDecision(t, Model{rootDir: root}, decision)
	if err != nil {
		t.Fatalf("accepted write error = %v", err)
	}

	outsideData, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("ReadFile(outside) error = %v", err)
	}
	if got := string(outsideData); got != "sentinel" {
		t.Fatalf("outside content = %q, want sentinel", got)
	}
	targetInfo, err := os.Lstat(filepath.Join(root, "accepted.txt"))
	if err != nil {
		t.Fatalf("Lstat(accepted.txt) error = %v", err)
	}
	if targetInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("accepted.txt is a symlink, want regular file")
	}
}

func TestAgentWriteProposalCapturesPanelDecisionKeys(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	defer model.cleanup()

	proposal := acp.AgentWriteFileMsg{
		Path:       "from-panel.txt",
		Content:    "panel content",
		ResponseCh: make(chan error, 1),
	}
	updatedAny, _ := model.handleACPMsg(acpMsg{msg: proposal})
	updated := updatedAny.(Model)
	if !updated.showAgent {
		t.Fatal("agent panel was not shown for write proposal")
	}
	if updated.focus != FocusAgent {
		t.Fatalf("focus = %v, want FocusAgent", updated.focus)
	}
	if !updated.agentPanel.HasPendingWrite() {
		t.Fatal("agent panel has no pending write proposal")
	}

	decidedAny, writeCmd := updated.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	decided := decidedAny.(Model)
	if writeCmd == nil {
		t.Fatal("Enter write command = nil")
	}
	result := writeCmd()
	if _, ok := result.(agentWriteResultMsg); !ok {
		t.Fatalf("Enter command message = %T, want agentWriteResultMsg", result)
	}
	finalAny, nextCmd := decided.Update(result)
	if nextCmd != nil {
		t.Fatal("unexpected command after the only write completed")
	}
	_ = finalAny.(Model)
	if writeErr := <-proposal.ResponseCh; writeErr != nil {
		t.Fatalf("panel-accepted write error = %v", writeErr)
	}
	data, readErr := os.ReadFile(filepath.Join(root, proposal.Path))
	if readErr != nil {
		t.Fatalf("ReadFile(from-panel.txt) error = %v", readErr)
	}
	if got := string(data); got != proposal.Content {
		t.Fatalf("written content = %q, want %q", got, proposal.Content)
	}
}
