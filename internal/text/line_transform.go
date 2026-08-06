package text

import (
	"context"
	"errors"
)

// LineTransform identifies a non-local line operation. The preparation
// function works only with immutable rope snapshots, so callers can run it in
// a tea.Cmd without making the Update loop scan or materialize a logical line.
type LineTransform uint8

const (
	LineTransformMoveUp LineTransform = iota
	LineTransformMoveDown
	LineTransformDuplicateUp
	LineTransformDuplicateDown
	LineTransformDelete
)

// PreparedLineTransform is the complete immutable result of a line command.
// Selections and Primary describe the same post-edit rope and are installed by
// Buffer only after the caller has confirmed its snapshot is still current.
type PreparedLineTransform struct {
	Rope       *Rope
	Selections []Selection
	Primary    int
	Changed    bool
}

// PrepareLineTransform applies a line operation to a rope snapshot. It
// preserves independent cursor blocks for duplication, merges adjacent blocks
// for movement/deletion, and never constructs a contiguous copy of the line
// being moved or duplicated. The context is checked between structural steps;
// stale editor requests can therefore be discarded without installing a late
// result.
func PrepareLineTransform(ctx context.Context, source *Rope, selections []Selection, primary int, transform LineTransform) (PreparedLineTransform, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return PreparedLineTransform{}, err
	}
	if source == nil {
		source = New(nil)
	}

	selectionSet := newLineTransformSelections(selections, primary)
	normalized := append([]Selection(nil), selectionSet.All()...)
	result := PreparedLineTransform{
		Rope:       source,
		Selections: normalized,
		Primary:    selectionSet.PrimaryIndex(),
	}

	var ranges []selectedLineRange
	mergeAdjacent := true
	switch transform {
	case LineTransformDuplicateUp, LineTransformDuplicateDown:
		mergeAdjacent = false
	case LineTransformMoveUp, LineTransformMoveDown, LineTransformDelete:
		mergeAdjacent = true
	default:
		return PreparedLineTransform{}, errors.New("unknown line transform")
	}
	view := &Buffer{rope: source, Cursor: selectionSet.PrimaryCursor(), Selections: selectionSet}
	ranges = view.selectedLineRanges(mergeAdjacent)
	if len(ranges) == 0 {
		return result, nil
	}

	switch transform {
	case LineTransformMoveUp, LineTransformMoveDown:
		return prepareMoveLine(ctx, source, normalized, selectionSet.PrimaryIndex(), ranges, transform == LineTransformMoveDown)
	case LineTransformDuplicateUp, LineTransformDuplicateDown:
		return prepareDuplicateLine(ctx, source, normalized, selectionSet.PrimaryIndex(), ranges, transform == LineTransformDuplicateDown)
	case LineTransformDelete:
		return prepareDeleteLines(ctx, source, normalized, selectionSet.PrimaryIndex(), ranges)
	default:
		return PreparedLineTransform{}, errors.New("unknown line transform")
	}
}

func newLineTransformSelections(selections []Selection, primary int) *Selections {
	if len(selections) == 0 {
		selections = []Selection{{}}
	}
	cloned := append([]Selection(nil), selections...)
	if primary < 0 || primary >= len(cloned) {
		primary = 0
	}
	set := &Selections{selections: cloned, primary: primary, dirty: true}
	set.Normalize()
	return set
}

func selectionLineRange(rope *Rope, selection Selection) selectedLineRange {
	lastLine := max(0, rope.LineCount()-1)
	start, end := selection.Ordered()
	startLine := min(lastLine, max(0, start.Line))
	endLine := min(lastLine, max(startLine, end.Line))
	if !selection.IsEmpty() && end.Col == 0 && endLine > startLine {
		endLine--
	}
	return selectedLineRange{start: startLine, end: endLine}
}

func lineRangeContains(outer, inner selectedLineRange) bool {
	return inner.start >= outer.start && inner.end <= outer.end
}

func positionLineShift(pos Position, delta int) Position {
	pos.Line += delta
	return pos
}

func moveSelectionsIntoBlock(rope *Rope, selections []Selection, block selectedLineRange, delta int) []Selection {
	result := append([]Selection(nil), selections...)
	for i, selection := range result {
		if lineRangeContains(block, selectionLineRange(rope, selection)) {
			selection.Anchor = positionLineShift(selection.Anchor, delta)
			selection.Head = positionLineShift(selection.Head, delta)
			result[i] = selection
		}
	}
	return result
}

func prepareMoveLine(ctx context.Context, source *Rope, selections []Selection, primary int, ranges []selectedLineRange, down bool) (PreparedLineTransform, error) {
	working := source
	resultSelections := append([]Selection(nil), selections...)
	lastLine := source.LineCount() - 1
	changed := false
	if down {
		for i := len(ranges) - 1; i >= 0; i-- {
			if err := ctx.Err(); err != nil {
				return PreparedLineTransform{}, err
			}
			block := ranges[i]
			if block.end >= lastLine {
				continue
			}
			working = swapLineBlock(working, block, true)
			resultSelections = moveSelectionsIntoBlock(source, resultSelections, block, 1)
			changed = true
		}
	} else {
		for _, block := range ranges {
			if err := ctx.Err(); err != nil {
				return PreparedLineTransform{}, err
			}
			if block.start == 0 {
				continue
			}
			working = swapLineBlock(working, block, false)
			resultSelections = moveSelectionsIntoBlock(source, resultSelections, block, -1)
			changed = true
		}
	}
	return finalizeLineTransform(working, resultSelections, primary, changed), nil
}

// swapLineBlock swaps a block with its immediate neighbour. The ranges refer
// to line numbers in the current rope; line count is unchanged by the swap.
func swapLineBlock(rope *Rope, block selectedLineRange, down bool) *Rope {
	if down {
		blockStart := rope.LineStart(block.start)
		blockEnd := rope.LineStart(block.end) + rope.LineLen(block.end)
		belowStart := rope.LineStart(block.end + 1)
		belowEnd := belowStart + rope.LineLen(block.end+1)
		return concatLineRopes(
			rope.Slice(0, blockStart),
			rope.Slice(belowStart, belowEnd),
			New([]byte{'\n'}),
			rope.Slice(blockStart, blockEnd),
			rope.Slice(belowEnd, rope.Len()),
		)
	}
	aboveStart := rope.LineStart(block.start - 1)
	aboveContentEnd := aboveStart + rope.LineLen(block.start-1)
	blockStart := rope.LineStart(block.start)
	blockEnd := rope.LineStart(block.end) + rope.LineLen(block.end)
	return concatLineRopes(
		rope.Slice(0, aboveStart),
		rope.Slice(blockStart, blockEnd),
		New([]byte{'\n'}),
		rope.Slice(aboveStart, aboveContentEnd),
		rope.Slice(blockEnd, rope.Len()),
	)
}

func prepareDuplicateLine(ctx context.Context, source *Rope, selections []Selection, primary int, ranges []selectedLineRange, down bool) (PreparedLineTransform, error) {
	working := source
	resultSelections := append([]Selection(nil), selections...)
	for i := len(ranges) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return PreparedLineTransform{}, err
		}
		block := ranges[i]
		blockStart := source.LineStart(block.start)
		blockEnd := source.LineStart(block.end) + source.LineLen(block.end)
		content := source.Slice(blockStart, blockEnd)
		var insertion *Rope
		var offset int
		if down {
			insertion = concatLineRopes(New([]byte{'\n'}), content)
			offset = blockEnd
		} else {
			insertion = concatLineRopes(content, New([]byte{'\n'}))
			offset = blockStart
		}
		working = insertLineRope(working, insertion, offset)

		shift := 0
		for _, previous := range ranges[:i] {
			shift += previous.end - previous.start + 1
		}
		if down {
			shift += block.end - block.start + 1
		}
		for selectionIndex, selection := range resultSelections {
			if lineRangeContains(block, selectionLineRange(source, selection)) {
				selection.Anchor = positionLineShift(selection.Anchor, shift)
				selection.Head = positionLineShift(selection.Head, shift)
				resultSelections[selectionIndex] = selection
			}
		}
	}
	return finalizeLineTransform(working, resultSelections, primary, true), nil
}

func prepareDeleteLines(ctx context.Context, source *Rope, selections []Selection, primary int, ranges []selectedLineRange) (PreparedLineTransform, error) {
	lastLine := source.LineCount() - 1
	working := source
	for i := len(ranges) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return PreparedLineTransform{}, err
		}
		block := ranges[i]
		start := source.LineStart(block.start)
		end := source.Len()
		if block.end < lastLine {
			end = source.LineStart(block.end + 1)
		} else if block.start > 0 {
			start-- // remove the newline joining the last surviving line
		}
		working = working.Delete(start, end-start)
	}

	resultSelections := make([]Selection, len(selections))
	for i, selection := range selections {
		selected := selectionLineRange(source, selection)
		blockIndex := -1
		for j, block := range ranges {
			if lineRangeContains(block, selected) {
				blockIndex = j
				break
			}
		}
		if blockIndex < 0 {
			resultSelections[i] = selection
			continue
		}
		block := ranges[blockIndex]
		removedBefore := 0
		for _, previous := range ranges[:blockIndex] {
			removedBefore += previous.end - previous.start + 1
		}
		targetLine := block.start - removedBefore
		if block.end == lastLine {
			targetLine--
		}
		targetLine = max(0, targetLine)
		col := selection.Head.Col
		resultSelections[i] = Selection{
			Anchor: Position{Line: targetLine, Col: col},
			Head:   Position{Line: targetLine, Col: col},
		}
	}
	return finalizeLineTransform(working, resultSelections, primary, working != source), nil
}

func insertLineRope(rope, insertion *Rope, offset int) *Rope {
	return concatLineRopes(rope.Slice(0, offset), insertion, rope.Slice(offset, rope.Len()))
}

func concatLineRopes(parts ...*Rope) *Rope {
	result := New(nil)
	for _, part := range parts {
		if part != nil && part.Len() > 0 {
			result = join(result, part)
		}
	}
	return result
}

func finalizeLineTransform(rope *Rope, selections []Selection, primary int, changed bool) PreparedLineTransform {
	set := &Selections{selections: selections, primary: primary, dirty: true}
	set.Normalize()
	clamp := &Buffer{rope: rope}
	for i, selection := range set.selections {
		set.selections[i] = Selection{
			Anchor: clamp.ClampPosition(selection.Anchor),
			Head:   clamp.ClampPosition(selection.Head),
		}
	}
	set.dirty = true
	set.Normalize()
	return PreparedLineTransform{
		Rope:       rope,
		Selections: append([]Selection(nil), set.All()...),
		Primary:    set.PrimaryIndex(),
		Changed:    changed,
	}
}

func (b *Buffer) applyLineTransform(transform LineTransform) {
	selections := []Selection{{Anchor: b.Cursor, Head: b.Cursor}}
	primary := 0
	if b.Selections != nil && b.Selections.Count() > 0 {
		b.Selections.Normalize()
		// Keep compatibility with callers that historically assigned Cursor
		// directly. A lone selection has no secondary state to preserve, so its
		// head follows the explicitly supplied cursor before preparation.
		if b.Selections.Count() == 1 && b.Selections.PrimaryCursor() != b.Cursor {
			b.Selections.selections[b.Selections.primary] = Selection{Anchor: b.Cursor, Head: b.Cursor}
		}
		selections = append([]Selection(nil), b.Selections.All()...)
		primary = b.Selections.PrimaryIndex()
	}
	prepared, err := PrepareLineTransform(context.Background(), b.rope, selections, primary, transform)
	if err != nil || !prepared.Changed {
		return
	}
	b.undo.Save(b.rope, b.Cursor, false)
	b.rope = prepared.Rope
	b.Selections = &Selections{selections: append([]Selection(nil), prepared.Selections...), primary: prepared.Primary}
	b.Cursor = b.Selections.PrimaryCursor()
	b.dirty = true
	b.version++
	b.lastChange = nil
}
