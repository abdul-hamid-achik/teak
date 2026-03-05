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
		// Handle Windows drive letters properly
		if len(abs) >= 2 && abs[1] == ':' {
			// Convert C:\path to file:///c:/path
			abs = "/" + strings.ToLower(string(abs[0])) + abs[1:]
		}
		abs = strings.ReplaceAll(abs, "\\", "/")
	}
	// Encode special characters but NOT path separators
	// We need to encode each path segment separately
	parts := strings.Split(abs, "/")
	for i, part := range parts {
		if part != "" {
			parts[i] = url.PathEscape(part)
		}
	}
	encoded := strings.Join(parts, "/")
	return "file://" + encoded
}

// URIToPath converts a file:// URI back to a file path.
func URIToPath(uri string) string {
	// Only handle file:// URIs
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	
	uri = strings.TrimPrefix(uri, "file://")
	
	// Decode percent-encoded characters
	decoded, err := url.PathUnescape(uri)
	if err != nil {
		// If decoding fails, use the URI as-is (minus file:// prefix)
		decoded = uri
	}
	
	path := decoded
	if runtime.GOOS == "windows" && len(path) > 0 && path[0] == '/' {
		// Handle file:///c:/path format
		path = path[1:]
		// Convert /c:/path back to C:\path
		if len(path) >= 3 && path[1] == ':' {
			path = strings.ToUpper(string(path[0])) + path[1:]
			path = strings.ReplaceAll(path, "/", "\\")
		}
	}
	
	return filepath.FromSlash(path)
}
