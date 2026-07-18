package main

import (
	"bytes"
	"testing"
)

func TestHandleCLI(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantFilePath string
		wantHandled  bool
		wantOutput   string
		wantErr      bool
	}{
		{
			name: "no arguments starts editor",
		},
		{
			name:         "file argument starts editor",
			args:         []string{"notes.md"},
			wantFilePath: "notes.md",
		},
		{
			name:         "additional arguments preserve first file behavior",
			args:         []string{"notes.md", "ignored.md"},
			wantFilePath: "notes.md",
		},
		{
			name:        "long version flag exits early",
			args:        []string{"--version"},
			wantHandled: true,
			wantOutput:  "teak 1.2.3-test\n",
		},
		{
			name:        "short version flag exits early",
			args:        []string{"-v"},
			wantHandled: true,
			wantOutput:  "teak 1.2.3-test\n",
		},
		{
			name:    "unknown long flag is rejected",
			args:    []string{"--verbose"},
			wantErr: true,
		},
		{
			name:    "unknown short flag is rejected",
			args:    []string{"-x"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer

			filePath, handled, err := handleCLI(tt.args, &stdout, "1.2.3-test")

			if (err != nil) != tt.wantErr {
				t.Fatalf("handleCLI() error = %v, wantErr %v", err, tt.wantErr)
			}
			if filePath != tt.wantFilePath {
				t.Errorf("handleCLI() filePath = %q, want %q", filePath, tt.wantFilePath)
			}
			if handled != tt.wantHandled {
				t.Errorf("handleCLI() handled = %v, want %v", handled, tt.wantHandled)
			}
			if got := stdout.String(); got != tt.wantOutput {
				t.Errorf("handleCLI() output = %q, want %q", got, tt.wantOutput)
			}
		})
	}
}

func TestDevelopmentVersionFallback(t *testing.T) {
	if version == "" {
		t.Fatal("version fallback must not be empty")
	}
}
