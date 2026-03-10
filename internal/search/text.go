package search

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxSearchLineBytes = 1<<20 + 1

// TextSearch performs a text/regex search across files in rootDir.
func TextSearch(rootDir, query string) ([]Result, error) {
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return nil, err
	}

	var results []Result
	maxResults := 100

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if len(results) >= maxResults {
			return filepath.SkipAll
		}

		name := info.Name()

		// Skip dotfiles and directories
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common non-text directories
		if info.IsDir() {
			switch name {
			case "node_modules", "vendor", "__pycache__", "target", "build", "dist", "bin":
				return filepath.SkipDir
			}
			return nil
		}

		// Skip large/binary files
		if info.Size() > 1<<20 { // 1MB
			return nil
		}

		// Skip files without common text extensions
		if !isTextFile(name) {
			return nil
		}

		fileResults, err := searchFile(path, rootDir, re, maxResults-len(results))
		if err != nil {
			return nil // skip errored files
		}
		results = append(results, fileResults...)
		return nil
	})

	return results, err
}

func searchFile(path, rootDir string, re *regexp.Regexp, limit int) ([]Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	relPath, err := filepath.Rel(rootDir, path)
	if err != nil {
		relPath = path
	}

	var results []Result
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxSearchLineBytes)
	lineNum := 0
	for scanner.Scan() {
		if len(results) >= limit {
			break
		}
		line := scanner.Text()
		if !utf8.ValidString(line) {
			return nil, nil // binary file
		}
		loc := re.FindStringIndex(line)
		if loc != nil {
			preview := strings.TrimSpace(line)
			results = append(results, Result{
				FilePath: relPath,
				Line:     lineNum,
				Col:      loc[0],
				Preview:  preview,
			})
		}
		lineNum++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func isTextFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	textExts := map[string]bool{
		".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
		".rs": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cc": true,
		".java": true, ".kt": true, ".scala": true, ".rb": true, ".php": true,
		".html": true, ".css": true, ".scss": true, ".less": true,
		".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true,
		".md": true, ".txt": true, ".sh": true, ".bash": true, ".zsh": true,
		".sql": true, ".graphql": true, ".proto": true,
		".lua": true, ".vim": true, ".el": true, ".clj": true, ".ex": true, ".exs": true,
		".zig": true, ".nim": true, ".dart": true, ".swift": true,
		".tf": true, ".hcl": true, ".nix": true,
		".mod": true, ".sum": true, ".lock": true,
		".env": false, // skip .env files
		"":     true,  // files without extension (Makefile, Dockerfile, etc.)
	}
	return textExts[ext]
}
