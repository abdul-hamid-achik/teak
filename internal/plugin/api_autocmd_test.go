package plugin

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestAutocmdRegisterListUnregisterAndClear(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	registerAutocmdAPI(L)
	L.SetGlobal("autocmd", L.Get(-1))
	L.Pop(1)

	if err := L.DoString(`
function on_read(ev)
  last_event = ev.event
end

autocmd.register("BufRead", on_read, { pattern = "*.go", group = "tests", once = true })
entries = autocmd.list("BufRead")
entry_count = #entries
entry_pattern = entries[1].pattern
entry_group = entries[1].group
entry_once = entries[1].once

autocmd.unregister("BufRead", on_read)
after_unregister = #autocmd.list("BufRead")

autocmd.register("BufWrite", on_read)
autocmd.clear("BufWrite")
after_clear = #autocmd.list("BufWrite")
`); err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	if got := L.GetGlobal("entry_count").String(); got != "1" {
		t.Fatalf("entry_count = %q, want %q", got, "1")
	}
	if got := L.GetGlobal("entry_pattern").String(); got != "*.go" {
		t.Fatalf("entry_pattern = %q, want %q", got, "*.go")
	}
	if got := L.GetGlobal("entry_group").String(); got != "tests" {
		t.Fatalf("entry_group = %q, want %q", got, "tests")
	}
	if got := L.GetGlobal("entry_once").String(); got != "true" {
		t.Fatalf("entry_once = %q, want %q", got, "true")
	}
	if got := L.GetGlobal("after_unregister").String(); got != "0" {
		t.Fatalf("after_unregister = %q, want %q", got, "0")
	}
	if got := L.GetGlobal("after_clear").String(); got != "0" {
		t.Fatalf("after_clear = %q, want %q", got, "0")
	}
}

func TestAutocmdTriggerMatchesPatternAndOnce(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	registerAutocmdAPI(L)
	L.SetGlobal("autocmd", L.Get(-1))
	L.Pop(1)

	if err := L.DoString(`
read_count = 0
once_count = 0

autocmd.register("BufRead", function(ev)
  read_count = read_count + 1
  last_event = ev.event
  last_file = ev.file
  last_relative = ev.relative_path
end, { pattern = "*.go" })

autocmd.register("BufRead", function(ev)
  once_count = once_count + 1
end, { once = true })
`); err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	goCtx := EventContext{
		Event:        EventBufRead,
		FilePath:     "/tmp/main.go",
		RelativePath: "main.go",
	}
	if err := triggerAutocommandsForState(L, goCtx); err != nil {
		t.Fatalf("triggerAutocommandsForState() error = %v", err)
	}
	if err := triggerAutocommandsForState(L, goCtx); err != nil {
		t.Fatalf("second triggerAutocommandsForState() error = %v", err)
	}
	txtCtx := EventContext{
		Event:        EventBufRead,
		FilePath:     "/tmp/main.txt",
		RelativePath: "main.txt",
	}
	if err := triggerAutocommandsForState(L, txtCtx); err != nil {
		t.Fatalf("txt triggerAutocommandsForState() error = %v", err)
	}

	if got := L.GetGlobal("read_count").String(); got != "2" {
		t.Fatalf("read_count = %q, want %q", got, "2")
	}
	if got := L.GetGlobal("once_count").String(); got != "1" {
		t.Fatalf("once_count = %q, want %q", got, "1")
	}
	if got := L.GetGlobal("last_event").String(); got != "BufRead" {
		t.Fatalf("last_event = %q, want %q", got, "BufRead")
	}
	if got := L.GetGlobal("last_file").String(); got != "/tmp/main.go" {
		t.Fatalf("last_file = %q, want %q", got, "/tmp/main.go")
	}
	if got := L.GetGlobal("last_relative").String(); got != "main.go" {
		t.Fatalf("last_relative = %q, want %q", got, "main.go")
	}
}
