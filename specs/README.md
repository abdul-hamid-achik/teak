# Teak terminal UI contracts

These Glyphrun specs exercise Teak as a real terminal application. They use a
fixed terminal size, an isolated home directory, an isolated copy of the test
workspace, and the Nord theme so mouse coordinates and rendered cell styles are
repeatable.

## Prerequisites

- Go 1.26 or newer
- Glyphrun 0.14.3

Build Teak and check the local Glyphrun setup:

```sh
go build -o ./bin/teak ./cmd/teak
glyph doctor
```

Validate every committed contract hash:

```sh
for spec in specs/tui_*.yml; do
  glyph spec verify "$spec"
done
```

## Regression suite

All twelve specs are expected to pass. Together they cover basic navigation,
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
  specs/tui_word_wrap_selection.yml
```

The focused regression contracts protect these previously defective
behaviors:

- `tui_ascii_font_fallback.yml` — controls remain readable without Nerd Fonts
- `tui_editor_selection.yml` — a normal click should clear a drag selection
- `tui_quick_open_mouse.yml` — clicking a Quick Open result should open it
- `tui_unsaved_confirm_mouse.yml` — clicking Cancel should dismiss the dialog
- `tui_status_bar_mouse.yml` — status-bar chrome should not move the cursor
- `tui_sidebar_problems_mouse.yml` — Problems must not click through to the tree
- `tui_settings_modal_mouse.yml` — Settings must capture background clicks
- `tui_tabbar_wheel.yml` — wheel input over tabs changes the active tab
- `tui_tiny_resize_recovery.yml` — a 1×1 terminal survives and restores layout
- `tui_word_wrap_selection.yml` — wrapped selections should remain highlighted

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

## Artifacts

Glyphrun writes run records, final screens, snapshots, and agent context under
`.glyphrun/runs/`. The root config retains Glyphrun's default number of recent
runs. For a full diagnostic sweep, give each spec its own artifact root:

```sh
glyph run specs/tui_status_bar_mouse.yml \
  --artifact-root /tmp/teak-glyphrun/status-bar
```

The target wrapper lives at `testdata/glyphrun/run-teak.sh`. Every invocation
recreates its isolated home and workspace, so tests cannot modify the committed
fixtures or inherit the developer's Teak session and configuration.
