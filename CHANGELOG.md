# Changelog

All notable changes to the Teak editor project.

## [Unreleased]

## [0.10.8] - 2026-08-05

### Editor State Synchronization

- Plugin-driven edits and cursor moves now pass through the same central
  reconciliation path as interactive edits, cancelling stale completion,
  hover, signature, and cursor-sensitive document requests.
- External file reloads now use that reconciliation path as well, keeping
  request invalidation, dirty/preview tab state, LSP `didChange`, and editor
  autocmds consistent instead of maintaining a partial duplicate.

## [0.10.7] - 2026-08-05

### Headless LSP Teardown

- Successful short-lived headless LSP queries no longer print false
  `shutdown deadline exceeded` errors when the bounded graceful handshake
  falls back to forced process teardown.
- Cancel-notification failures caused by an already-closing transport are now
  treated as expected only during shutdown; the same failures remain visible
  while a client is running, as do unexpected teardown errors.

## [0.10.6] - 2026-08-05

### LSP Compatibility

- Document-symbol requests now support both response shapes allowed by LSP:
  hierarchical `DocumentSymbol` values and flat `SymbolInformation` values.
  Servers such as `gopls` no longer produce symbol pickers and headless output
  whose names are correct but whose navigation ranges are all zero.
- Flat symbol locations pass through the negotiated position-encoding
  conversion, and their container name is retained as picker detail.

## [0.10.5] - 2026-08-05

### LSP Cancellation

- Completion, hover, signature help, definition, references, code actions,
  rename, symbols, and folding requests now accept caller cancellation while
  retaining their existing request-specific timeout ceilings.
- Superseded requests are latest-wins: Teak sends `$/cancelRequest` instead of
  leaving obsolete work running on the language server until timeout.
- Edits cancel all requests for the affected document. Cursor-only movement
  cancels position-dependent requests while allowing document symbols and
  folding ranges to finish, and tab switches cancel active-document work.
- Definition, references, code actions, and rename results now carry and
  validate their originating cursor position in addition to file, version,
  and generation identity.
- Headless hover, definition, references, symbols, and formatting commands now
  pass their operation context into the underlying LSP request.

## [0.10.4] - 2026-08-05

### LSP Reliability

- Completion, hover, and signature-help responses now carry the cursor
  position that originated the request. Moving without editing invalidates a
  late response instead of rendering or applying it at a different position.
- Code-action requests now convert and copy the applicable diagnostics while
  still on the Bubble Tea event loop. Background commands no longer retain an
  editor pointer or observe a later diagnostics publication.

## [0.10.3] - 2026-08-05

### In-buffer Find Reliability

- Closing the find widget now cancels its active scan and invalidates queued
  results, so a late result cannot move the cursor after `Esc`.
- Editing a query or toggling regex mode clears stale highlights immediately,
  shows an explicit searching state, and reports invalid regex syntax instead
  of presenting it as “No matches”.
- Reopening a preserved query starts a fresh scan rather than showing an empty
  result set until the query changes again.
- Asynchronous scans now cover the complete 64 MiB editor file limit, are
  canceled when superseded, and pass only the remaining 10,000-match budget to
  the regexp engine so dense single-line files cannot create unbounded match
  index allocations.

### Performance

- Visible find highlights now locate the viewport with binary search instead
  of walking every preceding match. A 10,000-match deep-viewport benchmark on
  Apple M5 improved from roughly 6.1–6.9 µs and 23.6 KB/5 allocations to
  1.3–1.6 µs and 6.9 KB/1 allocation per render.

## [0.10.2] - 2026-08-05

### Reliability

- Semantic-search waiters no longer restart a `vecgrep index` process after
  its hard timeout expires or application shutdown cancels it. Every caller
  sharing that setup flight now receives the same terminal outcome.
- Application teardown cancels the active text or semantic search before it
  stops shared indexers, preventing a late search command from starting new
  subprocess work during shutdown.
- The interactive-index timeout regression test now leaves realistic process
  startup headroom under a busy full-suite run while still proving bounded
  subprocess termination.
- Failed LSP document reopens are now surfaced through the editor instead of
  being discarded, and format-on-save reports synchronization failures rather
  than treating them as successful no-ops.
- Doctor and human-readable headless commands now detect output write failures
  and return a runtime-failure exit code instead of reporting false success.

### Maintenance

- Cleared the repository's full `golangci-lint` backlog, including unchecked
  I/O and subprocess errors, ineffective assignments, stale helpers, and
  intentional nil-context contract tests that now document their exemptions.
- Corrected `github.com/charmbracelet/ultraviolet` to be recorded as a direct
  module dependency, keeping `go mod tidy -diff` clean.

## [0.10.1] - 2026-08-04

### 2026-08 Codebase Audit — Fix Clusters

A read-only multi-agent audit of the editor landed fixes in four clusters.

#### Data Integrity (P0)
- Watcher watch-limit errors surface in the status bar; Linux `ENOSPC` is treated as a watch-limit error.
- Crash recovery persists dirty and untitled buffers and restores them as dirty tabs; cleared on clean exit.
- CRLF is normalized to LF in memory and restored on save across load, save, external-change, and session paths.
- Multi-cursor backspace/delete edit every cursor and rebase the rest; indent/dedent/comment-toggle rebase cursor and selections.
- Replace honors regex/case options; session restore surfaces and retains tabs it cannot read.
- Settings save patches managed keys in place, keeping comments and unknown sections.

#### Visible Feedback (P1)
- Find-widget matches are highlighted in the text; the current match gets a distinct style.
- LSP diagnostics underline their ranges in the text; the first diagnostic under the cursor echoes in the status bar.
- Autocomplete filters items as you type, closes when nothing matches, and dismisses on navigation.
- Clipboard copy falls back to OSC 52 over SSH (or `TEAK_OSC52=force`).

#### UX Polish (P2)
- Full command palette: format, go-to-definition, rename, code actions, hover, symbols, splits, folds, tab/problem navigation, and Restart Language Server.
- Hover popup wraps long lines and dismisses when the cursor leaves the anchor.
- A crashed language server restarts automatically (with a restart cap) and re-sends open documents.
- `editor.scroll_margin` (default 2) keeps the cursor off viewport edges; status bar shows the display column.
- Confirm dialogs gain digit/y/n accelerators; tab-bar wheel scrolls the strip; sidebar divider is draggable and persists `ui.tree_width`.
- Debugger control buttons and breakpoint rows are clickable; unknown `config.toml` keys are reported at startup.

#### Headless Server Mode
- New `teak headless` REST server (LSP, DAP, codemap, semantic search, MCP, project flows) plus `teak doctor` and the workspace health dashboard, with cancellation, quotas, and write-lock handling.

#### Known Issues
- Agent panel on Linux: the ACP agent connects but the TUI exits early under a Linux PTY, so the three `tui_agent_*` glyphrun specs fail there. They are excluded from Linux runs (CI and Taskfile gates) until root-caused; they pass on macOS.

### Performance Improvements

#### File Tree Rendering
- **Optimized style allocations** in `internal/filetree/filetree.go`
- Added cached styles struct to Model to avoid per-frame `lipgloss.NewStyle()` allocations
- Reduced allocation overhead in hot rendering paths
- Benchmarks added: `internal/filetree/filetree_bench_test.go`

#### Gutter Rendering  
- **Pre-cached theme styles** for breakpoints and execution line markers
- Added 5 new styles to Theme struct:
  - `BreakpointActive`
  - `BreakpointDisabled` 
  - `ExecLineMarker`
  - `FoldCollapsed`
  - `FoldExpanded`
- All theme variants updated: Nord, Dracula, Catppuccin, Solarized Dark, One Dark
- Replaced inline `lipgloss.NewStyle()` calls with theme style references

#### Viewport Rendering
- **Critical fix: Eliminated rune-by-rune styling** in `renderWrapSegment()`
- **Before:** Styled EACH CHARACTER individually → 1000+ allocations per wrapped line
- **After:** Styles segments by token boundaries → ~10 allocations per wrapped line
- **Impact:** 90-95% reduction in allocations for wrapped text rendering
- Added `extractWidthRange()` helper for efficient text extraction by display width

#### Editor Responsiveness on Large Files
- **Removed the rope's line-index cache**, which was rebuilt from a full document copy on every keystroke, in favor of an incremental tree descent
  - **Before:** ~2.2 ms and 6.9 MB of allocation per keystroke on a 100k-line file
  - **After:** ~4 us with no allocation
- **Precomputed terminal escape sequences for syntax-highlighted tokens** instead of running each one through the full style pipeline on every render
  - A 48-line tokenized viewport went from ~1.9 ms / 16,605 allocations to ~376 us / 3,680 allocations
- Undo now groups a run of typed characters into a single undo step again, instead of one snapshot per keystroke, also shrinking undo-stack memory use
- Typing no longer flashes the buffer to plain text for ~150 ms while it re-tokenizes
- In-buffer find no longer scans the whole document synchronously on every keystroke (previously ~13 ms and 210k allocations per character on a 50k-line file); searching is now debounced

### Bug Fixes

#### Git Panel Zone Collision
- Fixed zone ID collision in commit body rendering
- Removed redundant `zone.Mark()` calls that were being overwritten
- Commit body clicks now work correctly via positional hit testing

#### Focus, Split View, Folding and Completions
- Fixed focus handling across the app so the agent panel, git commit box, and sidebar no longer lose or misdirect keyboard input after switching focus areas or reopening the sidebar
- Fixed Ctrl+W/F/B/H/K closing the current tab or triggering editor actions instead of doing normal text editing while typing in the git commit message box
- Fixed split view so both panes no longer show the same buffer when switching focus, and switching focus back to the first pane works again; panes are now sized correctly instead of clipping the cursor out of view, and clicks in the second pane no longer land in the first
- Fixed the viewport rendering blank after pressing an arrow key immediately following a fold collapse
- Fixed accepting a completion sometimes producing garbled text (e.g. typing "fm" and accepting "fmt" could yield "fmfmt")
- Fixed completions and edits made via mouse (completion list, context menu) not being reported to the language server, which let its view of the file fall out of sync
- Fixed right-clicking the sidebar opening an invisible menu that still captured keyboard/mouse input
- Fixed a crash when pressing F12, Ctrl+K, Ctrl+Shift+O, or Ctrl+G while no files were open
- Fixed the cursor and selection being able to end up past the end of the file after certain whole-document edits

#### Language Servers
- Language servers that fail to start are now reported with an install command instead of the editor claiming "LSP ready" and then doing nothing
- A language server that failed repeatedly is no longer disabled for the rest of the session; it now retries after a cooldown, so installing it or fixing `PATH` takes effect without restarting Teak

#### External Tool Discovery
- External tools (formatters, linters, language servers, etc.) installed via Homebrew, asdf/mise, or `~/go/bin` are now found reliably even when Teak inherited an older `PATH`, such as in a long-running terminal session
- A configured tool path override that doesn't exist is now reported as an error instead of silently running a different binary
- Missing-tool messages now include an install hint instead of just "not found"

#### Project Search
- Fixed project search silently dropping matches already found in a file if it reached a line with invalid UTF-8

#### Saving Files
- Saving a file marked read-only is now refused with an explanation instead of silently overwriting it — an atomic rename only needs permission on the directory, so the file's own protection was being bypassed
- Symlinked files can now be saved. Teak already opened them, so a symlinked dotfile (stow, chezmoi) could be edited but never written back; the link is followed to its target and the link itself is preserved

#### Plugins
- Lua plugins now have the standard `string`, `table`, `math` and base libraries available. They previously ran with none, so `string.format`, `pcall`, `pairs` and `tostring` were all missing and the plugin example in the README failed on its first run. Filesystem, process, module-loading and introspection access remain withheld

### Features

#### Project Search via ripgrep
- Project-wide text search now uses `ripgrep` when it's available on `PATH`, for faster results and proper `.gitignore`-aware filtering; falls back automatically to Teak's built-in search when `ripgrep` isn't installed

#### Git Commit Body Textarea
- **Replaced custom `[]string` implementation** with `bubbles/textarea` component
- **Benefits:**
  - Standard text editing with word wrap, scrolling, cursor handling
  - Better UX with familiar text editing behavior
  - Reduced code complexity (~50 lines removed)
- **Changes:**
  - `commitBody` field changed from `[]string` to `textarea.Model`
  - Removed custom cursor management (`bodyLine`, `bodyCol`, `bodyScrollX`, `bodyScrollY`)
  - Removed custom editing handlers (enter, backspace, arrows)
  - Simplified Update() to delegate to textarea component
  - Updated View() to use `textarea.View()`

### Code Cleanup

- **Removed binaries:** Cleaned up `bin/teak` (25MB) and root `teak` binary
- **Updated .gitignore:** Added `.opencode/`, `.crush/`, `.claude/` directories
- **Removed obsolete code:**
  - `scrollBodyIntoView()`
  - `scrollBodyHorizontally()`
  - `bodyContentWidth()`

### Testing

- Added comprehensive benchmark suite:
  - `internal/filetree/filetree_bench_test.go` - 6 benchmarks
  - `internal/editor/gutter_bench_test.go` - 4 benchmarks  
  - `internal/editor/viewport_bench_test.go` - 6 benchmarks
- Updated git panel tests for textarea integration
- All 21 packages pass tests
- No test coverage regressions

### Files Changed

```
M .gitignore
M internal/editor/gutter.go
M internal/editor/viewport.go
M internal/filetree/filetree.go
M internal/git/panel.go
M internal/git/panel_test.go
M internal/ui/theme.go
A internal/editor/gutter_bench_test.go
A internal/editor/viewport_bench_test.go
A internal/filetree/filetree_bench_test.go
```

---

## Change Log Format

This changelog follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/) format.

### Types of changes
- **Added** for new features
- **Changed** for changes in existing functionality
- **Deprecated** for soon-to-be removed features
- **Removed** for now removed features
- **Fixed** for any bug fixes
- **Security** for security-related changes
