package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
)

// newLspRequestErrorModel points the active editor at an extension served by
// the fixture language server and installs a manager that can reach it.
func newLspRequestErrorModel(t *testing.T) (Model, *lsp.Manager, string) {
	t.Helper()

	model := newOverlayRequestTestModel(t)
	root := t.TempDir()
	filePath := filepath.Join(root, "main.lsperr")
	model.editors[0].Buffer.FilePath = filePath
	model.tabBar.Tabs[0].FilePath = filePath

	mgr := lsp.NewManager(root, []lsp.ServerConfig{{
		Extensions: []string{".lsperr"},
		Command:    os.Args[0],
		Args:       []string{"-test.run=^TestAppLSPRequestErrorFixtureProcess$", "--"},
		Env:        map[string]string{"TEAK_APP_LSP_REQUEST_ERROR_FIXTURE": "1"},
		LanguageID: "lsperr",
	}})
	t.Cleanup(mgr.ShutdownAll)
	if _, err := mgr.EnsureClient(filePath); err != nil {
		t.Fatalf("EnsureClient() error = %v", err)
	}
	model.lspMgr = mgr
	return model, mgr, filePath
}

// TestAppLSPRequestErrorFixtureProcess is not a test: it is the fixture
// language server process itself. It answers initialize, then fails every
// documented request method with a JSON-RPC error so the client surfaces a
// real request error from a live server.
func TestAppLSPRequestErrorFixtureProcess(t *testing.T) {
	if os.Getenv("TEAK_APP_LSP_REQUEST_ERROR_FIXTURE") != "1" {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		body, err := readAppFixtureFrame(reader)
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
		if request.ID == nil {
			continue
		}
		switch request.Method {
		case "initialize":
			sendAppFixtureResult(request.ID, map[string]any{
				"capabilities": map[string]any{
					"positionEncoding": "utf-8",
					"textDocumentSync": 1,
				},
			})
		case "shutdown":
			sendAppFixtureResult(request.ID, nil)
		default:
			sendAppFixtureError(request.ID, fmt.Sprintf("fixture failure: %s", request.Method))
		}
	}
}

func readAppFixtureFrame(reader *bufio.Reader) ([]byte, error) {
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

func sendAppFixtureResult(id *int, result any) {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *id, "result": result})
	sendAppFixturePayload(payload)
}

func sendAppFixtureError(id *int, message string) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      *id,
		"error":   map[string]any{"code": -32603, "message": message},
	})
	sendAppFixturePayload(payload)
}

func sendAppFixturePayload(payload []byte) {
	_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(payload))
	_, _ = os.Stdout.Write(payload)
}

// requireLspErrorMsg runs a request command and asserts it produced an
// LspErrorMsg for the given method.
func requireLspErrorMsg(t *testing.T, name string, cmd tea.Cmd, method string) lsp.LspErrorMsg {
	t.Helper()
	if cmd == nil {
		t.Fatalf("%s returned no command", name)
	}
	msg := cmd()
	errMsg, ok := msg.(lsp.LspErrorMsg)
	if !ok {
		t.Fatalf("%s message = %#v, want lsp.LspErrorMsg", name, msg)
	}
	if errMsg.Method != method {
		t.Fatalf("%s error method = %q, want %q", name, errMsg.Method, method)
	}
	return errMsg
}

func TestRequestCompletionSurfacesLiveClientError(t *testing.T) {
	model, _, _ := newLspRequestErrorModel(t)

	_, cmd := model.requestCompletion()
	errMsg := requireLspErrorMsg(t, "requestCompletion", cmd, "textDocument/completion")
	if !strings.Contains(errMsg.Message, "fixture failure") {
		t.Fatalf("completion error message = %q, want fixture failure detail", errMsg.Message)
	}

	updatedAny, _ := model.Update(errMsg)
	updated := updatedAny.(Model)
	if !strings.Contains(updated.status, "LSP error [textDocument/completion]") {
		t.Fatalf("status = %q, want completion error surfaced in status bar", updated.status)
	}
}

func TestRequestHoverSurfacesLiveClientError(t *testing.T) {
	model, _, _ := newLspRequestErrorModel(t)

	_, cmd := model.requestHover()
	requireLspErrorMsg(t, "requestHover", cmd, "textDocument/hover")
}

func TestRequestSignatureHelpSurfacesLiveClientError(t *testing.T) {
	model, _, _ := newLspRequestErrorModel(t)

	_, cmd := model.requestSignatureHelp()
	requireLspErrorMsg(t, "requestSignatureHelp", cmd, "textDocument/signatureHelp")
}

func TestRequestFoldingRangesSurfacesLiveClientError(t *testing.T) {
	model, _, filePath := newLspRequestErrorModel(t)

	_, cmd := model.requestFoldingRanges(filePath)
	requireLspErrorMsg(t, "requestFoldingRanges", cmd, "textDocument/foldingRange")
}

func TestRequestCodeActionsSurfacesRequesterError(t *testing.T) {
	model, _, _ := newLspRequestErrorModel(t)
	model.codeActionRequester = func(_ context.Context, _ string, _, _ int, _ []lsp.Diagnostic) ([]lsp.CodeAction, error) {
		return nil, errors.New("code action backend unavailable")
	}

	_, cmd := model.requestCodeActions()
	errMsg := requireLspErrorMsg(t, "requestCodeActions", cmd, "textDocument/codeAction")
	if !strings.Contains(errMsg.Message, "code action backend unavailable") {
		t.Fatalf("code action error message = %q, want requester error", errMsg.Message)
	}
}

// Cancellation is routine supersession, not a user-visible failure; it must
// keep the request silent even after the error-surfacing fix.
func TestRequestCodeActionsStaysSilentOnCancellation(t *testing.T) {
	model, _, _ := newLspRequestErrorModel(t)
	model.codeActionRequester = func(ctx context.Context, _ string, _, _ int, _ []lsp.Diagnostic) ([]lsp.CodeAction, error) {
		return nil, ctx.Err()
	}

	_, cmd := model.requestCodeActions()
	if cmd == nil {
		t.Fatal("requestCodeActions returned no command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("canceled code action message = %#v, want nil", msg)
	}
}

// A file with no language server is legitimate degradation, not an error:
// the request must stay silent so "no server" is not reported as a failure.
func TestRequestsWithoutServerStaySilent(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	// .go has a default server config but no client was ever started, so
	// ClientForFile legitimately returns nil here.
	tests := []struct {
		name string
		cmd  func() tea.Cmd
	}{
		{"completion", func() tea.Cmd { _, cmd := model.requestCompletion(); return cmd }},
		{"hover", func() tea.Cmd { _, cmd := model.requestHover(); return cmd }},
		{"signature help", func() tea.Cmd { _, cmd := model.requestSignatureHelp(); return cmd }},
		{"folding", func() tea.Cmd { _, cmd := model.requestFoldingRanges(model.activeEditor().Buffer.FilePath); return cmd }},
		{"code actions", func() tea.Cmd { _, cmd := model.requestCodeActions(); return cmd }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.cmd()
			if cmd == nil {
				return
			}
			if msg := cmd(); msg != nil {
				t.Fatalf("no-server %s message = %#v, want nil", tt.name, msg)
			}
		})
	}
}

// lspRequestRoutineErr decides which request outcomes the status bar must not
// surface. Supersession cancellation and mid-request server exit are always
// routine (the restart is reported separately); the per-method timeout budget
// is routine only for requests that fire automatically while typing or on
// open, where a slow cold-start server would otherwise print a deadline error
// on every keystroke. Explicitly invoked requests keep the timeout visible as
// feedback.
func TestLspRequestRoutineErrPolicy(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		err  error
		auto bool
		want bool
	}{
		{"canceled is always routine", context.Canceled, false, true},
		{"canceled is routine for auto requests", context.Canceled, true, true},
		{"client exit is always routine", lsp.ErrClientNotRunning, false, true},
		{"client exit is routine for auto requests", lsp.ErrClientNotRunning, true, true},
		{"deadline is routine for auto requests", context.DeadlineExceeded, true, true},
		{"deadline surfaces for explicit requests", context.DeadlineExceeded, false, false},
		{"real error surfaces for explicit requests", boom, false, false},
		{"real error surfaces for auto requests", boom, true, false},
		{"nil is not routine", nil, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lspRequestRoutineErr(tt.err, tt.auto); got != tt.want {
				t.Fatalf("lspRequestRoutineErr(%v, auto=%v) = %v, want %v", tt.err, tt.auto, got, tt.want)
			}
		})
	}
}
