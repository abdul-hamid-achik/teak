package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/text"
)

func newOverlayRequestTestModel(t *testing.T) Model {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	model, err := NewModel("", "", cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(model.cleanup)

	const filePath = "/workspace/main.go"
	model.editors[0].Buffer.FilePath = filePath
	model.tabBar.Tabs[0].FilePath = filePath
	return model
}

func overlayVisible(model Model, kind overlayRequestKind) bool {
	editor := model.activeEditor()
	if editor == nil {
		return false
	}
	switch kind {
	case overlayRequestCompletion:
		return editor.IsAutocompleteVisible()
	case overlayRequestHover:
		return editor.HoverView() != ""
	case overlayRequestSignature:
		return editor.SignatureHelpView() != ""
	default:
		return false
	}
}

func overlayResultMessage(kind overlayRequestKind, metadata lsp.OverlayRequestMetadata) tea.Msg {
	switch kind {
	case overlayRequestCompletion:
		return lsp.CompletionResultMsg{
			OverlayRequestMetadata: metadata,
			Items:                  []lsp.CompletionItem{{Label: "Println", InsertText: "Println"}},
		}
	case overlayRequestHover:
		return lsp.HoverResultMsg{
			OverlayRequestMetadata: metadata,
			Content:                "Println formats output.",
		}
	case overlayRequestSignature:
		return lsp.SignatureHelpResultMsg{
			OverlayRequestMetadata: metadata,
			Help: &lsp.SignatureHelp{
				Signatures: []lsp.SignatureInformation{{Label: "Println(a ...any)"}},
			},
		}
	default:
		panic("unsupported overlay request kind")
	}
}

func TestOverlayResultRejectsStaleRequestIdentity(t *testing.T) {
	kinds := []overlayRequestKind{
		overlayRequestCompletion,
		overlayRequestHover,
		overlayRequestSignature,
	}
	scenarios := []struct {
		name   string
		setup  func(*Model)
		mutate func(*Model, overlayRequestKind, *lsp.OverlayRequestMetadata)
		want   bool
	}{
		{
			name: "matching request",
			want: true,
		},
		{
			name: "different active document",
			mutate: func(_ *Model, _ overlayRequestKind, metadata *lsp.OverlayRequestMetadata) {
				metadata.FilePath = "/workspace/other.go"
			},
		},
		{
			name: "edited buffer",
			mutate: func(model *Model, _ overlayRequestKind, _ *lsp.OverlayRequestMetadata) {
				model.activeEditor().Buffer.InsertAtCursor([]byte("x"))
			},
		},
		{
			name: "moved cursor",
			setup: func(model *Model) {
				model.activeEditor().Buffer.LoadContent([]byte("alpha beta"))
				model.activeEditor().Buffer.SetCursor(text.Position{Line: 0, Col: 1})
			},
			mutate: func(model *Model, _ overlayRequestKind, _ *lsp.OverlayRequestMetadata) {
				model.activeEditor().Buffer.SetCursor(text.Position{Line: 0, Col: 7})
			},
		},
		{
			name: "superseded generation",
			mutate: func(model *Model, kind overlayRequestKind, _ *lsp.OverlayRequestMetadata) {
				model.beginOverlayRequest(kind)
			},
		},
	}

	for _, kind := range kinds {
		for _, scenario := range scenarios {
			t.Run(kind.String()+"/"+scenario.name, func(t *testing.T) {
				model := newOverlayRequestTestModel(t)
				if scenario.setup != nil {
					scenario.setup(&model)
				}
				metadata, ok := model.beginOverlayRequest(kind)
				if !ok {
					t.Fatal("beginOverlayRequest() ok = false")
				}
				if scenario.mutate != nil {
					scenario.mutate(&model, kind, &metadata)
				}

				updatedAny, _ := model.Update(overlayResultMessage(kind, metadata))
				updated := updatedAny.(Model)
				if got := overlayVisible(updated, kind); got != scenario.want {
					t.Fatalf("overlay visible = %t, want %t; metadata = %#v", got, scenario.want, metadata)
				}
			})
		}
	}
}

func TestBeginOverlayRequestCapturesCurrentEditorAndIncrementsGeneration(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	first, ok := model.beginOverlayRequest(overlayRequestCompletion)
	if !ok {
		t.Fatal("first beginOverlayRequest() ok = false")
	}
	second, ok := model.beginOverlayRequest(overlayRequestCompletion)
	if !ok {
		t.Fatal("second beginOverlayRequest() ok = false")
	}

	if first.FilePath != model.activeEditor().Buffer.FilePath {
		t.Fatalf("FilePath = %q, want %q", first.FilePath, model.activeEditor().Buffer.FilePath)
	}
	if first.Version != model.activeEditor().Buffer.Version() {
		t.Fatalf("Version = %d, want %d", first.Version, model.activeEditor().Buffer.Version())
	}
	if first.CursorLine != model.activeEditor().Buffer.Cursor.Line || first.CursorCol != model.activeEditor().Buffer.Cursor.Col {
		t.Fatalf("cursor = %d:%d, want %+v", first.CursorLine, first.CursorCol, model.activeEditor().Buffer.Cursor)
	}
	if second.Generation != first.Generation+1 {
		t.Fatalf("generations = %d then %d, want consecutive", first.Generation, second.Generation)
	}
}

func TestOverlayResultAllowsLegacyZeroMetadata(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	updatedAny, _ := model.Update(lsp.HoverResultMsg{Content: "legacy hover"})
	updated := updatedAny.(Model)
	if !overlayVisible(updated, overlayRequestHover) {
		t.Fatal("legacy zero-metadata hover result was rejected")
	}
}

func TestRequestSignatureHelpCapturesCurrentOverlayMetadata(t *testing.T) {
	model := newOverlayRequestTestModel(t)
	model.activeEditor().Buffer.InsertAtCursor([]byte("call("))

	version := model.activeEditor().Buffer.Version()
	updated, cmd := model.requestSignatureHelp()
	if cmd == nil {
		t.Fatal("requestSignatureHelp() command = nil")
	}

	metadata := lsp.OverlayRequestMetadata{
		FilePath:   model.activeEditor().Buffer.FilePath,
		Version:    version,
		CursorLine: model.activeEditor().Buffer.Cursor.Line,
		CursorCol:  model.activeEditor().Buffer.Cursor.Col,
		Generation: updated.overlayRequests.current(overlayRequestSignature),
	}
	if !updated.acceptsOverlayResult(overlayRequestSignature, metadata) {
		t.Fatalf("signature metadata was not accepted: %#v", metadata)
	}
}

func TestForwardToEditorTriggersSignatureHelpForCallDelimiters(t *testing.T) {
	tests := []struct {
		name        string
		key         tea.KeyPressMsg
		wantRequest bool
	}{
		{
			name:        "opening parenthesis",
			key:         tea.KeyPressMsg{Code: '(', Text: "("},
			wantRequest: true,
		},
		{
			name:        "parameter separator",
			key:         tea.KeyPressMsg{Code: ',', Text: ","},
			wantRequest: true,
		},
		{
			name: "ordinary identifier character",
			key:  tea.KeyPressMsg{Code: 'x', Text: "x"},
		},
		{
			name: "completion trigger character",
			key:  tea.KeyPressMsg{Code: '.', Text: "."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newOverlayRequestTestModel(t)
			updatedAny, _ := model.forwardToEditor(tt.key)
			updated := updatedAny.(Model)

			gotRequest := updated.overlayRequests.current(overlayRequestSignature) > 0
			if gotRequest != tt.wantRequest {
				t.Fatalf("signature request = %t, want %t", gotRequest, tt.wantRequest)
			}
		})
	}
}
