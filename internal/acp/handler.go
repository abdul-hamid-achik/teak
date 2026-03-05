package acp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	log "github.com/charmbracelet/log"
	sdk "github.com/coder/acp-go-sdk"
)

const msgChanTimeout = 100 * time.Millisecond

// ClientHandler implements sdk.Client. Its methods are called on the SDK's
// goroutine, so all data access is routed through the Bubbletea message loop
// via msgChan and blocking response channels.
type ClientHandler struct {
	msgChan chan<- tea.Msg

	// Terminal management
	mu         sync.Mutex
	terminals  map[string]*terminalState
	nextTermID int
}

type terminalState struct {
	cmd    *exec.Cmd
	output bytes.Buffer
	mu     sync.Mutex
	done   chan struct{}
	err    error
}

func newClientHandler(msgChan chan<- tea.Msg) *ClientHandler {
	return &ClientHandler{
		msgChan:   msgChan,
		terminals: make(map[string]*terminalState),
	}
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

// SessionUpdate receives streaming updates from the agent and dispatches
// them as typed Bubbletea messages.
func (h *ClientHandler) SessionUpdate(_ context.Context, params sdk.SessionNotification) error {
	u := params.Update

	switch {
	case u.AgentMessageChunk != nil:
		text := extractText(u.AgentMessageChunk.Content)
		if text != "" {
			h.sendNonBlocking(AgentTextMsg{Text: text})
		}

	case u.AgentThoughtChunk != nil:
		text := extractText(u.AgentThoughtChunk.Content)
		if text != "" {
			h.sendNonBlocking(AgentThoughtMsg{Text: text})
		}

	case u.ToolCall != nil:
		h.sendNonBlocking(AgentToolCallMsg{
			ID:        u.ToolCall.ToolCallId,
			Title:     u.ToolCall.Title,
			Kind:      u.ToolCall.Kind,
			Status:    u.ToolCall.Status,
			Locations: u.ToolCall.Locations,
			Content:   u.ToolCall.Content,
		})

	case u.ToolCallUpdate != nil:
		h.sendNonBlocking(AgentToolCallUpdateMsg{
			ID:        u.ToolCallUpdate.ToolCallId,
			Title:     u.ToolCallUpdate.Title,
			Kind:      u.ToolCallUpdate.Kind,
			Status:    u.ToolCallUpdate.Status,
			Content:   u.ToolCallUpdate.Content,
			Locations: u.ToolCallUpdate.Locations,
		})

	case u.Plan != nil:
		h.sendNonBlocking(AgentPlanMsg{Entries: u.Plan.Entries})
	}

	return nil
}

// ReadTextFile sends a read request through the Bubbletea loop and blocks
// until the result is available.
func (h *ClientHandler) ReadTextFile(_ context.Context, params sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
	resultCh := make(chan FileReadResult, 1)
	h.msgChan <- FileReadRequestMsg{
		Path:     params.Path,
		Line:     params.Line,
		Limit:    params.Limit,
		ResultCh: resultCh,
	}

	result := <-resultCh
	if result.Err != nil {
		return sdk.ReadTextFileResponse{}, result.Err
	}
	return sdk.ReadTextFileResponse{Content: result.Content}, nil
}

// WriteTextFile sends a write proposal through the Bubbletea loop and blocks
// until the user accepts or rejects.
func (h *ClientHandler) WriteTextFile(_ context.Context, params sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	responseCh := make(chan error, 1)
	h.msgChan <- AgentWriteFileMsg{
		Path:       params.Path,
		Content:    params.Content,
		ResponseCh: responseCh,
	}

	if err := <-responseCh; err != nil {
		return sdk.WriteTextFileResponse{}, err
	}
	return sdk.WriteTextFileResponse{}, nil
}

// RequestPermission sends a permission prompt through the Bubbletea loop.
func (h *ClientHandler) RequestPermission(_ context.Context, params sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	responseCh := make(chan sdk.RequestPermissionResponse, 1)
	h.msgChan <- AgentPermissionRequestMsg{
		ToolCall:   params.ToolCall,
		Options:    params.Options,
		ResponseCh: responseCh,
	}

	resp := <-responseCh
	return resp, nil
}

// CreateTerminal spawns a subprocess and tracks it.
func (h *ClientHandler) CreateTerminal(_ context.Context, params sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	h.mu.Lock()
	h.nextTermID++
	id := fmt.Sprintf("term-%d", h.nextTermID)
	h.mu.Unlock()

	cmd := exec.Command(params.Command, params.Args...)
	if params.Cwd != nil {
		cmd.Dir = *params.Cwd
	}
	for _, ev := range params.Env {
		cmd.Env = append(cmd.Env, ev.Name+"="+ev.Value)
	}
	if len(cmd.Env) > 0 {
		cmd.Env = append(os.Environ(), cmd.Env...)
	}

	ts := &terminalState{
		cmd:  cmd,
		done: make(chan struct{}),
	}
	cmd.Stdout = &ts.output
	cmd.Stderr = &ts.output

	if err := cmd.Start(); err != nil {
		return sdk.CreateTerminalResponse{}, fmt.Errorf("start terminal: %w", err)
	}

	go func() {
		ts.err = cmd.Wait()
		close(ts.done)
	}()

	h.mu.Lock()
	h.terminals[id] = ts
	h.mu.Unlock()

	return sdk.CreateTerminalResponse{TerminalId: id}, nil
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
		ts.cmd.Process.Kill()
	}
	return sdk.KillTerminalCommandResponse{}, nil
}

// TerminalOutput returns captured stdout/stderr.
func (h *ClientHandler) TerminalOutput(_ context.Context, params sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
	h.mu.Lock()
	ts, ok := h.terminals[params.TerminalId]
	h.mu.Unlock()
	if !ok {
		return sdk.TerminalOutputResponse{}, fmt.Errorf("unknown terminal: %s", params.TerminalId)
	}

	ts.mu.Lock()
	output := ts.output.String()
	ts.mu.Unlock()

	var exitStatus *sdk.TerminalExitStatus
	select {
	case <-ts.done:
		code := 0
		if ts.cmd.ProcessState != nil {
			code = ts.cmd.ProcessState.ExitCode()
		}
		exitStatus = &sdk.TerminalExitStatus{ExitCode: &code}
	default:
	}

	resp := sdk.TerminalOutputResponse{
		Output:     output,
		ExitStatus: exitStatus,
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
		ts.cmd.Process.Kill()
	}
	return sdk.ReleaseTerminalResponse{}, nil
}

// WaitForTerminalExit blocks until the terminal command exits.
func (h *ClientHandler) WaitForTerminalExit(_ context.Context, params sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
	h.mu.Lock()
	ts, ok := h.terminals[params.TerminalId]
	h.mu.Unlock()
	if !ok {
		return sdk.WaitForTerminalExitResponse{}, fmt.Errorf("unknown terminal: %s", params.TerminalId)
	}

	<-ts.done

	code := 0
	if ts.cmd.ProcessState != nil {
		code = ts.cmd.ProcessState.ExitCode()
	}
	return sdk.WaitForTerminalExitResponse{
		ExitCode: &code,
	}, nil
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

// readFileFromDisk reads a file with optional line/limit filtering.
// Line numbers are 1-based (ACP convention).
func ReadFileFromDisk(path string, line *int, limit *int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)

	if line == nil && limit == nil {
		return content, nil
	}

	lines := strings.Split(content, "\n")

	startLine := 0
	if line != nil {
		startLine = *line - 1 // convert 1-based to 0-based
		if startLine < 0 {
			startLine = 0
		}
		if startLine >= len(lines) {
			return "", nil
		}
	}

	endLine := len(lines)
	if limit != nil {
		endLine = startLine + *limit
		if endLine > len(lines) {
			endLine = len(lines)
		}
	}

	return strings.Join(lines[startLine:endLine], "\n"), nil
}
