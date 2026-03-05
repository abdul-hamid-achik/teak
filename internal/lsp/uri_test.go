package lsp

import (
	"runtime"
	"testing"
)

func TestFileURI(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/home/user/file.go",
			expected: "file:///home/user/file.go",
		},
		{
			name:     "path with spaces",
			path:     "/home/user/my file.go",
			expected: "file:///home/user/my%20file.go",
		},
		{
			name:     "path with unicode",
			path:     "/home/user/файл.go",
			expected: "file:///home/user/%D1%84%D0%B0%D0%B9%D0%BB.go",
		},
		{
			name:     "path with special chars",
			path:     "/home/user/file#1.go",
			expected: "file:///home/user/file%231.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Skipping Unix-specific tests on Windows")
			}
			result := FileURI(tt.path)
			if result != tt.expected {
				t.Errorf("FileURI(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestURIToPath(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		expected string
	}{
		{
			name:     "simple uri",
			uri:      "file:///home/user/file.go",
			expected: "/home/user/file.go",
		},
		{
			name:     "uri with spaces",
			uri:      "file:///home/user/my%20file.go",
			expected: "/home/user/my file.go",
		},
		{
			name:     "uri with unicode",
			uri:      "file:///home/user/%D1%84%D0%B0%D0%B9%D0%BB.go",
			expected: "/home/user/файл.go",
		},
		{
			name:     "uri with special chars",
			uri:      "file:///home/user/file%231.go",
			expected: "/home/user/file#1.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Skipping Unix-specific tests on Windows")
			}
			result := URIToPath(tt.uri)
			if result != tt.expected {
				t.Errorf("URIToPath(%q) = %q, want %q", tt.uri, result, tt.expected)
			}
		})
	}
}

func TestFileURIAndBack(t *testing.T) {
	paths := []string{
		"/home/user/file.go",
		"/home/user/my file.go",
		"/home/user/file#1.go",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Skipping Unix-specific tests on Windows")
			}
			uri := FileURI(path)
			result := URIToPath(uri)
			if result != path {
				t.Errorf("URIToPath(FileURI(%q)) = %q, want %q", path, result, path)
			}
		})
	}
}

func TestURIToPathNonFile(t *testing.T) {
	// Non-file URIs should be returned as-is (minus file:// prefix handling)
	uri := "https://example.com/file.go"
	result := URIToPath(uri)
	if result != uri {
		t.Errorf("URIToPath(%q) = %q, want %q", uri, result, uri)
	}
}
