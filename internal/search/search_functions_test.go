package search

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTextSearch tests the TextSearch function
func TestTextSearch(t *testing.T) {
	// Create temp directory with test files
	tmpDir := t.TempDir()

	// Create test files
	testFiles := map[string]string{
		"test1.go":   "package main\nfunc main() {\n\tprintln(\"hello\")\n}",
		"test2.go":   "package main\nfunc test() {\n\tprintln(\"world\")\n}",
		"test.txt":   "hello world\ntest line\nanother line",
		".hidden.go": "package hidden", // Should be skipped
	}

	for name, content := range testFiles {
		path := filepath.Join(tmpDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Test searching for "hello"
	results, err := TextSearch(tmpDir, "hello")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	if len(results) < 1 {
		t.Errorf("Expected at least 1 result, got %d", len(results))
	}

	// Verify result structure
	if len(results) > 0 {
		r := results[0]
		if r.FilePath == "" {
			t.Error("Expected non-empty FilePath")
		}
		if r.Line < 0 {
			t.Errorf("Expected non-negative Line, got %d", r.Line)
		}
		if r.Preview == "" {
			t.Error("Expected non-empty Preview")
		}
	}
}

// TestTextSearchWithEmptyQuery tests TextSearch with empty query
func TestTextSearchWithEmptyQuery(t *testing.T) {
	tmpDir := t.TempDir()

	results, err := TextSearch(tmpDir, "")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	// Empty query should return no results or error
	if len(results) > 0 {
		t.Errorf("Expected 0 results for empty query, got %d", len(results))
	}
}

// TestTextSearchWithNonExistentDir tests TextSearch with non-existent directory
func TestTextSearchWithNonExistentDir(t *testing.T) {
	results, _ := TextSearch("/nonexistent/directory/path", "test")
	// filepath.Walk may or may not return an error for non-existent directories
	// but it should return no results
	if len(results) > 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

// TestTextSearchWithSpecialCharacters tests TextSearch with special characters
func TestTextSearchWithSpecialCharacters(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file with special characters
	content := "package main\nfunc test() {\n\tprintln(\"hello.world+test\")\n}"
	path := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Search for string with special regex characters
	results, err := TextSearch(tmpDir, "hello.world")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	// Should find the match (QueryMeta escapes special chars)
	if len(results) < 1 {
		t.Errorf("Expected at least 1 result, got %d", len(results))
	}
}

// TestTextSearchResultLimit tests that results are limited
func TestTextSearchResultLimit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create many files with the same content
	for i := 0; i < 150; i++ {
		content := "package main\nfunc test() { println(\"match\") }"
		path := filepath.Join(tmpDir, "file"+string(rune('a'+i%26))+string(rune('0'+i/26))+".go")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	results, err := TextSearch(tmpDir, "match")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	// Should be limited to 100 results
	if len(results) > 100 {
		t.Errorf("Expected max 100 results, got %d", len(results))
	}
}

// TestTextSearchCaseInsensitive tests case-insensitive search
func TestTextSearchCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()

	content := "package main\nfunc Test() { }\nfunc test() { }"
	path := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Search for "TEST" should find both "Test" and "test"
	results, err := TextSearch(tmpDir, "TEST")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	if len(results) < 1 {
		t.Errorf("Expected at least 1 result, got %d", len(results))
	}
}

// TestSearchFile tests the searchFile helper function
func TestSearchFile(t *testing.T) {
	tmpDir := t.TempDir()

	content := "line 1\nline 2 with match\nline 3\nline 4 with match\n"
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Use regex that matches "match"
	re := regexp.MustCompile("match")
	results, err := searchFile(path, tmpDir, re, 10)
	if err != nil {
		t.Fatalf("searchFile failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Verify line numbers
	expectedLines := []int{1, 3} // 0-indexed
	for i, expected := range expectedLines {
		if results[i].Line != expected {
			t.Errorf("Expected line %d, got %d", expected, results[i].Line)
		}
	}
}

// TestSearchFileWithLimit tests searchFile with result limit
func TestSearchFileWithLimit(t *testing.T) {
	tmpDir := t.TempDir()

	content := "match\nmatch\nmatch\nmatch\nmatch\n"
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	re := regexp.MustCompile("match")
	results, err := searchFile(path, tmpDir, re, 2)
	if err != nil {
		t.Fatalf("searchFile failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results (limited), got %d", len(results))
	}
}

// TestIsTextFile tests the isTextFile helper function
func TestIsTextFile(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"test.go", true},
		{"test.py", true},
		{"test.js", true},
		{"test.ts", true},
		{"test.rs", true},
		{"test.c", true},
		{"test.h", true},
		{"test.cpp", true},
		{"test.java", true},
		{"test.html", true},
		{"test.css", true},
		{"test.json", true},
		{"test.yaml", true},
		{"test.yml", true},
		{"test.md", true},
		{"test.txt", true},
		{"test.sh", true},
		{"test.sql", true},
		{"test.lua", true},
		{"test.dart", true},
		{"test.swift", true},
		{"test.tf", true},
		{"Makefile", true}, // No extension
		{"Dockerfile", true},
		{"test.env", false}, // Should skip .env files
		{"test.bin", false}, // Unknown extension
		{"test.exe", false},
		{"test.dll", false},
		{"test.so", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTextFile(tt.name)
			if result != tt.expected {
				t.Errorf("isTextFile(%q) = %v, want %v", tt.name, result, tt.expected)
			}
		})
	}
}

// TestIsTextFileWithDifferentCases tests isTextFile with different cases
func TestIsTextFileWithDifferentCases(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"test.GO", true},
		{"test.Go", true},
		{"test.gO", true},
		{"test.PY", true},
		{"test.JS", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTextFile(tt.name)
			if result != tt.expected {
				t.Errorf("isTextFile(%q) = %v, want %v", tt.name, result, tt.expected)
			}
		})
	}
}

// TestTextSearchSkipsDotfiles tests that dotfiles are skipped
func TestTextSearchSkipsDotfiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create regular file
	regularFile := filepath.Join(tmpDir, "regular.go")
	if err := os.WriteFile(regularFile, []byte("package main\nfunc match() {}"), 0o644); err != nil {
		t.Fatalf("Failed to create regular file: %v", err)
	}

	// Create dotfile
	dotFile := filepath.Join(tmpDir, ".hidden.go")
	if err := os.WriteFile(dotFile, []byte("package hidden\nfunc match() {}"), 0o644); err != nil {
		t.Fatalf("Failed to create dotfile: %v", err)
	}

	results, err := TextSearch(tmpDir, "match")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	// Should only find the regular file
	if len(results) != 1 {
		t.Errorf("Expected 1 result (skipping dotfiles), got %d", len(results))
	}
	if results[0].FilePath != "regular.go" {
		t.Errorf("Expected 'regular.go', got %q", results[0].FilePath)
	}
}

// TestTextSearchSkipsBinaryFiles tests that binary files are skipped
func TestTextSearchSkipsBinaryFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file that's too large (>1MB should be skipped)
	largeContent := strings.Repeat("x", 1<<20+1) // 1MB + 1 byte
	largeFile := filepath.Join(tmpDir, "large.go")
	if err := os.WriteFile(largeFile, []byte(largeContent), 0o644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	// Create normal file
	normalFile := filepath.Join(tmpDir, "normal.go")
	if err := os.WriteFile(normalFile, []byte("package main\nfunc match() {}"), 0o644); err != nil {
		t.Fatalf("Failed to create normal file: %v", err)
	}

	results, err := TextSearch(tmpDir, "match")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	// Should only find the normal file
	if len(results) != 1 {
		t.Errorf("Expected 1 result (skipping large files), got %d", len(results))
	}
}

// TestTextSearchSkipsCommonDirectories tests that common directories are skipped
func TestTextSearchSkipsCommonDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create node_modules directory with matching file
	nodeModules := filepath.Join(tmpDir, "node_modules", "package", "index.js")
	if err := os.MkdirAll(filepath.Dir(nodeModules), 0o755); err != nil {
		t.Fatalf("Failed to create node_modules: %v", err)
	}
	if err := os.WriteFile(nodeModules, []byte("function match() {}"), 0o644); err != nil {
		t.Fatalf("Failed to create file in node_modules: %v", err)
	}

	// Create regular file
	regularFile := filepath.Join(tmpDir, "regular.js")
	if err := os.WriteFile(regularFile, []byte("function match() {}"), 0o644); err != nil {
		t.Fatalf("Failed to create regular file: %v", err)
	}

	results, err := TextSearch(tmpDir, "match")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	// Should only find the regular file
	if len(results) != 1 {
		t.Errorf("Expected 1 result (skipping node_modules), got %d", len(results))
	}
}

// TestTextSearchWithInvalidRegex tests TextSearch with invalid regex
func TestTextSearchWithInvalidRegex(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test file
	content := "package main"
	path := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// TextSearch compiles regex with (?i) prefix and QuoteMeta, so it should always be valid
	// This test verifies the function handles the compilation properly
	results, err := TextSearch(tmpDir, "test[invalid")
	if err != nil {
		// If there's an error, that's acceptable for invalid input
		return
	}

	// If no error, results should be empty or contain matches
	_ = results
}

// TestSearchFileWithInvalidPath tests searchFile with invalid path
func TestSearchFileWithInvalidPath(t *testing.T) {
	re := regexp.MustCompile("test")
	results, err := searchFile("/nonexistent/path/file.txt", "/tmp", re, 10)
	if err == nil {
		t.Error("Expected error for invalid path")
	}
	if len(results) > 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

// TestSearchFileWithBinaryContent tests searchFile with binary content
func TestSearchFileWithBinaryContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with invalid UTF-8
	content := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE}
	path := filepath.Join(tmpDir, "binary.txt")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("Failed to create binary file: %v", err)
	}

	re := regexp.MustCompile("test")
	results, err := searchFile(path, tmpDir, re, 10)
	if err != nil {
		t.Fatalf("searchFile failed: %v", err)
	}

	// Should return nil results for binary file
	if results != nil {
		t.Errorf("Expected nil results for binary file, got %d results", len(results))
	}
}

func TestSearchFileWithLongLine(t *testing.T) {
	tmpDir := t.TempDir()

	longLine := strings.Repeat("a", 70_000) + "needle"
	path := filepath.Join(tmpDir, "long.txt")
	if err := os.WriteFile(path, []byte(longLine+"\n"), 0o644); err != nil {
		t.Fatalf("Failed to create long-line file: %v", err)
	}

	re := regexp.MustCompile("needle")
	results, err := searchFile(path, tmpDir, re, 10)
	if err != nil {
		t.Fatalf("searchFile failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Col != 70_000 {
		t.Errorf("Expected match column 70000, got %d", results[0].Col)
	}
}

func TestInvalidateSemanticIndex(t *testing.T) {
	rootDir := t.TempDir()
	vecgrepReady.Store(rootDir, true)
	t.Cleanup(func() {
		vecgrepReady.Delete(rootDir)
	})

	InvalidateSemanticIndex(rootDir)

	if _, ok := vecgrepReady.Load(rootDir); ok {
		t.Fatal("expected semantic index cache entry to be cleared")
	}
}

// TestTextSearchResultStructure tests that results have proper structure
func TestTextSearchResultStructure(t *testing.T) {
	tmpDir := t.TempDir()

	content := "package main\nfunc TestFunction() {\n\tprintln(\"hello\")\n}"
	path := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	results, err := TextSearch(tmpDir, "TestFunction")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	r := results[0]

	// Verify all fields are populated correctly
	if r.FilePath == "" {
		t.Error("Expected non-empty FilePath")
	}
	if r.Line < 0 {
		t.Errorf("Expected non-negative Line, got %d", r.Line)
	}
	if r.Col < 0 {
		t.Errorf("Expected non-negative Col, got %d", r.Col)
	}
	if r.Preview == "" {
		t.Error("Expected non-empty Preview")
	}
	// Score is optional for text search
}

// TestTextSearchMultipleMatchesInFile tests multiple matches in same file
func TestTextSearchMultipleMatchesInFile(t *testing.T) {
	tmpDir := t.TempDir()

	content := "unique123 line 1\nno match\nunique123 line 3\nno match\nunique123 line 5\n"
	path := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	results, err := TextSearch(tmpDir, "unique123")
	if err != nil {
		t.Fatalf("TextSearch failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Verify line numbers
	expectedLines := []int{0, 2, 4} // 0-indexed
	for i, expected := range expectedLines {
		if i < len(results) && results[i].Line != expected {
			t.Errorf("Expected line %d, got %d", expected, results[i].Line)
		}
	}
}
