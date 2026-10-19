package search

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxPreviewBytes bounds the preview text kept for one result. A match on a
// minified or generated line can otherwise copy the whole line into every
// result.
const maxPreviewBytes = 1024

// previewAround returns the display preview for a line whose match starts at
// byte offset start. Short lines are returned trimmed. Longer lines are cut to
// a window of about maxPreviewBytes that contains the match, with "…" marking
// each side that was cut. The window is snapped to UTF-8 rune boundaries.
func previewAround(line string, start int) string {
	line = strings.TrimRight(line, "\r\n")
	lo, hi := 0, len(line)
	if len(line) > maxPreviewBytes {
		start = min(max(start, 0), len(line))
		lo = max(start-maxPreviewBytes/4, 0)
		hi = lo + maxPreviewBytes
		if hi > len(line) {
			hi = len(line)
			lo = max(hi-maxPreviewBytes, 0)
		}
		for lo > 0 && !utf8.RuneStart(line[lo]) {
			lo--
		}
		for hi < len(line) && !utf8.RuneStart(line[hi]) {
			hi--
		}
	}
	text := strings.TrimSpace(sanitizePreview(line[lo:hi]))
	if lo > 0 {
		text = "…" + text
	}
	if hi < len(line) {
		text += "…"
	}
	return text
}

// sanitizePreview replaces control characters (ESC, tab, CR, DEL and C1
// controls) with spaces. File content is untrusted here: a raw escape sequence
// in a preview would be interpreted by the terminal instead of drawn as text.
func sanitizePreview(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}

// SearchOpts controls pattern compilation for search and replace.
type SearchOpts struct {
	Regex         bool
	CaseSensitive bool
	WholeWord     bool
}

// CompilePattern compiles a search query into a regexp using the given options.
// When Regex is false, the query is literal-escaped. When CaseSensitive is false,
// the pattern is prefixed with (?i) for case-insensitive matching. WholeWord
// wraps a literal query in word boundaries so "cat" does not match "category".
func CompilePattern(query string, opts SearchOpts) (*regexp.Regexp, error) {
	pattern := query
	if !opts.Regex {
		pattern = regexp.QuoteMeta(query)
		if opts.WholeWord {
			pattern = `\b` + pattern + `\b`
		}
	}
	prefix := "(?i)"
	if opts.CaseSensitive {
		prefix = ""
	}
	re, err := regexp.Compile(prefix + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	return re, nil
}
