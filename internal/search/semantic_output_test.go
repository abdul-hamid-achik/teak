package search

import (
	"io"
	"strings"
	"testing"
)

func TestBoundedCommandBufferCapsIoCopy(t *testing.T) {
	calls := 0
	buffer := &boundedCommandBuffer{limit: 3}
	buffer.onLimit = func() { calls++ }
	if _, err := io.Copy(buffer, strings.NewReader("abcdef")); err == nil {
		t.Fatal("io.Copy() error = nil, want output-limit error")
	}
	if _, err := buffer.WriteString("overflow"); err == nil {
		t.Fatal("WriteString() after limit returned nil, want output-limit error")
	}
	if buffer.Len() != 3 || !buffer.exceeded || calls != 1 {
		t.Fatalf("bounded buffer length=%d exceeded=%t callback-calls=%d, want 3/true/1", buffer.Len(), buffer.exceeded, calls)
	}
}

func TestBoundedCommandBufferAllowsExactIoCopyLimit(t *testing.T) {
	buffer := &boundedCommandBuffer{limit: 3}

	// LimitReader does not implement io.WriterTo, so io.Copy exercises the
	// buffer's ReadFrom override rather than strings.Reader's fast path.
	n, err := io.Copy(buffer, io.LimitReader(strings.NewReader("abc"), 3))
	if err != nil {
		t.Fatalf("io.Copy() error = %v, want nil for an exact fit", err)
	}
	if n != 3 || buffer.Len() != 3 || buffer.exceeded {
		t.Fatalf("bounded buffer n=%d length=%d exceeded=%t, want 3/3/false", n, buffer.Len(), buffer.exceeded)
	}
}
