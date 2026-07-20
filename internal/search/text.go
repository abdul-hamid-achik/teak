package search

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxSearchLineBytes = 1<<20 + 1

// TextSearch performs a text/regex search across files in rootDir.
func TextSearch(rootDir, query string) ([]Result, error) {
	return TextSearchContext(context.Background(), rootDir, query, SearchOpts{})
}

// TextSearchContext is the cancellable form of TextSearch.
func TextSearchContext(ctx context.Context, rootDir, query string, opts SearchOpts) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	re, err := CompilePattern(query, opts)
	if err != nil {
		return nil, err
	}

	var results []Result
	maxResults := 100

	err = filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
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

		fileResults, err := searchFileContext(ctx, path, rootDir, re, maxResults-len(results))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return nil // skip errored files
		}
		results = append(results, fileResults...)
		return nil
	})

	return results, err
}

func searchFile(path, rootDir string, re *regexp.Regexp, limit int) (results []Result, retErr error) {
	return searchFileContext(context.Background(), path, rootDir, re, limit)
}

func searchFileContext(ctx context.Context, path, rootDir string, re *regexp.Regexp, limit int) (results []Result, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			closeErr := fmt.Errorf("close searched file %q: %w", path, err)
			if retErr == nil {
				retErr = closeErr
				return
			}
			retErr = errors.Join(retErr, closeErr)
		}
	}()

	relPath, err := filepath.Rel(rootDir, path)
	if err != nil {
		relPath = path
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxSearchLineBytes)
	lineNum := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
		".bp": true, ".http": true, ".hitspec": true,
		".tf": true, ".hcl": true, ".nix": true,
		".mod": true, ".sum": true, ".lock": true,
		".env": false, // skip .env files
		"":     true,  // files without extension (Makefile, Dockerfile, etc.)
	}
	return textExts[ext]
}
