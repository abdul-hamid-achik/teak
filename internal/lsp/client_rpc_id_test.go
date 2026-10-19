package lsp

import (
	"encoding/json"
	"testing"
)

func TestHandleMessageAnswersServerRequestWithStringID(t *testing.T) {
	stdin := &captureWriteCloser{}
	client := &Client{stdin: stdin}

	client.handleMessage(json.RawMessage(`{
		"jsonrpc": "2.0",
		"id": "srv-req-1",
		"method": "window/workDoneProgress/create",
		"params": {"token":"format"}
	}`))

	msg := decodeCapturedMessage(t, stdin.Bytes())
	if got, _ := msg["id"].(string); got != "srv-req-1" {
		t.Fatalf("id = %#v, want string %q", msg["id"], "srv-req-1")
	}
	if _, ok := msg["result"]; !ok {
		t.Fatalf("expected result response, got %#v", msg)
	}
}

func TestHandleMessageStringIDConfigurationRequestGetsItemAlignedResult(t *testing.T) {
	stdin := &captureWriteCloser{}
	client := &Client{stdin: stdin}

	client.handleMessage(json.RawMessage(`{
		"jsonrpc": "2.0",
		"id": "cfg-7",
		"method": "workspace/configuration",
		"params": {"items": [{"section":"gopls"}]}
	}`))

	msg := decodeCapturedMessage(t, stdin.Bytes())
	if got, _ := msg["id"].(string); got != "cfg-7" {
		t.Fatalf("id = %#v, want string %q", msg["id"], "cfg-7")
	}
	results, ok := msg["result"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("result = %#v, want one-entry array", msg["result"])
	}
}

func TestHandleMessageMalformedErrorFieldStillReleasesWaiter(t *testing.T) {
	ch := make(chan callResult, 1)
	client := &Client{pending: map[int]chan callResult{5: ch}}

	// An "error" member that is not an object used to fail the whole message
	// decode, leaving the waiter blocked until its context expired.
	client.handleMessage(json.RawMessage(`{"jsonrpc":"2.0","id":5,"error":"boom"}`))

	select {
	case res := <-ch:
		if res.Error == nil {
			t.Fatalf("callResult.Error = nil, want an error for a malformed error member")
		}
	default:
		t.Fatalf("waiter for id 5 was not released by a response with a malformed error member")
	}
}
