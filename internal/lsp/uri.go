package lsp

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// FileURI converts a file path to a file:// URI.
func FileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if runtime.GOOS == "windows" {
		abs = "/" + strings.ReplaceAll(abs, "\\", "/")
	}
	return "file://" + abs
}

// URIToPath converts a file:// URI back to a file path.
func URIToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		// Try stripping file:// prefix directly
		return strings.TrimPrefix(uri, "file://")
	}
	path := u.Path
	if runtime.GOOS == "windows" && len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}
