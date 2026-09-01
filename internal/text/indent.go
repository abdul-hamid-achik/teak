package text

import "bytes"

// LeadingWhitespace returns the leading whitespace prefix of a line.
func LeadingWhitespace(line []byte) []byte {
	for i, b := range line {
		if b != ' ' && b != '\t' {
			return line[:i]
		}
	}
	return line
}

// IndentString returns an indent string of tabSize spaces.
func IndentString(tabSize int) []byte {
	return IndentToken(tabSize, false)
}

// IndentToken returns one indentation unit: a tab character when insertTabs is
// set, otherwise tabSize spaces.
func IndentToken(tabSize int, insertTabs bool) []byte {
	if insertTabs {
		return []byte{'\t'}
	}
	if tabSize <= 0 {
		return nil
	}
	return bytes.Repeat([]byte{' '}, tabSize)
}

// Dedent returns the number of leading bytes to remove (up to tabSize spaces).
func Dedent(line []byte, tabSize int) int {
	n := 0
	for _, b := range line {
		if b == ' ' && n < tabSize {
			n++
		} else {
			break
		}
	}
	return n
}
