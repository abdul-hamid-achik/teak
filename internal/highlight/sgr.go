package highlight

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
)

// sgrPair holds the escape sequences that surround a style's text.
type sgrPair struct {
	prefix string
	suffix string
	fast   bool
}

// sgrSentinel is a string that cannot occur in source text, used to locate the
// boundary between what a style emits before and after its content.
const sgrSentinel = "\x00\x01teak-sgr\x01\x00"

// sgrProbes are rendered to confirm that a style wraps its text in constant
// sequences. They deliberately vary in length and display width so that any
// style whose output depends on the content — width, alignment, truncation —
// fails the check and is excluded from the fast path.
var sgrProbes = []string{"", "a", "func", "hello world", "áé漢字"}

// deriveSGR extracts the constant prefix and suffix a style wraps text in.
//
// The sequences are read back out of lipgloss rather than reconstructed from
// the style's attributes: reimplementing the mapping from colours and
// attributes to escape codes would silently drift from lipgloss's own output.
// fast is false whenever the extracted pair does not reproduce Render exactly.
func deriveSGR(style lipgloss.Style) sgrPair {
	rendered := style.Render(sgrSentinel)
	i := strings.Index(rendered, sgrSentinel)
	if i < 0 {
		return sgrPair{}
	}
	pair := sgrPair{
		prefix: rendered[:i],
		suffix: rendered[i+len(sgrSentinel):],
		fast:   true,
	}
	for _, probe := range sgrProbes {
		if pair.prefix+probe+pair.suffix != style.Render(probe) {
			return sgrPair{}
		}
	}
	return pair
}

// buildSGRMap derives the escape sequences for every style in the token map
// once, at highlighter construction. lipgloss.Style is not comparable, so this
// is keyed by token type — the same key the styles themselves come from.
func buildSGRMap(styles map[chroma.TokenType]lipgloss.Style) map[chroma.TokenType]sgrPair {
	pairs := make(map[chroma.TokenType]sgrPair, len(styles))
	for tokenType, style := range styles {
		pairs[tokenType] = deriveSGR(style)
	}
	return pairs
}

// newStyledToken builds a token carrying both its style and the precomputed
// escape sequences for it.
func newStyledToken(text string, style lipgloss.Style, pair sgrPair) StyledToken {
	return StyledToken{
		Text:    text,
		Style:   style,
		Prefix:  pair.prefix,
		Suffix:  pair.suffix,
		FastSGR: pair.fast,
	}
}
