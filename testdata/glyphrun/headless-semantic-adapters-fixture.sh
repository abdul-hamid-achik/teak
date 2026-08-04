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
bin_dir="$tmp_dir/bin"
trace="$tmp_dir/vecgrep.trace"
indexed="$tmp_dir/vecgrep.indexed"
mkdir -p "$project" "$bin_dir"
printf '%s\n' 'package main' >"$project/main.go"
: >"$trace"

cat >"$bin_dir/vecgrep" <<'SH'
#!/bin/sh
set -eu
case "$1" in
--version)
	printf '%s\n' 'vecgrep adapter fixture'
	;;
status)
	if [ -f "$VECGREP_INDEXED" ]; then
		printf '%s\n' '{"index_fresh":true,"stats":{"files":1},"pending_changes":{"total_pending":0},"freshness":{"state":"fresh"},"lightweight":true}'
	else
		printf '%s\n' '{"index_fresh":false,"stats":{"files":1},"pending_changes":{"total_pending":1},"freshness":{"state":"stale"},"lightweight":true}'
	fi
	;;
index)
	printf '%s\n' index >>"$VECGREP_TRACE"
	: >"$VECGREP_INDEXED"
	;;
search)
	printf '%s\n' search >>"$VECGREP_TRACE"
	printf '%s\n' '{"schema_version":1,"index":{"indexed":true,"fresh":true,"chunks":1},"hits":[{"file_path":"main.go","relative_path":"main.go","line":2,"col":1,"end_line":2,"preview":"adapter fixture hit","score":0.9,"symbol_name":"main","chunk_type":"function"}]}'
	;;
*)
	exit 2
	;;
esac
SH
chmod 755 "$bin_dir/vecgrep"
export HOME="$tmp_dir/home"
export PATH="$bin_dir:$PATH"
export VECGREP_TRACE="$trace"
export VECGREP_INDEXED="$indexed"

server_log="$tmp_dir/server.json"
"$repo_root/bin/teak" headless serve --listen 127.0.0.1:0 --token test-token --json --root "$project" >"$server_log" 2>"$tmp_dir/server.err" &
server_pid=$!

python3 - "$server_log" "$server_pid" "$trace" <<'PY'
import json
import os
import sys
import time
import urllib.error
import urllib.request

server_log, server_pid, trace = sys.argv[1:]
startup = None
for _ in range(150):
    try:
        with open(server_log, encoding="utf-8") as stream:
            startup = json.load(stream)
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

def get(path, confirm=False):
    request = urllib.request.Request(
        "http://" + startup["address"] + path,
        headers={"X-Teak-Token": "test-token", **({"X-Teak-Confirm": "true"} if confirm else {})},
    )
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)

status, rejected = get("/v1/search?q=meaning&semantic=true&index=true")
assert status == 428 and rejected.get("code") == "confirmation_required", (status, rejected)
assert open(trace, encoding="utf-8").read() == "", "unconfirmed REST request launched vecgrep"

status, accepted = get("/v1/search?q=meaning&semantic=true&index=true", confirm=True)
assert status == 200 and accepted.get("state") == "ready" and accepted.get("indexed") is True, (status, accepted)
PY

kill -TERM "$server_pid" 2>/dev/null || true
wait "$server_pid" 2>/dev/null || true
server_pid=""

MCP_OUTPUT=$(printf '%s\n' \
	'{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}' \
	'{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"teak_search","arguments":{"query":"meaning","semantic":true,"index":true}}}' \
	'{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"teak_search","arguments":{"query":"meaning","semantic":true,"index":true,"confirm":true}}}' \
	| "$repo_root/bin/teak" headless mcp --root "$project")

MCP_OUTPUT="$MCP_OUTPUT" TRACE="$trace" python3 - <<'PY'
import json
import os

responses = [json.loads(line) for line in os.environ["MCP_OUTPUT"].splitlines() if line.strip()]
by_id = {str(item.get("id")): item for item in responses}
assert by_id["1"]["error"]["code"] == -32602, responses
text = by_id["2"]["result"]["content"][0]["text"]
payload = json.loads(text)
assert payload.get("state") == "ready" and payload.get("indexed") is True, payload
calls = open(os.environ["TRACE"], encoding="utf-8").read().splitlines()
assert calls.count("index") == 1 and calls.count("search") == 2, calls
print("SEMANTIC_ADAPTERS_JSON_OK")
PY
