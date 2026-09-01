//go:build unix

package termpanel

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type unixSession struct {
	cmd  *exec.Cmd
	file *os.File
	out  chan OutputMsg
}

func (s *unixSession) Write(data []byte) error {
	_, err := s.file.Write(data)
	return err
}

func (s *unixSession) Resize(cols, rows int) error {
	return pty.Setsize(s.file, &pty.Winsize{Cols: uint16(max(1, cols)), Rows: uint16(max(1, rows))})
}

func (s *unixSession) Close() error {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	err := s.file.Close()
	return err
}

func (s *unixSession) Output() <-chan OutputMsg {
	return s.out
}

func (m *Model) Start() error {
	if m.session != nil {
		return nil
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = m.cwd
	cmd.Env = os.Environ()
	file, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(max(1, m.Width)),
		Rows: uint16(max(1, m.Height-1)),
	})
	if err != nil {
		return fmt.Errorf("start terminal: %w", err)
	}
	sess := &unixSession{cmd: cmd, file: file, out: make(chan OutputMsg, 8)}
	m.generation++
	m.session = sess
	go readPTY(file, sess.out)
	return nil
}

func readPTY(r io.Reader, out chan<- OutputMsg) {
	defer close(out)
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			out <- OutputMsg{Data: chunk}
		}
		if err != nil {
			if err != io.EOF {
				out <- OutputMsg{Err: err, Exited: true}
			} else {
				out <- OutputMsg{Exited: true}
			}
			return
		}
	}
}
