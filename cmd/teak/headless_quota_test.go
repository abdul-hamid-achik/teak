package main

import (
	"context"
	"errors"
	"testing"
)

func TestHeadlessQuotaRejectsConcurrentReservations(t *testing.T) {
	quota := newHeadlessQuota(1, 1024)
	release, err := quota.acquire(context.Background(), 768)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	t.Cleanup(release)

	if _, err := quota.acquire(context.Background(), 1); !errors.Is(err, ErrHeadlessQuotaExceeded) {
		t.Fatalf("second acquire() error = %v, want ErrHeadlessQuotaExceeded", err)
	}
	snapshot := quota.snapshot()
	if snapshot.Active != 1 || snapshot.ReservedOutputBytes != 768 {
		t.Fatalf("quota snapshot = %#v, want one active reservation of 768 bytes", snapshot)
	}

	release()
	release()
	if _, err := quota.acquire(context.Background(), 1024); err != nil {
		t.Fatalf("acquire after release() error = %v", err)
	}
}

func TestHeadlessQuotaRejectsOutputReservation(t *testing.T) {
	quota := newHeadlessQuota(4, 1024)
	release, err := quota.acquire(context.Background(), 768)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	if _, err := quota.acquire(context.Background(), 257); !errors.Is(err, ErrHeadlessQuotaExceeded) {
		t.Fatalf("output overflow acquire() error = %v, want ErrHeadlessQuotaExceeded", err)
	}
}

func TestHeadlessQuotaHonorsCancellation(t *testing.T) {
	quota := newHeadlessQuota(1, 1024)
	release, err := quota.acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("first acquire() error = %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := quota.acquire(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire() error = %v, want context.Canceled", err)
	}
}

func TestHeadlessLimitedBufferStopsGrowingPastLimit(t *testing.T) {
	var output headlessLimitedBuffer
	output.limit = 4
	if n, err := output.Write([]byte("12345")); n != 4 || !errors.Is(err, ErrHeadlessOutputLimit) {
		t.Fatalf("limited Write() = n:%d err:%v, want four bytes and ErrHeadlessOutputLimit", n, err)
	}
	if string(output.Bytes()) != "1234" || !output.Exceeded() {
		t.Fatalf("limited output = %q exceeded:%t, want 1234/true", output.Bytes(), output.Exceeded())
	}
	if n, err := output.Write([]byte("6")); n != 0 || !errors.Is(err, ErrHeadlessOutputLimit) {
		t.Fatalf("second limited Write() = n:%d err:%v, want zero and ErrHeadlessOutputLimit", n, err)
	}
}
