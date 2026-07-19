# Teak plugin examples

These examples are small, runnable Teak Lua plugins. Copy one or both into
Teak's plugin directory, then restart Teak.

On macOS, the directory is:

```sh
plugins="$HOME/Library/Application Support/teak/plugins"
mkdir -p "$plugins"
cp -R examples/plugins/autopairs "$plugins/"
cp -R examples/plugins/statusline "$plugins/"
```

On Linux, use:

```sh
plugins="${XDG_CONFIG_HOME:-$HOME/.config}/teak/plugins"
mkdir -p "$plugins"
cp -R examples/plugins/autopairs "$plugins/"
cp -R examples/plugins/statusline "$plugins/"
```

Each plugin needs a `plugin.toml` and the `init.lua` named by its `main`
field. Teak calls global `setup()` once while loading and optional global
`teardown()` while unloading. `setup()` may register commands, mappings, and
autocommands, but it cannot call runtime APIs such as `buffer.*`,
`editor.set_status`, or `ui.notify`; those only work inside a mapping, command,
or autocommand callback.

The available APIs are `buffer`, `editor`, `keymap`, `autocmd`, and `ui`.
There is no `vim` compatibility object. Mappings support the `n` editor mode,
the `a` all-context mode, and sidebar focus modes (`tree`, `git`, `problems`,
`debugger`, and `agent`). Teak does not provide an insert-mode mapping API, so
the autopairs example is an explicit pair-insertion command rather than a
per-keystroke typing hook.

## Autopairs

- `<leader>ap` (Space, `a`, `p`) runs `autopairs.insert_parens`.
- The command inserts `()` and leaves the cursor between the parentheses.

## Statusline

- `<leader>ss` (Space, `s`, `s`) runs `statusline.refresh`.
- `statusline.refresh` writes the active file and 1-based cursor position to
  Teak's status message.
- The same refresh runs whenever the cursor moves.
