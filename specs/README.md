# Teak terminal UI contracts

`tui_find_focus_help.yml` covers Find opened from the tree, inactive Find and
sidebar shortcuts, and long Help bindings, using both legacy and Kitty key
events. It also verifies a clean exit without changing the document.

`tui_terminal.yml` exercises the integrated panel with a real isolated shell:
carriage-return updates, screen clearing, Ctrl+U, bracketed paste, alternate
screens, output while hidden, shell exit, and preservation of the editor's
content and cursor. Run it with `glyph run specs/tui_terminal.yml --parallel 1`.

`tui_paste_resize_workflow.yml` pastes into Find and project search, checks
results after shrinking to 40 columns, and keeps the end of a long Save As
path visible. `tui_agent_prompt.yml` also pastes while an inactive Find is open.

`tui_split_terminal_layout.yml` combines two editor panes, sidebar, agent and
a real shell. It checks terminal/status rows and recovery through 120×40,
80×24 and 40×3. Run all TUI stories with `glyph run specs/tui_*.yml --parallel 1`;
run retained three-pass stability checks with
`python3 scripts/glyph-stability.py specs/tui_*.yml`.

These Glyphrun specs exercise Teak as a real terminal application. They use a
fixed terminal size, an isolated home directory, an isolated copy of the test
workspace, and the Nord theme so mouse coordinates and rendered cell styles are
repeatable.

## Prerequisites

- Go 1.26 or newer
- Glyphrun v0.16.1

Build Teak and check the local Glyphrun setup:

```sh
go build -o ./bin/teak ./cmd/teak
sh scripts/install-glyphrun.sh
glyph doctor
```

Validate every committed contract hash:

```sh
for spec in specs/tui_*.yml; do
  glyph spec verify "$spec"
done
```

## Regression suite

Theme stories run against the real editor using the existing isolated fixture:

```sh
glyph run specs/tui_theme_save.yml specs/tui_theme_discard.yml \
  --parallel 1 --artifact-root .glyphrun/theme-audit \
  --junit .glyphrun/theme-audit-junit.xml
glyph stories specs/tui_theme_save.yml specs/tui_theme_discard.yml \
  --artifact-root .glyphrun/theme-audit \
  --html --out .glyphrun/theme-audit/catalog.html
```

They capture the picker, light preview, discard confirmation, and final editor.
Cell assertions check editor colors, status text contrast, and explicit save
feedback colors; text-only snapshots would miss those regressions. These specs
are tagged `story` and work with the installed Glyphrun 0.18.0 catalog, without
requiring the newer `stories run` manifest API in the Glyphrun source checkout.

All committed specs are expected to pass locally and in CI. Together they cover basic navigation,
mouse routing, drag selection, scrolling, resize, modal capture, overlay
buttons, sidebar isolation, wrapped selections, font fallback, tiny-terminal
recovery, tab-wheel navigation, and clean exit behavior:

```sh
glyph run \
  specs/tui_ascii_font_fallback.yml \
  specs/tui_editor_selection.yml \
  specs/tui_navigation_mouse.yml \
  specs/tui_quick_open_mouse.yml \
  specs/tui_scroll_resize_mouse.yml \
  specs/tui_settings_modal_mouse.yml \
  specs/tui_sidebar_problems_mouse.yml \
  specs/tui_status_bar_mouse.yml \
  specs/tui_tabbar_wheel.yml \
  specs/tui_tiny_resize_recovery.yml \
  specs/tui_unsaved_confirm_mouse.yml \
  specs/tui_word_wrap_selection.yml \
  specs/headless_doctor_language.yml \
  specs/headless_doctor_actions.yml \
  specs/headless_doctor_protocol_probe.yml \
  specs/headless_doctor_protocol_failure.yml \
  specs/headless_context.yml \
  specs/headless_context_depth.yml \
  specs/headless_project.yml \
  specs/headless_lsp_status.yml \
  specs/headless_lsp_diagnostics.yml \
  specs/headless_lsp_intelligence.yml \
  specs/headless_codemap_queries.yml \
  specs/headless_codemap_navigation.yml \
  specs/headless_semantic_search.yml \
  specs/headless_lsp_format.yml \
  specs/headless_dap_status.yml \
  specs/headless_dap_probe.yml \
  specs/headless_git_status.yml \
  specs/headless_tools_status.yml \
  specs/headless_tool_overrides.yml \
  specs/headless_tools_failed_probe.yml \
  specs/headless_vecgrep_legacy_health.yml \
  specs/headless_health.yml \
  specs/headless_health_dashboard.yml \
  specs/headless_health_history.yml \
  specs/headless_mcp.yml \
  specs/headless_exec.yml \
  specs/headless_rest_write.yml \
  specs/headless_rest_multi_workspace.yml \
  specs/headless_server_quota.yml \
  specs/execpolicy.yml \
  specs/headless_buffer_write_guard.yml \
  specs/headless_search.yml \
  specs/headless_session_recovery.yml \
  specs/headless_agent_recovery.yml \
  specs/headless_agent_show.yml \
  specs/headless_agent_cancel.yml \
  specs/headless_agent_reap_stale.yml \
  specs/headless_rest_agent_control.yml \
  specs/headless_mcp_agent_control.yml \
  specs/tui_plugin_new_buffer.yml \
  specs/tui_plugin_confirm.yml \
  specs/tui_plugin_input.yml \
  specs/tui_plugin_select.yml \
  specs/tui_plugin_float.yml \
  specs/tui_plugin_highlight.yml \
  specs/headless_hitspec_validate.yml \
  specs/tui_edit_save.yml \
  specs/tui_agent_prompt.yml \
  specs/tui_agent_permission.yml \
  specs/tui_agent_permission_reject.yml \
  specs/tui_completion_mouse.yml \
  specs/tui_health_dashboard.yml \
  specs/tui_project_explorer.yml \
  specs/tui_project_explorer_actions.yml
```

The focused regression contracts protect these previously defective
behaviors:

- `tui_ascii_font_fallback.yml` — controls remain readable without Nerd Fonts
- `tui_editor_selection.yml` — a normal click should clear a drag selection
- `tui_quick_open_mouse.yml` — clicking a Quick Open result should open it
- `tui_unsaved_confirm_mouse.yml` — clicking Cancel should dismiss the dialog
- `headless_health_history.yml` — health recording requires confirmation and
  history reads remain bounded and newest-first
- `headless_health_dashboard.yml` — the read-only dashboard combines current
  health with explicit history and bounded trend deltas
- `tui_health_dashboard.yml` — the command palette opens and dismisses the
  asynchronous dashboard inside the editor lifecycle
- `headless_mcp.yml` — MCP exposes the fixed workspace, permits only an
  explicitly confirmed optimistic buffer write plus root-confined project
  mutations, and proves stale-write safety
- `headless_exec.yml` — a confirmed command runs in the selected workspace and
  returns structured stdout, stderr, state, and exit code; missing confirmation
  is rejected before the child process starts
- `headless_rest_write.yml` — the authenticated REST adapter permits only an
  explicitly confirmed optimistic buffer write and rejects stale content
- `headless_rest_multi_workspace.yml` — the REST adapter exposes an explicit
  workspace allowlist, routes namespaced requests to fixed roots, ignores root
  injection through query parameters, and exposes confirmed project mutation
  metadata only inside the selected root
- `headless_server_quota.yml` — a live REST server exposes its active request
  and aggregate response reservation limits through `/healthz`
- `tui_status_bar_mouse.yml` — status-bar chrome should not move the cursor
- `tui_sidebar_problems_mouse.yml` — Problems must not click through to the tree
- `tui_settings_modal_mouse.yml` — Settings must capture background clicks
- `tui_tabbar_wheel.yml` — wheel input over tabs changes the active tab
- `tui_tiny_resize_recovery.yml` — a 1×1 terminal survives and restores layout
- `tui_plugin_confirm.yml` — a plugin confirmation resumes Lua after a modal
  choice without blocking the Bubble Tea update loop
- `tui_agent_prompt.yml` — the agent panel opens through the keyboard shortcut
  and renders a streamed ACP response from a deterministic local fixture
- `tui_agent_permission.yml` — an ACP tool request pauses at an explicit
  permission prompt and continues only after the user allows it
- `tui_agent_permission_reject.yml` — rejecting an ACP permission request
  resumes the agent with a bounded denial and a clean process exit
- `tui_plugin_input.yml` — a plugin text prompt resumes Lua after accept or
  explicit cancellation without blocking the Bubble Tea update loop
- `tui_plugin_select.yml` — a plugin selector reuses the fuzzy picker and
  resumes Lua after selection or Escape without blocking the update loop
- `tui_plugin_float.yml` — a plugin can show and dismiss a bounded read-only
  float without blocking the Bubble Tea update loop
- `tui_plugin_highlight.yml` — a plugin can install a bounded, version-bound
  highlight namespace and clear it without blocking the editor
- `tui_word_wrap_selection.yml` — wrapped selections should remain highlighted
- `headless_doctor_language.yml` — a fresh project reports its detected
  language and the relevant language-server state
- `headless_doctor_actions.yml` — a missing detected language server produces a
  structured install action and actionable hint in the JSON report
- `headless_doctor_protocol_probe.yml` — an installed server without a CLI
  version command is verified through a bounded LSP initialize handshake
- `headless_doctor_protocol_failure.yml` — a resolved server whose protocol
  handshake fails is reported as failed in both language and tool checks
- `headless_doctor_missing_lsp.yml` — a configured but absent language server
  is reported as `missing`/`warn`, never as ready
- `headless_doctor_broken_lsp.yml` — a resolved but failing language server is
  reported as `failed`/`warn`, never as ready
- `headless_doctor_probe_budget.yml` — several hanging language-server probes
  stay inside one total doctor timeout instead of stacking per-tool timeouts,
  and their machine-readable details preserve `version probe timed out`
- `headless_lsp_status.yml` — configured language-server resolution and
  verified readiness are observable through bounded version or explicit
  protocol probes
- `headless_lsp_diagnostics.yml` — a configured fixture server can publish and
  return diagnostics through the one-shot headless control plane
- `headless_lsp_intelligence.yml` — a configured fixture server returns bounded
  document symbols, hover content, definition locations, and references with
  0-based coordinates and workspace-relative paths
- `headless_codemap_queries.yml` — context and impact queries return structured
  data without implicitly running codemap initialization or indexing
- `headless_codemap_navigation.yml` — file symbol listing and 0-based
  symbol-at navigation remain bounded and read-only
- `headless_semantic_search.yml` — stale semantic search is read-only by
  default and explicit `--index` performs the bounded vecgrep build/query
- `headless_semantic_adapters.yml` — REST and MCP expose confirmed semantic
  indexing without making normal search mutate the workspace
- `headless_lsp_format.yml` — a configured fixture server returns a bounded
  formatting preview without mutating the file by default
- `headless_dap_status.yml` — the built-in Go/Delve adapter contract is
  observable without starting a debug session
- `headless_dap_probe.yml` — a configured adapter completes a real DAP
  initialize handshake without launching a debuggee
- `headless_git_status.yml` — branch and changed files are observable through
  a bounded, read-only Git workflow
- `headless_tools_status.yml` — tool health includes bounded timing and
  process/runtime memory metrics without triggering indexing
- `headless_tools_capabilities.yml` — Codemap, Vecgrep, and Hitspec health
  verifies the exact structural, lightweight, and validation contracts Teak
  invokes
- `headless_tool_overrides.yml` — the real binary loads an explicit `[tools]`
  path outside `PATH` consistently in headless health and `doctor`
- `headless_context.yml` — a large workspace context listing is bounded and
  validates its deterministic JSON prefix and truncation flag
- `headless_context_depth.yml` — bounded nested project context returns stable
  relative paths without following symlinked directories
- `headless_project.yml` — explicit project list/stat/mkdir/rename/copy/remove
  operations return typed state and keep mutations behind `--confirm`
- `headless_tools_failed_probe.yml` — an executable whose bounded version
  probe fails is reported as `failed` and `ready: false`, not as usable
- `headless_tools_timed_out.yml` — an executable whose bounded version probe
  does not answer is reported as `timed_out` and `ready: false`
- `headless_vecgrep_legacy_health.yml` — a legacy vecgrep is reported as
  `unsupported` without invoking its vector-loading status fallback
- `headless_buffer_write_guard.yml` — stale headless writes are rejected by
  optimistic SHA-256 concurrency checks
- `headless_search.yml` — bounded project search returns stable JSON results
- `headless_session_recovery.yml` — named sessions can be inspected and
  activated after the editor is gone
- `headless_session_health.yml` — stale named sessions are observable and
  explicit cleanup removes only sessions with missing or unsafe tabs
- `headless_workflow.yml` — one headless workflow reads, edits, saves, searches,
  and inspects diagnostics in the same workspace
- `headless_agent_recovery.yml` — durable agent state is observable without
  launching an agent
- `headless_agent_show.yml` — one durable agent run can be inspected by id
  with stable JSON without mutating an active record during observation
- `headless_agent_cancel.yml` — explicit confirmation is required to cancel a
  persisted active run
- `headless_agent_reap_stale.yml` — explicit confirmation and a positive
  silence budget are required before a stale durable run is interrupted
- `headless_rest_agent_control.yml` — authenticated REST cancellation and
  stale reaping require confirmation and persist durable lifecycle state
- `headless_mcp_agent_control.yml` — MCP publishes confirmed agent cancel and
  stale-reap tools and rejects missing confirmation before the CLI boundary
- `tui_plugin_new_buffer.yml` — a Lua callback creates a focused untitled
  buffer through the replayable UI bridge
- `headless_hitspec_validate.yml` — hitspec syntax can be validated headlessly
  without executing HTTP requests
- `tui_edit_save.yml` — terminal editing, saving, closing, and persisted file
  content are verified as one workflow
- `tui_completion_mouse.yml` — clicking the buffer while completions are
  showing dismisses the popup so a stale completion cannot be inserted at the
  new cursor position

The LSP diagnostics/format/status/intelligence, DAP probe, search, tools-status, and buffer
write contracts parse their JSON responses with fixture-side assertions. They
check state, cardinality, typed fields, hashes, ranges, durations, and guarded
mutation flags; screen substring checks remain only as a visible diagnostic.

Headless failures requested with `--json` are emitted on `stderr` using the
stable `{state, code, message}` shape. `stale_write` is reserved for an
optimistic buffer update whose expected SHA-256 no longer matches; scripts
should branch on `code`, not on localized human-readable text.
- `tui_project_explorer.yml` — hidden/ignored visibility toggles and project
  filtering remain usable from the terminal tree
- `tui_project_explorer_actions.yml` — context-menu rename updates the tree
  without leaving the workspace

Use repeat mode when checking for flakiness. Give each spec its own artifact
root so retention and repeat runs cannot interfere with one another:

```sh
for spec in specs/tui_*.yml; do
  name=${spec##*/}
  name=${name%.yml}
  glyph run "$spec" \
    --repeat 3 \
    --artifact-root "/tmp/teak-glyphrun/$name"
done
```

The repository `task specs` and `task specs-repeat` gates use
`--parallel 1` because Glyphrun v0.16.1 prunes a shared artifact root from
individual workers. Parallel exploration is still safe when each invocation
uses its own `--artifact-root`, as in the example above.

Glyphrun 0.18 does not write JUnit in repeat mode. The CI and `specs-repeat`
gates remove the old report, run once with JUnit, then run a separate stability
probe. The XML describes that single run; the probe's exit status also gates
success. Do not combine `--repeat` and `--junit` or reuse an old XML as evidence.

Contracts that take an explicit snapshot before teardown may set
`artifacts.finalScreen: never`. This keeps a shutdown status frame from
becoming an accidental assertion; the behavior must still be asserted by a
named snapshot or process outcome.

## Artifacts

Glyphrun writes run records, final screens, snapshots, and agent context under
`.glyphrun/runs/`. The root config retains 100 recent runs so a complete batch
does not prune its own failure evidence. For longer diagnostic sweeps, give
each spec its own artifact root:

```sh
glyph run specs/tui_status_bar_mouse.yml \
  --artifact-root /tmp/teak-glyphrun/status-bar
```

The target wrapper lives at `testdata/glyphrun/run-teak.sh`. Every invocation
recreates its isolated home and workspace, so tests cannot modify the committed
fixtures or inherit the developer's Teak session and configuration.

### Component states across themes

`tui_component_states.yml` builds a deterministic fixture from Teak's real
component renderers. It captures 16 empty, loading, error and populated states
at 80×24 and 40×18, checking state-specific content and terminal bounds.
The fixture forces true color and does not start external integrations.

```sh
TEAK_STORY_THEME=github-light glyph run specs/tui_component_states.yml \
  --artifact-root .glyphrun/theme-audit/components/github-light
glyph stories specs/tui_component_states.yml \
  --artifact-root .glyphrun/theme-audit/components/github-light \
  --html --out .glyphrun/theme-audit/components/github-light/catalog.html
```

The theme defaults to Nord. After the first build,
`bin/teak-component-story --themes` lists supported IDs.
