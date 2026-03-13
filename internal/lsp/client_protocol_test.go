package lsp

import (
	"bytes"
	"encoding/json"
	"testing"
)

type captureWriteCloser struct {
	bytes.Buffer
}

func (c *captureWriteCloser) Close() error {
	return nil
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
	stdin := &captureWriteCloser{}
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

func ptrTo(v int) *int {
	return &v
}
