package ui

import "testing"

func TestNerdFontEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset", want: true},
		{name: "explicitly disabled", value: "1", set: true, want: false},
		{name: "disabled word", value: "true", set: true, want: false},
		{name: "explicitly enabled", value: "0", set: true, want: true},
		{name: "non boolean opt out", value: "yes", set: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nerdFontEnabled(func(string) (string, bool) { return tt.value, tt.set }); got != tt.want {
				t.Fatalf("nerdFontEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
