package plugin

import (
	lua "github.com/yuin/gopher-lua"
	"teak/internal/text"
)

// bufferContext stores the current buffer context for Lua scripts.
type bufferContext struct {
	buffer *text.Buffer
}

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

// getBufferFromContext gets the current buffer from Lua context.
func getBufferFromContext(L *lua.LState) *text.Buffer {
	// For now, we'll need to get this from the app context
	// This is a simplified version - in production, you'd store this properly
	return nil
}

// buffer.get_text() -> string
func bufferGetText(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("no active buffer"))
		return 2
	}

	L.Push(lua.LString(buf.Content()))
	return 1
}

// buffer.set_text(text: string)
func bufferSetText(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.RaiseError("no active buffer")
		return 0
	}

	text := L.CheckString(1)
	buf.LoadContentWithTabSize([]byte(text), 4)

	return 0
}

// buffer.get_cursor() -> line: number, col: number
func bufferGetCursor(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.Push(lua.LNumber(1))
		L.Push(lua.LNumber(1))
		return 2
	}

	cursor := buf.Cursor
	L.Push(lua.LNumber(cursor.Line + 1)) // Lua uses 1-based indexing
	L.Push(lua.LNumber(cursor.Col + 1))
	return 2
}

// buffer.set_cursor(line: number, col: number)
func bufferSetCursor(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.RaiseError("no active buffer")
		return 0
	}

	line := L.CheckInt(1) - 1 // Convert to 0-based
	col := L.CheckInt(2) - 1

	buf.SetCursor(text.Position{Line: line, Col: col})

	return 0
}

// buffer.get_selection() -> start_line, start_col, end_line, end_col or nil
func bufferGetSelection(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil || buf.Selections == nil || buf.Selections.Count() == 0 || buf.Selections.Primary().IsEmpty() {
		L.Push(lua.LNil)
		return 1
	}

	sel := buf.Selections.Primary()
	start, end := sel.Ordered()
	L.Push(lua.LNumber(start.Line + 1))
	L.Push(lua.LNumber(start.Col + 1))
	L.Push(lua.LNumber(end.Line + 1))
	L.Push(lua.LNumber(end.Col + 1))
	return 4
}

// buffer.insert(text: string)
func bufferInsert(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.RaiseError("no active buffer")
		return 0
	}

	text := L.CheckString(1)
	buf.InsertAtCursor([]byte(text))

	return 0
}

// buffer.delete()
func bufferDelete(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.RaiseError("no active buffer")
		return 0
	}

	buf.DeleteSelection()

	return 0
}

// buffer.get_line(line: number) -> string
func bufferGetLine(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.Push(lua.LNil)
		return 1
	}

	line := L.CheckInt(1) - 1
	content := buf.Line(line)
	L.Push(lua.LString(string(content)))
	return 1
}

// buffer.line_count() -> number
func bufferLineCount(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.Push(lua.LNumber(0))
		return 1
	}

	L.Push(lua.LNumber(buf.LineCount()))
	return 1
}

// buffer.save() -> boolean, error?
func bufferSave(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString("no active buffer"))
		return 2
	}

	if err := buf.Save(); err != nil {
		L.Push(lua.LFalse)
		L.Push(lua.LString(err.Error()))
		return 2
	}

	L.Push(lua.LTrue)
	return 1
}

// buffer.get_filepath() -> string or nil
func bufferGetFilepath(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.Push(lua.LNil)
		return 1
	}

	if buf.FilePath == "" {
		L.Push(lua.LNil)
	} else {
		L.Push(lua.LString(buf.FilePath))
	}
	return 1
}

// buffer.is_dirty() -> boolean
func bufferIsDirty(L *lua.LState) int {
	buf := getBufferFromContext(L)
	if buf == nil {
		L.Push(lua.LFalse)
		return 1
	}

	L.Push(lua.LBool(buf.Dirty()))
	return 1
}
