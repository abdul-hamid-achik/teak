package filetree

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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

// TestUpdateWithKeyPress tests Update with key press
func TestUpdateWithKeyPress(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}}
	model.Height = 10
	
	// Test down key
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.Cursor != 1 {
		t.Errorf("Expected Cursor 1, got %d", model.Cursor)
	}
	
	// Test up key
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if model.Cursor != 0 {
		t.Errorf("Expected Cursor 0, got %d", model.Cursor)
	}
}

// TestUpdateWithEnterKey tests Update with enter key on directory
func TestUpdateWithEnterKey(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	path := filepath.Join(tmpDir, "testdir")
	os.MkdirAll(path, 0o755)
	model.Entries = []Entry{{Name: "testdir", Path: path, IsDir: true, Expanded: false}}
	model.cachedFlat = nil
	
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.Entries[0].Expanded {
		t.Error("Expected directory to be expanded")
	}
}

// TestUpdateWithEnterKeyOnFile tests Update with enter key on file
func TestUpdateWithEnterKeyOnFile(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file.go", Path: filepath.Join(tmpDir, "file.go"), IsDir: false}}
	
	model, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Error("Expected PinFileMsg command")
		return
	}
	msg := cmd()
	if _, ok := msg.(PinFileMsg); !ok {
		t.Error("Expected PinFileMsg")
	}
}

// TestUpdateWithMouseClick tests Update with mouse click
func TestUpdateWithMouseClick(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}}
	model.Height = 10
	
	// Create a mock mouse click
	mouseMsg := tea.MouseClickMsg{}
	model, _ = model.Update(mouseMsg)
}

// TestUpdateWithMouseWheel tests Update with mouse wheel
func TestUpdateWithMouseWheel(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}, {Name: "file3.go"}}
	model.Height = 2
	model.ScrollY = 1
	
	// Test wheel down
	wheelMsg := tea.MouseWheelMsg{}
	model, _ = model.Update(wheelMsg)
}

// TestUpdateWithDirExpanded tests Update with DirExpandedMsg
func TestUpdateWithDirExpanded(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	path := filepath.Join(tmpDir, "testdir")
	model.Entries = []Entry{{Name: "testdir", Path: path, IsDir: true, Expanded: true, Loading: true}}
	
	children := []Entry{{Name: "child.go", Path: filepath.Join(path, "child.go")}}
	expandedMsg := DirExpandedMsg{Path: path, Children: children}
	
	model, _ = model.Update(expandedMsg)
	if model.Entries[0].Loading {
		t.Error("Expected Loading to be false")
	}
	if len(model.Entries[0].Children) != 1 {
		t.Errorf("Expected 1 child, got %d", len(model.Entries[0].Children))
	}
}

// TestUpdateWithUnknownMsg tests Update with unknown message type
func TestUpdateWithUnknownMsg(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	
	model, cmd := model.Update("unknown message")
	if cmd != nil {
		t.Error("Expected nil command for unknown message")
	}
}

// TestHandleKeyPressBounds tests handleKeyPress cursor bounds
func TestHandleKeyPressBounds(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}}
	model.Cursor = 0
	model.Height = 10
	
	// Try to go up from top
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if model.Cursor != 0 {
		t.Errorf("Expected Cursor to stay at 0, got %d", model.Cursor)
	}
	
	// Try to go down beyond bounds
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if model.Cursor > len(model.flatEntries())-1 {
		t.Errorf("Expected Cursor within bounds, got %d", model.Cursor)
	}
}

// TestHandleMouseClickOnDirectory tests handleMouseClick on directory
func TestHandleMouseClickOnDirectory(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	path := filepath.Join(tmpDir, "testdir")
	os.MkdirAll(path, 0o755)
	model.Entries = []Entry{{Name: "testdir", Path: path, IsDir: true, Expanded: false}}
	model.Height = 10
	model.cachedFlat = nil
	
	// Simulate click at Y position 0
	mouseMsg := tea.MouseClickMsg{}
	model, _ = model.Update(mouseMsg)
}

// TestHandleMouseClickDoubleClick tests double-click detection
func TestHandleMouseClickDoubleClick(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file.go", Path: filepath.Join(tmpDir, "file.go"), IsDir: false}}
	model.Height = 10
	
	// First click
	mouseMsg := tea.MouseClickMsg{}
	model, _ = model.Update(mouseMsg)
	
	// Second click (should be detected as double-click)
	model, cmd := model.Update(mouseMsg)
	if cmd == nil {
		t.Error("Expected PinFileMsg command on double-click")
	}
}

// TestHandleMouseWheelBounds tests handleMouseWheel scroll bounds
func TestHandleMouseWheelBounds(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}}
	model.Height = 10
	model.ScrollY = 0
	
	// Try to scroll up beyond top
	wheelMsg := tea.MouseWheelMsg{}
	model, _ = model.Update(wheelMsg)
	if model.ScrollY < 0 {
		t.Errorf("Expected ScrollY >= 0, got %d", model.ScrollY)
	}
}

// TestViewRenders tests View method renders without panic
func TestViewRenders(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 10
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}}
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestViewWithCursor tests View with cursor position
func TestViewWithCursor(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 10
	model.Entries = []Entry{{Name: "file1.go"}, {Name: "file2.go"}}
	model.Cursor = 1
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestViewWithScroll tests View with scroll offset
func TestViewWithScroll(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 5
	model.Entries = []Entry{
		{Name: "file1.go"}, {Name: "file2.go"},
		{Name: "file3.go"}, {Name: "file4.go"},
	}
	model.ScrollY = 2
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestViewWithDiagnostics tests View with diagnostics
func TestViewWithDiagnostics(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 10
	
	filePath := filepath.Join(tmpDir, "file.go")
	model.Entries = []Entry{{Name: "file.go", Path: filePath}}
	model.diagnostics = map[string]int{filePath: 1} // error
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestViewWithGitStatus tests View with git status
func TestViewWithGitStatus(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 10
	
	filePath := filepath.Join(tmpDir, "file.go")
	model.Entries = []Entry{{Name: "file.go", Path: filePath, IsDir: false}}
	model.gitStatus = map[string]string{"file.go": "M"}
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestViewWithGitIgnoredEntry tests View with gitignored entry
func TestViewWithGitIgnoredEntry(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 10
	
	// Create a .gitignore file
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	os.WriteFile(gitignorePath, []byte("*.log\n"), 0o644)
	
	// Recreate model to load gitignore
	model = New(tmpDir, theme)
	model.Width = 50
	model.Height = 10
	
	// Create a log file that should be gitignored
	logFile := filepath.Join(tmpDir, "test.log")
	os.WriteFile(logFile, []byte("test"), 0o644)
	
	// Refresh to pick up the new file
	model.RefreshDir(tmpDir)
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestViewWithNestedDirectory tests View with nested directory
func TestViewWithNestedDirectory(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 10
	
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0o755)
	model.Entries = []Entry{
		{Name: "subdir", Path: subDir, IsDir: true, Expanded: true, Depth: 0, Children: []Entry{
			{Name: "nested.go", Path: filepath.Join(subDir, "nested.go"), Depth: 1},
		}},
	}
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestViewWithEmptyTree tests View with empty tree
func TestViewWithEmptyTree(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 50
	model.Height = 5
	model.Entries = []Entry{}
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view (should have empty lines)")
	}
}

// TestViewWithLongNames tests View with long file names
func TestViewWithLongNames(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	model.Width = 30
	model.Height = 5
	model.Entries = []Entry{{Name: "very_long_file_name_that_should_be_truncated.go"}}
	
	view := model.View()
	if view == "" {
		t.Error("Expected non-empty view")
	}
}

// TestSetSize tests SetSize method
func TestSetSize(t *testing.T) {
	theme := ui.DefaultTheme()
	tmpDir := t.TempDir()
	model := New(tmpDir, theme)
	
	model.SetSize(100, 30)
	if model.Width != 100 {
		t.Errorf("Expected Width 100, got %d", model.Width)
	}
	if model.Height != 30 {
		t.Errorf("Expected Height 30, got %d", model.Height)
	}
}

// TestLoadGitignore tests loadGitignore function
func TestLoadGitignore(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a .gitignore file
	gitignorePath := filepath.Join(tmpDir, ".gitignore")
	content := "*.log\nbuild/\n# comment\n\n*.tmp"
	os.WriteFile(gitignorePath, []byte(content), 0o644)
	
	patterns := loadGitignore(tmpDir)
	if len(patterns) != 3 {
		t.Errorf("Expected 3 patterns, got %d: %v", len(patterns), patterns)
	}
}

// TestLoadGitignoreWithNonExistentFile tests loadGitignore with non-existent file
func TestLoadGitignoreWithNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	
	patterns := loadGitignore(tmpDir)
	if patterns != nil {
		t.Errorf("Expected nil patterns, got %v", patterns)
	}
}

// TestMatchesGitignore tests matchesGitignore function
func TestMatchesGitignore(t *testing.T) {
	patterns := []string{"*.log", "build/", "temp/**"}
	
	tests := []struct {
		path     string
		isDir    bool
		expected bool
	}{
		{"test.log", false, true},
		{"build", true, true},
		{"temp/file.go", false, true},
		{"src/main.go", false, false},
		{"README.md", false, false},
	}
	
	for _, test := range tests {
		result := matchesGitignore(test.path, patterns, test.isDir)
		if result != test.expected {
			t.Errorf("matchesGitignore(%q, %v) = %v, expected %v",
				test.path, patterns, result, test.expected)
		}
	}
}

// TestMatchesGitignoreWithBaseName tests matchesGitignore with basename matching
func TestMatchesGitignoreWithBaseName(t *testing.T) {
	patterns := []string{"*.go"}
	
	// Should match basename
	result := matchesGitignore("/path/to/file.go", patterns, false)
	if !result {
		t.Error("Expected to match *.go pattern")
	}
}

// TestMatchesGitignoreWithDirectoryPattern tests matchesGitignore with directory pattern
func TestMatchesGitignoreWithDirectoryPattern(t *testing.T) {
	patterns := []string{"node_modules/"}
	
	// Should match directory
	result := matchesGitignore("node_modules", patterns, true)
	if !result {
		t.Error("Expected to match node_modules/ pattern")
	}
	
	// Should not match file
	result = matchesGitignore("node_modules", patterns, false)
	if result {
		t.Error("Expected not to match node_modules/ pattern for file")
	}
}

// TestReadDirEntriesWithGitignore tests readDirEntries with gitignore
func TestReadDirEntriesWithGitignore(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "file.go"), []byte("test"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "file.log"), []byte("test"), 0o644)
	
	patterns := []string{"*.log"}
	entries := readDirEntries(tmpDir, 0, patterns)
	
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries (including .gitignored), got %d", len(entries))
	}
	
	// Check if .log file is marked as gitignored
	var logFileMarked bool
	for _, entry := range entries {
		if entry.Name == "file.log" && entry.IsGitIgnored {
			logFileMarked = true
			break
		}
	}
	if !logFileMarked {
		t.Error("Expected .log file to be marked as gitignored")
	}
}

// TestReadDirEntriesWithSubDirectory tests readDirEntries with subdirectory
func TestReadDirEntriesWithSubDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0o755)
	os.WriteFile(filepath.Join(subDir, "file.go"), []byte("test"), 0o644)
	
	patterns := []string{}
	entries := readDirEntries(tmpDir, 0, patterns)
	
	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}
	if !entries[0].IsDir {
		t.Error("Expected entry to be directory")
	}
}

// TestReadDirEntriesWithSorting tests readDirEntries sorting
func TestReadDirEntriesWithSorting(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create files in non-alphabetical order
	os.WriteFile(filepath.Join(tmpDir, "c.go"), []byte("test"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("test"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte("test"), 0o644)
	
	patterns := []string{}
	entries := readDirEntries(tmpDir, 0, patterns)
	
	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}
	
	// Check if sorted correctly
	expectedOrder := []string{"a.go", "b.go", "c.go"}
	for i, expected := range expectedOrder {
		if entries[i].Name != expected {
			t.Errorf("Expected entry %d to be %q, got %q", i, expected, entries[i].Name)
		}
	}
}

// TestReadDirEntriesWithMixedTypes tests readDirEntries with mixed files and directories
func TestReadDirEntriesWithMixedTypes(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create mixed entries
	os.WriteFile(filepath.Join(tmpDir, "z.go"), []byte("test"), 0o644)
	os.MkdirAll(filepath.Join(tmpDir, "adir"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("test"), 0o644)
	
	patterns := []string{}
	entries := readDirEntries(tmpDir, 0, patterns)
	
	// Directories should come before files
	if !entries[0].IsDir {
		t.Error("Expected first entry to be directory")
	}
	if entries[1].IsDir || entries[2].IsDir {
		t.Error("Expected remaining entries to be files")
	}
}

// TestRefreshInSlice tests refreshInSlice function
func TestRefreshInSlice(t *testing.T) {
	tmpDir := t.TempDir()
	
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0o755)
	// Create a file in subdir so it has children
	os.WriteFile(filepath.Join(subDir, "file.go"), []byte("test"), 0o644)
	
	entries := []Entry{
		{Name: "subdir", Path: subDir, IsDir: true, Expanded: true, Depth: 0, Children: nil},
		{Name: "file.go", Path: filepath.Join(tmpDir, "file.go"), IsDir: false},
	}
	
	result := refreshInSlice(entries, subDir, []string{})
	if !result {
		t.Error("Expected refreshInSlice to return true")
	}
	
	// Check if children were loaded (should have at least 1 child)
	if entries[0].Children == nil || len(entries[0].Children) == 0 {
		t.Error("Expected children to be loaded")
	}
	if entries[0].Loading {
		t.Error("Expected Loading to be false")
	}
}

// TestRefreshInSliceWithNestedDir tests refreshInSlice with nested directory
func TestRefreshInSliceWithNestedDir(t *testing.T) {
	tmpDir := t.TempDir()
	
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0o755)
	nestedDir := filepath.Join(subDir, "nested")
	os.MkdirAll(nestedDir, 0o755)
	
	entries := []Entry{
		{
			Name: "subdir", Path: subDir, IsDir: true, Expanded: true, Depth: 0,
			Children: []Entry{
				{Name: "nested", Path: nestedDir, IsDir: true, Expanded: true, Depth: 1},
			},
		},
	}
	
	result := refreshInSlice(entries, nestedDir, []string{})
	if !result {
		t.Error("Expected refreshInSlice to return true")
	}
}

// TestRefreshInSliceWithNonExistentDir tests refreshInSlice with non-existent directory
func TestRefreshInSliceWithNonExistentDir(t *testing.T) {
	entries := []Entry{{Name: "file.go", Path: "/file.go", IsDir: false}}
	
	result := refreshInSlice(entries, "/nonexistent", []string{})
	if result {
		t.Error("Expected refreshInSlice to return false")
	}
}

// TestToggleInSliceWithAsyncLoad tests toggleInSlice with async load
func TestToggleInSliceWithAsyncLoad(t *testing.T) {
	tmpDir := t.TempDir()
	
	path := filepath.Join(tmpDir, "testdir")
	os.MkdirAll(path, 0o755)
	
	entries := []Entry{{Name: "testdir", Path: path, IsDir: true, Expanded: false, Children: nil}}
	
	cmd := toggleInSlice(entries, path, []string{})
	if cmd == nil {
		t.Error("Expected async load command")
	}
	if !entries[0].Loading {
		t.Error("Expected Loading to be true")
	}
	if !entries[0].Expanded {
		t.Error("Expected Expanded to be true")
	}
}

// TestToggleInSliceWithAlreadyLoaded tests toggleInSlice with already loaded children
func TestToggleInSliceWithAlreadyLoaded(t *testing.T) {
	tmpDir := t.TempDir()
	
	path := filepath.Join(tmpDir, "testdir")
	entries := []Entry{
		{
			Name: "testdir", Path: path, IsDir: true, Expanded: false,
			Children: []Entry{{Name: "file.go"}},
		},
	}
	
	cmd := toggleInSlice(entries, path, []string{})
	if cmd != nil {
		t.Error("Expected nil command for already loaded children")
	}
}

// TestToggleInSliceWithNestedDir tests toggleInSlice with nested directory
func TestToggleInSliceWithNestedDir(t *testing.T) {
	tmpDir := t.TempDir()
	
	subDir := filepath.Join(tmpDir, "subdir")
	nestedDir := filepath.Join(subDir, "nested")
	os.MkdirAll(nestedDir, 0o755)
	
	entries := []Entry{
		{
			Name: "subdir", Path: subDir, IsDir: true, Expanded: true,
			Children: []Entry{
				{Name: "nested", Path: nestedDir, IsDir: true, Expanded: false, Children: nil},
			},
		},
	}
	
	cmd := toggleInSlice(entries, nestedDir, []string{})
	if cmd == nil {
		t.Error("Expected async load command for nested directory")
	}
}

// TestSetChildrenInSlice tests setChildrenInSlice function
func TestSetChildrenInSlice(t *testing.T) {
	tmpDir := t.TempDir()
	
	path := filepath.Join(tmpDir, "testdir")
	entries := []Entry{{Name: "testdir", Path: path, IsDir: true, Loading: true}}
	children := []Entry{{Name: "file.go"}}
	
	result := setChildrenInSlice(entries, path, children)
	if !result {
		t.Error("Expected setChildrenInSlice to return true")
	}
	if entries[0].Loading {
		t.Error("Expected Loading to be false")
	}
	if len(entries[0].Children) != 1 {
		t.Errorf("Expected 1 child, got %d", len(entries[0].Children))
	}
}

// TestSetChildrenInSliceWithNestedDir tests setChildrenInSlice with nested directory
func TestSetChildrenInSliceWithNestedDir(t *testing.T) {
	tmpDir := t.TempDir()
	
	subDir := filepath.Join(tmpDir, "subdir")
	nestedDir := filepath.Join(subDir, "nested")
	
	entries := []Entry{
		{
			Name: "subdir", Path: subDir, IsDir: true, Expanded: true,
			Children: []Entry{
				{Name: "nested", Path: nestedDir, IsDir: true, Expanded: true, Loading: true, Children: nil},
			},
		},
	}
	children := []Entry{{Name: "file.go"}}
	
	result := setChildrenInSlice(entries, nestedDir, children)
	if !result {
		t.Error("Expected setChildrenInSlice to return true")
	}
	if len(entries[0].Children) == 0 {
		t.Fatal("Expected children to be set")
	}
	if entries[0].Children[0].Loading {
		t.Error("Expected Loading to be false")
	}
}

// TestSetChildrenInSliceWithNonExistentDir tests setChildrenInSlice with non-existent directory
func TestSetChildrenInSliceWithNonExistentDir(t *testing.T) {
	entries := []Entry{{Name: "file.go", Path: "/file.go", IsDir: false}}
	children := []Entry{}
	
	result := setChildrenInSlice(entries, "/nonexistent", children)
	if result {
		t.Error("Expected setChildrenInSlice to return false")
	}
}
