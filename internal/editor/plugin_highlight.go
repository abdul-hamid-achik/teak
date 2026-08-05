package editor

import (
	"sort"

	"charm.land/lipgloss/v2"
)

// HighlightRange is a bounded, 0-based byte range supplied by a plugin. The
// range is valid for the editor buffer version at which it was installed.
type HighlightRange struct {
	Namespace int
	Line      int
	StartCol  int
	EndCol    int
	Style     lipgloss.Style
}

// ReplacePluginHighlights installs one namespace in prepared line order. The
// namespace slice is immutable after installation so viewport queries can use
// binary search without rebuilding a file-wide projection on every frame.
func (e *Editor) ReplacePluginHighlights(namespace int, ranges []HighlightRange) {
	existingHighlights := e.pluginHighlights
	if !e.pluginHighlightVersionCurrent() {
		existingHighlights = nil
	}
	next := make(map[int][]HighlightRange, len(existingHighlights)+1)
	for existingNamespace, existing := range existingHighlights {
		next[existingNamespace] = existing
	}
	if len(ranges) == 0 {
		delete(next, namespace)
	} else {
		owned := append([]HighlightRange(nil), ranges...)
		for i := range owned {
			owned[i].Namespace = namespace
		}
		sort.SliceStable(owned, func(i, j int) bool {
			return pluginHighlightLess(owned[i], owned[j])
		})
		next[namespace] = owned
	}
	e.pluginHighlights = next
	e.markPluginHighlightsCurrent()
}

// ClearPluginHighlights removes one namespace, or every namespace when zero
// is passed, and binds the resulting collection to the current buffer version.
func (e *Editor) ClearPluginHighlights(namespace int) {
	if namespace == 0 || !e.pluginHighlightVersionCurrent() {
		e.pluginHighlights = make(map[int][]HighlightRange)
	} else {
		next := make(map[int][]HighlightRange, max(0, len(e.pluginHighlights)-1))
		for existingNamespace, ranges := range e.pluginHighlights {
			if existingNamespace != namespace {
				next[existingNamespace] = ranges
			}
		}
		e.pluginHighlights = next
	}
	e.markPluginHighlightsCurrent()
}

// PluginHighlightCounts returns the namespace count, the number of ranges
// outside excludedNamespace, and whether that namespace currently exists.
func (e Editor) PluginHighlightCounts(excludedNamespace int) (int, int, bool) {
	if !e.pluginHighlightVersionCurrent() {
		return 0, 0, false
	}
	namespaceCount := len(e.pluginHighlights)
	total := 0
	_, exists := e.pluginHighlights[excludedNamespace]
	for namespace, ranges := range e.pluginHighlights {
		if namespace != excludedNamespace {
			total += len(ranges)
		}
	}
	return namespaceCount, total, exists
}

// CopyPreparedPluginHighlightsFrom shares an immutable prepared collection
// when an editor shell is rebuilt around the same buffer. Replacements and
// clears use copy-on-write maps, so later updates cannot mutate the source.
func (e *Editor) CopyPreparedPluginHighlightsFrom(source *Editor) {
	if source == nil || e.Buffer == nil || e.Buffer != source.Buffer || !source.pluginHighlightVersionCurrent() {
		e.pluginHighlights = make(map[int][]HighlightRange)
		e.markPluginHighlightsCurrent()
		return
	}
	e.pluginHighlights = source.pluginHighlights
	e.pluginHighlightVersion = source.pluginHighlightVersion
}

func (e *Editor) markPluginHighlightsCurrent() {
	e.pluginHighlightVersion = -1
	if e.Buffer != nil {
		e.pluginHighlightVersion = e.Buffer.Version()
	}
}

// PluginHighlightRanges returns the complete collection in deterministic
// order for inspection. Rendering uses the bounded viewport projection below.
func (e Editor) PluginHighlightRanges() []HighlightRange {
	if !e.pluginHighlightsCurrent() {
		return nil
	}
	total := 0
	for _, ranges := range e.pluginHighlights {
		total += len(ranges)
	}
	result := make([]HighlightRange, 0, total)
	for _, ranges := range e.pluginHighlights {
		result = append(result, ranges...)
	}
	sortPluginHighlightRanges(result)
	return result
}

func (e Editor) pluginHighlightRangesForProjection(visibleLines []int, startLine, endLine int) []HighlightRange {
	if !e.pluginHighlightsCurrent() || (len(visibleLines) == 0 && startLine > endLine) {
		return nil
	}
	count := 0
	e.visitPluginHighlightProjection(visibleLines, startLine, endLine, func(ranges []HighlightRange) {
		count += len(ranges)
	})
	if count == 0 {
		return nil
	}
	result := make([]HighlightRange, 0, count)
	e.visitPluginHighlightProjection(visibleLines, startLine, endLine, func(ranges []HighlightRange) {
		result = append(result, ranges...)
	})
	sortPluginHighlightRanges(result)
	return result
}

func (e Editor) visitPluginHighlightProjection(visibleLines []int, startLine, endLine int, visit func([]HighlightRange)) {
	for _, ranges := range e.pluginHighlights {
		if len(visibleLines) == 0 {
			visit(pluginHighlightLineRange(ranges, startLine, endLine))
			continue
		}
		start := visibleLines[0]
		previous := start
		for _, line := range visibleLines[1:] {
			if line == previous+1 {
				previous = line
				continue
			}
			visit(pluginHighlightLineRange(ranges, start, previous))
			start, previous = line, line
		}
		visit(pluginHighlightLineRange(ranges, start, previous))
	}
}

func pluginHighlightLineRange(ranges []HighlightRange, startLine, endLine int) []HighlightRange {
	first := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].Line >= startLine
	})
	last := sort.Search(len(ranges), func(i int) bool {
		return ranges[i].Line > endLine
	})
	return ranges[first:last]
}

func (e Editor) pluginHighlightsCurrent() bool {
	return e.pluginHighlightVersionCurrent() && len(e.pluginHighlights) > 0
}

func (e Editor) pluginHighlightVersionCurrent() bool {
	return e.Buffer != nil && e.pluginHighlightVersion == e.Buffer.Version()
}

func sortPluginHighlightRanges(ranges []HighlightRange) {
	sort.SliceStable(ranges, func(i, j int) bool {
		return pluginHighlightLess(ranges[i], ranges[j])
	})
}

func pluginHighlightLess(left, right HighlightRange) bool {
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	if left.StartCol != right.StartCol {
		return left.StartCol < right.StartCol
	}
	if left.EndCol != right.EndCol {
		return left.EndCol < right.EndCol
	}
	return left.Namespace < right.Namespace
}
