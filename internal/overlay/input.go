package overlay

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"teak/internal/ui"
)

// InputResult is the value returned by an Input overlay when it is submitted
// or dismissed. Accepted is false when the user presses Escape.
type InputResult struct {
	Value    string
	Accepted bool
}

// InputResultAction maps an input result to an application message. Keeping
// the mapper in the overlay avoids passing application-specific callback IDs
// through the generic overlay package.
type InputResultAction func(InputResult) tea.Msg

// Input is a single-line text prompt that captures keyboard input until the
// user submits it or presses Escape.
type Input struct {
	Prompt    string
	input     textinput.Model
	theme     ui.Theme
	width     int
	dismissed bool
	result    tea.Msg
	action    InputResultAction
}

// NewInput creates a text prompt with an optional initial value.
func NewInput(prompt, initial string, theme ui.Theme) *Input {
	ti := textinput.New()
	ti.CharLimit = 4096
	ti.SetValue(initial)
	return &Input{
		Prompt: prompt,
		input:  ti,
		theme:  theme,
		width:  60,
	}
}

// Focus gives the prompt keyboard focus and returns its blink command.
func (i *Input) Focus() tea.Cmd {
	return i.input.Focus()
}

// SetWidth limits the prompt to the available terminal width.
func (i *Input) SetWidth(width int) {
	i.width = max(1, width)
	i.input.SetWidth(max(1, min(i.width-8, 80)))
}

// SetResultAction registers the message mapper used on submit or dismissal.
func (i *Input) SetResultAction(action InputResultAction) {
	i.action = action
}

// CancelAction returns the cancellation message without changing the input
// state. App owners use it to release an asynchronous request when the whole
// overlay stack is cleared before the user responds.
func (i *Input) CancelAction() tea.Msg {
	if i.action == nil {
		return nil
	}
	return i.action(InputResult{})
}

// Value returns the current text in the prompt.
func (i *Input) Value() string {
	return i.input.Value()
}

// Update implements Overlay.
func (i *Input) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "esc", "escape":
			return i.finish(InputResult{}), i.resultCommand()
		case "enter":
			return i.finish(InputResult{Value: i.input.Value(), Accepted: true}), i.resultCommand()
		}
	}
	var cmd tea.Cmd
	i.input, cmd = i.input.Update(msg)
	return i, cmd
}

func (i *Input) finish(result InputResult) *Input {
	i.dismissed = true
	if i.action != nil {
		i.result = i.action(result)
	}
	return i
}

func (i *Input) resultCommand() tea.Cmd {
	result := i.result
	if result == nil {
		return nil
	}
	return func() tea.Msg { return result }
}

// View implements Overlay.
func (i *Input) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true)
	promptStyle := lipgloss.NewStyle().Foreground(ui.Nord4)
	hintStyle := lipgloss.NewStyle().Foreground(ui.Nord3)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Plugin input"))
	sb.WriteString("\n\n")
	if i.Prompt != "" {
		sb.WriteString(promptStyle.Render(i.Prompt))
		sb.WriteString("\n\n")
	}
	sb.WriteString(i.input.View())
	sb.WriteString("\n\n")
	sb.WriteString(hintStyle.Render("Enter to accept • Esc to cancel"))

	width := max(1, i.width)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ui.Nord3).
		Background(ui.Nord1).
		Padding(1, 2).
		Width(width).
		Render(sb.String())
}

// IsDismissed implements Overlay.
func (i *Input) IsDismissed() bool {
	return i.dismissed
}

// CapturesInput implements Overlay.
func (i *Input) CapturesInput() bool {
	return true
}
