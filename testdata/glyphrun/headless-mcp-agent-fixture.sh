#!/bin/sh
set -eu

ROOT=$(pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export HOME="$TMP/home"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_STATE_HOME="$TMP/state"
PROJECT="$TMP/project"
mkdir -p "$PROJECT"
printf 'package main\n' > "$PROJECT/main.go"
STORE="$XDG_STATE_HOME/teak/agent-runs/$(python3 -c 'import hashlib,os,sys; print(hashlib.sha256(os.path.realpath(sys.argv[1]).encode()).hexdigest())' "$PROJECT")/runs.json"
mkdir -p "$(dirname "$STORE")"
python3 - "$STORE" "$PROJECT" <<'PY'
import json
import sys

store, project = sys.argv[1:]
json.dump({"version": 1, "runs": [{
    "id": "mcp-active-run",
    "depth": 0,
    "status": "running",
    "spec": {"objective": "MCP cancel fixture", "workspace": project,
             "requested_capabilities": {"read": True}, "budget": {}},
    "effective_capabilities": {"read": True},
    "created_at": "2020-01-01T00:00:00Z",
    "started_at": "2020-01-01T00:00:00Z",
    "last_heartbeat_at": "2020-01-01T00:00:00Z",
}]}, open(store, "w"))
PY

python3 - "$ROOT/bin/teak" "$PROJECT" "$STORE" <<'PY'
import json
import subprocess
import sys

binary, project, store = sys.argv[1:]
proc = subprocess.Popen(
    [binary, "headless", "mcp", "--root", project],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
    bufsize=1,
)

def request(request_id, method, params=None):
    message = {"jsonrpc": "2.0", "id": request_id, "method": method}
    if params is not None:
        message["params"] = params
    proc.stdin.write(json.dumps(message) + "\n")
    proc.stdin.flush()
    while True:
        line = proc.stdout.readline()
        if not line:
            raise AssertionError("MCP server closed stdout before response")
        response = json.loads(line)
        if response.get("id") == request_id:
            return response

initialize = request(1, "initialize", {})
assert initialize["result"]["serverInfo"]["name"] == "teak"
tools = request(2, "tools/list", {})
names = {tool["name"] for tool in tools["result"]["tools"]}
assert {"teak_agent_cancel", "teak_agent_reap_stale"} <= names

denied = request(3, "tools/call", {
    "name": "teak_agent_cancel",
    "arguments": {"run_id": "mcp-active-run"},
})
assert denied.get("error", {}).get("code") == -32602

cancelled = request(4, "tools/call", {
    "name": "teak_agent_cancel",
    "arguments": {"run_id": "mcp-active-run", "confirm": True},
})
cancelled_json = json.loads(cancelled["result"]["content"][0]["text"])
assert cancelled_json["state"] == "cancelled"

with open(store, encoding="utf-8") as stream:
    state = json.load(stream)
state["runs"].append({
    "id": "mcp-stale-run",
    "depth": 0,
    "status": "running",
    "spec": {"objective": "MCP reap fixture", "workspace": project,
             "requested_capabilities": {"read": True}, "budget": {}},
    "effective_capabilities": {"read": True},
    "created_at": "2020-01-01T00:00:00Z",
    "started_at": "2020-01-01T00:00:00Z",
    "last_heartbeat_at": "2020-01-01T00:00:00Z",
})
with open(store, "w", encoding="utf-8") as stream:
    json.dump(state, stream)

reaped = request(5, "tools/call", {
    "name": "teak_agent_reap_stale",
    "arguments": {"max_silence": "1m", "confirm": True},
})
reaped_json = json.loads(reaped["result"]["content"][0]["text"])
assert reaped_json["state"] == "reaped" and reaped_json["reaped"] == ["mcp-stale-run"]

proc.stdin.close()
assert proc.wait(timeout=3) == 0, proc.stderr.read()
print("HEADLESS_MCP_AGENT_CONFIRMATION_OK")
print("HEADLESS_MCP_AGENT_CANCEL_OK")
print("HEADLESS_MCP_AGENT_REAP_OK")
PY
