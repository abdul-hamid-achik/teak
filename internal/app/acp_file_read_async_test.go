package app

import (
	"context"
	"path/filepath"
	"testing"

	"teak/internal/acp"
	"teak/internal/config"
)

func TestACPReadFromOpenBufferIsDeferredUntilCommandRuns(t *testing.T) {
	model := newSaveFlowModel(t, config.DefaultConfig(), t.TempDir())
	index := addDirtyEditor(t, &model, "open.txt", "disk\n", "unsaved\n")
	resultCh := make(chan acp.FileReadResult, 1)

	updated, cmd := model.handleFileReadRequest(acp.FileReadRequestMsg{
		Context:  context.Background(),
		RootDir:  model.rootDir,
		Path:     model.editors[index].Buffer.FilePath,
		ResultCh: resultCh,
	})
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("open-buffer ACP read did not return a command")
	}
	select {
	case result := <-resultCh:
		t.Fatalf("ACP read responded inside Update: %#v", result)
	default:
	}

	msg := cmd()
	if _, ok := msg.(acpMsg); !ok {
		t.Fatalf("read command returned %T, want acpMsg listener continuation", msg)
	}
	select {
	case result := <-resultCh:
		if result.Err != nil || result.Content != "unsaved\n" {
			t.Fatalf("open-buffer result = %#v", result)
		}
	default:
		t.Fatal("read command did not deliver the open-buffer snapshot")
	}
}

func TestACPReadRejectsEscapingPathOffUpdateLoop(t *testing.T) {
	root := t.TempDir()
	model := newSaveFlowModel(t, config.DefaultConfig(), root)
	resultCh := make(chan acp.FileReadResult, 1)

	_, cmd := model.handleFileReadRequest(acp.FileReadRequestMsg{
		Context:  context.Background(),
		RootDir:  root,
		Path:     filepath.Join(root, "..", "outside.txt"),
		ResultCh: resultCh,
	})
	if cmd == nil {
		t.Fatal("invalid ACP read did not return a command")
	}
	select {
	case result := <-resultCh:
		t.Fatalf("invalid ACP read responded inside Update: %#v", result)
	default:
	}

	_ = cmd()
	select {
	case result := <-resultCh:
		if result.Err == nil {
			t.Fatalf("escaping ACP read unexpectedly succeeded: %#v", result)
		}
	default:
		t.Fatal("invalid ACP read command did not respond")
	}
}
