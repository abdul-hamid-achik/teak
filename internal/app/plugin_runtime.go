package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"teak/internal/editor"
	"teak/internal/plugin"
	"teak/internal/text"
)

var _ plugin.Runtime = (*pluginRuntime)(nil)

type pluginRuntime struct {
	model             *Model
	cmds              []tea.Cmd
	retokenizeVersion int
}

func newPluginRuntime(model *Model) *pluginRuntime {
	return &pluginRuntime{
		model:             model,
		retokenizeVersion: -1,
	}
}

func (r *pluginRuntime) command() tea.Cmd {
	if r.retokenizeVersion >= 0 {
		version := r.retokenizeVersion
		r.cmds = append(r.cmds, func() tea.Msg {
			return editor.RetokenizeMsg{Version: version}
		})
		r.retokenizeVersion = -1
	}
	if len(r.cmds) == 0 {
		return nil
	}
	return tea.Batch(r.cmds...)
}

func (r *pluginRuntime) activeEditor() (*editor.Editor, error) {
	if r.model.isActiveDiffTab() {
		return nil, fmt.Errorf("no active buffer in diff view")
	}
	ed := r.model.activeEditor()
	if ed == nil || ed.Buffer == nil {
		return nil, fmt.Errorf("no active buffer")
	}
	return ed, nil
}

func (r *pluginRuntime) clampPosition(buf *text.Buffer, pos text.Position) text.Position {
	if pos.Line < 0 {
		pos.Line = 0
	}
	if pos.Col < 0 {
		pos.Col = 0
	}
	lineCount := buf.LineCount()
	if lineCount <= 0 {
		return text.Position{}
	}
	if pos.Line >= lineCount {
		pos.Line = lineCount - 1
	}
	lineLen := len(buf.Line(pos.Line))
	if pos.Col > lineLen {
		pos.Col = lineLen
	}
	return pos
}

func (r *pluginRuntime) syncActiveEditorAfterEdit(prevVersion int, prevCursor text.Position) error {
	ed, err := r.activeEditor()
	if err != nil {
		return err
	}
	ed.Viewport.EnsureCursorVisible(ed.Buffer.Cursor, ed.Buffer.LineCount())
	if ed.Buffer.Version() == prevVersion {
		return nil
	}
	if r.model.activeTab >= 0 && r.model.activeTab < len(r.model.tabBar.Tabs) {
		r.model.tabBar.Tabs[r.model.activeTab].Dirty = ed.Buffer.Dirty()
		if ed.Buffer.Dirty() && r.model.tabBar.Tabs[r.model.activeTab].Preview {
			r.model.tabBar.Tabs[r.model.activeTab].Preview = false
		}
	}
	if ed.Buffer.FilePath != "" {
		if client := r.model.lspMgr.ClientForFile(ed.Buffer.FilePath); client != nil {
			r.model.notifyLSPChange(client, ed)
		}
	}
	if ed.Highlighter != nil {
		r.retokenizeVersion = ed.Buffer.Version()
	}
	if cmd := r.model.triggerEditorAutocmds(ed.Buffer.FilePath, prevVersion, ed.Buffer.Version(), prevCursor, ed.Buffer.Cursor); cmd != nil {
		r.cmds = append(r.cmds, cmd)
	}
	return nil
}

func (r *pluginRuntime) applyModelCmd(result tea.Model, cmd tea.Cmd) {
	if result != nil {
		*r.model = result.(Model)
	}
	if cmd != nil {
		r.cmds = append(r.cmds, cmd)
	}
}

func (r *pluginRuntime) dispatchImmediate(msg tea.Msg) error {
	if msg == nil {
		return nil
	}
	result, cmd := r.model.Update(msg)
	r.applyModelCmd(result, cmd)
	switch m := msg.(type) {
	case FileErrorMsg:
		return m.Err
	case FileLoadErrorMsg:
		return m.Err
	default:
		return nil
	}
}

func (r *pluginRuntime) resolvePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if r.model.rootDir != "" {
		return filepath.Join(r.model.rootDir, path), nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

func (r *pluginRuntime) BufferText() (string, error) {
	ed, err := r.activeEditor()
	if err != nil {
		return "", err
	}
	return ed.Buffer.Content(), nil
}

func (r *pluginRuntime) SetBufferText(value string) error {
	ed, err := r.activeEditor()
	if err != nil {
		return err
	}
	prevVersion := ed.Buffer.Version()
	prevCursor := ed.Buffer.Cursor
	end := ed.Buffer.Rope().OffsetToPosition(ed.Buffer.Rope().Len())
	ed.Buffer.ReplaceRange(text.Position{}, end, []byte(value))
	ed.Buffer.SetCursor(text.Position{})
	return r.syncActiveEditorAfterEdit(prevVersion, prevCursor)
}

func (r *pluginRuntime) BufferCursor() (text.Position, error) {
	ed, err := r.activeEditor()
	if err != nil {
		return text.Position{}, err
	}
	return ed.Buffer.Cursor, nil
}

func (r *pluginRuntime) SetBufferCursor(pos text.Position) error {
	ed, err := r.activeEditor()
	if err != nil {
		return err
	}
	prevCursor := ed.Buffer.Cursor
	pos = r.clampPosition(ed.Buffer, pos)
	ed.Buffer.SetCursor(pos)
	ed.Viewport.EnsureCursorVisible(pos, ed.Buffer.LineCount())
	if cmd := r.model.triggerEditorAutocmds(ed.Buffer.FilePath, ed.Buffer.Version(), ed.Buffer.Version(), prevCursor, ed.Buffer.Cursor); cmd != nil {
		r.cmds = append(r.cmds, cmd)
	}
	return nil
}

func (r *pluginRuntime) BufferSelection() (*text.Selection, error) {
	ed, err := r.activeEditor()
	if err != nil {
		return nil, err
	}
	if ed.Buffer.Selections == nil || ed.Buffer.Selections.Count() == 0 {
		return nil, nil
	}
	selection := ed.Buffer.Selections.Primary()
	if selection.IsEmpty() {
		return nil, nil
	}
	copy := selection
	return &copy, nil
}

func (r *pluginRuntime) InsertText(value string) error {
	ed, err := r.activeEditor()
	if err != nil {
		return err
	}
	prevVersion := ed.Buffer.Version()
	prevCursor := ed.Buffer.Cursor
	ed.Buffer.InsertAtCursor([]byte(value))
	return r.syncActiveEditorAfterEdit(prevVersion, prevCursor)
}

func (r *pluginRuntime) DeleteSelection() error {
	ed, err := r.activeEditor()
	if err != nil {
		return err
	}
	prevVersion := ed.Buffer.Version()
	prevCursor := ed.Buffer.Cursor
	ed.Buffer.DeleteSelection()
	return r.syncActiveEditorAfterEdit(prevVersion, prevCursor)
}

func (r *pluginRuntime) BufferLine(line int) (string, error) {
	ed, err := r.activeEditor()
	if err != nil {
		return "", err
	}
	if line < 0 {
		line = 0
	}
	return string(ed.Buffer.Line(line)), nil
}

func (r *pluginRuntime) BufferLineCount() (int, error) {
	ed, err := r.activeEditor()
	if err != nil {
		return 0, err
	}
	return ed.Buffer.LineCount(), nil
}

func (r *pluginRuntime) SaveBuffer() error {
	ed, err := r.activeEditor()
	if err != nil {
		return err
	}
	if ed.Buffer.FilePath == "" {
		return fmt.Errorf("active buffer has no file path")
	}
	return r.dispatchImmediate(SaveFileCmd(ed.Buffer.Save, ed.Buffer.FilePath)())
}

func (r *pluginRuntime) BufferFilePath() (string, error) {
	ed, err := r.activeEditor()
	if err != nil {
		return "", err
	}
	return ed.Buffer.FilePath, nil
}

func (r *pluginRuntime) BufferDirty() (bool, error) {
	ed, err := r.activeEditor()
	if err != nil {
		return false, err
	}
	return ed.Buffer.Dirty(), nil
}

func (r *pluginRuntime) Mode() string {
	return "normal"
}

func (r *pluginRuntime) TabCount() int {
	return len(r.model.editors)
}

func (r *pluginRuntime) ActiveTab() int {
	if len(r.model.editors) == 0 {
		return 0
	}
	return r.model.activeTab
}

func (r *pluginRuntime) SetActiveTab(idx int) error {
	if idx < 0 || idx >= len(r.model.editors) {
		return fmt.Errorf("invalid tab index %d", idx+1)
	}
	result, cmd := r.model.Update(SwitchTabMsg{Index: idx})
	r.applyModelCmd(result, cmd)
	return nil
}

func (r *pluginRuntime) OpenFile(path string) error {
	resolvedPath, err := r.resolvePath(path)
	if err != nil {
		return err
	}
	result, cmd := r.model.openFilePinned(resolvedPath)
	r.applyModelCmd(result, nil)
	if cmd == nil {
		return nil
	}
	return r.dispatchImmediate(cmd())
}

func (r *pluginRuntime) CloseTab(idx int) error {
	if len(r.model.editors) == 0 {
		return fmt.Errorf("no open tabs")
	}
	if idx == -1 {
		idx = r.model.activeTab
	}
	if idx < 0 || idx >= len(r.model.editors) {
		return fmt.Errorf("invalid tab index %d", idx+1)
	}
	result, cmd := r.model.Update(CloseTabMsg{Index: idx})
	r.applyModelCmd(result, cmd)
	return nil
}

func (r *pluginRuntime) NextTab() {
	if len(r.model.editors) == 0 {
		return
	}
	idx := (r.model.activeTab + 1) % len(r.model.editors)
	result, cmd := r.model.Update(SwitchTabMsg{Index: idx})
	r.applyModelCmd(result, cmd)
}

func (r *pluginRuntime) PrevTab() {
	if len(r.model.editors) == 0 {
		return
	}
	idx := r.model.activeTab - 1
	if idx < 0 {
		idx = len(r.model.editors) - 1
	}
	result, cmd := r.model.Update(SwitchTabMsg{Index: idx})
	r.applyModelCmd(result, cmd)
}

func (r *pluginRuntime) Width() int {
	if ed, err := r.activeEditor(); err == nil && ed.Viewport.Width > 0 {
		return ed.Viewport.Width
	}
	return r.model.width
}

func (r *pluginRuntime) Height() int {
	if ed, err := r.activeEditor(); err == nil && ed.Viewport.Height > 0 {
		return ed.Viewport.Height
	}
	return r.model.height
}

func (r *pluginRuntime) Status() string {
	return r.model.status
}

func (r *pluginRuntime) SetStatus(status string) {
	r.model.status = status
}

func (r *pluginRuntime) FeedKeys(keys string) error {
	msgs, err := parseSyntheticKeys(keys)
	if err != nil {
		return err
	}
	for _, msg := range msgs {
		r.model.pluginFeedDepth++
		result, cmd := r.model.Update(msg)
		r.model.pluginFeedDepth--
		r.applyModelCmd(result, cmd)
	}
	return nil
}

func (r *pluginRuntime) ShowPanel(name string) error {
	switch normalizePluginPanelName(name) {
	case "tree":
		r.model.showTree = true
		r.model.sidebarTab = SidebarFiles
		r.model.focus = FocusTree
		r.model.relayout()
		return nil
	case "git":
		r.model.showTree = true
		r.model.sidebarTab = SidebarGit
		r.model.focus = FocusGitPanel
		r.model.relayout()
		return nil
	case "problems":
		r.model.showTree = true
		r.model.sidebarTab = SidebarProblems
		r.model.focus = FocusProblems
		r.model.relayout()
		return nil
	case "debugger":
		r.model.showTree = true
		r.model.sidebarTab = SidebarDebugger
		r.model.focus = FocusDebugger
		r.model.relayout()
		return nil
	case "agent":
		r.model.showAgent = true
		r.model.focus = FocusAgent
		r.model.relayout()
		return nil
	default:
		return fmt.Errorf("unsupported panel %q", name)
	}
}

func (r *pluginRuntime) HidePanel(name string) error {
	switch normalizePluginPanelName(name) {
	case "tree":
		r.model.showTree = false
		if r.model.focus == FocusTree || r.model.focus == FocusGitPanel || r.model.focus == FocusProblems || r.model.focus == FocusDebugger {
			r.model.focus = FocusEditor
		}
		r.model.relayout()
		return nil
	case "git":
		if r.model.sidebarTab == SidebarGit {
			r.model.sidebarTab = SidebarFiles
			if r.model.showTree {
				r.model.focus = FocusTree
			}
		}
		r.model.relayout()
		return nil
	case "problems":
		if r.model.sidebarTab == SidebarProblems {
			r.model.sidebarTab = SidebarFiles
			if r.model.showTree {
				r.model.focus = FocusTree
			}
		}
		r.model.relayout()
		return nil
	case "debugger":
		if r.model.sidebarTab == SidebarDebugger {
			r.model.sidebarTab = SidebarFiles
			if r.model.showTree {
				r.model.focus = FocusTree
			}
		}
		r.model.relayout()
		return nil
	case "agent":
		r.model.showAgent = false
		if r.model.focus == FocusAgent {
			r.model.focus = FocusEditor
		}
		r.model.relayout()
		return nil
	default:
		return fmt.Errorf("unsupported panel %q", name)
	}
}

func (r *pluginRuntime) TogglePanel(name string) error {
	switch normalizePluginPanelName(name) {
	case "tree":
		if r.model.showTree {
			return r.HidePanel("tree")
		}
		return r.ShowPanel("tree")
	case "git":
		if r.model.showTree && r.model.sidebarTab == SidebarGit {
			return r.HidePanel("git")
		}
		return r.ShowPanel("git")
	case "problems":
		if r.model.showTree && r.model.sidebarTab == SidebarProblems {
			return r.HidePanel("problems")
		}
		return r.ShowPanel("problems")
	case "debugger":
		if r.model.showTree && r.model.sidebarTab == SidebarDebugger {
			return r.HidePanel("debugger")
		}
		return r.ShowPanel("debugger")
	case "agent":
		if r.model.showAgent {
			return r.HidePanel("agent")
		}
		return r.ShowPanel("agent")
	default:
		return fmt.Errorf("unsupported panel %q", name)
	}
}

func (r *pluginRuntime) Notify(message, level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "notice":
		r.model.status = message
	case "info", "information":
		r.model.status = "Info: " + message
	case "warn", "warning":
		r.model.status = "Warning: " + message
	case "error", "err":
		r.model.status = "Error: " + message
	case "success":
		r.model.status = "Success: " + message
	default:
		r.model.status = message
	}
}

func normalizePluginPanelName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "files", "filetree":
		return "tree"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func parseSyntheticKeys(input string) ([]tea.KeyPressMsg, error) {
	if input == "" {
		return nil, nil
	}
	if !strings.Contains(input, "<") {
		if token, ok, err := parseSyntheticKeyToken(input, false); err != nil {
			return nil, err
		} else if ok {
			return []tea.KeyPressMsg{{Text: token}}, nil
		}
	}

	msgs := make([]tea.KeyPressMsg, 0, len(input))
	for len(input) > 0 {
		if input[0] == '<' {
			end := strings.IndexByte(input, '>')
			if end == -1 {
				return nil, fmt.Errorf("unterminated key token %q", input)
			}
			token, _, err := parseSyntheticKeyToken(input[1:end], true)
			if err != nil {
				return nil, err
			}
			msgs = append(msgs, tea.KeyPressMsg{Text: token})
			input = input[end+1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(input)
		if r == utf8.RuneError && size == 1 {
			return nil, fmt.Errorf("invalid UTF-8 in feed_keys input")
		}
		msgs = append(msgs, tea.KeyPressMsg{Text: string(r)})
		input = input[size:]
	}
	return msgs, nil
}

func parseSyntheticKeyToken(input string, bracketed bool) (string, bool, error) {
	token := strings.ToLower(strings.TrimSpace(input))
	if token == "" {
		return "", false, fmt.Errorf("empty key token")
	}

	if normalized, ok := normalizeNamedSyntheticKey(token); ok {
		return normalized, true, nil
	}

	parts := strings.Split(token, "+")
	if len(parts) > 1 {
		for _, modifier := range parts[:len(parts)-1] {
			switch modifier {
			case "ctrl", "alt", "shift":
			default:
				return "", false, fmt.Errorf("unsupported modifier %q", modifier)
			}
		}
		last := parts[len(parts)-1]
		if _, ok := normalizeNamedSyntheticKey(last); ok {
			return token, true, nil
		}
		if len([]rune(last)) == 1 {
			return token, true, nil
		}
		return "", false, fmt.Errorf("unsupported key %q", last)
	}

	if bracketed {
		return "", false, fmt.Errorf("unsupported key token %q", input)
	}
	return "", false, nil
}

func normalizeNamedSyntheticKey(token string) (string, bool) {
	switch token {
	case "space", "leader":
		return " ", true
	case "enter", "return":
		return "enter", true
	case "tab":
		return "tab", true
	case "backspace", "bs":
		return "backspace", true
	case "delete", "del":
		return "delete", true
	case "esc", "escape":
		return "esc", true
	case "left", "right", "up", "down", "home", "end", "pgup", "pgdown":
		return token, true
	default:
		if len(token) >= 2 && token[0] == 'f' {
			switch token {
			case "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12":
				return token, true
			}
		}
		return "", false
	}
}
