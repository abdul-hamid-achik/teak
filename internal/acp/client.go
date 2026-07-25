package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	log "github.com/charmbracelet/log"
	sdk "github.com/coder/acp-go-sdk"

	"teak/internal/toolpath"
)

const (
	acpStartupTimeout       = 10 * time.Second
	acpShutdownTimeout      = 2 * time.Second
	acpCancelTimeout        = 750 * time.Millisecond
	acpSessionChangeTimeout = 5 * time.Second
	acpPromptTimeout        = 10 * time.Minute
	maxTaggedFiles          = 32
	maxTaggedFileBytes      = 256 << 10
	maxTaggedTotalBytes     = 2 << 20
	maxPromptBytes          = 256 << 10
	maxOpenCodeConfigBytes  = 1 << 20
	maxOpenCodeMCPServers   = 64
	maxOpenCodeCommandArgs  = 64
	maxOpenCodeStringBytes  = 4 << 10
)

// sessionController is intentionally small so session model/mode requests can
// be bounded and tested without starting an ACP subprocess.
type sessionController interface {
	SetSessionModel(context.Context, sdk.SetSessionModelRequest) (sdk.SetSessionModelResponse, error)
	SetSessionMode(context.Context, sdk.SetSessionModeRequest) (sdk.SetSessionModeResponse, error)
}

// Manager manages the ACP agent subprocess and connection lifecycle.
type Manager struct {
	conn       *sdk.ClientSideConnection
	sessionCtl sessionController
	cmd        *exec.Cmd
	handler    *ClientHandler
	msgChan    chan tea.Msg
	sessionID  sdk.SessionId
	rootDir    string
	command    string
	args       []string
	mu         sync.Mutex
	running    bool
	cancelFunc context.CancelFunc
	done       chan struct{} // closed when process exits
	starting   bool

	// Exactly one prompt may be in flight for a session. Its context is
	// canceled immediately by Cancel and Stop, independently of the ACP cancel
	// notification's bounded network write.
	promptActive     bool
	promptGeneration uint64
	promptCancel     context.CancelFunc

	lifecycle        int
	lifecycleChanged chan struct{}
	trackedDone      map[chan struct{}]struct{}

	// Session state
	models       []sdk.ModelInfo
	currentModel sdk.ModelId
	modes        []sdk.SessionMode
	currentMode  sdk.SessionModeId

	// MCP servers to pass through
	mcpServers []sdk.McpServer
}

// NewManager creates a new ACP manager. Does not start the subprocess.
func NewManager(rootDir, command string, args []string) *Manager {
	mgr := &Manager{
		rootDir:          rootDir,
		command:          command,
		args:             args,
		msgChan:          make(chan tea.Msg, 100),
		lifecycleChanged: make(chan struct{}),
		trackedDone:      make(map[chan struct{}]struct{}),
	}
	mgr.mcpServers = mgr.loadMcpServers()
	return mgr
}

// MsgChan returns the channel for receiving ACP messages.
func (m *Manager) MsgChan() <-chan tea.Msg {
	return m.msgChan
}

// IsRunning returns whether the agent process is active.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Start spawns the agent subprocess, initializes the connection,
// and creates a session.
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.running || m.starting {
		m.mu.Unlock()
		return fmt.Errorf("agent already running")
	}
	m.starting = true
	m.beginLifecycleLocked()
	m.mu.Unlock()
	defer m.endLifecycle()
	failBeforeProcess := func() {
		m.mu.Lock()
		if !m.running {
			m.starting = false
		}
		m.mu.Unlock()
	}

	// Resolve through toolpath so an agent CLI installed under a version
	// manager or Homebrew is found even when Teak inherited a PATH without it.
	resolved, err := toolpath.Resolve(m.command)
	if err != nil {
		failBeforeProcess()
		return fmt.Errorf("agent command %q not found: %w", m.command, err)
	}

	processCtx, cancelProcess := context.WithCancel(context.Background())
	startupCtx, cancelStartup := context.WithTimeout(processCtx, acpStartupTimeout)
	defer cancelStartup()

	cmd := exec.CommandContext(processCtx, resolved, m.args...)
	cmd.Dir = m.rootDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancelProcess()
		failBeforeProcess()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancelProcess()
		failBeforeProcess()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancelProcess()
		failBeforeProcess()
		return fmt.Errorf("start agent: %w", err)
	}

	done := make(chan struct{})
	m.mu.Lock()
	m.trackDoneLocked(done)
	// Stop may have been called while LookPath/pipe setup was running.
	if !m.starting || m.running {
		m.mu.Unlock()
		cancelProcess()
		_ = cmd.Process.Kill()
		go reapACPProcess(cmd, done, nil)
		return fmt.Errorf("agent lifecycle changed while starting")
	}
	m.cmd = cmd
	m.cancelFunc = cancelProcess
	m.done = done
	m.starting = true
	m.mu.Unlock()
	go reapACPProcess(cmd, done, m)

	handler := newClientHandler(m.msgChan, m.rootDir)
	conn := sdk.NewClientSideConnection(handler, stdin, stdout)

	// Initialize
	initResp, err := conn.Initialize(startupCtx, sdk.InitializeRequest{
		ProtocolVersion: sdk.ProtocolVersion(sdk.ProtocolVersionNumber),
		ClientInfo: &sdk.Implementation{
			Name:    "teak",
			Version: "1.0.0",
		},
		ClientCapabilities: sdk.ClientCapabilities{
			Fs: sdk.FileSystemCapability{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
	})
	if err != nil {
		m.abortStart(cmd)
		return fmt.Errorf("initialize: %w", err)
	}
	_ = initResp

	// Create session with MCP servers
	sessResp, err := conn.NewSession(startupCtx, sdk.NewSessionRequest{
		Cwd:        m.rootDir,
		McpServers: m.mcpServers,
	})
	if err != nil {
		m.abortStart(cmd)
		return fmt.Errorf("new session: %w", err)
	}

	m.mu.Lock()
	if m.cmd != cmd || !m.starting || startupCtx.Err() != nil {
		m.mu.Unlock()
		m.abortStart(cmd)
		return fmt.Errorf("agent start canceled")
	}
	m.conn = conn
	m.sessionCtl = conn
	m.handler = handler
	m.sessionID = sessResp.SessionId
	m.running = true
	m.starting = false

	// Parse session model/mode state
	if sessResp.Models != nil {
		m.models = sessResp.Models.AvailableModels
		m.currentModel = sessResp.Models.CurrentModelId
	}
	if sessResp.Modes != nil {
		m.modes = sessResp.Modes.AvailableModes
		m.currentMode = sessResp.Modes.CurrentModeId
	}
	m.mu.Unlock()

	// Send session info to panel
	m.emit(AgentSessionInfoMsg{
		SessionID:    m.sessionID,
		Models:       m.models,
		CurrentModel: m.currentModel,
		Modes:        m.modes,
		CurrentMode:  m.currentMode,
	})
	m.emit(AgentStartedMsg{})
	return nil
}

// TaggedFile represents a file to include as context in a prompt.
type TaggedFile struct {
	Path string
	Name string
}

// Prompt starts one cancellable prompt. The returned command blocks until the
// agent responds, so Bubble Tea runs it asynchronously. Only one prompt can
// be active per ACP session; this avoids response interleaving and stale UI
// state when a user cancels then immediately retries.
func (m *Manager) Prompt(text string, files []TaggedFile) tea.Cmd {
	m.mu.Lock()
	conn := m.conn
	sessionID := m.sessionID
	running := m.running
	if !running || conn == nil {
		m.mu.Unlock()
		return func() tea.Msg {
			return AgentPromptResponseMsg{Err: fmt.Errorf("agent not running")}
		}
	}
	if m.promptActive {
		m.mu.Unlock()
		return func() tea.Msg {
			return AgentErrorMsg{Err: fmt.Errorf("another agent prompt is still running")}
		}
	}
	ctx, generation, err := m.startPromptLocked()
	if err != nil {
		m.mu.Unlock()
		return func() tea.Msg { return AgentPromptResponseMsg{Err: err} }
	}
	rootDir := m.rootDir
	m.mu.Unlock()

	return func() tea.Msg {
		defer m.finishPrompt(generation)
		if len(text) > maxPromptBytes {
			return AgentPromptResponseMsg{Generation: generation, Err: fmt.Errorf("prompt exceeds %d-byte limit", maxPromptBytes)}
		}
		if err := ctx.Err(); err != nil {
			return AgentPromptResponseMsg{Generation: generation, Err: err}
		}
		blocks, err := buildTaggedFileBlocks(ctx, rootDir, files)
		if err != nil {
			return AgentPromptResponseMsg{Generation: generation, Err: err}
		}
		blocks = append([]sdk.ContentBlock{sdk.TextBlock(text)}, blocks...)

		resp, err := conn.Prompt(ctx, sdk.PromptRequest{
			SessionId: sessionID,
			Prompt:    blocks,
		})
		if err != nil {
			return AgentPromptResponseMsg{Generation: generation, Err: err}
		}
		if err := ctx.Err(); err != nil {
			return AgentPromptResponseMsg{Generation: generation, Err: err}
		}
		return AgentPromptResponseMsg{Generation: generation, StopReason: resp.StopReason}
	}
}

func (m *Manager) beginPrompt() (context.Context, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startPromptLocked()
}

func (m *Manager) startPromptLocked() (context.Context, uint64, error) {
	if !m.running {
		return nil, 0, fmt.Errorf("agent not running")
	}
	if m.promptActive {
		return nil, 0, fmt.Errorf("another agent prompt is still running")
	}
	m.promptActive = true
	m.promptGeneration++
	ctx, cancel := context.WithTimeout(context.Background(), acpPromptTimeout)
	m.promptCancel = cancel
	return ctx, m.promptGeneration, nil
}

func (m *Manager) finishPrompt(generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.promptActive || m.promptGeneration != generation {
		return
	}
	m.promptActive = false
	if m.promptCancel != nil {
		m.promptCancel()
	}
	m.promptCancel = nil
}

// IsCurrentPromptGeneration reports whether a prompt response still belongs
// to the newest prompt started for this manager. The UI uses it to discard a
// delayed response if a newer prompt was started before Bubble Tea processed
// the old command result.
func (m *Manager) IsCurrentPromptGeneration(generation uint64) bool {
	if generation == 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return generation == m.promptGeneration
}

func (m *Manager) cancelCurrentPromptLocked() {
	if m.promptCancel != nil {
		m.promptCancel()
	}
}

func buildTaggedFileBlocks(ctx context.Context, rootDir string, files []TaggedFile) ([]sdk.ContentBlock, error) {
	if len(files) > maxTaggedFiles {
		return nil, fmt.Errorf("too many tagged files: %d (limit %d)", len(files), maxTaggedFiles)
	}
	blocks := make([]sdk.ContentBlock, 0, len(files))
	total := 0
	seen := make(map[string]struct{}, len(files))
	for _, tagged := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resolved, err := ResolveWorkspaceFile(rootDir, tagged.Path)
		if err != nil {
			return nil, fmt.Errorf("tagged file %q: %w", tagged.Path, err)
		}
		if _, exists := seen[resolved]; exists {
			continue
		}
		// ResolveWorkspaceFile supplies the canonical URI and duplicate key. Read
		// through a pinned workspace root so a path swap after that validation
		// cannot redirect the content outside the workspace.
		data, err := readWorkspaceFile(ctx, rootDir, tagged.Path, maxTaggedFileBytes)
		if err != nil {
			return nil, fmt.Errorf("read tagged file %q: %w", tagged.Path, err)
		}
		if total+len(data) > maxTaggedTotalBytes {
			return nil, fmt.Errorf("tagged files exceed %d-byte total limit", maxTaggedTotalBytes)
		}
		seen[resolved] = struct{}{}
		total += len(data)
		uri := (&url.URL{Scheme: "file", Path: resolved}).String()
		blocks = append(blocks, sdk.ResourceBlock(sdk.EmbeddedResourceResource{
			TextResourceContents: &sdk.TextResourceContents{
				Uri:  uri,
				Text: string(data),
			},
		}))
	}
	return blocks, nil
}

// SetModel changes the active model for the session.
func (m *Manager) SetModel(modelId sdk.ModelId) tea.Cmd {
	m.mu.Lock()
	controller := m.sessionCtl
	sessionID := m.sessionID
	running := m.running
	done := m.done
	m.mu.Unlock()

	if !running || controller == nil {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := boundedACPRequestContext(done, acpSessionChangeTimeout)
		defer cancel()
		_, err := controller.SetSessionModel(ctx, sdk.SetSessionModelRequest{
			SessionId: sessionID,
			ModelId:   modelId,
		})
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("set model: %w", err)}
		}
		return AgentModelChangedMsg{ModelId: modelId}
	}
}

// SetMode changes the active mode for the session.
func (m *Manager) SetMode(modeId sdk.SessionModeId) tea.Cmd {
	m.mu.Lock()
	controller := m.sessionCtl
	sessionID := m.sessionID
	running := m.running
	done := m.done
	m.mu.Unlock()

	if !running || controller == nil {
		return nil
	}

	return func() tea.Msg {
		ctx, cancel := boundedACPRequestContext(done, acpSessionChangeTimeout)
		defer cancel()
		_, err := controller.SetSessionMode(ctx, sdk.SetSessionModeRequest{
			SessionId: sessionID,
			ModeId:    modeId,
		})
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("set mode: %w", err)}
		}
		return AgentModeChangedMsg{ModeId: modeId}
	}
}

// boundedACPRequestContext ties a session-control RPC to both a short deadline
// and the lifetime of the subprocess it was issued against. The goroutine exits
// when either condition completes, including on a successful RPC via cancel.
func boundedACPRequestContext(done <-chan struct{}, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	if done == nil {
		return ctx, cancel
	}
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// Cancel sends a cancel notification for the current session.
func (m *Manager) Cancel() {
	m.mu.Lock()
	conn := m.conn
	sessionID := m.sessionID
	running := m.running
	m.cancelCurrentPromptLocked()
	m.mu.Unlock()

	if !running || conn == nil {
		return
	}

	// The UI path returns immediately after canceling the local prompt context.
	// The protocol notification is best effort and independently bounded.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), acpCancelTimeout)
		defer cancel()
		if err := conn.Cancel(ctx, sdk.CancelNotification{
			SessionId: sessionID,
		}); err != nil {
			log.Error("acp: cancel failed", "err", err)
		}
	}()
}

// Stop shuts down the agent subprocess gracefully.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.cancelCurrentPromptLocked()
	cancel := m.cancelFunc
	m.cancelFunc = nil
	done := m.done
	proc := m.cmd
	m.running = false
	m.starting = false
	m.conn = nil
	m.sessionCtl = nil
	m.trackDoneLocked(done)
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if proc != nil && proc.Process != nil {
		go func() {
			_ = proc.Process.Kill()
			if !waitACPDone(done, acpShutdownTimeout) {
				log.Warn("acp: process did not exit after forced shutdown")
			}
		}()
	}
}

// WaitForShutdown initiates shutdown and waits for in-flight startup work and
// every tracked agent process to be reaped, or until ctx is cancelled.
func (m *Manager) WaitForShutdown(ctx context.Context) bool {
	m.Stop()
	for {
		m.mu.Lock()
		if m.lifecycle == 0 {
			m.mu.Unlock()
			return true
		}
		changed := m.lifecycleChanged
		m.mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return false
		}
	}
}

func (m *Manager) beginLifecycleLocked() {
	m.lifecycle++
}

func (m *Manager) signalLifecycleLocked() {
	close(m.lifecycleChanged)
	m.lifecycleChanged = make(chan struct{})
}

func (m *Manager) endLifecycle() {
	m.mu.Lock()
	if m.lifecycle > 0 {
		m.lifecycle--
	}
	m.signalLifecycleLocked()
	m.mu.Unlock()
}

func (m *Manager) trackDoneLocked(done chan struct{}) {
	if done == nil {
		return
	}
	if _, ok := m.trackedDone[done]; ok {
		return
	}
	m.trackedDone[done] = struct{}{}
	m.beginLifecycleLocked()
	go func() {
		<-done
		m.mu.Lock()
		delete(m.trackedDone, done)
		if m.lifecycle > 0 {
			m.lifecycle--
		}
		m.signalLifecycleLocked()
		m.mu.Unlock()
	}()
}

func (m *Manager) abortStart(cmd *exec.Cmd) {
	m.mu.Lock()
	if m.cmd == cmd {
		cancel := m.cancelFunc
		m.cancelFunc = nil
		m.running = false
		m.starting = false
		m.conn = nil
		m.sessionCtl = nil
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return
	}
	m.mu.Unlock()
}

func reapACPProcess(cmd *exec.Cmd, done chan struct{}, m *Manager) {
	err := cmd.Wait()
	close(done)
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.cmd == cmd {
		m.running = false
		m.starting = false
		m.conn = nil
		m.sessionCtl = nil
	}
	m.mu.Unlock()
	m.emit(AgentStoppedMsg{Err: err})
}

func waitACPDone(done <-chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// emit isolates protocol goroutines from a stalled or already-closing TUI.
func (m *Manager) emit(msg tea.Msg) {
	if m.msgChan == nil {
		return
	}
	select {
	case m.msgChan <- msg:
	default:
		log.Warn("acp: dropping UI message because message queue is full")
	}
}

// loadMcpServers reads MCP server config from opencode config if available.
func (m *Manager) loadMcpServers() []sdk.McpServer {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp directory for CI environments
		home = os.TempDir()
		log.Info("acp: using temp directory as home", "path", home)
	}

	configHome := filepath.Join(home, ".config")
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		configHome = dir
	}
	configPath := filepath.Join(configHome, "opencode", "opencode.json")
	servers, err := loadMcpServersFromPath(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info("acp: no opencode config found", "path", configPath)
		} else {
			log.Warn("acp: ignored invalid opencode config", "path", configPath, "err", err)
		}
		return []sdk.McpServer{}
	}
	return servers
}

func loadMcpServersFromPath(configPath string) ([]sdk.McpServer, error) {
	data, err := readOpenCodeConfig(configPath)
	if err != nil {
		return nil, err
	}

	var ocConfig struct {
		MCP map[string]struct {
			Type    string   `json:"type"`
			Command []string `json:"command"`
			Enabled *bool    `json:"enabled"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(data, &ocConfig); err != nil {
		return nil, fmt.Errorf("parse opencode config: %w", err)
	}
	if len(ocConfig.MCP) > maxOpenCodeMCPServers {
		return nil, fmt.Errorf("opencode config has %d MCP servers; limit is %d", len(ocConfig.MCP), maxOpenCodeMCPServers)
	}

	var servers []sdk.McpServer
	var skipped []string
	for name, cfg := range ocConfig.MCP {
		if err := validateOpenCodeString("MCP server name", name); err != nil {
			return nil, err
		}
		if cfg.Enabled != nil && !*cfg.Enabled {
			skipped = append(skipped, name+": disabled")
			continue
		}
		if len(cfg.Command) == 0 {
			skipped = append(skipped, name+": no command configured")
			continue
		}
		if len(cfg.Command) > maxOpenCodeCommandArgs {
			return nil, fmt.Errorf("MCP server %q has too many command arguments", name)
		}
		for _, part := range cfg.Command {
			if err := validateOpenCodeString("MCP command", part); err != nil {
				return nil, err
			}
		}
		cmd := cfg.Command[0]
		var args []string
		if len(cfg.Command) > 1 {
			args = cfg.Command[1:]
		}
		servers = append(servers, sdk.McpServer{
			Stdio: &sdk.McpServerStdio{
				Name:    name,
				Command: cmd,
				Args:    args,
				Env:     []sdk.EnvVariable{},
			},
		})
		log.Info("acp: loaded MCP server", "name", name, "command", cmd)
	}

	if len(skipped) > 0 {
		log.Warn("acp: skipped MCP servers", "count", len(skipped), "servers", skipped)
	}

	return servers, nil
}

func readOpenCodeConfig(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("opencode config is not a regular file")
	}
	if info.Size() > maxOpenCodeConfigBytes {
		return nil, fmt.Errorf("opencode config exceeds %d-byte limit", maxOpenCodeConfigBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxOpenCodeConfigBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxOpenCodeConfigBytes {
		return nil, fmt.Errorf("opencode config exceeds %d-byte limit", maxOpenCodeConfigBytes)
	}
	return data, nil
}

func validateOpenCodeString(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if len(value) > maxOpenCodeStringBytes || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}
