package clipboard

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestOSC52CopySequenceRoundTrip(t *testing.T) {
	seq, err := OSC52CopySequence("hola mundo")
	if err != nil {
		t.Fatalf("OSC52CopySequence: %v", err)
	}
	if !strings.HasPrefix(seq, "\x1b]52;c;") || !strings.HasSuffix(seq, "\x07") {
		t.Fatalf("sequence = %q, want OSC 52 framing", seq)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\x07")
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload is not base64: %v", err)
	}
	if string(decoded) != "hola mundo" {
		t.Fatalf("decoded = %q, want the original text", decoded)
	}
}

func TestOSC52CopySequenceRejectsOversizedAndInvalid(t *testing.T) {
	if _, err := OSC52CopySequence(strings.Repeat("x", MaxOSC52Bytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized payload err = %v, want ErrTooLarge", err)
	}
	if _, err := OSC52CopySequence(string([]byte{0xff, 0xfe})); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("invalid UTF-8 err = %v, want ErrInvalidUTF8", err)
	}
}

func TestOSC52Available(t *testing.T) {
	clear := func(t *testing.T) {
		t.Helper()
		for _, key := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT", "TEAK_OSC52", "TEAK_CLIPBOARD"} {
			t.Setenv(key, "")
		}
	}

	t.Run("plain local session", func(t *testing.T) {
		clear(t)
		if OSC52Available() {
			t.Fatal("OSC52Available = true without SSH markers or force")
		}
	})

	t.Run("ssh session", func(t *testing.T) {
		clear(t)
		t.Setenv("SSH_CONNECTION", "10.0.0.1 51000 10.0.0.2 22")
		if !OSC52Available() {
			t.Fatal("OSC52Available = false with SSH_CONNECTION set")
		}
	})

	t.Run("forced", func(t *testing.T) {
		clear(t)
		t.Setenv("TEAK_OSC52", "force")
		if !OSC52Available() {
			t.Fatal("OSC52Available = false with TEAK_OSC52=force")
		}
	})

	t.Run("internal mode wins", func(t *testing.T) {
		clear(t)
		t.Setenv("SSH_CONNECTION", "10.0.0.1 51000 10.0.0.2 22")
		t.Setenv("TEAK_CLIPBOARD", "internal")
		if OSC52Available() {
			t.Fatal("OSC52Available = true while TEAK_CLIPBOARD=internal")
		}
	})
}
