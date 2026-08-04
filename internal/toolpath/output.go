package toolpath

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// ErrOutputLimit identifies a command that exceeded a configured stdout or
// stderr budget. Callers can use IsOutputLimit without depending on the
// concrete stream or limit.
var ErrOutputLimit = errors.New("external command output exceeds limit")

// OutputLimitError records which stream exhausted its budget.
type OutputLimitError struct {
	Stream string
	Limit  int
}

func (e *OutputLimitError) Error() string {
	return fmt.Sprintf("external command %s exceeds %d bytes", e.Stream, e.Limit)
}

func (e *OutputLimitError) Unwrap() error { return ErrOutputLimit }

// IsOutputLimit reports whether err was caused by a bounded command stream.
func IsOutputLimit(err error) bool { return errors.Is(err, ErrOutputLimit) }

// RunBounded executes a command while collecting at most stdoutLimit and
// stderrLimit bytes per stream. The command must have been created through
// Command (or otherwise configured with a usable cancellation path). Once a
// stream exceeds its budget, the command process group is terminated before
// RunBounded returns, so a noisy child cannot keep pipes or memory alive.
func RunBounded(cmd *exec.Cmd, stdoutLimit, stderrLimit int) ([]byte, []byte, error) {
	if cmd == nil {
		return nil, nil, errors.New("cannot run a nil external command")
	}
	if stdoutLimit <= 0 || stderrLimit <= 0 {
		return nil, nil, fmt.Errorf("external command output limits must be positive")
	}

	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() { _ = TerminateCommand(cmd) })
	}
	stdout := &boundedOutputBuffer{limit: stdoutLimit, stream: "stdout", onLimit: stop}
	stderr := &boundedOutputBuffer{limit: stderrLimit, stream: "stderr", onLimit: stop}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if stdout.exceeded {
		return stdout.Bytes(), stderr.Bytes(), stdout.limitError()
	}
	if stderr.exceeded {
		return stdout.Bytes(), stderr.Bytes(), stderr.limitError()
	}
	return stdout.Bytes(), stderr.Bytes(), err
}

type boundedOutputBuffer struct {
	bytes.Buffer
	limit    int
	stream   string
	exceeded bool
	onLimit  func()
}

func (b *boundedOutputBuffer) Write(p []byte) (int, error) {
	if b.Len() >= b.limit {
		b.markExceeded()
		return 0, ErrOutputLimit
	}
	remaining := b.limit - b.Len()
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.markExceeded()
		return remaining, ErrOutputLimit
	}
	return b.Buffer.Write(p)
}

// ReadFrom deliberately reads only one byte beyond the budget. bytes.Buffer's
// optimized ReadFrom would otherwise bypass Write and let a pipe accumulate
// unbounded output before the cap was observed.
func (b *boundedOutputBuffer) ReadFrom(r io.Reader) (int64, error) {
	if b.Len() >= b.limit {
		b.markExceeded()
		return 0, ErrOutputLimit
	}
	remaining := b.limit - b.Len()
	data, err := io.ReadAll(io.LimitReader(r, int64(remaining)+1))
	if len(data) > remaining {
		_, _ = b.Buffer.Write(data[:remaining])
		b.markExceeded()
		return int64(len(data)), ErrOutputLimit
	}
	if len(data) > 0 {
		_, _ = b.Buffer.Write(data)
	}
	if err != nil {
		return int64(len(data)), err
	}
	return int64(len(data)), nil
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

func (b *boundedOutputBuffer) limitError() error {
	return &OutputLimitError{Stream: b.stream, Limit: b.limit}
}
