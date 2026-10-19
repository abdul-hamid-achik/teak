package plugin

import (
	"testing"

	"teak/internal/text"
)

// bufferIndexRecorder records the zero-based line and column values that
// buffer.get_line and buffer.set_cursor hand to the runtime.
type bufferIndexRecorder struct {
	apiRuntimeStub
	lineRequests   []int
	cursorRequests []text.Position
}

func (r *bufferIndexRecorder) BufferLine(line int) (string, error) {
	r.lineRequests = append(r.lineRequests, line)
	return r.apiRuntimeStub.BufferLine(line)
}

func (r *bufferIndexRecorder) SetBufferCursor(pos text.Position) error {
	r.cursorRequests = append(r.cursorRequests, pos)
	return nil
}

func TestLuaBufferRejectsNonPositiveOneBasedIndices(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"get_line zero", "buffer.get_line(0)"},
		{"get_line negative", "buffer.get_line(-3)"},
		{"set_cursor zero line", "buffer.set_cursor(0, 1)"},
		{"set_cursor zero column", "buffer.set_cursor(1, 0)"},
		{"set_cursor negative column", "buffer.set_cursor(1, -2)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := newLuaStateFactory().Get()
			defer L.Close()
			registerTestModule(L, "buffer", registerBufferAPI)

			runtime := &bufferIndexRecorder{}
			setRuntimeForState(L, runtime)
			defer clearRuntimeForState(L)

			if err := L.DoString(tt.src); err == nil {
				t.Fatalf("%s: expected an error for a non-1-based index", tt.src)
			}
			if len(runtime.lineRequests) != 0 || len(runtime.cursorRequests) != 0 {
				t.Fatalf("%s: runtime was called with lines=%v cursors=%v; the index must be rejected before reaching it",
					tt.src, runtime.lineRequests, runtime.cursorRequests)
			}
		})
	}
}

func TestLuaBufferOneBasedIndicesAreZeroBasedAtBoundary(t *testing.T) {
	L := newLuaStateFactory().Get()
	defer L.Close()
	registerTestModule(L, "buffer", registerBufferAPI)

	runtime := &bufferIndexRecorder{}
	setRuntimeForState(L, runtime)
	defer clearRuntimeForState(L)

	if err := L.DoString("buffer.get_line(2)\nbuffer.set_cursor(3, 4)"); err != nil {
		t.Fatalf("valid 1-based indices failed: %v", err)
	}
	if len(runtime.lineRequests) != 1 || runtime.lineRequests[0] != 1 {
		t.Fatalf("get_line(2) forwarded %v, want zero-based [1]", runtime.lineRequests)
	}
	if len(runtime.cursorRequests) != 1 || runtime.cursorRequests[0] != (text.Position{Line: 2, Col: 3}) {
		t.Fatalf("set_cursor(3, 4) forwarded %v, want zero-based {2 3}", runtime.cursorRequests)
	}
}
