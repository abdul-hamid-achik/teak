package editor

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

func normalizeTabSize(tabSize int) int {
	if tabSize < 1 {
		return 1
	}
	if tabSize > 8 {
		return 8
	}
	return tabSize
}

func tabWidth(displayCol, tabSize int) int {
	tabSize = normalizeTabSize(tabSize)
	return tabSize - displayCol%tabSize
}

// displayColumn returns the visual column at a raw byte boundary. Columns in
// text.Buffer intentionally remain byte offsets; this conversion belongs only
// to terminal presentation and hit testing.
func displayColumn(raw []byte, byteCol, tabSize int) int {
	return advanceDisplayColumn(raw, 0, byteCol, 0, tabSize)
}

func displayColumnString(raw string, tabSize int) int {
	displayCol := 0
	for _, r := range raw {
		if r == '\t' {
			displayCol += tabWidth(displayCol, tabSize)
			continue
		}
		displayCol += max(0, runewidth.RuneWidth(r))
	}
	return displayCol
}

// advanceDisplayColumn advances from a known raw byte boundary and terminal
// display column. It avoids allocating rendered text and is the primitive used
// by cached wrapped segments.
func advanceDisplayColumn(raw []byte, start, end, displayCol, tabSize int) int {
	start = max(0, min(start, len(raw)))
	end = max(start, min(end, len(raw)))
	for i := start; i < end; {
		if raw[i] == '\t' {
			displayCol += tabWidth(displayCol, tabSize)
			i++
			continue
		}
		r, size := utf8.DecodeRune(raw[i:])
		if size <= 0 {
			break
		}
		displayCol += max(0, runewidth.RuneWidth(r))
		i += size
	}
	return displayCol
}

// expandTabsForDisplay converts tabs to spaces at terminal tab stops without
// altering the source bytes.
func expandTabsForDisplay(raw []byte, tabSize int) string {
	return expandTabsAtDisplayColumn(string(raw), 0, tabSize)
}

func expandTabsAtDisplayColumn(raw string, displayCol, tabSize int) string {
	expanded, _ := expandTabsAtDisplayColumnWithEnd(raw, displayCol, tabSize)
	return expanded
}

// expandTabsAtDisplayColumnWithEnd returns both the display-ready text and the
// absolute display column immediately after it. Wrapped rendering threads this
// value across token/selection slices, avoiding a scan from the beginning of a
// very long logical line for each slice.
func expandTabsAtDisplayColumnWithEnd(raw string, displayCol, tabSize int) (string, int) {
	if !strings.ContainsRune(raw, '\t') {
		for _, r := range raw {
			displayCol += max(0, runewidth.RuneWidth(r))
		}
		return raw, displayCol
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if r == '\t' {
			width := tabWidth(displayCol, tabSize)
			b.WriteString(getSpaces(width))
			displayCol += width
			continue
		}
		b.WriteRune(r)
		displayCol += max(0, runewidth.RuneWidth(r))
	}
	return b.String(), displayCol
}

// byteColumnAtDisplay maps a terminal display column back to a valid raw byte
// boundary. A tab cannot contain a cursor position; its first half maps before
// it and its latter half maps after it.
func byteColumnAtDisplay(raw []byte, displayCol, tabSize int) int {
	if displayCol <= 0 {
		return 0
	}
	col := 0
	for i := 0; i < len(raw); {
		start := col
		size := 1
		width := 0
		if raw[i] == '\t' {
			width = tabWidth(col, tabSize)
		} else {
			r, decoded := utf8.DecodeRune(raw[i:])
			if decoded <= 0 {
				break
			}
			size = decoded
			width = max(0, runewidth.RuneWidth(r))
		}
		end := start + width
		if displayCol < end {
			if raw[i] == '\t' && displayCol-start > width/2 {
				return i + 1
			}
			return i
		}
		if displayCol == end {
			return i + size
		}
		col = end
		i += size
	}
	return len(raw)
}

func byteColumnAtDisplayString(raw string, displayCol, tabSize int) int {
	if displayCol <= 0 {
		return 0
	}
	col := 0
	for i := 0; i < len(raw); {
		start := col
		r, size := utf8.DecodeRuneInString(raw[i:])
		width := max(0, runewidth.RuneWidth(r))
		if raw[i] == '\t' {
			size = 1
			width = tabWidth(col, tabSize)
		}
		end := start + width
		if displayCol < end {
			if raw[i] == '\t' && displayCol-start > width/2 {
				return i + 1
			}
			return i
		}
		if displayCol == end {
			return i + size
		}
		col = end
		i += size
	}
	return len(raw)
}

// byteColumnAtDisplayFrom is the bounded counterpart used by a wrapped
// segment. start must be a valid byte boundary and startDisplay the terminal
// column at that boundary.
func byteColumnAtDisplayFrom(raw []byte, start, startDisplay, target, tabSize int) int {
	start = max(0, min(start, len(raw)))
	if target <= startDisplay {
		return start
	}
	col := startDisplay
	for i := start; i < len(raw); {
		before := col
		size := 1
		width := 0
		if raw[i] == '\t' {
			width = tabWidth(col, tabSize)
		} else {
			r, decoded := utf8.DecodeRune(raw[i:])
			if decoded < 1 {
				decoded = 1
			}
			size = decoded
			width = max(0, runewidth.RuneWidth(r))
		}
		end := before + width
		if target < end {
			if raw[i] == '\t' && target-before > width/2 {
				return i + 1
			}
			return i
		}
		if target == end {
			return i + size
		}
		col = end
		i += size
	}
	return len(raw)
}
