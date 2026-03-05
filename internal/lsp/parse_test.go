package lsp

import (
	"fmt"
	"testing"
)

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       []byte
		wantContent string
		wantOK      bool
	}{
		{
			name:        "valid message",
			input:       []byte("Content-Length: 17\r\n\r\n{\"jsonrpc\":\"2.0\"}"),
			wantContent: `{"jsonrpc":"2.0"}`,
			wantOK:      true,
		},
		{
			name:        "valid message with spaces in header",
			input:       []byte("Content-Length:   17\r\n\r\n{\"jsonrpc\":\"2.0\"}"),
			wantContent: `{"jsonrpc":"2.0"}`,
			wantOK:      true,
		},
		{
			name:        "incomplete message",
			input:       []byte("Content-Length: 50\r\n\r\n{\"jsonrpc\""),
			wantContent: "",
			wantOK:      false,
		},
		{
			name:        "no header",
			input:       []byte("{\"jsonrpc\":\"2.0\"}"),
			wantContent: "",
			wantOK:      false,
		},
		{
			name:        "invalid content length",
			input:       []byte("Content-Length: abc\r\n\r\n{\"jsonrpc\":\"2.0\"}"),
			wantContent: "",
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, rest, ok := parseMessage(tt.input)
			if ok != tt.wantOK {
				t.Errorf("parseMessage() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if tt.wantOK {
				if string(content) != tt.wantContent {
					t.Errorf("parseMessage() content = %q, want %q", string(content), tt.wantContent)
				}
				if len(rest) != 0 {
					t.Errorf("parseMessage() rest = %q, want empty", string(rest))
				}
			}
		})
	}
}

func TestParseMessageWithRest(t *testing.T) {
	input := []byte("Content-Length: 17\r\n\r\n{\"jsonrpc\":\"2.0\"}Content-Length: 10\r\n\r\n{\"id\":1}")
	content, rest, ok := parseMessage(input)
	if !ok {
		t.Fatal("parseMessage() failed")
	}
	if string(content) != `{"jsonrpc":"2.0"}` {
		t.Errorf("content = %q, want %q", string(content), `{"jsonrpc":"2.0"}`)
	}
	// Should have remaining message
	if len(rest) == 0 {
		t.Error("expected remaining data in rest")
	}
}

func TestParseMessageMaxSize(t *testing.T) {
	// Create a message that exceeds maxMessageSize
	largeSize := maxMessageSize + 1
	input := []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", largeSize))
	// Don't actually allocate the large content, just test the header parsing
	content, rest, ok := parseMessage(input)
	if ok {
		t.Error("parseMessage() should reject large messages")
	}
	if content != nil {
		t.Error("parseMessage() should return nil content for large messages")
	}
	_ = rest
}

func TestParseMessageMultiple(t *testing.T) {
	// {"id":1} is 8 bytes
	input := []byte(
		"Content-Length: 17\r\n\r\n{\"jsonrpc\":\"2.0\"}" +
			"Content-Length: 8\r\n\r\n{\"id\":1}",
	)

	// Parse first message
	content1, rest1, ok1 := parseMessage(input)
	if !ok1 {
		t.Fatal("first parseMessage() failed")
	}
	if string(content1) != `{"jsonrpc":"2.0"}` {
		t.Errorf("first content = %q, want %q", string(content1), `{"jsonrpc":"2.0"}`)
	}

	// The rest should be the second message
	t.Logf("rest1 = %q", string(rest1))
	
	// Parse second message from rest
	content2, rest2, ok2 := parseMessage(rest1)
	if !ok2 {
		t.Logf("second parse failed, rest1 was: %q", string(rest1))
		t.Fatal("second parseMessage() failed")
	}
	if string(content2) != `{"id":1}` {
		t.Errorf("second content = %q, want %q", string(content2), `{"id":1}`)
	}

	if len(rest2) != 0 {
		t.Errorf("expected no remaining data, got %q", string(rest2))
	}
}
