package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"teak/internal/overlay"
)

// FileListMsg carries the cached file list from a background walk.
type FileListMsg struct {
	Files []string
}

// walkProjectFiles returns relative file paths under rootDir, skipping
// hidden directories, common build/dependency folders, and respecting
// a top-level .gitignore if present.
func walkProjectFiles(rootDir string) []string {
	ignorePatterns := loadGitignore(rootDir)
	var files []string

	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		rel, _ := filepath.Rel(rootDir, path)
		if rel == "." {
			return nil
		}

		name := d.Name()

		// Skip hidden and common non-source directories
		if d.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			if matchesGitignore(rel, ignorePatterns, true) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files and gitignored files
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if matchesGitignore(rel, ignorePatterns, false) {
			return nil
		}

		files = append(files, rel)
		return nil
	})

	return files
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".svn", ".hg", ".DS_Store",
		"node_modules", "vendor", "__pycache__",
		".next", ".nuxt", "dist", "build",
		".idea", ".vscode", ".cache", "coverage":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// loadGitignore reads a top-level .gitignore and returns simple patterns.
// Only supports basic glob patterns (no negation, no subdirectory gitignores).
func loadGitignore(rootDir string) []string {
	data, err := os.ReadFile(filepath.Join(rootDir, ".gitignore"))
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// matchesGitignore checks if a relative path matches any gitignore pattern.
func matchesGitignore(rel string, patterns []string, isDir bool) bool {
	for _, pat := range patterns {
		dirOnly := strings.HasSuffix(pat, "/")
		if dirOnly {
			if !isDir {
				continue
			}
			pat = strings.TrimSuffix(pat, "/")
		}

		// Match against basename
		base := filepath.Base(rel)
		if matched, _ := filepath.Match(pat, base); matched {
			return true
		}
		// Match against full relative path
		if matched, _ := filepath.Match(pat, rel); matched {
			return true
		}
		// Handle patterns like "dir/**"
		prefix := strings.TrimSuffix(pat, "/**")
		if prefix != pat {
			if strings.HasPrefix(rel, prefix+"/") || rel == prefix {
				return true
			}
		}
		// Handle directory prefix patterns like "bin"
		if !strings.Contains(pat, "*") && !strings.Contains(pat, "?") {
			if strings.HasPrefix(rel, pat+"/") || rel == pat {
				return true
			}
		}
	}
	return false
}

// quickOpenCmd walks the project directory in the background.
func quickOpenCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		files := walkProjectFiles(rootDir)
		return FileListMsg{Files: files}
	}
}

// filesToPickerItems converts file paths to picker items.
func filesToPickerItems(files []string) []overlay.PickerItem {
	items := make([]overlay.PickerItem, len(files))
	for i, f := range files {
		items[i] = overlay.PickerItem{
			Label:       filepath.Base(f),
			Description: filepath.Dir(f),
			Value:       f,
		}
	}
	return items
}
