package search

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrInvalidResult means an external search adapter returned a result that
// cannot be safely mapped to a workspace file and editor position.
var ErrInvalidResult = errors.New("invalid search result")

// ValidateResults copies and normalizes results returned by an external search
// adapter. Search engines are subprocesses and their JSON is untrusted: a
// malformed or compromised adapter must not make the editor or a headless
// client open an arbitrary path outside the selected workspace.
//
// FilePath is returned in workspace-relative slash form. Existing regular
// files are required because a search hit that cannot be opened is not a
// usable result. The input slice and its elements are never mutated.
func ValidateResults(rootDir string, results []Result) ([]Result, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, fmt.Errorf("%w: workspace root is empty", ErrInvalidResult)
	}
	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve workspace root: %v", ErrInvalidResult, err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve workspace root: %v", ErrInvalidResult, err)
	}
	rootInfo, err := os.Stat(rootReal)
	if err != nil || !rootInfo.IsDir() {
		if err == nil {
			err = errors.New("workspace root is not a directory")
		}
		return nil, fmt.Errorf("%w: workspace root: %v", ErrInvalidResult, err)
	}

	normalized := make([]Result, len(results))
	for index, result := range results {
		if result.FilePath == "" || strings.ContainsRune(result.FilePath, '\x00') {
			return nil, fmt.Errorf("%w: result %d has an invalid file path", ErrInvalidResult, index)
		}
		if result.Line < 0 || result.Col < 0 || result.EndLine < 0 {
			return nil, fmt.Errorf("%w: result %d has negative coordinates", ErrInvalidResult, index)
		}

		candidate := filepath.FromSlash(result.FilePath)
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(rootReal, candidate)
		}
		candidate, err = filepath.Abs(candidate)
		if err != nil {
			return nil, fmt.Errorf("%w: result %d path: %v", ErrInvalidResult, index, err)
		}
		candidateReal, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return nil, fmt.Errorf("%w: result %d file %q cannot be resolved: %v", ErrInvalidResult, index, result.FilePath, err)
		}
		info, err := os.Stat(candidateReal)
		if err != nil {
			return nil, fmt.Errorf("%w: result %d file %q cannot be read: %v", ErrInvalidResult, index, result.FilePath, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: result %d file %q is not regular", ErrInvalidResult, index, result.FilePath)
		}

		relative, err := filepath.Rel(rootReal, candidateReal)
		if err != nil || filepath.IsAbs(relative) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("%w: result %d file %q is outside workspace", ErrInvalidResult, index, result.FilePath)
		}
		result.FilePath = filepath.ToSlash(relative)
		normalized[index] = result
	}
	return normalized, nil
}
