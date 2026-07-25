# Changelog

All notable changes to the Teak editor project.

## [Unreleased]

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
