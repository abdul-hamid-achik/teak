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

run_teak() {
    if response=$("$repo_root/bin/teak" "$@"); then
        printf '%s\n' "$response"
    else
        status=$?
        printf 'Workflow command failed (%s): teak %s\n%s\n' "$status" "$*" "$response" >&2
        return "$status"
    fi
}

read_json=$(run_teak headless buffer read --json --root "$project" main.fixture)
expected_sha=$(python3 -c 'import json,sys; print(json.loads(sys.argv[1])["sha256"])' "$read_json")
write_json=$(printf '%s\n' 'fixture edited' | run_teak headless buffer write --json --root "$project" --expected-sha256 "$expected_sha" main.fixture)
search_json=$(run_teak headless search --json --root "$project" fixture)
diagnostics_json=$(run_teak headless lsp diagnostics --json --root "$project" main.fixture)

python3 - "$read_json" "$write_json" "$search_json" "$diagnostics_json" <<'PY'
import json
import sys

read, write, search, diagnostics = map(json.loads, sys.argv[1:])
assert read["content"] == "fixture source\n", read
assert write["content"] == "fixture edited\n", write
assert write["sha256"] != read["sha256"], (read, write)
assert search["state"] == "ready" and any("main.fixture" in hit["file_path"] for hit in search["results"]), search
assert diagnostics["state"] == "ready" and diagnostics["diagnostics"] and diagnostics["diagnostics"][0]["message"] == "fixture diagnostic", diagnostics
PY

printf '%s\n' 'HEADLESS_WORKFLOW_JSON_OK'
