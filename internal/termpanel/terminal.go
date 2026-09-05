package termpanel

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"teak/internal/ui"
)

const maxPasteBytes = 1 << 20

type terminalFrame struct {
	content       string
	width, height int
	cursor        *tea.Cursor
}

type terminalSize struct{ width, height int }

// terminal owns the emulator on one worker. Neither PTY writes nor escape
// parsing/rendering can block Bubble Tea's Update or View.
type terminal struct {
	input  chan any
	output chan OutputMsg
	done   chan struct{}
	cancel context.CancelFunc
}

func newTerminal(parent context.Context, s session, width, height int, theme ui.Theme) *terminal {
	ctx, cancel := context.WithCancel(parent)
	r := &terminal{input: make(chan any, 64), output: make(chan OutputMsg, 1), done: make(chan struct{}), cancel: cancel}
	e := vt.NewEmulator(width, height)
	e.SetScrollbackSize(maxTerminalLines)
	// Closing the pipe, rather than Emulator.Close, interrupts a blocked reply
	// without concurrently touching the emulator's mutable state.
	pipe := e.InputPipe().(io.Closer)
	stop := context.AfterFunc(ctx, func() { _ = s.Close(); _ = pipe.Close() })
	go r.run(ctx, s, e, pipe, stop, theme)
	return r
}

func (r *terminal) close() { r.cancel() }

func (r *terminal) send(msg any) error {
	if paste, ok := msg.(tea.PasteMsg); ok && len(paste.Content) > maxPasteBytes {
		return fmt.Errorf("terminal paste exceeds %d-byte limit", maxPasteBytes)
	}
	select {
	case <-r.done:
		return fmt.Errorf("terminal has exited")
	default:
	}
	select {
	case r.input <- msg:
		return nil
	default:
		return fmt.Errorf("terminal input queue is full; input was not sent")
	}
}

func (r *terminal) run(ctx context.Context, s session, e *vt.Emulator, pipe io.Closer, stop func() bool, theme ui.Theme) {
	defer close(r.done)
	defer close(r.output)
	defer r.cancel()
	repliesDone := make(chan struct{})
	replyErr := make(chan error, 1)
	go func() {
		defer close(repliesDone)
		defer pipe.Close() // release a blocked emulator write on a PTY failure
		buf := make([]byte, 4096)
		for {
			n, err := e.Read(buf)
			if n > 0 {
				if err := s.Write(buf[:n]); err != nil {
					replyErr <- fmt.Errorf("write terminal: %w", err)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		stop()
		_ = s.Close()
		_ = pipe.Close()
		<-repliesDone
		_ = e.Close()
	}()
	visible, blink, shape := true, true, tea.CursorBlock
	scrollOffset := 0
	mouseModes := make(map[ansi.Mode]bool)
	e.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(v bool) { visible = v },
		CursorStyle:      func(style vt.CursorStyle, b bool) { shape = tea.CursorShape(style); blink = b },
		EnableMode: func(mode ansi.Mode) {
			switch mode {
			case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
				mouseModes[mode] = true
			}
		},
		DisableMode: func(mode ansi.Mode) { delete(mouseModes, mode) },
	})
	applyTheme := func(theme ui.Theme) {
		e.SetDefaultForegroundColor(theme.Editor.GetForeground())
		e.SetDefaultBackgroundColor(theme.Editor.GetBackground())
		e.SetDefaultCursorColor(theme.PromptAccent.GetForeground())
	}
	applyTheme(theme)
	frame := func() *terminalFrame {
		// Resolve defaults per cell: ANSI resets in nested renders must not leak
		// the host terminal palette into a light-theme terminal panel.
		buf := uv.NewRenderBuffer(e.Width(), e.Height())
		for y := 0; y < e.Height(); y++ {
			for x := 0; x < e.Width(); {
				row := e.ScrollbackLen() + y - scrollOffset
				cell := e.CellAt(x, y-scrollOffset)
				if scrollOffset > 0 && row < e.ScrollbackLen() {
					cell = e.ScrollbackCellAt(x, row)
				}
				if cell == nil {
					cell = uv.EmptyCell.Clone()
				} else {
					cell = cell.Clone()
				}
				if cell.Style.Fg == nil {
					cell.Style.Fg = e.ForegroundColor()
				}
				if cell.Style.Bg == nil {
					cell.Style.Bg = e.BackgroundColor()
				}
				// Terminal-generated hyperlinks remain terminal-local data. No OSC
				// title, clipboard or cursor-control sequences are forwarded to Teak.
				buf.SetCell(x, y, cell)
				x += max(1, cell.Width)
			}
		}
		f := &terminalFrame{content: buf.Render(), width: e.Width(), height: e.Height()}
		if visible && scrollOffset == 0 {
			p := e.CursorPosition()
			f.cursor = tea.NewCursor(p.X, p.Y)
			f.cursor.Shape = shape
			f.cursor.Blink = blink
			f.cursor.Color = e.CursorColor()
		}
		return f
	}
	publish := func(msg OutputMsg) {
		msg.frame = frame()
		// Frames are complete snapshots. Retain the latest one while hidden or
		// when the UI is busy; never build an unbounded output backlog.
		select {
		case <-r.output:
		default:
		}
		r.output <- msg
	}
	publish(OutputMsg{})
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case err := <-replyErr:
			publish(OutputMsg{Err: err, Exited: true})
			return
		case msg, ok := <-s.Output():
			if !ok {
				publish(OutputMsg{Exited: true})
				return
			}
			if len(msg.Data) > 0 {
				before := e.ScrollbackLen()
				_, _ = e.Write(msg.Data)
				if e.IsAltScreen() {
					scrollOffset = 0
				} else if scrollOffset > 0 {
					scrollOffset = min(e.ScrollbackLen(), scrollOffset+max(0, e.ScrollbackLen()-before))
				}
			}
			publish(OutputMsg{Err: msg.Err, Exited: msg.Exited})
			if msg.Exited || msg.Err != nil {
				return
			}
		case msg := <-r.input:
			switch msg := msg.(type) {
			case tea.KeyPressMsg:
				if !e.IsAltScreen() {
					switch msg.String() {
					case "shift+pgup":
						scrollOffset = min(e.ScrollbackLen(), scrollOffset+e.Height())
						publish(OutputMsg{})
						continue
					case "shift+pgdown":
						scrollOffset = max(0, scrollOffset-e.Height())
						publish(OutputMsg{})
						continue
					}
				}
				scrollOffset = 0
				if msg.Text != "" && msg.Mod & ^tea.ModShift == 0 {
					e.SendText(msg.Text)
					publish(OutputMsg{})
					continue
				}
				// Bubbles may include printable Text alongside Code. VT's legacy
				// key matching expects control/navigation events without that Text.
				key := uv.KeyPressEvent(msg)
				if key.Mod&(uv.ModCtrl|uv.ModAlt) != 0 {
					key.Text = ""
				}
				e.SendKey(key)
			case tea.PasteMsg:
				scrollOffset = 0
				// Strip embedded bracket delimiters so pasted bytes cannot end the
				// bracketed region early in shells that enable this mode.
				e.Paste(strings.ReplaceAll(strings.ReplaceAll(msg.Content, "\x1b[201~", ""), "\x1b[200~", ""))
			case tea.MouseClickMsg:
				e.SendMouse(uv.MouseClickEvent(msg))
			case tea.MouseReleaseMsg:
				e.SendMouse(uv.MouseReleaseEvent(msg))
			case tea.MouseMotionMsg:
				e.SendMouse(uv.MouseMotionEvent(msg))
			case tea.MouseWheelMsg:
				if e.IsAltScreen() || len(mouseModes) > 0 {
					e.SendMouse(uv.MouseWheelEvent(msg))
				} else {
					switch msg.Button {
					case tea.MouseWheelUp:
						scrollOffset = min(e.ScrollbackLen(), scrollOffset+3)
					case tea.MouseWheelDown:
						scrollOffset = max(0, scrollOffset-3)
					}
				}
			case terminalSize:
				if msg.width == e.Width() && msg.height == e.Height() {
					publish(OutputMsg{})
					continue
				}
				if err := s.Resize(msg.width, msg.height); err != nil {
					publish(OutputMsg{Err: fmt.Errorf("resize terminal: %w", err), Exited: true})
					return
				}
				e.Resize(msg.width, msg.height)
			case ui.Theme:
				applyTheme(msg)
			}
			publish(OutputMsg{})
		}
	}
}
