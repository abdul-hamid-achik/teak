package app

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"teak/internal/lsp"
	"teak/internal/text"
)

// TestAppLSPCompletionFixtureProcess is not a test: it is a fixture language
// server that answers every completion request with one item. It negotiates
// UTF-8 positions, so the client still validates each request position
// against its synchronized document snapshot.
func TestAppLSPCompletionFixtureProcess(t *testing.T) {
	if os.Getenv("TEAK_APP_LSP_COMPLETION_FIXTURE") != "1" {
		return
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		body, err := readAppFixtureFrame(reader)
		if err != nil {
			return
		}
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
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
					"positionEncoding":   "utf-8",
					"textDocumentSync":   1,
					"completionProvider": map[string]any{"triggerCharacters": []string{"."}},
				},
			})
		case "textDocument/completion":
			sendAppFixtureResult(request.ID, []map[string]any{{"label": "Get", "insertText": "Get"}})
		default:
			sendAppFixtureResult(request.ID, nil)
		}
	}
}

// newCompletionFreshnessModel opens "srv.store" on a live fixture client at
// version v0, then types "." in the editor so the request is computed for
// v1 = v0+1 while the client still holds v0: exactly the state Bubble Tea
// produces when the completion command runs before the didChange command.
func newCompletionFreshnessModel(t *testing.T, fixtureEnv, extension string) (Model, *lsp.Manager, string) {
	t.Helper()
	model := newOverlayRequestTestModel(t)
	root := t.TempDir()
	filePath := filepath.Join(root, "main"+extension)
	ed := model.activeEditor()
	ed.Buffer.FilePath = filePath
	model.tabBar.Tabs[0].FilePath = filePath
	ed.Buffer.LoadContent([]byte("srv.store"))
	ed.Buffer.SetCursor(text.Position{Line: 0, Col: len("srv.store")})

	mgr := lsp.NewManager(root, []lsp.ServerConfig{{
		Extensions: []string{extension},
		Command:    os.Args[0],
		Args:       []string{"-test.run=^" + fixtureTestName(fixtureEnv) + "$", "--"},
		Env:        map[string]string{fixtureEnv: "1"},
		LanguageID: "fixture",
	}})
	t.Cleanup(mgr.ShutdownAll)
	generation := mgr.BeginDocument(filePath)
	v0 := ed.Buffer.Version()
	if err := mgr.OpenDocument(filePath, generation, "fixture", v0, "srv.store"); err != nil {
		t.Fatalf("OpenDocument() error = %v", err)
	}
	model.lspMgr = mgr

	ed.Buffer.InsertAtCursor([]byte("."))
	if ed.Buffer.Version() <= v0 || ed.Buffer.Cursor.Col != len("srv.store.") {
		t.Fatalf("typing did not advance the buffer: version %d -> %d, cursor %+v", v0, ed.Buffer.Version(), ed.Buffer.Cursor)
	}
	return model, mgr, filePath
}

func fixtureTestName(env string) string {
	if env == "TEAK_APP_LSP_REQUEST_ERROR_FIXTURE" {
		return "TestAppLSPRequestErrorFixtureProcess"
	}
	return "TestAppLSPCompletionFixtureProcess"
}

// runCommandAsync runs cmd off the test goroutine so the test can change the
// client's document while the command is waiting.
func runCommandAsync(cmd tea.Cmd) <-chan tea.Msg {
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	return result
}

// A completion request is computed from the editor's version and cursor, but
// the client validates the position against the snapshot it last synchronized.
// On fast typing the two are a keystroke apart and the user saw
// "LSP error [textDocument/completion]: completion position: column ...".
// A request or error that belongs to a superseded version must be dropped
// silently; one for the current version must still reach the user.
func TestRequestCompletionFreshnessAgainstClientDocumentVersion(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		// during runs while the completion command is in flight.
		during func(t *testing.T, model Model, mgr *lsp.Manager, filePath string)
		// before runs before the completion command starts.
		before func(t *testing.T, model Model, mgr *lsp.Manager, filePath string)
		// syncWait overrides completionSyncWait when non-zero.
		syncWait time.Duration
		want     string // "result", "nil" or "error"
		// wantError is a substring of the expected error message.
		wantError string
	}{
		{
			name:    "client catches up with the typed version",
			fixture: "TEAK_APP_LSP_COMPLETION_FIXTURE",
			during: func(t *testing.T, model Model, mgr *lsp.Manager, filePath string) {
				mgr.ChangeDocument(filePath, mgr.DocumentGeneration(filePath), model.activeEditor().Buffer.Version(), "srv.store.")
			},
			want: "result",
		},
		{
			name:    "newer edit supersedes the request while it waits",
			fixture: "TEAK_APP_LSP_COMPLETION_FIXTURE",
			during: func(t *testing.T, model Model, mgr *lsp.Manager, filePath string) {
				// syncEditorStateAfterUpdate cancels in-flight overlay
				// requests on every edit before queuing its didChange.
				model.overlayRequests.invalidateAll()
				mgr.ChangeDocument(filePath, mgr.DocumentGeneration(filePath), model.activeEditor().Buffer.Version()+1, "srv.stor")
			},
			want: "nil",
		},
		{
			name:    "client already holds a newer version",
			fixture: "TEAK_APP_LSP_COMPLETION_FIXTURE",
			before: func(t *testing.T, model Model, mgr *lsp.Manager, filePath string) {
				mgr.ChangeDocument(filePath, mgr.DocumentGeneration(filePath), model.activeEditor().Buffer.Version()+1, "srv.stor")
			},
			want: "nil",
		},
		{
			name:    "server error for the current version still surfaces",
			fixture: "TEAK_APP_LSP_REQUEST_ERROR_FIXTURE",
			before: func(t *testing.T, model Model, mgr *lsp.Manager, filePath string) {
				mgr.ChangeDocument(filePath, mgr.DocumentGeneration(filePath), model.activeEditor().Buffer.Version(), "srv.store.")
			},
			want:      "error",
			wantError: "fixture failure",
		},
		{
			// A version that never reaches the client is a real sync failure:
			// the bounded wait expires and the error is shown rather than
			// completion going quietly dead.
			name:      "version that never synchronizes still surfaces after the wait",
			fixture:   "TEAK_APP_LSP_COMPLETION_FIXTURE",
			syncWait:  20 * time.Millisecond,
			want:      "error",
			wantError: "completion position",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extension := ".lspok"
			if tt.fixture == "TEAK_APP_LSP_REQUEST_ERROR_FIXTURE" {
				extension = ".lsperr"
			}
			if tt.syncWait > 0 {
				previous := completionSyncWait
				completionSyncWait = tt.syncWait
				t.Cleanup(func() { completionSyncWait = previous })
			}
			model, mgr, filePath := newCompletionFreshnessModel(t, tt.fixture, extension)
			if tt.before != nil {
				tt.before(t, model, mgr, filePath)
			}
			updated, cmd := model.requestCompletion()
			model = updated.(Model)
			if cmd == nil {
				t.Fatal("requestCompletion returned no command")
			}
			result := runCommandAsync(cmd)
			if tt.during != nil {
				select {
				case msg := <-result:
					t.Fatalf("completion finished before the client synchronized its version: %#v", msg)
				case <-time.After(50 * time.Millisecond):
				}
				tt.during(t, model, mgr, filePath)
			}
			var msg tea.Msg
			select {
			case msg = <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("completion command did not finish")
			}

			switch tt.want {
			case "result":
				completion, ok := msg.(lsp.CompletionResultMsg)
				if !ok || len(completion.Items) == 0 {
					t.Fatalf("message = %#v, want completion items", msg)
				}
				updated, _ := model.Update(completion)
				if status := updated.(Model).status; strings.Contains(status, "LSP error") {
					t.Fatalf("status = %q, want no LSP error", status)
				}
			case "nil":
				if msg != nil {
					t.Fatalf("message = %#v, want the superseded request dropped silently", msg)
				}
			case "error":
				errMsg, ok := msg.(lsp.LspErrorMsg)
				if !ok || !strings.Contains(errMsg.Message, tt.wantError) {
					t.Fatalf("message = %#v, want a completion error containing %q", msg, tt.wantError)
				}
			}
		})
	}
}
