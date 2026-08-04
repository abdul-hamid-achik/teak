package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// TestDAPClientAgainstFixture exercises the real adapter subprocess and the
// request/response/event queues. It intentionally uses the same test binary
// as the adapter so the protocol fixture is deterministic on every platform.
func TestDAPClientAgainstFixture(t *testing.T) {
	t.Setenv("TEAK_DAP_INTEGRATION_FIXTURE", "1")
	msgChan := make(chan any, 16)
	client, err := NewClient(os.Args[0], []string{"-test.run=^TestDAPIntegrationFixtureProcess$", "--"}, msgChan)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		client.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if !client.WaitForShutdown(ctx) {
			t.Error("fixture DAP adapter did not shut down")
		}
	})

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !client.IsReady() {
		t.Fatal("fixture DAP client is not ready after Initialize")
	}
	if err := client.Launch("/workspace/main.go"); err != nil {
		t.Fatalf("Launch() error = %v", err)
	}

	breakpoints, err := client.SetBreakpoints("/workspace/main.go", []int{7})
	if err != nil {
		t.Fatalf("SetBreakpoints() error = %v", err)
	}
	if len(breakpoints) != 1 || !breakpoints[0].Verified || breakpoints[0].Line != 7 {
		t.Fatalf("SetBreakpoints() = %#v, want verified line 7", breakpoints)
	}

	threads, err := client.Threads()
	if err != nil {
		t.Fatalf("Threads() error = %v", err)
	}
	if len(threads) != 1 || threads[0].Id != 1 || threads[0].Name != "fixture" {
		t.Fatalf("Threads() = %#v, want fixture thread", threads)
	}

	select {
	case msg := <-msgChan:
		output, ok := msg.(OutputEventMsg)
		if !ok || output.Output != "fixture output\n" {
			t.Fatalf("first fixture event = %#v (%T), want output", msg, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fixture output event")
	}
}

func TestDAPIntegrationFixtureProcess(t *testing.T) {
	if os.Getenv("TEAK_DAP_INTEGRATION_FIXTURE") != "1" {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		body, err := readDAPFixtureFrame(reader)
		if err != nil {
			return
		}
		var request Request
		if json.Unmarshal(body, &request) != nil {
			return
		}

		switch request.Command {
		case "initialize":
			sendDAPFixtureResponse(request, map[string]any{"supportsConfigurationDoneRequest": true})
		case "launch", "configurationDone":
			sendDAPFixtureResponse(request, nil)
			if request.Command == "launch" {
				sendDAPFixtureEvent("output", map[string]any{"category": "console", "output": "fixture output\n"})
			}
		case "setBreakpoints":
			var args SetBreakpointsRequestArgs
			_ = json.Unmarshal(mustJSON(request.Arguments), &args)
			line := 1
			if len(args.Breakpoints) > 0 {
				line = args.Breakpoints[0].Line
			}
			sendDAPFixtureResponse(request, map[string]any{
				"breakpoints": []map[string]any{{"verified": true, "line": line}},
			})
		case "threads":
			sendDAPFixtureResponse(request, map[string]any{"threads": []map[string]any{{"id": 1, "name": "fixture"}}})
		case "disconnect":
			sendDAPFixtureResponse(request, nil)
		default:
			sendDAPFixtureResponse(request, nil)
		}
	}
}

func readDAPFixtureFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = string(bytesTrimSpace([]byte(line)))
		if line == "" {
			break
		}
		var value int
		if _, err := fmt.Sscanf(line, "Content-Length: %d", &value); err == nil {
			contentLength = value
		}
	}
	if contentLength <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func sendDAPFixtureResponse(request Request, body any) {
	payload, _ := json.Marshal(Response{
		Seq:        request.Seq + 100,
		Type:       "response",
		RequestSeq: request.Seq,
		Command:    request.Command,
		Success:    true,
		Body:       mustJSON(body),
	})
	sendDAPFixturePayload(payload)
}

func sendDAPFixtureEvent(name string, body any) {
	payload, _ := json.Marshal(Event{Seq: 999, Type: "event", Event: name, Body: body})
	sendDAPFixturePayload(payload)
}

func sendDAPFixturePayload(payload []byte) {
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(payload))
	_, _ = os.Stdout.Write(payload)
}

func mustJSON(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage("null")
	}
	payload, _ := json.Marshal(value)
	return payload
}

func bytesTrimSpace(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\r' || value[start] == '\n') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\r' || value[end-1] == '\n') {
		end--
	}
	return value[start:end]
}
