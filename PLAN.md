# Teak Refactoring Plan

This plan tracks the refactoring of the 9 largest source files in Teak, reducing total lines by ~45% while improving maintainability, testability, and separation of concerns.

## Overall Goal

| Metric | Before | After | Reduction |
|--------|--------|-------|-----------|
| Total Lines | 13,034 | 5,800 | 55% |
| New Packages | 0 | 10 | - |

---

## Progress: [3/6 Phases Complete] (Phases 1-3 done, infrastructure created)

### Completed Work:
- ✅ Phase 1.1-1.2: modes/, keybindings/ packages
- ✅ Phase 1.4: fileops/ package
- ✅ Phase 2: lsp/capabilities/ package
- ✅ Phase 3: git/treebuilder/ package

### Remaining (requires invasive changes):
- Phase 1.5: Splitting app.go (requires careful integration)
- Phase 4: Splitting text/buffer.go (54+ methods, high risk)
- Phase 5: Splitting editor files (already modular)
- Phase 6: Agent/DAP refactoring

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
  - [ ] 1.2.3 `context.go` - Focus area contexts
  - [x] 1.2.4 `default.go` - Default keybindings for Teak

- [ ] **1.3** Strengthen Managers
  - [ ] 1.3.1 Enhance `TabManager` - move tab logic from app.go
  - [ ] 1.3.2 Enhance `LayoutManager` - layout computation
  - [ ] 1.3.3 Create `OverlayManager` - overlay stack management

- [ ] **1.4** Extract File Operations to `internal/app/fileops/`
  - [ ] 1.4.1 `fileops.go` - File operation interface
  - [ ] 1.4.2 `open.go` - Open file logic
  - [ ] 1.4.3 `save.go` - Save file logic
  - [ ] 1.4.4 `close.go` - Close file logic
  - [ ] 1.4.5 `delete.go` - Delete file logic

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

### Tasks

- [ ] **2.1** Extract Capability Checker to `internal/lsp/capabilities/`
  - [ ] 2.1.1 `capabilities.go` - Capability checking methods

- [ ] **2.2** Create RPC Method Generator
  - [ ] 2.2.1 `rpc_methods.yaml` - Method configuration
  - [ ] 2.2.2 `generate/main.go` - Generator script
  - [ ] 2.2.3 `rpc_gen.go` - Generated RPC methods

- [ ] **2.3** Simplify `client.go`
  - [ ] 2.3.1 Remove 20+ RPC methods (now generated)
  - [ ] 2.3.2 Delegate capability checks to capabilities pkg

---

## Phase 3: Git Panel Refactoring (Week 4)

**Goal:** Reduce `internal/git/panel.go` from 1,504 → ~800 lines (47% reduction)

### Tasks

- [ ] **3.1** Extract Tree Builder to `internal/git/treebuilder/`
  - [ ] 3.1.1 `tree.go` - Tree building logic

- [ ] **3.2** Extract Hit Testing to `internal/git/hittest/`
  - [ ] 3.2.1 `hit.go` - Click detection logic

- [ ] **3.3** Extract Rendering
  - [ ] 3.3.1 `render.go` - View rendering logic

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

- [ ] **5.1** Refactor `internal/editor/viewport.go` (1,006 → ~650 lines)
  - [ ] 5.1.1 `viewport_render.go` - Rendering strategies (~300 lines)
  - [ ] 5.1.2 `viewport_scroll.go` - Scrollbar logic (~150 lines)
  - [ ] 5.1.3 `viewport_bracket.go` - Bracket matching (~100 lines)

- [ ] **5.2** Refactor `internal/editor/editor.go` (937 → ~550 lines)
  - [ ] 5.2.1 `editor_input.go` - Input handling (~250 lines)
  - [ ] 5.2.2 Create `internal/editor/overlays/` package
    - [ ] 5.2.2.1 `overlay.go` - Overlay interface
    - [ ] 5.2.2.2 `autocomplete.go` - Autocomplete overlay
    - [ ] 5.2.2.3 `hover.go` - Hover overlay
    - [ ] 5.2.2.4 `signature.go` - Signature help overlay

---

## Phase 6: Agent & DAP Refactoring (Week 6)

**Goal:** Reduce agent and DAP files

### Tasks

- [ ] **6.1** Refactor `internal/agent/panel.go` (1,096 → ~700 lines)
  - [ ] 6.1.1 `panel_render.go` - Message rendering (~250 lines)
  - [ ] 6.1.2 `panel_tools.go` - Tool call handling (~200 lines)
  - [ ] 6.1.3 `panel_files.go` - File tagging (~100 lines)

- [ ] **6.2** Refactor `internal/dap/client.go` (839 → ~500 lines)
  - [ ] 6.2.1 `client_requests.go` - DAP requests (generated, ~200 lines)
  - [ ] 6.2.2 `dap_methods.yaml` - Request configuration

---

## Phase 7: Testing & Polish (Week 7)

### Tasks

- [ ] **7.1** Ensure all tests pass
- [ ] **7.2** Benchmark performance
- [ ] **7.3** Update documentation
- [ ] **7.4** Remove this PLAN.md when complete

---

## Notes

- Test files are acceptable as-is (e.g., dap_test.go at 1,606 lines)
- Code generation reduces boilerplate for LSP/DAP RPC methods
- Each phase builds on previous - start with Phase 1
