package search

import (
	"fmt"
	"regexp"
)

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
