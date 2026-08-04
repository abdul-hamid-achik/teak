package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
)

const (
	// A headless server can have several clients, but an unbounded number of
	// subprocesses would turn individually bounded operations into an aggregate
	// memory and process-exhaustion path.
	headlessMaxConcurrentOperations              = 8
	headlessMaxReservedOutputBytes               = 8 << 20
	headlessRESTOperationOutputReservation int64 = 1 << 20
	headlessRESTErrorOutputLimit           int64 = 64 << 10
)

var (
	ErrHeadlessQuotaExceeded = errors.New("headless request quota exceeded")
	ErrHeadlessOutputLimit   = errors.New("headless response exceeded output limit")
)

// headlessQuota is a non-blocking per-server admission controller. Each
// admitted operation reserves its maximum response size before starting any
// subprocess, so concurrent requests cannot multiply bounded per-request
// buffers into an unbounded aggregate.
type headlessQuota struct {
	mu                     sync.Mutex
	maxConcurrent          int
	maxReservedOutputBytes int64
	active                 int
	reservedOutputBytes    int64
}

type headlessQuotaSnapshot struct {
	Active              int   `json:"active"`
	MaxConcurrent       int   `json:"max_concurrent"`
	ReservedOutputBytes int64 `json:"reserved_output_bytes"`
	MaxOutputBytes      int64 `json:"max_output_bytes"`
}

func newHeadlessQuota(maxConcurrent int, maxOutputBytes int64) *headlessQuota {
	if maxConcurrent <= 0 {
		maxConcurrent = headlessMaxConcurrentOperations
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = headlessMaxReservedOutputBytes
	}
	return &headlessQuota{
		maxConcurrent:          maxConcurrent,
		maxReservedOutputBytes: maxOutputBytes,
	}
}

func (q *headlessQuota) acquire(ctx context.Context, outputBytes int64) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if outputBytes <= 0 {
		return nil, fmt.Errorf("%w: output reservation must be positive", ErrHeadlessQuotaExceeded)
	}

	q.mu.Lock()
	if q.active >= q.maxConcurrent {
		active := q.active
		limit := q.maxConcurrent
		q.mu.Unlock()
		return nil, fmt.Errorf("%w: active operations %d/%d", ErrHeadlessQuotaExceeded, active, limit)
	}
	if outputBytes > q.maxReservedOutputBytes-q.reservedOutputBytes {
		reserved := q.reservedOutputBytes
		limit := q.maxReservedOutputBytes
		q.mu.Unlock()
		return nil, fmt.Errorf("%w: output reservation %d + %d exceeds %d bytes", ErrHeadlessQuotaExceeded, reserved, outputBytes, limit)
	}
	q.active++
	q.reservedOutputBytes += outputBytes
	q.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			q.mu.Lock()
			q.active--
			q.reservedOutputBytes -= outputBytes
			q.mu.Unlock()
		})
	}, nil
}

func (q *headlessQuota) snapshot() headlessQuotaSnapshot {
	if q == nil {
		return headlessQuotaSnapshot{}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return headlessQuotaSnapshot{
		Active:              q.active,
		MaxConcurrent:       q.maxConcurrent,
		ReservedOutputBytes: q.reservedOutputBytes,
		MaxOutputBytes:      q.maxReservedOutputBytes,
	}
}

// headlessLimitedBuffer prevents a compatibility runner from retaining more
// response data than the server's reservation. Writers that ignore errors
// still stop growing because the limit is enforced internally.
type headlessLimitedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *headlessLimitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		b.exceeded = true
		return 0, ErrHeadlessOutputLimit
	}
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.exceeded = true
		return 0, ErrHeadlessOutputLimit
	}
	if int64(len(p)) > remaining {
		_, _ = b.buffer.Write(p[:int(remaining)])
		b.exceeded = true
		return int(remaining), ErrHeadlessOutputLimit
	}
	return b.buffer.Write(p)
}

func (b *headlessLimitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *headlessLimitedBuffer) Len() int { return b.buffer.Len() }

func (b *headlessLimitedBuffer) Exceeded() bool { return b.exceeded }
