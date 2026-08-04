#!/bin/sh

# Deterministic contract test for perf-tools.sh. Real codemap/vecgrep runs are
# intentionally separate because they depend on the installed tool versions
# and, for vecgrep, may require a local model.

set -eu

timeout_bin=$(command -v timeout 2>/dev/null || command -v gtimeout 2>/dev/null || true)
if [ ! -x /usr/bin/time ] || ! command -v python3 >/dev/null 2>&1 || [ -z "$timeout_bin" ]; then
  printf '%s\n' 'SKIP perf-tools contract: /usr/bin/time, python3, and timeout are required'
  exit 0
fi

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/teak-perf-tools-test.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

cat >"$tmp_dir/codemap" <<'SH'
#!/bin/sh
case "$1" in
  init|index)
    exit 0
    ;;
  structural-manifest)
    printf '%s\n' '{"schema_version":1,"export_schema_version":1,"project":"fixture","project_key":"fixture","index_fingerprint":"fingerprint","total_records":1,"complete":true,"freshness":{"checked":true,"fresh":true,"changed":0,"new":0,"deleted":0}}'
    ;;
  *)
    printf 'unexpected codemap operation: %s\n' "$1" >&2
    exit 2
    ;;
esac
SH
cat >"$tmp_dir/vecgrep" <<'SH'
#!/bin/sh
case "$1" in
  init|index)
    exit 0
    ;;
  *)
    printf 'unexpected vecgrep operation: %s\n' "$1" >&2
    exit 2
    ;;
esac
SH
chmod 755 "$tmp_dir/codemap" "$tmp_dir/vecgrep"

output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep" \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3
)

case "$output" in
  *'corpus=synthetic-go files=3'*'codemap-index state=ready rss_available=true'*'codemap-status state=ready rss_available=true'*'vecgrep-index state=ready rss_available=true'*)
    printf '%s\n' 'PASS perf-tools contract'
    ;;
  *)
    printf '%s\n' "$output" >&2
    printf '%s\n' 'perf-tools contract output did not contain all expected measurements' >&2
    exit 1
    ;;
esac

json_output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep" \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3 --json
)
printf '%s\n' "$json_output" | python3 -c '
import json
import sys

payload = json.load(sys.stdin)
assert payload["schema_version"] == 1
assert payload["corpus"] == {"kind": "synthetic-go", "stress_files": 3}
measurements = {item["label"]: item for item in payload["measurements"]}
assert {"codemap-index", "codemap-status", "vecgrep-index"} <= set(measurements)
assert all(item["state"] == "ready" for item in measurements.values())
assert all(item["rss_available"] and item["rss_bytes"] >= 0 for item in measurements.values())
assert payload["rss_budget_bytes"] is None
assert all(not item["budget_exceeded"] for item in measurements.values())
'
printf '%s\n' 'PASS perf-tools JSON contract'

set +e
invalid_budget_output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep" \
  TEAK_PERF_MAX_RSS_BYTES=-1 \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3 2>&1
)
invalid_budget_status=$?
set -e
if [ "$invalid_budget_status" -ne 2 ]; then
  printf '%s\n' "$invalid_budget_output" >&2
  printf '%s\n' 'perf-tools invalid RSS budget was not rejected' >&2
  exit 1
fi
case "$invalid_budget_output" in
  *'TEAK_PERF_MAX_RSS_BYTES must be a non-negative integer number of bytes'*) ;;
  *)
    printf '%s\n' "$invalid_budget_output" >&2
    printf '%s\n' 'perf-tools invalid RSS budget error was not descriptive' >&2
    exit 1
    ;;
esac
printf '%s\n' 'PASS perf-tools RSS budget validation contract'

set +e
budget_output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep" \
  TEAK_PERF_MAX_RSS_BYTES=1 \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3 2>&1
)
budget_status=$?
set -e
case "$budget_output" in
  *'codemap-index state=over_budget rss_available=true'*'codemap-status state=over_budget rss_available=true'*'vecgrep-index state=over_budget rss_available=true'*)
    printf '%s\n' 'PASS perf-tools RSS budget contract'
    ;;
  *)
    printf '%s\n' "$budget_output" >&2
    printf '%s\n' 'perf-tools RSS budget contract did not report over_budget' >&2
    exit 1
    ;;
esac
if [ "$budget_status" -eq 0 ]; then
  printf '%s\n' 'perf-tools RSS budget unexpectedly returned success' >&2
  exit 1
fi

set +e
budget_json_output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep" \
  TEAK_PERF_MAX_RSS_BYTES=1 \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3 --json 2>/dev/null
)
budget_json_status=$?
set -e
if [ "$budget_json_status" -eq 0 ]; then
  printf '%s\n' 'perf-tools JSON RSS budget unexpectedly returned success' >&2
  exit 1
fi
printf '%s\n' "$budget_json_output" | python3 -c '
import json
import sys

payload = json.load(sys.stdin)
assert payload["rss_budget_bytes"] == 1
measurements = {item["label"]: item for item in payload["measurements"]}
assert all(item["state"] == "over_budget" for item in measurements.values())
assert all(item["budget_exceeded"] for item in measurements.values())
assert all(item["rss_budget_bytes"] == 1 for item in measurements.values())
'
printf '%s\n' 'PASS perf-tools JSON RSS budget contract'

cat >"$tmp_dir/codemap-legacy" <<'SH'
#!/bin/sh
case "$1" in
  init|index)
    exit 0
    ;;
  structural-manifest)
    exit 2
    ;;
  status)
    printf '%s\n' 'legacy-status-called' >> "$CODEMAP_LEGACY_TRACE"
    exit 0
    ;;
  *)
    exit 2
    ;;
esac
SH
chmod 755 "$tmp_dir/codemap-legacy"
legacy_trace="$tmp_dir/legacy-trace"
legacy_output=$(
  CODEMAP_BIN="$tmp_dir/codemap-legacy" \
  VECGREP_BIN="$tmp_dir/vecgrep" \
  CODEMAP_LEGACY_TRACE="$legacy_trace" \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3
)
case "$legacy_output" in
  *'codemap-status state=unsupported reason=structural-manifest-unavailable'*)
    ;;
  *)
    printf '%s\n' "$legacy_output" >&2
    printf '%s\n' 'legacy codemap was not reported as unsupported' >&2
    exit 1
    ;;
esac
if [ -s "$legacy_trace" ]; then
  printf '%s\n' 'legacy codemap status path was invoked' >&2
  exit 1
fi

cat >"$tmp_dir/vecgrep-timeout" <<'SH'
#!/bin/sh
case "$1" in
  init)
    exit 0
    ;;
  index)
    sleep 2
    ;;
  *)
    exit 2
    ;;
esac
SH
chmod 755 "$tmp_dir/vecgrep-timeout"
set +e
timeout_output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep-timeout" \
  TEAK_PERF_TOOL_TIMEOUT=1 \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3 2>&1
)
timeout_status=$?
set -e
case "$timeout_output" in
  *'vecgrep-index state=timed_out rss_available='*)
    printf '%s\n' 'PASS perf-tools timeout contract'
    ;;
  *)
    printf '%s\n' "$timeout_output" >&2
    printf '%s\n' 'perf-tools timeout contract did not report timed_out' >&2
    exit 1
    ;;
esac
if [ "$timeout_status" -eq 0 ]; then
  printf '%s\n' 'perf-tools timeout unexpectedly returned success' >&2
  exit 1
fi
printf '%s\n' 'PASS legacy codemap safety contract'

set +e
json_timeout_output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep-timeout" \
  TEAK_PERF_TOOL_TIMEOUT=1 \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3 --json 2>/dev/null
)
json_timeout_status=$?
set -e
if [ "$json_timeout_status" -eq 0 ]; then
  printf '%s\n' 'perf-tools JSON timeout unexpectedly returned success' >&2
  exit 1
fi
printf '%s\n' "$json_timeout_output" | python3 -c '
import json
import sys

payload = json.load(sys.stdin)
measurements = {item["label"]: item for item in payload["measurements"]}
assert measurements["vecgrep-index"]["state"] == "timed_out"
'
printf '%s\n' 'PASS perf-tools JSON timeout contract'

cat >"$tmp_dir/vecgrep-init-timeout" <<'SH'
#!/bin/sh
case "$1" in
  init)
    sleep 2
    ;;
  index)
    exit 0
    ;;
  *)
    exit 2
    ;;
esac
SH
chmod 755 "$tmp_dir/vecgrep-init-timeout"
set +e
init_timeout_output=$(
  CODEMAP_BIN="$tmp_dir/codemap" \
  VECGREP_BIN="$tmp_dir/vecgrep-init-timeout" \
  TEAK_PERF_TOOL_TIMEOUT=1 \
  sh "$repo_root/scripts/perf-tools.sh" --stress-files 3 2>&1
)
init_timeout_status=$?
set -e
case "$init_timeout_output" in
  *'vecgrep-init state=timed_out rss_available='*'vecgrep-index state=skipped reason=vecgrep-init-failed'*)
    printf '%s\n' 'PASS perf-tools setup timeout contract'
    ;;
  *)
    printf '%s\n' "$init_timeout_output" >&2
    printf '%s\n' 'perf-tools setup timeout contract did not stop before indexing' >&2
    exit 1
    ;;
esac
if [ "$init_timeout_status" -eq 0 ]; then
  printf '%s\n' 'perf-tools setup timeout unexpectedly returned success' >&2
  exit 1
fi
