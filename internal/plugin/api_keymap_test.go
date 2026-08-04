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
		keymap.set("a", "<leader>x", "command.global", { desc = "Global command" })
		keymap.set("tree", "z", "command.tree_z", { desc = "Tree Z" })
		keymap.set("tree", "a", "command.tree_a", { desc = "Tree A" })
		local action = keymap.get("n", "<leader>x")
		assert(action == "command.open")
		assert(keymap.which_key("<leader>x") == "Open file")
		assert(keymap.which_key("<leader>x", "n") == "Open file")
		assert(keymap.which_key("<leader>x", "tree") == "Global command")
		local all = keymap.list()
		assert(#all == 4)
		assert(all[1].mode == "a")
		assert(all[1].keys == "<leader>x")
		assert(all[1].desc == "Global command")
		assert(all[2].mode == "n")
		assert(all[3].mode == "tree")
		assert(all[3].keys == "a")
		assert(all[4].mode == "tree")
		assert(all[4].keys == "z")
		local normal = keymap.list("n")
		assert(#normal == 1)
		assert(normal[1].mode == "n")
		assert(normal[1].keys == "<leader>x")
		keymap.unset("n", "<leader>x")
		assert(keymap.get("n", "<leader>x") == "command.global")
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
