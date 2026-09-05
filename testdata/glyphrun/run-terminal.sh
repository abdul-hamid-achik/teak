#!/bin/sh
set -eu
# A real shell with a deterministic prompt and no user startup files.
terminal_env=$(mktemp)
trap 'rm -f "$terminal_env"' EXIT
printf '%s\n' "PS1='TEAK\$ '" > "$terminal_env"
export SHELL=/bin/sh ENV="$terminal_env"
sh testdata/glyphrun/run-teak.sh terminal testdata/glyphrun/config/default.toml testdata/glyphrun/workspace/alpha.txt
