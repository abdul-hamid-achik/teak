package search

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// vecgrepReady caches per-rootDir whether vecgrep has been initialized+indexed.
var vecgrepReady sync.Map

// InvalidateSemanticIndex clears the cached ready state for a workspace so the
// next semantic search rechecks and reindexes vecgrep as needed.
func InvalidateSemanticIndex(rootDir string) {
	if rootDir == "" {
		return
	}
	vecgrepReady.Delete(rootDir)
}

// SemanticSearch performs a semantic code search using vecgrep.
func SemanticSearch(rootDir, query string) ([]Result, error) {
	_, err := exec.LookPath("vecgrep")
	if err != nil {
		return nil, fmt.Errorf("vecgrep not found: install it for semantic search")
	}

	// Ensure the project is initialized and indexed
	if err := ensureVecgrepReady(rootDir); err != nil {
		return nil, fmt.Errorf("vecgrep setup failed: %w", err)
	}

	cmd := exec.Command("vecgrep", "search", query, "--format", "json", "--limit", "20")
	cmd.Dir = rootDir
	out, err := cmd.Output()
	if err != nil {
		// Try without --format flag for older versions
		cmd2 := exec.Command("vecgrep", "search", query)
		cmd2.Dir = rootDir
		out2, err2 := cmd2.Output()
		if err2 != nil {
			return nil, fmt.Errorf("vecgrep search failed: %w", err)
		}
		return parsePlainOutput(out2), nil
	}

	return parseJSONOutput(out)
}

// ensureVecgrepReady checks vecgrep status and initializes/indexes if needed.
// Results are cached per rootDir so subsequent calls in the same session are instant.
func ensureVecgrepReady(rootDir string) error {
	if _, ok := vecgrepReady.Load(rootDir); ok {
		return nil
	}

	// Check current status
	cmd := exec.Command("vecgrep", "status")
	cmd.Dir = rootDir
	out, err := cmd.Output()

	if err != nil || !isIndexed(string(out)) {
		// Initialize the project
		initCmd := exec.Command("vecgrep", "init", rootDir)
		initCmd.Dir = rootDir
		if initErr := initCmd.Run(); initErr != nil {
			return fmt.Errorf("vecgrep init failed: %w", initErr)
		}

		// Index the project
		indexCmd := exec.Command("vecgrep", "index")
		indexCmd.Dir = rootDir
		if indexErr := indexCmd.Run(); indexErr != nil {
			return fmt.Errorf("vecgrep index failed: %w", indexErr)
		}
	}

	vecgrepReady.Store(rootDir, true)
	return nil
}

// isIndexed checks vecgrep status output to determine if the project is indexed.
func isIndexed(statusOutput string) bool {
	s := strings.ToLower(statusOutput)
	// If status contains indicators of a healthy index, consider it ready
	if strings.Contains(s, "indexed") || strings.Contains(s, "files:") || strings.Contains(s, "ready") {
		return true
	}
	// If status mentions it's not initialized or has no index, it's not ready
	if strings.Contains(s, "not initialized") || strings.Contains(s, "no index") || strings.Contains(s, "not found") {
		return false
	}
	// If we got valid output, assume it's okay
	return len(strings.TrimSpace(statusOutput)) > 0
}

type vecgrepResult struct {
	File         string  `json:"file"`
	FilePath     string  `json:"file_path"`
	RelativePath string  `json:"relative_path"`
	Line         int     `json:"line"`
	StartLine    int     `json:"start_line"`
	Col          int     `json:"col"`
	Preview      string  `json:"preview"`
	Score        float64 `json:"score"`
	Text         string  `json:"text"`
	Content      string  `json:"content"`
}

func parseJSONOutput(data []byte) ([]Result, error) {
	// Try array of results
	var results []vecgrepResult
	if err := json.Unmarshal(data, &results); err != nil {
		// Try line-delimited JSON
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		for _, line := range lines {
			var r vecgrepResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				continue
			}
			results = append(results, r)
		}
	}

	var out []Result
	for _, r := range results {
		// Resolve file path: prefer relative_path > file_path > file
		filePath := r.RelativePath
		if filePath == "" {
			filePath = r.FilePath
		}
		if filePath == "" {
			filePath = r.File
		}

		// Resolve line number: prefer start_line > line
		line := r.StartLine
		if line == 0 && r.Line > 0 {
			line = r.Line
		}

		// Resolve preview: prefer preview > first line of content > text
		preview := r.Preview
		if preview == "" && r.Content != "" {
			// Use first non-empty line of content as preview
			for _, l := range strings.SplitN(r.Content, "\n", 5) {
				trimmed := strings.TrimSpace(l)
				if trimmed != "" {
					preview = trimmed
					break
				}
			}
		}
		if preview == "" {
			preview = r.Text
		}

		out = append(out, Result{
			FilePath: filePath,
			Line:     line,
			Col:      r.Col,
			Preview:  strings.TrimSpace(preview),
			Score:    r.Score,
		})
	}
	return out, nil
}

func parsePlainOutput(data []byte) []Result {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var results []Result
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Try to parse "file:line: text" format
		parts := strings.SplitN(line, ":", 3)
		if len(parts) >= 2 {
			lineNum := 0
			fmt.Sscanf(parts[1], "%d", &lineNum)
			preview := ""
			if len(parts) >= 3 {
				preview = strings.TrimSpace(parts[2])
			}
			results = append(results, Result{
				FilePath: parts[0],
				Line:     lineNum,
				Preview:  preview,
			})
		}
	}
	return results
}
