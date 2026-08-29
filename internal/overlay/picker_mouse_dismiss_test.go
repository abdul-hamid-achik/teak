package overlay

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/ui"
)

// blankFrame builds a width x height base of spaces, mirroring the terminal
// frame the app composites overlays onto.
func blankFrame(width, height int) string {
	row := make([]byte, width)
	for i := range row {
		row[i] = ' '
	}
	line := string(row)
	out := make([]byte, 0, height*(width+1))
	for y := 0; y < height; y++ {
		if y > 0 {
			out = append(out, '\n')
		}
		out = append(out, line...)
	}
	return string(out)
}

// compositeOverlayInFrame renders an overlay the way the app does — centered
// over the base frame via ui.RenderOverlay, then BubbleZone-scanned — so zone
// hit-testing resolves against absolute terminal coordinates.
func compositeOverlayInFrame(t *testing.T, o Overlay, width, height int) {
	t.Helper()
	_ = zone.Scan(ui.RenderOverlay(blankFrame(width, height), o.View(), width, height))
}

// awaitZone polls until BubbleZone's asynchronous worker has published the
// zone marked by the last scan. Scan feeds a background goroutine, so bounds
// are not visible to Get the instant Scan returns.
func awaitZone(t *testing.T, id string) *zone.ZoneInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if z := zone.Get(id); z != nil && !z.IsZero() {
			return z
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("zone %q was not registered by the rendered frame", id)
	return nil
}

func pickerBoxZone(t *testing.T, p *Picker) *zone.ZoneInfo {
	t.Helper()
	return awaitZone(t, p.zoneID+"-box")
}

func blockUntilCanceled(ctx context.Context) ([]PickerItem, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestPickerLeftClickOutsideBoxDismisses verifies that a left click landing
// outside the picker's rendered box dismisses the picker exactly like Escape:
// same state transitions and the same close message. The overlay stack still
// consumes the click, so it never reaches the editor or tree underneath.
func TestPickerLeftClickOutsideBoxDismisses(t *testing.T) {
	tests := []struct {
		name string
		x, y func(box *zone.ZoneInfo) int
	}{
		{"above and left of the box", func(b *zone.ZoneInfo) int { return b.StartX - 5 }, func(b *zone.ZoneInfo) int { return b.StartY - 5 }},
		{"below and right of the box", func(b *zone.ZoneInfo) int { return b.EndX + 5 }, func(b *zone.ZoneInfo) int { return b.EndY + 5 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestPicker()
			p.SetSize(60, 24)
			compositeOverlayInFrame(t, p, 100, 40)
			box := pickerBoxZone(t, p)

			updated, cmd := p.Update(tea.MouseClickMsg{X: tt.x(box), Y: tt.y(box), Button: tea.MouseLeft})
			if !updated.IsDismissed() {
				t.Fatal("left click outside the picker box should dismiss the picker")
			}
			if cmd == nil {
				t.Fatal("outside click should emit the same close command as Escape")
			}
			if _, ok := cmd().(PickerCloseMsg); !ok {
				t.Fatalf("outside click emitted %T, want PickerCloseMsg", cmd())
			}
		})
	}
}

// TestPickerOutsideClickHonorsDismissAction mirrors the Escape contract for
// pickers with a dismiss action: the owner's cancellation message is emitted
// instead of PickerCloseMsg.
func TestPickerOutsideClickHonorsDismissAction(t *testing.T) {
	type cancelMsg struct{}
	p := newTestPicker()
	p.SetSize(60, 24)
	p.SetDismissAction(cancelMsg{})
	compositeOverlayInFrame(t, p, 100, 40)
	box := pickerBoxZone(t, p)

	updated, cmd := p.Update(tea.MouseClickMsg{X: box.StartX - 5, Y: box.StartY - 5, Button: tea.MouseLeft})
	if !updated.IsDismissed() {
		t.Fatal("outside click should dismiss the picker")
	}
	if cmd == nil {
		t.Fatal("outside click should emit the dismiss action")
	}
	if _, ok := cmd().(cancelMsg); !ok {
		t.Fatalf("outside click emitted %T, want the dismiss action", cmd())
	}
}

// TestPickerClickInsideBoxOnItemSelects pins the existing in-box behavior: a
// left click on a rendered item row selects that item.
func TestPickerClickInsideBoxOnItemSelects(t *testing.T) {
	p := newTestPicker()
	p.SetSize(60, 24)
	compositeOverlayInFrame(t, p, 100, 40)

	item := awaitZone(t, p.itemZoneID(1))
	updated, cmd := p.Update(tea.MouseClickMsg{X: item.StartX, Y: item.StartY, Button: tea.MouseLeft})
	if !updated.IsDismissed() {
		t.Fatal("clicking an item should dismiss the picker")
	}
	if cmd == nil {
		t.Fatal("clicking an item should emit a selection")
	}
	sel, ok := cmd().(PickerSelectMsg)
	if !ok {
		t.Fatalf("item click emitted %T, want PickerSelectMsg", cmd())
	}
	if sel.Item.Label != "internal/app/app.go" {
		t.Fatalf("item click selected %q, want %q", sel.Item.Label, "internal/app/app.go")
	}
}

// TestPickerClickInsideBoxOutsideItemsKeepsPickerOpen: clicks inside the box
// that do not land on an item (border, title, input row) keep the picker open.
func TestPickerClickInsideBoxOutsideItemsKeepsPickerOpen(t *testing.T) {
	p := newTestPicker()
	p.SetSize(60, 24)
	compositeOverlayInFrame(t, p, 100, 40)
	box := pickerBoxZone(t, p)

	updated, cmd := p.Update(tea.MouseClickMsg{X: box.StartX, Y: box.StartY, Button: tea.MouseLeft})
	if updated.IsDismissed() {
		t.Error("click on the box border should not dismiss the picker")
	}
	if cmd != nil {
		t.Errorf("click on the box border emitted %T, want nil", cmd)
	}
}

// TestPickerNonLeftClickOutsideBoxDoesNotDismiss: only left clicks dismiss;
// right clicks outside the box are consumed without closing the picker.
func TestPickerNonLeftClickOutsideBoxDoesNotDismiss(t *testing.T) {
	p := newTestPicker()
	p.SetSize(60, 24)
	compositeOverlayInFrame(t, p, 100, 40)
	box := pickerBoxZone(t, p)

	updated, _ := p.Update(tea.MouseClickMsg{X: box.StartX - 5, Y: box.StartY - 5, Button: tea.MouseRight})
	if updated.IsDismissed() {
		t.Error("right click outside the box should not dismiss the picker")
	}
}

// TestPendingPickerOutsideClickDismissesAndCancels mirrors Escape: an outside
// click on a picker still loading its items dismisses it and cancels the
// in-flight preparation.
func TestPendingPickerOutsideClickDismissesAndCancels(t *testing.T) {
	p := NewPendingPicker("Async", ui.DefaultTheme(), "async-mouse-picker")
	prepareCmd := p.PrepareItemsCmd(blockUntilCanceled)
	p.SetSize(60, 24)
	compositeOverlayInFrame(t, p, 100, 40)
	box := pickerBoxZone(t, p)

	updated, _ := p.Update(tea.MouseClickMsg{X: box.EndX + 5, Y: box.EndY + 5, Button: tea.MouseLeft})
	if !updated.IsDismissed() {
		t.Fatal("outside click should dismiss a pending picker")
	}
	if updated.(*Picker).FilterPending() {
		t.Fatal("outside click should cancel the pending preparation")
	}
	ready := prepareCmd().(PickerItemsReadyMsg)
	if !errors.Is(ready.Err, context.Canceled) {
		t.Fatalf("preparation error = %v, want context cancellation", ready.Err)
	}
}

// TestConfirmOutsideClickDoesNotDismiss guards the scoping of outside-click
// dismissal to pickers: a destructive confirmation must keep capturing input
// until the user makes an explicit choice.
func TestConfirmOutsideClickDoesNotDismiss(t *testing.T) {
	c := newTestConfirm()
	compositeOverlayInFrame(t, c, 100, 40)

	// No box zone exists for confirm dialogs; click far from the centered
	// dialog (it spans at most the middle ~50 columns of a 100-column frame).
	updated, cmd := c.Update(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if updated.IsDismissed() {
		t.Error("outside click must not dismiss a confirm dialog")
	}
	if c.Result() != nil {
		t.Error("outside click must not produce a confirm result")
	}
	if cmd != nil {
		t.Errorf("outside click emitted %T, want nil", cmd)
	}
}
