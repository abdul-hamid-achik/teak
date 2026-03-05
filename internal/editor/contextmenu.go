package editor

import (
	"strings"

	"teak/internal/ui"
)

// ContextMenuActionMsg is sent when a context menu item is selected.
type ContextMenuActionMsg struct {
	Action string
}

// ContextMenuItem represents a single item in a context menu.
type ContextMenuItem struct {
	Label    string // Display text (e.g. "Go to Definition")
	Shortcut string // Hint text (e.g. "F12")
	Action   string // Action ID for dispatch (e.g. "goto_definition")
	Disabled bool   // Greyed out if not available
}

// ContextMenu manages the context menu popup state.
type ContextMenu struct {
	Items   []ContextMenuItem
	Cursor  int
	Visible bool
	X, Y    int // Screen position where menu was triggered
	theme   ui.Theme
}

// NewContextMenu creates a new context menu popup.
func NewContextMenu(theme ui.Theme) ContextMenu {
	return ContextMenu{theme: theme}
}

// Show displays the context menu with the given items at (x, y).
func (c *ContextMenu) Show(items []ContextMenuItem, x, y int) {
	c.Items = items
	c.X = x
	c.Y = y
	c.Visible = len(items) > 0
	c.Cursor = 0
	// Advance past any initial disabled/separator items
	c.skipDisabledForward()
}

// Hide dismisses the context menu.
func (c *ContextMenu) Hide() {
	c.Visible = false
	c.Items = nil
	c.Cursor = 0
}

// MoveUp moves the cursor up, skipping disabled items and separators.
func (c *ContextMenu) MoveUp() {
	for i := c.Cursor - 1; i >= 0; i-- {
		if !c.Items[i].Disabled && c.Items[i].Label != "" {
			c.Cursor = i
			return
		}
	}
}

// MoveDown moves the cursor down, skipping disabled items and separators.
func (c *ContextMenu) MoveDown() {
	for i := c.Cursor + 1; i < len(c.Items); i++ {
		if !c.Items[i].Disabled && c.Items[i].Label != "" {
			c.Cursor = i
			return
		}
	}
}

// Selected returns the currently selected item, or nil if nothing is selectable.
func (c *ContextMenu) Selected() *ContextMenuItem {
	if !c.Visible || c.Cursor >= len(c.Items) {
		return nil
	}
	item := &c.Items[c.Cursor]
	if item.Disabled || item.Label == "" {
		return nil
	}
	return item
}

// ItemCount returns the number of visible items (capped at 12).
func (c *ContextMenu) ItemCount() int {
	return min(12, len(c.Items))
}

// SelectAt selects the item at the given index (relative to menu top).
// Returns the selected item if valid and clickable, or nil.
func (c *ContextMenu) SelectAt(idx int) *ContextMenuItem {
	if idx < 0 || idx >= c.ItemCount() {
		return nil
	}
	item := &c.Items[idx]
	if item.Disabled || item.Label == "" {
		return nil
	}
	c.Cursor = idx
	return item
}

func (c *ContextMenu) skipDisabledForward() {
	for c.Cursor < len(c.Items) {
		if !c.Items[c.Cursor].Disabled && c.Items[c.Cursor].Label != "" {
			return
		}
		c.Cursor++
	}
	if c.Cursor >= len(c.Items) && len(c.Items) > 0 {
		c.Cursor = 0
	}
}

// View renders the context menu popup as a string.
func (c ContextMenu) View() string {
	if !c.Visible || len(c.Items) == 0 {
		return ""
	}

	maxItems := min(12, len(c.Items))

	// Calculate widths
	maxLabelW := 0
	maxShortcutW := 0
	for i := range maxItems {
		item := c.Items[i]
		if item.Label == "" {
			continue // separator
		}
		if len(item.Label) > maxLabelW {
			maxLabelW = len(item.Label)
		}
		if len(item.Shortcut) > maxShortcutW {
			maxShortcutW = len(item.Shortcut)
		}
	}

	// Total width: "  Label   Shortcut  "
	totalWidth := maxLabelW + 4 // 2 padding each side
	if maxShortcutW > 0 {
		totalWidth = maxLabelW + maxShortcutW + 6 // 2+label+2+shortcut+2 with gap
	}
	if totalWidth < 20 {
		totalWidth = 20
	}
	if totalWidth > 50 {
		totalWidth = 50
	}

	var sb strings.Builder
	for i := range maxItems {
		item := c.Items[i]

		// Separator
		if item.Label == "" {
			sep := strings.Repeat("─", totalWidth)
			sb.WriteString(c.theme.AutocompleteItem.Render(sep))
			if i < maxItems-1 {
				sb.WriteByte('\n')
			}
			continue
		}

		// Build line: "  Label          Shortcut  "
		var line string
		if maxShortcutW > 0 && item.Shortcut != "" {
			gap := totalWidth - len(item.Label) - len(item.Shortcut) - 4
			if gap < 2 {
				gap = 2
			}
			line = "  " + item.Label + strings.Repeat(" ", gap) + item.Shortcut + "  "
		} else {
			line = "  " + item.Label
			if len(line) < totalWidth {
				line += strings.Repeat(" ", totalWidth-len(line))
			}
		}

		// Pad to width
		if len(line) < totalWidth {
			line += strings.Repeat(" ", totalWidth-len(line))
		}

		switch {
		case i == c.Cursor && !item.Disabled:
			sb.WriteString(c.theme.AutocompleteCursor.Render(line))
		case item.Disabled:
			sb.WriteString(c.theme.ContextMenuDisabled.Render(line))
		default:
			sb.WriteString(c.theme.AutocompleteItem.Render(line))
		}

		if i < maxItems-1 {
			sb.WriteByte('\n')
		}
	}

	return c.theme.AutocompleteBox.Render(sb.String())
}
