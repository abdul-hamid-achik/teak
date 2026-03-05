package text

// Position represents a 0-based line and column in a text document.
type Position struct {
	Line int
	Col  int
}

// ByteOffset is an absolute byte offset into the document.
type ByteOffset = int

// Selection represents a selected range of text between Anchor and Head.
// Anchor is where the selection started, Head is the current cursor end.
type Selection struct {
	Anchor Position
	Head   Position
}

// Ordered returns the selection positions in document order (start, end).
func (s Selection) Ordered() (Position, Position) {
	if s.Anchor.Line < s.Head.Line || (s.Anchor.Line == s.Head.Line && s.Anchor.Col <= s.Head.Col) {
		return s.Anchor, s.Head
	}
	return s.Head, s.Anchor
}

// IsEmpty returns true if the selection has zero width.
func (s Selection) IsEmpty() bool {
	return s.Anchor == s.Head
}

// EditOp represents a single atomic edit operation.
type EditOp struct {
	Offset ByteOffset
	Delete int
	Insert []byte
}
