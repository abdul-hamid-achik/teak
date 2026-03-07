package app

import (
	"testing"

	"teak/internal/editor"
	"teak/internal/text"
	"teak/internal/ui"
)

func TestNewTabManager(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	if tm == nil {
		t.Fatal("NewTabManager returned nil")
	}

	if tm.EditorCount() != 0 {
		t.Errorf("Expected 0 editors, got %d", tm.EditorCount())
	}

	if tm.GetActiveTab() != 0 {
		t.Errorf("Expected active tab 0, got %d", tm.GetActiveTab())
	}

	if tm.GetPendingCloseTab() != -1 {
		t.Errorf("Expected pending close tab -1, got %d", tm.GetPendingCloseTab())
	}
}

func TestTabManagerAddEditor(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// Add first editor
	buf1 := text.NewBuffer()
	buf1.FilePath = "/tmp/test1.go"
	ed1 := editor.New(buf1, ui.DefaultTheme(), editor.DefaultConfig())
	idx1 := tm.AddEditor(ed1, "test1.go", "/tmp/test1.go")

	if idx1 != 0 {
		t.Errorf("Expected index 0, got %d", idx1)
	}

	if tm.EditorCount() != 1 {
		t.Errorf("Expected 1 editor, got %d", tm.EditorCount())
	}

	// Add second editor
	buf2 := text.NewBuffer()
	buf2.FilePath = "/tmp/test2.go"
	ed2 := editor.New(buf2, ui.DefaultTheme(), editor.DefaultConfig())
	idx2 := tm.AddEditor(ed2, "test2.go", "/tmp/test2.go")

	if idx2 != 1 {
		t.Errorf("Expected index 1, got %d", idx2)
	}

	if tm.EditorCount() != 2 {
		t.Errorf("Expected 2 editors, got %d", tm.EditorCount())
	}
}

func TestTabManagerGetActiveEditor(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// No editors yet
	if ed := tm.GetActiveEditor(); ed != nil {
		t.Error("Expected nil editor with no tabs")
	}

	// Add editor
	buf := text.NewBuffer()
	buf.FilePath = "/tmp/test.go"
	ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
	tm.AddEditor(ed, "test.go", "/tmp/test.go")

	activeEd := tm.GetActiveEditor()
	if activeEd == nil {
		t.Fatal("Expected active editor, got nil")
	}

	if activeEd.Buffer.FilePath != "/tmp/test.go" {
		t.Errorf("Expected file path /tmp/test.go, got %s", activeEd.Buffer.FilePath)
	}
}

func TestTabManagerSetActiveTab(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// Add three editors
	for i := 0; i < 3; i++ {
		buf := text.NewBuffer()
		buf.FilePath = "/tmp/test" + string(rune('0'+i)) + ".go"
		ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
		tm.AddEditor(ed, buf.FilePath, buf.FilePath)
	}

	// Switch to tab 2
	tm.SetActiveTab(2)
	if tm.GetActiveTab() != 2 {
		t.Errorf("Expected active tab 2, got %d", tm.GetActiveTab())
	}

	// Switch to tab 0
	tm.SetActiveTab(0)
	if tm.GetActiveTab() != 0 {
		t.Errorf("Expected active tab 0, got %d", tm.GetActiveTab())
	}

	// Try invalid index (should not change)
	tm.SetActiveTab(10)
	if tm.GetActiveTab() != 0 {
		t.Errorf("Active tab should remain 0, got %d", tm.GetActiveTab())
	}
}

func TestTabManagerCloseTab(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// Add three editors
	for i := 0; i < 3; i++ {
		buf := text.NewBuffer()
		buf.FilePath = "/tmp/test" + string(rune('0'+i)) + ".go"
		ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
		tm.AddEditor(ed, buf.FilePath, buf.FilePath)
	}

	// Close middle tab
	closed, err := tm.CloseTab(1)
	if err != nil {
		t.Fatalf("CloseTab failed: %v", err)
	}

	if closed == nil {
		t.Error("Expected ClosedTab for file-backed tab")
	} else if closed.FilePath != "/tmp/test1.go" {
		t.Errorf("Expected closed tab file /tmp/test1.go, got %s", closed.FilePath)
	}

	if tm.EditorCount() != 2 {
		t.Errorf("Expected 2 editors after close, got %d", tm.EditorCount())
	}

	// Close invalid tab
	_, err = tm.CloseTab(10)
	if err == nil {
		t.Error("Expected error for invalid tab index")
	}
}

func TestTabManagerCloseActiveTab(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// Add two editors
	for i := 0; i < 2; i++ {
		buf := text.NewBuffer()
		buf.FilePath = "/tmp/test" + string(rune('0'+i)) + ".go"
		ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
		tm.AddEditor(ed, buf.FilePath, buf.FilePath)
	}

	tm.SetActiveTab(1)

	// Close active tab
	_, err := tm.CloseActiveTab()
	if err != nil {
		t.Fatalf("CloseActiveTab failed: %v", err)
	}

	// Active tab should adjust
	if tm.GetActiveTab() != 0 {
		t.Errorf("Expected active tab to adjust to 0, got %d", tm.GetActiveTab())
	}
}

func TestTabManagerNextPrevTab(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// Add three editors
	for i := 0; i < 3; i++ {
		buf := text.NewBuffer()
		buf.FilePath = "/tmp/test" + string(rune('0'+i)) + ".go"
		ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
		tm.AddEditor(ed, buf.FilePath, buf.FilePath)
	}

	// Test NextTab
	tm.NextTab()
	if tm.GetActiveTab() != 1 {
		t.Errorf("Expected tab 1 after NextTab, got %d", tm.GetActiveTab())
	}

	tm.NextTab()
	if tm.GetActiveTab() != 2 {
		t.Errorf("Expected tab 2 after NextTab, got %d", tm.GetActiveTab())
	}

	// Wrap around
	tm.NextTab()
	if tm.GetActiveTab() != 0 {
		t.Errorf("Expected tab 0 after wrap, got %d", tm.GetActiveTab())
	}

	// Test PrevTab
	tm.PrevTab()
	if tm.GetActiveTab() != 2 {
		t.Errorf("Expected tab 2 after PrevTab from 0, got %d", tm.GetActiveTab())
	}
}

func TestTabManagerClosedTabs(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// Add and close a tab
	buf := text.NewBuffer()
	buf.FilePath = "/tmp/closed.go"
	ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
	tm.AddEditor(ed, "closed.go", "/tmp/closed.go")

	closed, _ := tm.CloseTab(0)
	if closed != nil {
		tm.AddClosedTab(*closed)
	}

	// Reopen last tab
	reopened, ok := tm.ReopenLastTab()
	if !ok {
		t.Fatal("Expected to reopen last tab")
	}

	if reopened.FilePath != "/tmp/closed.go" {
		t.Errorf("Expected reopened file /tmp/closed.go, got %s", reopened.FilePath)
	}

	// No more closed tabs
	_, ok = tm.ReopenLastTab()
	if ok {
		t.Error("Expected no more closed tabs")
	}
}

func TestTabManagerDiffViews(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// Add regular editor
	buf := text.NewBuffer()
	buf.FilePath = "/tmp/regular.go"
	ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
	regIdx := tm.AddEditor(ed, "regular.go", "/tmp/regular.go")

	if tm.IsDiffView(regIdx) {
		t.Error("Regular tab should not be a diff view")
	}

	// Add diff view (would need actual diff.Model in real test)
	// Skipping since diff.Model requires complex setup
}

func TestTabManagerAnyDirty(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// No editors - not dirty
	if tm.AnyDirty() {
		t.Error("Expected no dirty editors with empty tab manager")
	}

	// Add clean editor
	buf := text.NewBuffer()
	buf.FilePath = "/tmp/clean.go"
	ed := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
	tm.AddEditor(ed, "clean.go", "/tmp/clean.go")

	if tm.AnyDirty() {
		t.Error("Expected no dirty editors with clean buffer")
	}

	// Note: To test dirty state, we'd need to modify the buffer
	// which requires more complex setup
}

func TestTabManagerWelcome(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	if tm.HasWelcome() {
		t.Error("Expected no welcome screen initially")
	}

	welcome := editor.NewWelcome(ui.DefaultTheme())
	tm.SetWelcome(&welcome)

	if !tm.HasWelcome() {
		t.Error("Expected welcome screen after setting")
	}

	if tm.GetWelcome() == nil {
		t.Error("GetWelcome returned nil")
	}

	tm.DismissWelcome()

	if tm.HasWelcome() {
		t.Error("Expected no welcome screen after dismiss")
	}
}

func TestTabManagerPendingClose(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	if tm.GetPendingCloseTab() != -1 {
		t.Errorf("Expected -1, got %d", tm.GetPendingCloseTab())
	}

	tm.SetPendingCloseTab(2)
	if tm.GetPendingCloseTab() != 2 {
		t.Errorf("Expected 2, got %d", tm.GetPendingCloseTab())
	}

	tm.ClearPendingCloseTab()
	if tm.GetPendingCloseTab() != -1 {
		t.Errorf("Expected -1 after clear, got %d", tm.GetPendingCloseTab())
	}
}

func TestTabManagerUntitledCounter(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	c1 := tm.GetUntitledCounter()
	if c1 != 1 {
		t.Errorf("Expected counter 1, got %d", c1)
	}

	c2 := tm.GetUntitledCounter()
	if c2 != 2 {
		t.Errorf("Expected counter 2, got %d", c2)
	}

	c3 := tm.GetUntitledCounter()
	if c3 != 3 {
		t.Errorf("Expected counter 3, got %d", c3)
	}
}

func TestTabManagerGetEditor(t *testing.T) {
	tm := NewTabManager(ui.DefaultTheme())

	// No editors
	ed, ok := tm.GetEditor(0)
	if ok || ed != nil {
		t.Error("Expected no editor at index 0")
	}

	// Add editor
	buf := text.NewBuffer()
	buf.FilePath = "/tmp/test.go"
	newEd := editor.New(buf, ui.DefaultTheme(), editor.DefaultConfig())
	tm.AddEditor(newEd, "test.go", "/tmp/test.go")

	ed, ok = tm.GetEditor(0)
	if !ok || ed == nil {
		t.Error("Expected to get editor at index 0")
	}

	// Invalid index
	ed, ok = tm.GetEditor(100)
	if ok || ed != nil {
		t.Error("Expected no editor at invalid index")
	}
}
