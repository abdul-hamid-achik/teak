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

export HOME="$tmp_dir/home"
export XDG_STATE_HOME="$tmp_dir/state"
project="$tmp_dir/project"
mkdir -p "$project"
printf '%s\n' 'package main' > "$project/main.go"
store="$XDG_STATE_HOME/teak/agent-runs/$(python3 -c 'import hashlib,os,sys; print(hashlib.sha256(os.path.realpath(sys.argv[1]).encode()).hexdigest())' "$project")/runs.json"
mkdir -p "$(dirname "$store")"
python3 - "$store" "$project" <<'PY'
import json
import sys

store, project = sys.argv[1:]
json.dump({"version": 1, "runs": [{
    "id": "rest-active-run",
    "depth": 0,
    "status": "running",
    "spec": {"objective": "REST cancel fixture", "workspace": project,
             "requested_capabilities": {"read": True}, "budget": {}},
    "effective_capabilities": {"read": True},
    "created_at": "2020-01-01T00:00:00Z",
    "started_at": "2020-01-01T00:00:00Z",
    "last_heartbeat_at": "2020-01-01T00:00:00Z",
}]}, open(store, "w"))
PY

server_log="$tmp_dir/server.json"
"$repo_root/bin/teak" headless serve --listen 127.0.0.1:0 --token test-token --json --root "$project" >"$server_log" 2>"$tmp_dir/server.err" &
server_pid=$!

python3 - "$server_log" "$server_pid" "$project" "$store" <<'PY'
import json
import os
import sys
import time
import urllib.error
import urllib.request

server_log, server_pid, project, store = sys.argv[1:]
address = None
for _ in range(100):
    try:
        with open(server_log, encoding="utf-8") as stream:
            payload = stream.read()
        if payload:
            address = json.loads(payload)["address"]
            break
    except (FileNotFoundError, json.JSONDecodeError, KeyError):
        pass
    try:
        os.kill(int(server_pid), 0)
    except ProcessLookupError:
        raise SystemExit("REST server exited before publishing its address")
    time.sleep(0.02)
if not address:
    raise SystemExit("timed out waiting for REST address")

base = "http://" + address
headers = {"Authorization": "Bearer test-token"}

def request(method, route, extra=None):
    request_headers = dict(headers)
    if extra:
        request_headers.update(extra)
    request = urllib.request.Request(base + route, headers=request_headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)

status, denied = request("POST", "/v1/agent/cancel?run_id=rest-active-run")
assert status == 428 and denied["code"] == "confirmation_required"

status, cancelled = request("POST", "/v1/agent/cancel?run_id=rest-active-run", {"X-Teak-Confirm": "true"})
assert status == 200 and cancelled["state"] == "cancelled"
with open(store, encoding="utf-8") as stream:
    state = json.load(stream)
assert state["runs"][0]["status"] == "cancelled"

state["runs"].append({
    "id": "rest-stale-run",
    "depth": 0,
    "status": "running",
    "spec": {"objective": "REST reap fixture", "workspace": project,
             "requested_capabilities": {"read": True}, "budget": {}},
    "effective_capabilities": {"read": True},
    "created_at": "2020-01-01T00:00:00Z",
    "started_at": "2020-01-01T00:00:00Z",
    "last_heartbeat_at": "2020-01-01T00:00:00Z",
})
with open(store, "w", encoding="utf-8") as stream:
    json.dump(state, stream)

status, reaped = request("POST", "/v1/agent/reap-stale?max_silence=1m", {"X-Teak-Confirm": "true"})
assert status == 200 and reaped["state"] == "reaped" and reaped["reaped"] == ["rest-stale-run"]
with open(store, encoding="utf-8") as stream:
    state = json.load(stream)
assert state["runs"][1]["status"] == "interrupted"
print("HEADLESS_REST_AGENT_CONFIRMATION_OK")
print("HEADLESS_REST_AGENT_CANCEL_OK")
print("HEADLESS_REST_AGENT_REAP_OK")
PY
