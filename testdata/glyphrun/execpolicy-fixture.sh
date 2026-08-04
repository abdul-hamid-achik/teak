#!/bin/sh
set -eu

go test ./internal/execpolicy -count=1
printf '%s\n' 'EXEC_POLICY_GLYPHRUN_OK'
