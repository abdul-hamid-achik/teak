package app

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"teak/internal/config"
	"teak/internal/lsp"
	"teak/internal/problems"
)

func completeDiagnosticsForTest(t testing.TB, m Model, msg lsp.DiagnosticsMsg) Model {
	t.Helper()
	updatedAny, cmd := m.handleDiagnostics(msg)
	updated := updatedAny.(Model)
	if cmd == nil {
		return updated
	}
	preparedAny, snapshotCmd := updated.Update(cmd())
	prepared := preparedAny.(Model)
	if snapshotCmd == nil {
		return prepared
	}
	completedAny, _ := prepared.Update(snapshotCmd())
	return completedAny.(Model)
}

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

func TestSortProblemsBreaksSameLocationTiesDeterministically(t *testing.T) {
	probs := []problems.Problem{
		{FilePath: "a.go", Line: 1, Col: 2, Severity: 1, Message: "first"},
		{FilePath: "a.go", Line: 1, Col: 2, Severity: 1, Message: "second"},
	}

	sortProblems(probs)

	if got, want := []string{probs[0].Message, probs[1].Message}, []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("same-location problems order = %v, want %v", got, want)
	}
}

func TestHandleDiagnosticsProjectsOutsideUpdate(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	root := t.TempDir()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	path := filepath.Join(root, "main.go")
	updatedAny, cmd := m.handleDiagnostics(lsp.DiagnosticsMsg{
		URI: lsp.FileURI(path),
		Diagnostics: []lsp.Diagnostic{{
			Severity: lsp.SeverityError,
			Range:    lsp.DiagRange{Start: lsp.DiagPosition{Line: 3, Character: 2}},
			Message:  "undefined: value",
		}},
	})
	updated := updatedAny.(Model)
	if cmd == nil {
		t.Fatal("handleDiagnostics returned no preparation command")
	}
	if got := len(updated.fileDiagnostics); got != 0 {
		t.Fatalf("file diagnostics changed inside Update: got %d entries", got)
	}
	if got := updated.problemsPanel.ProblemCount(); got != 0 {
		t.Fatalf("problems changed inside Update: got %d", got)
	}

	preparedAny, snapshotCmd := updated.Update(cmd())
	prepared := preparedAny.(Model)
	if snapshotCmd == nil {
		t.Fatal("prepared diagnostic returned no panel snapshot command")
	}
	if got := prepared.fileDiagnostics[path]; got != int(lsp.SeverityError) {
		t.Fatalf("file severity after preparation = %d, want %d", got, lsp.SeverityError)
	}
	if got := prepared.problemsPanel.ProblemCount(); got != 0 {
		t.Fatalf("panel projection changed before snapshot was prepared: got %d", got)
	}

	completedAny, _ := prepared.Update(snapshotCmd())
	completed := completedAny.(Model)
	if got := completed.problemsPanel.ProblemCount(); got != 1 {
		t.Fatalf("ProblemCount after snapshot = %d, want 1", got)
	}
}

func TestHandleDiagnosticsLatestPreparationWins(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	path := filepath.Join(m.rootDir, "main.go")
	firstAny, firstCmd := m.handleDiagnostics(lsp.DiagnosticsMsg{
		URI:         lsp.FileURI(path),
		Diagnostics: []lsp.Diagnostic{{Severity: lsp.SeverityWarning, Message: "old"}},
	})
	m = firstAny.(Model)
	secondAny, secondCmd := m.handleDiagnostics(lsp.DiagnosticsMsg{
		URI:         lsp.FileURI(path),
		Diagnostics: []lsp.Diagnostic{{Severity: lsp.SeverityError, Message: "new"}},
	})
	m = secondAny.(Model)

	stalePreparation := firstCmd().(diagnosticsPreparedMsg)
	if !stalePreparation.Canceled {
		t.Fatal("superseded preparation did not observe cancellation")
	}
	staleAny, staleCmd := m.Update(stalePreparation)
	m = staleAny.(Model)
	if staleCmd != nil {
		t.Fatal("stale preparation scheduled a panel snapshot")
	}
	if _, ok := m.fileDiagnostics[path]; ok {
		t.Fatal("stale preparation changed the file severity")
	}

	preparedAny, snapshotCmd := m.Update(secondCmd())
	m = preparedAny.(Model)
	if snapshotCmd == nil {
		t.Fatal("latest preparation returned no panel snapshot")
	}
	completedAny, _ := m.Update(snapshotCmd())
	m = completedAny.(Model)
	if got := m.fileDiagnostics[path]; got != int(lsp.SeverityError) {
		t.Fatalf("file severity = %d, want error", got)
	}
	if problem := m.problemsPanel.SelectedProblem(); problem == nil || problem.Message != "new" {
		t.Fatalf("selected problem = %#v, want latest diagnostic", problem)
	}
}

func TestHandleDiagnosticsRejectsVersionChangedDuringPreparation(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	path := filepath.Join(t.TempDir(), "main.go")
	m, err := NewModel(path, filepath.Dir(path), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	version := m.activeEditor().Buffer.Version()
	updatedAny, cmd := m.handleDiagnostics(lsp.DiagnosticsMsg{
		URI:        lsp.FileURI(path),
		Version:    version,
		HasVersion: true,
		Diagnostics: []lsp.Diagnostic{{
			Severity: lsp.SeverityError,
			Message:  "stale after edit",
		}},
	})
	m = updatedAny.(Model)
	if cmd == nil {
		t.Fatal("versioned diagnostic returned no preparation command")
	}
	m.activeEditor().Buffer.InsertAtCursor([]byte("x"))

	completedAny, nextCmd := m.Update(cmd())
	m = completedAny.(Model)
	if nextCmd != nil {
		t.Fatal("obsolete version scheduled a panel snapshot")
	}
	if len(m.activeEditor().Diagnostics) != 0 || m.problemsPanel.ProblemCount() != 0 {
		t.Fatalf("obsolete version was applied: editor=%#v problems=%d", m.activeEditor().Diagnostics, m.problemsPanel.ProblemCount())
	}
}

func TestDiagnosticsPanelRejectsSupersededAggregateSnapshot(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	root := t.TempDir()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	firstPath := filepath.Join(root, "first.go")
	firstAny, firstPrepare := m.handleDiagnostics(lsp.DiagnosticsMsg{
		URI:         lsp.FileURI(firstPath),
		Diagnostics: []lsp.Diagnostic{{Severity: lsp.SeverityWarning, Message: "first"}},
	})
	m = firstAny.(Model)
	firstPreparedAny, firstSnapshot := m.Update(firstPrepare())
	m = firstPreparedAny.(Model)
	if firstSnapshot == nil {
		t.Fatal("first diagnostic returned no aggregate snapshot command")
	}

	secondPath := filepath.Join(root, "second.go")
	secondAny, secondPrepare := m.handleDiagnostics(lsp.DiagnosticsMsg{
		URI:         lsp.FileURI(secondPath),
		Diagnostics: []lsp.Diagnostic{{Severity: lsp.SeverityError, Message: "second"}},
	})
	m = secondAny.(Model)
	secondPreparedAny, secondSnapshot := m.Update(secondPrepare())
	m = secondPreparedAny.(Model)
	if secondSnapshot == nil {
		t.Fatal("second diagnostic returned no aggregate snapshot command")
	}

	staleAny, _ := m.Update(firstSnapshot())
	m = staleAny.(Model)
	if got := m.problemsPanel.ProblemCount(); got != 0 {
		t.Fatalf("superseded aggregate snapshot installed %d problems", got)
	}
	latestAny, _ := m.Update(secondSnapshot())
	m = latestAny.(Model)
	if got := m.problemsPanel.ProblemCount(); got != 2 {
		t.Fatalf("latest aggregate snapshot installed %d problems, want 2", got)
	}
}

func TestHandleDiagnosticsUpdatesOnlyChangedFileAndAncestors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	root := t.TempDir()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	a := filepath.Join(root, "a", "one.go")
	b := filepath.Join(root, "a", "two.go")
	c := filepath.Join(root, "b", "three.go")
	for _, step := range []struct {
		path     string
		severity lsp.DiagSeverity
		clear    bool
		files    map[string]int
		dirs     map[string]int
		count    int
	}{
		{a, lsp.SeverityWarning, false, map[string]int{a: 2}, map[string]int{filepath.Join(root, "a"): 2}, 1},
		{b, lsp.SeverityError, false, map[string]int{a: 2, b: 1}, map[string]int{filepath.Join(root, "a"): 1}, 2},
		{c, lsp.SeverityInfo, false, map[string]int{a: 2, b: 1, c: 3}, map[string]int{filepath.Join(root, "a"): 1, filepath.Join(root, "b"): 3}, 3},
		{b, lsp.SeverityHint, false, map[string]int{a: 2, b: 4, c: 3}, map[string]int{filepath.Join(root, "a"): 2, filepath.Join(root, "b"): 3}, 3},
		{a, 0, true, map[string]int{b: 4, c: 3}, map[string]int{filepath.Join(root, "a"): 4, filepath.Join(root, "b"): 3}, 2},
	} {
		var diagnostics []lsp.Diagnostic
		if !step.clear {
			diagnostics = []lsp.Diagnostic{{
				Severity: step.severity,
				Range:    lsp.DiagRange{Start: lsp.DiagPosition{Line: 3, Character: 2}},
				Message:  step.path,
			}}
		}
		m = completeDiagnosticsForTest(t, m, lsp.DiagnosticsMsg{URI: lsp.FileURI(step.path), Diagnostics: diagnostics})

		if !reflect.DeepEqual(m.fileDiagnostics, step.files) {
			t.Fatalf("file diagnostics after %q = %#v, want %#v", step.path, m.fileDiagnostics, step.files)
		}
		if !reflect.DeepEqual(m.dirDiagnostics, step.dirs) {
			t.Fatalf("directory diagnostics after %q = %#v, want %#v", step.path, m.dirDiagnostics, step.dirs)
		}
		if m.problemsPanel.ProblemCount() != step.count {
			t.Fatalf("ProblemCount after %q = %d, want %d", step.path, m.problemsPanel.ProblemCount(), step.count)
		}
		for path, severity := range step.files {
			if got := m.treeDiagnostics[path]; got != severity {
				t.Fatalf("tree file severity for %q = %d, want %d", path, got, severity)
			}
		}
		for path, severity := range step.dirs {
			if got := m.treeDiagnostics[path]; got != severity {
				t.Fatalf("tree directory severity for %q = %d, want %d", path, got, severity)
			}
		}
	}
}

func TestHandleDiagnosticsLargeBatchKeepsPanelAndTreeEquivalent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	root := t.TempDir()
	m, err := NewModel("", root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()

	const files = 1_000
	for i := 0; i < files; i++ {
		path := filepath.Join(root, fmt.Sprintf("pkg-%02d", i%10), fmt.Sprintf("file-%04d.go", i))
		severity := lsp.DiagSeverity((i / 10 % 4) + 1)
		m = completeDiagnosticsForTest(t, m, lsp.DiagnosticsMsg{
			URI: lsp.FileURI(path),
			Diagnostics: []lsp.Diagnostic{{
				Severity: severity,
				Range:    lsp.DiagRange{Start: lsp.DiagPosition{Line: i, Character: i % 80}},
				Message:  "diagnostic",
			}},
		})
	}

	if got := len(m.fileDiagnostics); got != files {
		t.Fatalf("file diagnostics = %d, want %d", got, files)
	}
	if got := m.problemsPanel.ProblemCount(); got != files {
		t.Fatalf("ProblemCount = %d, want %d", got, files)
	}
	for i := 0; i < 10; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg-%02d", i))
		if got := m.dirDiagnostics[dir]; got != 1 {
			t.Fatalf("directory %q severity = %d, want 1", dir, got)
		}
	}
}

func BenchmarkDispatchDiagnostics(b *testing.B) {
	for _, diagnosticCount := range []int{1, 100_000} {
		b.Run(fmt.Sprintf("diagnostics-%d", diagnosticCount), func(b *testing.B) {
			cfg := config.DefaultConfig()
			cfg.Session.Enabled = false
			cfg.Agent.Enabled = false
			root := b.TempDir()
			m, err := NewModel("", root, cfg)
			if err != nil {
				b.Fatal(err)
			}
			defer m.cleanup()

			msg := lsp.DiagnosticsMsg{
				URI:         lsp.FileURI(filepath.Join(root, "main.go")),
				Diagnostics: make([]lsp.Diagnostic, diagnosticCount),
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				updated, _ := m.Update(msg)
				m = updated.(Model)
			}
		})
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
		name  string
		line  *int
		limit *int
		want  string
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
