package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// TestLSPClientAgainstFixture exercises the real subprocess transport rather
// than calling handleMessage directly. Keeping this fixture in the test
// binary makes it deterministic, cross-platform, and independent of an
// installed language server.
func TestLSPClientAgainstFixture(t *testing.T) {
	root := t.TempDir()
	msgChan := make(chan any, 16)
	client, err := NewClient(ServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestLSPIntegrationFixtureProcess$", "--"},
		Env:     map[string]string{"TEAK_LSP_INTEGRATION_FIXTURE": "1"},
	}, root, msgChan)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		client.Shutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if !client.WaitForShutdown(ctx) {
			t.Error("fixture LSP server did not shut down")
		}
	})

	if err := client.Initialize(); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if !client.IsReady() || !client.SupportsHover() || !client.SupportsCompletion() ||
		!client.SupportsDefinition() || !client.SupportsReferences() ||
		!client.SupportsRename() || !client.SupportsFormatting() {
		t.Fatalf("initialized fixture did not expose expected capabilities: ready=%v hover=%v completion=%v definition=%v references=%v rename=%v formatting=%v",
			client.IsReady(), client.SupportsHover(), client.SupportsCompletion(),
			client.SupportsDefinition(), client.SupportsReferences(), client.SupportsRename(), client.SupportsFormatting())
	}

	uri := FileURI(root + "/main.go")
	client.DidOpen(uri, "go", 1, "package main\nfunc main() {}\n")

	select {
	case msg := <-msgChan:
		diagnostics, ok := msg.(DiagnosticsMsg)
		if !ok {
			t.Fatalf("first fixture notification = %T, want DiagnosticsMsg", msg)
		}
		if diagnostics.URI != uri || len(diagnostics.Diagnostics) != 1 ||
			diagnostics.Diagnostics[0].Message != "fixture diagnostic" {
			t.Fatalf("fixture diagnostics = %#v", diagnostics)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for fixture diagnostics")
	}

	hover, err := client.Hover(uri, 0, 1)
	if err != nil {
		t.Fatalf("Hover() error = %v", err)
	}
	if hover == nil || hover.Content != "fixture hover" {
		t.Fatalf("Hover() = %#v, want fixture hover", hover)
	}

	completion, err := client.Completion(uri, 0, 1)
	if err != nil {
		t.Fatalf("Completion() error = %v", err)
	}
	if len(completion) != 1 || completion[0].Label != "fixtureCompletion" {
		t.Fatalf("Completion() = %#v, want one fixture item", completion)
	}

	locations, err := client.Definition(uri, 0, 1)
	if err != nil {
		t.Fatalf("Definition() error = %v", err)
	}
	if len(locations) != 1 || locations[0].URI != uri || locations[0].StartLine != 0 {
		t.Fatalf("Definition() = %#v, want one fixture location", locations)
	}
}

func TestProtocolProbeAgainstFixture(t *testing.T) {
	root := t.TempDir()
	err := ProbeProtocol(t.Context(), ServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestLSPIntegrationFixtureProcess$", "--"},
		Env:     map[string]string{"TEAK_LSP_INTEGRATION_FIXTURE": "1"},
	}, root)
	if err != nil {
		t.Fatalf("ProbeProtocol() error = %v", err)
	}
}

func TestLSPIntegrationFixtureProcess(t *testing.T) {
	if os.Getenv("TEAK_LSP_INTEGRATION_FIXTURE") != "1" {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		body, err := readLSPFixtureFrame(reader)
		if err != nil {
			return
		}
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &request) != nil {
			return
		}

		switch request.Method {
		case "initialize":
			sendLSPFixtureResponse(request.ID, map[string]any{
				"capabilities": map[string]any{
					"positionEncoding":           "utf-8",
					"textDocumentSync":           1,
					"hoverProvider":              true,
					"definitionProvider":         true,
					"referencesProvider":         true,
					"renameProvider":             true,
					"documentFormattingProvider": true,
					"completionProvider":         map[string]any{"triggerCharacters": []string{"."}},
				},
				"serverInfo": map[string]string{"name": "teak-fixture"},
			})
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if json.Unmarshal(request.Params, &params) == nil {
				sendLSPFixtureNotification("textDocument/publishDiagnostics", map[string]any{
					"uri":     params.TextDocument.URI,
					"version": 1,
					"diagnostics": []map[string]any{{
						"severity": 1,
						"message":  "fixture diagnostic",
						"range": map[string]any{
							"start": map[string]int{"line": 0, "character": 0},
							"end":   map[string]int{"line": 0, "character": 3},
						},
					}},
				})
			}
		case "textDocument/hover":
			sendLSPFixtureResponse(request.ID, map[string]any{
				"contents": map[string]string{"kind": "plaintext", "value": "fixture hover"},
			})
		case "textDocument/completion":
			sendLSPFixtureResponse(request.ID, map[string]any{
				"isIncomplete": false,
				"items":        []map[string]any{{"label": "fixtureCompletion", "kind": 1}},
			})
		case "textDocument/definition":
			var params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(request.Params, &params)
			sendLSPFixtureResponse(request.ID, []map[string]any{{
				"uri": params.TextDocument.URI,
				"range": map[string]any{
					"start": map[string]int{"line": 0, "character": 0},
					"end":   map[string]int{"line": 0, "character": 4},
				},
			}})
		case "shutdown":
			sendLSPFixtureResponse(request.ID, nil)
		default:
			if request.ID != nil {
				sendLSPFixtureResponse(request.ID, nil)
			}
		}
	}
}

func readLSPFixtureFrame(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = string(bytes.TrimSpace([]byte(line)))
		if line == "" {
			break
		}
		name, value, ok := bytes.Cut([]byte(line), []byte(":"))
		if ok && bytes.EqualFold(bytes.TrimSpace(name), []byte("Content-Length")) {
			if _, err := fmt.Sscanf(string(bytes.TrimSpace(value)), "%d", &contentLength); err != nil {
				return nil, err
			}
		}
	}
	if contentLength < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, contentLength)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func sendLSPFixtureResponse(id *int, result any) {
	if id == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *id, "result": result})
	sendLSPFixturePayload(payload)
}

func sendLSPFixtureNotification(method string, params any) {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	sendLSPFixturePayload(payload)
}

func sendLSPFixturePayload(payload []byte) {
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(payload))
	_, _ = os.Stdout.Write(payload)
}
