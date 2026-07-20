# ADR 0001: Compose the Workbench MVU Shell

- Status: Accepted
- Date: 2026-07-18

## Context

`internal/app/app.go` is Teak's Bubble Tea composition root. It currently owns
document, tab, sidebar, overlay, diagnostics, protocol, debugger, agent, plugin,
layout, and rendering concerns in one `Model` and one large `Update` method.

The package previously contained managers introduced during an earlier
refactor. They were not canonical owners:

- `Model` and `TabManager` both retain editor and tab state and are manually
  synchronized.
- `Model` and `ProtocolManager` both retain protocol-related state; the latter
  also constructs another LSP manager.
- sidebar, overlay, and layout managers mirror fields that remain live on
  `Model`.

Promoting these mirrors would move code without establishing clear ownership.
Keeping `app.go` as one file makes smaller changes hard to review and increases
the chance of unrelated conflicts.

The former manager mirrors were retired after repository-wide call-site
analysis confirmed that production never constructed them. Characterization
coverage remains on the canonical `Model` paths for creation, preview
replacement, open, close, diff, and session restore.

Bubble Tea already supplies the architectural constraint Teak needs: state is
updated by messages, effects run as `tea.Cmd`, and rendering projects state.
The refactor should strengthen that model instead of adding a second event
system.

## Decision

Teak will use a **composed MVU application shell**.

`app.Model` remains the sole Bubble Tea root and composition boundary. Its
state will be grouped by domain, with exactly one canonical owner for every
mutable field:

```text
Model
├── DocumentsState   editors, tabs, diffs, pending loads and saves
├── SidebarState     tree, Git, problems, and debugger panels
├── UIState          dimensions, focus, status, and overlays
├── DebugState       breakpoints and execution position
├── AgentState       agent conversation and permission UI
└── Runtime          LSP, DAP, ACP, watcher, and plugin handles
```

`Runtime` owns external clients, processes, and listener commands only. It
emits typed messages and does not retain renderable UI state.

The migration stays inside package `app` until ownership and dependency
boundaries are stable. Splitting files is a preparatory step, not the final
architecture.

## Rules

### One owner per mutable field

Each field belongs to one domain state. Other domains communicate with its
owner through typed messages or narrow methods. They must not retain a second
copy and must not be kept in sync manually.

In particular, a future `DocumentsState` extraction must replace the current
`Model` fields atomically; it may not introduce a second writable copy.

### One authoritative handler per event

Root routing follows this precedence:

```text
modal capture
  → runtime event
  → application command or intent
  → focused child
  → active editor
```

Each routing stage returns whether it handled the message. A wrapped protocol
event is unwrapped once and routed directly; it is not recursively dispatched
through `Model.Update`.

### Effects are commands

File I/O, protocol requests, directory traversal, indexing, highlighting, and
other external work run as `tea.Cmd`. Reducers update state synchronously and
do not mutate UI state from background goroutines.

Async results carry enough identity to reject stale work, such as document
path, version, request ID, tab identity, or generation.

### View is a projection

The target contract for `View` is read-only:

- no model or child-model writes;
- no resizing;
- no process or I/O work;
- no mutation through shared slices, maps, or pointers.

Layout changes happen while handling `tea.WindowSizeMsg`.

The first extraction intentionally preserves existing behavior, including
current debug-gutter and panel-size writes during rendering. Those writes are
documented migration debt and will be removed in a separately tested change.

### Dependencies point away from the shell

```text
cmd/teak
  → app
    → feature state, runtime adapters, and shared contracts
      → editor, filetree, git, search, overlay, text, lsp, dap, acp, plugin
```

Feature and runtime packages must not import `app.Model`. Code that currently
does so, including staging keybinding or mode packages, must first consume
neutral intents and context before the root can depend on it.

### Commands resolve to intents

Keyboard shortcuts, the command palette, help, menus, and plugin-discoverable
commands should resolve through one command registry to typed application
intents. They must not duplicate behavior in independent switch statements or
capture `Model` in arbitrary closures.

## Alternatives considered

### Split `app.go` into files only

This reduces navigation and merge pressure and is useful as the first step.
Alone, it does not fix duplicate ownership or routing complexity, so it is not
the target architecture.

### Promote the existing managers and coordinators

Rejected as the target because several mirror state still owned by `Model`.
Synchronizing them indefinitely would make invariants harder to enforce.
Managers may be retained only when they become the sole owner of a coherent
domain; otherwise they should be removed.

### Add a global Redux/CQRS-style action bus

Rejected because Bubble Tea already provides typed message dispatch and
effects. A second global bus would add ceremony and indirection without solving
Teak's ownership problem.

## Migration

1. Add characterization tests around rendering, routing precedence, tab/editor
   invariants, stale async results, and shutdown.
2. Extract cohesive presentation methods from `app.go` into `view.go` without
   changing behavior.
3. Move render-time mutation into update/layout paths and add a test proving
   repeated `View` calls do not alter stored state.
4. Split `Update` into same-package routing stages: modal, runtime, intent, and
   focused component.
5. Remove unused duplicate manager construction after call-site and behavior
   tests prove it is safe. **Completed 2026-07-19.**
6. Keep the existing document fields on `Model` canonical until a
   `DocumentsState` extraction can replace them without two-way synchronization.
7. Introduce the remaining canonical domain states and runtime adapters.
8. Move domains into child packages only after dependency direction can be
   enforced without import cycles.

Each step must compile, pass focused and full tests, and preserve observable
behavior unless its acceptance tests explicitly describe a change.

## Consequences

- Teak keeps one understandable event loop and gains reviewable module
  boundaries.
- Ownership and routing rules become testable rather than implicit.
- Migration can proceed in small changes without a flag-day rewrite.
- `app` remains a modular monolith during the transition.
- Some temporary duplication remains until the relevant canonical state is
  introduced; no new mirrored state may be added.

The first implementation of this decision is the behavior-preserving
`view.go` extraction protected by `view_test.go`.
