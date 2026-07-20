package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxExecuteCommandNameBytes = 1024
	maxExecuteCommandArguments = 256
)

// ExecuteCommand invokes a server-defined command selected explicitly by the
// user. The caller owns ctx so a superseded picker selection can cancel the
// JSON-RPC request instead of leaving work queued at the language server.
func (c *Client) ExecuteCommand(ctx context.Context, command string, arguments []any) (json.RawMessage, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("workspace command is empty")
	}
	if len(command) > maxExecuteCommandNameBytes {
		return nil, fmt.Errorf("workspace command exceeds %d bytes", maxExecuteCommandNameBytes)
	}
	if len(arguments) > maxExecuteCommandArguments {
		return nil, fmt.Errorf("workspace command has more than %d arguments", maxExecuteCommandArguments)
	}
	return c.call(ctx, "workspace/executeCommand", map[string]any{
		"command":   command,
		"arguments": arguments,
	})
}
