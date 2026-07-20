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
)

const msgChanTimeout = 100 * time.Millisecond

const (
	maxTerminalCount       = 8
	maxTerminalOutputBytes = 1 << 20
	maxAgentWriteBytes     = 16 << 20
	maxACPReadFileBytes    = 1 << 20
	maxACPReadLines        = 10_000
)

// ClientHandler implements sdk.Client. Its methods are called on the SDK's
// goroutine, so all data access is routed through the Bubbletea message loop
// via msgChan and blocking response channels.
type ClientHandler struct {
	msgChan chan<- tea.Msg
	rootDir string

	// Terminal management
	mu               sync.Mutex
	terminals        map[string]*terminalState
	nextTermID       int
	pendingTerminals int
}

type terminalState struct {
	sequence  int
	cmd       *exec.Cmd
	output    []byte
	truncated bool
	mu        sync.Mutex
	done      chan struct{}
	err       error
	exitCode  *int
}

// newClientHandler creates a handler restricted to rootDir. The optional form
// is retained for tests and defaults to the current working directory.
func newClientHandler(msgChan chan<- tea.Msg, roots ...string) *ClientHandler {
	rootDir, _ := os.Getwd()
	if len(roots) > 0 && roots[0] != "" {
		rootDir = roots[0]
	}
	return &ClientHandler{
		msgChan:   msgChan,
		rootDir:   rootDir,
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
func (h *ClientHandler) ReadTextFile(ctx context.Context, params sdk.ReadTextFileRequest) (sdk.ReadTextFileResponse, error) {
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
		return sdk.ReadTextFileResponse{}, ctx.Err()
	}

	select {
	case result := <-resultCh:
		if result.Err != nil {
			return sdk.ReadTextFileResponse{}, result.Err
		}
		return sdk.ReadTextFileResponse{Content: result.Content}, nil
	case <-ctx.Done():
		return sdk.ReadTextFileResponse{}, ctx.Err()
	}
}

// WriteTextFile sends a write proposal through the Bubbletea loop and blocks
// until the user accepts or rejects.
func (h *ClientHandler) WriteTextFile(ctx context.Context, params sdk.WriteTextFileRequest) (sdk.WriteTextFileResponse, error) {
	if len(params.Content) > maxAgentWriteBytes {
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
		return sdk.WriteTextFileResponse{}, ctx.Err()
	}

	select {
	case err := <-responseCh:
		if err != nil {
			return sdk.WriteTextFileResponse{}, err
		}
		return sdk.WriteTextFileResponse{}, nil
	case <-ctx.Done():
		h.sendNonBlocking(AgentWriteCancelledMsg{ResponseCh: responseCh})
		return sdk.WriteTextFileResponse{}, ctx.Err()
	}
}

// RequestPermission sends a permission prompt through the Bubbletea loop.
func (h *ClientHandler) RequestPermission(ctx context.Context, params sdk.RequestPermissionRequest) (sdk.RequestPermissionResponse, error) {
	responseCh := make(chan sdk.RequestPermissionResponse, 1)
	select {
	case h.msgChan <- AgentPermissionRequestMsg{
		ToolCall:   params.ToolCall,
		Options:    params.Options,
		ResponseCh: responseCh,
	}:
	case <-ctx.Done():
		return sdk.RequestPermissionResponse{}, ctx.Err()
	}

	select {
	case resp := <-responseCh:
		return resp, nil
	case <-ctx.Done():
		return sdk.RequestPermissionResponse{}, ctx.Err()
	}
}

// CreateTerminal spawns a subprocess and tracks it. Explicit command and
// working-directory paths are confined to the workspace; bare commands remain
// available through PATH so normal developer tooling (git, go, sh) still works.
func (h *ClientHandler) CreateTerminal(ctx context.Context, params sdk.CreateTerminalRequest) (sdk.CreateTerminalResponse, error) {
	if err := ctx.Err(); err != nil {
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
	outputLimit, err := terminalOutputLimit(params.OutputByteLimit)
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

	ts := &terminalState{
		sequence: sequence,
		done:     make(chan struct{}),
	}
	cmd := exec.CommandContext(ctx, command, params.Args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdout = terminalOutputWriter{state: ts, limit: outputLimit}
	cmd.Stderr = terminalOutputWriter{state: ts, limit: outputLimit}
	ts.cmd = cmd

	if err := cmd.Start(); err != nil {
		h.mu.Lock()
		h.pendingTerminals--
		h.mu.Unlock()
		return sdk.CreateTerminalResponse{}, fmt.Errorf("start terminal: %w", err)
	}

	go func() {
		err := cmd.Wait()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		ts.mu.Lock()
		ts.err = err
		ts.exitCode = &code
		ts.mu.Unlock()
		close(ts.done)
	}()

	h.mu.Lock()
	h.pendingTerminals--
	h.terminals[id] = ts
	h.mu.Unlock()

	return sdk.CreateTerminalResponse{TerminalId: id}, nil
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
		_ = ts.cmd.Process.Kill()
	}
	return sdk.KillTerminalCommandResponse{}, nil
}

// TerminalOutput returns captured stdout/stderr.
func (h *ClientHandler) TerminalOutput(ctx context.Context, params sdk.TerminalOutputRequest) (sdk.TerminalOutputResponse, error) {
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
		_ = ts.cmd.Process.Kill()
	}
	return sdk.ReleaseTerminalResponse{}, nil
}

// WaitForTerminalExit blocks until the terminal command exits or its context
// is cancelled.
func (h *ClientHandler) WaitForTerminalExit(ctx context.Context, params sdk.WaitForTerminalExitRequest) (sdk.WaitForTerminalExitResponse, error) {
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

func terminalEnvironment(requested []sdk.EnvVariable) ([]string, error) {
	env := make([]string, 0, len(requested)+1)
	seen := make(map[string]struct{}, len(requested))
	pathSet := false
	for _, variable := range requested {
		if !validEnvironmentName(variable.Name) || strings.ContainsRune(variable.Value, '\x00') {
			return nil, fmt.Errorf("invalid terminal environment variable %q", variable.Name)
		}
		if _, exists := seen[variable.Name]; exists {
			return nil, fmt.Errorf("duplicate terminal environment variable %q", variable.Name)
		}
		seen[variable.Name] = struct{}{}
		if variable.Name == "PATH" {
			pathSet = true
		}
		env = append(env, variable.Name+"="+variable.Value)
	}
	if !pathSet {
		if path := os.Getenv("PATH"); path != "" {
			env = append(env, "PATH="+path)
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
		resolved, err := exec.LookPath(command)
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
