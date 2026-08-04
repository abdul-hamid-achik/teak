package plugin

import (
	"reflect"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestParseUIFloatOptionsDefaultsAndValues(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	defaults, err := parseUIFloatOptions(L.NewTable())
	if err != nil {
		t.Fatalf("parseUIFloatOptions(defaults) error = %v", err)
	}
	if defaults.Title != "Plugin float" || defaults.Width != 60 || defaults.Height != 12 {
		t.Fatalf("defaults = %#v", defaults)
	}

	configured := L.NewTable()
	L.SetField(configured, "title", lua.LString("Preview"))
	L.SetField(configured, "content", lua.LString("hello"))
	L.SetField(configured, "width", lua.LNumber(42))
	L.SetField(configured, "height", lua.LNumber(8))
	got, err := parseUIFloatOptions(configured)
	if err != nil {
		t.Fatalf("parseUIFloatOptions(configured) error = %v", err)
	}
	want := UIFloatOptions{Title: "Preview", Content: "hello", Width: 42, Height: 8}
	if got != want {
		t.Fatalf("configured = %#v, want %#v", got, want)
	}
}

func TestParseUIFloatOptionsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value lua.LValue
	}{
		{name: "empty title", field: "title", value: lua.LString("")},
		{name: "non-string content", field: "content", value: lua.LNumber(1)},
		{name: "non-integer width", field: "width", value: lua.LNumber(1.5)},
		{name: "too small height", field: "height", value: lua.LNumber(0)},
		{name: "too large width", field: "width", value: lua.LNumber(maxPluginUIFloatWidth + 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()
			table := L.NewTable()
			L.SetField(table, tt.field, tt.value)
			if _, err := parseUIFloatOptions(table); err == nil {
				t.Fatalf("parseUIFloatOptions() error = nil for %s", tt.name)
			}
		})
	}

	L := lua.NewState()
	defer L.Close()
	large := L.NewTable()
	L.SetField(large, "content", lua.LString(strings.Repeat("x", maxPluginUIFloatContentBytes+1)))
	if _, err := parseUIFloatOptions(large); err == nil {
		t.Fatal("parseUIFloatOptions() accepted oversized content")
	}
}

func TestParseUIHighlights(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	table := L.NewTable()
	entry := L.NewTable()
	L.SetField(entry, "line", lua.LNumber(2))
	L.SetField(entry, "start_col", lua.LNumber(1))
	L.SetField(entry, "end_col", lua.LNumber(7))
	L.SetField(entry, "fg", lua.LString("#88c0d0"))
	L.SetField(entry, "bg", lua.LString("#2e3440"))
	L.SetField(entry, "bold", lua.LTrue)
	table.Append(entry)

	got, err := parseUIHighlights(9, table)
	if err != nil {
		t.Fatalf("parseUIHighlights() error = %v", err)
	}
	want := []UIHighlight{{Line: 2, StartCol: 1, EndCol: 7, Foreground: "#88c0d0", Background: "#2e3440", Bold: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseUIHighlights() = %#v, want %#v", got, want)
	}
}

func TestParseUIHighlightsRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name      string
		namespace int
		makeTable func(*lua.LState) *lua.LTable
	}{
		{name: "non-positive namespace", namespace: 0, makeTable: func(L *lua.LState) *lua.LTable { return L.NewTable() }},
		{name: "missing line", namespace: 1, makeTable: func(L *lua.LState) *lua.LTable {
			table, entry := L.NewTable(), L.NewTable()
			L.SetField(entry, "start_col", lua.LNumber(0))
			L.SetField(entry, "end_col", lua.LNumber(1))
			table.Append(entry)
			return table
		}},
		{name: "reversed range", namespace: 1, makeTable: func(L *lua.LState) *lua.LTable {
			table, entry := L.NewTable(), L.NewTable()
			L.SetField(entry, "line", lua.LNumber(0))
			L.SetField(entry, "start_col", lua.LNumber(4))
			L.SetField(entry, "end_col", lua.LNumber(2))
			table.Append(entry)
			return table
		}},
		{name: "non-string color", namespace: 1, makeTable: func(L *lua.LState) *lua.LTable {
			table, entry := L.NewTable(), L.NewTable()
			L.SetField(entry, "line", lua.LNumber(0))
			L.SetField(entry, "start_col", lua.LNumber(0))
			L.SetField(entry, "end_col", lua.LNumber(1))
			L.SetField(entry, "fg", lua.LNumber(1))
			table.Append(entry)
			return table
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()
			if _, err := parseUIHighlights(tt.namespace, tt.makeTable(L)); err == nil {
				t.Fatal("parseUIHighlights() accepted invalid input")
			}
		})
	}

	L := lua.NewState()
	defer L.Close()
	tooMany := L.NewTable()
	for i := 0; i < maxPluginUIHighlights+1; i++ {
		entry := L.NewTable()
		L.SetField(entry, "line", lua.LNumber(0))
		L.SetField(entry, "start_col", lua.LNumber(i))
		L.SetField(entry, "end_col", lua.LNumber(i+1))
		tooMany.Append(entry)
	}
	if _, err := parseUIHighlights(1, tooMany); err == nil {
		t.Fatal("parseUIHighlights() accepted too many ranges")
	}

	largeColor := L.NewTable()
	entry := L.NewTable()
	L.SetField(entry, "line", lua.LNumber(0))
	L.SetField(entry, "start_col", lua.LNumber(0))
	L.SetField(entry, "end_col", lua.LNumber(1))
	L.SetField(entry, "fg", lua.LString(strings.Repeat("x", maxPluginUIHighlightColorBytes+1)))
	largeColor.Append(entry)
	if _, err := parseUIHighlights(1, largeColor); err == nil {
		t.Fatal("parseUIHighlights() accepted an oversized color")
	}
}

func TestValidateUIInputBoundsPromptAndInitialValue(t *testing.T) {
	if err := validateUIInput(strings.Repeat("p", maxPluginUIInputPromptBytes+1), ""); err == nil {
		t.Fatal("validateUIInput() accepted an oversized prompt")
	}
	if err := validateUIInput("Prompt", strings.Repeat("i", maxPluginUIInputBytes+1)); err == nil {
		t.Fatal("validateUIInput() accepted an oversized initial value")
	}
	if err := validateUIInput("Prompt", "initial"); err != nil {
		t.Fatalf("validateUIInput() rejected bounded values: %v", err)
	}
}

func TestValidateUIConfirmBoundsMessageAndOptions(t *testing.T) {
	if err := validateUIConfirm(strings.Repeat("m", maxPluginUIConfirmMessageBytes+1), []string{"OK"}); err == nil {
		t.Fatal("validateUIConfirm() accepted an oversized message")
	}
	if err := validateUIConfirm("Choose", []string{strings.Repeat("o", maxPluginUIConfirmOptionBytes+1)}); err == nil {
		t.Fatal("validateUIConfirm() accepted an oversized option")
	}
	if err := validateUIConfirm("Choose", []string{"OK", "Cancel"}); err != nil {
		t.Fatalf("validateUIConfirm() rejected bounded values: %v", err)
	}
}

func TestValidateUISelectBoundsPromptAndOptions(t *testing.T) {
	if err := validateUISelect(strings.Repeat("p", maxPluginUIInputPromptBytes+1), []string{"one"}); err == nil {
		t.Fatal("validateUISelect() accepted an oversized prompt")
	}
	if err := validateUISelect("Pick", nil); err == nil {
		t.Fatal("validateUISelect() accepted an empty option list")
	}
	if err := validateUISelect("Pick", []string{strings.Repeat("o", maxPluginUISelectOptionBytes+1)}); err == nil {
		t.Fatal("validateUISelect() accepted an oversized option")
	}
	if err := validateUISelect("Pick", []string{"one", "two"}); err != nil {
		t.Fatalf("validateUISelect() rejected bounded values: %v", err)
	}
}
