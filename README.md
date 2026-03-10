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
- **Text search** built in, plus **semantic search** when `vecgrep` is installed
- **Lua plugins** with commands, keymaps, autocmds, and live editor/runtime access
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

Semantic search is optional and requires [`vecgrep`](https://github.com/veces/vecgrep) to be installed and available on your `PATH`. Without it, regular text search still works and semantic search will report that the dependency is missing.

## Usage

```sh
# Open a file
teak main.go

# Open in the current directory
teak

# Open a directory
teak ~/projects/myapp
```

## Plugins

Teak loads Lua plugins from `~/.config/teak/plugins/<plugin-name>/`.
Each plugin directory needs a `plugin.toml` file and a Lua entrypoint, typically `init.lua`.

Minimal layout:

```text
~/.config/teak/plugins/
  my-plugin/
    plugin.toml
    init.lua
```

Minimal plugin:

```toml
# plugin.toml
name = "my-plugin"
main = "init.lua"
```

```lua
local function update_status()
  local path = buffer.get_filepath() or "[No Name]"
  local line, col = buffer.get_cursor()
  editor.set_status(string.format("%s | %d:%d", path, line, col))
end

function setup()
  editor.command("my_plugin.status", update_status)

  keymap.set("n", "<leader>ms", function()
    update_status()
    ui.notify("Status updated", "info")
  end, { desc = "Refresh plugin status" })

  autocmd.register("CursorMoved", function()
    update_status()
  end)
end
```

Plugin notes:

- `setup()` is called once when the plugin loads.
- `teardown()` is called when the plugin unloads, if it exists.
- Buffer, editor, and `ui.*` runtime operations are only available while Teak is dispatching a keymap, command, or autocmd. Calling them directly in `setup()` will fail.
- Supported keymap modes are `n`, `a`, `tree`, `git`, `problems`, `debugger`, and `agent`.
- `editor.feed_keys(keys)` supports plain text, named keys like `<enter>` and `<left>`, and modifiers like `ctrl+s` or `<shift+tab>`.
- `editor.feed_keys` bypasses plugin key dispatch for the injected keys, so it will not recursively trigger another plugin mapping.

Supported plugin APIs:

- `buffer.*`
- `editor.command`
- `editor.feed_keys`
- `editor.get_*`, `editor.set_status`, `editor.open_file`, and tab management
- `keymap.*`
- `autocmd.*`
- `ui.notify`
- `ui.show_panel`, `ui.hide_panel`, `ui.toggle_panel`

Unsupported plugin APIs:

- `ui.input`
- `ui.confirm`
- `ui.new_buffer`
- `ui.new_float`
- `ui.close_float`
- `ui.set_highlights`
- `ui.clear_highlights`

Current autocmd events:

- `VimEnter`
- `VimLeave`
- `BufRead`
- `BufEnter`
- `BufLeave`
- `BufWrite`
- `BufNew`
- `BufDelete`
- `TextChanged`
- `CursorMoved`
- `FileType`

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
