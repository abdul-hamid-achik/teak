package acp

import (
	"testing"

	agentruntime "teak/internal/agent/runtime"
)

func TestPromptWithRuntimePersistsFailureWhenACPIsUnavailable(t *testing.T) {
	root := t.TempDir()
	store := agentruntime.FileStore{Path: root + "/runs.json"}
	runManager, err := agentruntime.NewManager(agentruntime.ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManagerWithRuntime(root, "echo", nil, runManager)

	msg := mgr.Prompt("inspect the project", nil)()
	response, ok := msg.(AgentPromptResponseMsg)
	if !ok || response.Err == nil {
		t.Fatalf("Prompt() = %#v (%T), want a failed prompt response", msg, msg)
	}

	runs := runManager.List()
	if len(runs) != 1 {
		t.Fatalf("runtime runs = %#v, want one run", runs)
	}
	if runs[0].Status != agentruntime.RunFailed || runs[0].Spec.Objective != "inspect the project" {
		t.Fatalf("runtime run = %#v, want failed tracked prompt", runs[0])
	}
	if !runs[0].EffectiveCapabilities.Read || !runs[0].EffectiveCapabilities.Write || !runs[0].EffectiveCapabilities.Shell {
		t.Fatalf("runtime capabilities = %#v, want declared ACP read/write/shell contract", runs[0].EffectiveCapabilities)
	}

	reloaded, err := agentruntime.NewManager(agentruntime.ManagerConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.List(); len(got) != 1 || got[0].Status != agentruntime.RunFailed {
		t.Fatalf("reloaded runtime runs = %#v, want failed run", got)
	}
}
