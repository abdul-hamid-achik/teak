package codemap

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"teak/internal/toolpath"
)

const (
	commandTimeout = 15 * time.Second
	maxOutputBytes = 4 << 20
)

// Available returns true if the codemap binary can be resolved.
func Available() bool {
	return toolpath.Available("codemap")
}

// Symbol represents a code symbol from codemap.
type Symbol struct {
	Symbol    string `json:"symbol"`
	FQN       string `json:"fqn"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"` // 1-based (codemap convention)
	EndLine   int    `json:"end_line"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
}

// Line0 returns the 0-based start line (Teak convention).
func (s Symbol) Line0() int {
	if s.StartLine > 0 {
		return s.StartLine - 1
	}
	return 0
}

// ContextResult is the output of `codemap context <symbol>`.
type ContextResult struct {
	Definitions []Symbol `json:"definitions"`
	Callers     []Symbol `json:"callers"`
	Callees     []Symbol `json:"callees"`
	References  []Symbol `json:"references"`
	Tests       []Symbol `json:"tests"`
}

// ImpactResult is the output of `codemap impact <symbol>`.
type ImpactResult struct {
	Locations     []Symbol `json:"locations"`
	DirectCallers []Symbol `json:"direct_callers"`
	BlastRadius   []struct {
		Symbol
		Depth int `json:"depth"`
	} `json:"blast_radius"`
	Tests        []Symbol `json:"tests"`
	TestCommands []string `json:"test_commands"`
}

// StatusResult is the output of `codemap status`.
type StatusResult struct {
	Indexed bool `json:"indexed"`
	Nodes   int  `json:"nodes"`
	Edges   int  `json:"edges"`
	Files   int  `json:"files"`
	Stale   struct {
		Changed []string `json:"changed"`
		New     []string `json:"new"`
		Deleted []string `json:"deleted"`
	} `json:"stale"`
}

// EnsureReady checks if the project is indexed and indexes it if needed.
func EnsureReady(ctx context.Context, rootDir string) error {
	if !Available() {
		return fmt.Errorf("codemap not found: install it for code intelligence")
	}
	out, err := run(ctx, rootDir, "status", "--json")
	if err != nil {
		// Not indexed — initialize
		if _, initErr := run(ctx, rootDir, "init"); initErr != nil {
			return fmt.Errorf("codemap init: %w", initErr)
		}
		if _, idxErr := run(ctx, rootDir, "index", "--no-embed"); idxErr != nil {
			return fmt.Errorf("codemap index: %w", idxErr)
		}
		return nil
	}
	var status StatusResult
	if json.Unmarshal(out, &status) == nil && status.Indexed {
		return nil
	}
	if _, idxErr := run(ctx, rootDir, "index", "--no-embed"); idxErr != nil {
		return fmt.Errorf("codemap index: %w", idxErr)
	}
	return nil
}

// Context returns the full context for a symbol (definitions, callers, callees, references, tests).
func Context(ctx context.Context, rootDir, symbol string) (*ContextResult, error) {
	out, err := run(ctx, rootDir, "context", symbol, "--json")
	if err != nil {
		return nil, err
	}
	var result ContextResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse codemap context: %w", err)
	}
	return &result, nil
}

// Impact returns the blast radius and covering tests for a symbol.
func Impact(ctx context.Context, rootDir, symbol string, depth int) (*ImpactResult, error) {
	out, err := run(ctx, rootDir, "impact", symbol, "--depth", fmt.Sprintf("%d", depth), "--json")
	if err != nil {
		return nil, err
	}
	var result ImpactResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse codemap impact: %w", err)
	}
	return &result, nil
}

// Callers returns the call graph callers of a symbol.
func Callers(ctx context.Context, rootDir, symbol string) ([]Symbol, error) {
	out, err := run(ctx, rootDir, "callers", symbol, "--json")
	if err != nil {
		return nil, err
	}
	var result struct {
		Callers []Symbol `json:"callers"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		// Try plain array
		var symbols []Symbol
		if err2 := json.Unmarshal(out, &symbols); err2 != nil {
			return nil, fmt.Errorf("parse codemap callers: %w", err)
		}
		return symbols, nil
	}
	return result.Callers, nil
}

// Callees returns the call graph callees of a symbol.
func Callees(ctx context.Context, rootDir, symbol string) ([]Symbol, error) {
	out, err := run(ctx, rootDir, "callees", symbol, "--json")
	if err != nil {
		return nil, err
	}
	var result struct {
		Callees []Symbol `json:"callees"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		var symbols []Symbol
		if err2 := json.Unmarshal(out, &symbols); err2 != nil {
			return nil, fmt.Errorf("parse codemap callees: %w", err)
		}
		return symbols, nil
	}
	return result.Callees, nil
}

// SymbolAt resolves the symbol at a file:line position.
func SymbolAt(ctx context.Context, rootDir, file string, line int) (*Symbol, error) {
	out, err := run(ctx, rootDir, "symbol-at", fmt.Sprintf("%s:%d", file, line+1), "--json")
	if err != nil {
		return nil, err
	}
	var symbol Symbol
	if err := json.Unmarshal(out, &symbol); err != nil {
		return nil, fmt.Errorf("parse codemap symbol-at: %w", err)
	}
	return &symbol, nil
}

// Find searches for symbols by name across the project.
func Find(ctx context.Context, rootDir, name string) ([]Symbol, error) {
	out, err := run(ctx, rootDir, "find", name, "--json")
	if err != nil {
		return nil, err
	}
	var symbols []Symbol
	if err := json.Unmarshal(out, &symbols); err != nil {
		return nil, fmt.Errorf("parse codemap find: %w", err)
	}
	return symbols, nil
}

func run(ctx context.Context, rootDir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd, err := toolpath.Command(ctx, "codemap", args...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = rootDir
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail := strings.TrimSpace(string(exitErr.Stderr))
			if detail != "" {
				return nil, fmt.Errorf("%w: %s", err, detail)
			}
		}
		return nil, err
	}
	if len(out) > maxOutputBytes {
		out = out[:maxOutputBytes]
	}
	return out, nil
}
