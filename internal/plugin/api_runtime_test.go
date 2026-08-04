package plugin

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
	"teak/internal/text"
)

type apiRuntimeStub struct {
	bufferText     string
	cursor         text.Position
	selection      *text.Selection
	bufferPath     string
	bufferDirty    bool
	mode           string
	tabCount       int
	activeTab      int
	width          int
	height         int
	status         string
	lastKeys       string
	lastPanel      string
	lastFloat      UIFloatOptions
	lastFloatID    int
	lastHighlights UIHighlightRequest
	lastConfirm    UIConfirmRequest
	lastInput      UIInputRequest
	lastSelect     UISelectRequest
	lastNotice     string
}

func (r *apiRuntimeStub) BufferText() (string, error) { return r.bufferText, nil }
func (r *apiRuntimeStub) SetBufferText(value string) error {
	r.bufferText = value
	r.bufferDirty = true
	return nil
}
func (r *apiRuntimeStub) BufferCursor() (text.Position, error) { return r.cursor, nil }
func (r *apiRuntimeStub) SetBufferCursor(value text.Position) error {
	r.cursor = value
	return nil
}
func (r *apiRuntimeStub) BufferSelection() (*text.Selection, error) { return r.selection, nil }
func (r *apiRuntimeStub) InsertText(value string) error {
	r.bufferText += value
	r.bufferDirty = true
	return nil
}
func (r *apiRuntimeStub) DeleteSelection() error {
	r.selection = nil
	r.bufferDirty = true
	return nil
}
func (r *apiRuntimeStub) BufferLine(line int) (string, error) {
	lines := []string{"hello", "world"}
	if line < 0 || line >= len(lines) {
		return "", nil
	}
	return lines[line], nil
}
func (r *apiRuntimeStub) BufferLineCount() (int, error) { return 2, nil }
func (r *apiRuntimeStub) SaveBuffer() error {
	r.bufferDirty = false
	return nil
}
func (r *apiRuntimeStub) BufferFilePath() (string, error) { return r.bufferPath, nil }
func (r *apiRuntimeStub) BufferDirty() (bool, error)      { return r.bufferDirty, nil }
func (r *apiRuntimeStub) NewBuffer() (int, error) {
	r.tabCount++
	r.activeTab = r.tabCount - 1
	return r.tabCount, nil
}
func (r *apiRuntimeStub) Mode() string   { return r.mode }
func (r *apiRuntimeStub) TabCount() int  { return r.tabCount }
func (r *apiRuntimeStub) ActiveTab() int { return r.activeTab }
func (r *apiRuntimeStub) SetActiveTab(tab int) error {
	r.activeTab = tab
	return nil
}
func (r *apiRuntimeStub) OpenFile(string) error { r.tabCount++; return nil }
func (r *apiRuntimeStub) CloseTab(int) error {
	if r.tabCount > 0 {
		r.tabCount--
	}
	return nil
}
func (r *apiRuntimeStub) NextTab()                      { r.activeTab++ }
func (r *apiRuntimeStub) PrevTab()                      { r.activeTab-- }
func (r *apiRuntimeStub) Width() int                    { return r.width }
func (r *apiRuntimeStub) Height() int                   { return r.height }
func (r *apiRuntimeStub) Status() string                { return r.status }
func (r *apiRuntimeStub) SetStatus(value string)        { r.status = value }
func (r *apiRuntimeStub) FeedKeys(value string) error   { r.lastKeys = value; return nil }
func (r *apiRuntimeStub) ShowPanel(name string) error   { r.lastPanel = "show:" + name; return nil }
func (r *apiRuntimeStub) HidePanel(name string) error   { r.lastPanel = "hide:" + name; return nil }
func (r *apiRuntimeStub) TogglePanel(name string) error { r.lastPanel = "toggle:" + name; return nil }
func (r *apiRuntimeStub) NewFloat(options UIFloatOptions) (int, error) {
	r.lastFloat = options
	r.lastFloatID++
	return r.lastFloatID, nil
}
func (r *apiRuntimeStub) CloseFloat(int) error { return nil }
func (r *apiRuntimeStub) SetHighlights(request UIHighlightRequest) error {
	r.lastHighlights = request
	return nil
}
func (r *apiRuntimeStub) ClearHighlights(namespace int) error {
	r.lastHighlights = UIHighlightRequest{Namespace: namespace}
	return nil
}
func (r *apiRuntimeStub) RequestConfirm(request UIConfirmRequest) error {
	r.lastConfirm = request
	return nil
}
func (r *apiRuntimeStub) RequestInput(request UIInputRequest) error {
	r.lastInput = request
	return nil
}
func (r *apiRuntimeStub) RequestSelect(request UISelectRequest) error {
	r.lastSelect = request
	return nil
}
func (r *apiRuntimeStub) Notify(message, level string) { r.lastNotice = level + ":" + message }

func registerTestModule(L *lua.LState, name string, register func(*lua.LState)) {
	register(L)
	module := L.Get(-1)
	L.Pop(1)
	L.SetGlobal(name, module)
}

func TestLuaBufferEditorAndUIAPIsUseRuntimeBridge(t *testing.T) {
	L := newLuaStateFactory().Get()
	defer L.Close()
	registerTestModule(L, "buffer", registerBufferAPI)
	registerTestModule(L, "editor", registerEditorAPI)
	registerTestModule(L, "ui", registerUIAPI)

	runtime := &apiRuntimeStub{
		bufferText:  "hello\nworld",
		cursor:      text.Position{Line: 1, Col: 2},
		selection:   &text.Selection{Anchor: text.Position{Line: 0, Col: 1}, Head: text.Position{Line: 0, Col: 4}},
		bufferPath:  "main.go",
		bufferDirty: true,
		mode:        "normal",
		tabCount:    2,
		activeTab:   1,
		width:       100,
		height:      30,
	}
	setRuntimeForState(L, runtime)
	defer clearRuntimeForState(L)

	const source = `
assert(buffer.get_text() == "hello\nworld")
local line, col = buffer.get_cursor()
assert(line == 2 and col == 3)
local start_line, start_col, end_line, end_col = buffer.get_selection()
assert(start_line == 1 and start_col == 2 and end_line == 1 and end_col == 5)
buffer.set_text("changed")
buffer.set_cursor(3, 4)
buffer.insert("!")
buffer.delete()
assert(buffer.get_line(2) == "world")
assert(buffer.line_count() == 2)
assert(buffer.get_filepath() == "main.go")
assert(buffer.is_dirty())
assert(buffer.save())
assert(not buffer.is_dirty())

assert(editor.get_mode() == "normal")
assert(editor.get_tab_count() == 2)
assert(editor.get_active_tab() == 2)
editor.set_active_tab(1)
editor.open_file("notes.md")
editor.close_tab(1)
editor.next_tab()
editor.prev_tab()
editor.feed_keys("jk")
assert(editor.get_width() == 100 and editor.get_height() == 30)
editor.set_status("ready")
assert(editor.get_status() == "ready")
editor.echo("echo")
editor.echo_error("bad")
editor.echo_warning("warn")
editor.echo_info("info")
editor.command("hello", function() editor.set_status("command ran") end)
editor.command("hello")

assert(ui.new_buffer() == 3)
ui.show_panel("files")
ui.hide_panel("files")
ui.toggle_panel("files")
local float_id = ui.new_float({title = "Preview", content = "content", width = 40, height = 8})
ui.close_float(float_id)
ui.set_highlights(7, {{line = 0, start_col = 1, end_col = 4, fg = "#88c0d0", bold = true}})
ui.clear_highlights()
ui.input("Name", "initial", function() end)
ui.confirm("Continue?", {"Yes", "No"}, function() end)
ui.select("Pick", {"one", "two"}, function() end)
ui.notify("done", "info")
`
	if err := L.DoString(source); err != nil {
		t.Fatalf("runtime API script failed: %v", err)
	}

	if runtime.lastKeys != "jk" || runtime.lastPanel != "toggle:files" || runtime.lastNotice != "info:done" {
		t.Fatalf("runtime side effects = keys %q panel %q notice %q", runtime.lastKeys, runtime.lastPanel, runtime.lastNotice)
	}
	if runtime.lastFloat != (UIFloatOptions{Title: "Preview", Content: "content", Width: 40, Height: 8}) {
		t.Fatalf("float request = %#v", runtime.lastFloat)
	}
	if runtime.lastHighlights.Namespace != 0 || len(runtime.lastHighlights.Highlights) != 0 {
		t.Fatalf("clear highlights request = %#v", runtime.lastHighlights)
	}
	if runtime.lastInput.Prompt != "Name" || runtime.lastInput.InitialValue != "initial" || runtime.lastInput.CallbackID == 0 {
		t.Fatalf("input request = %#v", runtime.lastInput)
	}
	if runtime.lastConfirm.Message != "Continue?" || len(runtime.lastConfirm.Options) != 2 || runtime.lastConfirm.CallbackID == 0 {
		t.Fatalf("confirm request = %#v", runtime.lastConfirm)
	}
	if runtime.lastSelect.Prompt != "Pick" || len(runtime.lastSelect.Options) != 2 || runtime.lastSelect.CallbackID == 0 {
		t.Fatalf("select request = %#v", runtime.lastSelect)
	}
}

func TestLuaAPIsRejectCallsWithoutRuntime(t *testing.T) {
	L := newLuaStateFactory().Get()
	defer L.Close()
	registerTestModule(L, "buffer", registerBufferAPI)
	if err := L.DoString(`local ok = pcall(function() buffer.get_text() end); assert(not ok)`); err != nil {
		t.Fatalf("buffer API did not fail closed: %v", err)
	}
}

func TestManagerDispatchInstallsRuntimeForEventsAndKeys(t *testing.T) {
	mgr, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown()

	dir := writeBudgetPlugin(t, mgr.pluginDir, "runtime-bridge", `
autocmd.register("BufWrite", function(event)
  buffer.insert("!")
  editor.set_status(event.relative_path)
end)
keymap.set("n", "x", function()
  buffer.insert("?")
end)
`)
	if err := mgr.LoadPlugin(dir); err != nil {
		t.Fatalf("LoadPlugin() error = %v", err)
	}

	runtime := &apiRuntimeStub{bufferText: "start"}
	if exact, prefix := mgr.MatchKey("n", "x"); !exact || prefix {
		t.Fatalf("MatchKey() = exact:%t prefix:%t, want exact only", exact, prefix)
	}
	if err := mgr.DispatchEvent(runtime, "BufWrite", EventContext{RelativePath: "main.go"}); err != nil {
		t.Fatalf("DispatchEvent() error = %v", err)
	}
	if runtime.bufferText != "start!" || runtime.status != "main.go" {
		t.Fatalf("event runtime state = text %q status %q", runtime.bufferText, runtime.status)
	}
	handled, pending, err := mgr.DispatchKey(runtime, "n", "x")
	if err != nil || !handled || pending {
		t.Fatalf("DispatchKey() = handled:%t pending:%t err:%v", handled, pending, err)
	}
	if runtime.bufferText != "start!?" {
		t.Fatalf("key runtime text = %q, want start!?", runtime.bufferText)
	}
}
