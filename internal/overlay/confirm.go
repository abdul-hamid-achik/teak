package overlay

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// ButtonAction is the callback message returned when a confirm button is pressed.
type ButtonAction struct {
	Label string
}

// Button describes a single button in the confirm dialog.
type Button struct {
	Label  string
	Style  lipgloss.Style
	Action tea.Msg // message sent when this button is activated
}

// Confirm is a modal dialog with a title, message, optional item list,
// and a row of navigable buttons.
type Confirm struct {
	Title         string
	Message       string
	Items         []string
	Buttons       []Button
	theme         ui.Theme
	cursor        int
	dismissed     bool
	result        tea.Msg
	dismissAction tea.Msg
	width         int
}

// NewConfirm creates a confirm dialog. The first button is focused by default.
func NewConfirm(title, message string, items []string, buttons []Button, theme ui.Theme) *Confirm {
	return &Confirm{
		Title:   title,
		Message: message,
		Items:   items,
		Buttons: buttons,
		theme:   theme,
		width:   50,
	}
}

// SetWidth sets the dialog width.
func (c *Confirm) SetWidth(w int) {
	c.width = w
}

// Result returns the message produced when a button was pressed, or nil.
func (c *Confirm) Result() tea.Msg {
	return c.result
}

// SetDismissAction supplies an optional message for Escape dismissal. Existing
// callers leave it unset; async clients such as plugins can use it to observe
// cancellation without blocking the UI while waiting for a response.
func (c *Confirm) SetDismissAction(action tea.Msg) {
	c.dismissAction = action
}

// DismissAction returns the optional message associated with Escape. App
// owners can use it to release an asynchronous request when the whole overlay
// stack is cleared before the user responds.
func (c *Confirm) DismissAction() tea.Msg {
	return c.dismissAction
}

// Update implements Overlay.
func (c *Confirm) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "escape":
			c.dismissed = true
			if c.dismissAction != nil {
				c.result = c.dismissAction
				action := c.dismissAction
				return c, func() tea.Msg { return action }
			}
			return c, nil
		case "left", "shift+tab":
			if c.cursor > 0 {
				c.cursor--
			}
			return c, nil
		case "right", "tab":
			if c.cursor < len(c.Buttons)-1 {
				c.cursor++
			}
			return c, nil
		case "enter":
			if len(c.Buttons) > 0 && c.cursor < len(c.Buttons) {
				c.dismissed = true
				c.result = c.Buttons[c.cursor].Action
				action := c.Buttons[c.cursor].Action
				return c, func() tea.Msg { return action }
			}
			return c, nil
		default:
			if idx, ok := c.acceleratorIndex(msg.String()); ok {
				c.dismissed = true
				c.result = c.Buttons[idx].Action
				action := c.Buttons[idx].Action
				return c, func() tea.Msg { return action }
			}
			return c, nil
		}
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			for i, btn := range c.Buttons {
				if zone.Get(confirmZoneID(i)).InBounds(msg) {
					c.dismissed = true
					c.result = btn.Action
					action := btn.Action
					return c, func() tea.Msg { return action }
				}
			}
		}
		return c, nil
	}
	return c, nil
}

// View implements Overlay.
func (c *Confirm) View() string {
	var sb strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(ui.Nord8).
		Bold(true)
	sb.WriteString(titleStyle.Render(c.Title))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	// Message
	if c.Message != "" {
		msgStyle := lipgloss.NewStyle().Foreground(ui.Nord4)
		sb.WriteString(msgStyle.Render(c.Message))
		sb.WriteByte('\n')
	}

	// Items list
	if len(c.Items) > 0 {
		sb.WriteByte('\n')
		itemStyle := lipgloss.NewStyle().Foreground(ui.Nord4).PaddingLeft(2)
		for _, item := range c.Items {
			sb.WriteString(itemStyle.Render(item))
			sb.WriteByte('\n')
		}
	}

	// Buttons row
	sb.WriteByte('\n')
	btnNormal := lipgloss.NewStyle().
		Background(ui.Nord2).
		Foreground(ui.Nord4).
		Padding(0, 2)
	btnFocused := lipgloss.NewStyle().
		Background(ui.Nord10).
		Foreground(ui.Nord6).
		Padding(0, 2).
		Bold(true)

	for i, btn := range c.Buttons {
		style := btnNormal
		if btn.Style.GetForeground() != nil {
			style = btn.Style
		}
		if i == c.cursor {
			style = btnFocused
		}
		rendered := zone.Mark(confirmZoneID(i), style.Render(btn.Label))
		sb.WriteString(rendered)
		if i < len(c.Buttons)-1 {
			sb.WriteString("  ")
		}
	}

	content := sb.String()
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.Nord3).
		Background(ui.Nord1).
		Padding(1, 2).
		Width(c.width)

	return boxStyle.Render(content)
}

// IsDismissed implements Overlay.
func (c *Confirm) IsDismissed() bool {
	return c.dismissed
}

// CapturesInput implements Overlay.
func (c *Confirm) CapturesInput() bool {
	return true
}

func confirmZoneID(idx int) string {
	return "confirm-btn-" + strconv.Itoa(idx)
}

// acceleratorIndex maps a single keypress to a button without arrow-key
// navigation: digits pick by position (1-based, 0 selects the tenth), "y"
// prefers an affirmative label and falls back to the first button, "n"
// prefers a negative one and falls back to the last, and any other letter
// matches the first button whose label starts with it.
func (c *Confirm) acceleratorIndex(key string) (int, bool) {
	if len(c.Buttons) == 0 || len(key) != 1 {
		return 0, false
	}
	r := rune(key[0])
	if r >= '1' && r <= '9' {
		idx := int(r - '1')
		if idx < len(c.Buttons) {
			return idx, true
		}
		return 0, false
	}
	if r == '0' {
		if len(c.Buttons) > 9 {
			return 9, true
		}
		return 0, false
	}
	lower := unicode.ToLower(r)
	for i, btn := range c.Buttons {
		if btn.Label == "" {
			continue
		}
		if first, _ := utf8.DecodeRuneInString(btn.Label); unicode.ToLower(first) == lower {
			return i, true
		}
	}
	switch lower {
	case 'y':
		return 0, true
	case 'n':
		return len(c.Buttons) - 1, true
	}
	return 0, false
}
