#!/bin/sh
set -eu

ROOT=$(pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export HOME="$TMP/home"
export XDG_CONFIG_HOME="$HOME/.config"
PROJECT="$TMP/project"
mkdir -p "$XDG_CONFIG_HOME/teak" "$PROJECT"
printf 'package main\n' > "$PROJECT/main.go"
JSON=$("$ROOT/bin/teak" headless health --json --root "$PROJECT")
python3 -c 'import json,sys; p=json.loads(sys.argv[1]); s=p["summary"]; ls=p["language_servers"]; scan=p["language_scan"]; actions=p.get("actions", []); assert p["state"] in {"healthy","degraded","failed"}; assert p.get("collected_at"); assert s["tools_total"] == len(p["tools"]); assert s["lsp_total"] == len(ls) == 1; assert ls[0]["language_id"] == "go" and ls[0]["detected_files"] == 1; assert scan["truncated"] is False and scan["scanned_files"] == 1; assert s["actions"] == len(actions) and len(actions) <= 32; assert all(a.get("component") and a.get("action") and a.get("state") for a in actions); assert isinstance(p["git"]["changed"], int) and p["git"]["changed"] >= 0; assert isinstance(p["metrics"]["heap_alloc_bytes"], int) and p["metrics"]["heap_alloc_bytes"] > 0; assert all(k in p["timings_ms"] and p["timings_ms"][k] >= 0 for k in ("tools_ms", "lsp_ms", "git_ms", "metrics_ms")); assert len(p.get("issues", [])) <= 32' "$JSON"
printf '%s\nHEADLESS_HEALTH_JSON_OK\n' "$JSON"
