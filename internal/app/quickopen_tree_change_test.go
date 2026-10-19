package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/config"
	"teak/internal/overlay"
)

// collectCmdMsgsWithin runs every leaf of cmd concurrently and returns the
// messages produced within timeout. Leaves that block (watcher listeners,
// cursor blinks) are abandoned rather than awaited.
func collectCmdMsgsWithin(cmd tea.Cmd, timeout time.Duration) []tea.Msg {
	results := make(chan tea.Msg)
	done := make(chan struct{})
	defer close(done)
	var pending atomic.Int64
	var launch func(tea.Cmd)
	launch = func(c tea.Cmd) {
		if c == nil {
			return
		}
		pending.Add(1)
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, member := range batch {
					launch(member)
				}
				pending.Add(-1)
				return
			}
			select {
			case results <- msg:
			case <-done:
			}
		}()
	}
	launch(cmd)

	var msgs []tea.Msg
	deadline := time.After(timeout)
	for pending.Load() > 0 {
		select {
		case msg := <-results:
			pending.Add(-1)
			msgs = append(msgs, msg)
		case <-deadline:
			return msgs
		}
	}
	return msgs
}

// pumpQuickOpen feeds back the scan, item-preparation and filter results that
// cmd produces until the picker pipeline goes quiet. Unrelated messages (tree
// refreshes, git ticks, cursor blinks) are dropped so the test controls the
// order of the events it cares about.
func pumpQuickOpen(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, msg := range collectCmdMsgsWithin(next, 300*time.Millisecond) {
			switch msg.(type) {
			case FileListMsg, overlay.PickerItemsReadyMsg, overlay.PickerFilterReadyMsg:
				updated, follow := m.Update(msg)
				m = updated.(Model)
				queue = append(queue, follow)
			}
		}
	}
	return m
}

func quickOpenPickerView(t *testing.T, m Model) string {
	t.Helper()
	zone.NewGlobal()
	if m.overlayStack.IsEmpty() {
		t.Fatal("Quick Open picker is not open")
	}
	picker, ok := m.overlayStack.Top().(*overlay.Picker)
	if !ok || picker.ZoneID() != "quickopen" {
		t.Fatalf("top overlay = %T, want the Quick Open picker", m.overlayStack.Top())
	}
	return picker.View()
}

// Reproduces the audit: Quick Open opened shortly after startup while the
// watcher keeps reporting tree changes (the git panel's status refresh creates
// and removes .git/index.lock several times a second, and every .git event
// becomes a root TreeChangedMsg). A tree change used to cancel and invalidate
// the scan the picker was waiting for without starting another one, so the
// picker stayed empty and answered "No matches" for every query, forever.
func TestQuickOpenSurvivesTreeChangesDuringInitialScan(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"cmd/server/main.go",
		"internal/store/store.go",
		"internal/store/store_test.go",
		"README.md",
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel(filepath.Join(root, "cmd/server/main.go"), root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.cleanup)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)

	updated, openCmd := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(Model)
	if view := quickOpenPickerView(t, m); !strings.Contains(view, "Loading...") || strings.Contains(view, "No matches") {
		t.Fatalf("Quick Open waiting for its scan must say it is loading:\n%s", view)
	}

	// The user types and the tree changes repeatedly before the scan result
	// is delivered.
	var later []tea.Cmd
	for _, r := range "store" {
		updated, typeCmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(Model)
		later = append(later, typeCmd)
		updated, treeCmd := m.Update(TreeChangedMsg{Dir: root})
		m = updated.(Model)
		later = append(later, treeCmd)
	}
	m = pumpQuickOpen(t, m, openCmd)
	for _, cmd := range later {
		m = pumpQuickOpen(t, m, cmd)
	}

	view := quickOpenPickerView(t, m)
	if strings.Contains(view, "No matches") || !strings.Contains(view, "store.go") {
		t.Fatalf("Quick Open after tree changes during its scan:\n%s\nwant internal/store/store.go listed", view)
	}
	if m.cachedFilesReady {
		t.Fatal("a scan the tree changed under was cached as fresh; the next Quick Open would reuse it")
	}

	updated, _ = m.Update(overlay.PickerCloseMsg{})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(Model)
	if m.fileListCancel == nil {
		t.Fatal("reopening Quick Open after a stale scan did not rescan")
	}
}

// A picker that already shows files must not be rebuilt on every tree change:
// .git churn would otherwise keep resetting the cursor while the user browses.
func TestQuickOpenTreeChangeDoesNotRescanLoadedPicker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel(filepath.Join(root, "main.go"), root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.cleanup)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = updated.(Model)
	updated, openCmd := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = updated.(Model)
	m = pumpQuickOpen(t, m, openCmd)
	if !strings.Contains(quickOpenPickerView(t, m), "main.go") {
		t.Fatalf("Quick Open did not load:\n%s", quickOpenPickerView(t, m))
	}

	updated, treeCmd := m.Update(TreeChangedMsg{Dir: root})
	m = updated.(Model)
	if m.fileListCancel != nil {
		t.Fatal("tree change started a rescan for a picker that already has its files")
	}
	for _, msg := range collectCmdMsgsWithin(treeCmd, 300*time.Millisecond) {
		if _, ok := msg.(FileListMsg); ok {
			t.Fatalf("tree change produced a Quick Open scan result %#v for a loaded picker", msg)
		}
	}
}
