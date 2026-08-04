#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf -- "$tmp_dir"' EXIT

export HOME="$tmp_dir/home"
export XDG_CONFIG_HOME="$HOME/.config"
project="$tmp_dir/project"
mkdir -p "$HOME/.config/teak" "$project"
printf '%s\n' 'fixture source' > "$project/main.fixture"
printf '[[lsp]]\nextensions = [".fixture"]\ncommand = "python3"\nargs = ["%s/testdata/glyphrun/lsp-diagnostics-fixture.py"]\nlanguage_id = "fixture"\n' "$repo_root" > "$HOME/.config/teak/config.toml"

read_json=$("$repo_root/bin/teak" headless buffer read --json --root "$project" main.fixture)
expected_sha=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["sha256"])' "$read_json")
write_json=$(printf '%s\n' 'fixture edited' | "$repo_root/bin/teak" headless buffer write --json --root "$project" --expected-sha256 "$expected_sha" main.fixture)
search_json=$("$repo_root/bin/teak" headless search --json --root "$project" fixture)
diagnostics_json=$("$repo_root/bin/teak" headless lsp diagnostics --json --root "$project" main.fixture)

python3 - "$read_json" "$write_json" "$search_json" "$diagnostics_json" <<'PY'
import json
import sys

read, write, search, diagnostics = map(json.loads, sys.argv[1:])
assert read["content"] == "fixture source\n"
assert write["content"] == "fixture edited\n"
assert write["sha256"] != read["sha256"]
assert search["state"] == "ready" and any("main.fixture" in hit["file_path"] for hit in search["results"])
assert diagnostics["state"] == "ready" and diagnostics["diagnostics"][0]["message"] == "fixture diagnostic"
PY

printf '%s\n' 'HEADLESS_WORKFLOW_JSON_OK'
