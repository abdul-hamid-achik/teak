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

// PluginHighlightRanges flattens namespaces in deterministic order for the
// viewport renderer. It returns nil when ranges are absent or stale.
func (e Editor) PluginHighlightRanges() []HighlightRange {
	if e.Buffer == nil || e.PluginHighlightVersion != e.Buffer.Version() || len(e.PluginHighlights) == 0 {
		return nil
	}
	namespaces := make([]int, 0, len(e.PluginHighlights))
	for namespace := range e.PluginHighlights {
		namespaces = append(namespaces, namespace)
	}
	sort.Ints(namespaces)
	var result []HighlightRange
	for _, namespace := range namespaces {
		result = append(result, e.PluginHighlights[namespace]...)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Line != result[j].Line {
			return result[i].Line < result[j].Line
		}
		if result[i].StartCol != result[j].StartCol {
			return result[i].StartCol < result[j].StartCol
		}
		if result[i].EndCol != result[j].EndCol {
			return result[i].EndCol < result[j].EndCol
		}
		return result[i].Namespace < result[j].Namespace
	})
	return result
}
