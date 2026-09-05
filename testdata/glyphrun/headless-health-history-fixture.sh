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

SNAPSHOT=$("$ROOT/bin/teak" headless health --json --root "$PROJECT")
RECORD=$("$ROOT/bin/teak" headless health record --confirm --json --root "$PROJECT")
HISTORY=$("$ROOT/bin/teak" headless health history --limit 1 --json --root "$PROJECT")
python3 -c 'import json,sys; snapshot,record,history=map(json.loads,sys.argv[1:]); assert snapshot["summary"]["actions"] == len(snapshot.get("actions", [])); assert record["state"] == "recorded" and record["entries"] == 1; assert history["state"] == "ready" and history["limit"] == 1 and len(history["snapshots"]) == 1; assert history["snapshots"][0]["recorded_at"] == record["recorded_at"]' "$SNAPSHOT" "$RECORD" "$HISTORY"
# JSON assertions above verify real values; volatile measurements are not screen contracts.
printf '%s\n' 'HEADLESS_HEALTH_HISTORY_JSON_OK'
