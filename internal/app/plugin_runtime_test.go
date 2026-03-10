package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestParseSyntheticKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKeys []string
		wantErr  bool
	}{
		{
			name:     "plain text expands to individual printable keys",
			input:    "ab",
			wantKeys: []string{"a", "b"},
		},
		{
			name:     "angle-bracket tokens mix with text",
			input:    "a<left>!",
			wantKeys: []string{"a", "left", "!"},
		},
		{
			name:     "modifier token parses without brackets",
			input:    "ctrl+s",
			wantKeys: []string{"ctrl+s"},
		},
		{
			name:     "named special token parses without brackets",
			input:    "enter",
			wantKeys: []string{"enter"},
		},
		{
			name:    "unterminated token errors",
			input:   "<ctrl+s",
			wantErr: true,
		},
		{
			name:    "unknown token errors",
			input:   "<madeup>",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, err := parseSyntheticKeys(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseSyntheticKeys() error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSyntheticKeys() error = %v", err)
			}
			if len(msgs) != len(tt.wantKeys) {
				t.Fatalf("parseSyntheticKeys() len = %d, want %d", len(msgs), len(tt.wantKeys))
			}
			for i, msg := range msgs {
				if got := tea.KeyPressMsg(msg).String(); got != tt.wantKeys[i] {
					t.Fatalf("parseSyntheticKeys()[%d] = %q, want %q", i, got, tt.wantKeys[i])
				}
			}
		})
	}
}
