package agent

import (
	"time"

	sdk "github.com/coder/acp-go-sdk"
	"teak/internal/acp"
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

const (
	maxToolOutputLines    = 100
	maxChatMessages       = 500
	maxChatMessageBytes   = 512 << 10
	maxChatHistoryBytes   = 4 << 20
	maxStreamBlocks       = 256
	maxStreamContentBytes = 2 << 20
	// streamRenderBlockBytes bounds the amount of text that a single ACP
	// chunk can make the renderer revisit. Splitting only affects the
	// renderer's internal blocks; the final prompt still concatenates them.
	streamRenderBlockBytes = acp.MaxAgentStreamChunkBytes
	maxToolCalls           = 64
	maxToolContentBytes    = 32 << 10
	maxPendingWrites       = 32
	maxTaggedFiles         = 64
)

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

// PromptFinalizedMsg carries a prompt transcript prepared outside Bubble
// Tea's Update loop. Its payload is intentionally private: the app only routes
// the message back to the panel that owns the transcript.
type PromptFinalizedMsg struct {
	generation uint64
	messages   []preparedChatMessage
}

type preparedChatMessage struct {
	message ChatMessage
	size    int
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

// WriteDecisionMsg carries the user's decision for an ACP file-write proposal.
// The app owns responding to the agent and writing to disk.
type WriteDecisionMsg struct {
	Proposal acp.AgentWriteFileMsg
	Accepted bool
}
