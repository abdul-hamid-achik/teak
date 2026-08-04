package editor

import (
	"errors"
	"strings"
	"testing"
)

func clearClipboardEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT", "TEAK_OSC52", "TEAK_CLIPBOARD"} {
		t.Setenv(key, "")
	}
}

func TestOSC52FallbackEngagesOverSSH(t *testing.T) {
	clearClipboardEnv(t)
	t.Setenv("SSH_CONNECTION", "10.0.0.1 51000 10.0.0.2 22")

	seq, err := osc52FallbackFor(errors.New("pbcopy: not found"), "hola")
	if err != nil {
		t.Fatalf("osc52FallbackFor err = %v, want the terminal path to take over", err)
	}
	if !strings.HasPrefix(seq, "\x1b]52;c;") {
		t.Fatalf("sequence = %q, want an OSC 52 escape", seq)
	}
}

func TestOSC52FallbackStaysAwayLocally(t *testing.T) {
	clearClipboardEnv(t)

	copyErr := errors.New("pbcopy: not found")
	seq, err := osc52FallbackFor(copyErr, "hola")
	if seq != "" {
		t.Fatalf("sequence = %q, want no OSC 52 outside SSH sessions", seq)
	}
	if !errors.Is(err, copyErr) {
		t.Fatalf("err = %v, want the original copy error preserved", err)
	}
}

func TestOSC52FallbackNotNeededOnSuccess(t *testing.T) {
	clearClipboardEnv(t)
	t.Setenv("SSH_CONNECTION", "10.0.0.1 51000 10.0.0.2 22")

	seq, err := osc52FallbackFor(nil, "hola")
	if seq != "" || err != nil {
		t.Fatalf("seq = %q err = %v, want no fallback when the OS copy worked", seq, err)
	}
}
