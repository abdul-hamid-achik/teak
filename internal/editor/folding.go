package editor

import "sort"

// FoldRegion represents a foldable range in the document.
type FoldRegion struct {
	StartLine int
	EndLine   int
	Collapsed bool
}

// FoldState manages code folding state for an editor.
type FoldState struct {
	Regions []FoldRegion
}

// SetRegions replaces fold regions (from LSP or indent detection).
// Preserves collapsed state for matching regions.
func (fs *FoldState) SetRegions(regions []FoldRegion) {
	// Build lookup of currently collapsed ranges
	collapsed := make(map[[2]int]bool)
	for _, r := range fs.Regions {
		if r.Collapsed {
			collapsed[[2]int{r.StartLine, r.EndLine}] = true
		}
	}
	// Preserve collapsed state
	for i := range regions {
		key := [2]int{regions[i].StartLine, regions[i].EndLine}
		if collapsed[key] {
			regions[i].Collapsed = true
		}
	}
	fs.Regions = regions
}

// Toggle toggles the fold at the given line (the start line of a region).
func (fs *FoldState) Toggle(line int) {
	for i := range fs.Regions {
		if fs.Regions[i].StartLine == line {
			fs.Regions[i].Collapsed = !fs.Regions[i].Collapsed
			return
		}
	}
}

// Fold collapses the region at the given line.
func (fs *FoldState) Fold(line int) {
	for i := range fs.Regions {
		if fs.Regions[i].StartLine == line {
			fs.Regions[i].Collapsed = true
			return
		}
	}
}

// Unfold expands the region at the given line.
func (fs *FoldState) Unfold(line int) {
	for i := range fs.Regions {
		if fs.Regions[i].StartLine == line {
			fs.Regions[i].Collapsed = false
			return
		}
	}
}

// FoldAll collapses all regions.
func (fs *FoldState) FoldAll() {
	for i := range fs.Regions {
		fs.Regions[i].Collapsed = true
	}
}

// UnfoldAll expands all regions.
func (fs *FoldState) UnfoldAll() {
	for i := range fs.Regions {
		fs.Regions[i].Collapsed = false
	}
}

// IsLineHidden returns true if the line is inside a collapsed fold (not the start line).
func (fs *FoldState) IsLineHidden(line int) bool {
	for _, r := range fs.Regions {
		if r.Collapsed && line > r.StartLine && line <= r.EndLine {
			return true
		}
	}
	return false
}

// FoldIndicator returns the fold indicator for a gutter line:
// ">" if collapsed start, "v" if expanded start, "" otherwise.
func (fs *FoldState) FoldIndicator(line int) string {
	for _, r := range fs.Regions {
		if r.StartLine == line {
			if r.Collapsed {
				return ">"
			}
			return "v"
		}
	}
	return ""
}

// VisibleLines returns the list of visible buffer line numbers for a viewport range.
// startLine is the first visible buffer line, count is max visual rows to show.
func (fs *FoldState) VisibleLines(startLine, count, totalLines int) []int {
	if len(fs.Regions) == 0 {
		// Fast path: no folds
		lines := make([]int, 0, count)
		for line := startLine; line < totalLines && len(lines) < count; line++ {
			lines = append(lines, line)
		}
		return lines
	}

	lines := make([]int, 0, count)
	for line := startLine; line < totalLines && len(lines) < count; line++ {
		if !fs.IsLineHidden(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

// TotalVisibleLines returns the total count of visible lines accounting for folds.
func (fs *FoldState) TotalVisibleLines(totalLines int) int {
	if len(fs.Regions) == 0 {
		return totalLines
	}
	count := 0
	for line := 0; line < totalLines; line++ {
		if !fs.IsLineHidden(line) {
			count++
		}
	}
	return count
}

// VisualLineToBuffer converts a visual line index (0-based from top of document)
// to the actual buffer line number, accounting for folds.
func (fs *FoldState) VisualLineToBuffer(visualLine, totalLines int) int {
	if len(fs.Regions) == 0 {
		return visualLine
	}
	visible := 0
	for line := 0; line < totalLines; line++ {
		if !fs.IsLineHidden(line) {
			if visible == visualLine {
				return line
			}
			visible++
		}
	}
	return totalLines - 1
}

// BufferLineToVisual converts a buffer line to its visual line index.
func (fs *FoldState) BufferLineToVisual(bufLine, totalLines int) int {
	if len(fs.Regions) == 0 {
		return bufLine
	}
	visible := 0
	for line := 0; line < totalLines && line < bufLine; line++ {
		if !fs.IsLineHidden(line) {
			visible++
		}
	}
	return visible
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
		for _, b := range line {
			if b == ' ' {
				indent++
			} else if b == '\t' {
				indent += 4
			} else {
				break
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
		// Find next non-blank line
		nextNonBlank := -1
		for j := i + 1; j < lineCount; j++ {
			if !infos[j].blank {
				nextNonBlank = j
				break
			}
		}
		if nextNonBlank < 0 {
			continue
		}
		if infos[nextNonBlank].indent > infos[i].indent {
			// Find end of this indented block
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
	}

	// Sort by start line
	sort.Slice(regions, func(a, b int) bool {
		return regions[a].StartLine < regions[b].StartLine
	})

	return regions
}
