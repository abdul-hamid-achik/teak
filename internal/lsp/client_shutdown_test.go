package lsp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
)

func TestIsExpectedShutdownError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "client not running",
			err:  errClientNotRunning,
			want: true,
		},
		{
			name: "wrapped client not running",
			err:  fmt.Errorf("wrap: %w", errClientNotRunning),
			want: true,
		},
		{
			name: "io eof",
			err:  io.EOF,
			want: true,
		},
		{
			name: "os closed",
			err:  os.ErrClosed,
			want: true,
		},
		{
			name: "broken pipe string",
			err:  errors.New("write: broken pipe"),
			want: true,
		},
		{
			name: "closed pipe string",
			err:  errors.New("io: read/write on closed pipe"),
			want: true,
		},
		{
			name: "unexpected error",
			err:  errors.New("timeout while waiting for response"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExpectedShutdownError(tt.err)
			if got != tt.want {
				t.Fatalf("isExpectedShutdownError() = %v, want %v", got, tt.want)
			}
		})
	}
}
