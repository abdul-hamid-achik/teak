package termpanel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"teak/internal/ui"
)

type fixtureSession struct {
	out       chan OutputMsg
	writes    chan string
	closed    chan struct{}
	once      sync.Once
	writeErr  error
	resizeErr error
}

func (s *fixtureSession) Write(data []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	select {
	case s.writes <- string(data):
		return nil
	case <-s.closed:
		return errors.New("closed")
	}
}
func (s *fixtureSession) Resize(int, int) error    { return s.resizeErr }
func (s *fixtureSession) Output() <-chan OutputMsg { return s.out }
func (s *fixtureSession) Close() error             { s.once.Do(func() { close(s.closed) }); return nil }

func newFixtureTerminal(t *testing.T) (*terminal, *fixtureSession) {
	t.Helper()
	s := &fixtureSession{out: make(chan OutputMsg, 16), writes: make(chan string, 16), closed: make(chan struct{})}
	r := newTerminal(context.Background(), s, 20, 4, ui.NordTheme())
	t.Cleanup(func() {
		r.close()
		select {
		case <-r.done:
		case <-time.After(time.Second):
			t.Error("terminal worker leaked")
		}
	})
	return r, s
}

func awaitFrame(t *testing.T, r *terminal, predicate func(OutputMsg) bool) OutputMsg {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case msg, ok := <-r.output:
			if !ok {
				t.Fatal("terminal closed before expected frame")
			}
			if predicate(msg) {
				return msg
			}
		case <-timeout.C:
			t.Fatal("terminal did not produce expected frame")
		}
	}
}

func TestTerminalInterpretsControlSequences(t *testing.T) {
	for _, tt := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"carriage return", []string{"progress 1", "\rprogress 2"}, "progress 2"},
		{"erase line", []string{"abcdef\r\x1b[2Kok"}, "ok"},
		{"clear screen", []string{"old\x1b[2J\x1b[Hnew"}, "new"},
		{"cursor address", []string{"abc\x1b[1;2HX"}, "aXc"},
		{"alternate screen", []string{"shell\x1b[?1049happ\x1b[?1049l"}, "shell"},
		{"split unicode and CSI", []string{"caf\xc3", "\xa9\x1b[", "1;1HX"}, "Xafé"},
		{"wide text", []string{"你好"}, "你好"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, s := newFixtureTerminal(t)
			for _, chunk := range tt.chunks {
				s.out <- OutputMsg{Data: []byte(chunk)}
			}
			awaitFrame(t, r, func(msg OutputMsg) bool {
				return msg.frame != nil && strings.TrimSpace(ansi.Strip(msg.frame.content)) == tt.want
			})
		})
	}
}

func TestTerminalScrollbackAndMouseReporting(t *testing.T) {
	r, s := newFixtureTerminal(t)
	s.out <- OutputMsg{Data: []byte("oldest\r\nsecond\r\nthird\r\nfourth\r\nfifth")}
	awaitFrame(t, r, func(m OutputMsg) bool {
		return m.frame != nil && !strings.Contains(ansi.Strip(m.frame.content), "oldest") && strings.Contains(ansi.Strip(m.frame.content), "fifth")
	})
	if err := r.send(tea.MouseWheelMsg{Button: tea.MouseWheelUp}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, r, func(m OutputMsg) bool {
		return m.frame != nil && strings.Contains(ansi.Strip(m.frame.content), "oldest") && m.frame.cursor == nil
	})
	if err := r.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, r, func(m OutputMsg) bool {
		return m.frame != nil && strings.Contains(ansi.Strip(m.frame.content), "fifth") && m.frame.cursor != nil
	})
	s.out <- OutputMsg{Data: []byte("\x1b[?1049h\x1b[?1000h\x1b[?1006hMOUSE")}
	awaitFrame(t, r, func(m OutputMsg) bool {
		return m.frame != nil && strings.Contains(ansi.Strip(m.frame.content), "MOUSE")
	})
	if err := r.send(tea.MouseClickMsg{X: 2, Y: 1, Button: tea.MouseLeft}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-s.writes:
		if got != "\x1b[<0;3;2M" {
			t.Fatalf("mouse report=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("mouse click not sent")
	}
}

func TestTerminalKeysPasteAndReplies(t *testing.T) {
	r, s := newFixtureTerminal(t)
	for _, tt := range []struct {
		key  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "\x01"},
		{tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl}, "\x05"},
		{tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}, "\x15"},
		{tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl}, "\x17"},
		{tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt}, "\x1bb"},
		{tea.KeyPressMsg{Code: 'E', Text: "E", Mod: tea.ModShift}, "E"},
	} {
		if err := r.send(tt.key); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-s.writes:
			if got != tt.want {
				t.Fatalf("key %s = %q, want %q", tt.key.String(), got, tt.want)
			}
		case <-time.After(time.Second):
			t.Fatal("key not written")
		}
	}
	s.out <- OutputMsg{Data: []byte("\x1b[?2004h\x1b[?1hready")}
	awaitFrame(t, r, func(m OutputMsg) bool {
		return m.frame != nil && strings.Contains(ansi.Strip(m.frame.content), "ready")
	})
	if err := r.send(tea.KeyPressMsg{Code: tea.KeyUp}); err != nil {
		t.Fatal(err)
	}
	if got := <-s.writes; got != "\x1bOA" {
		t.Fatalf("application up = %q", got)
	}
	if err := r.send(tea.PasteMsg{Content: "a\nb"}); err != nil {
		t.Fatal(err)
	}
	var got string
	for !strings.HasSuffix(got, "\x1b[201~") {
		select {
		case part := <-s.writes:
			got += part
		case <-time.After(time.Second):
			t.Fatal("paste not written")
		}
	}
	if got != "\x1b[200~a\nb\x1b[201~" {
		t.Fatalf("paste = %q", got)
	}
	s.out <- OutputMsg{Data: []byte("\x1b[6n")}
	select {
	case got := <-s.writes:
		if got != "\x1b[1;6R" {
			t.Fatalf("cursor reply = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("cursor report not answered")
	}
}

func TestTerminalWriteFailureAndUnreadOutputClose(t *testing.T) {
	r, s := newFixtureTerminal(t)
	s.writeErr = errors.New("fixture write failed")
	if err := r.send(tea.KeyPressMsg{Code: tea.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, r, func(m OutputMsg) bool { return m.Err != nil && strings.Contains(m.Err.Error(), "fixture write failed") })
	r2, s2 := newFixtureTerminal(t)
	for range 16 {
		s2.out <- OutputMsg{Data: []byte("line\r\n")}
	}
	r2.close()
	select {
	case <-r2.done:
	case <-time.After(time.Second):
		t.Fatal("unread output prevented close")
	}
}

func TestBlockedPTYInputDoesNotBlockClose(t *testing.T) {
	s := &fixtureSession{out: make(chan OutputMsg, 1), writes: make(chan string), closed: make(chan struct{})}
	r := newTerminal(context.Background(), s, 20, 4, ui.NordTheme())
	defer r.close()
	if err := r.send(tea.PasteMsg{Content: strings.Repeat("x", 65536)}); err != nil {
		t.Fatal(err)
	}
	// A saturated writer must not turn close into a wait on the child reading.
	r.close()
	select {
	case <-r.done:
	case <-time.After(time.Second):
		t.Fatal("blocked PTY input prevented shutdown")
	}
}

func TestTerminalUnchangedSizeDoesNotTouchClosedPTY(t *testing.T) {
	s := &fixtureSession{out: make(chan OutputMsg), writes: make(chan string), closed: make(chan struct{}), resizeErr: errors.New("PTY already closed")}
	r := newTerminal(context.Background(), s, 20, 4, ui.NordTheme())
	defer r.close()
	awaitFrame(t, r, func(OutputMsg) bool { return true })
	if err := r.send(terminalSize{20, 4}); err != nil {
		t.Fatal(err)
	}
	msg := awaitFrame(t, r, func(OutputMsg) bool { return true })
	if msg.Err != nil || msg.Exited {
		t.Fatalf("redundant startup resize hid pending exit output: %v", msg.Err)
	}
}
