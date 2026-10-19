package git

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	zone "github.com/lrstanley/bubblezone/v2"
)

func sgrParams(style lipgloss.Style) string {
	rendered := style.Render("x")
	prefix := rendered[:strings.Index(rendered, "x")]
	return strings.TrimSuffix(strings.TrimPrefix(prefix, "\x1b["), "m")
}

// TestUnstageAllAffordanceIsMuted keeps the "−" button off the untracked-file
// colour: GitUntracked is green, which reads as a positive action.
func TestUnstageAllAffordanceIsMuted(t *testing.T) {
	previous := zone.DefaultManager
	zone.NewGlobal()
	t.Cleanup(func() { zone.Close(); zone.DefaultManager = previous })

	m := testModel([]StatusEntry{{Path: "a.go", IndexStatus: 'M', WorkStatus: ' '}})
	row := m.renderTreeRow(treeRowHit{kind: treeRowStagedHeader})
	idx := strings.Index(row, "−")
	if idx < 0 {
		t.Fatalf("staged header lacks the unstage-all control: %q", row)
	}
	// The control is its own styled run, so inspect only the text before it
	// back to the preceding reset.
	head := row[:idx]
	tail := head[strings.LastIndex(head, "\x1b[m")+1:]
	muted := sgrParams(lipgloss.NewStyle().Foreground(m.theme.Muted.GetForeground()))
	untracked := sgrParams(lipgloss.NewStyle().Foreground(m.theme.GitUntracked.GetForeground()))
	if !strings.Contains(tail, muted) {
		t.Errorf("unstage-all control lacks Muted colour %q: %q", muted, tail)
	}
	if strings.Contains(tail, untracked) {
		t.Errorf("unstage-all control still uses GitUntracked %q: %q", untracked, tail)
	}
}

// TestUntrackedStatusLetterKeepsUntrackedColour guards the other direction.
func TestUntrackedStatusLetterKeepsUntrackedColour(t *testing.T) {
	m := testModel(nil)
	if got, want := m.statusStyleForByte("U").GetForeground(), m.theme.GitUntracked.GetForeground(); got != want {
		t.Fatalf("U status colour = %v, want GitUntracked %v", got, want)
	}
}
