package search

import (
	"errors"
	"testing"
)

// TestModeConstants tests Mode constants
func TestModeConstants(t *testing.T) {
	if ModeText != 0 {
		t.Errorf("ModeText should be 0, got %d", ModeText)
	}
	if ModeSemantic != 1 {
		t.Errorf("ModeSemantic should be 1, got %d", ModeSemantic)
	}
}

// TestResultStruct tests Result struct
func TestResultStruct(t *testing.T) {
	result := Result{
		FilePath: "/test.go",
		Line:     10,
		Col:      5,
		Preview:  "test preview",
		Score:    0.95,
	}

	if result.FilePath != "/test.go" {
		t.Errorf("Expected FilePath '/test.go', got %q", result.FilePath)
	}
	if result.Line != 10 {
		t.Errorf("Expected Line 10, got %d", result.Line)
	}
	if result.Col != 5 {
		t.Errorf("Expected Col 5, got %d", result.Col)
	}
	if result.Preview != "test preview" {
		t.Errorf("Expected Preview 'test preview', got %q", result.Preview)
	}
	if result.Score != 0.95 {
		t.Errorf("Expected Score 0.95, got %f", result.Score)
	}
}

// TestOpenResultMsg tests OpenResultMsg struct
func TestOpenResultMsg(t *testing.T) {
	msg := OpenResultMsg{
		FilePath: "/test.go",
		Line:     10,
		Col:      5,
	}

	if msg.FilePath != "/test.go" {
		t.Errorf("Expected FilePath '/test.go', got %q", msg.FilePath)
	}
	if msg.Line != 10 {
		t.Errorf("Expected Line 10, got %d", msg.Line)
	}
	if msg.Col != 5 {
		t.Errorf("Expected Col 5, got %d", msg.Col)
	}
}

// TestCloseSearchMsg tests CloseSearchMsg struct
func TestCloseSearchMsg(t *testing.T) {
	msg := CloseSearchMsg{}
	// Just verify it can be created
	_ = msg
}

// TestToggleReplaceMsg tests ToggleReplaceMsg struct
func TestToggleReplaceMsg(t *testing.T) {
	msg := ToggleReplaceMsg{}
	// Just verify it can be created
	_ = msg
}

// TestReplaceOneMsg tests ReplaceOneMsg struct
func TestReplaceOneMsg(t *testing.T) {
	msg := ReplaceOneMsg{
		Query:       "old",
		Replacement: "new",
	}

	if msg.Query != "old" {
		t.Errorf("Expected Query 'old', got %q", msg.Query)
	}
	if msg.Replacement != "new" {
		t.Errorf("Expected Replacement 'new', got %q", msg.Replacement)
	}
}

// TestReplaceAllMsg tests ReplaceAllMsg struct
func TestReplaceAllMsg(t *testing.T) {
	msg := ReplaceAllMsg{
		Query:       "old",
		Replacement: "new",
	}

	if msg.Query != "old" {
		t.Errorf("Expected Query 'old', got %q", msg.Query)
	}
	if msg.Replacement != "new" {
		t.Errorf("Expected Replacement 'new', got %q", msg.Replacement)
	}
}

// TestSearchIndexingMsg tests SearchIndexingMsg struct
func TestSearchIndexingMsg(t *testing.T) {
	msg := SearchIndexingMsg{}
	// Just verify it can be created
	_ = msg
}

// TestSearchResultsMsg tests SearchResultsMsg struct
func TestSearchResultsMsg(t *testing.T) {
	msg := SearchResultsMsg{
		Results: []Result{
			{FilePath: "/test.go", Line: 10, Preview: "test"},
			{FilePath: "/test2.go", Line: 20, Preview: "test2"},
		},
		Err: nil,
	}

	if len(msg.Results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(msg.Results))
	}
	if msg.Err != nil {
		t.Errorf("Expected no error, got %v", msg.Err)
	}
}

// TestSearchResultsMsgWithError tests SearchResultsMsg with error
func TestSearchResultsMsgWithError(t *testing.T) {
	testErr := errors.New("test error")
	msg := SearchResultsMsg{
		Results: nil,
		Err:     testErr,
	}

	if msg.Results != nil {
		t.Errorf("Expected nil results, got %v", msg.Results)
	}
	if msg.Err == nil {
		t.Error("Expected error, got nil")
	}
}

// TestDebounceTickMsg tests debounceTickMsg struct
func TestDebounceTickMsg(t *testing.T) {
	msg := debounceTickMsg{generation: 5}

	if msg.generation != 5 {
		t.Errorf("Expected generation 5, got %d", msg.generation)
	}
}

// TestResultSlice tests Result slice operations
func TestResultSlice(t *testing.T) {
	results := []Result{
		{FilePath: "/test1.go", Line: 1},
		{FilePath: "/test2.go", Line: 2},
		{FilePath: "/test3.go", Line: 3},
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Test access
	if results[0].FilePath != "/test1.go" {
		t.Errorf("Expected first result '/test1.go', got %q", results[0].FilePath)
	}

	// Test empty slice
	empty := []Result{}
	if len(empty) != 0 {
		t.Errorf("Expected 0 results, got %d", len(empty))
	}
}

// TestResultWithDifferentScores tests results with different scores
func TestResultWithDifferentScores(t *testing.T) {
	results := []Result{
		{FilePath: "/test1.go", Score: 0.9},
		{FilePath: "/test2.go", Score: 0.95},
		{FilePath: "/test3.go", Score: 0.85},
	}

	// Verify scores
	expectedScores := []float64{0.9, 0.95, 0.85}
	for i, expected := range expectedScores {
		if results[i].Score != expected {
			t.Errorf("Expected score %f, got %f", expected, results[i].Score)
		}
	}
}

// TestResultWithEmptyFields tests Result with empty/zero fields
func TestResultWithEmptyFields(t *testing.T) {
	result := Result{}

	if result.FilePath != "" {
		t.Errorf("Expected empty FilePath, got %q", result.FilePath)
	}
	if result.Line != 0 {
		t.Errorf("Expected Line 0, got %d", result.Line)
	}
	if result.Col != 0 {
		t.Errorf("Expected Col 0, got %d", result.Col)
	}
	if result.Preview != "" {
		t.Errorf("Expected empty Preview, got %q", result.Preview)
	}
	if result.Score != 0.0 {
		t.Errorf("Expected Score 0.0, got %f", result.Score)
	}
}

// TestMessageTypesInstantiation tests all message types can be instantiated
func TestMessageTypesInstantiation(t *testing.T) {
	// Test all message types can be instantiated
	_ = debounceTickMsg{generation: 1}
	_ = OpenResultMsg{FilePath: "/test.go", Line: 10, Col: 5}
	_ = CloseSearchMsg{}
	_ = ToggleReplaceMsg{}
	_ = ReplaceOneMsg{Query: "old", Replacement: "new"}
	_ = ReplaceAllMsg{Query: "old", Replacement: "new"}
	_ = SearchIndexingMsg{}
	_ = SearchResultsMsg{Results: []Result{{}}}
}

// TestModeValues tests Mode type values
func TestModeValues(t *testing.T) {
	var textMode Mode = ModeText
	var semanticMode Mode = ModeSemantic

	if textMode != 0 {
		t.Errorf("Expected ModeText to be 0, got %d", textMode)
	}
	if semanticMode != 1 {
		t.Errorf("Expected ModeSemantic to be 1, got %d", semanticMode)
	}
}

// TestResultCopy tests Result value semantics
func TestResultCopy(t *testing.T) {
	original := Result{
		FilePath: "/test.go",
		Line:     10,
		Score:    0.95,
	}

	// Copy by value
	copy := original
	copy.FilePath = "/modified.go"

	if original.FilePath != "/test.go" {
		t.Error("Expected original to be unchanged")
	}
	if copy.FilePath != "/modified.go" {
		t.Errorf("Expected copy to be modified, got %q", copy.FilePath)
	}
}

// TestSearchResultsMsgCopy tests SearchResultsMsg value semantics
func TestSearchResultsMsgCopy(t *testing.T) {
	original := SearchResultsMsg{
		Results: []Result{{FilePath: "/test.go"}},
		Err:     nil,
	}

	// Copy by value
	copy := original
	copy.Results = append(copy.Results, Result{FilePath: "/test2.go"})

	if len(original.Results) != 1 {
		t.Errorf("Expected original to have 1 result, got %d", len(original.Results))
	}
	if len(copy.Results) != 2 {
		t.Errorf("Expected copy to have 2 results, got %d", len(copy.Results))
	}
}
