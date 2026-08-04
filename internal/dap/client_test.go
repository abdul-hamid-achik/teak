package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestIsDelveDAP(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    bool
	}{
		{
			name:    "dlv dap",
			command: "dlv",
			args:    []string{"dap"},
			want:    true,
		},
		{
			name:    "absolute dlv path",
			command: "/usr/local/bin/dlv",
			args:    []string{"dap", "--log"},
			want:    true,
		},
		{
			name:    "dlv non dap command",
			command: "dlv",
			args:    []string{"debug"},
			want:    false,
		},
		{
			name:    "non dlv adapter",
			command: "debugpy-adapter",
			args:    []string{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDelveDAP(tt.command, tt.args); got != tt.want {
				t.Fatalf("isDelveDAP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagerStartAndLaunchLeavesInactiveOnStartFailure(t *testing.T) {
	manager := NewManager(t.TempDir())
	err := manager.StartAndLaunch(DebugConfig{Command: "teak-definitely-missing-debug-adapter"})
	if err == nil {
		t.Fatal("StartAndLaunch() error = nil, want missing adapter error")
	}
	if got := manager.State(); got != StateInactive {
		t.Fatalf("State() = %v, want inactive after failed start", got)
	}
}

func TestHandleEventDoesNotBlockOnFullMessageChannel(t *testing.T) {
	msgChan := make(chan any)
	client := &Client{msgChan: msgChan}
	done := make(chan struct{})
	go func() {
		client.handleEvent(&Event{Event: "terminated"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("DAP event delivery blocked on an unread UI channel")
	}
}

func TestLifecycleEventMarksClientNotReady(t *testing.T) {
	client := &Client{msgChan: make(chan any, 1), initialized: true, running: true}
	client.handleEvent(&Event{Event: "terminated"})
	if client.IsReady() {
		t.Fatal("terminated adapter remained ready")
	}
	client.initialized = true
	client.running = true
	client.handleEvent(&Event{Event: "exited", Body: map[string]any{"exitCode": float64(1)}})
	if client.IsReady() {
		t.Fatal("exited adapter remained ready")
	}
}

func TestRequestReturnsWhenDAPTransportEnds(t *testing.T) {
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer readPipe.Close()
	defer writePipe.Close()

	client := &Client{
		stdin:       writePipe,
		pending:     make(map[int]chan callResult),
		processDone: make(chan struct{}),
		readDone:    make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.sendRequestContext(ctx, "threads", nil, nil) }()
	time.Sleep(10 * time.Millisecond)
	close(client.readDone)

	select {
	case requestErr := <-done:
		if requestErr == nil || !strings.Contains(requestErr.Error(), "transport") {
			t.Fatalf("request error = %v, want transport closure", requestErr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("request remained blocked after DAP transport ended")
	}
}

func TestEventDeliveryPrioritizesLifecycleOverOutputFlood(t *testing.T) {
	msgChan := make(chan any, 1)
	client := &Client{msgChan: msgChan}
	client.initEventDelivery()
	defer client.stopEventDelivery()

	for range 256 {
		client.handleEvent(&Event{
			Event: "output",
			Body:  map[string]any{"output": "noisy\n"},
		})
	}
	client.handleEvent(&Event{Event: "terminated"})

	// At most two outputs can already be in flight (one in the UI channel and
	// one selected by the dispatcher). The lifecycle notification must then
	// beat all output still queued internally.
	for received := 0; ; received++ {
		select {
		case msg := <-msgChan:
			if _, ok := msg.(TerminatedEventMsg); ok {
				if received > 2 {
					t.Fatalf("terminated event followed %d outputs, want at most 2 in-flight outputs", received)
				}
				return
			}
			if _, ok := msg.(OutputEventMsg); !ok {
				t.Fatalf("delivered event = %T, want output or TerminatedEventMsg", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("terminated event was not delivered after output flood")
		}
	}
}

func TestEventDeliveryPreservesCriticalEventOrder(t *testing.T) {
	msgChan := make(chan any)
	client := &Client{msgChan: msgChan}
	client.initEventDelivery()
	defer client.stopEventDelivery()

	client.handleEvent(&Event{Event: "stopped", Body: map[string]any{"reason": "breakpoint"}})
	client.handleEvent(&Event{Event: "continued", Body: map[string]any{"threadId": float64(1)}})
	client.handleEvent(&Event{Event: "exited", Body: map[string]any{"exitCode": float64(0)}})
	client.handleEvent(&Event{Event: "terminated"})

	want := []string{"stopped", "continued", "exited", "terminated"}
	for _, expected := range want {
		select {
		case msg := <-msgChan:
			var got string
			switch msg.(type) {
			case StoppedEventMsg:
				got = "stopped"
			case ContinuedEventMsg:
				got = "continued"
			case ExitedEventMsg:
				got = "exited"
			case TerminatedEventMsg:
				got = "terminated"
			}
			if got != expected {
				t.Fatalf("critical event order = %q, want %q", got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", expected)
		}
	}
}

func TestStopEventDeliveryUnblocksStalledDispatcher(t *testing.T) {
	msgChan := make(chan any)
	client := &Client{msgChan: msgChan}
	client.initEventDelivery()
	client.handleEvent(&Event{Event: "terminated"})

	client.stopEventDelivery()
	select {
	case <-client.eventDone:
	case <-time.After(time.Second):
		t.Fatal("event dispatcher remained blocked after stop")
	}
}

func TestEventDeliveryStopsAfterProtocolEndsWithNoQueuedEvents(t *testing.T) {
	msgChan := make(chan any)
	readDone := make(chan struct{})
	client := &Client{msgChan: msgChan, readDone: readDone}
	client.initEventDelivery()
	close(readDone)

	select {
	case <-client.eventDone:
	case <-time.After(time.Second):
		t.Fatal("idle event dispatcher survived protocol shutdown")
	}
}

func TestCriticalQueueKeepsTerminationAfterLifecycleFlood(t *testing.T) {
	client := &Client{}
	for range maxDAPQueuedCritical {
		client.enqueueCriticalEventLocked(StoppedEventMsg{Reason: "step"})
	}
	client.enqueueCriticalEventLocked(TerminatedEventMsg{})

	if got := len(client.criticalEvents); got != maxDAPQueuedCritical {
		t.Fatalf("critical queue length = %d, want %d", got, maxDAPQueuedCritical)
	}
	if _, ok := client.criticalEvents[len(client.criticalEvents)-1].(TerminatedEventMsg); !ok {
		t.Fatalf("last critical event = %T, want TerminatedEventMsg", client.criticalEvents[len(client.criticalEvents)-1])
	}
}

func TestClientShutdownReturnsBeforeAStuckAdapterExits(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDAPShutdownHelperProcess$", "--")
	cmd.Env = append(os.Environ(), "TEAK_DAP_SHUTDOWN_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		pending:     make(map[int]chan callResult),
		running:     true,
		processDone: make(chan struct{}),
	}
	go client.reapProcess()
	started := time.Now()
	client.Shutdown()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("Shutdown() blocked for %s", elapsed)
	}
	if !waitForDone(client.processDone, 4*time.Second) {
		t.Fatal("adapter was not killed and reaped")
	}
}

func TestManagerWaitForShutdownReapsStoppedAdapter(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDAPShutdownHelperProcess$", "--")
	cmd.Env = append(os.Environ(), "TEAK_DAP_SHUTDOWN_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      stdout,
		pending:     make(map[int]chan callResult),
		running:     true,
		processDone: make(chan struct{}),
	}
	go client.reapProcess()

	manager := NewManager(t.TempDir())
	manager.client = client
	manager.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if !manager.WaitForShutdown(ctx) {
		t.Fatal("WaitForShutdown() returned before the adapter was reaped")
	}
	if cmd.ProcessState == nil {
		t.Fatal("adapter process was not reaped")
	}
}

func TestDAPShutdownHelperProcess(t *testing.T) {
	if os.Getenv("TEAK_DAP_SHUTDOWN_HELPER") != "1" {
		return
	}
	select {}
}

func TestFindClientAddrArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{
			name: "equals form",
			args: []string{"dap", "--client-addr=127.0.0.1:12345"},
			want: "127.0.0.1:12345",
			ok:   true,
		},
		{
			name: "separate form",
			args: []string{"dap", "--client-addr", "127.0.0.1:22345"},
			want: "127.0.0.1:22345",
			ok:   true,
		},
		{
			name: "missing value after flag",
			args: []string{"dap", "--client-addr"},
			want: "",
			ok:   false,
		},
		{
			name: "not present",
			args: []string{"dap"},
			want: "",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := findClientAddrArg(tt.args)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("findClientAddrArg() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestAddClientAddrArg(t *testing.T) {
	args := []string{"dap", "--log"}
	got := addClientAddrArg(args, "127.0.0.1:0")
	if len(got) != len(args)+1 {
		t.Fatalf("len(got) = %d, want %d", len(got), len(args)+1)
	}
	if got[len(got)-1] != "--client-addr=127.0.0.1:0" {
		t.Fatalf("last arg = %q, want --client-addr=127.0.0.1:0", got[len(got)-1])
	}
}

func TestSendRequest_SeqMatchesPending(t *testing.T) {
	// Verify that the pending map key matches the Seq used in the request.
	// This was a bug where requestID and seq were separate counters.
	c := &Client{
		pending: make(map[int]chan callResult),
		seq:     0,
	}

	// Simulate what sendRequest does: get seq, store in pending
	seq := c.nextSeq()
	ch := make(chan callResult, 1)
	c.pending[seq] = ch

	// Verify the seq is in pending
	if _, ok := c.pending[seq]; !ok {
		t.Fatalf("pending map should contain seq %d", seq)
	}

	// Simulate a response arriving with RequestSeq matching our seq
	resp := Response{
		Type:       "response",
		RequestSeq: seq,
		Success:    true,
		Body:       json.RawMessage(`{}`),
	}

	// Look up in pending like handleMessage does
	pendingCh, ok := c.pending[resp.RequestSeq]
	if !ok {
		t.Fatalf("response with RequestSeq=%d should match pending entry", resp.RequestSeq)
	}
	pendingCh <- callResult{Result: resp.Body}

	result := <-ch
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

func TestNextSeq_Increments(t *testing.T) {
	c := &Client{seq: 0}

	s1 := c.nextSeq()
	s2 := c.nextSeq()
	s3 := c.nextSeq()

	if s1 != 1 || s2 != 2 || s3 != 3 {
		t.Errorf("nextSeq() = %d, %d, %d; want 1, 2, 3", s1, s2, s3)
	}
}

func TestHandleMessage_SuccessResponse(t *testing.T) {
	ch := make(chan callResult, 1)
	c := &Client{
		pending: map[int]chan callResult{
			5: ch,
		},
	}

	data := []byte(`{"seq":1,"type":"response","request_seq":5,"command":"initialize","success":true,"body":{"supportsConfigurationDoneRequest":true}}`)
	c.handleMessage(data)

	result := <-ch
	if result.Error != nil {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if result.Result == nil {
		t.Fatal("expected non-nil result body")
	}
}

func TestHandleMessage_ErrorResponse_WithBody(t *testing.T) {
	ch := make(chan callResult, 1)
	c := &Client{
		pending: map[int]chan callResult{
			3: ch,
		},
	}

	// Error response with structured body containing error details
	data := []byte(`{"seq":2,"type":"response","request_seq":3,"command":"launch","success":false,"message":"Launch failed","body":{"error":{"id":1001,"format":"Could not launch process: {reason}","message":"process not found"}}}`)
	c.handleMessage(data)

	result := <-ch
	if result.Error == nil {
		t.Fatal("expected error response")
	}
	if result.Error.Id != 1001 {
		t.Errorf("error id = %d, want 1001", result.Error.Id)
	}
	if result.Error.Format != "Could not launch process: {reason}" {
		t.Errorf("error format = %q, want structured format string", result.Error.Format)
	}
	if result.Error.Message != "process not found" {
		t.Errorf("error message = %q, want 'process not found'", result.Error.Message)
	}
}

func TestHandleMessage_ErrorResponse_NoBody(t *testing.T) {
	ch := make(chan callResult, 1)
	c := &Client{
		pending: map[int]chan callResult{
			7: ch,
		},
	}

	data := []byte(`{"seq":4,"type":"response","request_seq":7,"command":"next","success":false,"message":"Thread not found"}`)
	c.handleMessage(data)

	result := <-ch
	if result.Error == nil {
		t.Fatal("expected error response")
	}
	if result.Error.Message != "Thread not found" {
		t.Errorf("error message = %q, want 'Thread not found'", result.Error.Message)
	}
}

func TestHandleMessage_UnmatchedResponse(t *testing.T) {
	c := &Client{
		pending: make(map[int]chan callResult),
	}

	// Response with no matching pending entry should not panic
	data := []byte(`{"seq":1,"type":"response","request_seq":999,"command":"test","success":true}`)
	c.handleMessage(data) // should not panic or block
}

func TestHandleMessage_DuplicateResponseDoesNotBlockProtocolReader(t *testing.T) {
	ch := make(chan callResult, 1)
	c := &Client{
		pending: map[int]chan callResult{
			5: ch,
		},
	}

	data := []byte(`{"seq":1,"type":"response","request_seq":5,"command":"initialize","success":true,"body":{}}`)
	c.handleMessage(data) // fills the one response slot for the request.

	done := make(chan struct{})
	go func() {
		c.handleMessage(data) // a broken adapter may repeat the same response.
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("duplicate DAP response blocked the protocol reader")
	}
}

func TestHandleEventBoundsOutputPayload(t *testing.T) {
	msgChan := make(chan any, 1)
	client := &Client{msgChan: msgChan}
	payload := strings.Repeat("é", maxDAPOutputEventBytes)

	client.handleEvent(&Event{
		Event: "output",
		Body: map[string]any{
			"category": "stdout",
			"output":   payload,
		},
	})

	msg := (<-msgChan).(OutputEventMsg)
	if len(msg.Output) > maxDAPOutputEventBytes {
		t.Fatalf("output bytes = %d, want <= %d", len(msg.Output), maxDAPOutputEventBytes)
	}
	if !strings.HasSuffix(msg.Output, "é") {
		t.Fatalf("bounded output split UTF-8 or lost newest tail: %q", msg.Output[len(msg.Output)-8:])
	}
}

func TestHandleEventBoundsLifecycleText(t *testing.T) {
	msgChan := make(chan any, 1)
	client := &Client{msgChan: msgChan}
	payload := strings.Repeat("é", maxDAPOutputEventBytes)

	client.handleEvent(&Event{
		Event: "stopped",
		Body: map[string]any{
			"reason":      payload,
			"description": payload,
		},
	})

	msg := (<-msgChan).(StoppedEventMsg)
	if len(msg.Reason) > maxDAPOutputEventBytes || len(msg.Description) > maxDAPOutputEventBytes {
		t.Fatalf("lifecycle text escaped cap: reason=%d description=%d", len(msg.Reason), len(msg.Description))
	}
	if !strings.HasSuffix(msg.Description, "é") {
		t.Fatal("bounded lifecycle text split UTF-8")
	}
}

func TestHandleMessage_Event(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	data := []byte(`{"seq":1,"type":"event","event":"stopped","body":{"reason":"breakpoint","threadId":1,"allThreadsStopped":true}}`)
	c.handleMessage(data)

	msg := <-msgChan
	stopped, ok := msg.(StoppedEventMsg)
	if !ok {
		t.Fatalf("expected StoppedEventMsg, got %T", msg)
	}
	if stopped.Reason != "breakpoint" {
		t.Errorf("reason = %q, want 'breakpoint'", stopped.Reason)
	}
	if stopped.ThreadId != 1 {
		t.Errorf("threadId = %d, want 1", stopped.ThreadId)
	}
}

// --- New tests below ---

func TestHandleEvent_Continued(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	event := &Event{
		Type:  "event",
		Event: "continued",
		Body:  map[string]any{"threadId": float64(3), "allThreadsContinued": true},
	}
	c.handleEvent(event)

	msg := <-msgChan
	cont, ok := msg.(ContinuedEventMsg)
	if !ok {
		t.Fatalf("expected ContinuedEventMsg, got %T", msg)
	}
	if cont.ThreadId != 3 {
		t.Errorf("threadId = %d, want 3", cont.ThreadId)
	}
	if !cont.AllThreadsContinued {
		t.Error("AllThreadsContinued = false, want true")
	}
}

func TestHandleEvent_Exited(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	event := &Event{
		Type:  "event",
		Event: "exited",
		Body:  map[string]any{"exitCode": float64(42)},
	}
	c.handleEvent(event)

	msg := <-msgChan
	exited, ok := msg.(ExitedEventMsg)
	if !ok {
		t.Fatalf("expected ExitedEventMsg, got %T", msg)
	}
	if exited.ExitCode != 42 {
		t.Errorf("exitCode = %d, want 42", exited.ExitCode)
	}
}

func TestHandleEvent_Terminated(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	event := &Event{
		Type:  "event",
		Event: "terminated",
	}
	c.handleEvent(event)

	msg := <-msgChan
	if _, ok := msg.(TerminatedEventMsg); !ok {
		t.Fatalf("expected TerminatedEventMsg, got %T", msg)
	}
}

func TestHandleEvent_Output(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	event := &Event{
		Type:  "event",
		Event: "output",
		Body:  map[string]any{"category": "console", "output": "Hello, World!\n"},
	}
	c.handleEvent(event)

	msg := <-msgChan
	out, ok := msg.(OutputEventMsg)
	if !ok {
		t.Fatalf("expected OutputEventMsg, got %T", msg)
	}
	if out.Category != "console" {
		t.Errorf("category = %q, want 'console'", out.Category)
	}
	if out.Output != "Hello, World!\n" {
		t.Errorf("output = %q, want 'Hello, World!\\n'", out.Output)
	}
}

func TestHandleEvent_Breakpoint(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	event := &Event{
		Type:  "event",
		Event: "breakpoint",
		Body: map[string]any{
			"reason":   "changed",
			"verified": true,
			"message":  "Breakpoint verified",
			"line":     float64(25),
		},
	}
	c.handleEvent(event)

	msg := <-msgChan
	bp, ok := msg.(BreakpointEventMsg)
	if !ok {
		t.Fatalf("expected BreakpointEventMsg, got %T", msg)
	}
	if bp.Reason != "changed" {
		t.Errorf("reason = %q, want 'changed'", bp.Reason)
	}
	if !bp.Breakpoint.Verified {
		t.Error("verified = false, want true")
	}
	if bp.Breakpoint.Line != 25 {
		t.Errorf("line = %d, want 25", bp.Breakpoint.Line)
	}
	if bp.Breakpoint.Message != "Breakpoint verified" {
		t.Errorf("message = %q, want 'Breakpoint verified'", bp.Breakpoint.Message)
	}
}

func TestHandleEvent_BreakpointNestedBody(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	event := &Event{
		Type:  "event",
		Event: "breakpoint",
		Body: map[string]any{
			"reason": "changed",
			"breakpoint": map[string]any{
				"verified": true,
				"message":  "verified by adapter",
				"line":     float64(42),
				"source": map[string]any{
					"name": "main.go",
					"path": "/tmp/main.go",
				},
			},
		},
	}
	c.handleEvent(event)

	msg := <-msgChan
	bp, ok := msg.(BreakpointEventMsg)
	if !ok {
		t.Fatalf("expected BreakpointEventMsg, got %T", msg)
	}
	if bp.Reason != "changed" {
		t.Fatalf("reason = %q, want %q", bp.Reason, "changed")
	}
	if !bp.Breakpoint.Verified {
		t.Fatal("verified = false, want true")
	}
	if bp.Breakpoint.Line != 42 {
		t.Fatalf("line = %d, want 42", bp.Breakpoint.Line)
	}
	if bp.Breakpoint.Source.Path != "/tmp/main.go" {
		t.Fatalf("source.path = %q, want %q", bp.Breakpoint.Source.Path, "/tmp/main.go")
	}
}

func TestHandleEvent_NilMsgChan(t *testing.T) {
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: nil,
	}

	// Should not panic when msgChan is nil
	event := &Event{
		Type:  "event",
		Event: "stopped",
		Body:  map[string]any{"reason": "breakpoint"},
	}
	c.handleEvent(event)
}

func TestHandleEvent_UnknownEvent(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	event := &Event{
		Type:  "event",
		Event: "custom_unknown_event",
		Body:  map[string]any{"foo": "bar"},
	}
	c.handleEvent(event)

	// Unknown events should be silently ignored
	if len(msgChan) != 0 {
		t.Errorf("expected no messages for unknown event, got %d", len(msgChan))
	}
}

func TestHandleEvent_NonMapBody(t *testing.T) {
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	// Body that isn't map[string]any should be silently ignored (no panic)
	event := &Event{
		Type:  "event",
		Event: "stopped",
		Body:  "not a map",
	}
	c.handleEvent(event)

	if len(msgChan) != 0 {
		t.Errorf("expected no messages when body is not a map, got %d", len(msgChan))
	}
}

func TestGetStr(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want string
	}{
		{"existing string", map[string]any{"k": "hello"}, "k", "hello"},
		{"missing key", map[string]any{"k": "hello"}, "missing", ""},
		{"non-string value (int)", map[string]any{"k": float64(42)}, "k", ""},
		{"non-string value (bool)", map[string]any{"k": true}, "k", ""},
		{"empty string", map[string]any{"k": ""}, "k", ""},
		{"nil value", map[string]any{"k": nil}, "k", ""},
		{"empty map", map[string]any{}, "k", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStr(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("getStr(%v, %q) = %q, want %q", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

func TestGetInt(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want int
	}{
		{"float64 value", map[string]any{"k": float64(42)}, "k", 42},
		{"int value", map[string]any{"k": 7}, "k", 7},
		{"missing key", map[string]any{"k": float64(1)}, "missing", 0},
		{"string value", map[string]any{"k": "not an int"}, "k", 0},
		{"bool value", map[string]any{"k": true}, "k", 0},
		{"nil value", map[string]any{"k": nil}, "k", 0},
		{"zero float64", map[string]any{"k": float64(0)}, "k", 0},
		{"negative float64", map[string]any{"k": float64(-5)}, "k", -5},
		{"empty map", map[string]any{}, "k", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getInt(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("getInt(%v, %q) = %d, want %d", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

func TestGetBool(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]any
		key  string
		want bool
	}{
		{"true value", map[string]any{"k": true}, "k", true},
		{"false value", map[string]any{"k": false}, "k", false},
		{"missing key", map[string]any{"k": true}, "missing", false},
		{"string value", map[string]any{"k": "true"}, "k", false},
		{"int value", map[string]any{"k": float64(1)}, "k", false},
		{"nil value", map[string]any{"k": nil}, "k", false},
		{"empty map", map[string]any{}, "k", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBool(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("getBool(%v, %q) = %v, want %v", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

func TestHandleMessage_InvalidJSON(t *testing.T) {
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: make(chan any, 10),
	}

	// Invalid JSON should not panic
	c.handleMessage([]byte(`{invalid json`))
}

func TestHandleMessage_EmptyBody(t *testing.T) {
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: make(chan any, 10),
	}

	// Empty bytes should not panic
	c.handleMessage([]byte{})
}

func TestReadDAPFrameValidatesContentLength(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantError string
	}{
		{
			name:  "valid frame",
			input: "Content-Length: 2\r\n\r\n{}",
			want:  "{}",
		},
		{
			name:      "missing content length",
			input:     "X-Header: value\r\n\r\n",
			wantError: "missing Content-Length",
		},
		{
			name:      "negative content length",
			input:     "Content-Length: -1\r\n\r\n",
			wantError: "invalid Content-Length",
		},
		{
			name:      "zero content length",
			input:     "Content-Length: 0\r\n\r\n",
			wantError: "invalid Content-Length",
		},
		{
			name:      "oversized content length",
			input:     "Content-Length: 16777217\r\n\r\n",
			wantError: "exceeds limit",
		},
		{
			name:      "malformed content length",
			input:     "Content-Length: nope\r\n\r\n",
			wantError: "invalid Content-Length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readDAPFrame(bufio.NewReader(strings.NewReader(tt.input)))
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("readDAPFrame() error = %v, want containing %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("readDAPFrame() error = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("readDAPFrame() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsExpectedDAPReadError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", want: true},
		{name: "eof", err: io.EOF, want: true},
		{name: "closed file", err: errors.New("read |0: file already closed"), want: true},
		{name: "closed pipe", err: errors.New("write: closed pipe"), want: true},
		{name: "protocol failure", err: errors.New("malformed DAP header"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExpectedDAPReadError(tt.err); got != tt.want {
				t.Fatalf("isExpectedDAPReadError(%v) = %t, want %t", tt.err, got, tt.want)
			}
		})
	}
}

func TestHandleMessage_ErrorResponse_BodyWithFormat(t *testing.T) {
	ch := make(chan callResult, 1)
	c := &Client{
		pending: map[int]chan callResult{
			10: ch,
		},
	}

	// Error with format but no message in inner error — should use resp.Message as fallback
	data := []byte(`{"seq":1,"type":"response","request_seq":10,"command":"test","success":false,"message":"outer message","body":{"error":{"id":42,"format":"formatted: {detail}"}}}`)
	c.handleMessage(data)

	result := <-ch
	if result.Error == nil {
		t.Fatal("expected error response")
	}
	if result.Error.Id != 42 {
		t.Errorf("error id = %d, want 42", result.Error.Id)
	}
	if result.Error.Format != "formatted: {detail}" {
		t.Errorf("error format = %q, want 'formatted: {detail}'", result.Error.Format)
	}
	// Message should fall back to outer resp.Message since inner message is empty
	if result.Error.Message != "outer message" {
		t.Errorf("error message = %q, want 'outer message'", result.Error.Message)
	}
}

func TestHandleMessage_ErrorResponse_BodyInvalidJSON(t *testing.T) {
	ch := make(chan callResult, 1)
	c := &Client{
		pending: map[int]chan callResult{
			11: ch,
		},
	}

	// Error response with body that isn't valid structured error
	data := []byte(`{"seq":1,"type":"response","request_seq":11,"command":"test","success":false,"message":"some error","body":{"notAnError": true}}`)
	c.handleMessage(data)

	result := <-ch
	if result.Error == nil {
		t.Fatal("expected error response")
	}
	// Should still have the outer message
	if result.Error.Message != "some error" {
		t.Errorf("error message = %q, want 'some error'", result.Error.Message)
	}
}

func TestHandleMessage_EventViaHandleMessage(t *testing.T) {
	// Test event dispatch through handleMessage (full JSON path)
	msgChan := make(chan any, 10)
	c := &Client{
		pending: make(map[int]chan callResult),
		msgChan: msgChan,
	}

	tests := []struct {
		name    string
		data    string
		wantTyp string
	}{
		{
			"continued event",
			`{"seq":1,"type":"event","event":"continued","body":{"threadId":2,"allThreadsContinued":false}}`,
			"ContinuedEventMsg",
		},
		{
			"exited event",
			`{"seq":2,"type":"event","event":"exited","body":{"exitCode":0}}`,
			"ExitedEventMsg",
		},
		{
			"terminated event",
			`{"seq":3,"type":"event","event":"terminated"}`,
			"TerminatedEventMsg",
		},
		{
			"output event",
			`{"seq":4,"type":"event","event":"output","body":{"category":"stdout","output":"test output"}}`,
			"OutputEventMsg",
		},
		{
			"breakpoint event",
			`{"seq":5,"type":"event","event":"breakpoint","body":{"reason":"new","verified":false,"line":10}}`,
			"BreakpointEventMsg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.handleMessage([]byte(tt.data))
			msg := <-msgChan
			var gotTyp string
			switch msg.(type) {
			case ContinuedEventMsg:
				gotTyp = "ContinuedEventMsg"
			case ExitedEventMsg:
				gotTyp = "ExitedEventMsg"
			case TerminatedEventMsg:
				gotTyp = "TerminatedEventMsg"
			case OutputEventMsg:
				gotTyp = "OutputEventMsg"
			case BreakpointEventMsg:
				gotTyp = "BreakpointEventMsg"
			default:
				gotTyp = "unknown"
			}
			if gotTyp != tt.wantTyp {
				t.Errorf("got %s, want %s", gotTyp, tt.wantTyp)
			}
		})
	}
}

func TestDebugState_String(t *testing.T) {
	tests := []struct {
		state DebugState
		want  string
	}{
		{StateInactive, "inactive"},
		{StateRunning, "running"},
		{StateStopped, "stopped"},
		{StatePaused, "paused"},
		{DebugState(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("DebugState(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestIsReady(t *testing.T) {
	c := &Client{}
	if c.IsReady() {
		t.Error("IsReady() should be false before initialization")
	}

	c.initialized = true
	if !c.IsReady() {
		t.Error("IsReady() should be true after initialization")
	}
}
