package app

import (
	"strings"
	"testing"

	zone "github.com/lrstanley/bubblezone/v2"

	"teak/internal/config"
	"teak/internal/editor"
)

func modelForMenuTest(t *testing.T) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Session.Enabled = false
	cfg.Agent.Enabled = false
	m, err := NewModel("", t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("NewModel() error = %v", err)
	}
	t.Cleanup(m.cleanup)
	m.width, m.height = 120, 40
	zone.NewGlobal()
	return m
}

// The sidebar menus used to hang off an `else if` chained to the editor menu's
// diff-tab check, so on an ordinary tab they became visible — capturing input
// and hiding the cursor — while never being drawn.
func TestSidebarContextMenusRenderOnNonDiffTab(t *testing.T) {
	tests := []struct {
		name string
		open func(*Model)
		want string
	}{
		{
			name: "file tree",
			open: func(m *Model) {
				m.treeContextMenu.Show([]editor.ContextMenuItem{
					{Label: "New File", Action: "new_file"},
				}, 10, 5)
			},
			want: "New File",
		},
		{
			name: "git panel",
			open: func(m *Model) {
				m.gitContextMenu.Show([]editor.ContextMenuItem{
					{Label: "Stage", Action: "stage"},
				}, 10, 5)
			},
			want: "Stage",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := modelForMenuTest(t)
			if m.isActiveDiffTab() {
				t.Fatal("setup: expected an ordinary (non-diff) tab")
			}
			tc.open(&m)

			content := m.View().Content
			if !strings.Contains(content, tc.want) {
				t.Errorf("context menu is visible but %q was not rendered", tc.want)
			}
		})
	}
}
