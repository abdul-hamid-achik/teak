package procmon

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestProcessRSSCurrentProcess(t *testing.T) {
	rss, err := ProcessRSS(t.Context(), os.Getpid())
	if errors.Is(err, ErrRSSUnavailable) {
		t.Skip("current platform has no process RSS sampler")
	}
	if err != nil {
		t.Fatalf("ProcessRSS() error = %v", err)
	}
	if rss == 0 {
		t.Fatal("ProcessRSS() returned zero for the current process")
	}
}

func TestProcessGroupRSSCurrentProcess(t *testing.T) {
	rss, err := ProcessGroupRSS(t.Context(), os.Getpid())
	if errors.Is(err, ErrRSSUnavailable) {
		t.Skip("current platform has no process-group RSS sampler")
	}
	if err != nil {
		t.Fatalf("ProcessGroupRSS() error = %v", err)
	}
	if rss == 0 {
		t.Fatal("ProcessGroupRSS() returned zero for the current process group")
	}
}

func TestProcessRSSRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ProcessRSS(ctx, os.Getpid()); !errors.Is(err, context.Canceled) {
		t.Fatalf("ProcessRSS() error = %v, want context.Canceled", err)
	}
}

func TestProcessGroupRSSRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ProcessGroupRSS(ctx, os.Getpid()); !errors.Is(err, context.Canceled) {
		t.Fatalf("ProcessGroupRSS() error = %v, want context.Canceled", err)
	}
}

func TestProcessRSSRejectsInvalidPID(t *testing.T) {
	if _, err := ProcessRSS(t.Context(), 0); err == nil {
		t.Fatal("ProcessRSS() error = nil, want invalid-pid error")
	}
}

func TestProcessGroupRSSRejectsInvalidPID(t *testing.T) {
	if _, err := ProcessGroupRSS(t.Context(), 0); err == nil {
		t.Fatal("ProcessGroupRSS() error = nil, want invalid-pid error")
	}
}
