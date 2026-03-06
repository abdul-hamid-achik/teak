package app

import (
	"testing"

	"teak/internal/problems"
)

func TestSortProblems(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "b.go", Line: 5, Severity: 2, Message: "warning"},
		{FilePath: "a.go", Line: 10, Severity: 1, Message: "error"},
		{FilePath: "a.go", Line: 1, Severity: 1, Message: "first error"},
		{FilePath: "c.go", Line: 1, Severity: 3, Message: "info"},
	}

	sortProblems(probs)

	// Errors first (severity 1), then warnings (2), then info (3)
	if probs[0].Severity != 1 {
		t.Errorf("probs[0].Severity = %d, want 1", probs[0].Severity)
	}
	// Within same severity, sort by file path
	if probs[0].FilePath != "a.go" {
		t.Errorf("probs[0].FilePath = %q, want 'a.go'", probs[0].FilePath)
	}
	// Within same file, sort by line
	if probs[0].Line != 1 {
		t.Errorf("probs[0].Line = %d, want 1", probs[0].Line)
	}
	if probs[1].Line != 10 {
		t.Errorf("probs[1].Line = %d, want 10", probs[1].Line)
	}
	// Warning comes after errors
	if probs[2].Severity != 2 {
		t.Errorf("probs[2].Severity = %d, want 2", probs[2].Severity)
	}
	// Info last
	if probs[3].Severity != 3 {
		t.Errorf("probs[3].Severity = %d, want 3", probs[3].Severity)
	}
}

func TestSortProblems_Empty(t *testing.T) {
	var probs []problems.Problem
	sortProblems(probs) // should not panic
}

func TestSortProblems_Single(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "a.go", Line: 1, Severity: 1, Message: "error"},
	}
	sortProblems(probs)
	if probs[0].Message != "error" {
		t.Errorf("single element should remain unchanged")
	}
}

func TestFilterLines(t *testing.T) {
	content := "line0\nline1\nline2\nline3\nline4"

	tests := []struct {
		name    string
		line    *int
		limit   *int
		want    string
	}{
		{"no filter", nil, nil, content},
		{"start at line 2 (1-based)", intPtr(2), nil, "line1\nline2\nline3\nline4"},
		{"start at line 1, limit 2", intPtr(1), intPtr(2), "line0\nline1"},
		{"start at line 3, limit 1", intPtr(3), intPtr(1), "line2"},
		{"line beyond end", intPtr(100), nil, ""},
		{"limit 0", intPtr(1), intPtr(0), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterLines(content, tt.line, tt.limit)
			if got != tt.want {
				t.Errorf("filterLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

func intPtr(i int) *int { return &i }

// TestSortProblems_EqualSeverityDifferentPaths tests sorting with same severity but different paths.
func TestSortProblems_EqualSeverityDifferentPaths(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "z.go", Line: 1, Severity: 1, Message: "z error"},
		{FilePath: "a.go", Line: 1, Severity: 1, Message: "a error"},
		{FilePath: "m.go", Line: 1, Severity: 1, Message: "m error"},
	}
	sortProblems(probs)

	expected := []string{"a.go", "m.go", "z.go"}
	for i, want := range expected {
		if probs[i].FilePath != want {
			t.Errorf("probs[%d].FilePath = %q, want %q", i, probs[i].FilePath, want)
		}
	}
}

// TestSortProblems_EqualSeverityAndPath tests sorting by line within same file and severity.
func TestSortProblems_EqualSeverityAndPath(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "a.go", Line: 30, Severity: 2, Message: "line 30"},
		{FilePath: "a.go", Line: 5, Severity: 2, Message: "line 5"},
		{FilePath: "a.go", Line: 15, Severity: 2, Message: "line 15"},
		{FilePath: "a.go", Line: 1, Severity: 2, Message: "line 1"},
	}
	sortProblems(probs)

	expectedLines := []int{1, 5, 15, 30}
	for i, want := range expectedLines {
		if probs[i].Line != want {
			t.Errorf("probs[%d].Line = %d, want %d", i, probs[i].Line, want)
		}
	}
}

// TestSortProblems_AllSameSeverity tests that equal-severity items still sort by path then line.
func TestSortProblems_AllSameSeverity(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "b.go", Line: 10, Severity: 1},
		{FilePath: "a.go", Line: 20, Severity: 1},
		{FilePath: "b.go", Line: 5, Severity: 1},
		{FilePath: "a.go", Line: 1, Severity: 1},
	}
	sortProblems(probs)

	// Should be: a.go:1, a.go:20, b.go:5, b.go:10
	type expect struct {
		path string
		line int
	}
	expected := []expect{
		{"a.go", 1}, {"a.go", 20}, {"b.go", 5}, {"b.go", 10},
	}
	for i, e := range expected {
		if probs[i].FilePath != e.path || probs[i].Line != e.line {
			t.Errorf("probs[%d] = {%q, %d}, want {%q, %d}",
				i, probs[i].FilePath, probs[i].Line, e.path, e.line)
		}
	}
}

// TestSortProblems_AlreadySorted tests that already-sorted input stays the same.
func TestSortProblems_AlreadySorted(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "a.go", Line: 1, Severity: 1},
		{FilePath: "a.go", Line: 10, Severity: 1},
		{FilePath: "b.go", Line: 1, Severity: 2},
		{FilePath: "c.go", Line: 1, Severity: 3},
	}
	sortProblems(probs)

	if probs[0].FilePath != "a.go" || probs[0].Line != 1 || probs[0].Severity != 1 {
		t.Error("already sorted input was rearranged")
	}
	if probs[3].FilePath != "c.go" || probs[3].Severity != 3 {
		t.Error("already sorted input was rearranged")
	}
}

// TestSortProblems_ReverseSorted tests worst-case reverse-sorted input.
func TestSortProblems_ReverseSorted(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "c.go", Line: 99, Severity: 3},
		{FilePath: "b.go", Line: 50, Severity: 2},
		{FilePath: "a.go", Line: 10, Severity: 1},
	}
	sortProblems(probs)

	if probs[0].Severity != 1 || probs[1].Severity != 2 || probs[2].Severity != 3 {
		t.Errorf("reverse sorted input not correctly sorted: got severities %d,%d,%d",
			probs[0].Severity, probs[1].Severity, probs[2].Severity)
	}
}

// TestFilterLines_NegativeLine tests that a negative line number is clamped to 0.
func TestFilterLines_NegativeLine(t *testing.T) {
	content := "line0\nline1\nline2"
	line := -5
	got := filterLines(content, &line, nil)
	if got != content {
		t.Errorf("negative line: got %q, want %q", got, content)
	}
}

// TestFilterLines_LimitExceedsContent tests limit larger than available lines.
func TestFilterLines_LimitExceedsContent(t *testing.T) {
	content := "line0\nline1"
	line := 1
	limit := 100
	got := filterLines(content, &line, &limit)
	if got != content {
		t.Errorf("limit exceeds content: got %q, want %q", got, content)
	}
}

// TestFilterLines_EmptyContent tests filtering empty content.
func TestFilterLines_EmptyContent(t *testing.T) {
	got := filterLines("", nil, nil)
	if got != "" {
		t.Errorf("empty content: got %q, want %q", got, "")
	}
}

// TestFilterLines_SingleLine tests filtering content with exactly one line.
func TestFilterLines_SingleLine(t *testing.T) {
	content := "only line"
	tests := []struct {
		name  string
		line  *int
		limit *int
		want  string
	}{
		{"no filter", nil, nil, "only line"},
		{"line 1", intPtr(1), nil, "only line"},
		{"line 1 limit 1", intPtr(1), intPtr(1), "only line"},
		{"line 2 out of bounds", intPtr(2), nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterLines(content, tt.line, tt.limit)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFilterLines_OnlyLimit tests limit without a line offset.
func TestFilterLines_OnlyLimit(t *testing.T) {
	content := "a\nb\nc\nd\ne"
	limit := 3
	got := filterLines(content, nil, &limit)
	want := "a\nb\nc"
	if got != want {
		t.Errorf("only limit: got %q, want %q", got, want)
	}
}
