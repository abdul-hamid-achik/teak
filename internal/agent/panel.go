package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	sdk "github.com/coder/acp-go-sdk"
	zone "github.com/lrstanley/bubblezone/v2"
	"teak/internal/acp"
	"teak/internal/ui"
)

// Model is the Bubbletea model for the agent chat panel.
type Model struct {
	width, height int
	theme         ui.Theme

	messages     []ChatMessage
	streamBlocks []StreamBlock
	toolCallMap  map[string]*ToolCallState
	messageBytes int
	streamBytes  int
	chatCache    *chatRenderCache

	input     textinput.Model
	scrollY   int
	maxScroll int

	loading   bool
	connected bool
	state     AgentState

	permission  *PermissionPrompt
	alwaysAllow map[string]bool

	pendingWrite  *acp.AgentWriteFileMsg
	pendingWrites []acp.AgentWriteFileMsg

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

type chatRenderCache struct {
	width             int
	dirty             bool
	lines             []string
	streamBlockStarts []int
	streamTailStart   int
	streamDirtyFrom   int

	// Counters are deliberately kept in the cache so tests can ensure a
	// streamed update renders only its dirty tail. They do not affect output.
	fullBuilds           int
	incrementalBuilds    int
	renderedStreamBlocks int
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
		chatCache:   &chatRenderCache{dirty: true},
	}
}

// IsLoading returns whether the panel is in a loading state (for spinner forwarding).
func (m Model) IsLoading() bool {
	return m.loading
}

// SetSize sets the panel dimensions.
func (m *Model) SetSize(w, h int) {
	if m.width != w {
		m.invalidateChatCache()
	}
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
	m.invalidateChatCache()
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

// PruneCancelledWrites removes canceled proposals even if their explicit
// cancellation message was dropped under ACP channel backpressure.
func (m *Model) PruneCancelledWrites() {
	if m.pendingWrite != nil && writeProposalCancelled(*m.pendingWrite) {
		cancelled := *m.pendingWrite
		m.pendingWrite = nil
		respondToCancelledWrite(cancelled)
		m.promotePendingWrite()
		m.invalidateChatCache()
	}

	kept := m.pendingWrites[:0]
	for _, proposal := range m.pendingWrites {
		if writeProposalCancelled(proposal) {
			respondToCancelledWrite(proposal)
			continue
		}
		kept = append(kept, proposal)
	}
	clear(m.pendingWrites[len(kept):])
	m.pendingWrites = kept
}

// AcceptWrite accepts the pending write proposal and emits its decision.
// The app is responsible for responding to the agent and writing to disk.
func (m *Model) AcceptWrite() tea.Cmd {
	return m.resolvePendingWrite(true)
}

// RejectWrite rejects the pending write proposal and emits its decision.
// The app is responsible for responding to the agent.
func (m *Model) RejectWrite() tea.Cmd {
	return m.resolvePendingWrite(false)
}

func (m *Model) resolvePendingWrite(accepted bool) tea.Cmd {
	if m.pendingWrite == nil {
		return nil
	}

	proposal := *m.pendingWrite
	m.pendingWrite = nil
	m.promotePendingWrite()
	m.invalidateChatCache()
	return func() tea.Msg {
		return WriteDecisionMsg{Proposal: proposal, Accepted: accepted}
	}
}

func (m *Model) promotePendingWrite() {
	for len(m.pendingWrites) > 0 {
		next := m.pendingWrites[0]
		m.pendingWrites[0] = acp.AgentWriteFileMsg{}
		m.pendingWrites = m.pendingWrites[1:]
		if writeProposalCancelled(next) {
			respondToCancelledWrite(next)
			continue
		}
		m.pendingWrite = &next
		m.invalidateChatCache()
		return
	}
	m.pendingWrite = nil
	m.invalidateChatCache()
}

func (m *Model) cancelPendingWrite(responseCh chan error) {
	if m.pendingWrite != nil && m.pendingWrite.ResponseCh == responseCh {
		cancelled := *m.pendingWrite
		m.pendingWrite = nil
		respondToCancelledWrite(cancelled)
		m.promotePendingWrite()
		m.invalidateChatCache()
		return
	}

	kept := m.pendingWrites[:0]
	for _, proposal := range m.pendingWrites {
		if proposal.ResponseCh == responseCh {
			respondToCancelledWrite(proposal)
			continue
		}
		kept = append(kept, proposal)
	}
	clear(m.pendingWrites[len(kept):])
	m.pendingWrites = kept
	m.invalidateChatCache()
}

func writeProposalCancelled(proposal acp.AgentWriteFileMsg) bool {
	return proposal.Context != nil && proposal.Context.Err() != nil
}

func respondToCancelledWrite(proposal acp.AgentWriteFileMsg) {
	err := context.Canceled
	if proposal.Context != nil && proposal.Context.Err() != nil {
		err = proposal.Context.Err()
	}
	select {
	case proposal.ResponseCh <- err:
	default:
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
	if len(m.taggedFiles) >= maxTaggedFiles {
		return
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
	m.appendChatMessage(ChatMessage{Role: RoleSystem, Content: text})
}

func (m *Model) appendChatMessage(msg ChatMessage) {
	msg.Content = truncateUTF8Bytes(msg.Content, maxChatMessageBytes)
	if len(msg.ToolCalls) > maxToolCalls {
		msg.ToolCalls = msg.ToolCalls[:maxToolCalls]
	}
	for i, toolCall := range msg.ToolCalls {
		msg.ToolCalls[i] = boundedToolCall(toolCall)
	}

	size := chatMessageSize(msg)
	m.messages = append(m.messages, msg)
	m.messageBytes += size
	for len(m.messages) > maxChatMessages || m.messageBytes > maxChatHistoryBytes {
		if len(m.messages) == 0 {
			m.messageBytes = 0
			break
		}
		m.messageBytes -= chatMessageSize(m.messages[0])
		m.messages[0] = ChatMessage{}
		m.messages = m.messages[1:]
	}
	m.invalidateChatCache()
}

func chatMessageSize(msg ChatMessage) int {
	size := len(msg.Content)
	for _, toolCall := range msg.ToolCalls {
		if toolCall == nil {
			continue
		}
		size += len(toolCall.Title)
		for _, location := range toolCall.Locations {
			size += len(location.Path)
		}
		for _, content := range toolCall.Content {
			size += len(extractToolCallText(content))
		}
	}
	return size
}

func boundedToolCall(toolCall *ToolCallState) *ToolCallState {
	if toolCall == nil {
		return nil
	}
	clone := *toolCall
	clone.Title = truncateUTF8Bytes(clone.Title, 4096)
	clone.Locations = boundedToolLocations(clone.Locations)
	clone.Content = boundedToolContent(clone.Content)
	return &clone
}

func boundedToolLocations(locations []sdk.ToolCallLocation) []sdk.ToolCallLocation {
	if len(locations) > maxToolCalls {
		locations = locations[:maxToolCalls]
	}
	result := make([]sdk.ToolCallLocation, len(locations))
	for i, location := range locations {
		result[i] = sdk.ToolCallLocation{
			Path: truncateUTF8Bytes(location.Path, 4096),
			Line: location.Line,
		}
	}
	return result
}

func boundedToolContent(contents []sdk.ToolCallContent) []sdk.ToolCallContent {
	result := make([]sdk.ToolCallContent, 0, min(len(contents), maxToolCalls))
	remaining := maxToolContentBytes
	for _, content := range contents {
		if len(result) >= maxToolCalls || remaining <= 0 {
			break
		}
		switch {
		case content.Content != nil && content.Content.Content.Text != nil:
			text := truncateUTF8Bytes(content.Content.Content.Text.Text, remaining)
			if text == "" {
				continue
			}
			result = append(result, sdk.ToolContent(sdk.TextBlock(text)))
			remaining -= len(text)
		case content.Diff != nil:
			path := truncateUTF8Bytes(content.Diff.Path, min(remaining, 4096))
			if path == "" {
				continue
			}
			// The panel only renders the path summary. Retaining whole old/new
			// file bodies here would duplicate potentially huge documents.
			result = append(result, sdk.ToolDiffContent(path, ""))
			remaining -= len(path)
		case content.Terminal != nil:
			id := truncateUTF8Bytes(content.Terminal.TerminalId, min(remaining, 4096))
			if id == "" {
				continue
			}
			result = append(result, sdk.ToolTerminalRef(id))
			remaining -= len(id)
		}
	}
	return result
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

// ClearHistory clears all chat messages and state.
func (m *Model) ClearHistory() {
	m.messages = nil
	m.streamBlocks = nil
	m.toolCallMap = make(map[string]*ToolCallState)
	m.messageBytes = 0
	m.streamBytes = 0
	m.scrollY = 0
	m.autoScroll = true
	m.invalidateChatCache()
}

// appendToStreamBlock appends text to the last block of the given kind,
// or creates a new block if the last block is a different kind.
func (m *Model) appendToStreamBlock(kind StreamBlockKind, text string) {
	if m.streamBytes >= maxStreamContentBytes {
		return
	}
	text = truncateUTF8Bytes(text, maxStreamContentBytes-m.streamBytes)
	if text == "" {
		return
	}
	firstChanged := len(m.streamBlocks)
	for len(text) > 0 {
		n := len(m.streamBlocks)
		if n > 0 && m.streamBlocks[n-1].Kind == kind && m.streamBlocks[n-1].ToolCall == nil && len(m.streamBlocks[n-1].Content) < streamRenderBlockBytes {
			available := streamRenderBlockBytes - len(m.streamBlocks[n-1].Content)
			part := truncateUTF8Bytes(text, available)
			if part != "" {
				firstChanged = min(firstChanged, n-1)
				m.streamBlocks[n-1].Content += part
				m.streamBytes += len(part)
				text = text[len(part):]
				continue
			}
			// The remaining space cannot hold a full UTF-8 rune. Preserve
			// validity by starting the next render block instead of dropping
			// the remainder of this stream message.
		}
		if n >= maxStreamBlocks {
			break
		}
		part := truncateUTF8Bytes(text, streamRenderBlockBytes)
		if part == "" {
			break
		}
		m.streamBlocks = append(m.streamBlocks, StreamBlock{Kind: kind, Content: part})
		m.streamBytes += len(part)
		text = text[len(part):]
	}
	if firstChanged == 0 && len(m.streamBlocks) > 0 {
		// The loading placeholder disappears when the first stream block arrives.
		m.invalidateChatCache()
		return
	}
	m.invalidateStreamTail(firstChanged)
}

func (m *Model) invalidateChatCache() {
	if m.chatCache == nil {
		m.chatCache = &chatRenderCache{}
	}
	m.chatCache.dirty = true
	m.chatCache.streamDirtyFrom = -1
}

// invalidateStreamTail records the first changed streaming block. Completed
// transcript lines stay valid, so the next View only re-renders this tail.
func (m *Model) invalidateStreamTail(firstChanged int) {
	if m.chatCache == nil {
		m.chatCache = &chatRenderCache{dirty: true, streamDirtyFrom: -1}
		return
	}
	if m.chatCache.dirty || m.chatCache.streamTailStart < 0 || firstChanged < 0 {
		m.chatCache.dirty = true
		m.chatCache.streamDirtyFrom = -1
		return
	}
	if m.chatCache.streamDirtyFrom < 0 || firstChanged < m.chatCache.streamDirtyFrom {
		m.chatCache.streamDirtyFrom = firstChanged
	}
}

func (m *Model) cachedChatLines(width int) []string {
	if m.chatCache == nil {
		m.chatCache = &chatRenderCache{dirty: true, streamDirtyFrom: -1}
	}
	if m.chatCache.dirty || m.chatCache.width != width {
		m.rebuildChatCache(width)
	} else if m.chatCache.streamDirtyFrom >= 0 {
		m.rebuildChatStreamTail(width)
	}
	return m.chatCache.lines
}

func (m *Model) rebuildChatCache(width int) {
	lines, starts, tailStart := m.buildChatLinesWithStreamMetadata(width)
	// Reserve a modest tail so common streaming updates reuse the backing
	// array and keep the completed transcript allocation-free.
	lines = slices.Grow(lines, streamRenderBlockBytes/8)
	m.chatCache.lines = lines
	m.chatCache.width = width
	m.chatCache.dirty = false
	m.chatCache.streamBlockStarts = starts
	m.chatCache.streamTailStart = tailStart
	m.chatCache.streamDirtyFrom = -1
	m.chatCache.fullBuilds++
	m.chatCache.renderedStreamBlocks = len(starts)
}

func (m *Model) rebuildChatStreamTail(width int) {
	cache := m.chatCache
	from := cache.streamDirtyFrom
	if from < 0 || from > len(m.streamBlocks) || len(cache.streamBlockStarts) < from {
		cache.dirty = true
		m.rebuildChatCache(width)
		return
	}

	start := cache.streamTailStart
	starts := append([]int(nil), cache.streamBlockStarts[:from]...)
	if from < len(cache.streamBlockStarts) {
		start = cache.streamBlockStarts[from]
	}
	lines := cache.lines[:start]
	contentW := width - 2
	if contentW < 1 {
		contentW = 1
	}
	for i := from; i < len(m.streamBlocks); i++ {
		starts = append(starts, len(lines))
		lines = append(lines, m.renderStreamBlock(m.streamBlocks[i], contentW)...)
	}
	tailStart := len(lines)
	lines = append(lines, m.chatSuffixLines(contentW)...)
	cache.lines = lines
	cache.streamBlockStarts = starts
	cache.streamTailStart = tailStart
	cache.streamDirtyFrom = -1
	cache.incrementalBuilds++
	cache.renderedStreamBlocks = len(m.streamBlocks) - from
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
		if len(m.toolCallMap) >= maxToolCalls || len(m.streamBlocks) >= maxStreamBlocks {
			return m, nil
		}
		tc := &ToolCallState{
			ID:        msg.ID,
			Title:     truncateUTF8Bytes(msg.Title, 4096),
			Kind:      msg.Kind,
			Status:    msg.Status,
			Locations: boundedToolLocations(msg.Locations),
			Content:   boundedToolContent(msg.Content),
			StartTime: time.Now(),
		}
		m.toolCallMap[string(msg.ID)] = tc
		m.streamBlocks = append(m.streamBlocks, StreamBlock{Kind: BlockToolCall, ToolCall: tc})
		m.invalidateChatCache()
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentToolCallUpdateMsg:
		if tc, ok := m.toolCallMap[string(msg.ID)]; ok {
			if msg.Title != nil {
				tc.Title = truncateUTF8Bytes(*msg.Title, 4096)
			}
			if msg.Status != nil {
				tc.Status = *msg.Status
				if *msg.Status == sdk.ToolCallStatusCompleted || *msg.Status == sdk.ToolCallStatusFailed {
					tc.EndTime = time.Now()
				}
			}
			if msg.Content != nil {
				tc.Content = boundedToolContent(msg.Content)
			}
			if msg.Locations != nil {
				tc.Locations = boundedToolLocations(msg.Locations)
			}
			m.invalidateChatCache()
		}
		return m, nil

	case acp.AgentPlanMsg:
		return m, nil

	case acp.AgentWriteFileMsg:
		if writeProposalCancelled(msg) {
			respondToCancelledWrite(msg)
			return m, nil
		}
		if m.pendingWrite == nil {
			m.pendingWrite = &msg
		} else if len(m.pendingWrites) >= maxPendingWrites {
			select {
			case msg.ResponseCh <- fmt.Errorf("too many pending agent write proposals"):
			default:
			}
			return m, nil
		} else {
			m.pendingWrites = append(m.pendingWrites, msg)
		}
		m.invalidateChatCache()
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentWriteCancelledMsg:
		m.cancelPendingWrite(msg.ResponseCh)
		return m, nil

	case acp.AgentPermissionRequestMsg:
		kind := ""
		if msg.ToolCall.Kind != nil {
			kind = string(*msg.ToolCall.Kind)
		}
		if kind != "" && m.alwaysAllow[kind] {
			for _, opt := range msg.Options {
				if opt.Kind == sdk.PermissionOptionKindAllowOnce || opt.Kind == sdk.PermissionOptionKindAllowAlways {
					deliverPermissionResponse(msg.ResponseCh, sdk.RequestPermissionResponse{
						Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
					})
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
		m.invalidateChatCache()
		if m.autoScroll {
			m.scrollY = m.maxScroll + 10
		}
		return m, nil

	case acp.AgentPromptResponseMsg:
		if msg.Err != nil {
			m.AddSystemMessage("Agent request failed: " + msg.Err.Error())
		}
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
			m.appendChatMessage(ChatMessage{
				Role:      RoleAgent,
				Content:   content,
				ToolCalls: toolCalls,
			})
		}
		m.streamBlocks = nil
		m.streamBytes = 0
		m.toolCallMap = make(map[string]*ToolCallState)
		m.loading = false
		m.state = AgentIdle
		m.invalidateChatCache()
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
		m.invalidateChatCache()
		return m, nil

	case acp.AgentStoppedMsg:
		m.connected = false
		m.state = AgentDisconnected
		m.loading = false
		m.invalidateChatCache()
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.MouseWheelMsg:
		m.refreshScrollBounds()
		mouse := msg.Mouse()
		switch mouse.Button {
		case tea.MouseWheelUp:
			m.scrollY -= 3
			if m.scrollY < 0 {
				m.scrollY = 0
			}
			m.autoScroll = false
		case tea.MouseWheelDown:
			m.scrollY += 3
			if m.scrollY > m.maxScroll {
				m.scrollY = m.maxScroll
			}
			if m.scrollY >= m.maxScroll {
				m.autoScroll = true
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			if m.permission != nil {
				if zone.Get("agent-perm-allow").InBounds(msg) {
					return m.handlePermissionKey("y")
				}
				if zone.Get("agent-perm-deny").InBounds(msg) {
					return m.handlePermissionKey("n")
				}
				if zone.Get("agent-perm-always").InBounds(msg) {
					return m.handlePermissionKey("a")
				}
			}
			if m.pendingWrite != nil {
				if zone.Get("agent-write-accept").InBounds(msg) {
					return m, m.AcceptWrite()
				}
				if zone.Get("agent-write-reject").InBounds(msg) {
					return m, m.RejectWrite()
				}
			}
		}
		return m, m.input.Focus()

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
	if m.pendingWrite != nil {
		switch key {
		case "enter":
			return m, m.AcceptWrite()
		case "esc", "escape":
			return m, m.RejectWrite()
		default:
			return m, nil
		}
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
		m.appendChatMessage(ChatMessage{Role: RoleUser, Content: text})
		m.input.SetValue("")
		m.loading = true
		m.state = AgentThinking
		m.autoScroll = true
		m.invalidateChatCache()
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
			m.invalidateChatCache()
		}
		return m, nil

	case "pgup", "pageup":
		m.refreshScrollBounds()
		m.scrollY -= m.chatViewHeight()
		if m.scrollY < 0 {
			m.scrollY = 0
		}
		m.autoScroll = false
		return m, nil

	case "pgdown", "pagedown":
		m.refreshScrollBounds()
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
		m.refreshScrollBounds()
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
				deliverPermissionResponse(perm.ResponseCh, sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				})
				m.permission = nil
				m.state = AgentThinking
				m.invalidateChatCache()
				return m, nil
			}
		}
		if len(perm.Options) > 0 {
			deliverPermissionResponse(perm.ResponseCh, sdk.RequestPermissionResponse{
				Outcome: sdk.NewRequestPermissionOutcomeSelected(perm.Options[0].OptionId),
			})
			m.permission = nil
			m.state = AgentThinking
			m.invalidateChatCache()
		}
		return m, nil

	case "n":
		for _, opt := range perm.Options {
			if opt.Kind == sdk.PermissionOptionKindRejectOnce {
				deliverPermissionResponse(perm.ResponseCh, sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				})
				m.permission = nil
				m.state = AgentThinking
				m.invalidateChatCache()
				return m, nil
			}
		}
		deliverPermissionResponse(perm.ResponseCh, sdk.RequestPermissionResponse{
			Outcome: sdk.NewRequestPermissionOutcomeCancelled(),
		})
		m.permission = nil
		m.state = AgentThinking
		m.invalidateChatCache()
		return m, nil

	case "a":
		kind := ""
		if perm.ToolCall.Kind != nil {
			kind = string(*perm.ToolCall.Kind)
		}
		if kind != "" {
			m.alwaysAllow[kind] = true
		}
		for _, opt := range perm.Options {
			if opt.Kind == sdk.PermissionOptionKindAllowAlways {
				deliverPermissionResponse(perm.ResponseCh, sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				})
				m.permission = nil
				m.state = AgentThinking
				m.invalidateChatCache()
				return m, nil
			}
		}
		for _, opt := range perm.Options {
			if opt.Kind == sdk.PermissionOptionKindAllowOnce {
				deliverPermissionResponse(perm.ResponseCh, sdk.RequestPermissionResponse{
					Outcome: sdk.NewRequestPermissionOutcomeSelected(opt.OptionId),
				})
				m.permission = nil
				m.state = AgentThinking
				m.invalidateChatCache()
				return m, nil
			}
		}
		return m, nil
	}

	return m, nil
}

func deliverPermissionResponse(ch chan sdk.RequestPermissionResponse, response sdk.RequestPermissionResponse) {
	select {
	case ch <- response:
	default:
	}
}

func (m Model) chatViewHeight() int {
	h := m.height - 3
	if len(m.taggedFiles) > 0 {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

// refreshScrollBounds recalculates scroll state from the current chat content
// and panel dimensions. Scroll controls call this instead of relying on View
// having persisted a previous render's measurements.
func (m *Model) refreshScrollBounds() {
	width := m.width
	if width < 1 {
		width = 1
	}
	m.setScrollBounds(len(m.cachedChatLines(width)))
}

func (m *Model) setScrollBounds(chatLineCount int) {
	m.lastChatLineCount = chatLineCount
	m.maxScroll = chatLineCount - m.chatViewHeight()
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
	chatLines := m.cachedChatLines(innerW)
	m.lastChatLineCount = len(chatLines)

	// Compute scroll (pointer receiver — persists).
	m.setScrollBounds(len(chatLines))

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
		line = ansi.Truncate(line, width, "")
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
		if lipgloss.Width(modelStr) > 25 {
			modelStr = ansi.Truncate(modelStr, 25, "...")
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
	lines, _, _ := m.buildChatLinesWithStreamMetadata(width)
	return lines
}

// buildChatLinesWithStreamMetadata is the canonical renderer used for a full
// rebuild. The metadata identifies the independently cacheable stream tail.
func (m Model) buildChatLinesWithStreamMetadata(width int) ([]string, []int, int) {
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
		return lines, nil, -1
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
		return lines, nil, -1
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

	var streamBlockStarts []int
	streamTailStart := -1

	// Streaming blocks in chronological order.
	if len(m.streamBlocks) > 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(ui.Nord14).Bold(true).Render(" Agent:"))
		for _, block := range m.streamBlocks {
			streamBlockStarts = append(streamBlockStarts, len(lines))
			lines = append(lines, m.renderStreamBlock(block, contentW)...)
		}
		streamTailStart = len(lines)
	}

	lines = append(lines, m.chatSuffixLines(contentW)...)

	return lines, streamBlockStarts, streamTailStart
}

func (m Model) renderStreamBlock(block StreamBlock, contentW int) []string {
	var lines []string
	switch block.Kind {
	case BlockText:
		for _, line := range wrapText(block.Content, contentW) {
			lines = append(lines, "  "+line)
		}
	case BlockThought:
		thoughtStyle := lipgloss.NewStyle().Foreground(ui.Nord3).Italic(true)
		for _, line := range wrapText(block.Content, contentW) {
			lines = append(lines, "  "+thoughtStyle.Render(line))
		}
	case BlockToolCall:
		if block.ToolCall != nil {
			lines = append(lines, m.renderToolCall(block.ToolCall, contentW)...)
		}
	}
	return lines
}

func (m Model) chatSuffixLines(contentW int) []string {
	var lines []string
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
	if lipgloss.Width(line) > width+2 {
		line = ansi.Truncate(line, width+2, "")
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
	optLine += zone.Mark("agent-perm-allow", lipgloss.NewStyle().Foreground(ui.Nord14).Render("[y] Allow"))
	optLine += "  "
	optLine += zone.Mark("agent-perm-deny", lipgloss.NewStyle().Foreground(ui.Nord11).Render("[n] Deny"))
	optLine += "  "
	optLine += zone.Mark("agent-perm-always", lipgloss.NewStyle().Foreground(ui.Nord13).Render("[a] Always"))
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
	lines = append(lines, "  "+zone.Mark("agent-write-accept", lipgloss.NewStyle().Foreground(ui.Nord14).Render("[Enter] Accept"))+"  "+zone.Mark("agent-write-reject", lipgloss.NewStyle().Foreground(ui.Nord11).Render("[Esc] Reject")))

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
	text = strings.ToValidUTF8(text, "�")
	return strings.Split(ansi.Wrap(text, width, " "), "\n")
}
