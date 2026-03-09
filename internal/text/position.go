package text

import "sort"

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

// Selections manages multiple selections with a primary selection.
// All selections are kept sorted by start position and non-overlapping.
type Selections struct {
	selections []Selection
	primary    int  // Index of primary selection (receives focus)
	dirty      bool // Marks if normalization is needed
}

// NewSelections creates a Selections with a single cursor.
func NewSelections(cursor Position) *Selections {
	return &Selections{
		selections: []Selection{{Anchor: cursor, Head: cursor}},
		primary:    0,
	}
}

// Primary returns the primary selection.
func (s *Selections) Primary() Selection {
	return s.selections[s.primary]
}

// PrimaryCursor returns the primary cursor position (for backward compatibility).
func (s *Selections) PrimaryCursor() Position {
	return s.selections[s.primary].Head
}

// All returns all selections.
func (s *Selections) All() []Selection {
	return s.selections
}

// Count returns the number of selections.
func (s *Selections) Count() int {
	return len(s.selections)
}

// SetPrimary sets which selection is primary.
func (s *Selections) SetPrimary(idx int) {
	if idx >= 0 && idx < len(s.selections) {
		s.primary = idx
	}
}

// Add adds a new selection and makes it primary.
func (s *Selections) Add(sel Selection) {
	const MaxSelections = 1000
	if len(s.selections) >= MaxSelections {
		return // Prevent excessive selections
	}
	s.selections = append(s.selections, sel)
	s.primary = len(s.selections) - 1
	s.dirty = true
}

// Clear removes all but the primary selection.
func (s *Selections) Clear() {
	if len(s.selections) > 1 {
		primary := s.selections[s.primary]
		s.selections = []Selection{primary}
		s.primary = 0
		s.dirty = false
	}
}

// normalize sorts selections and removes overlaps (internal use).
func (s *Selections) normalize() {
	if !s.dirty || len(s.selections) <= 1 {
		return
	}

	// Sort by start position
	sort.Slice(s.selections, func(i, j int) bool {
		si, _ := s.selections[i].Ordered()
		sj, _ := s.selections[j].Ordered()
		if si.Line != sj.Line {
			return si.Line < sj.Line
		}
		return si.Col < sj.Col
	})

	// Remove overlapping selections
	normalized := make([]Selection, 0, len(s.selections))
	oldPrimaryIdx := s.primary
	var lastEnd Position

	for i, sel := range s.selections {
		start, end := sel.Ordered()
		if i == 0 || start.Line > lastEnd.Line ||
			(start.Line == lastEnd.Line && start.Col > lastEnd.Col) {
			normalized = append(normalized, sel)
			lastEnd = end
			if i == oldPrimaryIdx {
				s.primary = len(normalized) - 1
			}
		}
	}

	s.selections = normalized
	s.dirty = false
}

// Normalize ensures selections are sorted and non-overlapping.
func (s *Selections) Normalize() {
	s.normalize()
}
