package termpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

const maxTerminalLines = 2000

// OutputMsg is a generation-tagged PTY read. Superseded generations are dropped.
type OutputMsg struct {
	Generation uint64
	Data       []byte
	Err        error
	Exited     bool
}

// Model is a bottom-panel integrated terminal.
type Model struct {
	theme      ui.Theme
	cwd        string
	Width      int
	Height     int
	lines      []string
	generation uint64
	session    session
}

type session interface {
	Write(data []byte) error
	Resize(cols, rows int) error
	Close() error
	Output() <-chan OutputMsg
}

// New creates an idle terminal panel. The PTY starts on first Start().
func New(theme ui.Theme, cwd string) Model {
	return Model{theme: theme, cwd: cwd}
}

func (m *Model) SetSize(width, height int) {
	m.Width = max(1, width)
	m.Height = max(1, height)
	if m.session != nil {
		_ = m.session.Resize(m.Width, max(1, m.Height-1))
	}
}

func (m *Model) SetTheme(theme ui.Theme) {
	m.theme = theme
}

func (m *Model) Running() bool {
	return m.session != nil
}

func (m *Model) Close() {
	if m.session != nil {
		_ = m.session.Close()
		m.session = nil
	}
	m.generation++
}

func (m Model) View() string {
	title := m.theme.PromptAccent.Render(" Terminal ")
	if m.session == nil {
		title += m.theme.PromptMuted.Render(" (idle)")
	}
	pad := max(0, m.Width-ansi.StringWidth(title))
	title = ansi.Truncate(title+m.theme.StatusBar.Render(strings.Repeat(" ", pad)), m.Width, "")
	bodyRows := max(0, m.Height-1)
	visible := m.visibleLines(bodyRows)
	body := strings.Join(visible, "\n")
	if bodyRows > len(visible) {
		padLines := make([]string, bodyRows-len(visible))
		for i := range padLines {
			padLines[i] = m.theme.Editor.Render(strings.Repeat(" ", max(0, m.Width)))
		}
		if body != "" {
			body += "\n"
		}
		body += strings.Join(padLines, "\n")
	}
	if body == "" {
		return title
	}
	return title + "\n" + body
}

func (m Model) visibleLines(n int) []string {
	if n <= 0 {
		return nil
	}
	lines := m.lines
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = ansi.Truncate(line, m.Width, "")
		if pad := m.Width - ansi.StringWidth(out[i]); pad > 0 {
			out[i] += m.theme.Editor.Render(strings.Repeat(" ", pad))
		}
	}
	return out
}

func (m *Model) ApplyOutput(msg OutputMsg) {
	if msg.Generation != m.generation {
		return
	}
	if msg.Exited || msg.Err != nil {
		m.session = nil
		if msg.Err != nil {
			m.appendText("\n" + msg.Err.Error() + "\n")
		} else {
			m.appendText("\n[process exited]\n")
		}
		return
	}
	if len(msg.Data) > 0 {
		m.appendText(string(msg.Data))
	}
}

func (m *Model) appendText(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	if len(m.lines) == 0 {
		m.lines = []string{""}
	}
	m.lines[len(m.lines)-1] += parts[0]
	if len(parts) > 1 {
		m.lines = append(m.lines, parts[1:]...)
	}
	if len(m.lines) > maxTerminalLines {
		m.lines = append([]string(nil), m.lines[len(m.lines)-maxTerminalLines:]...)
	}
}

func (m *Model) WriteKey(msg tea.KeyPressMsg) {
	if m.session == nil {
		return
	}
	data := encodeKey(msg)
	if len(data) == 0 {
		return
	}
	_ = m.session.Write(data)
}

func encodeKey(msg tea.KeyPressMsg) []byte {
	switch msg.String() {
	case "enter":
		return []byte{'\r'}
	case "tab":
		return []byte{'\t'}
	case "backspace":
		return []byte{0x7f}
	case "delete":
		return []byte{0x1b, '[', '3', '~'}
	case "esc", "escape":
		return []byte{0x1b}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "ctrl+c":
		return []byte{0x03}
	case "ctrl+d":
		return []byte{0x04}
	case "ctrl+z":
		return []byte{0x1a}
	case "ctrl+l":
		return []byte{0x0c}
	default:
		if msg.Text != "" {
			return []byte(msg.Text)
		}
	}
	return nil
}

func (m *Model) Listen() tea.Cmd {
	if m.session == nil {
		return nil
	}
	ch := m.session.Output()
	if ch == nil {
		return nil
	}
	gen := m.generation
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return OutputMsg{Generation: gen, Exited: true}
		}
		msg.Generation = gen
		return msg
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
