#!/bin/sh

# Measure explicit project-intelligence work in isolated temporary stores.
# This intentionally never uses Teak's normal codemap/vecgrep data directory
# and never turns a read-only health check into an index operation.

set -eu

perf_timeout=${TEAK_PERF_TOOL_TIMEOUT:-90}
case "$perf_timeout" in
  ''|*[!0-9]*|0)
    printf '%s\n' 'TEAK_PERF_TOOL_TIMEOUT must be a positive integer number of seconds' >&2
    exit 2
    ;;
esac

rss_budget_bytes=${TEAK_PERF_MAX_RSS_BYTES:-0}
case "$rss_budget_bytes" in
  ''|*[!0-9]*)
    printf '%s\n' 'TEAK_PERF_MAX_RSS_BYTES must be a non-negative integer number of bytes' >&2
    exit 2
    ;;
esac

workspace=
stress_files=${TEAK_PERF_STRESS_FILES:-0}
json_output=false
while [ "$#" -gt 0 ]; do
	case "$1" in
		--json)
			json_output=true
			shift
			;;
		--stress-files)
      if [ "$#" -lt 2 ]; then
        printf '%s\n' '--stress-files requires a positive file count' >&2
        exit 2
      fi
      stress_files=$2
      shift 2
      ;;
    --)
      shift
      break
      ;;
    -*)
      printf 'unknown option: %s\n' "$1" >&2
      exit 2
      ;;
    *)
      if [ -n "$workspace" ]; then
        printf '%s\n' 'perf-tools accepts at most one workspace path' >&2
        exit 2
      fi
      workspace=$1
      shift
      ;;
  esac
done

if [ -z "$workspace" ]; then
  workspace=$(pwd)
fi
if [ ! -d "$workspace" ]; then
  printf 'workspace does not exist: %s\n' "$workspace" >&2
  exit 2
fi
case "$stress_files" in
  ''|*[!0-9]*)
    printf '%s\n' '--stress-files must be a non-negative integer' >&2
    exit 2
    ;;
esac
if [ "$stress_files" -gt 100000 ]; then
  printf '%s\n' '--stress-files is capped at 100000' >&2
  exit 2
fi

time_bin=/usr/bin/time
if [ ! -x "$time_bin" ]; then
  printf 'SKIP: /usr/bin/time is required for RSS measurement\n'
  exit 0
fi
timeout_bin=${TEAK_TIMEOUT_BIN:-}
if [ -z "$timeout_bin" ]; then
  timeout_bin=$(command -v timeout 2>/dev/null || true)
fi
if [ -z "$timeout_bin" ]; then
  timeout_bin=$(command -v gtimeout 2>/dev/null || true)
fi
if [ -z "$timeout_bin" ]; then
  printf 'SKIP: timeout or gtimeout is required for bounded tool measurement\n'
  exit 0
fi
case "$(uname -s)" in
  Darwin) time_flag=-l ;;
  Linux) time_flag=-v ;;
  *)
    printf 'SKIP: unsupported operating system for /usr/bin/time RSS output\n'
    exit 0
    ;;
esac

codemap_bin=${CODEMAP_BIN:-}
if [ -z "$codemap_bin" ]; then
  codemap_bin=$(command -v codemap 2>/dev/null || true)
fi
vecgrep_bin=${VECGREP_BIN:-}
if [ -z "$vecgrep_bin" ]; then
  vecgrep_bin=$(command -v vecgrep 2>/dev/null || true)
fi

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/teak-perf-tools.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM
measurements_file="$tmp_dir/measurements.tsv"
: >"$measurements_file"
measurement_failure_file="$tmp_dir/measurement-failure"
: >"$measurement_failure_file"

record_measurement() {
	label=$1
	state=$2
	rss=$3
	display=${4:-}
	budget_exceeded=false
	if [ "$rss_budget_bytes" -gt 0 ] && [ -n "$rss" ] && [ "$rss" -gt "$rss_budget_bytes" ]; then
		state=over_budget
		budget_exceeded=true
		printf '%s\n' '1' >"$measurement_failure_file"
	fi
	printf '%s\t%s\t%s\t%s\n' "$label" "$state" "$rss" "$budget_exceeded" >>"$measurements_file"
	if [ "$json_output" = false ]; then
		if [ -n "$display" ]; then
			printf '%s\n' "$display"
		elif [ -n "$rss" ]; then
			if [ "$rss_budget_bytes" -gt 0 ]; then
				printf '%s state=%s rss_available=true rss_bytes=%s rss_budget_bytes=%s budget_exceeded=%s\n' "$label" "$state" "$rss" "$rss_budget_bytes" "$budget_exceeded"
			else
				printf '%s state=%s rss_available=true rss_bytes=%s\n' "$label" "$state" "$rss"
			fi
		else
			printf '%s state=%s rss_available=false\n' "$label" "$state"
		fi
	fi
}

if [ "$stress_files" -gt 0 ]; then
  if ! command -v python3 >/dev/null 2>&1; then
    printf '%s\n' 'python3 is required for --stress-files corpus generation' >&2
    exit 2
  fi
  stress_workspace="$tmp_dir/stress-workspace"
  mkdir -p "$stress_workspace"
  python3 - "$stress_workspace" "$stress_files" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
count = int(sys.argv[2])
(root / "go.mod").write_text("module example.com/teak-stress\n\ngo 1.26\n", encoding="utf-8")
(root / "README.md").write_text("synthetic Teak performance corpus\n", encoding="utf-8")
for index in range(count):
    package_dir = root / f"pkg{index % 32:02d}"
    package_dir.mkdir(parents=True, exist_ok=True)
    source = (
        "package stress\n\n"
        f"// Symbol{index} gives codemap and semantic tools a stable declaration.\n"
        f"func Symbol{index}(input int) int {{ return input + {index} }}\n"
    )
    (package_dir / f"file{index:06d}.go").write_text(source, encoding="utf-8")
PY
  workspace="$stress_workspace"
fi

rss_from_report() {
  report=$1
  case "$(uname -s)" in
    Darwin)
      awk '/maximum resident set size/ { print $1; found=1 } END { if (!found) exit 1 }' "$report"
      ;;
    Linux)
      awk '/Maximum resident set size/ { print $6 * 1024; found=1 } END { if (!found) exit 1 }' "$report"
      ;;
    *)
      return 1
      ;;
  esac
}

run_measured() {
  label=$1
  shift
  stdout_file="$tmp_dir/$label.stdout"
  stderr_file="$tmp_dir/$label.stderr"
  status=0
  # Keep /usr/bin/time directly in front of the measured tool so its RSS report
  # describes the tool rather than the timeout wrapper. GNU timeout is outside
  # that pair and kills the complete measurement process after the budget.
  "$timeout_bin" --kill-after=5s "$perf_timeout" "$time_bin" "$time_flag" "$@" >"$stdout_file" 2>"$stderr_file" || status=$?
  rss=$(rss_from_report "$stderr_file" 2>/dev/null || true)
  if [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; then
    state=timed_out
  elif [ "$status" -eq 0 ]; then
    state=ready
  else
    state=failed
  fi
	record_measurement "$label" "$state" "$rss"
	if [ "$status" -ne 0 ]; then
		sed -n '1,12p' "$stderr_file" >&2
		if [ "$json_output" = true ]; then
			printf '1\n' >"$measurement_failure_file"
			return 0
		fi
		return "$status"
  fi
}

# Setup commands are part of the external-tool lifecycle too. Keep them under
# the same timeout and process reaping policy as the measured operation; a
# broken init must not strand the verification harness before it can emit a
# useful result.
run_setup() {
	label=$1
	shift
	stdout_file="$tmp_dir/$label.stdout"
	stderr_file="$tmp_dir/$label.stderr"
	status=0
	"$timeout_bin" --kill-after=5s "$perf_timeout" "$time_bin" "$time_flag" "$@" >"$stdout_file" 2>"$stderr_file" || status=$?
	rss=$(rss_from_report "$stderr_file" 2>/dev/null || true)
	if [ "$status" -eq 124 ] || [ "$status" -eq 137 ]; then
		state=timed_out
	elif [ "$status" -eq 0 ]; then
		state=ready
	else
		state=failed
	fi
	record_measurement "$label" "$state" "$rss"
	if [ "$status" -ne 0 ]; then
		sed -n '1,12p' "$stderr_file" >&2
		printf '%s\n' '1' >"$measurement_failure_file"
		return 1
	fi
	return 0
}

record_skipped() {
	label=$1
	reason=$2
	record_measurement "$label" skipped "" "$label state=skipped reason=$reason"
}

if [ "$json_output" = false ]; then
	printf 'Teak tool performance\n'
	printf 'workspace=%s\n' "$workspace"
	if [ "$stress_files" -gt 0 ]; then
		printf 'corpus=synthetic-go files=%s\n' "$stress_files"
	else
		printf 'corpus=existing workspace for codemap; testdata/glyphrun copy for vecgrep\n'
	fi
fi

if [ -n "$codemap_bin" ]; then
  codemap_data="$tmp_dir/codemap-data"
  codemap_config="$tmp_dir/codemap-config"
  codemap_cache="$tmp_dir/codemap-cache"
  export CODEMAP_DATA="$codemap_data"
  export CODEMAP_CONFIG_DIR="$codemap_config"
  export CODEMAP_CACHE="$codemap_cache"
  codemap_setup_ok=true
  if ! run_setup codemap-init "$codemap_bin" init --json -C "$workspace"; then
    codemap_setup_ok=false
  fi
  if [ "$codemap_setup_ok" = true ]; then
    run_measured codemap-index "$codemap_bin" index --json --no-embed --no-lsp --no-tips --reindex -C "$workspace"
    # Never fall back to legacy `status` here: older codemap versions load the
    # entire semantic vector store just to count vectors and can consume several
    # gigabytes. An explicit unsupported result is safer than a misleading or
    # destructive health measurement.
    if "$codemap_bin" structural-manifest --help >/dev/null 2>&1; then
      run_measured codemap-status "$codemap_bin" structural-manifest --json -C "$workspace"
    else
      record_measurement codemap-status unsupported "" 'codemap-status state=unsupported reason=structural-manifest-unavailable'
    fi
  else
    record_skipped codemap-index codemap-init-failed
    record_skipped codemap-status codemap-init-failed
  fi
else
	record_measurement codemap missing "" 'codemap state=missing'
fi

if [ -n "$vecgrep_bin" ] && { [ "$stress_files" -gt 0 ] || [ -d "$workspace/testdata/glyphrun" ]; }; then
  if [ "$stress_files" -gt 0 ]; then
    vecgrep_root="$workspace"
  else
    vecgrep_root="$tmp_dir/vecgrep-corpus"
    cp -R "$workspace/testdata/glyphrun" "$vecgrep_root"
  fi
  (
    cd "$vecgrep_root"
    export VECGREP_CODEMAP_ENABLED=false
    vecgrep_setup_ok=true
    if ! run_setup vecgrep-init "$vecgrep_bin" init --local; then
      vecgrep_setup_ok=false
    fi
    if [ "$vecgrep_setup_ok" = true ]; then
      run_measured vecgrep-index "$vecgrep_bin" index --yes --no-progress
    else
      record_skipped vecgrep-index vecgrep-init-failed
    fi
  )
elif [ -z "$vecgrep_bin" ]; then
	record_measurement vecgrep missing "" 'vecgrep state=missing'
else
	record_measurement vecgrep skipped "" 'vecgrep state=skipped reason=missing_testdata_glyphrun'
fi

if [ "$json_output" = true ]; then
	if ! command -v python3 >/dev/null 2>&1; then
		printf '%s\n' '--json requires python3' >&2
		exit 2
	fi
	python3 - "$workspace" "$stress_files" "$measurements_file" <<'PY'
import json
import os
import pathlib
import sys

workspace, stress_files, measurements_path = sys.argv[1:]
measurements = []
for raw in pathlib.Path(measurements_path).read_text(encoding="utf-8").splitlines():
    label, state, rss, budget_exceeded = raw.split("\t")
    item = {
        "label": label,
        "state": state,
        "rss_available": bool(rss),
        "budget_exceeded": budget_exceeded == "true",
    }
    if rss:
        item["rss_bytes"] = int(rss)
    if int(os.environ.get("TEAK_PERF_MAX_RSS_BYTES", "0")) > 0:
        item["rss_budget_bytes"] = int(os.environ["TEAK_PERF_MAX_RSS_BYTES"])
    measurements.append(item)

stress_count = int(stress_files)
print(json.dumps({
    "schema_version": 1,
    "workspace": workspace,
    "rss_budget_bytes": int(os.environ.get("TEAK_PERF_MAX_RSS_BYTES", "0")) or None,
    "corpus": {
        "kind": "synthetic-go" if stress_count else "workspace-and-glyphrun",
        "stress_files": stress_count,
    },
    "measurements": measurements,
}, sort_keys=True, separators=(",", ":")))
PY
fi

if [ -s "$measurement_failure_file" ]; then
	exit 1
fi
