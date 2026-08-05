package app

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"teak/internal/editor"
	"teak/internal/plugin"
)

const (
	maxPluginHighlightRanges     = 512
	maxPluginHighlightRangeBytes = 4096
	maxPluginHighlightColorBytes = 64
	maxPluginHighlightNamespaces = 64
	maxPluginHighlightTotal      = 4096
)

func validatePluginHighlightRequest(request plugin.UIHighlightRequest) error {
	if request.Namespace <= 0 {
		return fmt.Errorf("highlight namespace must be positive")
	}
	if len(request.Highlights) > maxPluginHighlightRanges {
		return fmt.Errorf("at most %d highlight ranges are allowed", maxPluginHighlightRanges)
	}
	for i, highlight := range request.Highlights {
		if highlight.Line < 0 || highlight.StartCol < 0 || highlight.EndCol <= highlight.StartCol {
			return fmt.Errorf("highlight %d has an invalid range", i+1)
		}
		if highlight.EndCol-highlight.StartCol > maxPluginHighlightRangeBytes {
			return fmt.Errorf("highlight %d exceeds %d bytes", i+1, maxPluginHighlightRangeBytes)
		}
		if err := validatePluginHighlightColor(highlight.Foreground, "foreground", i); err != nil {
			return err
		}
		if err := validatePluginHighlightColor(highlight.Background, "background", i); err != nil {
			return err
		}
	}
	return nil
}

func validatePluginHighlightColor(value, name string, index int) error {
	if len(value) > maxPluginHighlightColorBytes {
		return fmt.Errorf("highlight %d %s color exceeds %d bytes", index+1, name, maxPluginHighlightColorBytes)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("highlight %d %s color contains a control character", index+1, name)
	}
	return nil
}

func pluginHighlightStyle(highlight plugin.UIHighlight) lipgloss.Style {
	style := lipgloss.NewStyle()
	if highlight.Foreground != "" {
		style = style.Foreground(lipgloss.Color(highlight.Foreground))
	}
	if highlight.Background != "" {
		style = style.Background(lipgloss.Color(highlight.Background))
	}
	if highlight.Bold {
		style = style.Bold(true)
	}
	if highlight.Underline {
		style = style.Underline(true)
	}
	return style
}

func (m *Model) setPluginHighlightsForEditor(index int, request plugin.UIHighlightRequest) error {
	if err := validatePluginHighlightRequest(request); err != nil {
		return err
	}
	if index < 0 || index >= len(m.editors) {
		return fmt.Errorf("editor is not available")
	}
	ed := &m.editors[index]
	if ed.Buffer == nil {
		return fmt.Errorf("editor has no active buffer")
	}
	ranges := make([]editor.HighlightRange, 0, len(request.Highlights))
	for _, highlight := range request.Highlights {
		ranges = append(ranges, editor.HighlightRange{
			Namespace: request.Namespace,
			Line:      highlight.Line,
			StartCol:  highlight.StartCol,
			EndCol:    highlight.EndCol,
			Style:     pluginHighlightStyle(highlight),
		})
	}
	if len(ranges) > 0 {
		namespaceCount, existingTotal, exists := ed.PluginHighlightCounts(request.Namespace)
		if !exists && namespaceCount >= maxPluginHighlightNamespaces {
			return fmt.Errorf("highlight namespace limit reached (max %d)", maxPluginHighlightNamespaces)
		}
		if existingTotal+len(ranges) > maxPluginHighlightTotal {
			return fmt.Errorf("highlight range limit reached (max %d)", maxPluginHighlightTotal)
		}
	}
	ed.ReplacePluginHighlights(request.Namespace, ranges)
	m.setEditor(index, *ed)
	return nil
}

func (m *Model) clearPluginHighlightsForEditor(index, namespace int) error {
	if namespace < 0 {
		return fmt.Errorf("highlight namespace must not be negative")
	}
	if index < 0 || index >= len(m.editors) {
		return fmt.Errorf("editor is not available")
	}
	ed := &m.editors[index]
	if ed.Buffer == nil {
		return fmt.Errorf("editor has no active buffer")
	}
	ed.ClearPluginHighlights(namespace)
	m.setEditor(index, *ed)
	return nil
}

func (m *Model) activePluginHighlightEditorIndex() (int, error) {
	if m.isActiveDiffTab() {
		return -1, fmt.Errorf("no active buffer in diff view")
	}
	if m.activeTab < 0 || m.activeTab >= len(m.editors) {
		return -1, fmt.Errorf("no active buffer")
	}
	if m.editors[m.activeTab].Buffer == nil {
		return -1, fmt.Errorf("no active buffer")
	}
	return m.activeTab, nil
}

func (m *Model) setPluginHighlightsForActive(request plugin.UIHighlightRequest) error {
	index, err := m.activePluginHighlightEditorIndex()
	if err != nil {
		return err
	}
	return m.setPluginHighlightsForEditor(index, request)
}

func (m *Model) clearPluginHighlightsForActive(namespace int) error {
	index, err := m.activePluginHighlightEditorIndex()
	if err != nil {
		return err
	}
	return m.clearPluginHighlightsForEditor(index, namespace)
}
