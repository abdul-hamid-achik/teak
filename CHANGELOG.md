# Changelog

All notable changes to the Teak editor project.

## [Unreleased]

## [0.16.0] - 2026-08-06

### Bounded Multicursor Auto-Indent

- Enter with auto-indent now computes the leading whitespace independently for
  every cursor or selected range instead of copying the primary cursor's
  indentation across the complete selection set. Forward and reversed ranges
  collapse at their ordered insertion point as one undoable edit.
- Auto-indent reads directly from intersecting rope leaves and shares a 64 KiB
  scan-and-copy budget across at most 1,000 selections. A pathological prefix
  exceeding its fair share falls back to a plain newline, so Enter always
  remains responsive and usable.
- Normalization now coalesces collapsed cursors that fall inside a selected
  half-open range, transferring primary focus to the retained range. Cursors at
  the range end remain independent, preventing competing byte edits without
  losing valid adjacent cursors.
- Ordinary multicursor typing and paste now use the same validated selection
  transaction as paired delimiters and auto-indent, removing a second copy of
  the replacement, cursor-rebase, Undo, version, and LSP full-sync logic.
- On an Apple M5, Enter on an 8 MiB indentation takes about 18.8-19.0
  microseconds and allocates 74,346 bytes, down from about 35.7 MiB before the
  bounded rope read. A 1,000-cursor atomic transaction remains about 200-204
  microseconds.
- Regression coverage includes independent spaces and tabs, mixed forward and
  reversed ranges, incremental single-edit metadata, one-step Undo, interior
  cursor coalescing, half-open boundaries, and giant-line allocation limits.
- Full verification passes with the race detector and three stable Glyphrun
  runs. Aggregate statement coverage is 76.4%; `internal/editor` is 84.3% and
  `internal/text` is 82.6%.

## [0.15.0] - 2026-08-06

### Atomic Multicursor Delimiters

- Opening delimiters now auto-close at every cursor and replace every selected
  range, leaving each caret inside its pair as one undoable edit.
- Closing delimiters independently skip an existing match or insert a missing
  character at each cursor. A skip-only command moves the cursors without
  dirtying the buffer or incrementing its document version.
- Backspace now handles mixed selected ranges, empty delimiter pairs, ordinary
  text, UTF-8 runes, and cursors at the start of the document atomically across
  the complete selection set.
- The text buffer has a validated selection-edit transaction that rejects
  overlapping or invalid operations before mutation, rebases every resulting
  cursor, saves one Undo snapshot, and preserves precise incremental LSP sync
  for single edits while using the existing full-sync fallback for multicursor
  edits.
- Delimiter handling no longer materializes logical lines in the key-update
  path. A 1,000-cursor transaction takes about 197-199 microseconds on an Apple
  M5.
- Regression coverage includes mixed skip/insert commands, selected ranges,
  Unicode deletion, atomic Undo, document boundaries, invalid overlap, and 300
  deterministic randomized transactions checked against a byte-slice model.
- Full verification passes with the race detector and three stable Glyphrun
  runs. Aggregate statement coverage is 76.3%; `internal/editor` is 84.3% and
  `internal/text` is 82.4%.

## [0.14.0] - 2026-08-06

### Multi-selection Clipboard

- Copy and Cut now collect every non-empty selection in document order,
  joining their text with newlines for interoperable system and terminal
  clipboards. A collapsed primary cursor no longer hides selected secondary
  ranges.
- Cut validates the complete live selection snapshot before deleting. Changing
  any secondary range while clipboard preparation is in flight preserves the
  document, while a matching cut removes all selected ranges as one undoable
  edit and retains neighboring collapsed cursors.
- The 16 MiB clipboard limit now covers the combined ranges and their
  separators before any text is materialized. Invalid UTF-8 and stale results
  still cannot mutate the buffer.
- The immutable rope can compose disjoint byte ranges directly into one
  exactly-sized string. Clipboard preparation retains its existing allocation
  budget without constructing a temporary string or sub-rope for each
  selection.
- On an Apple M5, preparing 1 MiB from three ranges takes about 72-74
  microseconds and one allocation; the single-range path remains about 68-71
  microseconds and one allocation.
- Left/Right and Ctrl+Left/Right collapse each active selection to its
  directional edge; already-collapsed cursors in the same set continue moving.
  Cursor movement now reads individual runes from the rope and re-normalizes
  converged selections instead of copying whole logical lines.
- Regression coverage includes reversed and mixed selections, stale secondary
  ranges, combined-size rejection, atomic Undo, range composition ownership,
  and horizontal movement on an 8 MiB logical line.
- Full verification passes with the race detector and three stable Glyphrun
  runs. Aggregate statement coverage is 76.3%; `internal/editor` is 84.3% and
  `internal/text` is 82.2%.

## [0.13.0] - 2026-08-05

### Complete Multicursor Commands

- Ctrl+Left/Right and Home/End, including their Shift selection variants, plus
  Page Up/Down now transform every active cursor instead of collapsing the set
  to its primary cursor.
- Ctrl+Backspace and Ctrl+Delete apply selected and word-sized deletion ranges
  across all cursors as one undoable edit. Overlapping ranges are merged before
  the immutable rope is changed, and every surviving cursor is rebased through
  the resulting document.
- Mixed selections retain collapsed cursors while deleting non-empty ranges;
  cursors that converge after an edit are normalized without corrupting the
  primary cursor.
- Word navigation no longer materializes an entire logical line in the UI
  update path. Each command has a shared 64 KiB scan budget, divided among
  active cursors, so even thousand-cursor edits stay bounded.
- Regression coverage includes real editor key routing, mixed and overlapping
  deletion ranges, undo/redo, cursor rebasing between separate ranges, and an
  8 MiB single-token allocation guard.
- Full verification passes with the race detector and three stable Glyphrun
  runs. Aggregate statement coverage is 76.3%; `internal/editor` is 84.2% and
  `internal/text` is 82.7%.

## [0.12.0] - 2026-08-05

### Display-Column Cursor Navigation

- Up, Down, Page Up, Page Down, and their selection variants now preserve the
  terminal display column across UTF-8 runes, wide characters, and tab stops.
- Consecutive vertical movement retains its preferred column through shorter
  lines and resets that goal after horizontal movement, edits, tab-size
  changes, or viewport resizing.
- Arrow navigation follows visible rows: collapsed folds are skipped and word
  wrap moves within wrapped rows before crossing logical lines.
- Arrow and Shift+Arrow now move or extend every active cursor instead of
  silently collapsing a multicursor set to its primary cursor.
- Buffer clamping and all vertical cursor creation paths repair positions
  inside valid multibyte runes while preserving malformed continuation bytes as
  individually navigable input. Rope and LSP coordinates remain byte offsets.
- Display-column scans are capped at 64 KiB with a UTF-8-safe byte-column
  fallback. A regression guard exercises movement into an 8 MiB logical line
  and rejects document-sized allocation.
- Full verification passes with the race detector and three stable Glyphrun
  runs. Aggregate statement coverage is 76.4%; `internal/editor` is 84.2% and
  `internal/text` is 84.6%.

## [0.11.0] - 2026-08-05

### Unicode-Aware Word Editing

- Word navigation, selection, deletion, double-click selection, and initial
  occurrence selection now classify complete UTF-8 runes instead of bytes.
- Unicode letters, numbers, combining marks, and underscore form words;
  Unicode whitespace separates them; punctuation and symbols remain grouped.
- Rope, buffer, change-record, and LSP positions remain UTF-8 byte offsets.
  Word operations align stale mid-rune cursor columns safely, while stray
  invalid continuation bytes remain navigable one byte at a time.
- Tests cover accented Latin, Greek, CJK, decomposed combining text, fullwidth
  digits, emoji modifiers, non-breaking spaces, invalid UTF-8, ASCII behavior,
  deletion change records, and real editor double-click handling.
- Full verification passes with the race detector and three stable Glyphrun
  runs. Aggregate statement coverage is 76.3%; `internal/text` is 83.9%.

## [0.10.35] - 2026-08-05

### Single-Copy Clipboard Selections

- The immutable rope now exposes `StringRange`, which traverses only the
  intersecting leaves into an exactly-sized `strings.Builder` instead of
  constructing a temporary sub-rope and contiguous byte slice first. Its
  negative, reversed, empty, out-of-range, cross-leaf, and nil bounds match
  `Slice` semantics.
- `Rope.String` uses the same direct traversal, halving the storage allocated
  for whole-document string snapshots while preserving immutable ownership.
- Deferred copy and cut preparation now sends the selected rope range directly
  to the clipboard string. Invalid UTF-8 still fails before a cut can mutate
  the live buffer, and the existing 16 MiB clipboard limit remains enforced
  before materialization.
- Allocation guardrails require range and whole-rope strings to allocate only
  their returned storage, and clipboard preparation to avoid all temporary-rope
  allocations.
- On an Apple M5, converting a 1 MiB rope dropped from about 124-136
  microseconds, 2.10 MB, and two allocations to 81-86 microseconds, 1.05 MB,
  and one allocation. Preparing a 1 MiB clipboard selection dropped from
  132-141 microseconds, 2.10 MB, and 28 allocations to 70-76 microseconds,
  1.05 MB, and one benchmarked allocation.

## [0.10.34] - 2026-08-05

### Lazy Viewport Line Materialization

- The unwrapped viewport now renders ordinary syntax-highlighted rows directly
  from the highlighter cache without first copying and converting rope lines
  whose bytes are never inspected.
- Line bytes are loaded only for selections, plugin highlights, plain text, or
  the one or two rows containing a matched bracket. Selection detection can
  identify empty cursor-only rows before asking for the line length.
- Dedicated tests prove token-only frames leave the rope-line cache untouched
  while bracket rendering still materializes the matched row and applies its
  style on demand.
- Paired Apple M5 viewport benchmarks against v0.10.33 remove 48 allocations
  and 3,072 bytes per 24-row highlighted frame, and 96 allocations and 6,144
  bytes per 48-row frame. Plain-text frames remove 48 allocations and 3,072
  bytes, while selection frames remove 42 allocations and 2,688 bytes.
- Render time remains neutral to modestly faster: 48 highlighted rows measure
  about 315-325 microseconds versus 322-329 microseconds, and the plain-text
  path remains about 232-235 microseconds.

## [0.10.33] - 2026-08-05

### Single-Allocation Line Reads

- `Rope.Line` now copies directly from intersecting immutable leaves into its
  caller-owned result instead of constructing and flattening a temporary
  sub-rope. Returned bytes remain safe to mutate without changing the rope.
- Cross-leaf, empty, final, and out-of-range lines have dedicated behavior and
  ownership tests. A regression budget requires a 64 KiB line read to allocate
  only its result slice.
- On an Apple M5, a normal 80-byte line dropped from 252 bytes and three
  allocations to 80 bytes and one allocation, while improving from about 300
  to 242-252 nanoseconds. A 64 KiB cross-leaf line dropped from 74.6 KB and 11
  allocations to 65.5 KB and one allocation, and from roughly 8.5-9.5 to
  5.9-6.9 microseconds.
- Existing viewport benchmarks confirm 57 fewer allocations per 24-line frame
  and 114 fewer per 48-line frame, covering both highlighted and first-frame
  plain-text rendering.
- Release linting now checks `os.Root` cleanup errors in workspace-edit tests,
  documents the autocomplete nil-context edge case, and removes a superseded
  synchronous code-action picker helper.

## [0.10.32] - 2026-08-05

### Allocation-Free Bracket Scanning

- The immutable rope now implements `io.ReaderAt`, copying only intersecting
  leaves into caller-owned storage without flattening, exposing mutable leaf
  bytes, or allocating.
- Bounded forward and backward bracket matching reuse one 4 KiB stack buffer
  instead of building sixteen temporary rope slices while navigating around an
  unmatched bracket.
- Cross-leaf reads, partial EOF, invalid offsets, nil ropes, large bidirectional
  matches, scan budgets, and zero-allocation behavior have dedicated tests.
- A paired Apple M5 benchmark over the 64 KiB interactive scan budget dropped
  from 39.3-45.4 microseconds, 79.8 KB, and 141 allocations to 17.7-18.7
  microseconds with zero allocated bytes and zero allocations.
- Doctor Glyphrun specs with available LSP fixtures now allow the documented
  2-second language budget plus 3-second general-tool budget before declaring
  a hang. Their 9-second wait remains below the 10-second process hard limit,
  eliminating the exact 5.005-second CI timeout observed during verification.

## [0.10.31] - 2026-08-05

### Streaming UTF-8 Edit Validation

- LSP formatting and workspace-edit validation now checks UTF-8 directly
  across immutable rope leaves instead of materializing every referenced line.
  Multi-byte sequences split across leaves retain standard-library validity
  semantics without introducing a document-sized line-offset table.
- A request-scoped cache reuses each touched line's start, length, and validity
  for both endpoints while preserving line, column, malformed UTF-8, and rune
  boundary error ordering.
- Exhaustive subrange comparisons against `utf8.Valid`, split-leaf malformed
  input tests, and zero-allocation rope checks cover the streaming path.
  Preparation allocation budgets are tightened to 500 for formatting and
  1,000 for complete workspace edits with 1,024 replacements.
- With 4,096 one-byte edits on an Apple M5, formatting preparation now takes
  about 0.77-0.79 ms, 478 KB, and 64 allocations; workspace preparation takes
  about 0.77-0.78 ms, 479 KB, and 66 allocations. The previous v0.10.30 path
  took about 2.7-3.5 ms and 24.6 thousand allocations.

## [0.10.30] - 2026-08-05

### Linear LSP Text Edit Preparation

- Formatting and workspace edits now validate server ranges into one sorted
  offset representation, copy the source snapshot once, and assemble the
  resulting immutable rope in a single ascending pass. The old path rebuilt a
  persistent rope and remapped editor state once per edit.
- Cursor, primary selection, multicursor ranges, UTF-8 byte boundaries, adjacent
  edits, and sequential workspace-edit batches retain their previous behavior.
  Preparation checks cancellation throughout validation, assembly, and
  selection mapping.
- Deterministic differential tests compare the linear path with the former
  sequential algorithm across table-driven edge cases and 200 generated edit
  sets. Allocation budgets guard both formatting and workspace preparation.
- With 4,096 one-byte edits on an Apple M5, formatting preparation dropped from
  about 93 ms, 309 MB, and 3.35 million allocations to about 2.30-2.57 ms,
  707 KB, and 24.6 thousand allocations. Workspace-edit preparation reaches
  the same roughly 2.47 ms profile.

## [0.10.29] - 2026-08-05

### Responsive Atomic Formatting

- LSP formatting responses are now validated and applied to an immutable
  snapshot in a background command instead of performing server-sized work in
  Bubble Tea's `Update()` loop. The prepared rope is installed as one atomic,
  undoable editor mutation.
- Formatting results are bound to the editor identity, buffer version, and
  source rope. Typing, closing, or replacing the document while preparation is
  running discards the obsolete result instead of overwriting newer work;
  shutdown cancels all tracked preparations.
- Cursor and multicursor selections are mapped through the edits off-loop and
  restored with the prepared snapshot. Save, Save As, dirty-tab, preview-pin,
  highlighting, LSP sync, and plugin autocmd reconciliation still use their
  existing coordinated paths.
- With 4,096 one-byte edits on an Apple M5, result dispatch dropped from about
  72-90 ms, 309 MB, and 3.35 million allocations in `Update()` to about 15.8
  microseconds, 352 bytes, and four allocations. The roughly 93 ms preparation
  now runs outside the event loop.

## [0.10.28] - 2026-08-05

### Viewport-Prepared Plugin Highlights

- Plugin highlight namespaces are cloned and sorted once when published, then
  queried with binary search for only the visible line runs. Rendering no
  longer flattens, copies, and sorts every retained plugin range on each frame.
- Diagnostics, find matches, and plugin highlights share the same viewport
  projection, including sparse lines around collapsed folds. Copy-on-write
  prepared collections remain safe across Bubble Tea model copies and lexer
  rebuilds after cross-extension renames.
- Editing a buffer now invalidates the whole prior collection for replacement,
  clearing, and limit accounting, so updating one namespace cannot revive stale
  highlights or inherit obsolete namespace limits.
- On an Apple M5 with the maximum 4,096 retained ranges and 24 visible rows,
  editor rendering dropped from about 1.68-1.76 ms and 9.07 MB per frame to
  0.169-0.170 ms and 86 KB -- roughly 10x faster with 105x fewer allocated
  bytes. Preparing a maximum 512-range plugin update takes about 0.34 ms once.

## [0.10.27] - 2026-08-05

### Fold-Aware Find Highlights

- In-buffer find now projects highlights through the exact visible line runs
  when folds are collapsed, instead of treating the first and last rendered
  rows as one broad buffer range and materializing matches on hidden lines.
- The find and diagnostic paths share the same collapsed-line projection, so
  fold visibility rules cannot drift between the two render features.
- On an Apple M5 with the maximum 10,000 retained matches and only ten visible
  rows, highlight preparation dropped from about 0.65-0.67 ms and 6.81 MB per
  frame to about 0.0016 ms and 6.99 KB -- roughly 400x faster with 974x fewer
  allocated bytes.

## [0.10.26] - 2026-08-05

### Viewport-Indexed LSP Diagnostics

- LSP diagnostic preparation now builds a cancellable immutable interval
  index alongside the existing editor, Problems, and severity projections;
  `Update()` installs the prepared set without sorting or scanning it.
- Editor rendering, gutter severity, and the idle status-bar message query
  only diagnostics intersecting the visible lines. Collapsed folds use their
  sparse visible-line projection, so hidden ranges are neither underlined nor
  expanded into per-line render work.
- Code-action requests use the same index instead of scanning every diagnostic
  on the cursor keypress. Rebuilding an editor after a cross-extension rename
  safely shares the immutable projection.
- On an Apple M5 with 100,000 diagnostics, a normal editor frame dropped from
  about 0.39 ms to 0.19-0.20 ms. A frame with nearly all diagnostics inside one
  collapsed fold dropped from 114.8 ms and 468 MB to about 0.227 ms and
  233 KB. Cursor-line code-action projection dropped from 72-91 microseconds
  to about 0.21-0.23 microseconds; the one-time index build takes about 2.05 ms
  in the background.

## [0.10.25] - 2026-08-05

### Responsive LSP Autocomplete

- Completion-item conversion and every client-side prefix projection now run
  in cancellable, generation-bound commands instead of scanning and copying a
  server-sized result inside Bubble Tea's `Update()` loop.
- The popup exposes bounded loading and filtering states. Escape, later
  typing, tab switches, tab closure, and shutdown invalidate obsolete work;
  Enter or Tab waits for the current projection rather than selecting stale
  data.
- A completion accepted while filtering still passes through the root editor
  mutation reconciler, preserving dirty state, preview pinning, LSP sync, and
  plugin autocmds.
- With 20,000 items on an Apple M5, result dispatch dropped from about 236-239
  microseconds and 1.76 MiB to 0.45 microseconds and 208 bytes. Per-keystroke
  filter dispatch dropped from about 226-231 microseconds and 1.76 MiB to 0.064
  microseconds and 176 bytes; the roughly 0.2 ms projections run off-loop.

## [0.10.24] - 2026-08-05

### Responsive LSP Result Pickers

- Definitions, references, document symbols, and code actions now prepare and
  initially filter their picker items in cancellable background commands
  instead of doing server-sized work in Bubble Tea's `Update()` loop.
- Pending pickers show an explicit loading state and reject stale results by
  instance, zone, and generation. Closing an overlay or shutting down the
  model cancels its outstanding preparation and filtering work.
- Document symbols are flattened iteratively so deeply nested server output
  cannot exhaust the call stack, and unused child payloads are not retained by
  picker selections.
- Dispatching a 20,000-reference result now takes about 3.3 microseconds on an
  Apple M5, down from 6.4-7.3 ms; the roughly 3.0 ms conversion runs outside
  the event loop.

## [0.10.23] - 2026-08-05

### Bounded ACP Stream Ingestion

- Agent text and thought updates are normalized and split into ordered 64 KiB
  queue messages before they reach Bubble Tea, while preserving the existing
  4 MiB per-prompt budget and visible truncation marker.
- The panel defensively caps direct internal stream messages before UTF-8
  normalization, so a malformed or multi-megabyte payload cannot make one
  `Update()` call scan or split document-sized input.
- Processing a raw 4 MiB panel message now takes about 30 microseconds and 32
  bytes on an Apple M5, down from roughly 17.4 ms and 2.1 KiB. Normal queue
  sends also avoid allocating a timeout timer unless backpressure occurs.

## [0.10.22] - 2026-08-05

### Ordered and Asynchronous ACP Prompt Completion

- Agent prompt finalization now detaches its bounded stream snapshot in
  `Update()` and prepares text, tool calls, UTF-8 truncation, and history sizes
  in a generated background command. Superseded results are rejected and
  clearing history while preparation is pending cannot resurrect old output.
- The maximum 2 MiB completion dispatch takes about 0.8 microseconds and 112
  bytes on an Apple M5, down from roughly 1.02–1.04 ms and 2.10 MiB of
  synchronous work. Background projection takes about 0.27 ms and 525 KiB.
- Interactive ACP prompts now place their terminal result on the same FIFO as
  streaming notifications. This guarantees all already-emitted thought, text,
  and tool updates reach the panel before completion is finalized, while the
  direct `Prompt` API remains available to non-TUI consumers.

## [0.10.21] - 2026-08-05

### Fast Diff Rendering and Responsive Branch Selection

- Prepared diff tokens now carry their row background and write exact cached
  SGR sequences directly into the viewport builder. A prepared 40-row,
  10,000-line diff renders in about 0.37 ms on an Apple M5, down from roughly
  1.3 ms, with about 68% fewer allocations.
- Git branch filtering now runs in cancellable background commands. Query and
  picker-open generations reject obsolete filters and lists; Enter waits for
  the current result instead of switching a stale selection. Dispatching a
  50,000-branch filter takes about 3 microseconds in `Update()`, down from
  0.84–0.97 ms of synchronous scanning.
- Direct ACP command results now reach the root agent panel: prompt completion
  commits streamed output and leaves the thinking state, immediate errors are
  visible, and model/mode results keep panel and coordinator metadata aligned
  without starting a duplicate channel listener.
- The runtime failure-store fixture now synchronizes watcher reads and test
  failure toggles, eliminating the race that blocked the v0.10.20 release gate.

## [0.10.20] - 2026-08-05

### Viewport-Only Diff Highlighting

- Diff parsing, compact indexing, and initial viewport highlighting moved into
  the cancellable load command instead of running in the root `Update()` loop.
- Scroll highlighting captures only bounded context around visible rows and
  uses model identity, generations, and cancellation to reject obsolete work.
- Gutter width is precomputed and sparse token batches preserve coloring while
  avoiding whole-diff tokenization. Opening the 10,000-line benchmark dropped
  from roughly 380 ms and 403 MiB to about 7 ms and 5.4 MiB.

## [0.10.19] - 2026-08-05

### Responsive Git-Tree Interaction

- Directory and staged/unstaged section toggles now prepare persistent tree
  projections in cancellable commands instead of flattening changed paths in
  `Update()`.
- Status, expansion, and section generations reject obsolete projections;
  copy-on-write updates clone only the changed directory and its ancestors.
- Cached staged and unstaged row starts make cursor visibility constant-time,
  and input or resize never falls back to a synchronous tree flatten.

## [0.10.18] - 2026-08-05

### Prepared-Only File-Tree Input

- Keyboard navigation, mouse clicks, wheel scrolling, hit-testing, and resize
  now consume only an already prepared flat projection. If rows are pending or
  invalidated, input waits instead of rebuilding visibility/filter state in
  Bubble Tea's `Update()` loop.
- Cursor clamping and scrolling accept known projection lengths, keeping
  relayout independent of tree size. An AST invariant covers every interactive
  handler, while fixtures explicitly prepare rows before simulating input.
- On an Apple M5, resizing with an invalidated 100,000-entry root takes about
  46 ns with zero allocations and does not populate either flat cache.

## [0.10.17] - 2026-08-05

### Constant-Time File-Tree Filter Reset

- Clearing or blurring the project-tree filter now swaps back to its prepared
  unfiltered source and restores the selected row through compact positional
  indexes, without filtering or scanning the tree in `Update()`.
- Initial tree results restore hidden/ignored preferences and active filter
  state through a background projection command instead of three synchronous
  cache invalidation and rebuild passes.
- Opening the filter prompt uses only cached projection length. On an Apple
  M5, clearing a prepared 100,000-entry filter takes about 37 ns with zero
  allocations; its background filter command takes about 1.35 ms and 8.9 MiB.

## [0.10.16] - 2026-08-05

### Responsive File-Tree Interaction

- Expansion, collapse, asynchronous child installation, and hidden/ignored
  visibility toggles now flatten and filter persistent tree roots in
  cancellable commands instead of Bubble Tea's `Update()` loop.
- Prepared projections are bound to the entry revision, visibility flags,
  filter text, and generation. Installation transfers both caches and a
  precomputed selection index without traversing the tree; stale results are
  discarded.
- A context-menu click received during projection is replayed against the new
  rows, so rapid visibility toggles cannot accidentally open the workspace
  root menu. On an Apple M5, dispatching a 100,000-entry visibility projection
  takes about 53 ns; applying it takes about 159–182 ns with zero allocations.

## [0.10.15] - 2026-08-05

### Persistent File-Tree Refresh

- Directory expansion and asynchronous child installation now replace only
  the entry slices along the affected path. Background refreshes can capture
  the persistent root in constant time without cloning the visible tree or
  racing later keyboard and mouse updates.
- Refresh commands now prepare visibility filtering, flattened rows, path
  indexes, and selection projection before returning. `Update()` validates the
  entry revision and view state, then transfers the prepared projection; a
  conflicting interaction retries from the latest root instead of recursively
  merging and flattening the tree on the event loop.
- On an Apple M5, capturing and applying a prepared 100,000-entry refresh take
  about 20 ns and 52 ns respectively with zero allocations. The removed deep
  clone costs about 0.64–0.76 ms and 8.8 MiB per refresh dispatch.

## [0.10.14] - 2026-08-05

### Responsive LSP Diagnostics

- LSP diagnostic conversion, global Problems projection, sorting, grouping,
  and severity counting now run in cancellable background commands instead of
  Bubble Tea's `Update()` loop.
- Per-file and aggregate generations reject superseded publications and panel
  snapshots. Versioned diagnostics are revalidated after preparation so an
  intervening edit cannot install obsolete ranges.
- File moves now relocate the immutable diagnostic projection in constant time
  and rebuild the Problems snapshot asynchronously. On an Apple M5, dispatch
  measured about 108–129 ns and 96 B whether a publication contained one or
  100,000 diagnostics.

## [0.10.13] - 2026-08-05

### Responsive Agent Scrolling

- Agent-panel wheel, PageUp/PageDown, and End handling no longer rebuild a
  dirty transcript cache inside Bubble Tea's `Update()` loop. Scroll bounds
  use the last rendered shared cache snapshot and leave wrapping and styling
  to `View()`.
- A maximum-history benchmark now guards the real dirty-cache branch. On an
  Apple M5, the input path is about 0.89 microseconds with zero allocations,
  while the full render it previously triggered costs about 100 milliseconds
  and 38 MiB of allocations.

## [0.10.12] - 2026-08-05

### Responsive Git Status Refresh

- Git status entries are now grouped, indexed, projected into sidebar trees,
  flattened into hit-test rows, and converted into file-tree decorations in a
  background command instead of Bubble Tea's `Update()` loop.
- Refresh and expansion generations discard obsolete projections and retry a
  projection when a directory or section was toggled while it was being built,
  preserving the latest interaction without racing mutable tree nodes.
- Git tree construction now indexes child directories per parent instead of
  repeatedly scanning siblings, removing quadratic behavior for repositories
  with many changed paths in the same directory.

## [0.10.11] - 2026-08-05

### Path Reconciliation Performance

- Save As and file-tree move/rename reconciliation no longer materialize the
  complete editor buffer inside `Update()` when a file extension changes.
- The redundant highlighter replacement was removed; `editor.New` already
  creates the correct lexer and tokenizes a strictly byte-bounded prefix. An
  AST invariant now rejects future full-buffer materialization in both paths.

## [0.10.10] - 2026-08-05

### Recovery Resource Bounds

- Autosave now rejects recovery candidates before rope materialization when a
  buffer exceeds 4 MiB, the workspace exceeds 32 MiB of decoded recovery
  content, or 256 records have already been retained.
- Recovery persistence enforces the same per-record, aggregate, and count
  limits defensively. Startup also rejects encoded recovery files larger than
  48 MiB before reading them, with a limited reader protecting against growth
  after the size check.

## [0.10.9] - 2026-08-05

### Crash Recovery Responsiveness

- Crash-recovery bytes are now converted into owned immutable rope snapshots
  inside background load commands, so session and standalone recovery no
  longer build multi-megabyte ropes inside Bubble Tea's `Update()` loop.
- Existing clean buffers are compared with recovery snapshots in a background
  command bound to editor identity and version. Equal or stale results are
  discarded; applicable snapshots are installed by identity and pass through
  central editor reconciliation.

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
