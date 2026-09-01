package app

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"teak/internal/lsp"
	"teak/internal/text"
	"teak/internal/toolpath"
)

func fallbackFormatter(path string) (name string, args []string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "gofmt", nil
	case ".js", ".jsx", ".ts", ".tsx", ".json", ".css", ".html", ".md":
		return "prettier", []string{"--stdin-filepath", path}
	default:
		return "", nil
	}
}

func fallbackFormatDocument(ctx context.Context, filePath string, snapshot *text.Rope) ([]lsp.TextEdit, bool, error) {
	name, args := fallbackFormatter(filePath)
	if name == "" || snapshot == nil {
		return nil, false, nil
	}
	cmd, err := toolpath.Command(ctx, name, args...)
	if err != nil {
		return nil, false, err
	}
	var stdin bytes.Buffer
	if _, err := snapshot.WriteTo(&stdin); err != nil {
		return nil, false, err
	}
	cmd.Stdin = &stdin
	stdout, stderr, err := toolpath.RunBounded(cmd, int(maxEditorFileBytes), 64<<10)
	if err != nil {
		if len(stderr) > 0 {
			return nil, false, fmt.Errorf("%s: %w (%s)", name, err, bytes.TrimSpace(stderr))
		}
		return nil, false, fmt.Errorf("%s: %w", name, err)
	}
	if bytes.Equal(stdout, stdin.Bytes()) {
		return nil, true, nil
	}
	lastLine := snapshot.LineCount() - 1
	if lastLine < 0 {
		lastLine = 0
	}
	return []lsp.TextEdit{{
		StartLine: 0,
		StartCol:  0,
		EndLine:   lastLine,
		EndCol:    snapshot.LineLen(lastLine),
		NewText:   string(stdout),
	}}, true, nil
}
