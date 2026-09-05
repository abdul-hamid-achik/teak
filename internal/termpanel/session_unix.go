//go:build unix

package termpanel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"teak/internal/toolpath"
)

type unixSession struct {
	cmd    *exec.Cmd
	file   *os.File
	out    chan OutputMsg
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (s *unixSession) Write(data []byte) error { _, err := s.file.Write(data); return err }
func (s *unixSession) Resize(cols, rows int) error {
	conn, err := s.file.SyscallConn()
	if err != nil {
		return err
	}
	var resizeErr error
	err = conn.Control(func(fd uintptr) {
		resizeErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	})
	return errors.Join(err, resizeErr)
}
func (s *unixSession) Close() error {
	var err error
	s.once.Do(func() {
		// Interactive shells put foreground jobs in a separate process group.
		// Closing the master only sends HUP, which a child can ignore; stop that
		// group before cancelling the shell's own group.
		if conn, connErr := s.file.SyscallConn(); connErr == nil {
			_ = conn.Control(func(fd uintptr) {
				if group, groupErr := unix.IoctlGetInt(int(fd), unix.TIOCGPGRP); groupErr == nil && group > 0 && group != s.cmd.Process.Pid {
					_ = syscall.Kill(-group, syscall.SIGKILL)
				}
			})
		}
		s.cancel()
		err = s.file.Close()
	})
	return err
}
func (s *unixSession) Output() <-chan OutputMsg { return s.out }

func startSession(parent context.Context, cwd string, width, height int) (session, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	// Parent cancellation must close the foreground job before killing the
	// session leader; otherwise its PTY no longer exposes the foreground group.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd, err := toolpath.Command(ctx, shell)
	if err != nil {
		cancel()
		return nil, err
	}
	// The PTY creates a new session/process group. Setpgid and Setsid cannot
	// both be requested; retain toolpath's group cancellation callback.
	cmd.SysProcAttr.Setpgid = false
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start terminal: %w", err)
	}
	s := &unixSession{cmd: cmd, file: file, out: make(chan OutputMsg, 8), ctx: ctx, cancel: cancel}
	stop := context.AfterFunc(parent, func() { _ = s.Close() })
	go func() { defer stop(); s.read() }()
	return s, nil
}

func (s *unixSession) read() {
	defer close(s.out)
	defer s.file.Close()
	buf := make([]byte, 4096)
	var readErr error
	for {
		n, err := s.file.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			select {
			case s.out <- OutputMsg{Data: data}:
			case <-s.ctx.Done():
				readErr = s.ctx.Err()
			}
		}
		if readErr != nil {
			break
		}
		if err != nil {
			readErr = err
			break
		}
	}
	// Linux PTYs report EIO when the slave closes. Only Wait determines whether
	// that was successful completion, a nonzero exit, or cancellation.
	if !errors.Is(readErr, io.EOF) && !errors.Is(readErr, syscall.EIO) {
		s.cancel()
	}
	waitErr := s.cmd.Wait()
	if errors.Is(readErr, io.EOF) || errors.Is(readErr, syscall.EIO) {
		readErr = nil
	}
	if waitErr != nil {
		readErr = fmt.Errorf("terminal process: %w", waitErr)
	}
	select {
	case s.out <- OutputMsg{Exited: true, Err: readErr}:
	case <-s.ctx.Done():
	}
}
