package app

import (
	"strings"
	"testing"

	"teak/internal/config"
)

func TestNewModelRejectsInvalidConfiguration(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Editor.TabSize = 0

	model, err := NewModel("", t.TempDir(), cfg)
	if err == nil {
		model.cleanup()
		t.Fatal("NewModel() accepted an invalid configuration")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("NewModel() error = %v, want invalid config context", err)
	}
}
