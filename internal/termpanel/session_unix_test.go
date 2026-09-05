//go:build unix

package termpanel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPTYExitDrainsOutputAndReapsProcess(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		wantErr    bool
	}{
		{"success", "printf done; exit 0", false},
		{"nonzero", "printf done; exit 7", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shell")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tt.body+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SHELL", path)
			s, err := startSession(context.Background(), t.TempDir(), 20, 4)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			var content strings.Builder
			deadline := time.After(10 * time.Second)
			for {
				select {
				case msg := <-s.Output():
					content.Write(msg.Data)
					if msg.Exited {
						if !strings.Contains(content.String(), "done") {
							t.Fatalf("exit lost output %q", content.String())
						}
						if (msg.Err != nil) != tt.wantErr {
							t.Fatalf("exit error=%v", msg.Err)
						}
						if s.(*unixSession).cmd.ProcessState == nil {
							t.Fatal("process was not reaped before exit notification")
						}
						return
					}
				case <-deadline:
					t.Fatal("PTY did not terminate")
				}
			}
		})
	}
}

func TestPTYCloseWithoutReadingOutputDoesNotLeak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nwhile :; do printf 'busy busy busy busy\\n'; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", path)
	s, err := startSession(context.Background(), t.TempDir(), 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Output():
	case <-time.After(10 * time.Second):
		t.Fatal("PTY never started")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-s.Output():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("closed PTY reader leaked")
		}
	}
}

func TestPTYCloseStopsForegroundJob(t *testing.T) {
	for _, cancelParent := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel_parent_%t", cancelParent), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shell")
			// Job control gives the foreground command a different process group from
			// the shell. Ignoring HUP must not let it survive closing the editor.
			body := "#!/bin/sh\nset -m\n/bin/sh -c 'trap \"\" HUP; echo CHILD=$$; exec /bin/sleep 60'\n"
			if err := os.WriteFile(path, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SHELL", path)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			s, err := startSession(ctx, t.TempDir(), 20, 4)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			var output string
			var child int
			deadline := time.After(10 * time.Second)
			for child == 0 {
				select {
				case msg := <-s.Output():
					if msg.Exited {
						t.Fatalf("shell exited before foreground job: %v", msg.Err)
					}
					output += string(msg.Data)
					if i := strings.Index(output, "CHILD="); i >= 0 && strings.Contains(output[i:], "\n") {
						_, _ = fmt.Sscanf(output[i:], "CHILD=%d", &child)
					}
				case <-deadline:
					t.Fatalf("foreground job did not start: %q", output)
				}
			}
			t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
			if cancelParent {
				cancel()
			} else {
				_ = s.Close()
			}
			deadline = time.After(10 * time.Second)
			for {
				// A killed process can remain a zombie until init reaps it. The PTY
				// reader must finish, and the job must no longer be running.
				if err := syscall.Kill(child, 0); errors.Is(err, syscall.ESRCH) {
					return
				}
				select {
				case <-deadline:
					t.Fatal("foreground job survived closing the PTY")
				case <-time.After(10 * time.Millisecond):
				}
			}
		})
	}
}
