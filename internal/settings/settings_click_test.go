package settings

import "testing"

func TestSettingsClickTargetsFollowAlignedControls(t *testing.T) {
	tests := []struct {
		name       string
		category   int
		row        int
		x          func(m *Model, s *Setting) int
		wantPicker bool
		wantDelta  int // change of the int value, or 1 when a bool flips
	}{
		{"theme label text only selects", 1, 0, func(*Model, *Setting) int { return 3 }, false, 0},
		{"theme gap opens the picker", 1, 0, func(*Model, *Setting) int { return 12 }, true, 0},
		{"theme control opens the picker", 1, 0, func(m *Model, s *Setting) int { return m.settingControlStart(s) + 2 }, true, 0},
		{"bool label text only selects", 0, 1, func(*Model, *Setting) int { return 5 }, false, 0},
		{"bool gap toggles", 0, 1, func(m *Model, s *Setting) int { return m.settingControlStart(s) - 2 }, false, 1},
		{"bool control toggles", 0, 1, func(m *Model, s *Setting) int { return m.settingControlStart(s) + 3 }, false, 1},
		{"int gap does nothing", 0, 0, func(m *Model, s *Setting) int { return m.settingControlStart(s) - 2 }, false, 0},
		{"int minus decrements", 0, 0, func(m *Model, s *Setting) int { return m.settingControlStart(s) + 1 }, false, -1},
		{"int plus increments", 0, 0, func(m *Model, s *Setting) int { return m.settingControlStart(s) + 5 }, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := newViewModel(t, "nord", 66, 22)
			model.selectedCategory = tt.category
			setting := &model.categories[tt.category].Settings[tt.row]
			before := setting.Value
			handled, picker := model.HandleMouseClick(tt.x(&model, setting), settingsFirstRow+tt.row)
			if !handled || picker != tt.wantPicker {
				t.Fatalf("handled=%t picker=%t, want handled and picker=%t", handled, picker, tt.wantPicker)
			}
			if model.selectedSetting != tt.row {
				t.Fatalf("selected row = %d, want %d", model.selectedSetting, tt.row)
			}
			switch v := before.(type) {
			case int:
				if got := setting.Value.(int) - v; got != tt.wantDelta {
					t.Fatalf("int changed by %d, want %d", got, tt.wantDelta)
				}
			case bool:
				if flipped := setting.Value.(bool) != v; flipped != (tt.wantDelta == 1) {
					t.Fatalf("bool flipped=%t, want %t", flipped, tt.wantDelta == 1)
				}
			}
		})
	}
}
