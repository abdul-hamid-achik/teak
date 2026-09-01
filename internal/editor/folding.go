package editor

import "sort"

// FoldRegion represents a foldable range in the document.
type FoldRegion struct {
	StartLine int
	EndLine   int
	Collapsed bool
}

type lineInterval struct {
	start int // inclusive
	end   int // inclusive
}

// foldIndex is a derived, immutable view of the current regions. Collapsed
// ranges are merged so every render can skip a whole hidden span instead of
// asking every region about every document line.
type foldIndex struct {
	regionsFirst *FoldRegion
	regionsLen   int
	hidden       []lineInterval
	hiddenPrefix []int       // cumulative hidden lengths, one entry per interval
	starts       map[int]int // first region index for a start line
}

// FoldState manages code folding state for an editor.
type FoldState struct {
	// Regions is replaced through SetRegions and its collapsed state through
	// Toggle/Fold/Unfold. Replacing the slice directly is detected on the next
	// query for compatibility; callers should use the methods so the derived
	// index is rebuilt outside the render path.
	Regions []FoldRegion

	// index is invalidated by all mutation APIs. The exported Regions field is
	// retained for compatibility; ensureIndex also notices replacement slices.
	index *foldIndex
}

// SetRegions replaces fold regions (from LSP or indent detection).
// Preserves collapsed state for matching regions.
func (fs *FoldState) SetRegions(regions []FoldRegion) {
	// Build lookup of currently collapsed ranges.
	collapsed := make(map[[2]int]bool)
	for _, r := range fs.Regions {
		if r.Collapsed {
			collapsed[[2]int{r.StartLine, r.EndLine}] = true
		}
	}
	for i := range regions {
		key := [2]int{regions[i].StartLine, regions[i].EndLine}
		if collapsed[key] {
			regions[i].Collapsed = true
		}
	}
	fs.Regions = regions
	fs.invalidate()
	fs.ensureIndex()
}

func (fs *FoldState) invalidate() {
	fs.index = nil
}

func (fs *FoldState) ensureIndex() *foldIndex {
	if len(fs.Regions) == 0 {
		fs.index = nil
		return nil
	}
	if fs.index != nil && fs.index.regionsLen == len(fs.Regions) && fs.index.regionsFirst == &fs.Regions[0] {
		return fs.index
	}

	idx := &foldIndex{
		regionsFirst: &fs.Regions[0],
		regionsLen:   len(fs.Regions),
		starts:       make(map[int]int, len(fs.Regions)),
	}
	intervals := make([]lineInterval, 0, len(fs.Regions))
	for i, region := range fs.Regions {
		if _, exists := idx.starts[region.StartLine]; !exists {
			idx.starts[region.StartLine] = i
		}
		// The start line remains visible. A malformed or one-line region
		// cannot hide anything, matching the former range test.
		if !region.Collapsed || region.EndLine <= region.StartLine {
			continue
		}
		start := max(0, region.StartLine+1)
		if region.EndLine < start {
			continue
		}
		intervals = append(intervals, lineInterval{start: start, end: region.EndLine})
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start == intervals[j].start {
			return intervals[i].end < intervals[j].end
		}
		return intervals[i].start < intervals[j].start
	})
	for _, interval := range intervals {
		n := len(idx.hidden)
		if n == 0 || interval.start > idx.hidden[n-1].end+1 {
			idx.hidden = append(idx.hidden, interval)
			continue
		}
		idx.hidden[n-1].end = max(idx.hidden[n-1].end, interval.end)
	}
	idx.hiddenPrefix = make([]int, len(idx.hidden))
	for i, interval := range idx.hidden {
		idx.hiddenPrefix[i] = interval.end - interval.start + 1
		if i > 0 {
			idx.hiddenPrefix[i] += idx.hiddenPrefix[i-1]
		}
	}
	fs.index = idx
	return idx
}

// Toggle toggles the fold at the given line (the start line of a region).
func (fs *FoldState) Toggle(line int) {
	if idx := fs.ensureIndex(); idx != nil {
		if i, ok := idx.starts[line]; ok {
			fs.Regions[i].Collapsed = !fs.Regions[i].Collapsed
			fs.invalidate()
			fs.ensureIndex()
		}
	}
}

// Fold collapses the region at the given line.
func (fs *FoldState) Fold(line int) {
	if idx := fs.ensureIndex(); idx != nil {
		if i, ok := idx.starts[line]; ok && !fs.Regions[i].Collapsed {
			fs.Regions[i].Collapsed = true
			fs.invalidate()
			fs.ensureIndex()
		}
	}
}

// Unfold expands the region at the given line.
func (fs *FoldState) Unfold(line int) {
	if idx := fs.ensureIndex(); idx != nil {
		if i, ok := idx.starts[line]; ok && fs.Regions[i].Collapsed {
			fs.Regions[i].Collapsed = false
			fs.invalidate()
			fs.ensureIndex()
		}
	}
}

// FoldAll collapses all regions.
func (fs *FoldState) FoldAll() {
	for i := range fs.Regions {
		fs.Regions[i].Collapsed = true
	}
	fs.invalidate()
	fs.ensureIndex()
}

// ClampLineToVisible returns the fold header when line is hidden inside a
// collapsed region, otherwise line itself. FoldAll can leave the caret on a
// hidden line; callers should move it here so typing is not silent.
func (fs *FoldState) ClampLineToVisible(line int) int {
	if fs == nil || !fs.IsLineHidden(line) {
		return line
	}
	for _, region := range fs.Regions {
		if region.Collapsed && line > region.StartLine && line <= region.EndLine {
			return region.StartLine
		}
	}
	return line
}

// UnfoldAll expands all regions.
func (fs *FoldState) UnfoldAll() {
	for i := range fs.Regions {
		fs.Regions[i].Collapsed = false
	}
	fs.invalidate()
	fs.ensureIndex()
}

func (idx *foldIndex) isHidden(line int) bool {
	if idx == nil || len(idx.hidden) == 0 {
		return false
	}
	i := sort.Search(len(idx.hidden), func(i int) bool { return idx.hidden[i].start > line })
	return i > 0 && line <= idx.hidden[i-1].end
}

// hiddenBefore returns the number of hidden lines strictly before line.
func (idx *foldIndex) hiddenBefore(line int) int {
	if idx == nil || len(idx.hidden) == 0 || line <= 0 {
		return 0
	}
	i := sort.Search(len(idx.hidden), func(i int) bool { return idx.hidden[i].end >= line })
	if i == len(idx.hidden) {
		return idx.hiddenPrefix[len(idx.hiddenPrefix)-1]
	}
	hidden := 0
	if i > 0 {
		hidden = idx.hiddenPrefix[i-1]
	}
	if idx.hidden[i].start < line {
		hidden += min(idx.hidden[i].end, line-1) - idx.hidden[i].start + 1
	}
	return hidden
}

func (idx *foldIndex) hiddenWithin(totalLines int) int {
	if idx == nil || totalLines <= 0 {
		return 0
	}
	return idx.hiddenBefore(totalLines)
}

// IsLineHidden returns true if the line is inside a collapsed fold (not the start line).
func (fs *FoldState) IsLineHidden(line int) bool {
	return fs.ensureIndex().isHidden(line)
}

// FoldIndicator returns the fold indicator for a gutter line:
// ">" if collapsed start, "v" if expanded start, "" otherwise.
func (fs *FoldState) FoldIndicator(line int) string {
	idx := fs.ensureIndex()
	if idx == nil {
		return ""
	}
	if i, ok := idx.starts[line]; ok {
		if fs.Regions[i].Collapsed {
			return ">"
		}
		return "v"
	}
	return ""
}

// VisibleLines returns the list of visible buffer line numbers for a viewport range.
// startLine is the first visible buffer line, count is max visual rows to show.
func (fs *FoldState) VisibleLines(startLine, count, totalLines int) []int {
	if count <= 0 || totalLines <= 0 {
		return nil
	}
	lines := make([]int, 0, count)
	idx := fs.ensureIndex()
	for line := max(0, startLine); line < totalLines && len(lines) < count; {
		if idx == nil || !idx.isHidden(line) {
			lines = append(lines, line)
			line++
			continue
		}
		i := sort.Search(len(idx.hidden), func(i int) bool { return idx.hidden[i].start > line })
		line = idx.hidden[i-1].end + 1
	}
	return lines
}

// HasCollapsedRegions reports whether any region is currently folded, and so
// whether visual rows and buffer lines can diverge. Callers that only need to
// know "does folding affect coordinates right now" should use this rather than
// scanning Regions.
func (fs *FoldState) HasCollapsedRegions() bool {
	if fs == nil {
		return false
	}
	// ensureIndex returns nil when there are no regions at all.
	index := fs.ensureIndex()
	return index != nil && len(index.hidden) > 0
}

// TotalVisibleLines returns the total count of visible lines accounting for folds.
func (fs *FoldState) TotalVisibleLines(totalLines int) int {
	if totalLines <= 0 {
		return 0
	}
	return totalLines - fs.ensureIndex().hiddenWithin(totalLines)
}

// VisualLineToBuffer converts a visual line index (0-based from top of document)
// to the actual buffer line number, accounting for folds.
func (fs *FoldState) VisualLineToBuffer(visualLine, totalLines int) int {
	if totalLines <= 0 {
		return 0
	}
	visualLine = max(0, visualLine)
	idx := fs.ensureIndex()
	if idx == nil {
		return min(visualLine, totalLines-1)
	}
	visibleTotal := totalLines - idx.hiddenWithin(totalLines)
	if visualLine >= visibleTotal {
		return totalLines - 1
	}
	// visibleThrough(line) is monotonic. Binary search prevents a scroll jump
	// from walking every document line when folds are sparse.
	return sort.Search(totalLines, func(line int) bool {
		return line+1-idx.hiddenBefore(line+1) > visualLine
	})
}

// BufferLineToVisual converts a buffer line to its visual line index.
func (fs *FoldState) BufferLineToVisual(bufLine, totalLines int) int {
	if bufLine <= 0 || totalLines <= 0 {
		return 0
	}
	return min(bufLine, totalLines) - fs.ensureIndex().hiddenBefore(min(bufLine, totalLines))
}

// DetectIndentRegions generates fold regions based on indentation levels.
// Used as fallback when LSP doesn't provide foldingRange.
func DetectIndentRegions(lines func(int) []byte, lineCount int) []FoldRegion {
	if lineCount < 2 {
		return nil
	}

	type lineInfo struct {
		indent int
		blank  bool
	}

	infos := make([]lineInfo, lineCount)
	for i := 0; i < lineCount; i++ {
		line := lines(i)
		if len(line) == 0 {
			infos[i] = lineInfo{blank: true}
			continue
		}
		indent := 0
	indentLoop:
		for _, b := range line {
			switch b {
			case ' ':
				indent++
			case '\t':
				indent += 4
			default:
				break indentLoop
			}
		}
		if indent == len(line) {
			infos[i] = lineInfo{blank: true}
		} else {
			infos[i] = lineInfo{indent: indent}
		}
	}

	var regions []FoldRegion
	// Simple strategy: a line starts a fold if the next non-blank line is more indented
	for i := 0; i < lineCount-1; i++ {
		if infos[i].blank {
			continue
		}
		nextNonBlank := -1
		for j := i + 1; j < lineCount; j++ {
			if !infos[j].blank {
				nextNonBlank = j
				break
			}
		}
		if nextNonBlank < 0 || infos[nextNonBlank].indent <= infos[i].indent {
			continue
		}
		endLine := nextNonBlank
		for j := nextNonBlank + 1; j < lineCount; j++ {
			if infos[j].blank {
				continue
			}
			if infos[j].indent > infos[i].indent {
				endLine = j
			} else {
				break
			}
		}
		if endLine > i {
			regions = append(regions, FoldRegion{StartLine: i, EndLine: endLine})
		}
	}

	sort.Slice(regions, func(a, b int) bool {
		return regions[a].StartLine < regions[b].StartLine
	})
	return regions
}
