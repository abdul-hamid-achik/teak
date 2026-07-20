package filetree

import (
	"testing"

	"teak/internal/ui"
)

func TestIconForEntryCustomFileTypes(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "service.bp"},
		{name: "api.http"},
		{name: "contract.hitspec"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			icon, color := iconForEntry(Entry{Name: tt.name})
			if icon == IconFileDefault {
				t.Fatalf("iconForEntry(%q) returned default icon", tt.name)
			}
			if color == nil {
				t.Fatalf("iconForEntry(%q) returned nil color", tt.name)
			}
		})
	}
}

func TestIconForEntryASCIIWhenNerdFontIsDisabled(t *testing.T) {
	t.Setenv("TEAK_NO_NERD_FONT", "1")
	if ui.NerdFontEnabled() {
		t.Fatal("test setup did not disable Nerd Font icons")
	}

	tests := []struct {
		name  string
		entry Entry
		want  string
	}{
		{name: "file", entry: Entry{Name: "main.go"}, want: "-"},
		{name: "closed directory", entry: Entry{Name: "src", IsDir: true}, want: ">"},
		{name: "open directory", entry: Entry{Name: "src", IsDir: true, Expanded: true}, want: "v"},
		{name: "loading directory", entry: Entry{Name: "src", IsDir: true, Loading: true}, want: "~"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := iconForEntry(tt.entry)
			if got != tt.want {
				t.Fatalf("iconForEntry(%+v) = %q, want %q", tt.entry, got, tt.want)
			}
		})
	}
}
