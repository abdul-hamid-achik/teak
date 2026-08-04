#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf -- "$tmp_dir"' EXIT

export HOME="$tmp_dir/home"
export XDG_STATE_HOME="$tmp_dir/state"
project="$tmp_dir/project"
mkdir -p "$HOME" "$project"
printf '%s\n' 'package main' > "$project/main.go"

key=$(python3 -c 'import hashlib,os,sys; print(hashlib.sha256(os.path.realpath(sys.argv[1]).encode()).hexdigest())' "$project")
named_dir="$XDG_STATE_HOME/teak/sessions/$key/named"
mkdir -p "$named_dir"
printf '{"version":1,"root_dir":"%s","active_tab":0,"tabs":[{"file_path":"%s/main.go","cursor_line":0,"cursor_col":0,"scroll_y":0,"pinned":true}]}\n' "$project" "$project" > "$named_dir/healthy.json"
printf '{"version":1,"root_dir":"%s","active_tab":0,"tabs":[{"file_path":"%s/deleted.go","cursor_line":0,"cursor_col":0,"scroll_y":0,"pinned":true}]}\n' "$project" "$project" > "$named_dir/stale.json"

set +e
health=$("$repo_root/bin/teak" headless session health --json --root "$project")
health_code=$?
set -e
[ "$health_code" -eq 1 ]
cleanup=$("$repo_root/bin/teak" headless session cleanup --confirm --json --root "$project")
after=$("$repo_root/bin/teak" headless session list --json --root "$project")

python3 - "$health" "$cleanup" "$after" <<'PY'
import json
import sys

health, cleanup, after = map(json.loads, sys.argv[1:])
assert health["state"] == "stale"
assert {entry["name"] for entry in health["sessions"]} == {"healthy", "stale"}
stale = next(entry for entry in health["sessions"] if entry["name"] == "stale")
assert stale["state"] == "stale" and stale["issues"][0]["state"] == "missing"
assert cleanup["state"] == "cleaned" and cleanup["removed"] == ["stale"]
assert cleanup["skipped"] == ["healthy"]
assert after["names"] == ["healthy"]
PY

printf '%s\n' 'SESSION_HEALTH_JSON_OK'
