package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	log "github.com/charmbracelet/log"
	sdk "github.com/coder/acp-go-sdk"
)

// Manager manages the ACP agent subprocess and connection lifecycle.
type Manager struct {
	conn       *sdk.ClientSideConnection
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
		rootDir: rootDir,
		command: command,
		args:    args,
		msgChan: make(chan tea.Msg, 100),
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
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("agent already running")
	}
	m.mu.Unlock()

	// Check if command exists
	_, err := exec.LookPath(m.command)
	if err != nil {
		return fmt.Errorf("agent command %q not found: %w", m.command, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, m.command, m.args...)
	cmd.Dir = m.rootDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start agent: %w", err)
	}

	handler := newClientHandler(m.msgChan)
	conn := sdk.NewClientSideConnection(handler, stdin, stdout)

	// Initialize
	initResp, err := conn.Initialize(ctx, sdk.InitializeRequest{
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
		cancel()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("initialize: %w", err)
	}
	_ = initResp

	// Create session with MCP servers
	sessResp, err := conn.NewSession(ctx, sdk.NewSessionRequest{
		Cwd:        m.rootDir,
		McpServers: m.mcpServers,
	})
	if err != nil {
		cancel()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("new session: %w", err)
	}

	done := make(chan struct{})
	m.mu.Lock()
	m.conn = conn
	m.cmd = cmd
	m.handler = handler
	m.sessionID = sessResp.SessionId
	m.cancelFunc = cancel
	m.done = done
	m.running = true

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
	m.msgChan <- AgentSessionInfoMsg{
		SessionID:    m.sessionID,
		Models:       m.models,
		CurrentModel: m.currentModel,
		Modes:        m.modes,
		CurrentMode:  m.currentMode,
	}

	// Watch for process exit
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		close(done)
		m.msgChan <- AgentStoppedMsg{Err: err}
	}()

	m.msgChan <- AgentStartedMsg{}
	return nil
}

// TaggedFile represents a file to include as context in a prompt.
type TaggedFile struct {
	Path string
	Name string
}

// Prompt sends a user prompt to the agent. This blocks until the agent
// responds, so it must be called in a goroutine. Streaming updates flow
// through SessionUpdate notifications on the handler.
func (m *Manager) Prompt(text string, files []TaggedFile) tea.Cmd {
	m.mu.Lock()
	conn := m.conn
	sessionID := m.sessionID
	running := m.running
	m.mu.Unlock()

	if !running || conn == nil {
		return func() tea.Msg {
			return AgentPromptResponseMsg{Err: fmt.Errorf("agent not running")}
		}
	}

	return func() tea.Msg {
		blocks := []sdk.ContentBlock{sdk.TextBlock(text)}

		// Add tagged files as embedded resources
		for _, f := range files {
			absPath := f.Path
			if !filepath.IsAbs(absPath) {
				absPath = filepath.Join(m.rootDir, absPath)
			}
			data, err := os.ReadFile(absPath)
			if err != nil {
				continue
			}
			blocks = append(blocks, sdk.ResourceBlock(sdk.EmbeddedResourceResource{
				TextResourceContents: &sdk.TextResourceContents{
					Uri:  "file://" + absPath,
					Text: string(data),
				},
			}))
		}

		resp, err := conn.Prompt(context.Background(), sdk.PromptRequest{
			SessionId: sessionID,
			Prompt:    blocks,
		})
		if err != nil {
			return AgentPromptResponseMsg{Err: err}
		}
		return AgentPromptResponseMsg{StopReason: resp.StopReason}
	}
}

// SetModel changes the active model for the session.
func (m *Manager) SetModel(modelId sdk.ModelId) tea.Cmd {
	m.mu.Lock()
	conn := m.conn
	sessionID := m.sessionID
	running := m.running
	m.mu.Unlock()

	if !running || conn == nil {
		return nil
	}

	return func() tea.Msg {
		_, err := conn.SetSessionModel(context.Background(), sdk.SetSessionModelRequest{
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
	conn := m.conn
	sessionID := m.sessionID
	running := m.running
	m.mu.Unlock()

	if !running || conn == nil {
		return nil
	}

	return func() tea.Msg {
		_, err := conn.SetSessionMode(context.Background(), sdk.SetSessionModeRequest{
			SessionId: sessionID,
			ModeId:    modeId,
		})
		if err != nil {
			return AgentErrorMsg{Err: fmt.Errorf("set mode: %w", err)}
		}
		return AgentModeChangedMsg{ModeId: modeId}
	}
}

// Cancel sends a cancel notification for the current session.
func (m *Manager) Cancel() {
	m.mu.Lock()
	conn := m.conn
	sessionID := m.sessionID
	running := m.running
	m.mu.Unlock()

	if !running || conn == nil {
		return
	}

	conn.Cancel(context.Background(), sdk.CancelNotification{
		SessionId: sessionID,
	})
}

// Stop shuts down the agent subprocess gracefully.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancelFunc != nil {
		m.cancelFunc()
		m.cancelFunc = nil
	}
	done := m.done
	proc := m.cmd
	m.mu.Unlock()

	if proc != nil && proc.Process != nil {
		proc.Process.Kill()
		// Wait for the existing Start() goroutine to reap the process
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				log.Warn("acp: process wait timeout, may be zombie")
			}
		}
	}

	m.mu.Lock()
	m.running = false
	m.conn = nil
	m.mu.Unlock()
}

// loadMcpServers reads MCP server config from opencode config if available.
func (m *Manager) loadMcpServers() []sdk.McpServer {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp directory for CI environments
		home = os.TempDir()
		log.Info("acp: using temp directory as home", "path", home)
	}

	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Info("acp: no opencode config found", "path", configPath)
		return []sdk.McpServer{}
	}

	var ocConfig struct {
		MCP map[string]struct {
			Type    string   `json:"type"`
			Command []string `json:"command"`
			Enabled *bool    `json:"enabled"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(data, &ocConfig); err != nil {
		log.Error("acp: failed to parse opencode config", "err", err)
		return []sdk.McpServer{}
	}

	var servers []sdk.McpServer
	var skipped []string
	for name, cfg := range ocConfig.MCP {
		if cfg.Enabled != nil && !*cfg.Enabled {
			skipped = append(skipped, name+": disabled")
			continue
		}
		if len(cfg.Command) == 0 {
			skipped = append(skipped, name+": no command configured")
			continue
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

	return servers
}
