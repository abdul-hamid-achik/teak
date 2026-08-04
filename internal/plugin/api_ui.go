package plugin

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// registerUIAPI registers the ui.* API functions.
func registerUIAPI(L *lua.LState) {
	mod := L.SetFuncs(L.NewTable(), uiAPIFunctions)
	L.SetField(mod, "__index", L.SetFuncs(L.NewTable(), uiAPIFunctions))
	L.Push(mod)
}

var uiAPIFunctions = map[string]lua.LGFunction{
	"new_buffer":       uiNewBuffer,
	"show_panel":       uiShowPanel,
	"hide_panel":       uiHidePanel,
	"toggle_panel":     uiTogglePanel,
	"new_float":        uiNewFloat,
	"close_float":      uiCloseFloat,
	"set_highlights":   uiSetHighlights,
	"clear_highlights": uiClearHighlights,
	"input":            uiInput,
	"confirm":          uiConfirm,
	"select":           uiSelect,
	"notify":           uiNotify,
}

// ui.new_buffer() -> bufnr
func uiNewBuffer(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.new_buffer")
	bufnr, err := runtime.NewBuffer()
	if err != nil {
		L.RaiseError("ui.new_buffer failed: %v", err)
		return 0
	}
	L.Push(lua.LNumber(bufnr))
	return 1
}

// ui.show_panel(name: string)
func uiShowPanel(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.show_panel")
	if err := runtime.ShowPanel(L.CheckString(1)); err != nil {
		L.RaiseError("ui.show_panel failed: %v", err)
	}
	return 0
}

// ui.hide_panel(name: string)
func uiHidePanel(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.hide_panel")
	if err := runtime.HidePanel(L.CheckString(1)); err != nil {
		L.RaiseError("ui.hide_panel failed: %v", err)
	}
	return 0
}

// ui.toggle_panel(name: string)
func uiTogglePanel(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.toggle_panel")
	if err := runtime.TogglePanel(L.CheckString(1)); err != nil {
		L.RaiseError("ui.toggle_panel failed: %v", err)
	}
	return 0
}

// ui.new_float(opts: table) -> float_id
func uiNewFloat(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.new_float")
	options, err := parseUIFloatOptions(L.CheckTable(1))
	if err != nil {
		L.RaiseError("ui.new_float failed: %v", err)
		return 0
	}
	floatID, err := runtime.NewFloat(options)
	if err != nil {
		L.RaiseError("ui.new_float failed: %v", err)
		return 0
	}
	L.Push(lua.LNumber(floatID))
	return 1
}

// ui.close_float(float_id: number)
func uiCloseFloat(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.close_float")
	floatID := L.CheckInt(1)
	if err := runtime.CloseFloat(floatID); err != nil {
		L.RaiseError("ui.close_float failed: %v", err)
	}
	return 0
}

func parseUIFloatOptions(table *lua.LTable) (UIFloatOptions, error) {
	options := UIFloatOptions{Title: "Plugin float", Width: 60, Height: 12}
	for _, field := range []struct {
		name   string
		target *string
	}{{"title", &options.Title}, {"content", &options.Content}} {
		value := table.RawGetString(field.name)
		if value == lua.LNil {
			continue
		}
		text, ok := value.(lua.LString)
		if !ok {
			return UIFloatOptions{}, fmt.Errorf("%s must be a string", field.name)
		}
		*field.target = string(text)
	}
	for _, field := range []struct {
		name   string
		target *int
	}{{"width", &options.Width}, {"height", &options.Height}} {
		value := table.RawGetString(field.name)
		if value == lua.LNil {
			continue
		}
		number, ok := value.(lua.LNumber)
		if !ok || number != lua.LNumber(int(number)) {
			return UIFloatOptions{}, fmt.Errorf("%s must be an integer", field.name)
		}
		*field.target = int(number)
	}
	if options.Title == "" {
		return UIFloatOptions{}, fmt.Errorf("title must not be empty")
	}
	if len(options.Content) > maxPluginUIFloatContentBytes {
		return UIFloatOptions{}, fmt.Errorf("content exceeds %d bytes", maxPluginUIFloatContentBytes)
	}
	if options.Width < 1 || options.Width > maxPluginUIFloatWidth {
		return UIFloatOptions{}, fmt.Errorf("width must be between 1 and %d", maxPluginUIFloatWidth)
	}
	if options.Height < 1 || options.Height > maxPluginUIFloatHeight {
		return UIFloatOptions{}, fmt.Errorf("height must be between 1 and %d", maxPluginUIFloatHeight)
	}
	return options, nil
}

// ui.set_highlights(ns_id, highlights: table)
func uiSetHighlights(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.set_highlights")
	namespace := L.CheckInt(1)
	highlights, err := parseUIHighlights(namespace, L.CheckTable(2))
	if err != nil {
		L.RaiseError("ui.set_highlights failed: %v", err)
		return 0
	}
	if err := runtime.SetHighlights(UIHighlightRequest{Namespace: namespace, Highlights: highlights}); err != nil {
		L.RaiseError("ui.set_highlights failed: %v", err)
	}
	return 0
}

// ui.clear_highlights(ns_id?)
func uiClearHighlights(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.clear_highlights")
	namespace := 0
	if L.GetTop() >= 1 {
		namespace = L.CheckInt(1)
	}
	if err := runtime.ClearHighlights(namespace); err != nil {
		L.RaiseError("ui.clear_highlights failed: %v", err)
	}
	return 0
}

func parseUIHighlights(namespace int, table *lua.LTable) ([]UIHighlight, error) {
	if namespace <= 0 {
		return nil, fmt.Errorf("namespace must be positive")
	}
	var highlights []UIHighlight
	for index := 1; index <= maxPluginUIHighlights+1; index++ {
		value := table.RawGetInt(index)
		if value == lua.LNil {
			break
		}
		if index > maxPluginUIHighlights {
			return nil, fmt.Errorf("at most %d highlight ranges are allowed", maxPluginUIHighlights)
		}
		entry, ok := value.(*lua.LTable)
		if !ok {
			return nil, fmt.Errorf("highlight %d must be a table", index)
		}
		line, err := requiredHighlightInt(entry, "line")
		if err != nil {
			return nil, fmt.Errorf("highlight %d: %w", index, err)
		}
		start, err := requiredHighlightInt(entry, "start_col")
		if err != nil {
			return nil, fmt.Errorf("highlight %d: %w", index, err)
		}
		end, err := requiredHighlightInt(entry, "end_col")
		if err != nil {
			return nil, fmt.Errorf("highlight %d: %w", index, err)
		}
		if line < 0 || start < 0 || end <= start {
			return nil, fmt.Errorf("highlight %d has an invalid range", index)
		}
		if end-start > maxPluginUIHighlightRangeBytes {
			return nil, fmt.Errorf("highlight %d exceeds %d bytes", index, maxPluginUIHighlightRangeBytes)
		}
		foreground, err := optionalHighlightString(entry, "fg")
		if err != nil {
			return nil, fmt.Errorf("highlight %d: %w", index, err)
		}
		background, err := optionalHighlightString(entry, "bg")
		if err != nil {
			return nil, fmt.Errorf("highlight %d: %w", index, err)
		}
		if len(foreground) > maxPluginUIHighlightColorBytes || len(background) > maxPluginUIHighlightColorBytes {
			return nil, fmt.Errorf("highlight %d color exceeds %d bytes", index, maxPluginUIHighlightColorBytes)
		}
		bold, err := optionalHighlightBool(entry, "bold")
		if err != nil {
			return nil, fmt.Errorf("highlight %d: %w", index, err)
		}
		underline, err := optionalHighlightBool(entry, "underline")
		if err != nil {
			return nil, fmt.Errorf("highlight %d: %w", index, err)
		}
		highlights = append(highlights, UIHighlight{
			Line: line, StartCol: start, EndCol: end,
			Foreground: foreground, Background: background,
			Bold: bold, Underline: underline,
		})
	}
	return highlights, nil
}

func requiredHighlightInt(table *lua.LTable, name string) (int, error) {
	value := table.RawGetString(name)
	number, ok := value.(lua.LNumber)
	if !ok || number != lua.LNumber(int(number)) {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return int(number), nil
}

func optionalHighlightString(table *lua.LTable, name string) (string, error) {
	value := table.RawGetString(name)
	if value == lua.LNil {
		return "", nil
	}
	text, ok := value.(lua.LString)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return string(text), nil
}

func optionalHighlightBool(table *lua.LTable, name string) (bool, error) {
	value := table.RawGetString(name)
	if value == lua.LNil {
		return false, nil
	}
	boolean, ok := value.(lua.LBool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return bool(boolean), nil
}

// ui.input(prompt: string, callback: function)
// ui.input(prompt: string, initial: string, callback: function)
// The callback receives (value, accepted).
func uiInput(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.input")
	prompt := L.CheckString(1)
	var initial string
	var callback *lua.LFunction
	switch L.GetTop() {
	case 2:
		callback = L.CheckFunction(2)
	case 3:
		initial = L.CheckString(2)
		callback = L.CheckFunction(3)
	default:
		L.RaiseError("ui.input expects prompt, callback, or prompt, initial, callback")
		return 0
	}
	if err := validateUIInput(prompt, initial); err != nil {
		L.RaiseError("ui.input failed: %v", err)
		return 0
	}
	callbackID, err := registerUIInputCallback(L, callback)
	if err != nil {
		L.RaiseError("ui.input failed: %v", err)
		return 0
	}
	if err := runtime.RequestInput(UIInputRequest{
		Prompt:       prompt,
		InitialValue: initial,
		CallbackID:   callbackID,
	}); err != nil {
		discardUICallback(callbackID)
		L.RaiseError("ui.input failed: %v", err)
		return 0
	}
	return 0
}

// ui.confirm(message: string, options: table, callback: function)
// The callback receives (option, one_based_index, accepted).
func uiConfirm(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.confirm")
	message := L.CheckString(1)
	optionsTable := L.CheckTable(2)
	callback := L.CheckFunction(3)
	if L.GetTop() != 3 {
		L.RaiseError("ui.confirm expects message, options table, and callback")
		return 0
	}
	options := make([]string, 0, 4)
	for i := 1; i <= maxPluginUIConfirmOptions+1; i++ {
		value := optionsTable.RawGetInt(i)
		if value == lua.LNil {
			break
		}
		if i > maxPluginUIConfirmOptions {
			L.RaiseError("ui.confirm supports at most %d options", maxPluginUIConfirmOptions)
			return 0
		}
		label, ok := value.(lua.LString)
		if !ok || string(label) == "" {
			L.RaiseError("ui.confirm option %d must be a non-empty string", i)
			return 0
		}
		options = append(options, string(label))
	}
	if len(options) == 0 {
		L.RaiseError("ui.confirm requires at least one option")
		return 0
	}
	if err := validateUIConfirm(message, options); err != nil {
		L.RaiseError("ui.confirm failed: %v", err)
		return 0
	}
	callbackID, err := registerUIConfirmCallback(L, callback)
	if err != nil {
		L.RaiseError("ui.confirm failed: %v", err)
		return 0
	}
	if err := runtime.RequestConfirm(UIConfirmRequest{
		Message:    message,
		Options:    options,
		CallbackID: callbackID,
	}); err != nil {
		discardUIConfirmCallback(callbackID)
		L.RaiseError("ui.confirm failed: %v", err)
		return 0
	}
	return 0
}

func validateUIInput(prompt, initial string) error {
	if len(prompt) > maxPluginUIInputPromptBytes {
		return fmt.Errorf("prompt exceeds %d bytes", maxPluginUIInputPromptBytes)
	}
	if len(initial) > maxPluginUIInputBytes {
		return fmt.Errorf("initial value exceeds %d bytes", maxPluginUIInputBytes)
	}
	return nil
}

func validateUIConfirm(message string, options []string) error {
	if len(message) > maxPluginUIConfirmMessageBytes {
		return fmt.Errorf("message exceeds %d bytes", maxPluginUIConfirmMessageBytes)
	}
	if len(options) == 0 || len(options) > maxPluginUIConfirmOptions {
		return fmt.Errorf("confirmation requires 1-%d options", maxPluginUIConfirmOptions)
	}
	for i, option := range options {
		if option == "" {
			return fmt.Errorf("option %d is empty", i+1)
		}
		if len(option) > maxPluginUIConfirmOptionBytes {
			return fmt.Errorf("option %d exceeds %d bytes", i+1, maxPluginUIConfirmOptionBytes)
		}
	}
	return nil
}

// ui.select(prompt: string, options: table, callback: function)
// The callback receives (option, one_based_index, accepted).
func uiSelect(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.select")
	prompt := L.CheckString(1)
	optionsTable := L.CheckTable(2)
	callback := L.CheckFunction(3)
	if L.GetTop() != 3 {
		L.RaiseError("ui.select expects prompt, options table, and callback")
		return 0
	}
	options := make([]string, 0, 8)
	for index := 1; index <= maxPluginUISelectOptions+1; index++ {
		value := optionsTable.RawGetInt(index)
		if value == lua.LNil {
			break
		}
		if index > maxPluginUISelectOptions {
			L.RaiseError("ui.select supports at most %d options", maxPluginUISelectOptions)
			return 0
		}
		option, ok := value.(lua.LString)
		if !ok || string(option) == "" {
			L.RaiseError("ui.select option %d must be a non-empty string", index)
			return 0
		}
		options = append(options, string(option))
	}
	if err := validateUISelect(prompt, options); err != nil {
		L.RaiseError("ui.select failed: %v", err)
		return 0
	}
	callbackID, err := registerUISelectCallback(L, callback)
	if err != nil {
		L.RaiseError("ui.select failed: %v", err)
		return 0
	}
	if err := runtime.RequestSelect(UISelectRequest{
		Prompt:     prompt,
		Options:    options,
		CallbackID: callbackID,
	}); err != nil {
		discardUICallback(callbackID)
		L.RaiseError("ui.select failed: %v", err)
		return 0
	}
	return 0
}

func validateUISelect(prompt string, options []string) error {
	if len(prompt) > maxPluginUIInputPromptBytes {
		return fmt.Errorf("prompt exceeds %d bytes", maxPluginUIInputPromptBytes)
	}
	if len(options) == 0 || len(options) > maxPluginUISelectOptions {
		return fmt.Errorf("selector requires 1-%d options", maxPluginUISelectOptions)
	}
	for i, option := range options {
		if option == "" {
			return fmt.Errorf("option %d is empty", i+1)
		}
		if len(option) > maxPluginUISelectOptionBytes {
			return fmt.Errorf("option %d exceeds %d bytes", i+1, maxPluginUISelectOptionBytes)
		}
	}
	return nil
}

// ui.notify(message: string, level: string?)
func uiNotify(L *lua.LState) int {
	runtime := requireRuntime(L, "ui.notify")
	message := L.CheckString(1)
	level := ""
	if L.GetTop() >= 2 {
		level = L.CheckString(2)
	}
	runtime.Notify(message, level)
	return 0
}
