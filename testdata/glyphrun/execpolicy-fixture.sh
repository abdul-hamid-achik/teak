#!/bin/sh
set -eu

# Keep successful frames deterministic; preserve full diagnostics on failure.
if ! test_output=$(go test ./internal/execpolicy -count=1 2>&1); then
    printf '%s\n' "$test_output" >&2
    exit 1
fi
printf '%s\n' 'EXEC_POLICY_GLYPHRUN_OK'
