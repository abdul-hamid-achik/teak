package editor

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

var benchmarkDiagnosticProjectionSink []Diagnostic
var benchmarkDiagnosticMessageSink string

func TestDiagnosticSetIntersectingReturnsOnlyOverlappingRanges(t *testing.T) {
	diagnostics := []Diagnostic{
		{StartLine: 500, EndLine: 500, Message: "late"},
		{StartLine: 12, EndLine: 12, Message: "last visible"},
		{StartLine: 0, EndLine: 1_000, Message: "spanning"},
		{StartLine: 8, EndLine: 9, Message: "before"},
		{StartLine: 10, EndLine: 10, Message: "first visible"},
		{StartLine: 11, EndLine: 9, Message: "invalid"},
		{StartLine: 13, EndLine: 13, Message: "after"},
	}

	set, err := PrepareDiagnosticSet(context.Background(), diagnostics)
	if err != nil {
		t.Fatalf("PrepareDiagnosticSet() error = %v", err)
	}
	got := diagnosticMessages(set.Intersecting(10, 12))
	want := []string{"last visible", "spanning", "first visible"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Intersecting(10, 12) = %v, want %v", got, want)
	}

	got = diagnosticMessages(set.Intersecting(500, 500))
	want = []string{"late", "spanning"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Intersecting(500, 500) = %v, want %v", got, want)
	}
	if got := set.Intersecting(20, 10); got != nil {
		t.Fatalf("Intersecting(invalid range) = %#v, want nil", got)
	}
}

func TestDiagnosticSetIntersectingLinesSkipsCollapsedGap(t *testing.T) {
	diagnostics := []Diagnostic{
		{StartLine: 50, EndLine: 50, Message: "hidden"},
		{StartLine: 98, EndLine: 98, Message: "visible"},
		{StartLine: 0, EndLine: 99, Message: "spanning"},
	}
	set, err := PrepareDiagnosticSet(context.Background(), diagnostics)
	if err != nil {
		t.Fatalf("PrepareDiagnosticSet() error = %v", err)
	}
	got := diagnosticMessages(set.IntersectingLines([]int{0, 98, 99}))
	want := []string{"visible", "spanning"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IntersectingLines() = %v, want %v", got, want)
	}
}

func TestPrepareDiagnosticSetHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	set, err := PrepareDiagnosticSet(ctx, make([]Diagnostic, 1_000))
	if set != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("PrepareDiagnosticSet(canceled) = (%#v, %v), want (nil, context.Canceled)", set, err)
	}
}

func TestDiagnosticSetMatchesLinearIntersection(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	diagnostics := make([]Diagnostic, 2_000)
	for i := range diagnostics {
		start := rng.Intn(5_000)
		end := start + rng.Intn(80)
		if i%37 == 0 {
			end = start - 1
		}
		diagnostics[i] = Diagnostic{StartLine: start, EndLine: end, Message: string(rune(i + 1))}
	}
	set, err := PrepareDiagnosticSet(context.Background(), append([]Diagnostic(nil), diagnostics...))
	if err != nil {
		t.Fatalf("PrepareDiagnosticSet() error = %v", err)
	}
	for query := 0; query < 500; query++ {
		start := rng.Intn(5_000)
		end := start + rng.Intn(40)
		var want []Diagnostic
		for _, diagnostic := range diagnostics {
			if diagnostic.EndLine >= diagnostic.StartLine && diagnostic.StartLine <= end && diagnostic.EndLine >= start {
				want = append(want, diagnostic)
			}
		}
		if got := set.Intersecting(start, end); !reflect.DeepEqual(got, want) {
			t.Fatalf("Intersecting(%d, %d) differs from linear projection:\n got %#v\nwant %#v", start, end, got, want)
		}
	}
}

func TestDiagnosticMessageAtLineUsesPreparedIntervals(t *testing.T) {
	var editor Editor
	editor.InstallDiagnostics([]Diagnostic{
		{StartLine: 500, EndLine: 500, Message: "  later diagnostic message  "},
		{StartLine: 0, EndLine: 1_000, Message: "  spanning diagnostic message  "},
		{StartLine: 500, EndLine: 500, Message: "second"},
	})
	if got := editor.DiagnosticMessageAtLine(500, 12); got != "later diagn…" {
		t.Fatalf("DiagnosticMessageAtLine() = %q, want %q", got, "later diagn…")
	}
	if got := editor.DiagnosticMessageAtLine(2_000, 12); got != "" {
		t.Fatalf("DiagnosticMessageAtLine(outside) = %q, want empty", got)
	}
	if got := editor.DiagnosticsIntersecting(500, 500); len(got) != 3 {
		t.Fatalf("DiagnosticsIntersecting(500, 500) = %d entries, want 3", len(got))
	}
	if strings.TrimSpace(editor.Diagnostics[0].Message) != "later diagnostic message" {
		t.Fatalf("installed diagnostics did not preserve publication order: %#v", editor.Diagnostics)
	}
}

func diagnosticMessages(diagnostics []Diagnostic) []string {
	messages := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		messages[i] = diagnostic.Message
	}
	return messages
}

func BenchmarkDiagnosticSetIntersectingHundredThousand(b *testing.B) {
	diagnostics := make([]Diagnostic, 100_000)
	for i := range diagnostics {
		diagnostics[i] = Diagnostic{StartLine: i, EndLine: i, Severity: i%4 + 1}
	}
	set, err := PrepareDiagnosticSet(context.Background(), diagnostics)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkDiagnosticProjectionSink = set.Intersecting(50_000, 50_023)
	}
}

func BenchmarkPrepareDiagnosticSetHundredThousand(b *testing.B) {
	diagnostics := make([]Diagnostic, 100_000)
	for i := range diagnostics {
		diagnostics[i] = Diagnostic{StartLine: 100_000 - i, EndLine: 100_000 - i, Severity: i%4 + 1}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		set, err := PrepareDiagnosticSet(context.Background(), diagnostics)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkDiagnosticProjectionSink = set.Intersecting(50_000, 50_000)
	}
}

func BenchmarkDiagnosticMessageAtLineHundredThousand(b *testing.B) {
	diagnostics := make([]Diagnostic, 100_000)
	for i := range diagnostics {
		diagnostics[i] = Diagnostic{StartLine: i, EndLine: i, Message: "diagnostic"}
	}
	var editor Editor
	editor.InstallDiagnostics(diagnostics)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		benchmarkDiagnosticMessageSink = editor.DiagnosticMessageAtLine(50_000, 80)
	}
}
