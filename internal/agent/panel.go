package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	sdk "github.com/coder/acp-go-sdk"
	"teak/internal/acp"
	"teak/internal/ui"
)

// ChatRole indicates who sent a message.
type ChatRole int

const (
	RoleUser ChatRole = iota
	RoleAgent
	RoleSystem
)

// StreamBlockKind distinguishes content blocks during streaming.
type StreamBlockKind int

const (
	BlockText StreamBlockKind = iota
	BlockThought
	BlockToolCall
)

const maxToolOutputLines = 100

// StreamBlock is a single chunk of streaming content, preserving chronological order.
type StreamBlock struct {
	Kind     StreamBlockKind
	Content  string
	ToolCall *ToolCallState
}

// ChatMessage represents a completed message in the chat history.
type ChatMessage struct {
	Role      ChatRole
	Content   string
	ToolCalls []*ToolCallState
}

// ToolCallState tracks a tool call's lifecycle.
type ToolCallState struct {
	ID        sdk.ToolCallId
	Title     string
	Kind      sdk.ToolKind
	Status    sdk.ToolCallStatus
	Locations []sdk.ToolCallLocation
	Content   []sdk.ToolCallContent
	Expanded  bool
	StartTime time.Time
	EndTime   time.Time
}

// PermissionPrompt holds state for an inline permission UI.
type PermissionPrompt struct {
	ToolCall   sdk.RequestPermissionToolCall
	Options    []sdk.PermissionOption
	Selected   int
	ResponseCh chan sdk.RequestPermissionResponse
}

// TaggedFile represents a file tagged for inclusion in the next prompt.
type TaggedFile struct {
	Path string
	Name string
}

// AgentState tracks the agent's current state for the header indicator.
type AgentState int

const (
	AgentDisconnected AgentState = iota
	AgentIdle
	AgentThinking
	AgentPermission
)

// CancelRequestedMsg signals the app to cancel the current agent operation.
type CancelRequestedMsg struct{}

// Model is the Bubbletea model for the agent chat panel.
type Model struct {
	width, height int
	theme         ui.Theme

	messages     []ChatMessage
	streamBlocks []StreamBlock
	toolCallMap  map[string]*ToolCallState

	input     textinput.Model
	scrollY   int
	maxScroll int

	loading   bool
	connected bool
	state     AgentState

	permission  *PermissionPrompt
	alwaysAllow map[string]bool

	pendingWrite *acp.AgentWriteFileMsg

	spinner   spinner.Model
	spinFrame int

	lastEscTime time.Time
	autoScroll  bool

	// Model selection
	models       []sdk.ModelInfo
	currentModel sdk.ModelId
	modes        []sdk.SessionMode
	currentMode  sdk.SessionModeId

	// File tagging
	taggedFiles []TaggedFile

	// Cached rendered content line count for scroll calculations
	lastChatLineCount int
}

// New creates a new agent panel model.
func New(theme ui.Theme) Model {
	ti := textinput.New()
	ti.Placeholder = "Ask the agent... (@file, /model)"
	ti.Prompt = ""
	ti.CharLimit = 4096

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipgloss.NewStyle().Foreground(ui.Nord13)

	return Model{
		theme:       theme,
		toolCallMap: make(map[string]*ToolCallState),
		input:       ti,
		alwaysAllow: make(map[string]bool),
		spinner:     sp,
		autoScroll:  true,
		state:       AgentDisconnected,
	}
}

// IsLoading returns whether the panel is in a loading state (for spinner forwarding).
func (m Model) IsLoading() bool {
	return m.loading
}

// SetSize sets the panel dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	innerW := w - 2
	if innerW < 1 {
		innerW = 1
	}
	m.input.SetWidth(innerW)
}

// SetConnected updates the connection state.
func (m *Model) SetConnected(connected bool) {
	m.connected = connected
	if connected {
		m.state = AgentIdle
	} else {
		m.state = AgentDisconnected
	}
}

// State returns the current agent state.
func (m Model) State() AgentState {
	return m.state
}

// HasPermissionPending returns true if there's a pending permission prompt.
func (m Model) HasPermissionPending() bool {
	return m.permission != nil
}

// HasPendingWrite returns true if there's a pending write proposal.
func (m Model) HasPendingWrite() bool {
	return m.pendingWrite != nil
}

// PendingWrite returns the pending write proposal.
func (m Model) PendingWrite() *acp.AgentWriteFileMsg {
	return m.pendingWrite
}

// AcceptWrite accepts the pending write proposal.
func (m *Model) AcceptWrite() {
	if m.pendingWrite != nil {
		m.pendingWrite.ResponseCh <- nil
		m.pendingWrite = nil
	}
}

// RejectWrite rejects the pending write proposal.
func (m *Model) RejectWrite() {
	if m.pendingWrite != nil {
		m.pendingWrite.ResponseCh <- fmt.Errorf("user rejected edit")
		m.pendingWrite = nil
	}
}

// InputValue returns the current input text.
func (m Model) InputValue() string {
	return m.input.Value()
}

// ClearInput clears the input field.
func (m *Model) ClearInput() {
	m.input.SetValue("")
}

// Focus gives focus to the input field.
func (m *Model) Focus() tea.Cmd {
	return m.input.Focus()
}

// Blur removes focus from the input field.
func (m *Model) Blur() {
	m.input.Blur()
}

// TaggedFiles returns the currently tagged files.
func (m Model) TaggedFiles() []TaggedFile {
	return m.taggedFiles
}

// AddTaggedFile adds a file to the tagged files list.
func (m *Model) AddTaggedFile(path string) {
	name := filepath.Base(path)
	for _, f := range m.taggedFiles {
		if f.Path == path {
			return
		}
	}
	m.taggedFiles = append(m.taggedFiles, TaggedFile{Path: path, Name: name})
}

// RemoveTaggedFile removes a file from the tagged files list by index.
func (m *Model) RemoveTaggedFile(idx int) {
	if idx >= 0 && idx < len(m.taggedFiles) {
		m.taggedFiles = append(m.taggedFiles[:idx], m.taggedFiles[idx+1:]...)
	}
}

// ClearTaggedFiles clears all tagged files.
func (m *Model) ClearTaggedFiles() {
	m.taggedFiles = nil
}

// CurrentModel returns the current model ID.
func (m Model) CurrentModel() sdk.ModelId {
	return m.currentModel
}

// AvailableModels returns the available models.
func (m Model) AvailableModels() []sdk.ModelInfo {
	return m.models
}

// AvailableModes returns the available modes.
func (m Model) AvailableModes() []sdk.SessionMode {
	return m.modes
}

// CurrentMode returns the current mode ID.
func (m Model) CurrentMode() sdk.SessionModeId {
	return m.currentMode
}

// AddSystemMessage adds a system/info message to the chat.
func (m *Model) AddSystemMessage(text string) {
	m.messages = append(m.messages, ChatMessage{Role: RoleSystem, Content: text})
}

// ClearHistory clears all chat messages and state.
func (m *Model) ClearHistory() {
	m.messages = nil
	m.streamBlocks = nil
	m.toolCallMap = make(map[string]*ToolCallState)
	m.scrollY = 0
	m.autoScroll = true
}

// appendToStreamBlock appends text to the last block of the given kind,
// or creates a new block if the last block is a different kind.
func (m *Model) appendToStreamBlock(kind StreamBlockKind, text string) {
	n := len(m.streamBlocks)
	if n > 0 && m.streamBlocks[n-1].Kind == kind && m.streamBlocks[n-1].ToolCall == nil {
		m.streamBlocks[n-1].Content += text
	} else {
		m.streamBlocks = append(m.streamBlocks, StreamBlock{Kind: kind, Content: text})
	}
}

// Update handles messages for the agent panel.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case acp.AgentTextMsg:
		m.appendToStreamBlock(BlockText, msg.Text)
		m.state = AgentThinking
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10 // will be clamped in render
		}
		return m, nil

	case acp.AgentThoughtMsg:
		m.appendToStreamBlock(BlockThought, msg.Text)
		m.state = AgentThinking
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentToolCallMsg:
		tc := &ToolCallState{
			ID:        msg.ID,
			Title:     msg.Title,
			Kind:      msg.Kind,
			Status:    msg.Status,
			Locations: msg.Locations,
			Content:   msg.Content,
			StartTime: time.Now(),
		}
		m.toolCallMap[string(msg.ID)] = tc
		m.streamBlocks = append(m.streamBlocks, StreamBlock{Kind: BlockToolCall, ToolCall: tc})
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentToolCallUpdateMsg:
		if tc, ok := m.toolCallMap[string(msg.ID)]; ok {
			if msg.Title != nil {
				tc.Title = *msg.Title
			}
			if msg.Status != nil {
				tc.Status = *msg.Status
				if *msg.Status == sdk.ToolCallStatusCompleted || *msg.Status == sdk.ToolCallStatusFailed {
					tc.EndTime = time.Now()
				}
			}
			if msg.Content != nil {
				tc.Content = msg.Content
			}
			if msg.Locations != nil {
				tc.Locations = msg.Locations
			}
		}
		return m, nil

	case acp.AgentPlanMsg:
		return m, nil

	case acp.AgentWriteFileMsg:
		m.pendingWrite = &msg
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentPermissionRequestMsg:
		kind := ""
		if msg.ToolCall.Kind != nil {
			kind = string(*msg.ToolCall.Kind)
		}
		if m.alwaysAllow[kind] {
			for _, opt := range msg.Options {
				if opt.Kind == sdk.PermissionOptionKindAllowOnce || opt.Kind == sdk.PermissionOptionKindAllowAlways {
					msg.ResponseCh <- sdk.RequestPermissionResponse{
						Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
					}
					return m, nil
				}
			}
		}
		m.permission = &PermissionPrompt{
			ToolCall:   msg.ToolCall,
			Options:    msg.Options,
			ResponseCh: msg.ResponseCh,
		}
		m.state = AgentPermission
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentPromptResponseMsg:
		var toolCalls []*ToolCallState
		var textParts []string
		for _, block := range m.streamBlocks {
			switch block.Kind {
			case BlockText:
				textParts = append(textParts, block.Content)
			case BlockToolCall:
				if block.ToolCall != nil {
					toolCalls = append(toolCalls, block.ToolCall)
				}
			}
		}
		content := strings.Join(textParts, "")
		if content != "" || len(toolCalls) > 0 {
			m.messages = append(m.messages, ChatMessage{
				Role:      RoleAgent,
				Content:   content,
				ToolCalls: toolCalls,
			})
		}
		m.streamBlocks = nil
		m.toolCallMap = make(map[string]*ToolCallState)
		m.loading = false
		m.state = AgentIdle
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentSessionInfoMsg:
		m.models = msg.Models
		m.currentModel = msg.CurrentModel
		m.modes = msg.Modes
		m.currentMode = msg.CurrentMode
		return m, nil

	case acp.AgentModelChangedMsg:
		m.currentModel = msg.ModelId
		return m, nil

	case acp.AgentModeChangedMsg:
		m.currentMode = msg.ModeId
		return m, nil

	case acp.AgentStartedMsg:
		m.connected = true
		m.state = AgentIdle
		return m, nil

	case acp.AgentStoppedMsg:
		m.connected = false
		m.state = AgentDisconnected
		m.loading = false
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseWheelUp {
			m.scrollY -= 3
			if m.scrollY < 0 {
				m.scrollY = 0
			}
			m.autoScroll = false
		} else if mouse.Button == tea.MouseWheelDown {
			m.scrollY += 3
			if m.scrollY > m.maxScroll {
				m.scrollY = m.maxScroll
			}
			if m.scrollY >= m.maxScroll {
				m.autoScroll = true
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	key := msg.String()

	if m.permission != nil {
		return m.handlePermissionKey(key)
	}

	switch key {
	case "enter":
		if m.loading {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.messages = append(m.messages, ChatMessage{Role: RoleUser, Content: text})
		m.input.SetValue("")
		m.loading = true
		m.state = AgentThinking
		m.autoScroll = true
		return m, m.spinner.Tick

	case "esc", "escape":
		now := time.Now()
		if now.Sub(m.lastEscTime) < 300*time.Millisecond {
			m.lastEscTime = time.Time{}
			return m, nil
		}
		m.lastEscTime = now
		return m, nil

	case "ctrl+c":
		if m.loading {
			return m, func() tea.Msg { return CancelRequestedMsg{} }
		}
		return m, nil

	case "ctrl+l":
		m.ClearHistory()
		return m, nil

	case "tab":
		if tc := m.lastVisibleToolCall(); tc != nil {
			tc.Expanded = !tc.Expanded
		}
		return m, nil

	case "pgup", "pageup":
		m.scrollY -= m.chatViewHeight()
		if m.scrollY < 0 {
			m.scrollY = 0
		}
		m.autoScroll = false
		return m, nil

	case "pgdown", "pagedown":
		m.scrollY += m.chatViewHeight()
		if m.scrollY > m.maxScroll {
			m.scrollY = m.maxScroll
		}
		if m.scrollY >= m.maxScroll {
			m.autoScroll = true
		}
		return m, nil

	case "home":
		m.scrollY = 0
		m.autoScroll = false
		return m, nil

	case "end":
		m.scrollY = m.maxScroll
		m.autoScroll = true
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// lastVisibleToolCall returns the most recent tool call (streaming or completed).
func (m Model) lastVisibleToolCall() *ToolCallState {
	for i := len(m.streamBlocks) - 1; i >= 0; i-- {
		if m.streamBlocks[i].Kind == BlockToolCall && m.streamBlocks[i].ToolCall != nil {
			return m.streamBlocks[i].ToolCall
		}
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		tcs := m.messages[i].ToolCalls
		if len(tcs) > 0 {
			return tcs[len(tcs)-1]
		}
	}
	return nil
}

func (m Model) handlePermissionKey(key string) (Model, tea.Cmd) {
	perm := m.permission
	if perm == nil {
		return m, nil
	}

	switch key {
	case "y", "enter":
		for _, opt := range perm.Options {
			if opt.Kind == sdk.PermissionOptionKindAllowOnce {
				perm.ResponseCh <- sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				}
				m.permission = nil
				m.state = AgentThinking
				return m, nil
			}
		}
		if len(perm.Options) > 0 {
			perm.ResponseCh <- sdk.RequestPermissionResponse{
				Outcome: sdk.NewRequestPermissionOutcomeSelected(perm.Options[0].OptionId),
			}
			m.permission = nil
			m.state = AgentThinking
		}
		return m, nil

	case "n":
		for _, opt := range perm.Options {
			if opt.Kind == sdk.PermissionOptionKindRejectOnce {
				perm.ResponseCh <- sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				}
				m.permission = nil
				m.state = AgentThinking
				return m, nil
			}
		}
		perm.ResponseCh <- sdk.RequestPermissionResponse{
			Outcome: sdk.NewRequestPermissionOutcomeCancelled(),
		}
		m.permission = nil
		m.state = AgentThinking
		return m, nil

	case "a":
		kind := ""
		if perm.ToolCall.Kind != nil {
			kind = string(*perm.ToolCall.Kind)
		}
		m.alwaysAllow[kind] = true
		for _, opt := range perm.Options {
			if opt.Kind == sdk.PermissionOptionKindAllowAlways {
				perm.ResponseCh <- sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				}
				m.permission = nil
				m.state = AgentThinking
				return m, nil
			}
		}
		for _, opt := range perm.Options {
			if opt.Kind == sdk.PermissionOptionKindAllowOnce {
				perm.ResponseCh <- sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				}
				m.permission = nil
				m.state = AgentThinking
				return m, nil
			}
		}
		return m, nil
	}

	return m, nil
}

func (m Model) chatViewHeight() int {
	h := m.height - 4
	if len(m.taggedFiles) > 0 {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// View renders the agent panel. Call on a *Model (via &m.agentPanel) to
// persist scroll state across frames.
func (m *Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var sb strings.Builder
	innerW := m.width
	if innerW < 1 {
		innerW = 1
	}

	// Header
	header := m.renderHeader()
	sb.WriteString(header)
	sb.WriteByte('\n')
	linesUsed := 1

	// Input area at bottom (1 line divider + 1 line input + optional tags)
	inputHeight := 2
	if len(m.taggedFiles) > 0 {
		inputHeight++
	}
	chatHeight := m.height - linesUsed - inputHeight
	if chatHeight < 1 {
		chatHeight = 1
	}

	// Build chat content
	chatLines := m.buildChatLines(innerW)
	m.lastChatLineCount = len(chatLines)

	// Compute scroll (pointer receiver — persists)
	m.maxScroll = len(chatLines) - chatHeight
	if m.maxScroll < 0 {
		m.maxScroll = 0
	}
	if m.autoScroll {
		m.scrollY = m.maxScroll
	}
	if m.scrollY > m.maxScroll {
		m.scrollY = m.maxScroll
	}
	if m.scrollY < 0 {
		m.scrollY = 0
	}

	// Render visible chat lines
	for i := 0; i < chatHeight; i++ {
		lineIdx := m.scrollY + i
		if lineIdx < len(chatLines) {
			sb.WriteString(chatLines[lineIdx])
		}
		sb.WriteByte('\n')
	}

	// Tagged files row (above divider)
	if len(m.taggedFiles) > 0 {
		tagLine := m.renderTaggedFiles(innerW)
		sb.WriteString(tagLine)
		sb.WriteByte('\n')
	}

	// Input divider
	divider := lipgloss.NewStyle().Foreground(ui.Nord3).Render(strings.Repeat("─", innerW))
	sb.WriteString(divider)
	sb.WriteByte('\n')

	// Input line
	if m.connected {
		inputView := m.input.View()
		sb.WriteString(lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).Render(" " + inputView))
	} else {
		sb.WriteString(lipgloss.NewStyle().Width(innerW).Foreground(ui.Nord3).Render(" Agent not connected"))
	}

	return sb.String()
}

func (m Model) renderTaggedFiles(width int) string {
	tagStyle := lipgloss.NewStyle().Foreground(ui.Nord0).Background(ui.Nord8)
	dimStyle := lipgloss.NewStyle().Foreground(ui.Nord3)

	var parts []string
	for _, f := range m.taggedFiles {
		parts = append(parts, tagStyle.Render(" "+f.Name+" ×"))
	}
	line := " " + strings.Join(parts, dimStyle.Render(" "))
	if lipgloss.Width(line) > width {
		line = line[:width]
	}
	return line
}

func (m Model) renderHeader() string {
	w := m.width

	label := " Agent"
	var indicator string
	indicatorStyle := lipgloss.NewStyle()

	switch m.state {
	case AgentDisconnected:
		indicator = " ○"
		indicatorStyle = indicatorStyle.Foreground(ui.Nord3)
	case AgentIdle:
		indicator = " ●"
		indicatorStyle = indicatorStyle.Foreground(ui.Nord14)
	case AgentThinking:
		indicator = " " + m.spinner.View()
		indicatorStyle = indicatorStyle.Foreground(ui.Nord13)
	case AgentPermission:
		indicator = " ⏸"
		indicatorStyle = indicatorStyle.Foreground(ui.Nord12)
	}

	titleStyle := lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true)
	title := titleStyle.Render(label)
	ind := indicatorStyle.Render(indicator)

	// Model name in header (compact)
	modelLabel := ""
	if m.currentModel != "" {
		modelStr := string(m.currentModel)
		if len(modelStr) > 25 {
			modelStr = modelStr[:22] + "..."
		}
		modelLabel = " " + lipgloss.NewStyle().Foreground(ui.Nord4).Render(modelStr)
	}

	dashW := w - lipgloss.Width(title) - lipgloss.Width(ind) - lipgloss.Width(modelLabel)
	if dashW < 1 {
		dashW = 1
	}
	dashes := lipgloss.NewStyle().Foreground(ui.Nord3).Render(" " + strings.Repeat("─", dashW-1))

	return title + modelLabel + dashes + ind
}

func (m Model) buildChatLines(width int) []string {
	var lines []string

	if !m.connected {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render(" Agent not connected."))
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render(" Configure in ~/.config/teak/config.toml:"))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render("   [agent]"))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render("   enabled = true"))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render("   command = \"opencode\""))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render("   args = [\"acp\"]"))
		return lines
	}

	hasContent := len(m.messages) > 0 || len(m.streamBlocks) > 0 || m.loading
	if !hasContent {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render(" Try asking:"))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord4).Width(width).Render("   \"explain this function\""))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord4).Width(width).Render("   \"find usages of X\""))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord4).Width(width).Render("   \"fix the bug in auth.go\""))
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord3).Width(width).Render(" Commands:"))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord4).Width(width).Render("   /model  — switch model"))
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord4).Width(width).Render("   @       — attach file"))
		return lines
	}

	contentW := width - 2
	if contentW < 1 {
		contentW = 1
	}

	systemStyle := lipgloss.NewStyle().Foreground(ui.Nord3).Italic(true)

	for _, msg := range m.messages {
		lines = append(lines, "")
		switch msg.Role {
		case RoleUser:
			lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord8).Bold(true).Render(" You:"))
			wrapped := wrapText(msg.Content, contentW)
			for _, l := range wrapped {
				lines = append(lines, "  "+l)
			}
		case RoleAgent:
			lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord14).Bold(true).Render(" Agent:"))
			for _, tc := range msg.ToolCalls {
				lines = append(lines, m.renderToolCall(tc, contentW)...)
			}
			if msg.Content != "" {
				wrapped := wrapText(msg.Content, contentW)
				for _, l := range wrapped {
					lines = append(lines, "  "+l)
				}
			}
		case RoleSystem:
			wrapped := wrapText(msg.Content, contentW)
			for _, l := range wrapped {
				lines = append(lines, "  "+systemStyle.Render(l))
			}
		}
	}

	// Streaming blocks in chronological order
	if len(m.streamBlocks) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord14).Bold(true).Render(" Agent:"))

		thoughtStyle := lipgloss.NewStyle().Foreground(ui.Nord3).Italic(true)
		for _, block := range m.streamBlocks {
			switch block.Kind {
			case BlockText:
				wrapped := wrapText(block.Content, contentW)
				for _, l := range wrapped {
					lines = append(lines, "  "+l)
				}
			case BlockThought:
				wrapped := wrapText(block.Content, contentW)
				for _, l := range wrapped {
					lines = append(lines, "  "+thoughtStyle.Render(l))
				}
			case BlockToolCall:
				if block.ToolCall != nil {
					lines = append(lines, m.renderToolCall(block.ToolCall, contentW)...)
				}
			}
		}
	}

	if m.loading && len(m.streamBlocks) == 0 {
		lines = append(lines, "")
		lines = append(lines, "  "+m.spinner.View()+" Thinking...")
	}

	if m.permission != nil {
		lines = append(lines, m.renderPermission(contentW)...)
	}

	if m.pendingWrite != nil {
		lines = append(lines, m.renderWriteProposal(contentW)...)
	}

	return lines
}

func (m Model) renderToolCall(tc *ToolCallState, width int) []string {
	var lines []string

	var statusIcon string
	switch tc.Status {
	case sdk.ToolCallStatusPending, sdk.ToolCallStatusInProgress:
		statusIcon = lipgloss.NewStyle().Foreground(ui.Nord13).Render("◐")
	case sdk.ToolCallStatusCompleted:
		statusIcon = lipgloss.NewStyle().Foreground(ui.Nord14).Render("✓")
	case sdk.ToolCallStatusFailed:
		statusIcon = lipgloss.NewStyle().Foreground(ui.Nord11).Render("✗")
	default:
		statusIcon = lipgloss.NewStyle().Foreground(ui.Nord3).Render("⊘")
	}

	arrow := "▸"
	if tc.Expanded {
		arrow = "▾"
	}

	kindStr := ""
	if tc.Kind != "" {
		kindStr = string(tc.Kind)
		if len(kindStr) > 5 {
			kindStr = kindStr[:5]
		}
		if len(kindStr) > 0 {
			kindStr = strings.ToUpper(kindStr[:1]) + kindStr[1:]
		}
	}

	loc := ""
	if len(tc.Locations) > 0 {
		loc = tc.Locations[0].Path
		if idx := strings.LastIndex(loc, "/"); idx >= 0 {
			loc = loc[idx+1:]
		}
	}

	dur := ""
	if !tc.EndTime.IsZero() {
		d := tc.EndTime.Sub(tc.StartTime)
		dur = fmt.Sprintf("%.1fs", d.Seconds())
	}

	title := tc.Title
	if title == "" {
		title = string(tc.Kind)
	}

	line := fmt.Sprintf("  %s %s  %-6s %-20s %s  %s", arrow, statusIcon, kindStr, loc, dur, title)
	if len(line) > width+2 {
		line = line[:width+2]
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord4).Render(line))

	if tc.Expanded {
		lineCount := 0
		for _, c := range tc.Content {
			if lineCount >= maxToolOutputLines {
				lines = append(lines, "    "+lipgloss.NewStyle().Foreground(ui.Nord3).Render("│ ... (truncated)"))
				break
			}
			text := extractToolCallText(c)
			if text != "" {
				wrapped := wrapText(text, width-4)
				for _, l := range wrapped {
					if lineCount >= maxToolOutputLines {
						lines = append(lines, "    "+lipgloss.NewStyle().Foreground(ui.Nord3).Render("│ ... (truncated)"))
						break
					}
					lines = append(lines, "    "+lipgloss.NewStyle().Foreground(ui.Nord3).Render("│ "+l))
					lineCount++
				}
			}
		}
	}

	return lines
}

func (m Model) renderPermission(width int) []string {
	perm := m.permission
	if perm == nil {
		return nil
	}

	var lines []string
	lines = append(lines, "")

	boxStyle := lipgloss.NewStyle().Foreground(ui.Nord12)
	lines = append(lines, boxStyle.Render("  Agent wants to:"))

	title := ""
	if perm.ToolCall.Title != nil {
		title = *perm.ToolCall.Title
	}
	if title == "" {
		title = "perform an action"
	}
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(ui.Nord6).Bold(true).Render(title))

	optLine := "  "
	optLine += lipgloss.NewStyle().Foreground(ui.Nord14).Render("[y] Allow")
	optLine += "  "
	optLine += lipgloss.NewStyle().Foreground(ui.Nord11).Render("[n] Deny")
	optLine += "  "
	optLine += lipgloss.NewStyle().Foreground(ui.Nord13).Render("[a] Always")
	lines = append(lines, optLine)

	return lines
}

func (m Model) renderWriteProposal(width int) []string {
	pw := m.pendingWrite
	if pw == nil {
		return nil
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(ui.Nord12).Render("Edit proposal:"))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(ui.Nord6).Render(pw.Path))

	lineCount := strings.Count(pw.Content, "\n") + 1
	lines = append(lines, fmt.Sprintf("  %d lines", lineCount))
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(ui.Nord14).Render("[Enter] Accept")+"  "+lipgloss.NewStyle().Foreground(ui.Nord11).Render("[Esc] Reject"))

	return lines
}

func extractToolCallText(c sdk.ToolCallContent) string {
	if c.Content != nil {
		if c.Content.Content.Text != nil {
			return c.Content.Content.Text.Text
		}
	}
	if c.Diff != nil {
		return fmt.Sprintf("diff: %s", c.Diff.Path)
	}
	if c.Terminal != nil {
		return fmt.Sprintf("terminal: %s", c.Terminal.TerminalId)
	}
	return ""
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}

	var result []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			result = append(result, "")
			continue
		}
		for len(paragraph) > width {
			breakAt := width
			for breakAt > 0 && paragraph[breakAt] != ' ' {
				breakAt--
			}
			if breakAt == 0 {
				breakAt = width
			}
			result = append(result, paragraph[:breakAt])
			paragraph = strings.TrimLeft(paragraph[breakAt:], " ")
		}
		result = append(result, paragraph)
	}
	return result
}
