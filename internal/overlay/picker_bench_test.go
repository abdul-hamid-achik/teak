package overlay

import (
	"fmt"
	"testing"

	"teak/internal/ui"
)

func BenchmarkPickerRefilterTwentyThousand(b *testing.B) {
	items := make([]PickerItem, 20_000)
	for i := range items {
		items[i] = PickerItem{
			Label:       fmt.Sprintf("internal/package%04d/file%05d.go", i%500, i),
			Description: "Go source file",
		}
	}
	picker := NewPicker("Files", items, ui.DefaultTheme(), "benchmark-picker")
	picker.input.SetValue("pkg")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		picker.refilter()
	}
}
