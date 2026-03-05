package editor

import "teak/internal/text"

// Bracket pairs: open → close
var bracketPairs = map[byte]byte{
	'(': ')',
	'[': ']',
	'{': '}',
}

// Reverse: close → open
var closeToBracket = map[byte]byte{
	')': '(',
	']': '[',
	'}': '{',
}

// IsOpenBracket returns true if the byte is an opening bracket.
func IsOpenBracket(b byte) bool {
	_, ok := bracketPairs[b]
	return ok
}

// IsCloseBracket returns true if the byte is a closing bracket.
func IsCloseBracket(b byte) bool {
	_, ok := closeToBracket[b]
	return ok
}

// MatchingClose returns the closing bracket for an opening bracket, or 0 if not a bracket.
func MatchingClose(b byte) byte {
	return bracketPairs[b]
}

// AutoClosePair returns the closing bracket to auto-insert for the given character, or 0.
func AutoClosePair(ch byte) byte {
	return bracketPairs[ch]
}

// FindMatchingBracket finds the matching bracket for the bracket at the given position.
// Returns the position of the matching bracket and true, or zero position and false if not found.
func FindMatchingBracket(buf *text.Buffer, pos text.Position) (text.Position, bool) {
	if pos.Line < 0 || pos.Line >= buf.LineCount() {
		return text.Position{}, false
	}
	line := buf.Line(pos.Line)
	if pos.Col < 0 || pos.Col >= len(line) {
		return text.Position{}, false
	}
	ch := line[pos.Col]

	if IsOpenBracket(ch) {
		return findForward(buf, pos, ch, bracketPairs[ch])
	}
	if IsCloseBracket(ch) {
		return findBackward(buf, pos, closeToBracket[ch], ch)
	}
	return text.Position{}, false
}

// findForward searches forward for the matching closing bracket, handling nesting.
func findForward(buf *text.Buffer, pos text.Position, open, close byte) (text.Position, bool) {
	depth := 1
	line := pos.Line
	col := pos.Col + 1
	lineCount := buf.LineCount()

	for line < lineCount {
		content := buf.Line(line)
		for col < len(content) {
			ch := content[col]
			if ch == open {
				depth++
			} else if ch == close {
				depth--
				if depth == 0 {
					return text.Position{Line: line, Col: col}, true
				}
			}
			col++
		}
		line++
		col = 0
	}
	return text.Position{}, false
}

// findBackward searches backward for the matching opening bracket, handling nesting.
func findBackward(buf *text.Buffer, pos text.Position, open, close byte) (text.Position, bool) {
	depth := 1
	line := pos.Line
	col := pos.Col - 1

	for line >= 0 {
		content := buf.Line(line)
		if col < 0 {
			if line == 0 {
				break
			}
			line--
			content = buf.Line(line)
			col = len(content) - 1
			continue
		}
		for col >= 0 {
			ch := content[col]
			if ch == close {
				depth++
			} else if ch == open {
				depth--
				if depth == 0 {
					return text.Position{Line: line, Col: col}, true
				}
			}
			col--
		}
		line--
		if line >= 0 {
			col = len(buf.Line(line)) - 1
		}
	}
	return text.Position{}, false
}

// IsBetweenBrackets checks if cursor is between an empty bracket pair (e.g., "()").
// Returns true if the character before cursor is an open bracket and the character
// at cursor is its matching close bracket.
func IsBetweenBrackets(buf *text.Buffer, cursor text.Position) bool {
	if cursor.Col == 0 {
		return false
	}
	line := buf.Line(cursor.Line)
	if cursor.Col >= len(line) {
		return false
	}
	before := line[cursor.Col-1]
	after := line[cursor.Col]
	return IsOpenBracket(before) && bracketPairs[before] == after
}
