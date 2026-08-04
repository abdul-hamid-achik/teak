package acp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	log "github.com/charmbracelet/log"
	sdk "github.com/coder/acp-go-sdk"

	agentruntime "teak/internal/agent/runtime"
	"teak/internal/execpolicy"
	"teak/internal/toolpath"
)

const msgChanTimeout = 100 * time.Millisecond

const (
	maxTerminalCount         = 8
	maxTerminalOutputBytes   = 1 << 20
	maxTerminalArgs          = 64
	maxTerminalArgBytes      = 16 << 10
	maxTerminalArgsBytes     = 128 << 10
	maxTerminalEnvVars       = 64
	maxTerminalEnvValueBytes = 32 << 10
	maxTerminalEnvBytes      = 128 << 10
	maxPermissionOptions     = 16
	maxACPStreamOutputBytes  = 4 << 20
	maxACPToolCallBlocks     = 128
	maxACPLocations          = 64
	maxACPPlanEntries        = 128
	maxACPMetadataBytes      = 4 << 10
	maxAgentWriteBytes       = 16 << 20
	maxACPReadFileBytes      = 1 << 20
	maxACPReadLines          = 10_000
)

const acpStreamTruncatedMarker = "\n[truncated]"

const acpToolContentTruncatedMarker = "\n[tool content truncated]"

// ClientHandler implements sdk.Client. Its methods are called on the SDK's
// goroutine, so all data access is routed through the Bubbletea message loop
// via msgChan and blocking response channels.
type ClientHandler struct {
	msgChan chan<- tea.Msg
	rootDir string

	// runtime is optional for embedders that do not persist ACP runs. When it
	// is present, every capability-bearing handler method must authorize the
	// active run before touching the workspace or starting a process.
	runtime         *agentruntime.Manager
	activeRunID     func() agentruntime.RunID
	executionPolicy execpolicy.Policy

	// Terminal management
	mu               sync.Mutex
	terminals        map[string]*terminalState
	nextTermID       int
	pendingTerminals int
	streamBytes      int64
	streamLimit      int64
	streamTruncated  bool
}

func (h *ClientHandler) setRuntime(manager *agentruntime.Manager, activeRunID func() agentruntime.RunID) {
	h.runtime = manager
	h.activeRunID = activeRunID
}

func (h *ClientHandler) authorize(ctx context.Context, required agentruntime.Capabilities) error {
	ctx = normalizeACPContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.runtime == nil {
		return nil
	}
	if h.activeRunID == nil {
		return fmt.Errorf("agent runtime has no active run")
	}
	id := h.activeRunID()
	if id == "" {
		return fmt.Errorf("agent runtime has no active run")
	}
	if err := h.runtime.Authorize(id, required); err != nil {
		return fmt.Errorf("authorize agent capability: %w", err)
	}
	return nil
}

func (h *ClientHandler) currentRuntimeRunID() agentruntime.RunID {
	if h.runtime == nil || h.activeRunID == nil {
		return ""
	}
	return h.activeRunID()
}

// auditRuntimeOperation records only stable operation classifications. The
// runtime audit deliberately excludes command lines, paths, environment
// values, and process output; a persistence failure is logged but does not
// turn a successfully started child into a phantom failed request.
func (h *ClientHandler) auditRuntimeOperation(runID agentruntime.RunID, operation, outcome, detail string) {
	if h.runtime == nil || runID == "" {
		return
	}
	if err := h.runtime.RecordAudit(runID, operation, outcome, detail); err != nil {
		log.Debug("acp: unable to persist operation audit", "run_id", runID, "operation", operation, "outcome", outcome, "error", err)
	}
}

type terminalState struct {
	sequence      int
	cmd           *exec.Cmd
	output        []byte
	truncated     bool
	mu            sync.Mutex
	done          chan struct{}
	err           error
	exitCode      *int
	stopRuntime   func() bool
	cancelContext context.CancelFunc
}

// newClientHandler creates a handler restricted to rootDir. The optional form
// is retained for tests and defaults to the current working directory.
func newClientHandler(msgChan chan<- tea.Msg, roots ...string) *ClientHandler {
	rootDir, _ := os.Getwd()
	if len(roots) > 0 && roots[0] != "" {
		rootDir = roots[0]
	}
	return &ClientHandler{
		msgChan:         msgChan,
		rootDir:         rootDir,
		terminals:       make(map[string]*terminalState),
		executionPolicy: execpolicy.Policy{Root: rootDir, Mode: execpolicy.ModeOff},
	}
}

func (h *ClientHandler) setExecutionPolicy(policy execpolicy.Policy) {
	if policy.Root == "" {
		policy.Root = h.rootDir
	}
	h.executionPolicy = policy
}

// sendNonBlocking sends a message to msgChan with a timeout to prevent deadlocks.
// If the channel is full, it logs a warning and drops the message.
func (h *ClientHandler) sendNonBlocking(msg tea.Msg) {
	select {
	case h.msgChan <- msg:
	case <-time.After(msgChanTimeout):
		log.Warn("acp: dropped message (channel full)", "type", fmt.Sprintf("%T", msg))
	}
}

// beginPromptOutput resets the aggregate stream budget for one prompt. ACP
// can deliver many small session/update notifications, so bounding each chunk
// independently would still allow an unbounded response to accumulate in the
// message queue. The durable run budget is the tighter ceiling when available.
func (h *ClientHandler) beginPromptOutput() {
	limit := int64(maxACPStreamOutputBytes)
	if h.runtime != nil && h.activeRunID != nil {
		if runLimit, err := h.runtime.OutputLimit(h.activeRunID()); err == nil && runLimit > 0 && runLimit < limit {
			limit = runLimit
		}
	}
	h.mu.Lock()
	h.streamBytes = 0
	h.streamLimit = limit
	h.streamTruncated = false
	h.mu.Unlock()
}

// boundStreamText returns the part of one streamed text chunk that fits in
// the current prompt budget. It emits a short visible marker once and drops
// later chunks after the budget is exhausted.
func (h *ClientHandler) boundStreamText(input string) string {
	if input == "" {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	limit := h.streamLimit
	if limit <= 0 {
		limit = maxACPStreamOutputBytes
		h.streamLimit = limit
	}
	remaining := limit - h.streamBytes
	if remaining <= 0 {
		h.streamTruncated = true
		return ""
	}
	if int64(len(input)) <= remaining {
		h.streamBytes += int64(len(input))
		return input
	}

	markerBytes := int64(len(acpStreamTruncatedMarker))
	if remaining <= markerBytes {
		prefix := validUTF8Prefix([]byte(input[:minInt(len(input), int(remaining))]))
		h.streamBytes = limit
		h.streamTruncated = true
		return prefix
	}
	prefixLimit := int(remaining - markerBytes)
	prefix := validUTF8Prefix([]byte(input[:minInt(len(input), prefixLimit)]))
	h.streamBytes = limit
	h.streamTruncated = true
	return prefix + acpStreamTruncatedMarker
}

// boundToolCallContents copies tool-call payloads before they enter the
// Bubbletea queue. Agent message/thought chunks already use boundStreamText,
// but tool-call content can contain diffs, embedded resources, images, and
// arbitrary extension metadata. Keeping the SDK-owned input slice here would
// let one malicious notification retain many megabytes despite the stream
// budget.
func (h *ClientHandler) boundToolCallContents(contents []sdk.ToolCallContent) []sdk.ToolCallContent {
	if len(contents) == 0 {
		return nil
	}
	limit := len(contents)
	if limit > maxACPToolCallBlocks {
		limit = maxACPToolCallBlocks
	}
	bounded := make([]sdk.ToolCallContent, 0, limit+1)
	for _, content := range contents[:limit] {
		bounded = append(bounded, h.boundToolCallContent(content))
	}
	if len(contents) > limit {
		if marker := h.boundStreamText(acpToolContentTruncatedMarker); marker != "" {
			bounded = append(bounded, sdk.ToolContent(sdk.TextBlock(marker)))
		}
	}
	return bounded
}

func (h *ClientHandler) boundToolCallContent(content sdk.ToolCallContent) sdk.ToolCallContent {
	if content.Content != nil {
		value := *content.Content
		value.Content = h.boundContentBlock(value.Content)
		return sdk.ToolCallContent{Content: &value}
	}
	if content.Diff != nil {
		value := *content.Diff
		value.Meta = nil
		value.Path = capACPMetadataString(value.Path)
		value.NewText = h.boundStreamText(value.NewText)
		if value.OldText != nil {
			oldText := h.boundStreamText(*value.OldText)
			value.OldText = &oldText
		}
		return sdk.ToolCallContent{Diff: &value}
	}
	if content.Terminal != nil {
		value := *content.Terminal
		value.TerminalId = capACPMetadataString(value.TerminalId)
		return sdk.ToolCallContent{Terminal: &value}
	}
	return sdk.ToolCallContent{}
}

func (h *ClientHandler) boundContentBlock(block sdk.ContentBlock) sdk.ContentBlock {
	switch {
	case block.Text != nil:
		value := *block.Text
		value.Meta = nil
		value.Annotations = nil
		value.Text = h.boundStreamText(value.Text)
		return sdk.ContentBlock{Text: &value}
	case block.Image != nil:
		value := *block.Image
		value.Meta = nil
		value.Annotations = nil
		value.MimeType = capACPMetadataString(value.MimeType)
		value.Uri = capACPMetadata(value.Uri)
		bounded := h.boundBinaryContent("image", value.Data)
		if bounded != value.Data {
			return sdk.TextBlock(bounded)
		}
		value.Data = bounded
		return sdk.ContentBlock{Image: &value}
	case block.Audio != nil:
		value := *block.Audio
		value.Meta = nil
		value.Annotations = nil
		value.MimeType = capACPMetadataString(value.MimeType)
		bounded := h.boundBinaryContent("audio", value.Data)
		if bounded != value.Data {
			return sdk.TextBlock(bounded)
		}
		value.Data = bounded
		return sdk.ContentBlock{Audio: &value}
	case block.ResourceLink != nil:
		value := *block.ResourceLink
		value.Meta = nil
		value.Annotations = nil
		value.Description = capACPMetadata(value.Description)
		value.MimeType = capACPMetadata(value.MimeType)
		value.Name = capACPMetadataString(value.Name)
		value.Title = capACPMetadata(value.Title)
		value.Uri = capACPMetadataString(value.Uri)
		return sdk.ContentBlock{ResourceLink: &value}
	case block.Resource != nil:
		value := *block.Resource
		value.Meta = nil
		value.Annotations = nil
		if resource := value.Resource.TextResourceContents; resource != nil {
			resourceCopy := *resource
			resourceCopy.Meta = nil
			resourceCopy.Text = h.boundStreamText(resourceCopy.Text)
			resourceCopy.Uri = capACPMetadataString(resourceCopy.Uri)
			resourceCopy.MimeType = capACPMetadata(resourceCopy.MimeType)
			value.Resource = sdk.EmbeddedResourceResource{TextResourceContents: &resourceCopy}
			return sdk.ContentBlock{Resource: &value}
		}
		if resource := value.Resource.BlobResourceContents; resource != nil {
			bounded := h.boundBinaryContent("resource", resource.Blob)
			if bounded != resource.Blob {
				return sdk.TextBlock(bounded)
			}
			resourceCopy := *resource
			resourceCopy.Meta = nil
			resourceCopy.Blob = bounded
			resourceCopy.Uri = capACPMetadataString(resourceCopy.Uri)
			resourceCopy.MimeType = capACPMetadata(resourceCopy.MimeType)
			value.Resource = sdk.EmbeddedResourceResource{BlobResourceContents: &resourceCopy}
			return sdk.ContentBlock{Resource: &value}
		}
		return sdk.ContentBlock{Resource: &value}
	default:
		return sdk.ContentBlock{}
	}
}

func (h *ClientHandler) boundBinaryContent(kind, input string) string {
	bounded := h.boundStreamText(input)
	if bounded == input {
		return bounded
	}
	return "[" + kind + " content truncated]"
}

func capACPMetadata(value *string) *string {
	if value == nil {
		return nil
	}
	bounded := capACPMetadataString(*value)
	return &bounded
}

func capACPMetadataString(value string) string {
	if len(value) <= maxACPMetadataBytes {
		return value
	}
	return validUTF8Prefix([]byte(value[:maxACPMetadataBytes]))
}

func (h *ClientHandler) boundToolCallLocations(locations []sdk.ToolCallLocation) []sdk.ToolCallLocation {
	if len(locations) == 0 {
		return nil
	}
	limit := len(locations)
	if limit > maxACPLocations {
		limit = maxACPLocations
	}
	bounded := make([]sdk.ToolCallLocation, 0, limit)
	for _, location := range locations[:limit] {
		value := sdk.ToolCallLocation{
			Path: capACPMetadataString(location.Path),
		}
		if location.Line != nil && *location.Line >= 0 {
			line := *location.Line
			value.Line = &line
		}
		// Metadata is an extension point controlled by the agent. Do not retain
		// an arbitrarily large object graph in the Bubbletea queue.
		bounded = append(bounded, value)
	}
	return bounded
}

func (h *ClientHandler) boundPlanEntries(entries []sdk.PlanEntry) []sdk.PlanEntry {
	if len(entries) == 0 {
		return nil
	}
	limit := len(entries)
	if limit > maxACPPlanEntries {
		limit = maxACPPlanEntries
	}
	bounded := make([]sdk.PlanEntry, 0, limit)
	for _, entry := range entries[:limit] {
		bounded = append(bounded, sdk.PlanEntry{
			Content:  h.boundStreamText(capACPMetadataString(entry.Content)),
			Priority: sdk.PlanEntryPriority(capACPMetadataString(string(entry.Priority))),
			Status:   sdk.PlanEntryStatus(capACPMetadataString(string(entry.Status))),
		})
	}
	return bounded
}

func capToolKind(value *sdk.ToolKind) *sdk.ToolKind {
	if value == nil {
		return nil
	}
	bounded := sdk.ToolKind(capACPMetadataString(string(*value)))
	return &bounded
}

func capToolCallStatus(value *sdk.ToolCallStatus) *sdk.ToolCallStatus {
	if value == nil {
		return nil
	}
	bounded := sdk.ToolCallStatus(capACPMetadataString(string(*value)))
	return &bounded
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SessionUpdate receives streaming updates from the agent and dispatches
// them as typed Bubbletea messages.
func (h *ClientHandler) SessionUpdate(_ context.Context, params sdk.SessionNotification) error {
	u := params.Update

	switch {
	case u.AgentMessageChunk != nil:
		text := h.boundStreamText(extractText(u.AgentMessageChunk.Content))
		if text != "" {
			h.sendNonBlocking(AgentTextMsg{Text: text})
		}

	case u.AgentThoughtChunk != nil:
		text := h.boundStreamText(extractText(u.AgentThoughtChunk.Content))
		if text != "" {
			h.sendNonBlocking(AgentThoughtMsg{Text: text})
		}

	case u.ToolCall != nil:
		h.sendNonBlocking(AgentToolCallMsg{
			ID:        sdk.ToolCallId(capACPMetadataString(string(u.ToolCall.ToolCallId))),
			Title:     capACPMetadataString(u.ToolCall.Title),
			Kind:      sdk.ToolKind(capACPMetadataString(string(u.ToolCall.Kind))),
			Status:    sdk.ToolCallStatus(capACPMetadataString(string(u.ToolCall.Status))),
			Locations: h.boundToolCallLocations(u.ToolCall.Locations),
			Content:   h.boundToolCallContents(u.ToolCall.Content),
		})

	case u.ToolCallUpdate != nil:
		h.sendNonBlocking(AgentToolCallUpdateMsg{
			ID:        sdk.ToolCallId(capACPMetadataString(string(u.ToolCallUpdate.ToolCallId))),
			Title:     capACPMetadata(u.ToolCallUpdate.Title),
			Kind:      capToolKind(u.ToolCallUpdate.Kind),
			Status:    capToolCallStatus(u.ToolCallUpdate.Status),
			Content:   h.boundToolCallContents(u.ToolCallUpdate.Content),
			Locations: h.boundToolCallLocations(u.ToolCallUpdate.Locations),
		})

	case u.Plan != nil:
		h.sendNonBlocking(AgentPlanMsg{Entries: h.boundPlanEntries(u.Plan.Entries)})
	}

	return nil
}

// ReadTextFile sends a read request through the Bubbletea loop and blocks
// until the result is available.
func (h *ClientHandler) ReadTextFile(ctx context.Context, params sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	ctx = normalizeACPContext(ctx)
	runID := h.currentRuntimeRunID()
	if err := h.authorize(ctx, agentruntime.Capabilities{Read: true}); err != nil {
		h.auditRuntimeOperation(runID, "file_read", "denied", "read_capability")
		return sdk.ReadTextFileResponse{}, err
	}
	resultCh := make(chan FileReadResult, 1)
	select {
	case h.msgChan <- FileReadRequestMsg{
		Path:     params.Path,
		Line:     params.Line,
		Limit:    params.Limit,
		RootDir:  h.rootDir,
		Context:  ctx,
		ResultCh: resultCh,
	}:
	case <-ctx.Done():
		h.auditRuntimeOperation(runID, "file_read", "failed", "request_cancelled")
		return sdk.ReadTextFileResponse{}, ctx.Err()
	}

	select {
	case result := <-resultCh:
		if result.Err != nil {
			h.auditRuntimeOperation(runID, "file_read", "failed", "workspace_read")
			return sdk.ReadTextFileResponse{}, result.Err
		}
		h.auditRuntimeOperation(runID, "file_read", "completed", fmt.Sprintf("bytes=%d", len(result.Content)))
		return sdk.ReadTextFileResponse{Content: result.Content}, nil
	case <-ctx.Done():
		h.auditRuntimeOperation(runID, "file_read", "failed", "request_cancelled")
		return sdk.ReadTextFileResponse{}, ctx.Err()
	}
}

// WriteTextFile sends a write proposal through the Bubbletea loop and blocks
// until the user accepts or rejects.
func (h *ClientHandler) WriteTextFile(ctx context.Context, params sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	ctx = normalizeACPContext(ctx)
	runID := h.currentRuntimeRunID()
	if err := h.authorize(ctx, agentruntime.Capabilities{Write: true}); err != nil {
		h.auditRuntimeOperation(runID, "file_write", "denied", "write_capability")
		return sdk.WriteTextFileResponse{}, err
	}
	if len(params.Content) > maxAgentWriteBytes {
		h.auditRuntimeOperation(runID, "file_write", "denied", "proposal_limit")
		return sdk.WriteTextFileResponse{}, fmt.Errorf(
			"write proposal is %d bytes; limit is %d",
			len(params.Content),
			maxAgentWriteBytes,
		)
	}
	responseCh := make(chan error, 1)
	select {
	case h.msgChan <- AgentWriteFileMsg{
		Path:       params.Path,
		Content:    params.Content,
		ResponseCh: responseCh,
		Context:    ctx,
	}:
	case <-ctx.Done():
		h.auditRuntimeOperation(runID, "file_write", "failed", "request_cancelled")
		return sdk.WriteTextFileResponse{}, ctx.Err()
	}

	select {
	case err := <-responseCh:
		if err != nil {
			h.auditRuntimeOperation(runID, "file_write", "failed", "workspace_write")
			return sdk.WriteTextFileResponse{}, err
		}
		h.auditRuntimeOperation(runID, "file_write", "completed", fmt.Sprintf("bytes=%d", len(params.Content)))
		return sdk.WriteTextFileResponse{}, nil
	case <-ctx.Done():
		h.sendNonBlocking(AgentWriteCancelledMsg{ResponseCh: responseCh})
		h.auditRuntimeOperation(runID, "file_write", "failed", "request_cancelled")
		return sdk.WriteTextFileResponse{}, ctx.Err()
	}
}

// RequestPermission sends a permission prompt through the Bubbletea loop.
func (h *ClientHandler) RequestPermission(ctx context.Context, params sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	ctx = normalizeACPContext(ctx)
	runID := h.currentRuntimeRunID()
	// Permission is not itself a capability-bearing operation, but a terminal
	// or externally cancelled run must not be able to keep presenting prompts.
	if err := h.authorize(ctx, agentruntime.Capabilities{}); err != nil {
		h.auditRuntimeOperation(runID, "permission", "denied", "runtime_boundary")
		return sdk.RequestPermissionResponse{}, err
	}
	toolCall, options, err := h.boundPermissionPayload(params.ToolCall, params.Options)
	if err != nil {
		h.auditRuntimeOperation(runID, "permission", "denied", "payload_limit")
		return sdk.RequestPermissionResponse{}, err
	}
	responseCh := make(chan sdk.RequestPermissionResponse, 1)
	select {
	case h.msgChan <- AgentPermissionRequestMsg{
		ToolCall:   toolCall,
		Options:    options,
		ResponseCh: responseCh,
	}:
	case <-ctx.Done():
		h.auditRuntimeOperation(runID, "permission", "failed", "request_cancelled")
		return sdk.RequestPermissionResponse{}, ctx.Err()
	}

	select {
	case resp := <-responseCh:
		outcome, detail := permissionAuditDecision(resp, options)
		h.auditRuntimeOperation(runID, "permission", outcome, detail)
		return resp, nil
	case <-ctx.Done():
		h.auditRuntimeOperation(runID, "permission", "failed", "request_cancelled")
		return sdk.RequestPermissionResponse{}, ctx.Err()
	}
}

func permissionAuditDecision(response sdk.RequestPermissionResponse, options []sdk.PermissionOption) (string, string) {
	if response.Outcome.Cancelled != nil {
		return "cancelled", "user_or_session"
	}
	if response.Outcome.Selected == nil {
		return "failed", "invalid_outcome"
	}
	for _, option := range options {
		if option.OptionId != response.Outcome.Selected.OptionId {
			continue
		}
		switch option.Kind {
		case sdk.PermissionOptionKindAllowOnce:
			return "allowed_once", "scope=once"
		case sdk.PermissionOptionKindAllowAlways:
			return "allowed_always", "scope=always"
		case sdk.PermissionOptionKindRejectOnce:
			return "rejected_once", "scope=once"
		case sdk.PermissionOptionKindRejectAlways:
			return "rejected_always", "scope=always"
		default:
			return "failed", "unsupported_option"
		}
	}
	return "failed", "unknown_option"
}

// boundPermissionPayload copies the subset of a permission request that the
// UI needs. Raw input/output and extension metadata are deliberately dropped:
// they are agent-controlled object graphs and should never be retained in the
// Bubble Tea queue. Option IDs are rejected rather than truncated because the
// selected ID must round-trip exactly to the ACP server.
func (h *ClientHandler) boundPermissionPayload(toolCall sdk.RequestPermissionToolCall, options []sdk.PermissionOption) (sdk.RequestPermissionToolCall, []sdk.PermissionOption, error) {
	if len(options) > maxPermissionOptions {
		return sdk.RequestPermissionToolCall{}, nil, fmt.Errorf("permission options exceed %d entries", maxPermissionOptions)
	}
	boundedOptions := make([]sdk.PermissionOption, 0, len(options))
	seenIDs := make(map[sdk.PermissionOptionId]struct{}, len(options))
	for _, option := range options {
		if len(option.Name) > maxACPMetadataBytes || len(option.OptionId) > maxACPMetadataBytes || len(option.Kind) > maxACPMetadataBytes {
			return sdk.RequestPermissionToolCall{}, nil, fmt.Errorf("permission option metadata exceeds %d bytes", maxACPMetadataBytes)
		}
		if _, duplicate := seenIDs[option.OptionId]; duplicate {
			return sdk.RequestPermissionToolCall{}, nil, fmt.Errorf("permission options contain duplicate id %q", option.OptionId)
		}
		seenIDs[option.OptionId] = struct{}{}
		boundedOptions = append(boundedOptions, sdk.PermissionOption{
			Kind:     option.Kind,
			Name:     option.Name,
			OptionId: option.OptionId,
		})
	}

	bounded := sdk.RequestPermissionToolCall{
		ToolCallId: sdk.ToolCallId(capACPMetadataString(string(toolCall.ToolCallId))),
		Kind:       capToolKind(toolCall.Kind),
		Status:     capToolCallStatus(toolCall.Status),
		Locations:  h.boundToolCallLocations(toolCall.Locations),
		Content:    h.boundToolCallContents(toolCall.Content),
	}
	if toolCall.Title != nil {
		title := capACPMetadataString(*toolCall.Title)
		bounded.Title = &title
	}
	return bounded, boundedOptions, nil
}

// CreateTerminal spawns a subprocess and tracks it. Explicit command and
// working-directory paths are confined to the workspace; bare commands remain
// available through PATH so normal developer tooling (git, go, sh) still works.
func (h *ClientHandler) CreateTerminal(ctx context.Context, params sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	ctx = normalizeACPContext(ctx)
	runID := h.currentRuntimeRunID()
	if err := h.authorize(ctx, agentruntime.Capabilities{Shell: true}); err != nil {
		h.auditRuntimeOperation(runID, "terminal", "denied", "shell_capability")
		return sdk.CreateTerminalResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return sdk.CreateTerminalResponse{}, err
	}
	if err := validateTerminalInvocation(params.Command, params.Args, params.Cwd); err != nil {
		return sdk.CreateTerminalResponse{}, err
	}

	rootDir, err := resolveWorkspaceRoot(h.rootDir)
	if err != nil {
		return sdk.CreateTerminalResponse{}, err
	}
	cwd, err := resolveTerminalCwd(rootDir, params.Cwd)
	if err != nil {
		return sdk.CreateTerminalResponse{}, err
	}
	command, err := resolveTerminalCommand(rootDir, cwd, params.Command)
	if err != nil {
		return sdk.CreateTerminalResponse{}, err
	}
	outputLimit, err := h.terminalOutputLimit(ctx, params.OutputByteLimit)
	if err != nil {
		return sdk.CreateTerminalResponse{}, err
	}
	env, err := terminalEnvironment(params.Env)
	if err != nil {
		return sdk.CreateTerminalResponse{}, err
	}

	h.mu.Lock()
	h.pruneCompletedTerminalsLocked()
	if len(h.terminals)+h.pendingTerminals >= maxTerminalCount {
		h.mu.Unlock()
		return sdk.CreateTerminalResponse{}, fmt.Errorf("terminal limit reached (%d)", maxTerminalCount)
	}
	h.nextTermID++
	sequence := h.nextTermID
	id := fmt.Sprintf("term-%d", sequence)
	h.pendingTerminals++
	h.mu.Unlock()

	terminalCtx := ctx
	var cancelContext context.CancelFunc
	var stopRuntime func() bool
	cleanupContext := func() {
		if stopRuntime != nil {
			stopRuntime()
		}
		if cancelContext != nil {
			cancelContext()
		}
	}
	if h.runtime != nil {
		if runID == "" {
			h.mu.Lock()
			h.pendingTerminals--
			h.mu.Unlock()
			return sdk.CreateTerminalResponse{}, fmt.Errorf("agent runtime has no active run")
		}
		runCtx, err := h.runtime.ActiveContext(runID)
		if err != nil {
			h.mu.Lock()
			h.pendingTerminals--
			h.mu.Unlock()
			return sdk.CreateTerminalResponse{}, fmt.Errorf("bind terminal to agent run: %w", err)
		}
		terminalCtx, cancelContext = context.WithCancel(ctx)
		stopRuntime = context.AfterFunc(runCtx, cancelContext)
	}

	ts := &terminalState{
		sequence:      sequence,
		done:          make(chan struct{}),
		stopRuntime:   stopRuntime,
		cancelContext: cancelContext,
	}
	allowWrite, allowNetwork := true, false
	if h.runtime != nil {
		if runID == "" {
			cleanupContext()
			h.mu.Lock()
			h.pendingTerminals--
			h.mu.Unlock()
			return sdk.CreateTerminalResponse{}, fmt.Errorf("agent runtime has no active run")
		}
		caps, capsErr := h.runtime.EffectiveCapabilities(runID)
		if capsErr != nil {
			cleanupContext()
			h.mu.Lock()
			h.pendingTerminals--
			h.mu.Unlock()
			return sdk.CreateTerminalResponse{}, fmt.Errorf("read agent execution capabilities: %w", capsErr)
		}
		allowWrite = caps.Write
		allowNetwork = caps.Network
	}
	policy := h.executionPolicy
	if policy.Mode != execpolicy.ModeOff {
		normalizedPolicy, policyErr := execpolicy.New(rootDir, policy.Mode)
		if policyErr != nil {
			cleanupContext()
			h.mu.Lock()
			h.pendingTerminals--
			h.mu.Unlock()
			return sdk.CreateTerminalResponse{}, fmt.Errorf("prepare terminal execution policy: %w", policyErr)
		}
		normalizedPolicy.SandboxExecutable = policy.SandboxExecutable
		policy = normalizedPolicy
	} else if policy.Root == "" {
		policy.Root = rootDir
	}
	cmd, sandboxStatus, err := policy.Command(terminalCtx, command, params.Args, allowWrite, allowNetwork)
	if err != nil {
		h.auditRuntimeOperation(runID, "terminal", "denied", "execution_policy")
		cleanupContext()
		h.mu.Lock()
		h.pendingTerminals--
		h.mu.Unlock()
		return sdk.CreateTerminalResponse{}, fmt.Errorf("resolve terminal command: %w", err)
	}
	log.Debug("acp terminal execution policy", "status", sandboxStatus, "write", allowWrite, "network", allowNetwork)
	auditDetail := fmt.Sprintf("sandbox=%s write=%t network=%t", sandboxStatus, allowWrite, allowNetwork)
	if h.runtime != nil {
		if err := h.runtime.RecordAudit(runID, "terminal", "authorized", auditDetail); err != nil {
			cleanupContext()
			h.mu.Lock()
			h.pendingTerminals--
			h.mu.Unlock()
			return sdk.CreateTerminalResponse{}, fmt.Errorf("record terminal authorization: %w", err)
		}
	}
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdout = terminalOutputWriter{state: ts, limit: outputLimit}
	cmd.Stderr = terminalOutputWriter{state: ts, limit: outputLimit}
	ts.cmd = cmd

	if err := cmd.Start(); err != nil {
		h.auditRuntimeOperation(runID, "terminal", "failed", "process_start")
		cleanupContext()
		h.mu.Lock()
		h.pendingTerminals--
		h.mu.Unlock()
		return sdk.CreateTerminalResponse{}, fmt.Errorf("start terminal: %w", err)
	}
	h.auditRuntimeOperation(runID, "terminal", "started", auditDetail)

	go func() {
		err := cmd.Wait()
		if ts.stopRuntime != nil {
			ts.stopRuntime()
		}
		if ts.cancelContext != nil {
			ts.cancelContext()
		}
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		ts.mu.Lock()
		ts.err = err
		ts.exitCode = &code
		ts.mu.Unlock()
		outcome := "exited"
		if err != nil {
			outcome = "failed"
		}
		h.auditRuntimeOperation(runID, "terminal", outcome, fmt.Sprintf("exit_code=%d", code))
		close(ts.done)
	}()

	h.mu.Lock()
	h.pendingTerminals--
	h.terminals[id] = ts
	h.mu.Unlock()

	return sdk.CreateTerminalResponse{TerminalId: id}, nil
}

// terminalOutputLimit combines the transport cap with the active durable run
// budget. The runtime budget is intentionally only a ceiling here: a caller's
// lower request and ACP's hard safety cap remain in force as well.
func (h *ClientHandler) terminalOutputLimit(ctx context.Context, requested *int) (int, error) {
	ctx = normalizeACPContext(ctx)
	limit, err := terminalOutputLimit(requested)
	if err != nil {
		return 0, err
	}
	if h.runtime == nil {
		return limit, nil
	}
	if h.activeRunID == nil {
		return 0, fmt.Errorf("agent runtime has no active run")
	}
	runLimit, err := h.runtime.OutputLimit(h.activeRunID())
	if err != nil {
		return 0, fmt.Errorf("read agent output budget: %w", err)
	}
	if runLimit < int64(limit) {
		limit = int(runLimit)
	}
	return limit, nil
}

// pruneCompletedTerminalsLocked releases the oldest completed terminal only
// when a new terminal needs its slot. Completed output remains available until
// then, while live processes and in-flight CreateTerminal calls are never
// eligible for removal. h.mu must be held.
func (h *ClientHandler) pruneCompletedTerminalsLocked() {
	for len(h.terminals)+h.pendingTerminals >= maxTerminalCount {
		var oldestID string
		var oldest *terminalState
		for id, state := range h.terminals {
			select {
			case <-state.done:
				if oldest == nil || state.sequence < oldest.sequence {
					oldestID = id
					oldest = state
				}
			default:
			}
		}
		if oldest == nil {
			return
		}
		delete(h.terminals, oldestID)
	}
}

// KillTerminalCommand sends SIGKILL to the terminal's process.
func (h *ClientHandler) KillTerminalCommand(_ context.Context, params sdk.KillTerminalCommandRequest) (sdk.KillTerminalCommandResponse, error) {
	h.mu.Lock()
	ts, ok := h.terminals[params.TerminalId]
	h.mu.Unlock()
	if !ok {
		return sdk.KillTerminalCommandResponse{}, fmt.Errorf("unknown terminal: %s", params.TerminalId)
	}
	if ts.cmd.Process != nil {
		_ = toolpath.TerminateCommand(ts.cmd)
	}
	return sdk.KillTerminalCommandResponse{}, nil
}

// TerminalOutput returns captured stdout/stderr.
func (h *ClientHandler) TerminalOutput(ctx context.Context, params sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	ctx = normalizeACPContext(ctx)
	if err := ctx.Err(); err != nil {
		return sdk.TerminalOutputResponse{}, err
	}
	h.mu.Lock()
	ts, ok := h.terminals[params.TerminalId]
	h.mu.Unlock()
	if !ok {
		return sdk.TerminalOutputResponse{}, fmt.Errorf("unknown terminal: %s", params.TerminalId)
	}

	ts.mu.Lock()
	output := validUTF8Prefix(ts.output)
	truncated := ts.truncated
	ts.mu.Unlock()

	var exitStatus *sdk.TerminalExitStatus
	select {
	case <-ts.done:
		ts.mu.Lock()
		code := ts.exitCode
		ts.mu.Unlock()
		exitStatus = &sdk.TerminalExitStatus{ExitCode: code}
	default:
	}

	resp := sdk.TerminalOutputResponse{
		Output:     output,
		ExitStatus: exitStatus,
		Truncated:  truncated,
	}
	// Workaround: SDK validation requires non-empty output
	if resp.Output == "" {
		resp.Output = " "
	}
	return resp, nil
}

// ReleaseTerminal kills and removes a terminal.
func (h *ClientHandler) ReleaseTerminal(_ context.Context, params sdk.ReleaseTerminalRequest) (sdk.ReleaseTerminalResponse, error) {
	h.mu.Lock()
	ts, ok := h.terminals[params.TerminalId]
	if ok {
		delete(h.terminals, params.TerminalId)
	}
	h.mu.Unlock()

	if ok && ts.cmd.Process != nil {
		_ = toolpath.TerminateCommand(ts.cmd)
	}
	return sdk.ReleaseTerminalResponse{}, nil
}

// WaitForTerminalExit blocks until the terminal command exits or its context
// is cancelled.
func (h *ClientHandler) WaitForTerminalExit(ctx context.Context, params sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	ctx = normalizeACPContext(ctx)
	h.mu.Lock()
	ts, ok := h.terminals[params.TerminalId]
	h.mu.Unlock()
	if !ok {
		return sdk.WaitForTerminalExitResponse{}, fmt.Errorf("unknown terminal: %s", params.TerminalId)
	}

	select {
	case <-ts.done:
	case <-ctx.Done():
		return sdk.WaitForTerminalExitResponse{}, ctx.Err()
	}

	ts.mu.Lock()
	code := ts.exitCode
	ts.mu.Unlock()
	return sdk.WaitForTerminalExitResponse{
		ExitCode: code,
	}, nil
}

// terminalOutputWriter bounds terminal output memory while preserving the tail
// of the stream. It deliberately reports a full write to avoid killing a child
// process simply because its older output was discarded.
type terminalOutputWriter struct {
	state *terminalState
	limit int
}

func (w terminalOutputWriter) Write(p []byte) (int, error) {
	w.state.mu.Lock()
	defer w.state.mu.Unlock()

	if len(w.state.output)+len(p) > w.limit {
		w.state.truncated = true
	}
	w.state.output = appendOutputWithinLimit(w.state.output, p, w.limit)
	return len(p), nil
}

func appendOutputWithinLimit(existing, incoming []byte, limit int) []byte {
	if len(incoming) >= limit {
		result := append([]byte(nil), incoming[len(incoming)-limit:]...)
		return trimLeadingUTF8Continuation(result)
	}

	keep := limit - len(incoming)
	if keep > len(existing) {
		keep = len(existing)
	}
	result := make([]byte, 0, keep+len(incoming))
	result = append(result, existing[len(existing)-keep:]...)
	result = append(result, incoming...)
	return trimLeadingUTF8Continuation(result)
}

func trimLeadingUTF8Continuation(data []byte) []byte {
	for len(data) > 0 && data[0]&0xc0 == 0x80 {
		data = data[1:]
	}
	return data
}

func validUTF8Prefix(data []byte) string {
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data)
}

func terminalOutputLimit(requested *int) (int, error) {
	if requested == nil {
		return maxTerminalOutputBytes, nil
	}
	if *requested <= 0 {
		return 0, fmt.Errorf("terminal output byte limit must be positive")
	}
	if *requested > maxTerminalOutputBytes {
		return maxTerminalOutputBytes, nil
	}
	return *requested, nil
}

// validateTerminalInvocation bounds agent-controlled argv and path strings
// before they reach os/exec. Output limits alone do not protect the parent
// from a request that allocates a huge argument vector or environment.
func validateTerminalInvocation(command string, args []string, cwd *string) error {
	if len(command) > maxTerminalArgBytes {
		return fmt.Errorf("terminal arguments exceed %d bytes: command is too large", maxTerminalArgBytes)
	}
	if cwd != nil && len(*cwd) > maxTerminalArgBytes {
		return fmt.Errorf("terminal arguments exceed %d bytes: working directory is too large", maxTerminalArgBytes)
	}
	return validateTerminalArgs(args)
}

func validateTerminalArgs(args []string) error {
	if len(args) > maxTerminalArgs {
		return fmt.Errorf("terminal arguments exceed %d entries", maxTerminalArgs)
	}
	total := 0
	for _, arg := range args {
		if len(arg) > maxTerminalArgBytes {
			return fmt.Errorf("terminal arguments exceed %d bytes per entry", maxTerminalArgBytes)
		}
		if total > maxTerminalArgsBytes-len(arg) {
			return fmt.Errorf("terminal arguments exceed %d bytes", maxTerminalArgsBytes)
		}
		total += len(arg)
	}
	return nil
}

func terminalEnvironment(requested []sdk.EnvVariable) ([]string, error) {
	if len(requested) > maxTerminalEnvVars {
		return nil, fmt.Errorf("terminal environment exceeds %d variables", maxTerminalEnvVars)
	}
	env := make([]string, 0, len(requested)+1)
	seen := make(map[string]struct{}, len(requested))
	pathSet := false
	totalBytes := 0
	appendVariable := func(name, value string) error {
		entryBytes := len(name) + 1 + len(value)
		if totalBytes > maxTerminalEnvBytes-entryBytes {
			return fmt.Errorf("terminal environment exceeds %d bytes", maxTerminalEnvBytes)
		}
		totalBytes += entryBytes
		env = append(env, name+"="+value)
		return nil
	}
	for _, variable := range requested {
		if !validEnvironmentName(variable.Name) || strings.ContainsRune(variable.Value, '\x00') {
			return nil, fmt.Errorf("invalid terminal environment variable %q", variable.Name)
		}
		if len(variable.Value) > maxTerminalEnvValueBytes {
			return nil, fmt.Errorf("terminal environment variable %q exceeds %d bytes", variable.Name, maxTerminalEnvValueBytes)
		}
		if _, exists := seen[variable.Name]; exists {
			return nil, fmt.Errorf("duplicate terminal environment variable %q", variable.Name)
		}
		seen[variable.Name] = struct{}{}
		if variable.Name == "PATH" {
			pathSet = true
		}
		if err := appendVariable(variable.Name, variable.Value); err != nil {
			return nil, err
		}
	}
	if !pathSet {
		if path := os.Getenv("PATH"); path != "" {
			if len(path) > maxTerminalEnvValueBytes {
				return nil, fmt.Errorf("terminal environment variable %q exceeds %d bytes", "PATH", maxTerminalEnvValueBytes)
			}
			if err := appendVariable("PATH", path); err != nil {
				return nil, err
			}
		}
	}
	return env, nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for i, char := range name {
		if char != '_' && (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (i == 0 || char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func resolveWorkspaceRoot(rootDir string) (string, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("stat workspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace root is not a directory: %s", rootDir)
	}
	return resolvedRoot, nil
}

func resolveTerminalCwd(rootDir string, requested *string) (string, error) {
	if requested == nil {
		return rootDir, nil
	}
	if !filepath.IsAbs(*requested) {
		return "", fmt.Errorf("terminal cwd must be absolute: %s", *requested)
	}
	resolved, err := filepath.EvalSymlinks(*requested)
	if err != nil {
		return "", fmt.Errorf("resolve terminal cwd: %w", err)
	}
	if !pathWithinRoot(rootDir, resolved) {
		return "", fmt.Errorf("terminal cwd is outside workspace: %s", *requested)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat terminal cwd: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("terminal cwd is not a directory: %s", *requested)
	}
	return resolved, nil
}

func resolveTerminalCommand(rootDir, cwd, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("terminal command is empty")
	}
	if !strings.ContainsRune(command, filepath.Separator) {
		// The agent's terminal inherits Teak's environment, so resolving here
		// rather than with a bare PATH lookup is what lets it find developer
		// tools that live outside the inherited PATH.
		resolved, err := toolpath.Resolve(command)
		if err != nil {
			return "", fmt.Errorf("find terminal command %q: %w", command, err)
		}
		return resolved, nil
	}

	path := command
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve terminal command: %w", err)
	}
	if !pathWithinRoot(rootDir, resolved) {
		return "", fmt.Errorf("terminal command is outside workspace: %s", command)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat terminal command: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("terminal command is not executable: %s", command)
	}
	return resolved, nil
}

func pathWithinRoot(rootDir, path string) bool {
	rel, err := filepath.Rel(rootDir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// extractText pulls the text content from a ContentBlock.
func extractText(block sdk.ContentBlock) string {
	if block.Text != nil {
		return block.Text.Text
	}
	// For resource blocks, try to extract text
	if block.Resource != nil && block.Resource.Resource.TextResourceContents != nil {
		return block.Resource.Resource.TextResourceContents.Text
	}
	return ""
}

// ResolveWorkspaceFile resolves path and ensures the final regular file stays
// inside rootDir. Symlinks are resolved before the containment check so a tag
// or ACP read cannot escape the workspace through a link.
func ResolveWorkspaceFile(rootDir, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("file path is empty")
	}
	root, err := resolveWorkspaceRoot(rootDir)
	if err != nil {
		return "", err
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	} else {
		// Normalize an absolute path written through the workspace's original
		// spelling (for example /var on macOS) onto its resolved root before
		// checking for symlinks below that root.
		originalRoot, rootErr := filepath.Abs(rootDir)
		if rootErr == nil {
			if rel, relErr := filepath.Rel(originalRoot, candidate); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				candidate = filepath.Join(root, rel)
			}
		}
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(candidate))
	if err != nil {
		return "", fmt.Errorf("resolve workspace file: %w", err)
	}
	if !pathWithinRoot(root, resolved) {
		return "", fmt.Errorf("file is outside workspace: %s", path)
	}
	if filepath.Clean(candidate) != resolved {
		return "", fmt.Errorf("workspace file must not traverse a symlink: %s", path)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat workspace file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("workspace path is not a regular file: %s", path)
	}
	return resolved, nil
}

// ReadFileFromDisk reads a workspace-confined file with a hard byte limit and
// optional ACP 1-based line filtering. It checks ctx between reads so canceled
// protocol requests don't keep a large disk read alive.
func ReadFileFromDisk(ctx context.Context, rootDir, path string, line *int, limit *int) (string, error) {
	data, err := readWorkspaceFile(ctx, rootDir, path, maxACPReadFileBytes)
	if err != nil {
		return "", err
	}
	return FilterReadContent(string(data), line, limit)
}

// FilterReadContent applies the ACP line selection while retaining the same
// output bound used for disk reads. It is also used for open editor buffers.
func FilterReadContent(content string, line *int, limit *int) (string, error) {
	if len(content) > maxACPReadFileBytes {
		return "", fmt.Errorf("file exceeds %d-byte read limit", maxACPReadFileBytes)
	}
	if line == nil && limit == nil {
		return content, nil
	}
	if line != nil && *line < 1 {
		return "", fmt.Errorf("line must be positive")
	}
	if limit != nil && (*limit < 1 || *limit > maxACPReadLines) {
		return "", fmt.Errorf("line limit must be between 1 and %d", maxACPReadLines)
	}
	lines := strings.Split(content, "\n")
	startLine := 0
	if line != nil {
		startLine = *line - 1
		if startLine >= len(lines) {
			return "", nil
		}
	}
	endLine := len(lines)
	if limit != nil && startLine+*limit < endLine {
		endLine = startLine + *limit
	}
	return strings.Join(lines[startLine:endLine], "\n"), nil
}

func readWorkspaceFile(ctx context.Context, rootDir, path string, maximum int) ([]byte, error) {
	return readWorkspaceFileWithBeforeOpen(ctx, rootDir, path, maximum, nil)
}

// readWorkspaceFileWithBeforeOpen opens a workspace path relative to an open
// directory handle. The hook exists solely to make the validation-to-open
// race testable; production callers always pass nil.
func readWorkspaceFileWithBeforeOpen(ctx context.Context, rootDir, path string, maximum int, beforeOpen func()) ([]byte, error) {
	ctx = normalizeACPContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, fmt.Errorf("open workspace root: %w", err)
	}
	defer func() { _ = root.Close() }()

	relativePath, err := workspaceRelativePath(rootDir, path)
	if err != nil {
		return nil, err
	}
	expected, err := validatePinnedRootFile(root, relativePath, maximum)
	if err != nil {
		return nil, err
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	file, err := openPinnedReadOnly(root, relativePath)
	if err != nil {
		return nil, fmt.Errorf("open workspace file: %w", err)
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, fmt.Errorf("workspace path is not a regular file: %s", path)
	}
	if !os.SameFile(expected, opened) {
		return nil, fmt.Errorf("workspace file changed while opening: %s", path)
	}
	if opened.Size() > int64(maximum) {
		return nil, fmt.Errorf("file exceeds %d-byte read limit", maximum)
	}
	// A second component check catches an internal-link replacement after the
	// initial validation. SameFile above also protects the data read from a
	// transient replacement that is subsequently swapped back.
	if _, err := validatePinnedRootFile(root, relativePath, maximum); err != nil {
		return nil, err
	}

	data := make([]byte, 0, min(maximum, 32<<10))
	buffer := make([]byte, 32<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := file.Read(buffer)
		if n > 0 {
			if len(data)+n > maximum {
				return nil, fmt.Errorf("file exceeds %d-byte read limit", maximum)
			}
			data = append(data, buffer[:n]...)
		}
		if readErr == io.EOF {
			return data, nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

func normalizeACPContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// workspaceRelativePath translates an ACP path to a safe relative path for an
// already-pinned workspace root. It is intentionally lexical: resolving a
// symlink here would reintroduce a path-based time-of-check-time-of-use gap.
func workspaceRelativePath(rootDir, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return "", fmt.Errorf("file path is empty")
	}
	relativePath := requested
	if filepath.IsAbs(requested) {
		workspace, err := filepath.Abs(rootDir)
		if err != nil {
			return "", fmt.Errorf("resolve workspace root: %w", err)
		}
		relativePath, err = filepath.Rel(workspace, requested)
		if err != nil {
			return "", fmt.Errorf("resolve workspace file: %w", err)
		}
		if !safeRelativeWorkspacePath(relativePath) {
			// Preserve support for an absolute path spelled through a resolved
			// workspace-root alias (for example /private/var on macOS). This
			// only maps the root spelling; requested file symlinks are never
			// resolved outside the pinned-root operations below.
			resolvedWorkspace, resolveErr := filepath.EvalSymlinks(workspace)
			if resolveErr == nil {
				relativePath, err = filepath.Rel(resolvedWorkspace, requested)
				if err != nil {
					return "", fmt.Errorf("resolve workspace file: %w", err)
				}
			}
		}
	}
	relativePath = filepath.Clean(relativePath)
	if !safeRelativeWorkspacePath(relativePath) {
		return "", fmt.Errorf("file is outside workspace: %s", requested)
	}
	return relativePath, nil
}

func safeRelativeWorkspacePath(path string) bool {
	path = filepath.Clean(path)
	return path != "." && path != ".." && !strings.HasPrefix(path, ".."+string(filepath.Separator)) && !filepath.IsAbs(path)
}

// validatePinnedRootFile keeps ACP's existing policy of rejecting any symlink
// in a requested path and rejects non-regular or already-oversized files
// before opening them. Root.Open below remains the security boundary: even if
// a component is swapped after this check, it cannot resolve outside the
// pinned root.
func validatePinnedRootFile(root *os.Root, relativePath string, maximum int) (os.FileInfo, error) {
	componentPath := ""
	components := strings.Split(relativePath, string(filepath.Separator))
	for index, component := range components {
		componentPath = filepath.Join(componentPath, component)
		info, err := root.Lstat(componentPath)
		if err != nil {
			return nil, fmt.Errorf("stat workspace file: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("workspace file must not traverse a symlink: %s", relativePath)
		}
		if index == len(components)-1 {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("workspace path is not a regular file: %s", relativePath)
			}
			if info.Size() > int64(maximum) {
				return nil, fmt.Errorf("file exceeds %d-byte read limit", maximum)
			}
			return info, nil
		}
	}
	return nil, fmt.Errorf("workspace file path is empty")
}
