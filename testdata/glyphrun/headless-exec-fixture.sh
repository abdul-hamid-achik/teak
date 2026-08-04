#!/bin/sh
set -eu

ROOT=$(pwd)
TMP=$(mktemp -d)
PROJECT="$TMP/project"
MARKER="$TMP/should-not-run"
mkdir -p "$PROJECT"

if "$ROOT/bin/teak" headless exec --json --root "$PROJECT" -- sh -c "touch '$MARKER'" >/dev/null 2>&1; then
    exit 3
fi
if [ -e "$MARKER" ]; then
    echo "headless exec ran a command without confirmation" >&2
    exit 4
fi

JSON=$(
    "$ROOT/bin/teak" headless exec --confirm --json --root "$PROJECT" -- sh -c \
        'printf "HEADLESS_EXEC_STDOUT\n"; printf "HEADLESS_EXEC_STDERR\n" >&2; pwd'
)

python3 -c "import json,sys; p=json.loads(sys.argv[1]); root=sys.argv[2]; assert p.get('state') == 'completed', p; assert p.get('exit_code') == 0, p; assert p.get('workspace') == root, p; assert p.get('command') == 'sh', p; assert p.get('args') == ['-c', 'printf \"HEADLESS_EXEC_STDOUT\\\\n\"; printf \"HEADLESS_EXEC_STDERR\\\\n\" >&2; pwd'], p; assert p.get('stdout') == 'HEADLESS_EXEC_STDOUT\\n' + root + '\\n', p; assert p.get('stderr') == 'HEADLESS_EXEC_STDERR\\n', p; assert p.get('truncated') is False, p" "$JSON" "$PROJECT"
printf '%s\nHEADLESS_EXEC_JSON_OK\n' "$JSON"
