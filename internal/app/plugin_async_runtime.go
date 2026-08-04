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
// Update. Each logical tab owns an immutable-rope snapshot, and buffer edits
// are coalesced into ordered optimistic full-document effects. This preserves
// callback order when Lua changes focus or creates a tab before editing it.
// The Runtime API exposes only one selection, so committing an edit deliberately
// collapses live multicursors to the callback's resulting primary cursor.
type pluginAsyncRuntime struct {
	editorID      uint64
	targetID      uint64
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
	tabIDs        []uint64
	targets       map[uint64]*text.Buffer
	targetVersion map[uint64]int
	selections    map[uint64]*text.Selection
	nextVirtualID uint64
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
	pluginEffectNewBuffer
	pluginEffectApplyBuffer
	pluginEffectNewFloat
	pluginEffectCloseFloat
	pluginEffectSetHighlights
	pluginEffectClearHighlights
	pluginEffectConfirm
	pluginEffectInput
	pluginEffectSelect
)

type pluginAsyncEffect struct {
	kind               pluginAsyncEffectKind
	value              string
	level              string
	index              int
	targetID           uint64
	snapshot           *pluginAsyncBufferEffect
	options            []string
	callbackID         uint64
	initial            string
	floatID            int
	float              plugin.UIFloatOptions
	highlightRequest   plugin.UIHighlightRequest
	highlightNamespace int
	expectedVersion    int
}

type pluginAsyncBufferEffect struct {
	targetID        uint64
	expectedVersion int
	rope            *text.Rope
	cursor          text.Position
	bufferChanged   bool
	cursorChanged   bool
}

var _ plugin.Runtime = (*pluginAsyncRuntime)(nil)

func newPluginAsyncRuntime(m Model) *pluginAsyncRuntime {
	r := &pluginAsyncRuntime{
		rootDir:       m.rootDir,
		width:         m.width,
		height:        m.height,
		status:        m.status,
		tabCount:      len(m.editors),
		activeTab:     m.activeTab,
		tabIDs:        make([]uint64, len(m.editors)),
		targets:       make(map[uint64]*text.Buffer),
		targetVersion: make(map[uint64]int),
		selections:    make(map[uint64]*text.Selection),
		nextVirtualID: 1 << 63,
	}
	for i := range m.editors {
		ed := &m.editors[i]
		if ed.Buffer == nil {
			continue
		}
		id := ed.ID()
		r.tabIDs[i] = id
		snapshot := text.NewBufferFromRope(ed.Buffer.Rope())
		snapshot.FilePath = ed.Buffer.FilePath
		snapshot.SetCursor(ed.Buffer.Cursor)
		r.targets[id] = snapshot
		r.targetVersion[id] = ed.Buffer.Version()
		if ed.Buffer.Selections != nil && ed.Buffer.Selections.Count() > 0 {
			selection := ed.Buffer.Selections.Primary()
			if !selection.IsEmpty() {
				selectionCopy := selection
				r.selections[id] = &selectionCopy
			}
		}
	}
	if m.isActiveDiffTab() || m.activeTab < 0 || m.activeTab >= len(r.tabIDs) {
		return r
	}
	r.setTarget(r.tabIDs[m.activeTab])
	return r
}

func (r *pluginAsyncRuntime) setTarget(targetID uint64) {
	r.targetID = targetID
	r.editorID = targetID
	r.bufferVersion = r.targetVersion[targetID]
	r.buffer = r.targets[targetID]
	r.hasBuffer = r.buffer != nil
	r.selection = nil
	if selection := r.selections[targetID]; selection != nil {
		copy := *selection
		r.selection = &copy
	}
	r.bufferChanged = false
	r.cursorChanged = false
}

func (r *pluginAsyncRuntime) flushBufferSnapshot() {
	if !r.hasBuffer || r.buffer == nil || r.targetID == 0 || (!r.bufferChanged && !r.cursorChanged) {
		return
	}
	r.effects = append(r.effects, pluginAsyncEffect{
		kind:     pluginEffectApplyBuffer,
		targetID: r.targetID,
		snapshot: &pluginAsyncBufferEffect{
			targetID:        r.targetID,
			expectedVersion: r.bufferVersion,
			rope:            r.buffer.Rope(),
			cursor:          r.buffer.Cursor,
			bufferChanged:   r.bufferChanged,
			cursorChanged:   r.cursorChanged,
		},
	})
	if r.selection == nil {
		delete(r.selections, r.targetID)
	} else {
		copy := *r.selection
		r.selections[r.targetID] = &copy
	}
	if r.bufferChanged {
		r.bufferVersion++
		r.targetVersion[r.targetID] = r.bufferVersion
	}
	r.bufferChanged = false
	r.cursorChanged = false
}

func (r *pluginAsyncRuntime) newVirtualTarget() uint64 {
	id := r.nextVirtualID
	r.nextVirtualID++
	return id
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
	r.flushBufferSnapshot()
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectSave, targetID: r.targetID})
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

func (r *pluginAsyncRuntime) NewBuffer() (int, error) {
	bufnr := r.tabCount + 1
	r.flushBufferSnapshot()
	targetID := r.newVirtualTarget()
	r.tabIDs = append(r.tabIDs, targetID)
	r.targets[targetID] = text.NewBufferFromBytes(nil)
	r.targetVersion[targetID] = 0
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectNewBuffer, targetID: targetID})
	r.tabCount++
	r.activeTab = r.tabCount - 1
	r.setTarget(targetID)
	return bufnr, nil
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
	r.flushBufferSnapshot()
	r.activeTab = idx
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectSetTab, index: idx})
	if idx < len(r.tabIDs) {
		r.setTarget(r.tabIDs[idx])
	} else {
		r.setTarget(0)
	}
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
	r.flushBufferSnapshot()
	targetID := r.newVirtualTarget()
	r.tabIDs = append(r.tabIDs, targetID)
	snapshot := text.NewBufferFromBytes(nil)
	snapshot.FilePath = filepath.Clean(path)
	r.targets[targetID] = snapshot
	r.targetVersion[targetID] = 0
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectOpen, value: filepath.Clean(path), targetID: targetID})
	r.tabCount++
	r.activeTab = r.tabCount - 1
	r.setTarget(targetID)
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
	r.flushBufferSnapshot()
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectClose, index: idx})
	if idx < len(r.tabIDs) {
		delete(r.targets, r.tabIDs[idx])
		delete(r.targetVersion, r.tabIDs[idx])
		delete(r.selections, r.tabIDs[idx])
		r.tabIDs = append(r.tabIDs[:idx], r.tabIDs[idx+1:]...)
	}
	r.tabCount--
	if r.tabCount == 0 {
		r.activeTab = 0
		r.setTarget(0)
	} else {
		if idx < r.activeTab {
			r.activeTab--
		} else if r.activeTab >= r.tabCount {
			r.activeTab = r.tabCount - 1
		}
		if r.activeTab >= 0 && r.activeTab < len(r.tabIDs) {
			r.setTarget(r.tabIDs[r.activeTab])
		}
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

func (r *pluginAsyncRuntime) NewFloat(options plugin.UIFloatOptions) (int, error) {
	if err := validatePluginFloat(options); err != nil {
		return 0, err
	}
	id, err := allocatePluginFloatID()
	if err != nil {
		return 0, err
	}
	r.effects = append(r.effects, pluginAsyncEffect{
		kind:    pluginEffectNewFloat,
		floatID: id,
		float:   options,
	})
	return id, nil
}

func (r *pluginAsyncRuntime) CloseFloat(id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid float id %d", id)
	}
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectCloseFloat, floatID: id})
	return nil
}

func (r *pluginAsyncRuntime) SetHighlights(request plugin.UIHighlightRequest) error {
	if err := validatePluginHighlightRequest(request); err != nil {
		return err
	}
	if _, err := r.activeBuffer(); err != nil {
		return err
	}
	r.flushBufferSnapshot()
	request.Highlights = append([]plugin.UIHighlight(nil), request.Highlights...)
	r.effects = append(r.effects, pluginAsyncEffect{
		kind:             pluginEffectSetHighlights,
		targetID:         r.targetID,
		expectedVersion:  r.bufferVersion,
		highlightRequest: request,
	})
	return nil
}

func (r *pluginAsyncRuntime) ClearHighlights(namespace int) error {
	if namespace < 0 {
		return fmt.Errorf("highlight namespace must not be negative")
	}
	if _, err := r.activeBuffer(); err != nil {
		return err
	}
	r.flushBufferSnapshot()
	r.effects = append(r.effects, pluginAsyncEffect{
		kind:               pluginEffectClearHighlights,
		targetID:           r.targetID,
		expectedVersion:    r.bufferVersion,
		highlightNamespace: namespace,
	})
	return nil
}

func (r *pluginAsyncRuntime) RequestConfirm(request plugin.UIConfirmRequest) error {
	if err := validatePluginConfirm(request); err != nil {
		return err
	}
	r.effects = append(r.effects, pluginAsyncEffect{
		kind:       pluginEffectConfirm,
		value:      request.Message,
		options:    append([]string(nil), request.Options...),
		callbackID: request.CallbackID,
	})
	return nil
}

func (r *pluginAsyncRuntime) RequestInput(request plugin.UIInputRequest) error {
	if err := validatePluginInput(request); err != nil {
		return err
	}
	r.effects = append(r.effects, pluginAsyncEffect{
		kind:       pluginEffectInput,
		value:      request.Prompt,
		initial:    request.InitialValue,
		callbackID: request.CallbackID,
	})
	return nil
}

func (r *pluginAsyncRuntime) RequestSelect(request plugin.UISelectRequest) error {
	if err := validatePluginSelect(request); err != nil {
		return err
	}
	r.effects = append(r.effects, pluginAsyncEffect{
		kind:       pluginEffectSelect,
		value:      request.Prompt,
		options:    append([]string(nil), request.Options...),
		callbackID: request.CallbackID,
	})
	return nil
}
func (r *pluginAsyncRuntime) Notify(message, level string) {
	r.effects = append(r.effects, pluginAsyncEffect{kind: pluginEffectStatus, value: message, level: level})
}

func (r *pluginAsyncRuntime) apply(m *Model) tea.Cmd {
	// Focus and tab effects flush the snapshot at the point where Lua issued
	// them. Flush the final target now so a callback ending with a buffer edit
	// is replayed after its preceding focus/new-buffer effect.
	r.flushBufferSnapshot()
	direct := newPluginRuntime(m)
	virtualTargets := make(map[uint64]uint64)
	for _, effect := range r.effects {
		switch effect.kind {
		case pluginEffectApplyBuffer:
			r.applyBufferEffect(m, direct, effect, virtualTargets)
		case pluginEffectSave:
			targetID := resolvePluginAsyncTarget(effect.targetID, virtualTargets)
			for i := range m.editors {
				if m.editors[i].ID() == targetID {
					if cmd := m.beginSaveForTab(i, false, false); cmd != nil {
						direct.cmds = append(direct.cmds, cmd)
					}
					break
				}
			}
		case pluginEffectOpen:
			if err := direct.OpenFile(effect.value); err == nil && effect.targetID != 0 {
				if ed := m.activeEditor(); ed != nil {
					virtualTargets[effect.targetID] = ed.ID()
				}
			}
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
		case pluginEffectNewBuffer:
			if _, err := direct.NewBuffer(); err == nil && effect.targetID != 0 {
				if ed := m.activeEditor(); ed != nil {
					virtualTargets[effect.targetID] = ed.ID()
				}
			}
		case pluginEffectNewFloat:
			_ = direct.newFloatWithID(effect.floatID, effect.float)
		case pluginEffectCloseFloat:
			id := effect.floatID
			if resolved, ok := virtualTargets[uint64(id)]; ok {
				id = int(resolved)
			}
			_ = direct.CloseFloat(id)
		case pluginEffectSetHighlights:
			r.applyHighlightEffect(m, effect, virtualTargets, direct)
		case pluginEffectClearHighlights:
			r.applyHighlightEffect(m, effect, virtualTargets, direct)
		case pluginEffectConfirm:
			_ = direct.RequestConfirm(plugin.UIConfirmRequest{
				Message:    effect.value,
				Options:    append([]string(nil), effect.options...),
				CallbackID: effect.callbackID,
			})
		case pluginEffectInput:
			_ = direct.RequestInput(plugin.UIInputRequest{
				Prompt:       effect.value,
				InitialValue: effect.initial,
				CallbackID:   effect.callbackID,
			})
		case pluginEffectSelect:
			_ = direct.RequestSelect(plugin.UISelectRequest{
				Prompt:     effect.value,
				Options:    append([]string(nil), effect.options...),
				CallbackID: effect.callbackID,
			})
		}
	}
	return direct.command()
}

func (r *pluginAsyncRuntime) applyHighlightEffect(m *Model, effect pluginAsyncEffect, virtualTargets map[uint64]uint64, direct *pluginRuntime) {
	targetID := resolvePluginAsyncTarget(effect.targetID, virtualTargets)
	index := -1
	for i := range m.editors {
		if m.editors[i].ID() == targetID {
			index = i
			break
		}
	}
	if index < 0 || m.editors[index].Buffer == nil || m.editors[index].Buffer.Version() != effect.expectedVersion {
		m.status = "Plugin highlights discarded: buffer changed while callback ran"
		return
	}
	var err error
	if effect.kind == pluginEffectSetHighlights {
		err = m.setPluginHighlightsForEditor(index, effect.highlightRequest)
	} else {
		err = m.clearPluginHighlightsForEditor(index, effect.highlightNamespace)
	}
	if err != nil {
		direct.Notify(err.Error(), "error")
	}
}

func resolvePluginAsyncTarget(targetID uint64, virtualTargets map[uint64]uint64) uint64 {
	if resolved, ok := virtualTargets[targetID]; ok {
		return resolved
	}
	return targetID
}

func (r *pluginAsyncRuntime) applyBufferEffect(m *Model, direct *pluginRuntime, effect pluginAsyncEffect, virtualTargets map[uint64]uint64) {
	snapshot := effect.snapshot
	if snapshot == nil {
		return
	}
	targetID := resolvePluginAsyncTarget(snapshot.targetID, virtualTargets)
	idx := -1
	for i := range m.editors {
		if m.editors[i].ID() == targetID {
			idx = i
			break
		}
	}
	if idx < 0 || m.editors[idx].Buffer == nil || m.editors[idx].Buffer.Version() != snapshot.expectedVersion {
		m.status = "Plugin edit discarded: buffer changed while callback ran"
		return
	}
	previous := m.activeTab
	m.activateTab(idx)
	if snapshot.bufferChanged {
		_ = direct.replaceBufferSnapshot(snapshot.rope, snapshot.cursor)
	} else if snapshot.cursorChanged {
		_ = direct.SetBufferCursor(snapshot.cursor)
	}
	m.activateTab(previous)
}
