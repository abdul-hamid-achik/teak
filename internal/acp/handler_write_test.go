package acp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/coder/acp-go-sdk"
)

func TestWriteTextFileRejectsOversizedProposalWithoutEnqueueing(t *testing.T) {
	msgChan := make(chan tea.Msg, 1)
	handler := newClientHandler(msgChan)

	_, err := handler.WriteTextFile(context.Background(), sdk.WriteTextFileRequest{
		Path:    "generated.txt",
		Content: strings.Repeat("x", maxAgentWriteBytes+1),
	})
	if err == nil {
		t.Fatal("WriteTextFile() error = nil, want oversized proposal error")
	}
	select {
	case msg := <-msgChan:
		t.Fatalf("oversized proposal enqueued %T", msg)
	default:
	}
}

func TestWriteTextFileCancellationEmitsCorrelatedCancellationMessage(t *testing.T) {
	msgChan := make(chan tea.Msg, 2)
	handler := newClientHandler(msgChan)
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)

	go func() {
		_, err := handler.WriteTextFile(ctx, sdk.WriteTextFileRequest{
			Path:    "cancelled.txt",
			Content: "cancelled",
		})
		resultCh <- err
	}()

	var proposal AgentWriteFileMsg
	select {
	case msg := <-msgChan:
		var ok bool
		proposal, ok = msg.(AgentWriteFileMsg)
		if !ok {
			t.Fatalf("first message = %T, want AgentWriteFileMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for write proposal")
	}

	cancel()
	select {
	case err := <-resultCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("WriteTextFile() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for WriteTextFile cancellation")
	}

	select {
	case msg := <-msgChan:
		cancelled, ok := msg.(AgentWriteCancelledMsg)
		if !ok {
			t.Fatalf("second message = %T, want AgentWriteCancelledMsg", msg)
		}
		if cancelled.ResponseCh != proposal.ResponseCh {
			t.Fatal("cancellation message did not retain the proposal response channel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancellation message")
	}
}
