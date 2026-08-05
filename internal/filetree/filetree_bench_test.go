package filetree

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"teak/internal/ui"
)

var (
	benchmarkTreeSnapshot Model
	benchmarkTreeEntries  []Entry
	benchmarkApplyResult  bool
	benchmarkTreeCmd      tea.Cmd
)

// createTestTree creates a file tree model with a specified number of entries
func createTestTree(entryCount int, theme ui.Theme) Model {
	// Create a temporary directory structure
	tmpDir := os.TempDir()
	root := filepath.Join(tmpDir, "teak_test_tree")

	// Clean up and create fresh
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		panic(err)
	}

	// Create test entries
	for i := 0; i < entryCount; i++ {
		if i%5 == 0 {
			// Create a directory
			dirPath := filepath.Join(root, filepath.FromSlash(getTestDirName(i)))
			if err := os.MkdirAll(dirPath, 0o755); err != nil {
				panic(err)
			}
		} else {
			// Create a file
			filePath := filepath.Join(root, getTestFileName(i))
			if err := os.WriteFile(filePath, []byte("test content"), 0o644); err != nil {
				panic(err)
			}
		}
	}

	m := New(root, theme)
	m.SetSize(30, 30) // Set a reasonable size for rendering
	return m
}

func getTestFileName(i int) string {
	return filepath.FromSlash("file_" + string(rune('a'+i%26)) + ".go")
}

func getTestDirName(i int) string {
	return filepath.FromSlash("dir_" + string(rune('A'+i%26)))
}

func BenchmarkFileTreeView10Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(10, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeView30Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(30, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeView50Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(50, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeView100Entries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(100, theme)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeViewWithDiagnostics(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(30, theme)

	// Set diagnostics (use paths that might exist)
	diags := map[string]int{
		"file_a.go": 1, // Error
		"file_b.go": 2, // Warning
		"file_c.go": 3, // Info
	}
	m.SetDiagnostics(diags)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeViewWithGitStatus(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(30, theme)

	// Set git status
	gitStatus := map[string]string{
		"file_a.go": "M", // Modified
		"file_b.go": "A", // Added
		"file_c.go": "D", // Deleted
	}
	m.SetGitStatus(gitStatus)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

func BenchmarkFileTreeFlatEntries(b *testing.B) {
	theme := ui.NordTheme()
	m := createTestTree(50, theme)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// flatEntries checks m.sharedFlatCache before m.cachedFlat, so
		// clearing only cachedFlat left every iteration after the first
		// hitting the shared cache instead of rebuilding. Reset both so each
		// iteration genuinely rebuilds.
		m.cachedFlat = nil
		m.sharedFlatCache = &flatEntryCache{}
		_ = m.flatEntries()
	}
}

func BenchmarkFileTreeFilterInput100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	model.StartFilter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidate := model
		candidate, cmd := candidate.Update(tea.KeyPressMsg{Text: "needle"})
		if cmd == nil || !candidate.FilterPending() {
			b.Fatal("filter input did not schedule a pending projection")
		}
		_ = cmd()
	}
}

func BenchmarkFileTreeFilterUpdate100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	model.StartFilter()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		candidate := model
		candidate, cmd := candidate.Update(tea.KeyPressMsg{Text: "needle"})
		if cmd == nil || !candidate.FilterPending() {
			b.Fatal("filter input did not schedule a pending projection")
		}
	}
}

func BenchmarkFileTreeSnapshotForRefresh100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkTreeSnapshot = model.SnapshotForRefresh()
	}
	if len(benchmarkTreeSnapshot.Entries) != len(model.Entries) {
		b.Fatal("snapshot benchmark did not retain the entry root")
	}
}

func BenchmarkFileTreeDeepCloneComparison100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkTreeEntries = deepCloneEntriesForBenchmark(model.Entries)
	}
	if len(benchmarkTreeEntries) != len(model.Entries) {
		b.Fatal("deep-clone comparison did not copy the entry root")
	}
}

func BenchmarkFileTreeApplyPreparedRefresh100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	result, err := model.prepareRefreshResult(context.Background(), model.Entries, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		candidate := model
		benchmarkApplyResult = candidate.ApplyRefresh(result)
		benchmarkTreeSnapshot = candidate
	}
	if !benchmarkApplyResult || len(benchmarkTreeSnapshot.sharedFlatCache.entries) != len(model.Entries) {
		b.Fatal("apply benchmark did not install the prepared projection")
	}
}

func BenchmarkFileTreeVisibilityProjectionDispatch100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		candidate := model
		_, benchmarkTreeCmd = candidate.ToggleShowHiddenAsync()
	}
	if benchmarkTreeCmd == nil {
		b.Fatal("visibility dispatch did not schedule a projection")
	}
}

func BenchmarkFileTreeApplyInteractiveProjection100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	_, cmd := model.ToggleShowHiddenAsync()
	msg, ok := cmd().(FilterReadyMsg)
	if !ok {
		b.Fatal("visibility projection returned an unexpected message")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		candidate := model
		benchmarkTreeSnapshot, _ = candidate.Update(msg)
	}
	if benchmarkTreeSnapshot.FilterPending() {
		b.Fatal("interactive projection benchmark did not install its result")
	}
}

func BenchmarkFileTreeClearPreparedFilter100000(b *testing.B) {
	model := largeFilterBenchmarkModel(b)
	model.StartFilter()
	pending, cmd := model.Update(tea.KeyPressMsg{Text: "file_a"})
	ready, ok := cmd().(FilterReadyMsg)
	if !ok {
		b.Fatal("filter projection returned an unexpected message")
	}
	model, _ = pending.Update(ready)
	filtered := model.sharedFlatCache.entries
	sourceIndices := model.sharedFlatCache.sourceIndices
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		model.filter = "file_a"
		model.filterActive = true
		model.cachedFlat = filtered
		model.sharedFlatCache.entries = filtered
		model.sharedFlatCache.sourceIndices = sourceIndices
		model.ClearFilter()
		benchmarkTreeSnapshot = model
	}
	if benchmarkTreeSnapshot.Filter() != "" || benchmarkTreeSnapshot.FilterActive() {
		b.Fatal("clear benchmark did not clear the prepared filter")
	}
}

func deepCloneEntriesForBenchmark(entries []Entry) []Entry {
	cloned := make([]Entry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry
		cloned[i].Children = deepCloneEntriesForBenchmark(entry.Children)
	}
	return cloned
}

func largeFilterBenchmarkModel(b *testing.B) Model {
	b.Helper()
	model := NewEmpty(b.TempDir(), ui.DefaultTheme())
	model.Entries = make([]Entry, 100_000)
	for i := range model.Entries {
		name := getTestFileName(i)
		model.Entries[i] = Entry{Name: name, Path: filepath.Join("/workspace", name)}
	}
	_ = model.flatEntries() // the interactive path reuses this immutable source
	return model
}
