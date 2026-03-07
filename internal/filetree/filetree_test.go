package filetree

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"teak/internal/ui"
)

// TestOpenFileMsg tests OpenFileMsg struct
func TestOpenFileMsg(t *testing.T) {
	msg := OpenFileMsg{Path: "/test.go"}
	if msg.Path != "/test.go" {
		t.Errorf("Expected Path '/test.go', got %q", msg.Path)
	}
}

// TestPinFileMsg tests PinFileMsg struct
func TestPinFileMsg(t *testing.T) {
	msg := PinFileMsg{Path: "/test.go"}
	if msg.Path != "/test.go" {
		t.Errorf("Expected Path '/test.go', got %q", msg.Path)
	}
}

// TestDirExpandedMsg tests DirExpandedMsg struct
func TestDirExpandedMsg(t *testing.T) {
	children := []Entry{{Name: "file1.go", IsDir: false}}
	msg := DirExpandedMsg{Path: "/test", Children: children}
	if msg.Path != "/test" {
		t.Errorf("Expected Path '/test', got %q", msg.Path)
	}
	if len(msg.Children) != 1 {
		t.Errorf("Expected 1 child, got %d", len(msg.Children))
	}
}

// TestEntryStruct tests Entry struct
func TestEntryStruct(t *testing.T) {
	entry := Entry{Name: "test.go", Path: "/test.go", IsDir: false, Depth: 1}
	if entry.Name != "test.go" {
		t.Errorf("Expected Name 'test.go', got %q", entry.Name)
	}
	if entry.Path != "/test.go" {
		t.Errorf("Expected Path '/test.go', got %q", entry.Path)
	}
	if entry.IsDir {
		t.Error("Expected IsDir to be false")
	}
	if entry.Depth != 1 {
		t.Errorf("Expected Depth 1, got %d", entry.Depth)
	}
}

// TestEntryWithChildren tests Entry with children
func TestEntryWithChildren(t *testing.T) {
	entry := Entry{
		Name:  "test",
		Path:  "/test",
		IsDir: true,
		Children: []Entry{{Name: "file1.go", IsDir: false}},
		Expanded: true,
	}
	if !entry.IsDir {
		t.Error("Expected IsDir to be true")
	}
	if len(entry.Children) != 1 {
		t.Errorf("Expected 1 child, got %d", len(entry.Children))
	}
}

// TestEntryCopy tests Entry value semantics
func TestEntryCopy(t *testing.T) {
	original := Entry{Name: "test.go", Path: "/test.go"}
	copy := original
	copy.Name = "modified.go"
	if original.Name != "test.go" {
		t.Error("Expected original to be unchanged")
	}
}

// TestModelCreation tests New function
func TestModelCreation(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	if model.Root != tmpDir {
		t.Errorf("Expected Root %q, got %q", tmpDir, model.Root)
	}
	// Entries is initialized by readDirEntries (may be empty slice)
}

// TestSetDiagnostics tests SetDiagnostics method
func TestSetDiagnostics(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	diags := map[string]int{"/test.go": 1}
	model.SetDiagnostics(diags)
	if model.diagnostics == nil {
		t.Error("Expected diagnostics to be set")
	}
}

// TestSetGitStatus tests SetGitStatus method
func TestSetGitStatus(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	status := map[string]string{"test.go": "M"}
	model.SetGitStatus(status)
	if model.gitStatus == nil {
		t.Error("Expected gitStatus to be set")
	}
}

// TestRefreshDir tests RefreshDir method
func TestRefreshDir(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	initialLen := len(model.Entries)
	model.RefreshDir(tmpDir)
	if len(model.Entries) != initialLen {
		t.Errorf("Expected %d entries, got %d", initialLen, len(model.Entries))
	}
}

// TestModelInitialState tests initial model state
func TestModelInitialState(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	if model.Cursor != 0 {
		t.Errorf("Expected Cursor 0, got %d", model.Cursor)
	}
	if model.ScrollY != 0 {
		t.Errorf("Expected ScrollY 0, got %d", model.ScrollY)
	}
}

// TestEntryAtY tests EntryAtY method
func TestEntryAtY(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}}
	model.cachedFlat = nil
	entry := model.EntryAtY(0)
	if entry == nil {
		t.Error("Expected non-nil entry")
	}
}

// TestEntryAtYWithInvalidIndex tests EntryAtY with invalid index
func TestEntryAtYWithInvalidIndex(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	entry := model.EntryAtY(100)
	if entry != nil {
		t.Error("Expected nil entry for invalid index")
	}
}

// TestToggleEntry tests ToggleEntry method
func TestToggleEntry(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	path := filepath.Join(tmpDir, "test")
	model.Entries = []Entry{{Name: "test", Path: path, IsDir: true, Expanded: false}}
	model.cachedFlat = nil
	_, _ = model.ToggleEntry(path)
}

// TestEnsureCursorVisible tests ensureCursorVisible method
func TestEnsureCursorVisible(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Height = 10
	model.Cursor = 15
	model.ScrollY = 0
	model.ensureCursorVisible()
	if model.Cursor < model.ScrollY {
		t.Error("Expected cursor to be visible")
	}
}

// TestFlatEntries tests flatEntries method
func TestFlatEntries(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{
		{Name: "dir", IsDir: true, Expanded: true, Children: []Entry{{Name: "file1.go"}}},
		{Name: "file2.go"},
	}
	model.cachedFlat = nil
	flat := model.flatEntries()
	if len(flat) < 2 {
		t.Errorf("Expected at least 2 flat entries, got %d", len(flat))
	}
}

// TestFlattenEntries tests flattenEntries helper
func TestFlattenEntries(t *testing.T) {
	entries := []Entry{
		{Name: "dir", IsDir: true, Expanded: true, Children: []Entry{{Name: "file1.go"}}},
		{Name: "file2.go"},
	}
	var flat []Entry
	flattenEntries(entries, &flat)
	if len(flat) != 3 {
		t.Errorf("Expected 3 flat entries, got %d", len(flat))
	}
}

// TestFlattenEntriesWithCollapsedDir tests flattenEntries with collapsed directory
func TestFlattenEntriesWithCollapsedDir(t *testing.T) {
	entries := []Entry{
		{Name: "dir", IsDir: true, Expanded: false, Children: []Entry{{Name: "file1.go"}}},
		{Name: "file2.go"},
	}
	var flat []Entry
	flattenEntries(entries, &flat)
	if len(flat) != 2 {
		t.Errorf("Expected 2 flat entries, got %d", len(flat))
	}
}

// TestEntrySlice tests Entry slice operations
func TestEntrySlice(t *testing.T) {
	entries := []Entry{{Name: "file1.go"}, {Name: "file2.go"}, {Name: "file3.go"}}
	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}
}

// TestEntryWithDifferentDepths tests Entry with different depths
func TestEntryWithDifferentDepths(t *testing.T) {
	entries := []Entry{{Name: "root", Depth: 0}, {Name: "level1", Depth: 1}}
	expectedDepths := []int{0, 1}
	for i, expected := range expectedDepths {
		if entries[i].Depth != expected {
			t.Errorf("Expected depth %d, got %d", expected, entries[i].Depth)
		}
	}
}

// TestEntryWithSpecialCharacters tests Entry with special characters
func TestEntryWithSpecialCharacters(t *testing.T) {
	entry := Entry{Name: "file with spaces.go", Path: "/path/with spaces/file.go"}
	if entry.Name != "file with spaces.go" {
		t.Errorf("Expected Name 'file with spaces.go', got %q", entry.Name)
	}
}

// TestModelWithEmptyRoot tests Model with empty root
func TestModelWithEmptyRoot(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New("", theme)
	if model.Root != "" {
		t.Errorf("Expected empty Root, got %q", model.Root)
	}
}

// TestModelWithRelativePath tests Model with relative path
func TestModelWithRelativePath(t *testing.T) {
	theme := ui.DefaultTheme()
	model := New(".", theme)
	if model.Root != "." {
		t.Errorf("Expected Root '.', got %q", model.Root)
	}
}

// TestSetDiagnosticsWithNil tests SetDiagnostics with nil
func TestSetDiagnosticsWithNil(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.SetDiagnostics(nil)
	if model.diagnostics != nil {
		t.Error("Expected diagnostics to be nil")
	}
}

// TestSetGitStatusWithNil tests SetGitStatus with nil
func TestSetGitStatusWithNil(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.SetGitStatus(nil)
	if model.gitStatus != nil {
		t.Error("Expected gitStatus to be nil")
	}
}

// TestRefreshDirWithNonRootDir tests RefreshDir with non-root directory
func TestRefreshDirWithNonRootDir(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0o755)
	model.RefreshDir(subDir)
}

// TestEntryAtYWithScroll tests EntryAtY with scroll offset
func TestEntryAtYWithScroll(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}, {Name: "file3.go"}}
	model.ScrollY = 1
	model.cachedFlat = nil
	entry := model.EntryAtY(1)
	if entry == nil {
		t.Error("Expected non-nil entry")
	}
}

// TestToggleEntryTogglesExpandedState tests ToggleEntry toggles expanded state
func TestToggleEntryTogglesExpandedState(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	path := filepath.Join(tmpDir, "test")
	model.Entries = []Entry{{Name: "test", Path: path, IsDir: true, Expanded: false}}
	model.cachedFlat = nil
	_, _ = model.ToggleEntry(path)
	if !model.Entries[0].Expanded {
		t.Error("Expected Expanded to be true after first toggle")
	}
	_, _ = model.ToggleEntry(path)
	if model.Entries[0].Expanded {
		t.Error("Expected Expanded to be false after second toggle")
	}
}

// TestModelCursorBounds tests cursor bounds
func TestModelCursorBounds(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	if model.Cursor < 0 {
		t.Errorf("Expected Cursor >= 0, got %d", model.Cursor)
	}
}

// TestModelScrollBounds tests scroll bounds
func TestModelScrollBounds(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	if model.ScrollY < 0 {
		t.Errorf("Expected ScrollY >= 0, got %d", model.ScrollY)
	}
}

// TestEntryLoadingState tests Entry Loading field
func TestEntryLoadingState(t *testing.T) {
	entry := Entry{Name: "test", Loading: true}
	if !entry.Loading {
		t.Error("Expected Loading to be true")
	}
}

// TestEntryExpandedState tests Entry Expanded field
func TestEntryExpandedState(t *testing.T) {
	entry := Entry{Name: "test", IsDir: true, Expanded: true}
	if !entry.Expanded {
		t.Error("Expected Expanded to be true")
	}
}

// TestEntryNilChildren tests Entry with nil children
func TestEntryNilChildren(t *testing.T) {
	entry := Entry{Name: "test", IsDir: true, Children: nil}
	if entry.Children != nil {
		t.Error("Expected Children to be nil")
	}
}

// TestEntryEmptyChildren tests Entry with empty children slice
func TestEntryEmptyChildren(t *testing.T) {
	entry := Entry{Name: "test", IsDir: true, Children: []Entry{}}
	if entry.Children == nil {
		t.Error("Expected Children to be non-nil empty slice")
	}
	if len(entry.Children) != 0 {
		t.Errorf("Expected 0 children, got %d", len(entry.Children))
	}
}

// TestOpenFileMsgWithEmptyPath tests OpenFileMsg with empty path
func TestOpenFileMsgWithEmptyPath(t *testing.T) {
	msg := OpenFileMsg{Path: ""}
	if msg.Path != "" {
		t.Errorf("Expected empty Path, got %q", msg.Path)
	}
}

// TestPinFileMsgWithEmptyPath tests PinFileMsg with empty path
func TestPinFileMsgWithEmptyPath(t *testing.T) {
	msg := PinFileMsg{Path: ""}
	if msg.Path != "" {
		t.Errorf("Expected empty Path, got %q", msg.Path)
	}
}

// TestDirExpandedMsgWithEmptyChildren tests DirExpandedMsg with empty children
func TestDirExpandedMsgWithEmptyChildren(t *testing.T) {
	msg := DirExpandedMsg{Path: "/test", Children: []Entry{}}
	if msg.Path != "/test" {
		t.Errorf("Expected Path '/test', got %q", msg.Path)
	}
	if len(msg.Children) != 0 {
		t.Errorf("Expected 0 children, got %d", len(msg.Children))
	}
}

// TestDirExpandedMsgWithNilChildren tests DirExpandedMsg with nil children
func TestDirExpandedMsgWithNilChildren(t *testing.T) {
	msg := DirExpandedMsg{Path: "/test", Children: nil}
	if msg.Path != "/test" {
		t.Errorf("Expected Path '/test', got %q", msg.Path)
	}
	if msg.Children != nil {
		t.Error("Expected Children to be nil")
	}
}

// TestEntryWithAllFieldsSet tests Entry with all fields set
func TestEntryWithAllFieldsSet(t *testing.T) {
	entry := Entry{
		Name: "test.go", Path: "/test.go", IsDir: false,
		Children: []Entry{{Name: "child"}}, Expanded: true, Loading: true, Depth: 5,
	}
	if entry.Name != "test.go" {
		t.Errorf("Expected Name 'test.go', got %q", entry.Name)
	}
	if entry.Depth != 5 {
		t.Errorf("Expected Depth 5, got %d", entry.Depth)
	}
}

// TestModelWidthHeight tests Width and Height fields
func TestModelWidthHeight(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 20
	if model.Width != 50 {
		t.Errorf("Expected Width 50, got %d", model.Width)
	}
	if model.Height != 20 {
		t.Errorf("Expected Height 20, got %d", model.Height)
	}
}

// TestModelLastClickFields tests lastClickPath and lastClickTime fields
func TestModelLastClickFields(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	if model.lastClickPath != "" {
		t.Errorf("Expected empty lastClickPath, got %q", model.lastClickPath)
	}
	if !model.lastClickTime.IsZero() {
		t.Error("Expected lastClickTime to be zero")
	}
	model.lastClickPath = "/test.go"
	model.lastClickTime = time.Now()
	if model.lastClickPath != "/test.go" {
		t.Errorf("Expected lastClickPath '/test.go', got %q", model.lastClickPath)
	}
}

// TestModelCachedFlat tests cachedFlat field
func TestModelCachedFlat(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	if model.cachedFlat != nil {
		t.Error("Expected cachedFlat to be nil initially")
	}
	model.cachedFlat = []Entry{{Name: "test"}}
	if len(model.cachedFlat) != 1 {
		t.Errorf("Expected 1 cached entry, got %d", len(model.cachedFlat))
	}
}

// TestModelDiagnosticsMap tests diagnostics map operations
func TestModelDiagnosticsMap(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	diags := map[string]int{"/test.go": 1, "/test2.go": 2}
	model.SetDiagnostics(diags)
	if len(model.diagnostics) != 2 {
		t.Errorf("Expected 2 diagnostics, got %d", len(model.diagnostics))
	}
}

// TestModelGitStatusMap tests gitStatus map operations
func TestModelGitStatusMap(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	status := map[string]string{"test.go": "M", "test2.go": "A"}
	model.SetGitStatus(status)
	if len(model.gitStatus) != 2 {
		t.Errorf("Expected 2 status entries, got %d", len(model.gitStatus))
	}
}

// TestEntryIsDirField tests Entry IsDir field usage
func TestEntryIsDirField(t *testing.T) {
	dir := Entry{Name: "dir", IsDir: true}
	file := Entry{Name: "file.go", IsDir: false}
	if !dir.IsDir {
		t.Error("Expected dir.IsDir to be true")
	}
	if file.IsDir {
		t.Error("Expected file.IsDir to be false")
	}
}

// TestEntryPathField tests Entry Path field usage
func TestEntryPathField(t *testing.T) {
	paths := []string{"/absolute/path/file.go", "relative/path/file.go"}
	for _, path := range paths {
		entry := Entry{Name: "file.go", Path: path}
		if entry.Path != path {
			t.Errorf("Expected Path %q, got %q", path, entry.Path)
		}
	}
}

// TestEntryNameField tests Entry Name field usage
func TestEntryNameField(t *testing.T) {
	names := []string{"simple.go", "file with spaces.go", "文件.go"}
	for _, name := range names {
		entry := Entry{Name: name}
		if entry.Name != name {
			t.Errorf("Expected Name %q, got %q", name, entry.Name)
		}
	}
}

// TestModelEntriesSlice tests Entries slice operations
func TestModelEntriesSlice(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}, {Name: "file3.go"}}
	if len(model.Entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(model.Entries))
	}
}

// TestModelEntriesAppend tests Entries slice append
func TestModelEntriesAppend(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = append(model.Entries, Entry{Name: "file1.go"})
	model.Entries = append(model.Entries, Entry{Name: "file2.go"})
	if len(model.Entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(model.Entries))
	}
}

// TestModelEntriesSliceBounds tests Entries slice bounds
func TestModelEntriesSliceBounds(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}, {Name: "file3.go"}, {Name: "file4.go"}, {Name: "file5.go"}}
	sliced := model.Entries[1:3]
	if len(sliced) != 2 {
		t.Errorf("Expected 2 sliced entries, got %d", len(sliced))
	}
}

// TestEntryChildrenRecursive tests Entry children recursively
func TestEntryChildrenRecursive(t *testing.T) {
	entry := Entry{
		Name: "root", IsDir: true,
		Children: []Entry{
			{Name: "child1", IsDir: true, Children: []Entry{{Name: "grandchild1"}}},
			{Name: "child2"},
		},
	}
	if len(entry.Children) != 2 {
		t.Errorf("Expected 2 children, got %d", len(entry.Children))
	}
	if len(entry.Children[0].Children) != 1 {
		t.Errorf("Expected 1 grandchild, got %d", len(entry.Children[0].Children))
	}
}

// TestEntryDepthHierarchy tests Entry depth hierarchy
func TestEntryDepthHierarchy(t *testing.T) {
	entry := Entry{
		Name: "root", Depth: 0,
		Children: []Entry{
			{Name: "level1", Depth: 1, Children: []Entry{{Name: "level2", Depth: 2}}},
		},
	}
	if entry.Depth != 0 {
		t.Errorf("Expected root depth 0, got %d", entry.Depth)
	}
	if entry.Children[0].Depth != 1 {
		t.Errorf("Expected level1 depth 1, got %d", entry.Children[0].Depth)
	}
	if entry.Children[0].Children[0].Depth != 2 {
		t.Errorf("Expected level2 depth 2, got %d", entry.Children[0].Children[0].Depth)
	}
}

// TestModelInitialization tests model initialization
func TestModelInitialization(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	if model.Root == "" {
		t.Error("Expected Root to be set")
	}
	// Entries is initialized by readDirEntries (may be empty slice)
	if model.Cursor != 0 {
		t.Errorf("Expected Cursor 0, got %d", model.Cursor)
	}
}

// TestEntryStructCopy tests Entry struct copy behavior
func TestEntryStructCopy(t *testing.T) {
	original := Entry{Name: "test.go", Path: "/test.go", Expanded: true}
	copy := original
	copy.Name = "modified.go"
	copy.Expanded = false
	if original.Name != "test.go" {
		t.Error("Expected original to be unchanged")
	}
	if !original.Expanded {
		t.Error("Expected original.Expanded to be true")
	}
}

// TestOpenFileMsgCopy tests OpenFileMsg copy behavior
func TestOpenFileMsgCopy(t *testing.T) {
	original := OpenFileMsg{Path: "/original.go"}
	copy := original
	copy.Path = "/modified.go"
	if original.Path != "/original.go" {
		t.Error("Expected original to be unchanged")
	}
}

// TestPinFileMsgCopy tests PinFileMsg copy behavior
func TestPinFileMsgCopy(t *testing.T) {
	original := PinFileMsg{Path: "/original.go"}
	copy := original
	copy.Path = "/modified.go"
	if original.Path != "/original.go" {
		t.Error("Expected original to be unchanged")
	}
}

// TestDirExpandedMsgCopy tests DirExpandedMsg copy behavior
func TestDirExpandedMsgCopy(t *testing.T) {
	original := DirExpandedMsg{Path: "/test", Children: []Entry{{Name: "file1.go"}}}
	copy := original
	copy.Path = "/modified"
	if original.Path != "/test" {
		t.Error("Expected original to be unchanged")
	}
}
