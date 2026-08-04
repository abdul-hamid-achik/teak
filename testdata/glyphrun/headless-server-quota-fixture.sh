#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp_dir=$(mktemp -d)
server_pid=""
cleanup() {
	if [ -n "$server_pid" ]; then
		kill -TERM "$server_pid" 2>/dev/null || true
		wait "$server_pid" 2>/dev/null || true
	fi
	rm -rf -- "$tmp_dir"
}
trap cleanup EXIT INT TERM

project="$tmp_dir/project"
mkdir -p "$project"
printf '%s\n' 'package main' > "$project/main.go"
server_log="$tmp_dir/server.json"
"$repo_root/bin/teak" headless serve --listen 127.0.0.1:0 --token test-token --json --root "$project" >"$server_log" 2>"$tmp_dir/server.err" &
server_pid=$!

python3 - "$server_log" "$server_pid" <<'PY'
import json
import os
import sys
import time
import urllib.request

server_log, server_pid = sys.argv[1:]
startup = None
for _ in range(100):
    try:
        with open(server_log, encoding="utf-8") as stream:
            payload = stream.read()
        if payload:
            startup = json.loads(payload)
            break
    except (FileNotFoundError, json.JSONDecodeError):
        pass
    try:
        os.kill(int(server_pid), 0)
    except ProcessLookupError:
        raise SystemExit("REST server exited before publishing its address")
    time.sleep(0.02)
if not startup:
    raise SystemExit("timed out waiting for REST address")

with urllib.request.urlopen("http://" + startup["address"] + "/healthz", timeout=5) as response:
    assert response.status == 200
    health = json.load(response)
quota = health["quota"]
assert health["state"] == "ready"
assert quota["active"] == 0
assert quota["max_concurrent"] == 8
assert quota["reserved_output_bytes"] == 0
assert quota["max_output_bytes"] >= 8 * (1 << 20)
print("HEADLESS_SERVER_QUOTA_OK")
PY
