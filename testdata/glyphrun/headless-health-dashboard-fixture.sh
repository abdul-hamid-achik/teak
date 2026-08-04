#!/bin/sh
set -eu

ROOT=$(pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export HOME="$TMP/home"
export XDG_CONFIG_HOME="$HOME/.config"
export XDG_STATE_HOME="$TMP/state"
PROJECT="$TMP/project"
mkdir -p "$XDG_CONFIG_HOME/teak" "$PROJECT"
printf 'package main\n' > "$PROJECT/main.go"

RECORD=$("$ROOT/bin/teak" headless health record --confirm --json --root "$PROJECT")
DASHBOARD=$("$ROOT/bin/teak" headless health dashboard --limit 1 --json --root "$PROJECT")
python3 -c 'import json,sys; record,dashboard=map(json.loads,sys.argv[1:]); assert dashboard["state"] == dashboard["current"]["state"]; assert dashboard["current"].get("collected_at"); assert dashboard["history"]["state"] == "ready" and len(dashboard["history"]["snapshots"]) == 1; assert dashboard["trend"]["entries"] == 1 and dashboard["trend"]["latest_at"] == record["recorded_at"]; assert dashboard["trend"]["latest_state"] == record["snapshot"]["state"]' "$RECORD" "$DASHBOARD"
printf '%s\nHEADLESS_HEALTH_DASHBOARD_JSON_OK\n' "$DASHBOARD"
