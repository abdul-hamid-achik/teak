package text

import "sort"

// MaxSelections bounds the work performed by multi-cursor commands and keeps a
// malformed or overly broad selection operation from exhausting the UI.
const MaxSelections = 1000

// MaxOccurrenceSearchBytes bounds the synchronous multi-cursor occurrence
// shortcuts. Full search/replace is asynchronous at the app layer; these
// interactive selection helpers intentionally stay small enough for one frame.
const MaxOccurrenceSearchBytes = 2 << 20

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

// EditOp represents one replacement in a set of atomic selection edits.
// Offset and Delete address the original document. Cursor is an absolute byte
// offset in the document after this replacement alone; edits before it are
// rebased by Buffer.ApplySelectionEdits.
type EditOp struct {
	Offset ByteOffset
	Delete int
	Insert []byte
	Cursor ByteOffset
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
	if len(s.selections) == 0 {
		return Selection{}
	}
	return s.selections[s.primary]
}

// PrimaryCursor returns the primary cursor position (for backward compatibility).
func (s *Selections) PrimaryCursor() Position {
	if len(s.selections) == 0 {
		return Position{}
	}
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

// PrimaryIndex returns the index receiving cursor focus. It is useful when a
// bounded background edit snapshots multi-cursor state and must restore it
// atomically with its immutable rope result.
func (s *Selections) PrimaryIndex() int {
	if s == nil || s.primary < 0 || s.primary >= len(s.selections) {
		return 0
	}
	return s.primary
}

// SetPrimary sets which selection is primary.
func (s *Selections) SetPrimary(idx int) {
	if idx >= 0 && idx < len(s.selections) {
		s.primary = idx
	}
}

// Add adds a new selection and makes it primary.
func (s *Selections) Add(sel Selection) {
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

	type indexedSelection struct {
		selection Selection
		primary   bool
	}

	indexed := make([]indexedSelection, len(s.selections))
	for i, selection := range s.selections {
		indexed[i] = indexedSelection{selection: selection, primary: i == s.primary}
	}

	// Sort by start position. Stable ordering makes the first selection at a
	// position canonical when callers accidentally create duplicate ranges.
	sort.SliceStable(indexed, func(i, j int) bool {
		si, _ := indexed[i].selection.Ordered()
		sj, _ := indexed[j].selection.Ordered()
		if si.Line != sj.Line {
			return si.Line < sj.Line
		}
		if si.Col != sj.Col {
			return si.Col < sj.Col
		}
		// A range owns its half-open span. Sort it before a cursor at the
		// same start so the sweep below can coalesce that cursor regardless
		// of the order in which callers added the selections.
		return !indexed[i].selection.IsEmpty() && indexed[j].selection.IsEmpty()
	})

	// Selections use half-open ranges: [start, end). Thus adjacent ranges are
	// distinct (end == next start), while a later range or cursor starting before
	// the end of a retained range overlaps it. Collapsed cursors outside ranges
	// may coexist with selected text, while interior and duplicate cursors are
	// coalesced. This guarantees that one edit per normalized selection never
	// addresses competing bytes.
	// When a primary selection is coalesced, focus transfers to its canonical
	// retained selection; this keeps primary valid after normalization.
	normalized := make([]Selection, 0, len(s.selections))
	primary := 0
	lastRange := -1
	var lastRangeEnd Position
	lastCursor := -1

	for _, item := range indexed {
		start, end := item.selection.Ordered()
		if start == end {
			if lastRange >= 0 && positionLess(start, lastRangeEnd) {
				if item.primary {
					primary = lastRange
				}
				continue
			}
			if lastCursor >= 0 && normalized[lastCursor].Anchor == start && normalized[lastCursor].Head == start {
				if item.primary {
					primary = lastCursor
				}
				continue
			}
			normalized = append(normalized, item.selection)
			lastCursor = len(normalized) - 1
			if item.primary {
				primary = len(normalized) - 1
			}
			continue
		}

		if lastRange >= 0 && positionLess(start, lastRangeEnd) {
			if item.primary {
				primary = lastRange
			}
			continue
		}

		normalized = append(normalized, item.selection)
		lastRange = len(normalized) - 1
		lastRangeEnd = end
		if item.primary {
			primary = lastRange
		}
	}

	s.selections = normalized
	s.primary = primary
	s.dirty = false
}

func positionLess(a, b Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Col < b.Col)
}

// Normalize ensures selections are sorted and non-overlapping.
func (s *Selections) Normalize() {
	s.normalize()
}

// SelectionLineIterator walks normalized selections for monotonically
// increasing buffer lines. It returns subslices of the immutable-for-the-frame
// selection backing array, so viewport rendering does not copy or re-sort up
// to MaxSelections for every physical terminal row. Calling ForLine again for
// the same logical line is supported for word-wrapped segments.
//
// A fresh iterator must be used after changing selections. This is deliberate:
// input handlers already own selection mutation, while a render frame only
// needs a short-lived, allocation-free view of that current state.
type SelectionLineIterator struct {
	selections []Selection
	next       int
	lastLine   int
	lastStart  int
	lastEnd    int
}

// LineIterator returns an iterator over this selection set. Normalization is
// deferred until here so commands which add many cursors only sort once; after
// that, the viewport can sweep visible lines in O(S+H), where S is the number
// of selections reached and H the number of visible logical lines.
func (s *Selections) LineIterator() *SelectionLineIterator {
	if s == nil {
		return &SelectionLineIterator{lastLine: -1}
	}
	s.normalize()
	return &SelectionLineIterator{
		selections: s.selections,
		lastLine:   -1,
	}
}

// ForLine returns selections that can overlap line. The returned slice aliases
// the selection set and is valid until the selections change. Empty selections
// are included so callers that render cursor state can decide how to handle
// them; text selection rendering normally ignores them.
func (it *SelectionLineIterator) ForLine(line int) []Selection {
	if it == nil || len(it.selections) == 0 {
		return nil
	}
	if line == it.lastLine {
		return it.selections[it.lastStart:it.lastEnd]
	}
	if line < it.lastLine {
		// Renderers are monotonic, but resetting here keeps this small API safe
		// for diagnostics and focused unit callers without retaining stale state.
		it.next = 0
	}

	i := it.next
	for i < len(it.selections) {
		_, end := it.selections[i].Ordered()
		if end.Line >= line {
			break
		}
		i++
	}
	start := i
	for i < len(it.selections) {
		selectionStart, selectionEnd := it.selections[i].Ordered()
		if selectionStart.Line > line {
			break
		}
		i++
		if selectionEnd.Line > line {
			// Non-overlapping selections are ordered by start position, so every
			// following selection starts after this one ends and cannot be on this
			// line. Keep this selection as the next active one for the next row.
			break
		}
	}

	it.lastLine = line
	it.lastStart = start
	it.lastEnd = i
	if i > start {
		_, end := it.selections[i-1].Ordered()
		if end.Line > line {
			it.next = i - 1
		} else {
			it.next = i
		}
	} else {
		it.next = start
	}
	return it.selections[start:i]
}
