package capabilities

import (
	"testing"
)

func TestSyncKindValues(t *testing.T) {
	tests := []struct {
		kind SyncKind
		want int
	}{
		{SyncNone, 0},
		{SyncFull, 1},
		{SyncIncremental, 2},
	}

	for _, tt := range tests {
		name := func() string {
			switch tt.kind {
			case SyncNone:
				return "SyncNone"
			case SyncFull:
				return "SyncFull"
			case SyncIncremental:
				return "SyncIncremental"
			default:
				return "Unknown"
			}
		}()

		t.Run(name, func(t *testing.T) {
			if int(tt.kind) != tt.want {
				t.Errorf("SyncKind = %d, want %d", tt.kind, tt.want)
			}
		})
	}
}

func TestCapabilityEnabled(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected bool
	}{
		{"nil", nil, false},
		{"true bool", true, true},
		{"false bool", false, false},
		{"empty map", map[string]any{}, false},
		{"map with entries", map[string]any{"a": 1}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capabilityEnabled(tt.value)
			if got != tt.expected {
				t.Errorf("capabilityEnabled(%v) = %v, want %v", tt.value, got, tt.expected)
			}
		})
	}
}

func TestNewChecker(t *testing.T) {
	caps := ServerCapabilities{
		HoverProvider: true,
	}
	checker := NewChecker(caps, SyncIncremental)

	if checker == nil {
		t.Fatal("expected non-nil checker")
	}
	if checker.syncKind != SyncIncremental {
		t.Errorf("syncKind = %v, want SyncIncremental", checker.syncKind)
	}
}

func TestSupportsHover(t *testing.T) {
	tests := []struct {
		name   string
		caps   ServerCapabilities
		expect bool
	}{
		{"nil hover", ServerCapabilities{HoverProvider: nil}, false},
		{"true hover", ServerCapabilities{HoverProvider: true}, true},
		{"false hover", ServerCapabilities{HoverProvider: false}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChecker(tt.caps, SyncFull)
			if got := c.SupportsHover(); got != tt.expect {
				t.Errorf("SupportsHover() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestSupportsCompletion(t *testing.T) {
	c := NewChecker(ServerCapabilities{CompletionProvider: nil}, SyncFull)
	if c.SupportsCompletion() {
		t.Error("expected false for nil CompletionProvider")
	}

	c = NewChecker(ServerCapabilities{CompletionProvider: &CompletionOptions{}}, SyncFull)
	if !c.SupportsCompletion() {
		t.Error("expected true for non-nil CompletionProvider")
	}
}

func TestSupportsDefinition(t *testing.T) {
	tests := []struct {
		name   string
		caps   ServerCapabilities
		expect bool
	}{
		{"nil definition", ServerCapabilities{DefinitionProvider: nil}, false},
		{"true definition", ServerCapabilities{DefinitionProvider: true}, true},
		{"false definition", ServerCapabilities{DefinitionProvider: false}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChecker(tt.caps, SyncFull)
			if got := c.SupportsDefinition(); got != tt.expect {
				t.Errorf("SupportsDefinition() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestSupportsReferences(t *testing.T) {
	c := NewChecker(ServerCapabilities{ReferencesProvider: true}, SyncFull)
	if !c.SupportsReferences() {
		t.Error("expected true")
	}
}

func TestSupportsRename(t *testing.T) {
	c := NewChecker(ServerCapabilities{RenameProvider: true}, SyncFull)
	if !c.SupportsRename() {
		t.Error("expected true")
	}
}

func TestSupportsFormatting(t *testing.T) {
	c := NewChecker(ServerCapabilities{FormattingProvider: true}, SyncFull)
	if !c.SupportsFormatting() {
		t.Error("expected true")
	}
}

func TestSupportsRangeFormatting(t *testing.T) {
	c := NewChecker(ServerCapabilities{RangeFormattingProvider: true}, SyncFull)
	if !c.SupportsRangeFormatting() {
		t.Error("expected true")
	}
}

func TestSupportsFoldingRange(t *testing.T) {
	c := NewChecker(ServerCapabilities{FoldingRangeProvider: true}, SyncFull)
	if !c.SupportsFoldingRange() {
		t.Error("expected true")
	}
}

func TestSupportsCodeAction(t *testing.T) {
	c := NewChecker(ServerCapabilities{CodeActionProvider: true}, SyncFull)
	if !c.SupportsCodeAction() {
		t.Error("expected true")
	}
}

func TestSupportsDocumentSymbol(t *testing.T) {
	c := NewChecker(ServerCapabilities{DocumentSymbolProvider: true}, SyncFull)
	if !c.SupportsDocumentSymbol() {
		t.Error("expected true")
	}
}

func TestSupportsSignatureHelp(t *testing.T) {
	c := NewChecker(ServerCapabilities{SignatureHelpProvider: nil}, SyncFull)
	if c.SupportsSignatureHelp() {
		t.Error("expected false for nil SignatureHelpProvider")
	}

	c = NewChecker(ServerCapabilities{SignatureHelpProvider: &SignatureHelpOptions{}}, SyncFull)
	if !c.SupportsSignatureHelp() {
		t.Error("expected true for non-nil SignatureHelpProvider")
	}
}

func TestGetCompletionTriggerCharacters(t *testing.T) {
	c := NewChecker(ServerCapabilities{
		CompletionProvider: &CompletionOptions{
			TriggerCharacters: []string{"a", "b"},
		},
	}, SyncFull)

	chars := c.GetCompletionTriggerCharacters()
	if len(chars) != 2 {
		t.Errorf("expected 2 characters, got %d", len(chars))
	}

	c = NewChecker(ServerCapabilities{CompletionProvider: nil}, SyncFull)
	chars = c.GetCompletionTriggerCharacters()
	if chars != nil {
		t.Error("expected nil for nil CompletionProvider")
	}
}

func TestGetSignatureHelpTriggerCharacters(t *testing.T) {
	c := NewChecker(ServerCapabilities{
		SignatureHelpProvider: &SignatureHelpOptions{
			TriggerCharacters: []string{"(", ")"},
		},
	}, SyncFull)

	chars := c.GetSignatureHelpTriggerCharacters()
	if len(chars) != 2 {
		t.Errorf("expected 2 characters, got %d", len(chars))
	}

	c = NewChecker(ServerCapabilities{SignatureHelpProvider: nil}, SyncFull)
	chars = c.GetSignatureHelpTriggerCharacters()
	if chars != nil {
		t.Error("expected nil for nil SignatureHelpProvider")
	}
}

func TestGetSyncKind(t *testing.T) {
	c := NewChecker(ServerCapabilities{}, SyncFull)
	if c.GetSyncKind() != SyncFull {
		t.Errorf("expected SyncFull, got %v", c.GetSyncKind())
	}

	c = NewChecker(ServerCapabilities{}, SyncIncremental)
	if c.GetSyncKind() != SyncIncremental {
		t.Errorf("expected SyncIncremental, got %v", c.GetSyncKind())
	}
}
