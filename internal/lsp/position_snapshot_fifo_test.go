//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package lsp

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestUnopenedFIFOPositionIsDeferredWithoutBlockingProtocolPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server-controlled.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("Mkfifo unavailable: %v", err)
	}

	client := &Client{positionEncoding: positionEncodingUTF16}
	done := make(chan struct{})
	go func() {
		defer close(done)
		location, err := client.locationFromProtocol(Location{
			URI: FileURI(path), StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 1,
		})
		if err != nil {
			t.Errorf("locationFromProtocol() error = %v", err)
			return
		}
		if location.ProtocolEncoding != string(positionEncodingUTF16) {
			t.Errorf("ProtocolEncoding = %q, want utf-16", location.ProtocolEncoding)
		}
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("unopened FIFO caused a blocking protocol-position lookup")
	}
}
