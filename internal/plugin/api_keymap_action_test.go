package plugin

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// TestKeymapSetRejectsActionsThatCannotBeDispatched covers actions that
// executePluginAction can never run. Accepting them at registration would
// make the key match and then fail on every press.
func TestKeymapSetRejectsActionsThatCannotBeDispatched(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{"nil action", `keymap.set("n", "<leader>q", nil)`},
		{"missing action", `keymap.set("n", "<leader>q")`},
		{"number action", `keymap.set("n", "<leader>q", 42)`},
		{"table action", `keymap.set("n", "<leader>q", {})`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()
			registerKeymapAPI(L)
			L.SetGlobal("keymap", L.Get(-1))
			L.Pop(1)
			defer clearKeybindingsForState(L)

			if err := L.DoString(tt.src); err == nil {
				t.Fatalf("%s: expected keymap.set to reject the action", tt.src)
			}
			if err := L.DoString(`assert(keymap.get("n", "<leader>q") == nil)`); err != nil {
				t.Fatalf("rejected binding must not be registered: %v", err)
			}
		})
	}
}
