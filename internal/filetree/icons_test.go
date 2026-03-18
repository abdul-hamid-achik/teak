package filetree

import "testing"

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
