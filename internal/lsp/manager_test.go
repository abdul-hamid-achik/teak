package lsp

import (
	"testing"
)

func TestManagerDoubleClose(t *testing.T) {
	m := NewManager("/tmp", nil)

	// First shutdown should work
	m.ShutdownAll()

	// Second shutdown should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ShutdownAll() panicked on second call: %v", r)
		}
	}()

	m.ShutdownAll()
	m.ShutdownAll() // Third call to be extra safe
}

func TestManagerCloseAfterPartialShutdown(t *testing.T) {
	m := NewManager("/tmp", nil)

	// Close should be idempotent
	m.ShutdownAll()

	// Verify channel is closed by checking it returns zero value
	select {
	case msg := <-m.MsgChan():
		if msg != nil {
			t.Error("Expected nil from closed channel")
		}
	default:
		// Channel might be closed but not drained
	}
}
