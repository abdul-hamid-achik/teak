package lsp

import "testing"

func TestClientSupportsFormatting(t *testing.T) {
	client := &Client{
		capabilities: ServerCapabilities{
			FormattingProvider: map[string]any{"workDoneProgress": true},
		},
	}

	if !client.SupportsFormatting() {
		t.Fatal("expected formatting support to be detected")
	}

	client.capabilities.FormattingProvider = false
	if client.SupportsFormatting() {
		t.Fatal("expected disabled formatting support to be reported")
	}
}

func TestFormattingRequestParamsUsesOptions(t *testing.T) {
	params := formattingRequestParams("file:///tmp/test.go", FormattingOptions{
		TabSize:      8,
		InsertSpaces: false,
	})

	textDocument, ok := params["textDocument"].(map[string]any)
	if !ok {
		t.Fatalf("textDocument = %T, want map[string]any", params["textDocument"])
	}
	if got := textDocument["uri"]; got != "file:///tmp/test.go" {
		t.Fatalf("uri = %v", got)
	}

	options, ok := params["options"].(map[string]any)
	if !ok {
		t.Fatalf("options = %T, want map[string]any", params["options"])
	}
	if got := options["tabSize"]; got != 8 {
		t.Fatalf("tabSize = %v, want 8", got)
	}
	if got := options["insertSpaces"]; got != false {
		t.Fatalf("insertSpaces = %v, want false", got)
	}
}
