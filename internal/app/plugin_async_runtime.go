package app

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"teak/internal/plugin"
	"teak/internal/text"
)

// pluginAsyncRuntime is a private, mutable snapshot used exclusively by a
// plugin tea.Cmd. It never retains a pointer to Model or Editor: Lua can run
// on a worker goroutine while all real model mutations are replayed later in
// Update. Buffer edits are coalesced into one optimistic full-document effect.
// The Runtime API exposes only one selection, so committing an edit deliberately
// collapses live multicursors to the callback's resulting primary cursor.
type pluginAsyncRuntime struct {
	editorID      uint64
	bufferVersion int
	buffer        *text.Buffer
	hasBuffer     bool
	selection     *text.Selection
	bufferChanged bool
	cursorChanged bool
	rootDir       string
	width         int
	height        int
	status        string
	tabCount      int
	activeTab     int
	effects       []pluginAsyncEffect
}

type pluginAsyncEffectKind uint8

const (
	pluginEffectSave pluginAsyncEffectKind = iota
	pluginEffectOpen
	pluginEffectClose
	pluginEffectSetTab
	pluginEffectFeedKeys
	pluginEffectShowPanel
	pluginEffectHidePanel
	pluginEffectTogglePanel
	pluginEffectStatus
)

type pluginAsyncEffect struct {
	kind  pluginAsyncEffectKind
	value string
	level string
	index int
}

var _ plugin.Runtime = (*pluginAsyncRuntime)(nil)

func newPluginAsyncRuntime(m Model) *pluginAsyncRuntime {
	r := &pluginAsyncRuntime{
		rootDir:   m.rootDir,
		width:     m.width,
		height:    m.height,
		status:    m.status,
		tabCount:  len(m.editors),
		activeTab: m.activeTab,
	}
	if m.isActiveDiffTab() {
		return r
	}
	ed := m.activeEditor()
	if ed == nil || ed.Buffer == nil {
		return r
	}
	r.editorID = ed.ID()
	r.bufferVersion = ed.Buffer.Version()
	// Rope snapshots are immutable, so the worker can share the live document
	// without copying or flattening it on the Bubble Tea update goroutine.
	r.buffer = text.NewBufferFromRope(ed.Buffer.Rope())
	r.buffer.FilePath = ed.Buffer.FilePath
	r.buffer.SetCursor(ed.Buffer.Cursor)
	if ed.Buffer.Selections != nil && ed.Buffer.Selections.Count() > 0 {
		selection := ed.Buffer.Selections.Primary()
		if !selection.IsEmpty() {
			r.selection = &selection
		}
	}
	r.hasBuffer = true
	return r
}

func (r *pluginAsyncRuntime) activeBuffer() (*text.Buffer, error) {
	if !r.hasBuffer || r.buffer == nil {
		return nil, fmt.Errorf("no active buffer")
	}
	return r.buffer, nil
}

func (r *pluginAsyncRuntime) clampPosition(buf *text.Buffer, pos text.Position) text.Position {
	if pos.Line < 0 {
		pos.Line = 0
	}
	if pos.Col < 0 {
		pos.Col = 0
	}
	if count := buf.LineCount(); count > 0 && pos.Line >= count {
		pos.Line = count - 1
	}
	if pos.Col > len(buf.Line(pos.Line)) {
		pos.Col = len(buf.Line(pos.Line))
	}
	return pos
}

func (r *pluginAsyncRuntime) BufferText() (string, error) {
	buf, err := r.activeBuffer()
	if err != nil {
		return "", err
	}
	return buf.Content(), nil
}

func (r *pluginAsyncRuntime) SetBufferText(value string) error {
	buf, err := r.activeBuffer()
	if err != nil {
		return err
	}
	if buf.Content() == value {
		return nil
	}
	end := buf.Rope().OffsetToPosition(buf.Rope().Len())
	buf.ReplaceRange(text.Position{}, end, []byte(value))
	buf.SetCursor(text.Position{})
	r.selection = nil
	r.bufferChanged, r.cursorChanged = true, true
	return nil
}

func (r *pluginAsyncRuntime) BufferCursor() (text.Position, error) {
	buf, err := r.activeBuffer()
	if err != nil {
		return text.Position{}, err
	}
	return buf.Cursor, nil
}

func (r *pluginAsyncRuntime) SetBufferCursor(pos text.Position) error {
	buf, err := r.activeBuffer()
	if err != nil {
		return err
	}
	pos = r.clampPosition(buf, pos)
	if buf.Cursor != pos {
		buf.SetCursor(pos)
		r.cursorChanged = true
	}
	return nil
}

func (r *pluginAsyncRuntime) BufferSelection() (*text.Selection, error) {
	if _, err := r.activeBuffer(); err != nil {
		return nil, err
	}
	if r.selection == nil {
		return nil, nil
	}
	selection := *r.selection
	return &selection, nil
}

func (r *pluginAsyncRuntime) InsertText(value string) error {
	buf, err := r.activeBuffer()
	if err != nil {
		return err
	}
	beforeVersion, beforeCursor := buf.Version(), buf.Cursor
	if r.selection != nil {
		start, end := r.selection.Ordered()
		buf.ReplaceRange(start, end, []byte(value))
		buf.SetCursor(buf.Rope().OffsetToPosition(buf.Rope().PositionToOffset(start) + len(value)))
		r.selection = nil
	} else {
		buf.InsertAtCursor([]byte(value))
	}
	r.bufferChanged = r.bufferChanged || buf.Version() != beforeVersion
	r.cursorChanged = r.cursorChanged || buf.Cursor != beforeCursor
	return nil
}

func (r *pluginAsyncRuntime) DeleteSelection() error {
	buf, err := r.activeBuffer()
	if err != nil {
		return err
	}
	beforeVersion, beforeCursor := buf.Version(), buf.Cursor
	if r.selection != nil {
		start, end := r.selection.Ordered()
		buf.ReplaceRange(start, end, nil)
		buf.SetCursor(start)
		r.selection = nil
	}
	r.bufferChanged = r.bufferChanged || buf.Version() != beforeVersion
	r.cursorChanged = r.cursorChanged || buf.Cursor != beforeCursor
	return nil
}

func (r *pluginAsyncRuntime) BufferLine(line int) (string, error) {
	buf, err := r.activeBuffer()
	if err != nil {
		return "", err
	}
	if line < 0 {
		line = 0
	}
	if line >= buf.LineCount() {
		return "", fmt.Errorf("line %d out of range", line+1)
	}
	return string(buf.Line(line)), nil
}

func (r *pluginAsyncRuntime) BufferLineCount() (int, error) {
	buf, err := r.activeBuffer()
	if err != nil {
		return 0, err
	}
	return buf.LineCount(), nil
}

func (r *pluginAsyncRuntime) SaveBuffer() error {
	buf, err := r.activeBuffer()
	if err != nil {
		return err
	}
	if buf.FilePath == "" {
		return fmt.Errorf("active buffer has no file path")
	}
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectSave})
	return nil
}

func (r *pluginAsyncRuntime) BufferFilePath() (string, error) {
	buf, err := r.activeBuffer()
	if err != nil {
		return "", err
	}
	return buf.FilePath, nil
}
func (r *pluginAsyncRuntime) BufferDirty() (bool, error) {
	buf, err := r.activeBuffer()
	if err != nil {
		return false, err
	}
	return buf.Dirty(), nil
}
func (r *pluginAsyncRuntime) Mode() string  { return "normal" }
func (r *pluginAsyncRuntime) TabCount() int { return r.tabCount }
func (r *pluginAsyncRuntime) ActiveTab() int {
	if r.tabCount == 0 {
		return 0
	}
	return r.activeTab
}

func (r *pluginAsyncRuntime) SetActiveTab(idx int) error {
	if idx < 0 || idx >= r.tabCount {
		return fmt.Errorf("invalid tab index %d", idx+1)
	}
	r.activeTab = idx
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectSetTab, index: idx})
	return nil
}

func (r *pluginAsyncRuntime) OpenFile(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		if r.rootDir != "" {
			path = filepath.Join(r.rootDir, path)
		} else if abs, err := filepath.Abs(path); err == nil {
			path = abs
		} else {
			return err
		}
	}
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectOpen, value: filepath.Clean(path)})
	r.tabCount++
	r.activeTab = r.tabCount - 1
	return nil
}

func (r *pluginAsyncRuntime) CloseTab(idx int) error {
	if r.tabCount == 0 {
		return fmt.Errorf("no open tabs")
	}
	if idx == -1 {
		idx = r.activeTab
	}
	if idx < 0 || idx >= r.tabCount {
		return fmt.Errorf("invalid tab index %d", idx+1)
	}
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectClose, index: idx})
	r.tabCount--
	if r.tabCount == 0 {
		r.activeTab = 0
	} else if r.activeTab >= r.tabCount {
		r.activeTab = r.tabCount - 1
	}
	return nil
}

func (r *pluginAsyncRuntime) NextTab() {
	if r.tabCount > 0 {
		_ = r.SetActiveTab((r.activeTab + 1) % r.tabCount)
	}
}
func (r *pluginAsyncRuntime) PrevTab() {
	if r.tabCount > 0 {
		_ = r.SetActiveTab((r.activeTab - 1 + r.tabCount) % r.tabCount)
	}
}
func (r *pluginAsyncRuntime) Width() int     { return r.width }
func (r *pluginAsyncRuntime) Height() int    { return r.height }
func (r *pluginAsyncRuntime) Status() string { return r.status }
func (r *pluginAsyncRuntime) SetStatus(status string) {
	r.status = status
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectStatus, value: status})
}
func (r *pluginAsyncRuntime) FeedKeys(keys string) error {
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectFeedKeys, value: keys})
	return nil
}
func (r *pluginAsyncRuntime) ShowPanel(name string) error {
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectShowPanel, value: name})
	return nil
}
func (r *pluginAsyncRuntime) HidePanel(name string) error {
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectHidePanel, value: name})
	return nil
}
func (r *pluginAsyncRuntime) TogglePanel(name string) error {
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectTogglePanel, value: name})
	return nil
}
func (r *pluginAsyncRuntime) Notify(message, level string) {
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectStatus, value: message, level: level})
}

func (r *pluginAsyncRuntime) apply(m *Model) tea.Cmd {
	direct := newPluginRuntime(m)
	if (r.bufferChanged || r.cursorChanged) && r.editorID != 0 {
		idx := -1
		for i := range m.editors {
			if m.editors[i].ID() == r.editorID {
				idx = i
				break
			}
		}
		if idx < 0 || m.editors[idx].Buffer.Version() != r.bufferVersion {
			m.status = "Plugin edit discarded: buffer changed while callback ran"
		} else {
			previous := m.activeTab
			m.activateTab(idx)
			if r.bufferChanged {
				_ = direct.replaceBufferSnapshot(r.buffer.Rope(), r.buffer.Cursor)
			} else if r.cursorChanged {
				_ = direct.SetBufferCursor(r.buffer.Cursor)
			}
			m.activateTab(previous)
		}
	}
	for _, effect := range r.effects {
		switch effect.kind {
		case pluginEffectSave:
			for i := range m.editors {
				if m.editors[i].ID() == r.editorID {
					if cmd := m.beginSaveForTab(i, false, false); cmd != nil {
						direct.cmds = append(direct.cmds, cmd)
					}
					break
				}
			}
		case pluginEffectOpen:
			_ = direct.OpenFile(effect.value)
		case pluginEffectClose:
			_ = direct.CloseTab(effect.index)
		case pluginEffectSetTab:
			_ = direct.SetActiveTab(effect.index)
		case pluginEffectFeedKeys:
			_ = direct.FeedKeys(effect.value)
		case pluginEffectShowPanel:
			_ = direct.ShowPanel(effect.value)
		case pluginEffectHidePanel:
			_ = direct.HidePanel(effect.value)
		case pluginEffectTogglePanel:
			_ = direct.TogglePanel(effect.value)
		case pluginEffectStatus:
			if effect.level == "" {
				direct.SetStatus(effect.value)
			} else {
				direct.Notify(effect.value, effect.level)
			}
		}
	}
	return direct.command()
}
