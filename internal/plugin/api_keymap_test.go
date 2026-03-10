package plugin

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestKeymapSetGetUnsetAndClear(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	registerKeymapAPI(L)
	mod := L.Get(-1)
	L.Pop(1)
	L.SetGlobal("keymap", mod)
	defer clearKeybindingsForState(L)

	if err := L.DoString(`
		keymap.set("n", "<leader>x", "command.open", { desc = "Open file" })
		local action = keymap.get("n", "<leader>x")
		assert(action == "command.open")
		assert(keymap.which_key("<leader>x") == "Open file")
		keymap.unset("n", "<leader>x")
		assert(keymap.get("n", "<leader>x") == nil)
		keymap.set("n", "<leader>a", "a")
		keymap.set("i", "<leader>b", "b")
		keymap.clear("n")
		assert(keymap.get("n", "<leader>a") == nil)
		assert(keymap.get("i", "<leader>b") == "b")
		keymap.clear()
		assert(keymap.get("i", "<leader>b") == nil)
	`); err != nil {
		t.Fatalf("DoString() error = %v", err)
	}
}

func TestKeymapStateIsolation(t *testing.T) {
	L1 := lua.NewState()
	defer L1.Close()
	registerKeymapAPI(L1)
	mod1 := L1.Get(-1)
	L1.Pop(1)
	L1.SetGlobal("keymap", mod1)
	defer clearKeybindingsForState(L1)

	L2 := lua.NewState()
	defer L2.Close()
	registerKeymapAPI(L2)
	mod2 := L2.Get(-1)
	L2.Pop(1)
	L2.SetGlobal("keymap", mod2)
	defer clearKeybindingsForState(L2)

	if err := L1.DoString(`keymap.set("n", "x", "one", { desc = "First" })`); err != nil {
		t.Fatalf("L1 DoString() error = %v", err)
	}
	if err := L2.DoString(`assert(keymap.get("n", "x") == nil)`); err != nil {
		t.Fatalf("L2 should not see L1 bindings: %v", err)
	}
}
