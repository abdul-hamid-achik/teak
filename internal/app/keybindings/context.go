package keybindings

import (
	"teak/internal/app"
	"teak/internal/app/modes"
)

type Context struct {
	Focus app.FocusArea
	Mode  modes.ModeID
}

func NewContext(focus app.FocusArea, mode modes.ModeID) Context {
	return Context{
		Focus: focus,
		Mode:  mode,
	}
}

func (c Context) IsEditor() bool {
	return c.Focus == app.FocusEditor
}

func (c Context) IsTree() bool {
	return c.Focus == app.FocusTree
}

func (c Context) IsGitPanel() bool {
	return c.Focus == app.FocusGitPanel
}

func (c Context) IsProblems() bool {
	return c.Focus == app.FocusProblems
}

func (c Context) IsDebugger() bool {
	return c.Focus == app.FocusDebugger
}

func (c Context) IsAgent() bool {
	return c.Focus == app.FocusAgent
}

func (c Context) IsNormalMode() bool {
	return c.Mode == modes.ModeNormal
}

func (c Context) IsInsertMode() bool {
	return c.Mode == modes.ModeInsert
}

func (c Context) IsAnyMode(m ...modes.ModeID) bool {
	for _, mode := range m {
		if c.Mode == mode {
			return true
		}
	}
	return false
}

func (c Context) IsAnyFocus(f ...app.FocusArea) bool {
	for _, focus := range f {
		if c.Focus == focus {
			return true
		}
	}
	return false
}
