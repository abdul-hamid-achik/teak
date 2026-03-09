# Teak

A modern terminal code editor built with Go.

![Go 1.24+](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)

Teak brings a familiar, VS Code-like editing experience to the terminal — syntax highlighting, LSP integration, multi-tab editing, a file tree, git status, and mouse support, all running in your shell.

## Features

- **Multi-tab editor** with tab bar, preview tabs, and pinned tabs
- **Syntax highlighting** for 40+ languages via Chroma
- **LSP support** — autocomplete, hover, go-to-definition, diagnostics, rename
- **File tree** sidebar with lazy-loaded directories, diagnostic indicators, and context menus
- **Git panel** showing branch name and changed files with color-coded status
- **Text & semantic search** with debounced input and clickable results
- **Live file watching** — external changes are detected and reloaded automatically
- **Undo/redo** backed by a persistent, immutable rope data structure
- **Mouse support** — click, drag-select, scroll, double-click to select word, right-click context menus
- **System clipboard** integration (macOS and Linux)
- **Nord color theme** with 30+ carefully tuned styles
- **Welcome screen** with animated logo on startup
- **Built-in help overlay** with searchable keyboard shortcuts

## Install

```sh
go install teak/cmd/teak@latest
```

Or build from source:

```sh
git clone <repo-url> && cd teak
go build -o bin/teak ./cmd/teak
```

Requires **Go 1.24+**.

## Usage

```sh
# Open a file
teak main.go

# Open in the current directory
teak

# Open a directory
teak ~/projects/myapp
```

## Keyboard Shortcuts

### General

| Key | Action |
|-----|--------|
| `Ctrl+Q` | Quit |
| `Ctrl+S` | Save file |
| `F1` | Toggle help |

### Navigation

| Key | Action |
|-----|--------|
| `Arrows` | Move cursor |
| `Ctrl+Left/Right` | Word jump |
| `Home/End` | Line start/end |
| `Ctrl+Home/End` | Document start/end |
| `PgUp/PgDn` | Page up/down |
| `Ctrl+G` | Go to line |

### Selection

| Key | Action |
|-----|--------|
| `Shift+Arrows` | Select characters |
| `Ctrl+Shift+Left/Right` | Select words |
| `Shift+Home/End` | Select to line edge |
| `Ctrl+A` | Select all |
| `Ctrl+D` | Select next occurrence |
| `Ctrl+U` | Select all occurrences |
| `Ctrl+Alt+Up` | Add cursor above |
| `Ctrl+Alt+Down` | Add cursor below |
| `Double-click` | Select word |
| `Click+Drag` | Select with mouse |

### Multi-Cursor Editing

| Key | Action |
|-----|--------|
| `Ctrl+D` | Select next occurrence of current selection |
| `Ctrl+U` | Select all occurrences of current selection |
| `Ctrl+Alt+Up` | Add cursor on line above |
| `Ctrl+Alt+Down` | Add cursor on line below |
| `Esc` | Clear additional cursors (keep primary) |

**Multi-Cursor Features:**
- Type at multiple positions simultaneously
- Delete/edit multiple selections at once
- All cursors move together with arrow keys
- Primary selection (last added) determines viewport scrolling

### Editing

| Key | Action |
|-----|--------|
| `Ctrl+C/X/V` | Copy / Cut / Paste |
| `Tab / Shift+Tab` | Indent / Dedent |
| `Ctrl+/` | Toggle comment |
| `Alt+Up/Down` | Move line |
| `Alt+Shift+Up/Down` | Duplicate line |
| `Ctrl+Shift+K` | Delete line |
| `Ctrl+Z / Ctrl+Y` | Undo / Redo |

### Search

| Key | Action |
|-----|--------|
| `Ctrl+F` | Text search |
| `Ctrl+Shift+F` | Semantic search |
| `Tab` | Toggle search mode |

### Panels

| Key | Action |
|-----|--------|
| `Ctrl+B` | Toggle file tree |
| `Ctrl+Shift+G` | Toggle git panel |
| `Ctrl+Tab` | Next tab |
| `Ctrl+Shift+Tab` | Previous tab |

### LSP

| Key | Action |
|-----|--------|
| `Ctrl+Space` | Autocomplete |
| `F12` | Go to definition |

## Architecture

```
cmd/teak/           Entry point
internal/
  app/              Root Bubbletea model, file watcher, commands
  text/             Rope data structure, buffer, undo/redo, indentation
  editor/           Viewport, gutter, tabs, autocomplete, hover, help, welcome
  filetree/         File explorer sidebar with icons and context menus
  highlight/        Syntax highlighting via Chroma with viewport optimization
  lsp/              LSP client, multi-server manager, language configs
  search/           Text (grep) and semantic (vecgrep) search
  git/              Git panel showing branch and file status
  clipboard/        Cross-platform clipboard (pbcopy/xclip)
  ui/               Nord theme and styles
```

The text buffer uses an **immutable rope** — insert and delete operations return a new tree, enabling efficient undo/redo without copying. Syntax highlighting is **viewport-optimized**, only materializing styles for visible lines. LSP servers are started **lazily** per language and communicate over stdin/stdout with JSON-RPC 2.0.

## Dependencies

| Package | Purpose |
|---------|---------|
| [Bubbletea v2](https://charm.land/bubbletea) | TUI framework |
| [Lipgloss v2](https://charm.land/lipgloss) | Terminal styling |
| [Bubbles v2](https://charm.land/bubbles) | Text input, spinner components |
| [BubbleZone v2](https://github.com/lrstanley/bubblezone) | Mouse zone support |
| [Chroma v2](https://github.com/alecthomas/chroma) | Syntax highlighting |
| [fsnotify](https://github.com/fsnotify/fsnotify) | File system watching |

## Build & Test

```sh
# Using Task runner
task build        # Build binary to bin/teak
task test         # Run all tests
task run -- file  # Build and run
task clean        # Remove build artifacts

# Using Go directly
go build -o bin/teak ./cmd/teak
go test ./...
```

## License

MIT
