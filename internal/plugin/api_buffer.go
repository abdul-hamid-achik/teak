package plugin

import (
	lua "github.com/yuin/gopher-lua"
	"teak/internal/text"
)

// registerBufferAPI registers the buffer.* API functions.
func registerBufferAPI(L *lua.LState) {
	mod := L.SetFuncs(L.NewTable(), bufferAPIFunctions)
	L.SetField(mod, "__index", L.SetFuncs(L.NewTable(), bufferAPIFunctions))
	L.Push(mod)
}

var bufferAPIFunctions = map[string]lua.LGFunction{
	"get_text":      bufferGetText,
	"set_text":      bufferSetText,
	"get_cursor":    bufferGetCursor,
	"set_cursor":    bufferSetCursor,
	"get_selection": bufferGetSelection,
	"insert":        bufferInsert,
	"delete":        bufferDelete,
	"get_line":      bufferGetLine,
	"line_count":    bufferLineCount,
	"save":          bufferSave,
	"get_filepath":  bufferGetFilepath,
	"is_dirty":      bufferIsDirty,
}

func requireRuntime(L *lua.LState, apiName string) Runtime {
	runtime := getRuntimeFromContext(L)
	if runtime == nil {
		L.RaiseError("%s is unavailable outside an active plugin dispatch context", apiName)
		return nil
	}
	return runtime
}

// buffer.get_text() -> string
func bufferGetText(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.get_text")
	text, err := runtime.BufferText()
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lua.LString(text))
	return 1
}

// buffer.set_text(text: string)
func bufferSetText(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.set_text")
	if err := runtime.SetBufferText(L.CheckString(1)); err != nil {
		L.RaiseError("buffer.set_text failed: %v", err)
		return 0
	}
	return 0
}

// buffer.get_cursor() -> line: number, col: number
func bufferGetCursor(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.get_cursor")
	cursor, err := runtime.BufferCursor()
	if err != nil {
		L.RaiseError("buffer.get_cursor failed: %v", err)
		return 0
	}
	L.Push(lua.LNumber(cursor.Line + 1)) // Lua uses 1-based indexing
	L.Push(lua.LNumber(cursor.Col + 1))
	return 2
}

// buffer.set_cursor(line: number, col: number)
func bufferSetCursor(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.set_cursor")
	line := L.CheckInt(1) - 1 // Convert to 0-based
	col := L.CheckInt(2) - 1
	if err := runtime.SetBufferCursor(text.Position{Line: line, Col: col}); err != nil {
		L.RaiseError("buffer.set_cursor failed: %v", err)
		return 0
	}
	return 0
}

// buffer.get_selection() -> start_line, start_col, end_line, end_col or nil
func bufferGetSelection(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.get_selection")
	selection, err := runtime.BufferSelection()
	if err != nil {
		L.RaiseError("buffer.get_selection failed: %v", err)
		return 0
	}
	if selection == nil || selection.IsEmpty() {
		L.Push(lua.LNil)
		return 1
	}

	start, end := selection.Ordered()
	L.Push(lua.LNumber(start.Line + 1))
	L.Push(lua.LNumber(start.Col + 1))
	L.Push(lua.LNumber(end.Line + 1))
	L.Push(lua.LNumber(end.Col + 1))
	return 4
}

// buffer.insert(text: string)
func bufferInsert(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.insert")
	if err := runtime.InsertText(L.CheckString(1)); err != nil {
		L.RaiseError("buffer.insert failed: %v", err)
		return 0
	}
	return 0
}

// buffer.delete()
func bufferDelete(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.delete")
	if err := runtime.DeleteSelection(); err != nil {
		L.RaiseError("buffer.delete failed: %v", err)
		return 0
	}
	return 0
}

// buffer.get_line(line: number) -> string
func bufferGetLine(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.get_line")
	content, err := runtime.BufferLine(L.CheckInt(1) - 1)
	if err != nil {
		L.RaiseError("buffer.get_line failed: %v", err)
		return 0
	}
	L.Push(lua.LString(string(content)))
	return 1
}

// buffer.line_count() -> number
func bufferLineCount(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.line_count")
	count, err := runtime.BufferLineCount()
	if err != nil {
		L.RaiseError("buffer.line_count failed: %v", err)
		return 0
	}
	L.Push(lua.LNumber(count))
	return 1
}

// buffer.save() -> boolean, error?
func bufferSave(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.save")
	if err := runtime.SaveBuffer(); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// buffer.get_filepath() -> string or nil
func bufferGetFilepath(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.get_filepath")
	path, err := runtime.BufferFilePath()
	if err != nil {
		L.RaiseError("buffer.get_filepath failed: %v", err)
		return 0
	}
	if path == "" {
		L.Push(lua.LNil)
	} else {
		L.Push(lua.LString(path))
	}
	return 1
}

// buffer.is_dirty() -> boolean
func bufferIsDirty(L *lua.LState) int {
	runtime := requireRuntime(L, "buffer.is_dirty")
	dirty, err := runtime.BufferDirty()
	if err != nil {
		L.RaiseError("buffer.is_dirty failed: %v", err)
		return 0
	}
	L.Push(lua.LBool(dirty))
	return 1
}
