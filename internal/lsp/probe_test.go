package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestProbeProtocolHonorsContextAndCleansUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-specific")
	}
	path := filepath.Join(t.TempDir(), "hanging-lsp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nwhile :; do sleep 30; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := ProbeProtocol(ctx, ServerConfig{Command: path}, t.TempDir())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ProbeProtocol() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("ProbeProtocol() took %s after cancellation, want bounded cleanup", elapsed)
	}
}

func TestCapabilityNamesAreStableAndMachineReadable(t *testing.T) {
	caps := ServerCapabilities{
		CompletionProvider: &struct {
			ResolveProvider   bool     `json:"resolveProvider,omitempty"`
			TriggerCharacters []string `json:"triggerCharacters,omitempty"`
		}{},
		HoverProvider:           true,
		DefinitionProvider:      map[string]any{},
		ReferencesProvider:      true,
		DocumentSymbolProvider:  true,
		FormattingProvider:      true,
		RangeFormattingProvider: true,
	}
	want := []string{"completion", "hover", "definition", "references", "document_symbols", "formatting", "range_formatting"}
	if got := CapabilityNames(caps); !reflect.DeepEqual(got, want) {
		t.Fatalf("CapabilityNames() = %#v, want %#v", got, want)
	}
}
