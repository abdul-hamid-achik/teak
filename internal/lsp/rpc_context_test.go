package lsp

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestRPCContextMethodsPropagateCallerCancellation(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client, context.Context) error
	}{
		{"completion", func(client *Client, ctx context.Context) error {
			_, err := client.CompletionContext(ctx, "file:///workspace/main.go", 0, 0)
			return err
		}},
		{"hover", func(client *Client, ctx context.Context) error {
			_, err := client.HoverContext(ctx, "file:///workspace/main.go", 0, 0)
			return err
		}},
		{"definition", func(client *Client, ctx context.Context) error {
			_, err := client.DefinitionContext(ctx, "file:///workspace/main.go", 0, 0)
			return err
		}},
		{"references", func(client *Client, ctx context.Context) error {
			_, err := client.ReferencesContext(ctx, "file:///workspace/main.go", 0, 0)
			return err
		}},
		{"rename", func(client *Client, ctx context.Context) error {
			_, err := client.RenameContext(ctx, "file:///workspace/main.go", 0, 0, "renamed")
			return err
		}},
		{"signature help", func(client *Client, ctx context.Context) error {
			_, err := client.SignatureHelpContext(ctx, "file:///workspace/main.go", 0, 0)
			return err
		}},
		{"formatting", func(client *Client, ctx context.Context) error {
			_, err := client.FormattingContext(ctx, "file:///workspace/main.go", FormattingOptions{})
			return err
		}},
		{"folding range", func(client *Client, ctx context.Context) error {
			_, err := client.FoldingRangeContext(ctx, "file:///workspace/main.go")
			return err
		}},
		{"code action", func(client *Client, ctx context.Context) error {
			_, err := client.CodeActionContext(ctx, "file:///workspace/main.go", 0, 0, 0, 0, nil)
			return err
		}},
		{"document symbol", func(client *Client, ctx context.Context) error {
			_, err := client.DocumentSymbolContext(ctx, "file:///workspace/main.go")
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdin := &captureWriteCloser{}
			client := &Client{
				stdin:   stdin,
				pending: make(map[int]chan callResult),
				running: true,
				capabilities: ServerCapabilities{
					FormattingProvider: true,
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			err := tt.call(client, ctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if !bytes.Contains(stdin.Bytes(), []byte(`"method":"$/cancelRequest"`)) {
				t.Fatalf("cancel notification was not sent: %q", stdin.Bytes())
			}
			if len(client.pending) != 0 {
				t.Fatalf("pending requests = %d, want 0", len(client.pending))
			}
		})
	}
}
