package editor

import "teak/internal/text"

// MaxBracketScanBytes is the deterministic per-frame budget used by the
// viewport. Unmatched brackets in generated or minified files must not make a
// render walk the entire document.
const MaxBracketScanBytes = 64 << 10

const bracketScanChunkBytes = 4 << 10

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
func MatchingClose(b byte) byte { return bracketPairs[b] }

// AutoClosePair returns the closing bracket to auto-insert for the given character, or 0.
func AutoClosePair(ch byte) byte { return bracketPairs[ch] }

// FindMatchingBracket finds a matching bracket without a scan limit. It is
// retained for editing callers and tests; the viewport uses the bounded form.
func FindMatchingBracket(buf *text.Buffer, pos text.Position) (text.Position, bool) {
	return findMatchingBracket(buf, pos, -1)
}

// FindMatchingBracketWithinBudget finds a matching bracket while examining at
// most budget bytes after the bracket itself. It never materializes a whole
// Rope line or document; a budget miss deliberately degrades to no highlight.
func FindMatchingBracketWithinBudget(buf *text.Buffer, pos text.Position, budget int) (text.Position, bool) {
	if budget <= 0 {
		return text.Position{}, false
	}
	return findMatchingBracket(buf, pos, budget)
}

func findMatchingBracket(buf *text.Buffer, pos text.Position, budget int) (text.Position, bool) {
	if buf == nil || buf.Rope() == nil {
		return text.Position{}, false
	}
	rope := buf.Rope()
	offset, ok := rope.PositionToOffsetUncached(pos)
	if !ok {
		return text.Position{}, false
	}
	ch, ok := rope.ByteAtSafe(offset)
	if !ok {
		return text.Position{}, false
	}
	if IsOpenBracket(ch) {
		return findForward(rope, offset, ch, bracketPairs[ch], budget)
	}
	if IsCloseBracket(ch) {
		return findBackward(rope, offset, closeToBracket[ch], ch, budget)
	}
	return text.Position{}, false
}

// findForward scans bounded Rope chunks into one reusable buffer. An unlimited
// budget is used only by FindMatchingBracket.
func findForward(rope *text.Rope, offset int, open, close byte, budget int) (text.Position, bool) {
	depth := 1
	start := offset + 1
	end := rope.Len()
	var storage [bracketScanChunkBytes]byte
	if budget >= 0 {
		end = min(end, start+budget)
	}
	for start < end {
		chunkEnd := min(end, start+bracketScanChunkBytes)
		chunk := storage[:chunkEnd-start]
		n, _ := rope.ReadAt(chunk, int64(start))
		chunk = chunk[:n]
		for i, ch := range chunk {
			switch ch {
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return rope.OffsetToPosition(start + i), true
				}
			}
		}
		if n == 0 {
			break
		}
		start += n
	}
	return text.Position{}, false
}

func findBackward(rope *text.Rope, offset int, open, close byte, budget int) (text.Position, bool) {
	depth := 1
	start := 0
	var storage [bracketScanChunkBytes]byte
	if budget >= 0 {
		start = max(0, offset-budget)
	}
	end := offset
	for end > start {
		chunkStart := max(start, end-bracketScanChunkBytes)
		chunk := storage[:end-chunkStart]
		n, _ := rope.ReadAt(chunk, int64(chunkStart))
		chunk = chunk[:n]
		for i := len(chunk) - 1; i >= 0; i-- {
			switch chunk[i] {
			case close:
				depth++
			case open:
				depth--
				if depth == 0 {
					return rope.OffsetToPosition(chunkStart + i), true
				}
			}
		}
		if n == 0 {
			break
		}
		end = chunkStart
	}
	return text.Position{}, false
}

// IsBetweenBrackets checks if cursor is between an empty bracket pair (e.g., "()").
func IsBetweenBrackets(buf *text.Buffer, cursor text.Position) bool {
	if buf == nil || cursor.Col == 0 {
		return false
	}
	rope := buf.Rope()
	beforeOffset, ok := rope.PositionToOffsetUncached(text.Position{Line: cursor.Line, Col: cursor.Col - 1})
	if !ok {
		return false
	}
	afterOffset, ok := rope.PositionToOffsetUncached(cursor)
	if !ok {
		return false
	}
	before, ok := rope.ByteAtSafe(beforeOffset)
	if !ok {
		return false
	}
	after, ok := rope.ByteAtSafe(afterOffset)
	return ok && IsOpenBracket(before) && bracketPairs[before] == after
}

func selectionOffsets(rope *text.Rope, selection text.Selection) (int, int, bool) {
	start, end := selection.Ordered()
	startOffset, startOK := rope.PositionToOffsetUncached(start)
	endOffset, endOK := rope.PositionToOffsetUncached(end)
	return startOffset, endOffset, startOK && endOK && endOffset >= startOffset
}

func autoCloseSelectionEdits(buf *text.Buffer, open, close byte) ([]text.EditOp, bool) {
	if buf == nil || buf.Selections == nil || buf.Selections.Count() == 0 {
		return nil, false
	}
	buf.Selections.Normalize()
	rope := buf.Rope()
	edits := make([]text.EditOp, buf.Selections.Count())
	for i, selection := range buf.Selections.All() {
		start, end, ok := selectionOffsets(rope, selection)
		if !ok {
			return nil, false
		}
		edits[i] = text.EditOp{
			Offset: start,
			Delete: end - start,
			Insert: []byte{open, close},
			Cursor: start + 1,
		}
	}
	return edits, true
}

func closingBracketSelectionEdits(buf *text.Buffer, close byte) ([]text.EditOp, bool) {
	if buf == nil || buf.Selections == nil || buf.Selections.Count() == 0 {
		return nil, false
	}
	buf.Selections.Normalize()
	rope := buf.Rope()
	edits := make([]text.EditOp, buf.Selections.Count())
	for i, selection := range buf.Selections.All() {
		start, end, ok := selectionOffsets(rope, selection)
		if !ok {
			return nil, false
		}
		if !selection.IsEmpty() {
			edits[i] = text.EditOp{
				Offset: start,
				Delete: end - start,
				Insert: []byte{close},
				Cursor: start + 1,
			}
			continue
		}
		edits[i] = text.EditOp{Offset: start, Cursor: start + 1}
		if next, exists := rope.ByteAtSafe(start); !exists || next != close {
			edits[i].Insert = []byte{close}
		}
	}
	return edits, true
}

func backspaceSelectionEdits(buf *text.Buffer) ([]text.EditOp, bool) {
	if buf == nil || buf.Selections == nil || buf.Selections.Count() == 0 {
		return nil, false
	}
	buf.Selections.Normalize()
	rope := buf.Rope()
	edits := make([]text.EditOp, buf.Selections.Count())
	for i, selection := range buf.Selections.All() {
		start, end, ok := selectionOffsets(rope, selection)
		if !ok {
			return nil, false
		}
		if !selection.IsEmpty() {
			edits[i] = text.EditOp{Offset: start, Delete: end - start, Cursor: start}
			continue
		}
		if start == 0 {
			edits[i] = text.EditOp{Offset: start, Cursor: start}
			continue
		}
		before, beforeOK := rope.ByteAtSafe(start - 1)
		after, afterOK := rope.ByteAtSafe(start)
		if beforeOK && afterOK && IsOpenBracket(before) && bracketPairs[before] == after {
			edits[i] = text.EditOp{Offset: start - 1, Delete: 2, Cursor: start - 1}
			continue
		}
		_, size, exists := rope.RuneBefore(start)
		if !exists || size <= 0 {
			edits[i] = text.EditOp{Offset: start, Cursor: start}
			continue
		}
		edits[i] = text.EditOp{Offset: start - size, Delete: size, Cursor: start - size}
	}
	return edits, true
}
