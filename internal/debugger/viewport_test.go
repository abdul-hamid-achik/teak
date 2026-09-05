package debugger

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"teak/internal/dap"
	"teak/internal/ui"
)

func TestStackSelectionRemainsVisibleAndFitsPanel(t *testing.T) {
	for _, size := range [][2]int{{25, 12}, {40, 18}, {60, 30}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m := New(ui.DefaultTheme())
			m.SetSize(size[0], size[1])
			m.SetState(dap.StateStopped)
			var frames []dap.StackFrame
			for i := range 20 {
				frames = append(frames, dap.StackFrame{Name: fmt.Sprintf("frame_%02d_%s", i, strings.Repeat("界", 30)), Source: dap.Source{Path: "/tmp/sample.go"}, Line: i + 1})
			}
			m.SetStackFrames(frames)
			m.SelectFrame(19)
			view := m.View()
			if !strings.Contains(view, "frame_19") {
				t.Error("selected frame is outside visible stack")
			}
			if w, h := lipgloss.Width(view), lipgloss.Height(view); w > size[0] || h > size[1] {
				t.Errorf("view %dx%d exceeds %dx%d", w, h, size[0], size[1])
			}
			m.SetStackFrames(frames[:1])
			if !strings.Contains(m.View(), "frame_00") {
				t.Error("new stack inherits stale scroll position")
			}
		})
	}
}
