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
printf '%s\n' 'before' > "$project/notes.txt"

server_log="$tmp_dir/server.json"
"$repo_root/bin/teak" headless serve --listen 127.0.0.1:0 --token test-token --json --root "$project" >"$server_log" 2>"$tmp_dir/server.err" &
server_pid=$!

python3 - "$server_log" "$server_pid" "$project/notes.txt" <<'PY'
import hashlib
import json
import os
import sys
import time
import urllib.error
import urllib.request

server_log, server_pid, path = sys.argv[1:]
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

def request(method, route, body=None, extra=None):
    request_headers = dict(headers)
    if extra:
        request_headers.update(extra)
    payload = None
    if body is not None:
        payload = json.dumps(body).encode()
        request_headers["Content-Type"] = "application/json"
    request = urllib.request.Request(base + route, data=payload, headers=request_headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)

status, before = request("GET", "/v1/buffer/read?path=notes.txt")
assert status == 200 and before["content"] == "before\n"
sha = before["sha256"]

status, denied = request("POST", "/v1/buffer/write", {
    "path": "notes.txt", "expected_sha256": sha, "content": "denied\n"
})
assert status == 428 and denied["code"] == "confirmation_required"

status, updated = request("POST", "/v1/buffer/write", {
    "path": "notes.txt", "expected_sha256": sha, "content": "after\n"
}, {"X-Teak-Confirm": "true"})
assert status == 200 and updated["content"] == "after\n"
assert updated["sha256"] != sha

status, stale = request("POST", "/v1/buffer/write", {
    "path": "notes.txt", "expected_sha256": sha, "content": "stale\n"
}, {"X-Teak-Confirm": "true"})
assert status == 409 and stale["code"] == "stale_write"
with open(path, "rb") as stream:
    assert stream.read() == b"after\n"
assert hashlib.sha256(b"after\n").hexdigest() == updated["sha256"]
print("HEADLESS_REST_WRITE_OK")
print("HEADLESS_REST_STALE_GUARD_OK")
PY
