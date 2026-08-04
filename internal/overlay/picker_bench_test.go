package overlay

import (
	"context"
	"fmt"
	"testing"

	"teak/internal/ui"
)

func BenchmarkPickerRefilterTwentyThousand(b *testing.B) {
	items := makePickerBenchmarkItems()
	picker := NewPicker("Files", items, ui.DefaultTheme(), "benchmark-picker")
	picker.input.SetValue("pkg")

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		picker.refilter()
	}
}

func BenchmarkPickerProjectionTwentyThousand(b *testing.B) {
	items := makePickerBenchmarkItems()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		matches, err := filterItemsContext(context.Background(), items, "pkg")
		if err != nil || len(matches) == 0 {
			b.Fatalf("filterItemsContext() = (%d, %v), want matches", len(matches), err)
		}
	}
}

func makePickerBenchmarkItems() []PickerItem {
	items := make([]PickerItem, 20_000)
	for i := range items {
		items[i] = PickerItem{
			Label:       fmt.Sprintf("internal/package%04d/file%05d.go", i%500, i),
			Description: "Go source file",
		}
	}
	return items
}
