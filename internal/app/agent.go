package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sdk "github.com/coder/acp-go-sdk"
	"teak/internal/acp"
	"teak/internal/agent"
	"teak/internal/overlay"
	"teak/internal/ui"
)

// acpMsg wraps an ACP message from the msgChan so it routes through the
// main Update switch without colliding with other message types.
type acpMsg struct {
	msg tea.Msg
}

// toggleAgentMsg is sent by the command palette.
type toggleAgentMsg struct{}

// focusAgentMsg is sent by the command palette.
type focusAgentMsg struct{}

// agentCancelMsg is sent to cancel the current agent operation.
type agentCancelMsg struct{}

// agentModelPickerSelectMsg is sent when a model is selected from the overlay picker.
type agentModelPickerSelectMsg struct {
	ModelId string
}

// agentFilePickerSelectMsg is sent when a file is selected from the overlay picker.
type agentFilePickerSelectMsg struct {
	Path string
}

// listenACP returns a tea.Cmd that waits for the next ACP message.
func (m Model) listenACP() tea.Cmd {
	if m.acpMgr == nil {
		return nil
	}
	ch := m.acpMgr.MsgChan()
	return func() tea.Msg {
		raw, ok := <-ch
		if !ok {
			return nil
		}
		return acpMsg{msg: raw}
	}
}

// handleACPMsg dispatches an ACP message to the appropriate handler.
func (m Model) handleACPMsg(msg acpMsg) (tea.Model, tea.Cmd) {
	if msg.msg == nil {
		return m, m.listenACP()
	}

	switch inner := msg.msg.(type) {
	case acp.AgentTextMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentThoughtMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentToolCallMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentToolCallUpdateMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentPlanMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentWriteFileMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentPermissionRequestMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
		// Auto-show panel when permission is needed
		if !m.showAgent {
			m.showAgent = true
			m.relayout()
		}
	case acp.AgentPromptResponseMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentSessionInfoMsg:
		m.agentPanel, _ = m.agentPanel.Update(inner)
	case acp.AgentModelChangedMsg:
		m.agentPanel, _ = m.agentPanel.Update(acp.AgentSessionInfoMsg{
			Models:       m.agentPanel.AvailableModels(),
			CurrentModel: inner.ModelId,
		})
	case acp.AgentModeChangedMsg:
		m.agentPanel, _ = m.agentPanel.Update(acp.AgentSessionInfoMsg{
			Modes:       m.agentPanel.AvailableModes(),
			CurrentMode: inner.ModeId,
		})
		m.agentPanel.AddSystemMessage("Mode changed to " + string(inner.ModeId))
	case acp.AgentErrorMsg:
		m.agentPanel.AddSystemMessage("Error: " + inner.Err.Error())
	case acp.AgentStartedMsg:
		m.agentPanel.SetConnected(true)
	case acp.AgentStoppedMsg:
		m.agentPanel.SetConnected(false)
	case acp.FileReadRequestMsg:
		return m.handleFileReadRequest(inner)
	case agent.CancelRequestedMsg:
		if m.acpMgr != nil {
			m.acpMgr.Cancel()
			m.agentPanel.AddSystemMessage("Cancelled.")
		}
	}

	return m, m.listenACP()
}

// handleFileReadRequest reads file content from an open buffer or disk,
// responding on the request channel. This runs in the Bubbletea loop
// for goroutine safety (no racing with editor buffer mutations).
func (m Model) handleFileReadRequest(req acp.FileReadRequestMsg) (tea.Model, tea.Cmd) {
	// Check open buffers synchronously (safe: runs in Bubbletea loop)
	for i := range m.editors {
		buf := m.editors[i].Buffer
		if buf.FilePath == req.Path {
			content := buf.Content()
			if req.Line != nil || req.Limit != nil {
				content = filterLines(content, req.Line, req.Limit)
			}
			req.ResultCh <- acp.FileReadResult{Content: content}
			return m, m.listenACP()
		}
	}
	// Disk fallback in goroutine (no shared state accessed)
	go func() {
		content, err := acp.ReadFileFromDisk(req.Path, req.Line, req.Limit)
		req.ResultCh <- acp.FileReadResult{Content: content, Err: err}
	}()

	return m, m.listenACP()
}

// filterLines applies line/limit filtering to content.
// Line is 1-based (ACP convention).
func filterLines(content string, line *int, limit *int) string {
	lines := strings.Split(content, "\n")

	startLine := 0
	if line != nil {
		startLine = *line - 1
		if startLine < 0 {
			startLine = 0
		}
		if startLine >= len(lines) {
			return ""
		}
	}

	endLine := len(lines)
	if limit != nil {
		endLine = startLine + *limit
		if endLine > len(lines) {
			endLine = len(lines)
		}
	}

	return strings.Join(lines[startLine:endLine], "\n")
}

// toggleAgentPanel toggles the agent panel visibility.
func (m *Model) toggleAgentPanel() tea.Cmd {
	if m.showAgent {
		m.showAgent = false
		if m.focus == FocusAgent {
			m.focus = FocusEditor
		}
	} else {
		m.showAgent = true
		m.focus = FocusAgent
		m.relayout()
		return m.agentPanel.Focus()
	}
	m.relayout()
	return nil
}

// agentPanelWidth calculates the responsive agent panel width.
func (m Model) agentPanelWidth() int {
	if !m.showAgent {
		return 0
	}

	available := m.width
	if m.showTree {
		available -= m.treeWidth() + 1
	}
	available -= 1 // right border

	aw := available * 35 / 100
	if aw < 25 {
		aw = 25
	}
	if aw > 60 {
		aw = 60
	}

	// Ensure editor keeps at least 40 columns
	editorWidth := available - aw
	if editorWidth < 40 {
		aw = available - 40
		if aw < 20 {
			return 0 // auto-hide: terminal too narrow
		}
	}
	return aw
}

// sendAgentPrompt sends a user prompt to the agent with tagged files.
func (m Model) sendAgentPrompt(text string) tea.Cmd {
	if m.acpMgr == nil {
		return nil
	}
	// Convert panel tagged files to acp.TaggedFile
	panelFiles := m.agentPanel.TaggedFiles()
	var files []acp.TaggedFile
	for _, f := range panelFiles {
		files = append(files, acp.TaggedFile{Path: f.Path, Name: f.Name})
	}
	// Clear tagged files after sending
	m.agentPanel.ClearTaggedFiles()
	return m.acpMgr.Prompt(text, files)
}

// startAgent starts the ACP agent subprocess.
func (m Model) startAgent() tea.Cmd {
	if m.acpMgr == nil {
		return nil
	}
	mgr := m.acpMgr
	return func() tea.Msg {
		if err := mgr.Start(); err != nil {
			return acpMsg{msg: acp.AgentStoppedMsg{Err: err}}
		}
		return nil
	}
}

// agentIndicator returns the agent status string for the status bar.
func (m Model) agentIndicator() string {
	if m.acpMgr == nil {
		return ""
	}
	state := m.agentPanel.State()
	switch state {
	case agent.AgentDisconnected:
		return ""
	case agent.AgentIdle:
		return "  Agent " + lipgloss.NewStyle().Foreground(ui.Nord14).Render("●")
	case agent.AgentThinking:
		return "  Agent " + lipgloss.NewStyle().Foreground(ui.Nord13).Render("◐")
	case agent.AgentPermission:
		return "  Agent " + lipgloss.NewStyle().Foreground(ui.Nord12).Render("⏸")
	}
	return ""
}

// agentBorderColumn renders the right-side border between editor and agent panel.
func (m Model) agentBorderColumn(height int) string {
	borderStyle := m.theme.AgentBorder
	if m.focus == FocusAgent {
		borderStyle = m.theme.AgentBorderFocused
	}
	borderLines := make([]string, height)
	for i := range height {
		borderLines[i] = borderStyle.Render("│")
	}
	return strings.Join(borderLines, "\n")
}

// handleAgentEnter processes an Enter key press in the agent panel input.
// Returns (model, cmd, handled). If handled is false, the caller should
// send the input as a normal prompt.
func (m Model) handleAgentEnter() (Model, tea.Cmd, bool) {
	text := strings.TrimSpace(m.agentPanel.InputValue())
	if text == "" {
		return m, nil, true
	}

	// Handle slash commands
	switch {
	case text == "/model":
		m.agentPanel.ClearInput()
		cmd := m.openAgentModelPicker()
		return m, cmd, true
	case text == "/mode":
		m.agentPanel.ClearInput()
		return m.cycleAgentMode()
	case text == "/clear":
		m.agentPanel.ClearInput()
		m.agentPanel.ClearHistory()
		return m, nil, true
	case text == "/cancel":
		m.agentPanel.ClearInput()
		if m.acpMgr != nil {
			m.acpMgr.Cancel()
			m.agentPanel.AddSystemMessage("Cancelled.")
		}
		return m, nil, true
	case text == "/help":
		m.agentPanel.ClearInput()
		m.agentPanel.AddSystemMessage("Commands: /model, /mode, /clear, /help\nUse @ to attach files to your prompt.")
		return m, nil, true
	case text == "@":
		m.agentPanel.ClearInput()
		cmd := m.openAgentFilePicker()
		return m, cmd, true
	case strings.HasPrefix(text, "@"):
		// @ with a partial path — open picker with initial filter
		m.agentPanel.ClearInput()
		cmd := m.openAgentFilePicker()
		return m, cmd, true
	}

	// Not a slash command — send as prompt
	return m, nil, false
}

// openAgentModelPicker opens an overlay picker with available models.
func (m Model) openAgentModelPicker() tea.Cmd {
	models := m.agentPanel.AvailableModels()
	if len(models) == 0 {
		m.agentPanel.AddSystemMessage("No models available.")
		return nil
	}

	current := m.agentPanel.CurrentModel()
	items := make([]overlay.PickerItem, len(models))
	for i, mi := range models {
		desc := string(mi.ModelId)
		if mi.ModelId == current {
			desc += " (current)"
		}
		items[i] = overlay.PickerItem{
			Label:       string(mi.ModelId),
			Description: desc,
			Value:       agentModelPickerSelectMsg{ModelId: string(mi.ModelId)},
		}
	}

	picker := overlay.NewPicker("Select Model", items, m.theme, "agent-model-picker")
	m.overlayStack.Push(picker)
	return nil
}

// openAgentFilePicker opens an overlay picker to tag a file.
func (m Model) openAgentFilePicker() tea.Cmd {
	if m.cachedFilesReady {
		items := filesToAgentPickerItems(m.cachedFiles)
		picker := overlay.NewPicker("Attach File", items, m.theme, "agent-file-picker")
		m.overlayStack.Push(picker)
		return nil
	}
	// Trigger file scan; when FileListMsg arrives it will be handled
	// For now, show empty picker — it will populate when files arrive
	picker := overlay.NewPicker("Attach File (loading...)", nil, m.theme, "agent-file-picker")
	m.overlayStack.Push(picker)
	return quickOpenCmd(m.rootDir)
}

// filesToAgentPickerItems converts file paths to picker items with agent-specific Value.
func filesToAgentPickerItems(files []string) []overlay.PickerItem {
	items := make([]overlay.PickerItem, len(files))
	for i, f := range files {
		items[i] = overlay.PickerItem{
			Label:       filepath.Base(f),
			Description: filepath.Dir(f),
			Value:       agentFilePickerSelectMsg{Path: f},
		}
	}
	return items
}

// cycleAgentMode cycles to the next available mode.
func (m Model) cycleAgentMode() (Model, tea.Cmd, bool) {
	modes := m.agentPanel.AvailableModes()
	if len(modes) == 0 {
		m.agentPanel.AddSystemMessage("No modes available.")
		return m, nil, true
	}
	current := m.agentPanel.CurrentMode()
	var nextMode string
	foundCurrent := false
	for _, mi := range modes {
		if foundCurrent {
			nextMode = string(mi.Id)
			break
		}
		if mi.Id == current {
			foundCurrent = true
		}
	}
	if nextMode == "" {
		nextMode = string(modes[0].Id)
	}
	if m.acpMgr != nil {
		m.agentPanel.AddSystemMessage("Switching mode to " + nextMode)
		return m, m.acpMgr.SetMode(sdk.SessionModeId(nextMode)), true
	}
	return m, nil, true
}

// openFileContent reads file content. If the file is open in a buffer,
// returns that content; otherwise reads from disk.
func (m Model) openFileContent(path string) string {
	for i := range m.editors {
		buf := m.editors[i].Buffer
		if buf.FilePath == path {
			return buf.Content()
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// applyAgentWrite applies a file write from the agent to the editor.
func (m *Model) applyAgentWrite(path, content string) tea.Cmd {
	// Validate path is within root directory (security)
	validatedPath, err := validatePathStrict(m.rootDir, path)
	if err != nil {
		return func() tea.Msg {
			return agentWriteErrorMsg{Path: path, Err: err}
		}
	}

	// Check if file is already open in a tab
	for i := range m.editors {
		if m.editors[i].Buffer.FilePath == validatedPath {
			// Replace buffer content via LoadContent
			m.editors[i].Buffer.LoadContent([]byte(content))
			return nil
		}
	}
	// Write directly to disk if not open
	return func() tea.Msg {
		if err := os.WriteFile(validatedPath, []byte(content), 0644); err != nil {
			return agentWriteErrorMsg{Path: validatedPath, Err: err}
		}
		return nil
	}
}

// agentWriteErrorMsg represents a write error for the agent
type agentWriteErrorMsg struct {
	Path string
	Err  error
}

// validatePathStrict performs stricter validation including symlink resolution
func validatePathStrict(rootDir, path string) (string, error) {
	cleanPath := filepath.Clean(path)

	absPath, err := filepath.Abs(filepath.Join(rootDir, cleanPath))
	if err != nil {
		return "", err
	}

	realRoot, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		realRoot = rootDir
	}

	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		realPath = absPath
	}

	if !strings.HasPrefix(realPath, realRoot) {
		return "", fmt.Errorf("path %q is outside root directory %q", path, rootDir)
	}

	return realPath, nil
}
