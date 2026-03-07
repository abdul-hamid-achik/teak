package app

import (
	"teak/internal/acp"
	"teak/internal/config"
	"teak/internal/dap"
	"teak/internal/lsp"
	"teak/internal/ui"
)

// ProtocolManager handles LSP, DAP, and ACP protocol managers.
type ProtocolManager struct {
	// Protocol managers
	lspMgr   *lsp.Manager
	debugMgr *dap.Manager
	acpMgr   *acp.Manager

	// Coordinator (orchestrates LSP/DAP/ACP)
	coordinator *Coordinator

	// Debug state
	breakpoints     map[string][]breakpointEntry
	currentExecFile string
	currentExecLine int

	// Git info
	gitBranch string

	// Config and paths
	appCfg  config.Config
	rootDir string
}

// NewProtocolManager creates a new protocol manager.
func NewProtocolManager(rootDir string, cfg config.Config, theme ui.Theme) *ProtocolManager {
	// Build LSP configs from app config
	var lspConfigs []lsp.ServerConfig
	for _, lc := range cfg.LSP {
		lspConfigs = append(lspConfigs, lsp.ServerConfig{
			Extensions: lc.Extensions,
			Command:    lc.Command,
			Args:       lc.Args,
			LanguageID: lc.LanguageID,
		})
	}

	pm := &ProtocolManager{
		lspMgr:          lsp.NewManager(rootDir, lspConfigs),
		breakpoints:     make(map[string][]breakpointEntry),
		currentExecLine: -1,
		appCfg:          cfg,
		rootDir:         rootDir,
	}

	// Initialize ACP if enabled
	if cfg.Agent.Enabled && cfg.Agent.Command != "" {
		pm.acpMgr = acp.NewManager(rootDir, cfg.Agent.Command, cfg.Agent.Args)
	}

	return pm
}

// ============== LSP ==============

// GetLSPManager returns the LSP manager.
func (pm *ProtocolManager) GetLSPManager() *lsp.Manager {
	return pm.lspMgr
}

// ============== DAP ==============

// GetDAPManager returns the DAP manager, initializing if needed.
func (pm *ProtocolManager) GetDAPManager() *dap.Manager {
	if pm.debugMgr == nil {
		pm.debugMgr = dap.NewManager(pm.rootDir)
	}
	return pm.debugMgr
}

// HasDAPManager returns whether DAP manager is initialized.
func (pm *ProtocolManager) HasDAPManager() bool {
	return pm.debugMgr != nil
}

// ============== ACP ==============

// GetACPManager returns the ACP manager.
func (pm *ProtocolManager) GetACPManager() *acp.Manager {
	return pm.acpMgr
}

// IsACPEnabled returns whether ACP is enabled.
func (pm *ProtocolManager) IsACPEnabled() bool {
	return pm.acpMgr != nil
}

// ============== Coordinator ==============

// SetCoordinator sets the coordinator.
func (pm *ProtocolManager) SetCoordinator(c *Coordinator) {
	pm.coordinator = c
}

// GetCoordinator returns the coordinator.
func (pm *ProtocolManager) GetCoordinator() *Coordinator {
	return pm.coordinator
}

// HasCoordinator returns whether coordinator is set.
func (pm *ProtocolManager) HasCoordinator() bool {
	return pm.coordinator != nil
}

// ShutdownCoordinator shuts down the coordinator.
func (pm *ProtocolManager) ShutdownCoordinator() {
	if pm.coordinator != nil {
		pm.coordinator.Shutdown()
		pm.coordinator = nil
	}
}

// ============== Breakpoints ==============

// SetBreakpoint sets a breakpoint at the given file and line.
func (pm *ProtocolManager) SetBreakpoint(file string, line int) {
	entries := pm.breakpoints[file]

	// Check if already exists
	for i, e := range entries {
		if e.Line == line {
			// Toggle enable/disable
			entries[i].Enabled = !entries[i].Enabled
			pm.breakpoints[file] = entries
			return
		}
	}

	// Add new breakpoint
	entries = append(entries, breakpointEntry{
		Line:    line,
		Enabled: true,
	})
	pm.breakpoints[file] = entries
}

// ClearBreakpoint removes a breakpoint.
func (pm *ProtocolManager) ClearBreakpoint(file string, line int) {
	entries := pm.breakpoints[file]
	for i, e := range entries {
		if e.Line == line {
			// Remove this entry
			entries = append(entries[:i], entries[i+1:]...)
			if len(entries) == 0 {
				delete(pm.breakpoints, file)
			} else {
				pm.breakpoints[file] = entries
			}
			return
		}
	}
}

// ClearAllBreakpoints removes all breakpoints.
func (pm *ProtocolManager) ClearAllBreakpoints() {
	pm.breakpoints = make(map[string][]breakpointEntry)
}

// HasBreakpoint returns whether a breakpoint exists at the given location.
func (pm *ProtocolManager) HasBreakpoint(file string, line int) bool {
	entries := pm.breakpoints[file]
	for _, e := range entries {
		if e.Line == line {
			return true
		}
	}
	return false
}

// IsBreakpointEnabled returns whether a breakpoint is enabled.
func (pm *ProtocolManager) IsBreakpointEnabled(file string, line int) bool {
	entries := pm.breakpoints[file]
	for _, e := range entries {
		if e.Line == line {
			return e.Enabled
		}
	}
	return false
}

// GetBreakpoints returns all breakpoints for a file.
func (pm *ProtocolManager) GetBreakpoints(file string) []breakpointEntry {
	return pm.breakpoints[file]
}

// GetAllBreakpoints returns all breakpoints.
func (pm *ProtocolManager) GetAllBreakpoints() map[string][]breakpointEntry {
	return pm.breakpoints
}

// ToggleBreakpoint toggles a breakpoint at the given location.
func (pm *ProtocolManager) ToggleBreakpoint(file string, line int) {
	if pm.HasBreakpoint(file, line) {
		pm.ClearBreakpoint(file, line)
	} else {
		pm.SetBreakpoint(file, line)
	}
}

// ============== Execution State ==============

// SetExecutionPoint sets the current execution point.
func (pm *ProtocolManager) SetExecutionPoint(file string, line int) {
	pm.currentExecFile = file
	pm.currentExecLine = line
}

// ClearExecutionPoint clears the current execution point.
func (pm *ProtocolManager) ClearExecutionPoint() {
	pm.currentExecFile = ""
	pm.currentExecLine = -1
}

// GetExecutionPoint returns the current execution file and line.
func (pm *ProtocolManager) GetExecutionPoint() (file string, line int) {
	return pm.currentExecFile, pm.currentExecLine
}

// HasExecutionPoint returns whether there's a current execution point.
func (pm *ProtocolManager) HasExecutionPoint() bool {
	return pm.currentExecLine >= 0
}

// IsAtExecutionPoint returns whether the given location is the execution point.
func (pm *ProtocolManager) IsAtExecutionPoint(file string, line int) bool {
	return pm.currentExecFile == file && pm.currentExecLine == line
}

// ============== Git Info ==============

// GetGitBranch returns the current git branch.
func (pm *ProtocolManager) GetGitBranch() string {
	return pm.gitBranch
}

// SetGitBranch sets the current git branch.
func (pm *ProtocolManager) SetGitBranch(branch string) {
	pm.gitBranch = branch
}

// ============== Config ==============

// GetConfig returns the app config.
func (pm *ProtocolManager) GetConfig() config.Config {
	return pm.appCfg
}

// ============== Lifecycle ==============

// Shutdown shuts down all protocol managers.
func (pm *ProtocolManager) Shutdown() {
	if pm.coordinator != nil {
		pm.coordinator.Shutdown()
	}
	// Note: Individual managers don't have Shutdown methods currently
	// This is handled by the coordinator or process termination
}
