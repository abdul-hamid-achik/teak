package lsp

import (
	"reflect"
	"strings"
	"testing"
)

func TestClientCapabilityQueriesRespectServerCapabilities(t *testing.T) {
	completion := &struct {
		ResolveProvider   bool     `json:"resolveProvider,omitempty"`
		TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	}{TriggerCharacters: []string{".", ":"}}
	client := &Client{capabilities: ServerCapabilities{
		HoverProvider:      true,
		CompletionProvider: completion,
		DefinitionProvider: map[string]any{"workDoneProgress": true},
		ReferencesProvider: true,
		RenameProvider:     true,
		FormattingProvider: true,
		TextDocumentSync:   SyncIncremental,
		SignatureHelpProvider: &struct {
			TriggerCharacters []string `json:"triggerCharacters,omitempty"`
		}{TriggerCharacters: []string{"("}},
	}, syncKind: SyncIncremental}

	if !client.SupportsHover() || !client.SupportsCompletion() || !client.SupportsDefinition() ||
		!client.SupportsReferences() || !client.SupportsRename() || !client.SupportsFormatting() {
		t.Fatal("enabled capabilities were not exposed by the client")
	}
	if got := client.GetCompletionTriggerCharacters(); !reflect.DeepEqual(got, []string{".", ":"}) {
		t.Fatalf("completion triggers = %#v", got)
	}
	if got := client.GetSyncKind(); got != SyncIncremental {
		t.Fatalf("sync kind = %v, want incremental", got)
	}

	client.capsChecker = client.newCapabilitiesChecker(client.capabilities, SyncIncremental)
	if !client.SupportsHover() || !client.SupportsCompletion() || !client.SupportsDefinition() ||
		!client.SupportsReferences() || !client.SupportsRename() || !client.SupportsFormatting() {
		t.Fatal("delegated capability checker lost an enabled capability")
	}
	if got := client.capsChecker.GetSignatureHelpTriggerCharacters(); !reflect.DeepEqual(got, []string{"("}) {
		t.Fatalf("signature help triggers = %#v", got)
	}

	disabled := &Client{capabilities: ServerCapabilities{
		HoverProvider:      false,
		DefinitionProvider: false,
		ReferencesProvider: false,
		RenameProvider:     false,
		FormattingProvider: false,
	}}
	if disabled.SupportsHover() || disabled.SupportsCompletion() || disabled.SupportsDefinition() ||
		disabled.SupportsReferences() || disabled.SupportsRename() || disabled.SupportsFormatting() {
		t.Fatal("disabled capabilities were reported as enabled")
	}
	if got := disabled.GetCompletionTriggerCharacters(); got != nil {
		t.Fatalf("disabled completion triggers = %#v, want nil", got)
	}
}

func TestDocumentStateTracksFullSyncVersions(t *testing.T) {
	state := NewDocumentState("file:///workspace/main.go", "go")
	if state.URI != "file:///workspace/main.go" || state.LanguageID != "go" || state.SyncKind != SyncFull || state.Version != 0 {
		t.Fatalf("new document state = %#v", state)
	}
	if got := state.IncrementVersion(); got != 1 {
		t.Fatalf("first version = %d, want 1", got)
	}
	if got := state.IncrementVersion(); got != 2 || state.Version != 2 {
		t.Fatalf("second version/state = %d/%d, want 2/2", got, state.Version)
	}
}

func TestPositionFromProtocolValidatesEncodingAndUnicodeBoundary(t *testing.T) {
	got, err := PositionFromProtocol("a😀b", " UTF-16 ", Position{Line: 0, Character: 3})
	if err != nil {
		t.Fatalf("PositionFromProtocol() error = %v", err)
	}
	if want := (Position{Line: 0, Character: 5}); got != want {
		t.Fatalf("PositionFromProtocol() = %#v, want %#v", got, want)
	}
	if _, err := PositionFromProtocol("a😀b", "utf-16", Position{Line: 0, Character: 2}); err == nil {
		t.Fatal("surrogate split was accepted")
	}
	if _, err := PositionFromProtocol("ok", "made-up", Position{}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unsupported encoding error = %v", err)
	}
}

func TestClientConvertsStructuredProtocolRangesRecursively(t *testing.T) {
	const uri = "file:///workspace/main.go"
	client := &Client{openDocs: map[string]int{uri: 1}, positionEncoding: positionEncodingUTF16}
	client.setDocumentSnapshot(uri, 1, "a😀b\nβ")

	folded, err := client.foldingRangesFromProtocol(uri, []FoldingRange{{
		StartLine: 0, StartCharacter: 1, EndLine: 0, EndCharacter: 3, Kind: "region",
	}})
	if err != nil {
		t.Fatalf("foldingRangesFromProtocol() error = %v", err)
	}
	if got, want := folded[0], (FoldingRange{StartLine: 0, StartCharacter: 1, EndLine: 0, EndCharacter: 5, Kind: "region"}); got != want {
		t.Fatalf("folding range = %#v, want %#v", got, want)
	}

	symbols, err := client.documentSymbolsFromProtocol(uri, []DocumentSymbol{{
		Name:           "parent",
		Range:          Range{Start: Position{Line: 0, Character: 1}, End: Position{Line: 0, Character: 3}},
		SelectionRange: Range{Start: Position{Line: 0, Character: 1}, End: Position{Line: 0, Character: 3}},
		Children: []DocumentSymbol{{
			Name:           "child",
			Range:          Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 1}},
			SelectionRange: Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 1}},
		}},
	}})
	if err != nil {
		t.Fatalf("documentSymbolsFromProtocol() error = %v", err)
	}
	if got := symbols[0].Range.End.Character; got != 5 {
		t.Fatalf("parent symbol end = %d, want byte offset 5", got)
	}
	if got := symbols[0].Children[0].Range.End.Character; got != 2 {
		t.Fatalf("child symbol end = %d, want byte offset 2", got)
	}
}

func TestClientConvertsWorkspaceAndCodeActionEditsOrRejectsUnknownSnapshots(t *testing.T) {
	const uri = "file:///workspace/main.go"
	client := &Client{openDocs: map[string]int{uri: 3}, positionEncoding: positionEncodingUTF16}
	client.setDocumentSnapshot(uri, 3, "a😀b")

	version := 3
	converted, err := client.workspaceEditFromProtocol(WorkspaceEdit{
		Changes: map[string][]TextEdit{uri: {{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 3, NewText: "X"}}},
		DocumentChanges: []WorkspaceDocumentChange{
			{URI: uri, Version: &version, Edits: []TextEdit{{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 3, NewText: "Y"}}},
			{FileOperation: &WorkspaceFileOperation{Kind: FileOpRename, OldURI: uri, NewURI: "file:///workspace/next.go"}},
		},
	})
	if err != nil {
		t.Fatalf("workspaceEditFromProtocol() error = %v", err)
	}
	if got := converted.Changes[uri][0]; got.StartCol != 1 || got.EndCol != 5 {
		t.Fatalf("changes edit = %#v, want byte range 1:5", got)
	}
	if got := converted.DocumentChanges[0].Edits[0]; got.StartCol != 1 || got.EndCol != 5 {
		t.Fatalf("document change edit = %#v, want byte range 1:5", got)
	}
	if got := converted.DocumentChanges[1].FileOperation; got == nil || got.Kind != FileOpRename || got.NewURI == "" {
		t.Fatalf("file operation = %#v", got)
	}

	actions, err := client.codeActionsFromProtocol(uri, []CodeAction{{
		Title:       "fix emoji",
		Diagnostics: []Diagnostic{{Range: DiagRange{Start: DiagPosition{Line: 0, Character: 1}, End: DiagPosition{Line: 0, Character: 3}}}},
		Edit:        &WorkspaceEdit{Changes: map[string][]TextEdit{uri: {{StartLine: 0, StartCol: 1, EndLine: 0, EndCol: 3, NewText: "Z"}}}},
	}})
	if err != nil {
		t.Fatalf("codeActionsFromProtocol() error = %v", err)
	}
	if got := actions[0].Diagnostics[0].Range.End.Character; got != 5 {
		t.Fatalf("code action diagnostic end = %d, want 5", got)
	}
	if got := actions[0].Edit.Changes[uri][0].EndCol; got != 5 {
		t.Fatalf("code action edit end = %d, want 5", got)
	}

	unknown := WorkspaceEdit{Changes: map[string][]TextEdit{"file:///elsewhere.go": {{StartLine: 0, EndLine: 0}}}}
	if _, err := client.workspaceEditFromProtocol(unknown); err == nil || !strings.Contains(err.Error(), "no open document snapshot") {
		t.Fatalf("unknown document edit error = %v", err)
	}
}
