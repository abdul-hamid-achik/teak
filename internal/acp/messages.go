package acp

import (
	sdk "github.com/coder/acp-go-sdk"
)

// AgentTextMsg carries a streaming text chunk from the agent.
type AgentTextMsg struct {
	Text string
}

// AgentThoughtMsg carries a reasoning/thought chunk from the agent.
type AgentThoughtMsg struct {
	Text string
}

// AgentToolCallMsg is sent when the agent initiates a new tool call.
type AgentToolCallMsg struct {
	ID        sdk.ToolCallId
	Title     string
	Kind      sdk.ToolKind
	Status    sdk.ToolCallStatus
	Locations []sdk.ToolCallLocation
	Content   []sdk.ToolCallContent
}

// AgentToolCallUpdateMsg is sent when a tool call status/content changes.
type AgentToolCallUpdateMsg struct {
	ID        sdk.ToolCallId
	Title     *string
	Kind      *sdk.ToolKind
	Status    *sdk.ToolCallStatus
	Content   []sdk.ToolCallContent
	Locations []sdk.ToolCallLocation
}

// AgentPlanMsg is sent when the agent updates its execution plan.
type AgentPlanMsg struct {
	Entries []sdk.PlanEntry
}

// AgentWriteFileMsg is sent when the agent wants to write a file.
// The handler blocks on ResponseCh until the UI responds.
type AgentWriteFileMsg struct {
	Path       string
	Content    string
	ResponseCh chan error
}

// AgentPermissionRequestMsg is sent when the agent requests user permission.
// The handler blocks on ResponseCh until the UI responds.
type AgentPermissionRequestMsg struct {
	ToolCall   sdk.RequestPermissionToolCall
	Options    []sdk.PermissionOption
	ResponseCh chan sdk.RequestPermissionResponse
}

// FileReadRequestMsg is sent when the agent wants to read a file.
// Routed through the Bubbletea loop for goroutine safety.
type FileReadRequestMsg struct {
	Path     string
	Line     *int
	Limit    *int
	ResultCh chan FileReadResult
}

// FileReadResult carries the result of a file read request.
type FileReadResult struct {
	Content string
	Err     error
}

// AgentPromptResponseMsg is sent when a Prompt() call returns.
type AgentPromptResponseMsg struct {
	StopReason sdk.StopReason
	Err        error
}

// AgentSessionInfoMsg carries session model/mode state from NewSession.
type AgentSessionInfoMsg struct {
	SessionID    sdk.SessionId
	Models       []sdk.ModelInfo
	CurrentModel sdk.ModelId
	Modes        []sdk.SessionMode
	CurrentMode  sdk.SessionModeId
}

// AgentModelChangedMsg indicates the model was changed successfully.
type AgentModelChangedMsg struct {
	ModelId sdk.ModelId
}

// AgentStartedMsg indicates the ACP agent process has started.
type AgentStartedMsg struct{}

// AgentStoppedMsg indicates the ACP agent process has stopped.
type AgentStoppedMsg struct {
	Err error
}

// AgentModeChangedMsg indicates the mode was changed successfully.
type AgentModeChangedMsg struct {
	ModeId sdk.SessionModeId
}

// AgentErrorMsg indicates an error from the agent.
type AgentErrorMsg struct {
	Err error
}
