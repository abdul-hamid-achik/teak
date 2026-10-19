package dap

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

// newPipedAdapterClient wires a Client to an in-memory fake adapter. handle
// answers each client request with a response body (ok=false marks it failed).
// The returned send writes an adapter-originated frame such as an event.
func newPipedAdapterClient(t *testing.T, handle func(req Request) (any, bool)) (*Client, func(frame any)) {
	t.Helper()
	adapterIn, clientOut := io.Pipe()
	clientIn, adapterOut := io.Pipe()
	c := &Client{
		stdin:       clientOut,
		stdout:      clientIn,
		pending:     make(map[int]chan callResult),
		processDone: make(chan struct{}),
		readDone:    make(chan struct{}),
	}
	go c.readLoop()

	var writeMu sync.Mutex
	write := func(frame any) {
		data, err := json.Marshal(frame)
		if err != nil {
			t.Errorf("marshal fake adapter frame: %v", err)
			return
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_, _ = fmt.Fprintf(adapterOut, "Content-Length: %d\r\n\r\n", len(data))
		_, _ = adapterOut.Write(data)
	}

	go func() {
		reader := bufio.NewReader(adapterIn)
		for {
			data, err := readDAPFrame(reader)
			if err != nil {
				return
			}
			var req Request
			if err := json.Unmarshal(data, &req); err != nil {
				return
			}
			body, ok := handle(req)
			write(map[string]any{
				"seq":         0,
				"type":        "response",
				"request_seq": req.Seq,
				"command":     req.Command,
				"success":     ok,
				"body":        body,
			})
		}
	}()

	t.Cleanup(func() {
		_ = clientOut.Close()
		_ = adapterOut.Close()
	})
	return c, write
}

// TestManagerStepsTheStoppedThread reproduces a stop in a thread that is not
// first in the adapter's thread list: the step request must target the thread
// named by the stopped event, not threads[0].
func TestManagerStepsTheStoppedThread(t *testing.T) {
	nextThread := make(chan int, 1)
	client, send := newPipedAdapterClient(t, func(req Request) (any, bool) {
		switch req.Command {
		case "threads":
			return map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 7, "name": "worker"}}}, true
		case "next":
			args, _ := req.Arguments.(map[string]any)
			id, _ := args["threadId"].(float64)
			nextThread <- int(id)
		}
		return nil, true
	})
	m := NewManager(t.TempDir())
	m.client = client

	send(map[string]any{"seq": 0, "type": "event", "event": "stopped",
		"body": map[string]any{"reason": "breakpoint", "threadId": 7}})
	if err := m.Next(); err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	select {
	case got := <-nextThread:
		if got != 7 {
			t.Fatalf("next requested threadId %d, want 7 (the stopped thread)", got)
		}
	case <-time.After(time.Second):
		t.Fatal("adapter never received a next request")
	}
}

// TestManagerStepWithNoThreadsReportsError ensures a step that could not reach
// any thread is reported as a failure instead of silently succeeding.
func TestManagerStepWithNoThreadsReportsError(t *testing.T) {
	client, _ := newPipedAdapterClient(t, func(req Request) (any, bool) {
		if req.Command == "threads" {
			return map[string]any{"threads": []any{}}, true
		}
		return nil, true
	})
	m := NewManager(t.TempDir())
	m.client = client

	steps := map[string]func() error{
		"Continue": m.Continue,
		"Next":     m.Next,
		"StepIn":   m.StepIn,
		"StepOut":  m.StepOut,
	}
	for name, step := range steps {
		if err := step(); err == nil {
			t.Errorf("%s() with no threads returned nil, want an error", name)
		}
	}
}
