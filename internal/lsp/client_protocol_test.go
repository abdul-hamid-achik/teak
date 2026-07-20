package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

type captureWriteCloser struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	written chan struct{}
}

func (c *captureWriteCloser) Close() error {
	return nil
}

func (c *captureWriteCloser) WriteString(value string) (int, error) {
	c.mu.Lock()
	n, err := c.buffer.WriteString(value)
	c.mu.Unlock()
	return n, err
}

func (c *captureWriteCloser) Write(value []byte) (int, error) {
	c.mu.Lock()
	n, err := c.buffer.Write(value)
	c.mu.Unlock()
	if c.written != nil {
		select {
		case c.written <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (c *captureWriteCloser) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buffer.Bytes()...)
}

func decodeCapturedMessage(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	content, _, ok := parseMessage(raw)
	if !ok {
		t.Fatalf("parseMessage() failed for %q", string(raw))
	}

	var msg map[string]any
	if err := json.Unmarshal(content, &msg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return msg
}

func TestHandleWorkspaceConfigurationMatchesRequestedItems(t *testing.T) {
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	client := &Client{stdin: stdin}

	client.handleWorkspaceConfiguration(ptrTo(7), json.RawMessage(`{
		"items": [
			{"section":"gopls"},
			{"section":"gopls.formatting"}
		]
	}`))

	msg := decodeCapturedMessage(t, stdin.Bytes())
	if got := int(msg["id"].(float64)); got != 7 {
		t.Fatalf("id = %d, want 7", got)
	}

	results, ok := msg["result"].([]any)
	if !ok {
		t.Fatalf("result = %T, want []any", msg["result"])
	}
	if len(results) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(results))
	}
	if results[0] != nil || results[1] != nil {
		t.Fatalf("result = %#v, want nil entries", results)
	}
}

func TestHandleMessageRespondsToWorkDoneProgressCreate(t *testing.T) {
	stdin := &captureWriteCloser{}
	client := &Client{stdin: stdin}

	client.handleMessage(json.RawMessage(`{
		"jsonrpc": "2.0",
		"id": 11,
		"method": "window/workDoneProgress/create",
		"params": {"token":"format"}
	}`))

	msg := decodeCapturedMessage(t, stdin.Bytes())
	if got := int(msg["id"].(float64)); got != 11 {
		t.Fatalf("id = %d, want 11", got)
	}
	if _, ok := msg["result"]; !ok {
		t.Fatalf("expected result response, got %#v", msg)
	}
}

func TestHandleMessageUnknownRequestReturnsMethodNotFound(t *testing.T) {
	stdin := &captureWriteCloser{}
	client := &Client{stdin: stdin}

	client.handleMessage(json.RawMessage(`{
		"jsonrpc": "2.0",
		"id": 19,
		"method": "client/unknownMethod",
		"params": {}
	}`))

	msg := decodeCapturedMessage(t, stdin.Bytes())
	if got := int(msg["id"].(float64)); got != 19 {
		t.Fatalf("id = %d, want 19", got)
	}

	errVal, ok := msg["error"].(map[string]any)
	if !ok {
		t.Fatalf("error = %T, want map[string]any", msg["error"])
	}
	if got := int(errVal["code"].(float64)); got != jsonrpcMethodNotFound {
		t.Fatalf("error.code = %d, want %d", got, jsonrpcMethodNotFound)
	}
}

func TestWorkspaceApplyEditIsDeferredToTheUIAndRespondsWithItsDecision(t *testing.T) {
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	requests := make(chan any, 1)
	client := &Client{stdin: stdin, msgChan: requests}

	client.handleMessage(json.RawMessage(`{
		"jsonrpc":"2.0",
		"id":23,
		"method":"workspace/applyEdit",
		"params":{
			"label":"safe edit",
			"edit":{"changes":{"file:///workspace/main.go":[{
				"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},
				"newText":"package main\\n"
			}]}}
		}
	}`))

	select {
	case raw := <-requests:
		req, ok := raw.(ApplyEditRequestMsg)
		if !ok {
			t.Fatalf("request = %T, want ApplyEditRequestMsg", raw)
		}
		if req.Label != "safe edit" || req.RequestID != 23 {
			t.Fatalf("request identity = %#v", req)
		}
		if len(req.Edit.Changes) != 1 {
			t.Fatalf("edit = %#v, want one change", req.Edit)
		}
		req.Respond(true, "")
	case <-time.After(time.Second):
		t.Fatal("workspace/applyEdit was not delivered to the UI")
	}

	select {
	case <-stdin.written:
	case <-time.After(time.Second):
		t.Fatal("workspace/applyEdit response was not sent")
	}
	msg := decodeCapturedMessage(t, stdin.Bytes())
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %T, want object", msg["result"])
	}
	if applied, _ := result["applied"].(bool); !applied {
		t.Fatalf("applied = %#v, want true", result["applied"])
	}
	if _, ok := result["failureReason"]; ok {
		t.Fatalf("success response included failureReason: %#v", result)
	}
}

func TestExecuteCommandUsesCallerContextAndProtocolParameters(t *testing.T) {
	stdin := &captureWriteCloser{written: make(chan struct{}, 1)}
	client := &Client{
		stdin:   stdin,
		pending: make(map[int]chan callResult),
		running: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := client.ExecuteCommand(ctx, "gopls.apply_fix", []any{"file:///workspace/main.go"})
		result <- err
	}()

	select {
	case <-stdin.written:
	case <-time.After(time.Second):
		t.Fatal("workspace/executeCommand was not sent")
	}
	request := decodeCapturedMessage(t, stdin.Bytes())
	if got := request["method"]; got != "workspace/executeCommand" {
		t.Fatalf("method = %q, want workspace/executeCommand", got)
	}
	params, ok := request["params"].(map[string]any)
	if !ok || params["command"] != "gopls.apply_fix" {
		t.Fatalf("params = %#v", request["params"])
	}
	id := int(request["id"].(float64))
	client.handleMessage(json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":null}`, id)))
	if err := <-result; err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}
}

func TestExecuteCommandPropagatesContextCancellation(t *testing.T) {
	stdin := &captureWriteCloser{}
	client := &Client{
		stdin:   stdin,
		pending: make(map[int]chan callResult),
		running: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ExecuteCommand(ctx, "gopls.apply_fix", nil)
	if err != context.Canceled {
		t.Fatalf("ExecuteCommand() error = %v, want context.Canceled", err)
	}
	if !bytes.Contains(stdin.Bytes(), []byte(`"method":"$/cancelRequest"`)) {
		t.Fatalf("cancel notification was not sent: %q", stdin.Bytes())
	}
	if len(client.pending) != 0 {
		t.Fatalf("pending requests = %d, want 0 after cancellation", len(client.pending))
	}
}

func ptrTo(v int) *int {
	return &v
}
