package search

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"teak/internal/toolpath"
)

// This file implements TextSearchContext's primary engine: shelling out to
// ripgrep (rg). textSearchGoWalker in text.go remains the fallback used when
// rg cannot be resolved or fails for a given query.
//
// Output format: --json rather than --vimgrep. --vimgrep emits one
// colon-delimited "file:line:col:text" line per match, which requires
// re-splitting on ':' and is ambiguous when the file path or match text
// itself contains a colon (common in Go: "pkg.Func", Windows-style paths,
// URLs in comments, etc). --json instead gives structured begin/match/
// end/summary records with an explicit byte offset per submatch, so parsing
// is unambiguous and the column offset lines up exactly with what
// regexp.FindStringIndex produces for the Go walker (both are byte offsets
// into the line). The tradeoff is slightly more output per match; that is
// bounded below via boundedCommandBuffer.
const (
	ripgrepTimeout        = 10 * time.Second
	maxRipgrepOutputBytes = 8 << 20  // 8MB of stdout
	maxRipgrepStderrBytes = 64 << 10 // 64KB of stderr, for error messages only
	ripgrepMaxFileSize    = "1M"     // mirrors textSearchGoWalker's 1MB file-size skip
)

// ripgrepAvailableFn and ripgrepCommandFn are indirections over toolpath so
// tests can simulate "rg is missing" or "rg fails" deterministically without
// depending on whether rg happens to be installed on the machine running the
// tests.
var (
	ripgrepAvailableFn = func() bool { return toolpath.Available("rg") }
	ripgrepCommandFn   = func(ctx context.Context, args ...string) (*exec.Cmd, error) {
		return toolpath.Command(ctx, "rg", args...)
	}
)

// ripgrepSearchContext runs rg over rootDir and parses its --json output into
// Results. Its error contract is intentionally loose: any error other than
// context cancellation/deadline should be treated by the caller as "try the
// fallback engine instead", not surfaced to the user. That covers:
//   - rg not resolving after all (races with ripgrepAvailableFn)
//   - rg rejecting the pattern as invalid Rust regex syntax (exit code 2)
//   - any other rg failure (missing rootDir, permission errors, ...)
//   - the internal ripgrepTimeout firing
//   - output the parser could not make sense of at all
//
// A nil error with a non-nil (possibly empty) result slice means rg ran
// successfully; an empty slice with exit code 1 (no matches) is not an error.
func ripgrepSearchContext(ctx context.Context, rootDir, query string, opts SearchOpts, maxResults int) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Bound the subprocess so a pathological search (e.g. a huge repo on a
	// slow filesystem) cannot hang the request indefinitely. This is a
	// separate, tighter bound than whatever deadline the caller's ctx may
	// carry; hitting it falls back to the Go walker rather than propagating
	// as a user-visible cancellation.
	runCtx, cancel := context.WithTimeout(ctx, ripgrepTimeout)
	defer cancel()

	cmd, err := ripgrepCommandFn(runCtx, ripgrepArgs(query, opts)...)
	if err != nil {
		return nil, err
	}
	cmd.Dir = rootDir

	stdout := &boundedCommandBuffer{limit: maxRipgrepOutputBytes, onLimit: cancel}
	stderr := &boundedCommandBuffer{limit: maxRipgrepStderrBytes, onLimit: cancel}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()

	results, parseErr := parseRipgrepJSON(stdout.Bytes(), maxResults)
	if stdout.exceeded {
		// A bounded stdout stream may contain valid matches before it is
		// truncated. Those matches are only a prefix of the search, though,
		// so returning them as success would silently hide later files. Let
		// TextSearchContext use the bounded Go walker to recover a complete
		// result set instead.
		return nil, errors.New("ripgrep output exceeded limit")
	}
	if parseErr != nil {
		// A scanner error means the stream was not fully readable (for example,
		// a single JSON record exceeded maxSearchLineBytes). Even when earlier
		// matches parsed successfully, returning them as success would silently
		// hide the remainder of the search. Let the caller use the complete
		// bounded Go fallback instead.
		return nil, parseErr
	}
	if len(results) > 0 {
		// Matches already parsed are valid even if rg exited with an error
		// after emitting them (e.g. a later file in the walk hit a permission
		// error). Discarding them would throw away real results for no
		// benefit. Output-limit termination is handled above because those
		// matches are only a partial prefix of the search.
		return results, nil
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1 {
			// Exit code 1 means "ran fine, found nothing" -- not a failure.
			return nil, nil
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("ripgrep: %w: %s", runErr, detail)
		}
		return nil, fmt.Errorf("ripgrep: %w", runErr)
	}

	if parseErr != nil {
		return nil, parseErr
	}

	return results, nil
}

// ripgrepArgs builds the rg invocation. The pattern and search path are
// placed after "--" so a query beginning with '-' can never be misread as a
// flag.
func ripgrepArgs(query string, opts SearchOpts) []string {
	args := []string{
		"--json",
		// Sort in rg before the parser caps results, so repeated queries keep
		// the same files and order instead of depending on worker scheduling.
		"--sort", "path",
		"--max-filesize", ripgrepMaxFileSize,
	}
	if opts.CaseSensitive {
		args = append(args, "--case-sensitive")
	} else {
		args = append(args, "--ignore-case")
	}
	if !opts.Regex {
		// A literal query is passed through --fixed-strings so Go-regex
		// metacharacters typed by the user (e.g. "a.b+c") are matched
		// literally, matching CompilePattern's regexp.QuoteMeta behaviour
		// for the non-regex case.
		args = append(args, "--fixed-strings")
	}
	if opts.WholeWord {
		args = append(args, "--word-regexp")
	}
	// rg respects .gitignore by default, which is a deliberate improvement
	// over the walker's hardcoded blocklist. But .gitignore alone does not
	// guarantee commonSkipDirs are excluded in a repo that doesn't happen to
	// ignore them, so exclude that same fixed list explicitly to keep the
	// guarantee identical between engines regardless of .gitignore content.
	for _, dir := range commonSkipDirs {
		args = append(args, "--glob", "!"+dir+"/")
	}
	// No --hidden: rg's default (skip dotfiles/dot-directories) mirrors the
	// walker's dotfile skip.
	args = append(args, "--", query, ".")
	return args
}

// rgMessage is the subset of ripgrep's --json line schema this package needs.
// See https://docs.rs/grep-printer/latest/grep_printer/struct.JSON.html for
// the full schema; only "match" typed lines carry data we use.
type rgMessage struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
		Submatches []struct {
			Start int `json:"start"`
		} `json:"submatches"`
	} `json:"data"`
}

// parseRipgrepJSON decodes rg --json output (one JSON object per line) into
// Results, taking at most the first submatch per line -- matching
// searchFileContext's use of regexp.FindStringIndex, which likewise reports
// only the first match location per line. Parsing stops once maxResults
// matches have been collected. A malformed or truncated trailing line (e.g.
// output cut off mid-object by the bounded buffer) is tolerated: whatever
// complete match lines came before it are still returned.
func parseRipgrepJSON(data []byte, maxResults int) ([]Result, error) {
	var results []Result

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxSearchLineBytes)

	for scanner.Scan() {
		if len(results) >= maxResults {
			break
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rgMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// A single unparsable line (most often the last one, truncated
			// by the output cap) should not invalidate matches already
			// collected from earlier, well-formed lines. Scanner/stream errors are not
			// tolerated: callers need to know that the result set may be incomplete.
			continue
		}
		if msg.Type != "match" || len(msg.Data.Submatches) == 0 {
			continue
		}
		if msg.Data.Path.Text == "" || msg.Data.LineNumber <= 0 || msg.Data.Submatches[0].Start < 0 {
			// Do not expose malformed external-tool data as a negative or
			// workspace-less editor location. A valid rg match always has a
			// non-empty path, a 1-based line number, and a non-negative byte
			// offset.
			continue
		}

		filePath := filepath.FromSlash(strings.TrimPrefix(msg.Data.Path.Text, "./"))
		preview := strings.TrimSpace(strings.TrimRight(msg.Data.Lines.Text, "\r\n"))

		results = append(results, Result{
			FilePath: filePath,
			Line:     msg.Data.LineNumber - 1, // rg is 1-based; Teak is 0-based
			Col:      msg.Data.Submatches[0].Start,
			Preview:  preview,
		})
	}

	if err := scanner.Err(); err != nil {
		// Preserve already decoded matches for diagnostics/tests, but report the
		// stream error so callers never mistake a partial result set for a
		// complete search.
		return results, err
	}
	return results, nil
}
