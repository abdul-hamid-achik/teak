package search

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const maxSearchLineBytes = 1<<20 + 1

// MaxTextSearchResults is the hard upper bound for project text-search
// results. Callers that expose a machine-readable response can use it to
// explain when a result set reached the bounded limit.
const MaxTextSearchResults = 100

// Keep the internal name local to the search implementation and tests while
// exposing the contract above to control-plane callers.
const maxTextSearchResults = MaxTextSearchResults

// commonSkipDirs are non-text directories both search engines skip
// unconditionally, regardless of .gitignore contents. ripgrep respects
// .gitignore on its own (a feature over the plain walker below, which has no
// such support), but that alone would NOT skip these directories in a repo
// that doesn't happen to ignore them -- so ripgrepArgs (in ripgrep.go) also
// excludes this same list explicitly, keeping the guarantee identical
// between engines.
var commonSkipDirs = []string{"node_modules", "vendor", "__pycache__", "target", "build", "dist", "bin"}

// TextSearch performs a text/regex search across files in rootDir.
func TextSearch(rootDir, query string) ([]Result, error) {
	return TextSearchContext(context.Background(), rootDir, query, SearchOpts{})
}

// TextSearchContext is the cancellable form of TextSearch. It prefers the
// ripgrep-backed engine (see ripgrep.go) when the rg binary resolves, and
// falls back to the pure-Go walker below otherwise -- including when rg is
// present but fails for this particular query (rejected pattern, timeout,
// unparsable output, etc). The pure-Go walker is therefore always kept fully
// working; it is never removed, only used conditionally.
func TextSearchContext(ctx context.Context, rootDir, query string, opts SearchOpts) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if ripgrepAvailableFn() {
		results, err := ripgrepSearchContext(ctx, rootDir, query, opts, maxTextSearchResults)
		if err == nil {
			return results, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		// rg failed for this query alone (e.g. it rejected the pattern with
		// exit code 2, an internal timeout fired, or the output was
		// unparsable). Silently fall back to the Go walker rather than
		// surfacing an error to the user -- see requirement in the package
		// doc comment on ripgrep.go.
	}

	return textSearchGoWalker(ctx, rootDir, query, opts)
}

// textSearchGoWalker is the original pure-Go search engine: single-threaded,
// filepath.Walk based, with a hardcoded directory blocklist and no .gitignore
// support. It is the fallback used when ripgrep is unavailable or fails.
func textSearchGoWalker(ctx context.Context, rootDir, query string, opts SearchOpts) ([]Result, error) {
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
	maxResults := maxTextSearchResults

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
		isRoot := filepath.Clean(path) == filepath.Clean(rootDir)

		// Skip dotfiles and directories
		if strings.HasPrefix(name, ".") && !isRoot {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common non-text directories
		if info.IsDir() {
			if !isRoot && slices.Contains(commonSkipDirs, name) {
				return filepath.SkipDir
			}
			return nil
		}
		// filepath.Walk does not follow directory symlinks, but it can still
		// encounter symlinks, devices, and named pipes. Searching only regular
		// files prevents the fallback from blocking on a FIFO or reading an
		// unrelated device when rg is unavailable.
		if !info.Mode().IsRegular() {
			return nil
		}

		// Skip large/binary files
		if info.Size() > 1<<20 { // 1MB
			return nil
		}

		// Prefer the cheap extension allowlist, but probe unknown extensions by
		// content so the fallback matches rg for project-specific files such as
		// .svelte, .conf, and .inc. Secret env files remain excluded explicitly.
		if !isSearchableFile(path, name) {
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
			// Treat the rest of the file as binary/undecodable, but keep the
			// matches already found in it rather than discarding them.
			break
		}
		if strings.IndexByte(line, 0) >= 0 {
			// A NUL byte is ripgrep's binary-file sentinel. Keep any earlier
			// matches, but do not treat the remainder as source text.
			break
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

const textProbeBytes = 8 * 1024

func isSearchableFile(path, name string) bool {
	if isTextFile(name) {
		return true
	}
	if strings.EqualFold(filepath.Ext(name), ".env") {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, textProbeBytes)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	buf = buf[:n]
	return !slices.Contains(buf, byte(0)) && utf8.Valid(buf)
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
