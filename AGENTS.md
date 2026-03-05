# AGENTS.md — Teak Codebase Guide for AI Agents

## Project Overview

Teak is a terminal code editor written in Go 1.24+ using the Bubbletea v2 TUI framework. It follows the Elm architecture (Model-View-Update) with message passing for all state changes.

## Build & Test

```sh
go build -o bin/teak ./cmd/teak   # or: task build
go test ./...                      # or: task test
```

All code must compile and pass tests before being considered complete.

## Code Conventions

- **Go 1.24+** — use standard library where possible
- **0-based indexing** for lines and columns throughout the entire codebase
- **Immutable rope** — `Insert` and `Delete` on `*Rope` return a new `*Rope`, never mutate in place
- **Return errors, don't panic** — no `panic()`, `log.Fatal()`, or `log.Panic()` in library code
- **Logging** — use `github.com/charmbracelet/log` for structured logging. Prefer `log.Error()` with key-value pairs over `log.Printf()`.
- **Table-driven tests** in `_test.go` files alongside source
- **No unnecessary abstractions** — keep things simple, avoid premature generalization

## Package Layout

```
cmd/teak/main.go        Entry point — initializes BubbleZone, creates app model, runs program
internal/
  app/                  Root Bubbletea model
    app.go              Main Model struct, Update(), View(), all message routing
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
```

## Key Architectural Patterns

### Message Passing

All state changes flow through `Update(msg tea.Msg) (tea.Model, tea.Cmd)`. Components communicate via typed messages:

- `filetree.OpenFileMsg` — file selected in tree (preview)
- `filetree.PinFileMsg` — file opened permanently (double-click/enter)
- `search.OpenResultMsg` — search result selected
- `search.CloseSearchMsg` — close search overlay
- `lsp.DiagnosticsMsg` — LSP diagnostics received
- `editor.WelcomeTickMsg` — welcome animation frame

### Focus Management

The app tracks which panel has focus via `FocusArea`:
- `FocusEditor` — keyboard input goes to the active editor
- `FocusTree` — keyboard input goes to the file tree
- `FocusGitPanel` — keyboard input goes to the git panel

### Async Operations

Heavy operations run as `tea.Cmd` (functions returning `tea.Msg`):
- File loading and saving
- Directory reading (lazy tree expansion)
- LSP requests (completion, hover, definition)
- Syntax tokenization
- Search queries
- Git status refresh

### Lipgloss v2 Notes

In Lipgloss v2, `lipgloss.Color()` is a **function** that returns `color.Color`, not a type. Color values are `color.Color` (from `image/color`). The Nord palette is defined as package-level `color.Color` variables in `internal/ui/theme.go`.

### Rope Data Structure

The rope in `internal/text/rope.go` is **persistent and immutable**:
- `rope.Insert(pos, text)` returns a new `*Rope`
- `rope.Delete(start, end)` returns a new `*Rope`
- Leaf nodes hold up to 512 bytes
- Auto-rebalances using Fibonacci depth thresholds
- Line indexing is cached and invalidated on structural changes

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

## Common Tasks

### Adding a new keyboard shortcut

1. Add the keybinding to `internal/editor/help.go` in the `helpGroups` variable
2. Handle the key in `internal/app/app.go` in the `tea.KeyPressMsg` switch
3. If it's editor-specific, handle it in `internal/editor/editor.go` instead

### Adding a new LSP language

1. Add a `ServerConfig` entry in `internal/lsp/config.go`
2. Map the file extension to the language ID in the manager's detection logic

### Adding a new file icon

1. Edit `internal/filetree/icons.go`
2. Add an entry to the extension or name map with the Nerd Font v3 codepoint and color

### Adding a new theme style

1. Add the `lipgloss.Style` field to the `Theme` struct in `internal/ui/theme.go`
2. Initialize it in `DefaultTheme()` using Nord palette colors

## Dependencies

| Package | Import Path | Purpose |
|---------|-------------|---------|
| Bubbletea v2 | `charm.land/bubbletea/v2` | TUI framework (Elm architecture) |
| Lipgloss v2 | `charm.land/lipgloss/v2` | Terminal styling |
| Bubbles v2 | `charm.land/bubbles/v2` | Text input, spinner components |
| BubbleZone v2 | `github.com/lrstanley/bubblezone/v2` | Mouse click zones |
| Chroma v2 | `github.com/alecthomas/chroma/v2` | Syntax highlighting lexers |
| fsnotify | `github.com/fsnotify/fsnotify` | File system event watching |
