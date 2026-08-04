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

alpha="$tmp_dir/alpha"
beta="$tmp_dir/beta"
mkdir -p "$alpha" "$beta"
printf '%s\n' 'alpha' > "$alpha/alpha.txt"
printf '%s\n' 'beta' > "$beta/beta.txt"

server_log="$tmp_dir/server.json"
"$repo_root/bin/teak" headless serve \
	--listen 127.0.0.1:0 \
	--token test-token \
	--json \
	--workspace "alpha=$alpha" \
	--workspace "beta=$beta" \
	>"$server_log" 2>"$tmp_dir/server.err" &
server_pid=$!

python3 - "$server_log" "$server_pid" "$alpha" "$beta" <<'PY'
import json
import os
import sys
import time
import urllib.error
import urllib.request

server_log, server_pid, alpha, beta = sys.argv[1:]
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

assert startup["state"] == "ready"
assert startup["default"] == "alpha"
assert {entry["name"] for entry in startup["workspaces"]} == {"alpha", "beta"}
base = "http://" + startup["address"]
headers = {"Authorization": "Bearer test-token"}

def request(route):
    request = urllib.request.Request(base + route, headers=headers, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)

def mutate(route, payload, confirmed=True):
    request = urllib.request.Request(
        base + route,
        data=json.dumps(payload).encode("utf-8"),
        headers={**headers, "Content-Type": "application/json", "X-Teak-Confirm": "true"} if confirmed else {**headers, "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)

status, listing = request("/v1/workspaces")
assert status == 200
assert listing["default"] == "alpha"
assert {entry["name"] for entry in listing["workspaces"]} == {"alpha", "beta"}

status, default_context = request("/v1/context?root=/")
assert status == 200
assert default_context["workspace"].endswith("/alpha")
assert [entry["path"] for entry in default_context["entries"]] == ["alpha.txt"]

status, beta_context = request("/v1/workspaces/beta/context?root=/")
assert status == 200
assert beta_context["workspace"].endswith("/beta")
assert [entry["path"] for entry in beta_context["entries"]] == ["beta.txt"]

status, beta_sessions = request("/v1/workspaces/beta/session/list")
assert status == 200 and beta_sessions["names"] == []

status, unknown = request("/v1/workspaces/gamma/context")
assert status == 404 and unknown["code"] == "not_found"

status, project = mutate("/v1/workspaces/beta/project/mkdir", {"source": "created"})
assert status == 200
assert project["operation"] == "mkdir" and project["committed"] is True
assert os.path.isdir(os.path.join(beta, "created"))
assert not os.path.exists(os.path.join(alpha, "created"))
print("HEADLESS_REST_MULTI_WORKSPACE_OK")
print("HEADLESS_REST_ROOT_ALLOWLIST_OK")
print("HEADLESS_REST_PROJECT_MUTATION_OK")
PY
