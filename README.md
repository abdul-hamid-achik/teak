# Teak

A fast, modern code editor for the terminal, built with Go and Bubble Tea.

![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)

Teak brings a familiar graphical-editor workflow to the terminal: tabs and
splits, project navigation, language-server features, Git integration,
debugging, and an AI agent panel. It stays responsive by keeping file-sized
work out of the UI update loop.

## Highlights

- **Code intelligence** — completion, hover, go to definition, diagnostics,
  rename, code actions, document symbols, and formatting through LSP
- **Workspace navigation** — preview and pinned tabs, editor splits, file tree,
  quick open, command palette, and project-wide search
- **Editing tools** — consistent multicursor navigation, line movement and
  duplication, deletion, paired delimiters, per-cursor automatic indentation,
  structural indent/dedent and comment toggling, and multi-selection clipboard
  operations; Unicode-aware word and display-column navigation; code folding,
  find and replace, undo/redo, and file watching
- **Integrated workflows** — Git status and commits, problems panel, Go
  debugging with Delve, and an ACP-compatible AI agent panel
- **Customizable UI** — built-in settings, Lua plugins, mouse support, system
  clipboard integration, and five bundled color themes
- **Large-file foundations** — an immutable rope, viewport-oriented rendering,
  asynchronous search and tokenization, and stale-result protection

## Quick start

Install Teak with Homebrew, then open the current directory:

```sh
brew install abdul-hamid-achik/tap/teak
teak
```

Press `F1` inside Teak to open the searchable shortcut reference. A Nerd Font
is recommended for the best icons, but is not required.

## Installation

### Homebrew

The Homebrew formula provides builds for macOS and Linux on Intel and ARM64:

```sh
brew install abdul-hamid-achik/tap/teak
```

### Release archive

Download a prebuilt archive for macOS, Linux, or Windows from
[GitHub Releases](https://github.com/abdul-hamid-achik/teak/releases), then put
the `teak` binary somewhere on your `PATH`.

### Build from source

Teak requires Go 1.26.0 or newer. The module recommends Go 1.26.5; Go 1.21+
can download that toolchain automatically when `GOTOOLCHAIN=auto` is enabled.

```sh
git clone https://github.com/abdul-hamid-achik/teak.git
cd teak
go build -o bin/teak ./cmd/teak
./bin/teak
```

To make the command available outside the repository, copy `bin/teak` to a
directory on your `PATH`.

Run the complete local verification gate before contributing:

```sh
task check       # tests, vet, race detector, build, and Glyphrun specs
task specs-repeat
task coverage    # aggregate Go statement coverage
task verify      # check + coverage + three-run Glyphrun stability probe
task perf-tools  # isolated codemap/vecgrep RSS measurements
task perf-tools-test
```

## Usage

```sh
# Open the current directory
teak

# Open a directory
teak ~/projects/myapp

# Open a file; the workspace is the nearest project root
teak main.go

# Open multiple files as tabs
teak a.go b.go

# Jump to a 1-based line and optional column
teak main.go:42
teak main.go:42:7
teak +10 internal/app/app.go

# Show CLI help or version information
teak --help
teak --version

# Diagnose configuration and optional project tools
teak doctor
teak doctor --json --root ~/projects/myapp

# Read-only machine-facing operations
teak headless context --depth 2 --json --root ~/projects/myapp
teak headless project list --depth 2 --json --root ~/projects/myapp
teak headless project stat --json --root ~/projects/myapp internal/app/app.go
teak headless project mkdir --confirm --json --root ~/projects/myapp tmp
teak headless project rename --confirm --json --root ~/projects/myapp tmp tmp-old
teak headless project copy --confirm --json --root ~/projects/myapp tmp-old tmp-copy
teak headless project remove --confirm --json --root ~/projects/myapp tmp-copy
teak headless buffer read --json --root ~/projects/myapp internal/app/app.go
teak headless search --json --root ~/projects/myapp 'TODO'
teak headless session health --json --root ~/projects/myapp
teak headless session cleanup --confirm --json --root ~/projects/myapp
```

`teak doctor --json` separates a resolved language-server path from verified
health: detected languages expose `available` only after a bounded version or
protocol probe succeeds, `failed` when the resolved server is broken,
`timed_out` when the resolved server exceeds its bounded probe budget, and
`version_probe: unsupported` when Teak has no safe CLI version probe for that
server.
When a CLI version probe is unavailable, `doctor` verifies the LSP
`initialize`/`initialized` handshake and reports `protocol_probe` separately.
The built-in YAML and Lua servers have safe `--version` probes; launchers such
as `vscode-json-language-server` can therefore be verified without exposing a
fake version.
The report also includes bounded structured `actions`, using `install` for
missing capabilities and `repair` for invalid or failed probes. Codemap,
Vecgrep, and Hitspec additionally expose their verified health capability; an
installed Vecgrep without `status --lightweight`, or an installed Hitspec
without `validate`, is reported as a warning instead of a false-ready tool.

`teak headless lsp status` is read-only with respect to the project: it does
not index the workspace or open documents. `available` means its executable
resolves and `ready` means a configured version probe succeeded. For a server
without a safe version probe, the default is the explicit `unsupported` state;
pass `--probe` to opt into a short-lived LSP `initialize`/`initialized` handshake.
Protocol evidence then appears as `protocol_probe: ready|failed|timed_out`;
`state: timed_out` remains distinct from a failed server.
An explicit protocol probe also reports `capability_probe` and a stable
`capabilities` list; version-only checks leave method capabilities unknown.

When you open a file, Teak finds the nearest project marker—such as `.git`,
`go.mod`, `package.json`, or `Cargo.toml`—and uses that directory as the
workspace root.

### Sessions

Teak restores open tabs and editor state per workspace when session persistence
is enabled. Sessions are saved under the XDG state directory:

```text
$XDG_STATE_HOME/teak/sessions/<hash>/session.json
# Linux default: ~/.local/state/teak/sessions/<hash>/session.json
```

### Headless operations

The headless interface does not require an interactive terminal. It is intended
for Glyphrun, scripts, diagnostics, and agents. Reads are bounded by workspace
and size checks; writes require an expected SHA-256:

```sh
teak headless context --depth 2 --json --root ~/projects/myapp
teak headless buffer read --json --root ~/projects/myapp internal/app/app.go
teak headless search --json --root ~/projects/myapp 'workspaceRelativePath'
teak headless search --semantic --json --root ~/projects/myapp 'authentication'
teak headless search --semantic --index --json --root ~/projects/myapp 'authentication'
teak headless codemap context --json --root ~/projects/myapp MySymbol
teak headless codemap impact --json --depth 3 --root ~/projects/myapp MySymbol
teak headless codemap symbols --json --root ~/projects/myapp internal/app/app.go
teak headless codemap symbol-at --line 120 --json --root ~/projects/myapp internal/app/app.go
teak headless session show --json --root ~/projects/myapp
teak headless session list --json --root ~/projects/myapp
teak headless session save review --json --root ~/projects/myapp
teak headless session activate review --json --root ~/projects/myapp
teak headless session health --json --root ~/projects/myapp
teak headless session cleanup --confirm --json --root ~/projects/myapp
teak headless tools status --json --root ~/projects/myapp
teak headless health --json --root ~/projects/myapp
teak headless health dashboard --limit 20 --json --root ~/projects/myapp
teak headless health record --confirm --json --root ~/projects/myapp
teak headless health history --limit 20 --json --root ~/projects/myapp
teak headless serve --listen 127.0.0.1:8787 --token "$TEAK_TOKEN" --root ~/projects/myapp
teak headless serve --listen 127.0.0.1:8787 --token "$TEAK_TOKEN" --json \
  --workspace app=~/projects/myapp --workspace tools=~/projects/tools
teak headless mcp --root ~/projects/myapp
teak headless lsp status --json --root ~/projects/myapp
teak headless lsp status --probe --json --root ~/projects/myapp
teak headless lsp diagnostics --json --root ~/projects/myapp main.go
teak headless lsp format --json --root ~/projects/myapp main.go
teak headless lsp symbols --json --root ~/projects/myapp main.go
teak headless lsp hover --line 12 --column 8 --json --root ~/projects/myapp main.go
teak headless lsp definition --line 12 --column 8 --json --root ~/projects/myapp main.go
teak headless lsp references --line 12 --column 8 --json --root ~/projects/myapp main.go
teak headless dap status --json --root ~/projects/myapp
teak headless dap probe --json --root ~/projects/myapp
teak headless git status --json --root ~/projects/myapp
teak headless agent list --json --root ~/projects/myapp
teak headless agent show --json --root ~/projects/myapp <run-id>
teak headless agent cancel --confirm --json --root ~/projects/myapp <run-id>
teak headless agent reap-stale --confirm --json --root ~/projects/myapp --max-silence 2m
teak headless hitspec validate --json --root ~/projects/myapp api.http
# Direct command execution requires an explicit confirmation flag and `--`.
teak headless exec --confirm --json --root ~/projects/myapp -- go test ./...
```

Direct execution is shell-free by default, requires confirmation, and bounds
both command arguments and captured output before running the process. When a
caller context is canceled, the command's process group is stopped promptly
and the JSON response reports `state: "cancelled"`.

Inside the editor, open `Ctrl+Shift+P` and choose `Workspace Health Dashboard`
to inspect the same bounded current health, explicit history, actions, and
runtime deltas without leaving the TUI.

For integrations that cannot invoke a subprocess per request, `headless
serve` exposes the bounded local control plane over HTTP. It binds only to
loopback, requires an explicit bearer token, and fixes its workspace allowlist at
startup. `--root` registers the backwards-compatible `default` workspace;
`--workspace name=directory` can be repeated up to 32 times. Without `--root`,
the first named workspace becomes the default. `/v1/workspaces` lists the
registered roots, while `/v1/workspaces/<name>/...` routes to one of them;
request query parameters cannot replace a registered root. It supports JSON
routes such as `/v1/context`, `/v1/search?q=TODO`,
`/v1/health`, `/v1/health/dashboard?limit=20`, `/v1/health/history?limit=20`,
`/v1/tools/status`, `/v1/session/{show,list,health}`,
`/v1/codemap/{symbols,symbol-at}`,
`/v1/lsp/{status,diagnostics,format,symbols,hover,definition,references}`, `/v1/dap/{status,probe}`, and
`/v1/agent/{list,show}`. Confirmed lifecycle mutations are available through
`POST /v1/agent/cancel?run_id=...` and
`POST /v1/agent/reap-stale?max_silence=...`; both require
`X-Teak-Confirm: true` and mutate only durable run state. The
semantic search route remains read-only by default; an explicit
`/v1/search?q=meaning&semantic=true&index=true` build requires the same
`X-Teak-Confirm: true` header. MCP exposes the equivalent `index: true` plus
`confirm: true` arguments on `teak_search`.
`POST /v1/buffer/write` route requires `X-Teak-Confirm: true`, a
workspace-relative path, and the SHA-256 returned by the preceding read. Stale
writes return HTTP 409 and never overwrite newer content. The project mutation
routes `/v1/project/{mkdir,rename,copy,remove}` accept one bounded JSON body,
require the same confirmation header, and return the typed commit state, node/
byte counts, and duration from the root-confined project backend.
Agent cancellation and stale reaping use the same confirmation header and
persist lifecycle state without launching a process. Each returned run snapshot
also includes a bounded lifecycle `events` list, so supervisors can see whether
it started, was recovered after restart, cancelled, timed out, or completed
successfully without storing unbounded terminal output.
Active ACP file, permission, and terminal operations also retain a separate
bounded `audit` list with stable operation/outcome classifications and short
sandbox, scope, or size metadata. It never stores command lines, arguments,
paths, environment values, file content, permission payloads, model content, or
stdout, and is available through the same `agent list/show` snapshots.
All read-only REST routes derive their bounded collectors from the HTTP request
context; disconnecting a client cancels active scans, external probes, and LSP
diagnostics/formatting instead of leaving them running until their maximum
timeout. URL paths and query values are capped before becoming headless command
arguments; oversized requests return a typed `request_too_long` error.

`teak headless mcp` provides the bounded local control-plane surface over
newline-delimited JSON-RPC 2.0 for MCP-compatible agents. It exposes bounded
project, buffer, search, codemap (including file symbols and symbol-at), tool, health dashboard/history, Git,
hitspec, and read-only session/LSP/DAP/agent inspection tools, including
diagnostics, format previews, document symbols, hover, definition and
reference locations, adapter probes, named-session health, and individual
agent-run inspection. `teak_buffer_write` requires
`confirm: true`, the last-read `expected_sha256`, and bounded `content`; stale
writes return a typed error and never overwrite newer content. The explicit
`teak_project_mkdir`, `teak_project_rename`, `teak_project_copy`, and
`teak_project_remove`, `teak_agent_cancel`, and `teak_agent_reap_stale` tools
reuse the root-confined CLI operations and all require `confirm: true`. Shell
execution and arbitrary agent launching remain rejected. The workspace is
fixed at process start, tool subprocesses inherit cancellation and output
limits, and `notifications/cancelled` terminates the active child. Cancelling
the server context also closes a closable stdio input, so an open client stream
cannot strand the JSON-RPC reader. JSON-RPC
request IDs are restricted to strings and numbers; malformed scalar types are
rejected before a request can occupy the active-request map. `tools/list`
declares required arguments for each operation, so compatible agents can reject
incomplete calls before sending them.

Semantic headless search is read-only by default: a stale or uninitialized
vecgrep index returns a structured non-ready state. Add `--index` only when
the caller explicitly authorizes the potentially expensive index operation.
Results from vecgrep are normalized to workspace-relative paths and rejected if
they point to missing, non-regular, symlink-escaping, or out-of-root files;
source line numbers are converted from vecgrep's 1-based contract to Teak's
0-based editor coordinates.

The interface enforces workspace boundaries for buffer reads and writes, limits
the size of a single buffer operation, returns stable JSON fields, and bounds
searches with a timeout. Search JSON includes `truncated: true` when the
bounded result limit (100 text or 20 semantic hits) was reached, so callers
can refine a query instead of treating a partial list as exhaustive. Command
execution is direct (no implicit shell),
requires `--confirm`, uses a workspace working directory, and is bounded by a
two-minute timeout and one MiB per output stream.

If ripgrep reaches its bounded output cap after producing a partial prefix,
Teak falls back to the bounded pure-Go walker so a partial external-tool
stream is not reported as a complete search.

`teak headless tools status` also reports bounded version-probe results for
known tools (`version_probe: ready|failed|timed_out|unsupported`); a path that merely
exists is not treated as a verified capability. When a known probe fails, the
tool state is `failed` and `ready` is `false`; when it exceeds the probe
budget, the state is `timed_out`, also with `ready: false`, even if the
executable resolves.
The JSON `mode` field identifies the bounded health path: codemap uses
`structural-manifest`, vecgrep uses `lightweight-status`, and simple tools use
`version-probe`; health never loads a semantic index.

`teak headless project` provides the explicit filesystem surface for agents.
Listing and stat are read-only and bounded; mkdir, rename, copy, and remove
require `--confirm`, stay relative to `--root`, reject traversal, reject
symlink copies, and never invoke a shell. JSON mutation responses include
`committed`, node/byte counts, and duration.

## Common shortcuts

This is the compact reference. Press `F1` in Teak for the complete, searchable
list.

| Area | Key | Action |
|------|-----|--------|
| Files | `Ctrl+N` | New file |
| Files | `Ctrl+S` / `Ctrl+Shift+S` | Save / Save as |
| Files | `Ctrl+P` | Quick open |
| Files | `Ctrl+W` / `Ctrl+Shift+T` | Close / reopen tab |
| Commands | `Ctrl+Shift+P` | Command palette |
| Commands | `Ctrl+,` | Settings |
| Search | `Ctrl+F` | Find in the active file |
| Search | `Ctrl+H` | Find and replace |
| Search | `Ctrl+Shift+F` | Search the project |
| Search | `Tab` | Switch text/semantic project search |
| File tree | `/` (tree focus) | Filter project files |
| File tree | `Ctrl+H` (tree focus) | Toggle hidden files |
| File tree | `Ctrl+K` (tree focus) | Toggle Git-ignored files |
| Editing | `Ctrl+/` | Toggle comment |
| Editing | `Alt+Up/Down` | Move line |
| Editing | `Alt+Shift+Up/Down` | Duplicate line |
| Selection | `Ctrl+D` | Select next occurrence |
| Selection | `Ctrl+U` | Select all occurrences |
| LSP | `Ctrl+Space` | Completion |
| LSP | `Alt+K` | Hover information |
| LSP | `Ctrl+K` | Code actions |
| LSP | `F12` | Go to definition |
| LSP | `Ctrl+Shift+O` | Document symbols |
| LSP | `Ctrl+Alt+F` | Format document |
| Panels | `Ctrl+B` | Toggle sidebar |
| Panels | `Ctrl+Shift+G` | Open Git panel |
| Panels | `Ctrl+J` | Toggle agent panel |
| Splits | `Ctrl+\\` | Toggle editor split |
| Splits | `F6` | Switch split focus |
| Problems | `F8` / `Shift+F8` | Next / previous problem |
| Debugging | `F5` / `Shift+F5` | Start / stop debugging |
| General | `F1` | Shortcut reference |
| General | `Ctrl+Q` | Quit |

Right-click a file-tree entry for safe `Rename`, `Duplicate`, `Move to`, and
`Delete` actions. File operations stay inside the workspace and run
asynchronously; open tabs follow committed moves and renames while preserving
dirty buffers and diagnostic state.

## Configuration

Open the settings interface with `Ctrl+,`, or edit `config.toml` directly.
Teak uses the platform configuration directory:

| Platform | Configuration file |
|----------|--------------------|
| macOS | `~/Library/Application Support/teak/config.toml` |
| Linux | `$XDG_CONFIG_HOME/teak/config.toml` or `~/.config/teak/config.toml` |
| Windows | `%AppData%\\teak\\config.toml` |

The file is optional; missing values use built-in defaults.

```toml
[editor]
tab_size = 4
insert_tabs = false
auto_indent = true
format_on_save = false
word_wrap = false

[ui]
theme = "nord"
show_tree = true

# Optional absolute or relative executable overrides. These are used by the
# editor, doctor, headless commands, and LSP/agent launchers.
[tools]
codemap = "/opt/homebrew/bin/codemap"
vecgrep = "/Users/me/.local/bin/vecgrep"

[agent]
enabled = true
command = "opencode"
args = ["acp"]
# OS-level policy for ACP-created terminals: off, auto, or required.
sandbox = "auto"

[session]
enabled = true
auto_save_interval = 30
```

Available themes are `nord`, `dracula`, `catppuccin`, `solarized-dark`, and
`one-dark`. `auto_save_interval` is measured in seconds. `agent.sandbox` applies
to terminal processes created by the ACP agent: `off` keeps the logical Teak
authorization checks without an OS wrapper, `auto` uses macOS Seatbelt when
available and reports fallback status, and `required` refuses to start a
terminal when no supported backend is available. The ACP transport itself is
not wrapped because the configured agent may need its own network connection;
the terminal child still receives only the active run's effective write and
network capabilities.

Custom language servers can be configured by extension:

```toml
[[lsp]]
extensions = [".go"]
command = "gopls"
language_id = "go"
# Optional variables are passed only to this server process.
env = { GOWORK = "off" }
```

`env` is bounded and validated; names cannot contain `=` or NUL bytes. This is
useful for virtual environments and language-server wrappers without changing
Teak's own process environment.

## Optional tools

Teak remains usable when an optional tool is missing. When possible, it reports
what to install and periodically checks again, so newly installed tools work
without restarting the editor.

| Tool | Enables | Fallback |
|------|---------|----------|
| [`rg`](https://github.com/BurntSushi/ripgrep) | Fast, `.gitignore`-aware project search | Built-in recursive search |
| Language servers (`gopls`, `typescript-language-server`, …) | LSP code intelligence | Editing without language features |
| [`vecgrep`](https://github.com/abdul-hamid-achik/vecgrep) | Semantic project search | Text project search |
| [`dlv`](https://github.com/go-delve/delve) | Go debugging | Debugger reports the missing adapter |
| `opencode acp` | AI agent panel | Editing without agent assistance |

External tools are resolved from `PATH` and common locations often omitted by
non-login shells, including Homebrew, `asdf`/`mise` shims, `~/go/bin`,
`~/.cargo/bin`, and `~/.local/bin`.

Use `[tools]` when a tool is installed in a project-specific or otherwise
non-standard location. Overrides are normalized to absolute paths and only
select an executable; Teak never runs an install command or a shell because of
this setting. Missing or broken overrides remain visible in `teak doctor` and
headless health output.

## Terminal compatibility

Teak requires interactive stdin and stdout and a terminal with cursor support.
It rejects `TERM=dumb` and redirected I/O with a diagnostic. Bubble Tea handles
terminal color detection, and `NO_COLOR=1` disables color.

Set the following environment variable if your terminal does not use a Nerd
Font:

```sh
TEAK_NO_NERD_FONT=1 teak
```

To keep copy and paste inside the Teak process instead of using the operating
system clipboard, set `TEAK_CLIPBOARD=internal`.

## Lua plugins

Teak loads each plugin from a directory containing `plugin.toml` and a Lua
entrypoint such as `init.lua`.

| Platform | Plugin directory |
|----------|------------------|
| macOS | `~/Library/Application Support/teak/plugins/` |
| Linux | `$XDG_CONFIG_HOME/teak/plugins/` or `~/.config/teak/plugins/` |
| Windows | `%AppData%\\teak\\plugins\\` |

Minimal layout:

```text
plugins/
  my-plugin/
    plugin.toml
    init.lua
```

```toml
# plugin.toml
name = "my-plugin"
main = "init.lua"
api_version = 1
event_version = 2
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

  autocmd.register("CursorMoved", update_status)
end
```

`setup()` may register commands, mappings, and autocommands. Runtime operations
such as `buffer.*`, `editor.set_status`, `ui.new_buffer`, and `ui.notify` are
available only inside a command, mapping, or autocommand callback—not directly
inside `setup()`.

`ui.new_buffer()` creates and focuses a new untitled tab and returns its 1-based
tab/buffer number. In asynchronous callbacks, tab focus and buffer edits are
replayed in the same order as Lua issued them, so `ui.new_buffer()` or
`editor.set_active_tab()` can safely precede `buffer.set_text()` in one callback.
`editor.open_file()` also creates a tracked loading destination; an edit issued
immediately afterward is preserved if the file load later detects the change.
Panel visibility and notifications are also wired through
`ui.show_panel`, `ui.hide_panel`, `ui.toggle_panel`, and `ui.notify`.
`ui.confirm(message, options, callback)` is non-blocking; the callback receives
the selected option, its one-based index, and an accepted boolean. Float
widgets are bounded read-only panels: `ui.new_float({title, content, width,
height})` returns an ID, `ui.close_float(id)` closes it, and Enter/Escape
dismiss the visible panel. `ui.set_highlights(namespace, ranges)` replaces a
bounded highlight namespace for the active buffer; ranges use 0-based byte
columns and expire after the buffer changes. `ui.clear_highlights(namespace)`
clears one namespace, while `ui.clear_highlights()` clears all plugin ranges.
`ui.input(prompt, callback)`
and
`ui.input(prompt, initial, callback)` are also non-blocking; the callback
receives `(value, accepted)` and Escape reports `accepted == false`.
`ui.select(prompt, options, callback)` opens the same fuzzy picker used by the
editor and resumes with `(option, index, accepted)`; Escape reports index `0`
and `accepted == false`.

```lua
ui.confirm("Run tests?", { "Run", "Cancel" }, function(option, index, accepted)
  if accepted and index == 1 then
    editor.set_status("Tests requested")
  end
end)

ui.input("Branch name", "feature/", function(value, accepted)
  if accepted then
    editor.set_status("Creating " .. value)
  end
end)

ui.select("Target", { "current file", "workspace" }, function(option, index, accepted)
  if accepted then
    editor.set_status("Selected " .. option)
  end
end)

ui.set_highlights(1, {
  { line = 0, start_col = 0, end_col = 5, fg = "#88c0d0", bold = true },
})
```

The plugin surface includes `buffer`, `editor`, `keymap`, `autocmd`, and `ui`.
The `teak` metadata module exposes the stable Lua contract:
`teak.api_version()` and `teak.event_version()` return contract versions,
`teak.capabilities()` and `teak.has_capability(name)` support feature detection,
`teak.ui_capabilities()` and `teak.has_ui_capability(name)` expose the UI
functions that are safe in the current synchronous and asynchronous bridge,
`keymap.list(mode?)` returns bounded mapping metadata in stable mode/key order;
`keymap.which_key(keys, mode?)` resolves a description for a specific mode and
falls back to the `a` mapping. `teak.events()` lists the supported non-Vim
autocommand names, and
`teak.has_event(name)` checks one event without scanning the list. The current
API contract is version `1` and the event contract is version `2`; a plugin
should check them before using optional APIs.
The event list contains only hooks Teak actually dispatches; there is no
insert-mode hook or Vim emulation.
The optional `api_version` and `event_version` manifest fields make that
requirement machine-checkable: a plugin declaring a newer contract is rejected
before its Lua code runs, while older manifests remain compatible.
The sandbox withholds `io`, `os`, `package`, `debug`, coroutines, filesystem
loading, and process execution. Runtime editor operations remain available only
inside command, mapping, or autocommand callbacks. See [the runnable plugin
examples](examples/plugins/README.md) for installation steps and sample
commands.

## Architecture

Teak follows the Elm-style Model-View-Update pattern: state changes flow through
typed Bubble Tea messages, and blocking or file-sized work runs in commands.

```text
cmd/teak/           CLI and application entrypoint
internal/
  app/              Root model, message routing, coordinators, file watching
  text/             Immutable rope, buffer, selections, undo/redo
  editor/           Viewport, tabs, splits, overlays, folding, highlighting UI
  filetree/         Lazy file explorer and filesystem actions
  highlight/        Chroma tokenization and viewport cache
  lsp/              JSON-RPC client, server manager, document synchronization
  search/           Text, ripgrep, and semantic project search
  git/              Repository status and commit panel
  dap/, debugger/   Debug adapter client and debugger UI
  acp/, agent/      Agent protocol client and chat panel
  plugin/           Sandboxed Lua plugin runtime and APIs
  problems/, diff/  Diagnostics and diff presentation
  config/, settings/ Configuration loading, validation, and settings UI
  session/          Per-workspace session persistence
  toolpath/         External command resolution and install hints
  clipboard/, ui/   Platform integration, themes, and shared UI primitives
```

The editor buffer uses a persistent, immutable rope. Inserts and deletes return
a new tree, which makes undo/redo efficient without copying the whole document.
Line lookups descend through per-node newline counts instead of rebuilding a
document-wide index after each edit.

Search, syntax tokenization, file I/O, and protocol requests run outside the UI
update path. Asynchronous results carry versions or generations so superseded
work cannot replace newer editor state.

## Development

Use the Task runner or Go directly:

```sh
# Task
task build
task test
task doctor
task context -- --json --root .
task check
task coverage
task verify
task perf-tools
task perf-tools-test
task run -- main.go

# Go
go build ./...
go test ./...
go test -race ./...
go vet ./...
```

Before opening a pull request, keep the build, tests, race detector, and vet
checks green. See [AGENTS.md](AGENTS.md) for architectural invariants and test
conventions, and [CHANGELOG.md](CHANGELOG.md) for notable changes.

## License

MIT
