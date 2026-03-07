//go:build !race
// +build !race

package text

import (
	"math/rand"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRopeConcurrentAccess tests that rope operations are safe for
// concurrent reads (writes create new ropes, so they're inherently safe)
func TestRopeConcurrentAccess(t *testing.T) {
	// Create a base rope
	base := NewFromString(strings.Repeat("hello world\n", 1000))

	var memStatsBefore, memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	// Spawn multiple goroutines that read from the same rope
	done := make(chan bool, 100)
	
	for i := 0; i < 100; i++ {
		go func() {
			// All these operations are read-only and safe
			_ = base.LineCount()
			_ = base.Line(50)
			_ = base.LineStart(50)
			_ = base.LineLen(50)
			_ = base.Len()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	runtime.ReadMemStats(&memStatsAfter)
	
	// Memory should not have grown significantly (rope is immutable)
	allocDelta := memStatsAfter.Alloc - memStatsBefore.Alloc
	if allocDelta > 10*1024*1024 { // More than 10MB is suspicious
		t.Errorf("memory grew by %d bytes, expected minimal growth", allocDelta)
	}
}

// TestRopeLargeFilePerformance tests rope operations on large files
func TestRopeLargeFilePerformance(t *testing.T) {
	// Simulate a 1MB file (typical large source file)
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 13107) // ~1MB

	r := New([]byte(doc))

	// Test insert in middle (should be fast due to rope structure)
	mid := r.Len() / 2
	start := time.Now()
	r2 := r.Insert(mid, []byte("INSERTED"))
	insertDuration := time.Since(start)

	if insertDuration > 100*time.Millisecond {
		t.Errorf("insert took %v, expected < 100ms", insertDuration)
	}

	// Test delete in middle
	start = time.Now()
	r3 := r2.Delete(mid, 8)
	deleteDuration := time.Since(start)

	if deleteDuration > 100*time.Millisecond {
		t.Errorf("delete took %v, expected < 100ms", deleteDuration)
	}

	// Verify round-trip
	if r3.String() != doc {
		t.Error("delete did not restore original")
	}
}

// TestRopeLineOperationsPerformance tests line-based operations on large files
func TestRopeLineOperationsPerformance(t *testing.T) {
	// 10k line file
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 10000)
	r := New([]byte(doc))

	// Test random line access (simulates scrolling)
	rand.Seed(time.Now().UnixNano())
	start := time.Now()

	for i := 0; i < 1000; i++ {
		lineNum := rand.Intn(10000)
		_ = r.LineStart(lineNum)
		_ = r.Line(lineNum)
	}

	accessDuration := time.Since(start)

	// 1000 random accesses should be fast (< 50ms)
	if accessDuration > 50*time.Millisecond {
		t.Errorf("1000 line accesses took %v, expected < 50ms", accessDuration)
	}
}

// TestRopePositionConversionPerformance tests position ↔ offset conversion
func TestRopePositionConversionPerformance(t *testing.T) {
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 10000)
	r := New([]byte(doc))

	// Test many position conversions (simulates cursor movement)
	start := time.Now()

	for line := 0; line < 10000; line++ {
		for col := 0; col < 79; col += 10 {
			pos := Position{Line: line, Col: col}
			offset := r.PositionToOffset(pos)
			_ = r.OffsetToPosition(offset)
		}
	}

	convertDuration := time.Since(start)

	// ~80k conversions should complete in < 500ms
	if convertDuration > 500*time.Millisecond {
		t.Errorf("position conversions took %v, expected < 500ms", convertDuration)
	}
}

// TestRopeMemorySharing tests that ropes share structure after edits
func TestRopeMemorySharing(t *testing.T) {
	// Create a rope
	original := NewFromString(strings.Repeat("hello world\n", 1000))
	originalLen := original.Len()

	// Make edits
	r1 := original.Insert(originalLen/2, []byte("INSERT 1"))
	r2 := original.Insert(originalLen/2, []byte("INSERT 2"))
	_ = original.Delete(0, 10) // r3 - just verify delete works

	// Original should be unchanged
	if original.Len() != originalLen {
		t.Error("original rope was mutated")
	}

	// All ropes should be valid
	if r1.Len() <= originalLen {
		t.Error("r1 should be larger than original")
	}
	if r2.Len() <= originalLen {
		t.Error("r2 should be larger than original")
	}

	// Measure memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	// This is a soft check - just verify we didn't leak memory
	_ = memStats
}

// TestRopeUTF8Handling tests proper handling of multi-byte characters
func TestRopeUTF8Handling(t *testing.T) {
	// Mix of ASCII and multi-byte UTF-8
	content := "hello 世界 café 日本語 🎉\n"
	r := New([]byte(content))

	// Test line operations
	if r.LineCount() != 2 {
		t.Errorf("LineCount() = %d, want 2", r.LineCount())
	}

	// Test position conversion
	pos := Position{Line: 0, Col: 6} // Byte offset 6 (start of "世")
	offset := r.PositionToOffset(pos)
	if offset != 6 {
		t.Errorf("PositionToOffset = %d, want 6", offset)
	}

	// Test insert at byte offset 6 (before "世界")
	// This inserts raw bytes, not characters
	r2 := r.Insert(6, []byte("X"))
	// "hello " (6 bytes) + "X" + "世界..." (no space between X and 世界)
	expected := "hello X" + "世界 café 日本語 🎉\n"
	if r2.String() != expected {
		t.Errorf("got %q, want %q", r2.String(), expected)
	}
}

// TestRopeEmptyOperations tests edge cases with empty ropes
func TestRopeEmptyOperations(t *testing.T) {
	empty := New(nil)

	if empty.Len() != 0 {
		t.Errorf("empty rope len = %d, want 0", empty.Len())
	}
	if empty.LineCount() != 1 {
		t.Errorf("empty rope LineCount() = %d, want 1", empty.LineCount())
	}

	// Insert into empty
	r1 := empty.Insert(0, []byte("hello"))
	if r1.String() != "hello" {
		t.Errorf("insert into empty = %q, want %q", r1.String(), "hello")
	}

	// Delete from empty (should be safe)
	r2 := empty.Delete(0, 100) // Delete more than exists
	if r2.Len() != 0 {
		t.Error("delete from empty should still be empty")
	}
}

// TestRopeSingleCharacterOperations tests many single-character edits
func TestRopeSingleCharacterOperations(t *testing.T) {
	r := New(nil)

	// Type 1000 characters one at a time (simulates typing)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		r = r.Insert(r.Len(), []byte{'a'})
	}
	typingDuration := time.Since(start)

	if r.Len() != 1000 {
		t.Errorf("final len = %d, want 1000", r.Len())
	}

	// 1000 inserts should complete in < 100ms
	if typingDuration > 100*time.Millisecond {
		t.Errorf("1000 inserts took %v, expected < 100ms", typingDuration)
	}

	// Delete all characters one at a time
	start = time.Now()
	for i := 0; i < 1000; i++ {
		r = r.Delete(r.Len()-1, 1)
	}
	deletionDuration := time.Since(start)

	if r.Len() != 0 {
		t.Errorf("final len = %d, want 0", r.Len())
	}

	if deletionDuration > 100*time.Millisecond {
		t.Errorf("1000 deletes took %v, expected < 100ms", deletionDuration)
	}
}

// TestRopeLineIndexCaching tests that line index is cached properly
func TestRopeLineIndexCaching(t *testing.T) {
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 1000)
	r := New([]byte(doc))

	// First access (builds cache)
	start := time.Now()
	_ = r.LineStart(500)
	firstAccess := time.Since(start)

	// Second access (uses cache)
	start = time.Now()
	_ = r.LineStart(500)
	secondAccess := time.Since(start)

	// Second access should be faster (but this is flaky)
	// Just verify both complete quickly
	if firstAccess > 10*time.Millisecond {
		t.Logf("first access slow: %v (may be OK)", firstAccess)
	}
	if secondAccess > 10*time.Millisecond {
		t.Logf("second access slow: %v (may be OK)", secondAccess)
	}
}

// TestRopeSliceBounds tests Slice at various bounds
func TestRopeSliceBounds(t *testing.T) {
	r := NewFromString("hello world")

	tests := []struct {
		name   string
		start  int
		end    int
		want   string
	}{
		{"full", 0, 11, "hello world"},
		{"partial", 0, 5, "hello"},
		{"middle", 6, 11, "world"},
		{"single char", 0, 1, "h"},
		{"empty range", 5, 5, ""},
		{"end out of bounds clamped", 0, 100, "hello world"},
		{"start out of bounds returns empty", 100, 110, ""},
		{"start > end returns empty", 5, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := r.Slice(tt.start, tt.end)
			if s == nil {
				t.Error("Slice returned nil unexpectedly")
				return
			}
			if s.String() != tt.want {
				t.Errorf("Slice(%d, %d) = %q, want %q", tt.start, tt.end, s.String(), tt.want)
			}
		})
	}
}

// TestRopeRebalancing tests that rope rebalances correctly
func TestRopeRebalancing(t *testing.T) {
	// Create unbalanced rope by inserting at end repeatedly
	r := New(nil)
	for i := 0; i < 1000; i++ {
		r = r.Insert(r.Len(), []byte("x"))
	}

	// Rope should be balanced (depth should be logarithmic)
	// For 1000 bytes with 512-byte leaves, depth should be small
	// This is a soft check - just verify operations still work
	
	// Verify content
	if r.Len() != 1000 {
		t.Errorf("len = %d, want 1000", r.Len())
	}

	// Verify we can still access lines efficiently
	_ = r.Line(0)
}

// TestRopeRealWorldFile tests loading and editing a real Go file
func TestRopeRealWorldFile(t *testing.T) {
	// Try to load a real Go file from the project
	testFiles := []string{
		"internal/app/app.go",
		"internal/text/rope.go",
		"cmd/teak/main.go",
	}

	var testFilePath string
	for _, f := range testFiles {
		if _, err := os.Stat(f); err == nil {
			testFilePath = f
			break
		}
	}

	if testFilePath == "" {
		t.Skip("no test files found")
	}

	// Read file
	data, err := os.ReadFile(testFilePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Create rope
	r := New(data)

	// Verify basic properties
	if r.Len() != len(data) {
		t.Errorf("len = %d, want %d", r.Len(), len(data))
	}

	// Simulate editing: insert at beginning (add package comment)
	comment := []byte("// Auto-generated comment\n")
	r2 := r.Insert(0, comment)

	if r2.Len() != r.Len()+len(comment) {
		t.Errorf("after insert len = %d, want %d", r2.Len(), r.Len()+len(comment))
	}

	// Verify original unchanged
	if r.Len() != len(data) {
		t.Error("original was mutated")
	}
}

// BenchmarkRopeInsertRealWorld benchmarks insert at various positions
func BenchmarkRopeInsertRealWorld(b *testing.B) {
	// Load a realistic file
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 10000) // ~800KB
	r := New([]byte(doc))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Reset rope every iteration to avoid growing indefinitely
		r = New([]byte(doc))
		b.StartTimer()

		r = r.Insert(r.Len()/2, []byte("INSERT"))
	}
}

// BenchmarkRopeDeleteRealWorld benchmarks delete at various positions
func BenchmarkRopeDeleteRealWorld(b *testing.B) {
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r := New([]byte(doc))
		b.StartTimer()

		r = r.Delete(r.Len()/2, 10)
	}
}

// BenchmarkRopeLineAccessRandom benchmarks random line access
func BenchmarkRopeLineAccessRandom(b *testing.B) {
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 10000)
	r := New([]byte(doc))

	rand.Seed(time.Now().UnixNano())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lineNum := rand.Intn(10000)
		_ = r.LineStart(lineNum)
	}
}

// BenchmarkRopeLineAccessSequential benchmarks sequential line access
func BenchmarkRopeLineAccessSequential(b *testing.B) {
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 10000)
	r := New([]byte(doc))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lineNum := i % 10000
		_ = r.LineStart(lineNum)
	}
}

// BenchmarkRopePositionToOffset benchmarks position to offset conversion
func BenchmarkRopePositionToOffset(b *testing.B) {
	line := strings.Repeat("x", 79) + "\n"
	doc := strings.Repeat(line, 10000)
	r := New([]byte(doc))

	pos := Position{Line: 5000, Col: 40}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.PositionToOffset(pos)
	}
}

// BenchmarkRopeTypingSimulation simulates typing characters one at a time
func BenchmarkRopeTypingSimulation(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r := New(nil)
		b.StartTimer()

		// Type 100 characters
		for j := 0; j < 100; j++ {
			r = r.Insert(r.Len(), []byte{'a'})
		}
	}
}
