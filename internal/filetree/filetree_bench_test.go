package filetree

import (
	"os"
	"path/filepath"
	"testing"

	"teak/internal/ui"
)

// createTestTree creates a file tree model with a specified number of entries
func createTestTree(entryCount int, theme ui.Theme) Model {
	// Create a temporary directory structure
	tmpDir := os.TempDir()
	root := filepath.Join(tmpDir, "teak_test_tree")

	// Clean up and create fresh
	os.RemoveAll(root)
	os.MkdirAll(root, 0755)

	// Create test entries
	for i := 0; i < entryCount; i++ {
		if i%5 == 0 {
			// Create a directory
			dirPath := filepath.Join(root, filepath.FromSlash(getTestDirName(i)))
			os.MkdirAll(dirPath, 0755)
		} else {
			// Create a file
			filePath := filepath.Join(root, getTestFileName(i))
			os.WriteFile(filePath, []byte("test content"), 0644)
		}
	}

	m := New(root, theme)
	m.SetSize(30, 30) // Set a reasonable size for rendering
	return m
}

func getTestFileName(i int) string {
	return filepath.FromSlash("file_" + string(rune('a'+i%26)) + ".go")
}

func getTestDirName(i int) string {
	return filepath.FromSlash("dir_" + string(rune('A'+i%26)))
}

func BenchmarkFileTreeView10Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(10, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeView30Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(30, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeView50Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(50, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeView100Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(100, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeViewWithDiagnostics(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(30, theme)

	// Set diagnostics (use paths that might exist)
	diags := map[string]int{
		"file_a.go": 1, // Error
		"file_b.go": 2, // Warning
		"file_c.go": 3, // Info
	}
	m.SetDiagnostics(diags)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeViewWithGitStatus(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(30, theme)

	// Set git status
	gitStatus := map[string]string{
		"file_a.go": "M", // Modified
		"file_b.go": "A", // Added
		"file_c.go": "D", // Deleted
	}
	m.SetGitStatus(gitStatus)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeFlatEntries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(50, theme)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.cachedFlat = nil // Clear cache to force rebuild
		_ = m.flatEntries()
	}
}
