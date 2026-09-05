package termpanel

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

const maxTerminalLines = 2000

// OutputMsg carries either raw PTY data internally or a prepared immutable frame.
type OutputMsg struct {
	Generation uint64
	Data       []byte
	Err        error
	Exited     bool
	frame      *terminalFrame
}

// StartedMsg is the result of starting a shell outside Update.
type StartedMsg struct {
	Generation uint64
	Err        error
	terminal   *terminal
}

type Model struct {
	theme               ui.Theme
	cwd                 string
	Width, Height       int
	generation          uint64
	terminal            *terminal
	starting, listening bool
	cancelStart         context.CancelFunc
	frame               *terminalFrame
	lastError           error
	exited              bool
	pending             []any
}

type session interface {
	Write([]byte) error
	Resize(int, int) error
	Close() error
	Output() <-chan OutputMsg
}

func New(theme ui.Theme, cwd string) Model {
	return Model{theme: theme, cwd: cwd, Width: 80, Height: 6}
}

func (m *Model) SetSize(width, height int) {
	width, height = max(1, width), max(1, height)
	if width == m.Width && height == m.Height {
		return
	}
	m.Width, m.Height = width, height
	m.send(terminalSize{width, max(1, height-1)})
}
func (m *Model) SetTheme(theme ui.Theme) { m.theme = theme; m.send(theme) }
func (m *Model) Running() bool           { return m.terminal != nil || m.starting }
func (m *Model) Error() error            { return m.lastError }
func (m *Model) Close() {
	if m.cancelStart != nil {
		m.cancelStart()
		m.cancelStart = nil
	}
	if m.terminal != nil {
		m.terminal.close()
		m.terminal = nil
	}
	m.starting, m.listening = false, false
	m.pending = nil
	m.generation++
}

// Start schedules process creation and emulator setup; repeated toggles do not
// start another shell or create a second reader for the same session.
func (m *Model) Start() tea.Cmd {
	if m.Running() {
		return nil
	}
	m.generation++
	gen, width, height, cwd, theme := m.generation, m.Width, max(1, m.Height-1), m.cwd, m.theme
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelStart = cancel
	m.starting = true
	m.lastError = nil
	m.exited = false
	return func() tea.Msg {
		s, err := startSession(ctx, cwd, width, height)
		if err != nil {
			return StartedMsg{Generation: gen, Err: err}
		}
		return StartedMsg{Generation: gen, terminal: newTerminal(ctx, s, width, height, theme)}
	}
}

func (m *Model) ApplyStarted(msg StartedMsg) tea.Cmd {
	if msg.Generation != m.generation || !m.starting {
		if msg.terminal != nil {
			msg.terminal.close()
		}
		return nil
	}
	m.starting = false
	m.lastError = msg.Err
	if msg.Err != nil {
		m.pending = nil
		if m.cancelStart != nil {
			m.cancelStart()
		}
		return nil
	}
	m.terminal = msg.terminal
	// Size/theme may have changed while process startup was in flight.
	m.send(terminalSize{m.Width, max(1, m.Height-1)})
	m.send(m.theme)
	for _, input := range m.pending {
		m.send(input)
	}
	m.pending = nil
	return m.Listen()
}

func (m *Model) send(msg any) {
	if m.terminal == nil {
		if !m.starting {
			return
		}
		switch input := msg.(type) {
		case tea.PasteMsg:
			if len(input.Content) > maxPasteBytes {
				m.lastError = fmt.Errorf("terminal paste exceeds %d-byte limit", maxPasteBytes)
				return
			}
		case tea.KeyPressMsg:
		default:
			return
		}
		if len(m.pending) >= 32 {
			m.lastError = fmt.Errorf("terminal startup input queue is full; input was not sent")
			return
		}
		m.pending = append(m.pending, msg)
		return
	}
	if err := m.terminal.send(msg); err != nil {
		m.lastError = err
	} else {
		m.lastError = nil
	}
}
func (m *Model) WriteKey(msg tea.KeyPressMsg) { m.send(msg) }
func (m *Model) Paste(msg tea.PasteMsg)       { m.send(msg) }

// Mouse receives panel-local coordinates; the header is not part of the PTY.
func (m *Model) Mouse(msg tea.MouseMsg) {
	mouse := msg.Mouse()
	if mouse.Y < 1 {
		return
	}
	mouse.Y--
	switch msg.(type) {
	case tea.MouseClickMsg:
		m.send(tea.MouseClickMsg(mouse))
	case tea.MouseReleaseMsg:
		m.send(tea.MouseReleaseMsg(mouse))
	case tea.MouseMotionMsg:
		m.send(tea.MouseMotionMsg(mouse))
	case tea.MouseWheelMsg:
		m.send(tea.MouseWheelMsg(mouse))
	}
}

func (m *Model) ApplyOutput(msg OutputMsg) {
	if msg.Generation != m.generation {
		return
	}
	m.listening = false
	if msg.frame != nil && msg.frame.width == m.Width && msg.frame.height == max(1, m.Height-1) {
		m.frame = msg.frame
	}
	if msg.Err != nil {
		m.lastError = msg.Err
	}
	if msg.Exited || msg.Err != nil {
		m.Close()
		m.exited = true
	}
}

func (m *Model) Listen() tea.Cmd {
	if m.terminal == nil || m.listening {
		return nil
	}
	m.listening = true
	out, gen := m.terminal.output, m.generation
	return func() tea.Msg {
		msg, ok := <-out
		if !ok {
			msg = OutputMsg{Exited: true}
		}
		msg.Generation = gen
		return msg
	}
}

// Cursor returns a copy in panel coordinates, including the header row.
func (m Model) Cursor() *tea.Cursor {
	if m.frame == nil || m.frame.cursor == nil || m.terminal == nil || m.frame.width != m.Width || m.frame.height != m.Height-1 {
		return nil
	}
	cursor := *m.frame.cursor
	cursor.Y++
	return &cursor
}

func (m Model) View() string {
	label := " Terminal "
	switch {
	case m.lastError != nil:
		label += fmt.Sprintf("— %s", ansi.Strip(m.lastError.Error()))
	case m.starting:
		label += "(starting…)"
	case m.exited:
		label += "(exited — reopen to restart)"
	case m.terminal == nil:
		label += "(idle)"
	}
	title := m.theme.StatusBar.Render(ansi.Truncate(label, m.Width, ""))
	if pad := m.Width - ansi.StringWidth(title); pad > 0 {
		title += m.theme.StatusBar.Render(strings.Repeat(" ", pad))
	}
	if m.Height <= 1 {
		return title
	}
	body := ""
	if m.frame != nil {
		body = m.frame.content
	}
	// Old-size frames can arrive during resize; keep them inside panel bounds
	// until the matching prepared frame replaces them.
	lines := strings.Split(body, "\n")
	var out strings.Builder
	out.WriteString(title)
	for y := 0; y < m.Height-1; y++ {
		line := ""
		if y < len(lines) {
			line = ansi.Truncate(lines[y], m.Width, "")
		}
		out.WriteByte('\n')
		out.WriteString(line)
		out.WriteString(m.theme.Editor.Render(strings.Repeat(" ", max(0, m.Width-lipgloss.Width(line)))))
	}
	return out.String()
}
