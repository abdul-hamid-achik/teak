# AGENTS.md — Teak Codebase Guide for AI Agents

## Project Overview

Teak is a terminal code editor written in Go 1.24+ using the Bubbletea v2 TUI framework. It follows the Elm architecture (Model-View-Update) with message passing for all state changes.

## Build & Test

```sh
go build -o bin/teak ./cmd/teak   # or: task build
go test ./...                      # or: task test
```

**All code must:**
- ✅ Compile without errors
- ✅ Pass all tests
- ✅ Include tests for new functionality
- ✅ Maintain or improve code coverage

## Code Conventions

### Core Principles

- **Go 1.24+** — use standard library where possible
- **0-based indexing** for lines and columns throughout the entire codebase
- **Immutable rope** — `Insert` and `Delete` on `*Rope` return a new `*Rope`, never mutate in place
- **Return errors, don't panic** — no `panic()`, `log.Fatal()`, or `log.Panic()` in library code
- **Logging** — use `github.com/charmbracelet/log` for structured logging. Prefer `log.Error()` with key-value pairs over `log.Printf()`.
- **Table-driven tests** in `_test.go` files alongside source
- **No unnecessary abstractions** — keep things simple, avoid premature generalization

### Testing Requirements

**Before implementing features:**
1. Write tests for the expected behavior
2. Run tests to confirm they fail (red-green-refactor)
3. Implement the feature
4. Verify tests pass

**Test coverage targets:**
- Core packages (`text/`, `editor/`): > 60%
- Integration packages (`lsp/`, `app/`): > 40%
- New code: Match or exceed existing coverage

**Test types:**
```go
// Unit tests — test individual functions
func TestRopeInsert(t *testing.T) { ... }

// Stress tests — test edge cases and performance
func TestRopeLargeFilePerformance(t *testing.T) { ... }

// Integration tests — test workflows
func TestFileOpenEditSaveWorkflow(t *testing.T) { ... }

// Benchmarks — track performance
func BenchmarkRopeInsert(b *testing.B) { ... }
```

### Thread Safety

**Use `sync.RWMutex` for read-heavy structures:**
```go
type Client struct {
    mu sync.RWMutex  // NOT sync.Mutex
}

// Read operations use RLock
func (c *Client) SupportsHover() bool {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.capabilities.HoverProvider
}

// Write operations use Lock
func (c *Client) Initialize() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    // ... write operations
}
```

**Use `context.Context` for cancellable operations:**
```go
func (c *Client) Completion(ctx context.Context, ...) {
    select {
    case result := <-response:
        return result, nil
    case <-ctx.Done():
        // Clean up and return cancellation error
        return nil, ctx.Err()
    }
}
```

### Configuration Validation

**Always validate config after loading:**
```go
cfg, err := config.Load()
if err != nil {
    return err
}
if err := cfg.Validate(); err != nil {
    return fmt.Errorf("invalid config: %w", err)
}
```

**Validate all user-provided values:**
- Tab size: 1-8
- Themes: must be in known list
- Commands: non-empty if enabled
- Intervals: positive if enabled

## Package Layout

```
cmd/teak/main.go        Entry point — initializes BubbleZone, creates app model, runs program
internal/
  app/                  Root Bubbletea model
    app.go              Main Model struct, Update(), View(), all message routing
    coordinator.go      Orchestrates subsystem coordinators
    messages.go         Type-safe message definitions
    lsp_coordinator.go  LSP message routing, client lifecycle
    dap_coordinator.go  Debug session management
    acp_coordinator.go  ACP agent coordination
    file_manager.go     Tab/file/tree coordination
    search_manager.go   Search coordination
    git_manager.go      Git panel coordination
    commands.go         Helper commands (file loading, saving)
    watcher.go          fsnotify file watcher for external change detection
  text/                 Core text data structures
    rope.go             Persistent immutable rope (512-byte leaves, Fibonacci balancing)
    buffer.go           High-level buffer with cursor, selection, undo/redo, file I/O
    position.go         Line/column position types
    undo.go             Undo/redo stack
    indent.go           Auto-indentation logic
  editor/               Editor UI components
    editor.go           Main editor model — viewport, scrolling, input handling
    viewport.go         Renders visible text lines with syntax highlighting
    gutter.go           Line numbers with diagnostic indicators
    tabbar.go           Tab bar for multiple open files
    autocomplete.go     LSP completion dropdown
    hover.go            LSP hover information display
    comments.go         Toggle line/block comments
    contextmenu.go      Right-click context menu
    help.go             Help overlay with searchable keyboard shortcuts
    welcome.go          Welcome screen with color-cycling logo animation
    config.go           Editor configuration (tab size, etc.)
  filetree/             File tree sidebar
    filetree.go         Tree model with lazy async directory loading, mouse/keyboard nav
    icons.go            Nerd Font v3 Material Design icons for file types
  highlight/            Syntax highlighting
    highlight.go        Chroma-based highlighter with per-line caching, viewport optimization
    styles.go           Token type → Nord color mapping
  lsp/                  Language Server Protocol
    client.go           LSP client — process management, JSON-RPC 2.0 over stdin/stdout
    manager.go          Multi-server manager — lazy init, per-language routing, retry logic
    config.go           Built-in server configs (gopls, typescript-language-server, etc.)
    messages.go         LSP message types and structs
    handler.go          Notification handlers (diagnostics, etc.)
    sync.go             Document synchronization (didOpen, didChange, didSave)
    uri.go              File path ↔ URI conversion
  search/               Search functionality
    search.go           Search overlay model — text input, results, mode toggle
    text.go             Grep-based text search (recursive, regex)
    semantic.go         Vecgrep-based semantic search with indexing
  git/                  Git integration
    panel.go            Sidebar panel showing branch + changed files via `git status --porcelain`
  clipboard/            System clipboard
    clipboard.go        macOS (pbcopy/pbpaste), Linux (xclip/xsel), internal fallback
  ui/                   Theme and styling
    theme.go            Nord color palette (Nord0–Nord15), Theme struct with 30+ lipgloss styles
    overlay.go          Overlay rendering utilities
  config/               Configuration management
    config.go           Config loading, merging, validation
  dap/                  Debug Adapter Protocol
    client.go           DAP client — debug session management
    manager.go          Multi-session manager
  debugger/             Debugger UI panel
    debugger.go         Debugger panel rendering and interaction
  acp/                  Agent Communication Protocol
    client.go           ACP agent client
    manager.go          Agent session management
  diff/                 Diff viewing
    diff.go             Diff view for comparing files
  problems/             Problems panel
    problems.go         Aggregated diagnostics display
  session/              Session persistence
    session.go          Save/restore editor state
  settings/             Settings UI
    settings.go         Settings editor overlay
  agent/                AI agent panel
    agent.go            Chat interface for AI agent
  overlay/              Overlay components
    picker.go           Generic picker/selector
    confirm.go          Confirmation dialogs
```

## Key Architectural Patterns

### Message Passing

All state changes flow through `Update(msg tea.Msg) (tea.Model, tea.Cmd)`. Components communicate via typed messages:

**File Operations:**
- `filetree.OpenFileMsg` — file selected in tree (preview)
- `filetree.PinFileMsg` — file opened permanently (double-click/enter)
- `FileOpenedMsg` — file loaded successfully
- `FileSavedMsg` — file saved to disk
- `FileClosedMsg` — file closed

**LSP Operations:**
- `lsp.DiagnosticsMsg` — LSP diagnostics received
- `LSPReadyMsg` — LSP client initialized
- `LSPCompletionMsg` — completion items received

**DAP Operations:**
- `DAPStoppedMsg` — debugger paused
- `DAPContinuedMsg` — debugger resumed
- `DAPTerminatedMsg` — debug session ended

**UI Operations:**
- `search.OpenResultMsg` — search result selected
- `search.CloseSearchMsg` — close search overlay
- `ToggleTreeMsg` — toggle file tree visibility
- `GoToLineMsg` — open go-to-line dialog

### Focus Management

The app tracks which panel has focus via `FocusArea`:
- `FocusEditor` — keyboard input goes to the active editor
- `FocusTree` — keyboard input goes to the file tree
- `FocusGitPanel` — keyboard input goes to the git panel
- `FocusProblems` — keyboard input goes to problems panel
- `FocusDebugger` — keyboard input goes to debugger panel
- `FocusAgent` — keyboard input goes to AI agent panel

### Async Operations

Heavy operations run as `tea.Cmd` (functions returning `tea.Msg`):
- File loading and saving
- Directory reading (lazy tree expansion)
- LSP requests (completion, hover, definition)
- Syntax tokenization
- Search queries
- Git status refresh

**Always use context for cancellable operations:**
```go
func loadFile(ctx context.Context, path string) tea.Cmd {
    return func() tea.Msg {
        select {
        case <-ctx.Done():
            return fileLoadErrMsg{err: ctx.Err()}
        default:
            data, err := os.ReadFile(path)
            return fileLoadedMsg{data: data, err: err}
        }
    }
}
```

---

## Testing Guidelines

### Test-First Development

**Always write tests before implementing features:**

```
1. Write test for expected behavior
   ↓
2. Run test (should fail)
   ↓
3. Implement feature
   ↓
4. Run test (should pass)
   ↓
5. Refactor (tests must still pass)
```

### Test Coverage Targets

| Package Type | Target | Priority |
|--------------|--------|----------|
| Core (text/, editor/) | > 70% | Critical |
| Integration (app/, lsp/) | > 60% | High |
| UI (filetree/, git/) | > 50% | Medium |
| **Overall Project** | **> 80%** | **Goal** |

### Unit Testing

**Use standard Go testing for unit tests:**

```go
func TestRopeInsert(t *testing.T) {
    r := NewFromString("hello")
    r2 := r.Insert(5, []byte(" world"))
    if r2.String() != "hello world" {
        t.Errorf("got %q, want %q", r2.String(), "hello world")
    }
}

// Table-driven tests for multiple cases
func TestRopeInsertMultiple(t *testing.T) {
    tests := []struct {
        name   string
        base   string
        offset int
        insert string
        want   string
    }{
        {"at beginning", "hello", 0, "X", "Xhello"},
        {"at end", "hello", 5, "X", "helloX"},
        {"in middle", "hello", 2, "X", "heXllo"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // test implementation
        })
    }
}
```

### Integration Testing with teatest

**For Bubble Tea integration tests, use teatest:**

```bash
go get github.com/charmbracelet/x/exp/teatest@latest
```

**Import:**
```go
import "github.com/charmbracelet/x/exp/teatest"
```

**Basic Integration Test:**
```go
func TestFileOpenEditSave(t *testing.T) {
    // Create model with test config
    cfg := config.DefaultConfig()
    cfg.Session.Enabled = false
    
    model, err := NewModel("test.go", ".", cfg)
    if err != nil {
        t.Fatal(err)
    }
    
    // Create test model wrapper
    tm := teatest.NewTestModel(t, model,
        teatest.WithInitialTermSize(80, 24))
    
    // Send messages
    tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})
    
    // Wait for condition
    teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
        return bytes.Contains(bts, []byte("Saved"))
    }, teatest.WithDuration(2*time.Second))
    
    // Verify final state
    m := tm.FinalModel(t).(Model)
    if m.activeEditor().Buffer.Dirty() {
        t.Error("Buffer should be clean after save")
    }
    
    tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}
```

**Coordinator Integration Test:**
```go
func TestLSPToDAPIntegration(t *testing.T) {
    model, _ := NewModel("", ".", config.DefaultConfig())
    tm := teatest.NewTestModel(t, model)
    
    // Send LSP diagnostic
    tm.Send(lspMsg{
        msg: lsp.DiagnosticsMsg{
            URI: "file:///test.go",
            Diagnostics: []lsp.Diagnostic{
                {Severity: 1, Message: "error"},
            },
        },
    })
    
    // Wait for processing
    time.Sleep(200 * time.Millisecond)
    
    // Verify coordinator state
    m := tm.FinalModel(t).(Model)
    diags := m.coordinator.GetLSPCoordinator().GetDiagnostics("/test.go")
    
    if len(diags) != 1 {
        t.Errorf("Expected 1 diagnostic, got %d", len(diags))
    }
    
    tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}
```

**Concurrency Test (run with -race):**
```go
func TestLSPCoordinatorConcurrentDiagnostics(t *testing.T) {
    model, _ := NewModel("", ".", config.DefaultConfig())
    tm := teatest.NewTestModel(t, model)
    
    // Send 100 concurrent messages
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            tm.Send(lspMsg{
                msg: lsp.DiagnosticsMsg{
                    URI: fmt.Sprintf("file:///test%d.go", id),
                },
            })
        }(i)
    }
    wg.Wait()
    
    time.Sleep(500 * time.Millisecond)
    
    // Verify no race conditions
    m := tm.FinalModel(t).(Model)
    _ = m.coordinator.GetLSPCoordinator().GetDiagnostics("/test50.go")
    
    tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}
```

**Golden File Test:**
```go
func TestWelcomeScreenOutput(t *testing.T) {
    model, _ := NewModel("", ".", config.DefaultConfig())
    tm := teatest.NewTestModel(t, model,
        teatest.WithInitialTermSize(80, 24))
    
    out, err := io.ReadAll(tm.FinalOutput(t))
    if err != nil {
        t.Fatal(err)
    }
    
    // Compare against golden file
    teatest.RequireEqualOutput(t, out)
    
    tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}
```

**Update golden files:**
```bash
go test ./... -update
```

### Test File Organization

```
internal/app/
  # Unit tests
  messages_test.go              # Message type tests
  coordinator_test.go           # Coordinator unit tests
  coordinator_error_test.go     # Error condition tests
  
  # Integration tests (use teatest)
  coordinator_integration_test.go  # Coordinator interactions
  coordinator_race_test.go         # Concurrency tests (run with -race)
  coordinator_state_test.go        # State transition tests
  coordinator_routing_test.go      # Message routing tests
  coordinator_memory_test.go       # Memory leak tests
  
  # Golden file tests
  testdata/
    welcome_screen.golden
    file_tree_view.golden
    problems_panel.golden
```

### CI/CD Testing

**GitHub Actions:**
```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      
      - name: Run unit tests
        run: go test ./... -v
      
      - name: Run race detection
        run: go test -race ./internal/app/... -v
      
      - name: Run integration tests
        run: go test ./internal/app/... -run Integration -v
      
      - name: Check coverage
        run: |
          go test ./... -coverprofile=coverage.out
          go tool cover -func=coverage.out | grep total
```

**Local Test Commands:**
```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./internal/app/...

# Run integration tests
go test ./internal/app/... -run Integration -v

# Update golden files
go test ./internal/app/... -update

# Check coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run specific test
go test ./internal/app/... -run TestLSPCoordinator -v
```

### Testing Best Practices

1. **Test coordinators in isolation** — Mock dependencies when possible
2. **Use teatest for integration** — End-to-end Bubble Tea tests
3. **Run with -race flag** — Catch race conditions early
4. **Test error conditions** — Not just happy paths
5. **Test state transitions** — Verify state machine behavior
6. **Test concurrent access** — Multiple goroutines accessing state
7. **Test memory limits** — Verify cleanup logic works
8. **Use golden files** — Catch UI regressions
9. **Keep tests fast** — Use timeouts, avoid sleeps when possible
10. **Document test intent** — Clear test names and comments

---

### Coordinator Pattern

**Subsystems are managed by coordinators:**
```
┌─────────────────────────────────────────┐
│           Root Model (app.go)           │
│  - Window management                    │
│  - Focus routing                        │
│  - Rendering                            │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│         Coordinator (coordinator.go)    │
│  - Route messages to subsystems         │
│  - Aggregate commands                   │
└─────┬──────────────┬──────────────┬─────┘
      │              │              │
      ▼              ▼              ▼
┌──────────┐  ┌──────────┐  ┌──────────┐
│   LSP    │  │   DAP    │  │   ACP    │
│Coordinator│  │Coordinator│  │Coordinator│
└──────────┘  └──────────┘  └──────────┘
```

**Benefits:**
- Separation of concerns
- Easier testing (test coordinators in isolation)
- Clearer message routing
- Reduced merge conflicts

### Lipgloss v2 Notes

In Lipgloss v2, `lipgloss.Color()` is a **function** that returns `color.Color`, not a type. Color values are `color.Color` (from `image/color`). The Nord palette is defined as package-level `color.Color` variables in `internal/ui/theme.go`.

### Rope Data Structure

The rope in `internal/text/rope.go` is **persistent and immutable**:
- `rope.Insert(pos, text)` returns a new `*Rope`
- `rope.Delete(start, end)` returns a new `*Rope`
- Leaf nodes hold up to 512 bytes
- Auto-rebalances using Fibonacci depth thresholds
- Line indexing is cached and invalidated on structural changes

**Thread-safe for concurrent reads** (immutable design).

### Syntax Highlighting

The highlighter in `internal/highlight/` uses Chroma v2:
- `TokenizeToLines()` tokenizes the full file
- `TokenizeViewportToLines()` tokenizes only visible lines (for scroll performance)
- `SetLines()` merges partial viewport results into the full cache (never replaces)
- Edit-triggered retokenization does full file; scroll-triggered does viewport only

### LSP Integration

- Servers are configured in `internal/lsp/config.go` per language
- Manager lazily starts servers on first request for a language
- Communication is JSON-RPC 2.0 over stdin/stdout
- Diagnostics arrive as async notifications and are routed to both the editor and file tree
- **Requests support cancellation via context**
- **Capability checks use RWMutex for concurrency**

### Configuration System

Configuration is loaded from `~/.config/teak/config.toml`:

```toml
[editor]
tab_size = 4
insert_tabs = false
auto_indent = true
format_on_save = true
word_wrap = false

[ui]
theme = "nord"
show_tree = true

[[lsp]]
extensions = [".go"]
command = "gopls"
language_id = "go"

[agent]
enabled = true
command = "opencode"
args = ["acp"]

[session]
enabled = true
auto_save_interval = 30
```

**Always validate after loading:**
```go
cfg, err := config.Load()
if err := cfg.Validate(); err != nil {
    // Handle validation error
}
```

## Common Tasks

### Adding a new keyboard shortcut

1. Add the keybinding to `internal/editor/help.go` in the `helpGroups` variable
2. Handle the key in `internal/app/app.go` in the `tea.KeyPressMsg` switch (or appropriate coordinator)
3. If it's editor-specific, handle it in `internal/editor/editor.go` instead
4. **Add a test** for the new shortcut behavior

### Adding a new LSP language

1. Add a `ServerConfig` entry in `internal/lsp/config.go`
2. Map the file extension to the language ID in the manager's detection logic
3. **Add a test** for language detection

### Adding a new file icon

1. Edit `internal/filetree/icons.go`
2. Add an entry to the extension or name map with the Nerd Font v3 codepoint and color
3. **Add a test** for icon lookup

### Adding a new theme style

1. Add the `lipgloss.Style` field to the `Theme` struct in `internal/ui/theme.go`
2. Initialize it in `DefaultTheme()` using Nord palette colors
3. **Add a test** for theme creation

### Adding a new coordinator

1. Create `internal/app/x_coordinator.go`
2. Define the coordinator struct and interface
3. Implement `Update(msg tea.Msg) ([]tea.Cmd, tea.Cmd)`
4. Register in `Coordinator` struct
5. **Add tests** for coordinator message handling

### Adding configuration validation

1. Add validation logic to `Config.Validate()` in `internal/config/config.go`
2. **Add tests** in `internal/config/config_validation_test.go`
3. Ensure `main.go` calls `cfg.Validate()` after `config.Load()`

## Development Workflow

### Test-First Approach

```
1. Write test for expected behavior
   ↓
2. Run test (should fail)
   ↓
3. Implement feature
   ↓
4. Run test (should pass)
   ↓
5. Refactor (tests must still pass)
```

### Before Submitting Code

```bash
# 1. Build
go build -o bin/teak ./cmd/teak

# 2. Run all tests
go test ./...

# 3. Check coverage (optional)
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# 4. Run linter (if configured)
go vet ./...
```

### Debugging Tips

**Enable logging:**
```go
log.SetLevel(log.DebugLevel)
log.Debug("message", "key", value)
```

**Test async operations:**
```go
func TestAsyncOperation(t *testing.T) {
    model, cmd := setupModel()
    
    // Execute command
    msg := cmd()
    
    // Process message
    model, _ = model.Update(msg)
    
    // Verify state
    if model.state != expected {
        t.Errorf("got %v, want %v", model.state, expected)
    }
}
```

## Dependencies

| Package | Import Path | Purpose |
|---------|-------------|---------|
| Bubbletea v2 | `charm.land/bubbletea/v2` | TUI framework (Elm architecture) |
| Lipgloss v2 | `charm.land/lipgloss/v2` | Terminal styling |
| Bubbles v2 | `charm.land/bubbles/v2` | Text input, spinner components |
| BubbleZone v2 | `github.com/lrstanley/bubblezone/v2` | Mouse click zones |
| Chroma v2 | `github.com/alecthomas/chroma/v2` | Syntax highlighting lexers |
| fsnotify | `github.com/fsnotify/fsnotify` | File system event watching |
| BurntSushi/toml | `github.com/BurntSushi/toml` | TOML config parsing |
| coder/acp-go-sdk | `github.com/coder/acp-go-sdk` | ACP agent protocol |

## Architecture Documentation

For detailed architecture and refactoring plans, see:
- `REFACTOR_PLAN.md` — Coordinator pattern refactoring plan

## Performance Guidelines

**Keep these operations async:**
- File I/O (loading, saving)
- Network requests (LSP, ACP)
- Directory traversal
- Syntax highlighting (for large files)

**Optimize hot paths:**
- Cursor movement (should be instant)
- Scrolling (viewport-only highlighting)
- Typing (rope operations are O(log n))

**Use benchmarks:**
```go
func BenchmarkOperation(b *testing.B) {
    // Setup
    for i := 0; i < b.N; i++ {
        // Operation to benchmark
    }
}
```

Run with: `go test ./... -bench=. -benchmem`
