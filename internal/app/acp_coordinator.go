package app

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"teak/internal/acp"
)

// Memory limits for ACP coordinator
const (
	maxACPChatHistory = 500
)

// ACPCoordinator manages ACP agent lifecycle and message handling.
type ACPCoordinator struct {
	mu           sync.RWMutex
	mgr          *acp.Manager
	running      bool
	sessionID    string
	modelID      string
	mode         string
	chatHistory  []ChatMessage
}

// ChatMessage represents a message in the chat history.
type ChatMessage struct {
	Role    string
	Content string
}

// NewACPCoordinator creates a new ACP coordinator.
func NewACPCoordinator(mgr *acp.Manager) *ACPCoordinator {
	return &ACPCoordinator{
		mgr:         mgr,
		running:     false,
		chatHistory: make([]ChatMessage, 0),
	}
}

// HandleMessage routes ACP messages to appropriate handlers.
func (c *ACPCoordinator) HandleMessage(msg tea.Msg) []tea.Cmd {
	switch m := msg.(type) {
	case acp.AgentTextMsg:
		return c.handleAgentText(m)
	case acp.AgentThoughtMsg:
		return c.handleAgentThought(m)
	case acp.AgentToolCallMsg:
		return c.handleAgentToolCall(m)
	case acp.AgentToolCallUpdateMsg:
		return c.handleAgentToolCallUpdate(m)
	case acp.AgentPlanMsg:
		return c.handleAgentPlan(m)
	case acp.AgentWriteFileMsg:
		return c.handleAgentWriteFile(m)
	case acp.AgentPermissionRequestMsg:
		return c.handleAgentPermissionRequest(m)
	case acp.AgentPromptResponseMsg:
		return c.handleAgentPromptResponse(m)
	case acp.AgentSessionInfoMsg:
		return c.handleAgentSessionInfo(m)
	case acp.AgentModelChangedMsg:
		return c.handleAgentModelChanged(m)
	case acp.AgentModeChangedMsg:
		return c.handleAgentModeChanged(m)
	case acp.AgentErrorMsg:
		return c.handleAgentError(m)
	case acp.AgentStartedMsg:
		return c.handleAgentStarted(m)
	case acp.AgentStoppedMsg:
		return c.handleAgentStopped(m)
	case acp.FileReadRequestMsg:
		return c.handleFileReadRequest(m)
	default:
		return nil
	}
}

// handleAgentText handles agent text messages.
func (c *ACPCoordinator) handleAgentText(msg acp.AgentTextMsg) []tea.Cmd {
	c.AddToHistory("agent", msg.Text)
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentThought handles agent thought messages.
func (c *ACPCoordinator) handleAgentThought(msg acp.AgentThoughtMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentToolCall handles tool call messages.
func (c *ACPCoordinator) handleAgentToolCall(msg acp.AgentToolCallMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentToolCallUpdate handles tool call update messages.
func (c *ACPCoordinator) handleAgentToolCallUpdate(msg acp.AgentToolCallUpdateMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentPlan handles agent plan messages.
func (c *ACPCoordinator) handleAgentPlan(msg acp.AgentPlanMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentWriteFile handles write file messages.
func (c *ACPCoordinator) handleAgentWriteFile(msg acp.AgentWriteFileMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentPermissionRequest handles permission request messages.
func (c *ACPCoordinator) handleAgentPermissionRequest(msg acp.AgentPermissionRequestMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentPromptResponse handles prompt response messages.
func (c *ACPCoordinator) handleAgentPromptResponse(msg acp.AgentPromptResponseMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentSessionInfo handles session info messages.
func (c *ACPCoordinator) handleAgentSessionInfo(msg acp.AgentSessionInfoMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = string(msg.CurrentModel)
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentModelChanged handles model changed messages.
func (c *ACPCoordinator) handleAgentModelChanged(msg acp.AgentModelChangedMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.modelID = string(msg.ModelId)
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentModeChanged handles mode changed messages.
func (c *ACPCoordinator) handleAgentModeChanged(msg acp.AgentModeChangedMsg) []tea.Cmd {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = string(msg.ModeId)
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentError handles error messages.
func (c *ACPCoordinator) handleAgentError(msg acp.AgentErrorMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentStarted handles agent started messages.
func (c *ACPCoordinator) handleAgentStarted(msg acp.AgentStartedMsg) []tea.Cmd {
	c.running = true
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleAgentStopped handles agent stopped messages.
func (c *ACPCoordinator) handleAgentStopped(msg acp.AgentStoppedMsg) []tea.Cmd {
	c.running = false
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// handleFileReadRequest handles file read requests.
func (c *ACPCoordinator) handleFileReadRequest(msg acp.FileReadRequestMsg) []tea.Cmd {
	return []tea.Cmd{func() tea.Msg { return msg }}
}

// AddToHistory adds a message to the chat history.
func (c *ACPCoordinator) AddToHistory(role, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chatHistory = append(c.chatHistory, ChatMessage{
		Role:    role,
		Content: content,
	})
	
	// Clean old entries if too many
	if len(c.chatHistory) > maxACPChatHistory {
		// Remove oldest entries (keep last maxACPChatHistory)
		c.chatHistory = c.chatHistory[len(c.chatHistory)-maxACPChatHistory:]
	}
}

// ClearHistory clears the chat history.
func (c *ACPCoordinator) ClearHistory() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chatHistory = nil
}

// GetHistory returns the chat history.
func (c *ACPCoordinator) GetHistory() []ChatMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// Return a copy to prevent external modification
	history := make([]ChatMessage, len(c.chatHistory))
	copy(history, c.chatHistory)
	return history
}

// SetSessionInfo sets the session information.
func (c *ACPCoordinator) SetSessionInfo(sessionID, modelID, mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = sessionID
	c.modelID = modelID
	c.mode = mode
}

// GetSessionInfo returns the session information.
func (c *ACPCoordinator) GetSessionInfo() (string, string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID, c.modelID, c.mode
}

// IsRunning returns true if the agent is running.
func (c *ACPCoordinator) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// SetRunning sets the running state.
func (c *ACPCoordinator) SetRunning(running bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = running
}

// GetManager returns the ACP manager.
func (c *ACPCoordinator) GetManager() *acp.Manager {
	return c.mgr
}

// Shutdown stops the agent.
func (c *ACPCoordinator) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mgr != nil {
		c.mgr.Stop()
	}
	c.running = false
}
