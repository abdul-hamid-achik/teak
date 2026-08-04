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

python3 - "$ROOT/bin/teak" "$PROJECT" <<'PY'
import json
import os
import subprocess
import sys

binary, project = sys.argv[1:]
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
assert {
    "teak_context", "teak_search", "teak_tools_status", "teak_health_dashboard",
    "teak_health_history", "teak_agent_list", "teak_project_mkdir",
    "teak_project_rename", "teak_project_copy", "teak_project_remove",
    "teak_session_list", "teak_session_health", "teak_lsp_diagnostics",
    "teak_lsp_format", "teak_dap_probe", "teak_agent_show",
} <= names

context = request(3, "tools/call", {
    "name": "teak_context",
    "arguments": {"depth": 0},
})
context_text = context["result"]["content"][0]["text"]
context_json = json.loads(context_text)
assert context_json["workspace"] == project
assert any(entry["path"] == "main.go" for entry in context_json["entries"])

sessions = request(13, "tools/call", {
    "name": "teak_session_list",
    "arguments": {},
})
sessions_json = json.loads(sessions["result"]["content"][0]["text"])
assert sessions_json["names"] == []

history = request(4, "tools/call", {
    "name": "teak_health_history",
    "arguments": {"limit": 1},
})
history_json = json.loads(history["result"]["content"][0]["text"])
assert history_json["state"] == "empty" and history_json["snapshots"] == []

dashboard = request(5, "tools/call", {
    "name": "teak_health_dashboard",
    "arguments": {"limit": 1},
})
dashboard_json = json.loads(dashboard["result"]["content"][0]["text"])
assert dashboard_json["history"]["state"] == "empty" and dashboard_json["trend"]["entries"] == 0

read = request(6, "tools/call", {
    "name": "teak_buffer_read",
    "arguments": {"path": "main.go"},
})
read_json = json.loads(read["result"]["content"][0]["text"])
assert read_json["content"] == "package main\n"

write = request(7, "tools/call", {
    "name": "teak_buffer_write",
    "arguments": {
        "path": "main.go",
        "expected_sha256": read_json["sha256"],
        "content": "package main\n// edited through MCP\n",
        "confirm": True,
    },
})
write_json = json.loads(write["result"]["content"][0]["text"])
assert not write["result"].get("isError") and write_json["content"] == "package main\n// edited through MCP\n"

stale = request(8, "tools/call", {
    "name": "teak_buffer_write",
    "arguments": {
        "path": "main.go",
        "expected_sha256": read_json["sha256"],
        "content": "stale\n",
        "confirm": True,
    },
})
stale_json = json.loads(stale["result"]["content"][0]["text"])
assert stale["result"].get("isError") and stale_json["code"] == "stale_write"

with open(project + "/main.go", encoding="utf-8") as stream:
    assert stream.read() == "package main\n// edited through MCP\n"

mkdir = request(9, "tools/call", {
    "name": "teak_project_mkdir",
    "arguments": {"path": "scratch", "confirm": True},
})
mkdir_json = json.loads(mkdir["result"]["content"][0]["text"])
assert mkdir_json["committed"] and os.path.isdir(project + "/scratch")

rename = request(10, "tools/call", {
    "name": "teak_project_rename",
    "arguments": {"source": "scratch", "destination": "moved", "confirm": True},
})
rename_json = json.loads(rename["result"]["content"][0]["text"])
assert rename_json["committed"] and os.path.isdir(project + "/moved")

copy = request(11, "tools/call", {
    "name": "teak_project_copy",
    "arguments": {"source": "moved", "destination": "copied", "confirm": True},
})
copy_json = json.loads(copy["result"]["content"][0]["text"])
assert copy_json["committed"] and os.path.isdir(project + "/copied")

remove = request(12, "tools/call", {
    "name": "teak_project_remove",
    "arguments": {"path": "copied", "confirm": True},
})
remove_json = json.loads(remove["result"]["content"][0]["text"])
assert remove_json["committed"] and not os.path.exists(project + "/copied")

proc.stdin.close()
assert proc.wait(timeout=3) == 0, proc.stderr.read()
print("HEADLESS_MCP_JSON_OK")
print("HEADLESS_MCP_WRITE_OK")
print("HEADLESS_MCP_STALE_GUARD_OK")
print("HEADLESS_MCP_PROJECT_MUTATIONS_OK")
PY
