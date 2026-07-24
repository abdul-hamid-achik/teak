# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```sh
task build                          # Build binary to bin/teak
task test                           # Run all tests
task run -- file.go                 # Build and run with a file
task clean                          # Remove build artifacts

go build -o bin/teak ./cmd/teak     # Build directly
go test ./...                       # Test directly
go test ./internal/text/...         # Test a single package
go test ./internal/text/ -run TestRopeInsert  # Run a single test
```

## Architecture

Teak is a terminal code editor using the Bubbletea v2 Elm architecture (Model-View-Update). All state changes flow through typed messages and `Update()` methods. Heavy operations (file I/O, LSP, tokenization, search) run as async `tea.Cmd` functions.

### Message Flow

The root model in `internal/app/app.go` receives all messages and routes them:
- `tea.KeyPressMsg` → routed by `handleKeyPressPrecedence` in `internal/app/input_routing.go`. The order is: unsaved-changes confirm > `overlayStack` > branch picker > search > go-to-line > rename > save-as > new-item > delete-confirm > tree context menu > git context menu > help > settings > plugin keys > focused panel. Read the function rather than trusting this list — it is the only authority.
- `tea.MouseClickMsg` / `tea.MouseWheelMsg` → routed by overlay state, then by X coordinate (tree vs editor)
- Sub-model messages (e.g. `filetree.OpenFileMsg`, `search.OpenResultMsg`, `lsp.DiagnosticsMsg`) → handled directly in the root Update switch

### Focus System

`FocusArea` controls keyboard routing: `FocusEditor`, `FocusTree`, `FocusGitPanel`, `FocusProblems`, `FocusDebugger`, `FocusAgent`. The active area receives unhandled key events.

**Always change focus through `Model.setFocus`, never by assigning `m.focus`.** It releases what the previous area held — blurring the agent input, unfocusing the git commit form. Direct assignment was the source of a phantom caret in the agent panel and a commit box that silently swallowed navigation keys.

Global shortcuts that collide with `bubbles` text-input bindings (`ctrl+w/f/b/h/k`) must be guarded with `Model.textInputFocused()`; the global handler runs before focused-panel routing.

### Key Packages

- **`internal/text/`** — Persistent immutable rope (512-byte leaves, Fibonacci balancing). `Insert`/`Delete` return new `*Rope`, never mutate. Buffer wraps rope with cursor, selection, undo/redo stack.
- **`internal/app/`** — Root model coordinating all components. `app.go` is the central hub (~5100 lines). `watcher.go` uses fsnotify for external file change detection.
- **`internal/editor/`** — Editor viewport, gutter, tab bar, autocomplete, hover, context menu, help overlay, welcome screen. Each is a separate sub-model.
- **`internal/highlight/`** — Chroma v2 highlighter with two modes: `TokenizeToLines()` for full file (edit-triggered), `TokenizeViewportToLines()` for visible lines only (scroll-triggered). `SetLines()` **replaces** the cache wholesale; `MergeLines()`/`MergeBatch()` are the merging variants. `InvalidateEdited()` keeps tokens on screen across an edit while clearing coverage, so typing does not flash the buffer to plain text; `Invalidate()` drops everything.
- **`internal/toolpath/`** — Resolves every external binary (`rg`, `git`, language servers, `codemap`, `vecgrep`, …). Never call `exec.LookPath` or hand a bare name to `os/exec` directly.
- **`internal/lsp/`** — JSON-RPC 2.0 client over stdin/stdout. Manager lazily starts per-language servers. Configs in `config.go`.
- **`internal/filetree/`** — Lazy-loaded tree with async directory expansion. Nerd Font v3 icons in `icons.go`.
- **`internal/search/`** — Project-wide text search backed by ripgrep when `rg` resolves, falling back to a pure-Go walker otherwise; semantic search via `vecgrep`. Input is debounced and results are generation-tagged so a superseded query cannot overwrite newer ones.
- **`internal/git/`** — Sidebar panel using `git status --porcelain`.
- **`internal/ui/`** — Nord color palette (`Nord0`–`Nord15` as `color.Color` vars) and `Theme` struct with 30+ lipgloss styles.

## Code Conventions

- **Go 1.26+**, use standard library where possible
- **0-based line and column indexing** throughout the entire codebase
- **Rope is immutable**: `Insert`/`Delete` return new `*Rope`. It carries **no line-offset table** — `LineStart` descends the tree using per-node newline counts. A per-root table was rebuilt from a full document copy after every keystroke (2.2 ms and 6.9 MB on a 100k-line file); do not reintroduce one.
- **Return errors, don't panic**
- **Nothing proportional to document size may run inside `Update()`** — no full-buffer scans, no rope materialization, no `exec`. Use a `tea.Cmd` with a generation counter so stale results are discarded.
- **Table-driven tests** in `_test.go` files alongside source

## Lipgloss v2 Pitfall

In Lipgloss v2, `lipgloss.Color()` is a **function** returning `color.Color` (from `image/color`), not a type. All color values in the codebase are `color.Color`. The Nord palette is defined as package-level vars in `internal/ui/theme.go`.

## Dependencies

- Bubbletea v2: `charm.land/bubbletea/v2`
- Lipgloss v2: `charm.land/lipgloss/v2`
- Bubbles v2: `charm.land/bubbles/v2` (textinput, spinner)
- BubbleZone v2: `github.com/lrstanley/bubblezone/v2` (mouse zones)
- Chroma v2: `github.com/alecthomas/chroma/v2` (syntax highlighting)
- fsnotify: `github.com/fsnotify/fsnotify` (file watching)
