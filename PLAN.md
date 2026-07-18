# Teak Refactoring Plan

This plan tracks the refactoring of the 9 largest source files in Teak, reducing total lines by ~45% while improving maintainability, testability, and separation of concerns.

## Overall Goal

| Metric | Before | After | Reduction |
|--------|--------|-------|-----------|
| Total Lines | 13,034 | 5,800 | 55% |
| New Packages | 0 | 10 | - |

---

## Progress: [Major phases complete, remaining items deferred due to coupling]

### Completed Work:
- ✅ Phase 1.1-1.2: modes/, keybindings/ packages
- ✅ Phase 1.2.3: context.go - Focus area contexts
- ✅ Phase 1.3: TabManager, LayoutManager, OverlayManager (exist in separate files)
- ✅ Phase 1.4: fileops/ package (expanded with delete.go, open.go, save.go, close.go, path helpers, tests)
- ✅ Phase 2: lsp/capabilities/ package + integration into client.go
- ✅ Phase 2.2: rpc_gen.go - 10 RPC methods moved to separate file
- ✅ Phase 3.1: git/treebuilder/ restored, git/types.go - shared types extracted
- ✅ Phase 3.2: git/hittest/ package (hit.go - click detection)
- ✅ Phase 5.1: viewport_scroll.go created (scroll/cursor functions extracted)
- ✅ Phase 5.2: editor/overlays/ package with autocomplete, hover, signature (95.7% coverage)
- ✅ Phase 6: agent/types.go and dap/types.go created - shared and protocol types extracted

### Deferred (tightly coupled to parent):
- ⚠️ Phase 1.5: app_init.go extraction (too invasive - 5009 lines, 82 methods)
- ⚠️ Phase 3.3: git/render.go extraction (rendering tightly coupled to Model)
- ⚠️ Phase 5.1: viewport_render.go, viewport_bracket.go (tightly coupled to viewport)
- ⚠️ Phase 4: Buffer refactoring (high risk due to coupling)

### Testing Coverage Added:
- internal/app/fileops/ - 32 tests (79.1% coverage, up from 29.9%)
- internal/editor/overlays/ - 43 tests (95.7% coverage)
- internal/app/keybindings/ - 12 tests (binding, registry)
- internal/app/modes/ - 4 tests (mode interface)
- internal/git/hittest/ - 11 tests (hit testing)
- internal/lsp/capabilities/ - 17 tests (capability checking)
- internal/git/treebuilder/ - 11 tests + 6 benchmarks (90.7% coverage)

### Benchmarks Added:
- internal/git/treebuilder/ - 6 benchmarks (Build operations)

---

## Phase 1: app.go Refactoring (Week 1-2)

**Goal:** Reduce `internal/app/app.go` from 5,009 → ~1,200 lines (76% reduction)

### Tasks

- [x] **1.1** Create `internal/app/modes/` package
  - [x] 1.1.1 `mode.go` - Mode interface + base types
  - [x] 1.1.2 `normal.go` - Normal editing mode
  - [x] 1.1.3 `rename.go` - Rename input mode
  - [x] 1.1.4 `goto_line.go` - Go-to-line dialog
  - [x] 1.1.5 `search.go` - Search overlay mode
  - [x] 1.1.6 `input.go` - New file/folder input mode
  - [x] 1.1.7 `confirm.go` - Confirmation dialogs
  - [x] 1.1.8 `context_menu.go` - Context menu mode
  - [x] 1.1.9 `branch_picker.go` - Git branch picker
  - [x] 1.1.10 `settings.go` - Settings overlay
  - [x] 1.1.11 `manager.go` - Mode manager

- [x] **1.2** Create `internal/app/keybindings/` package
  - [x] 1.2.1 `binding.go` - Individual binding definition
  - [x] 1.2.2 `registry.go` - Key binding registry
  - [x] 1.2.3 `context.go` - Focus area contexts
  - [x] 1.2.4 `default.go` - Default keybindings for Teak

- [x] **1.3** Strengthen Managers
  - [x] 1.3.1 Enhance `TabManager` - exists in tab_manager.go
  - [x] 1.3.2 Enhance `LayoutManager` - exists in layout_manager.go
  - [x] 1.3.3 Create `OverlayManager` - exists in overlay_manager.go

- [x] **1.4** Extract File Operations to `internal/app/fileops/`
  - [x] 1.4.1 `fileops.go` - File operation interface
  - [x] 1.4.2 `open.go` - Open file logic
  - [x] 1.4.3 `save.go` - Save file logic
  - [x] 1.4.4 `close.go` - Close file logic
  - [x] 1.4.5 `delete.go` - Delete file logic

- [ ] **1.5** Split app.go into Multiple Files
  - [ ] 1.5.1 `app_update.go` - Update() implementation (~800 lines)
  - [ ] 1.5.2 `app_view.go` - View() implementation (~600 lines)
  - [ ] 1.5.3 `app_init.go` - NewModel(), session handling (~400 lines)
  - [ ] 1.5.4 `app_lsp.go` - LSP-specific handlers (~300 lines)
  - [ ] 1.5.5 `app_dap.go` - DAP-specific handlers (~200 lines)
  - [ ] 1.5.6 `app_acp.go` - ACP-specific handlers (~200 lines)
  - [ ] 1.5.7 `app_tabs.go` - Tab bar handlers (~150 lines)
  - [ ] 1.5.8 `app_search.go` - Search handlers (~150 lines)
  - [ ] 1.5.9 `app_git.go` - Git panel handlers (~150 lines)
  - [ ] 1.5.10 `app_debugger.go` - Debugger handlers (~150 lines)
  - [ ] 1.5.11 `app_agent.go` - Agent panel handlers (~150 lines)
  - [ ] 1.5.12 `app_helpers.go` - Small helper functions (~200 lines)

---

## Phase 2: LSP Client Refactoring (Week 3)

**Goal:** Reduce `internal/lsp/client.go` from 1,288 → ~500 lines (61% reduction)

**Status:** Phase 2 complete - RPC methods moved to `rpc_gen.go`, capabilities integrated

### Tasks

- [x] **2.1** Extract Capability Checker to `internal/lsp/capabilities/
  - [x] 2.1.1 `capabilities.go` - Capability checking methods

- [x] **2.2** Create RPC Method Generator
  - [x] 2.2.1 `rpc_methods.yaml` - Method configuration (simplified to direct implementation)
  - [x] 2.2.2 `generate/main.go` - Generator script (not needed - direct implementation)
  - [x] 2.2.3 `rpc_gen.go` - Generated RPC methods

- [x] **2.3** Simplify `client.go`
  - [x] 2.3.1 Remove 10 RPC methods (now in rpc_gen.go)
  - [x] 2.3.2 Delegate capability checks to capabilities pkg

---

## Phase 3: Git Panel Refactoring (Week 4)

**Goal:** Reduce `internal/git/panel.go` from 1,504 → ~800 lines (47% reduction)

**Status:** Phase 3.1-3.2 complete - treebuilder restored, hittest created

### Tasks

- [x] **3.1** Extract Tree Builder to `internal/git/treebuilder/`
  - [x] 3.1.1 `tree.go` - Tree building logic (restored - types extracted to git/types.go)

- [x] **3.2** Extract Hit Testing to `internal/git/hittest/`
  - [x] 3.2.1 `hit.go` - Click detection logic (created hittest.go)

- [ ] **3.3** Extract Rendering
  - [ ] 3.3.1 `render.go` - View rendering logic (deferred: tightly coupled to Model)

---

## Phase 4: Buffer Refactoring (Week 5)

**Goal:** Reduce `internal/text/buffer.go` from 1,355 → ~900 lines (34% reduction)

### Tasks

- [ ] **4.1** Split buffer.go
  - [ ] 4.1.1 `buffer_edit.go` - Edit operations (~300 lines)
  - [ ] 4.1.2 `buffer_cursor.go` - Cursor movement (~200 lines)

- [ ] **4.2** Extract Multi-Cursor to `internal/text/multicursor/`
  - [ ] 4.2.1 `buffer.go` - Multi-cursor extensions (~250 lines)

- [ ] **4.3** Extract LSP Sync
  - [ ] 4.3.1 `lsp_sync.go` - LSP incremental sync (~150 lines)

---

## Phase 5: Editor Refactoring (Week 5-6)

**Goal:** Reduce editor files

### Tasks

- [x] **5.1** Refactor `internal/editor/viewport.go` (1,006 → ~650 lines)
  - [ ] 5.1.1 `viewport_render.go` - Rendering strategies (~300 lines) - deferred: tightly coupled
  - [x] 5.1.2 `viewport_scroll.go` - Scrollbar logic (~150 lines) - created
  - [ ] 5.1.3 `viewport_bracket.go` - Bracket matching (~100 lines) - deferred: tightly coupled

- [x] **5.2** Extract Overlays to `internal/editor/overlays/` package
  - [x] 5.2.1 `overlay.go` - Overlay interface (reference)
  - [x] 5.2.2 `autocomplete.go` - Autocomplete overlay (moved to overlays/)
  - [x] 5.2.3 `hover.go` - Hover overlay (moved to overlays/)
  - [x] 5.2.4 `signature.go` - Signature help overlay (moved to overlays/)
  - [x] Integration completed - editor.go and app.go updated to use overlays package
  - [x] Tests added - 95.7% coverage

---

## Phase 6: Agent & DAP Refactoring (Week 6)

**Goal:** Reduce agent and DAP files

**Status:** Phase 6 complete - types extracted to separate files

### Tasks

- [x] **6.1** Refactor `internal/agent/panel.go` (1,096 → ~1,000 lines)
  - [x] 6.1.1 `types.go` - Shared types extracted (ChatRole, ChatMessage, ToolCallState, etc.)

- [x] **6.2** Refactor `internal/dap/client.go` (839 → ~650 lines)
  - [x] 6.2.1 `types.go` - Protocol types extracted (Request, Response, Event, etc.)

---

## Phase 7: Testing & Polish (Week 7)

### Tasks

- [x] **7.1** Ensure all tests pass
  - [x] keybindings tests added
  - [x] modes tests added
  - [x] hittest tests added
  - [x] capabilities tests added
- [x] **7.2** Benchmark performance
  - [x] treebuilder benchmarks added
- [ ] **7.3** Update documentation
- [ ] **7.4** Remove this PLAN.md when complete

---

## Notes

- Test files are acceptable as-is (e.g., dap_test.go at 1,606 lines)
- Code generation reduces boilerplate for LSP/DAP RPC methods
- Each phase builds on previous - start with Phase 1
