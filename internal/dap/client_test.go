package dap

import (
	"encoding/json"
	"testing"
)

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
