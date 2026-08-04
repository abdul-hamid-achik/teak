#!/bin/sh
set -eu

if [ "$#" -lt 3 ] || [ "$#" -gt 5 ]; then
	echo "usage: run-teak.sh <home-id> <config> <file> [assert-first-line <expected>]" >&2
	exit 64
fi

home_id=$1
config_path=$2
fixture_path=$3
assert_mode=${4:-}
expected_first_line=${5:-}

if [ -n "$assert_mode" ] && [ "$assert_mode" != "assert-first-line" ]; then
	echo "unknown assertion mode: $assert_mode" >&2
	exit 64
fi
if [ "$assert_mode" = "assert-first-line" ] && [ "$#" -ne 5 ]; then
	echo "assert-first-line requires an expected value" >&2
	exit 64
fi

case "$home_id" in
	"" | *[!A-Za-z0-9_-]*)
		echo "invalid Glyphrun home id: $home_id" >&2
		exit 64
		;;
esac

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
# Include the launcher PID so repeated or parallel Glyphrun executions never
# share HOME, session state, or plugin/config caches. The value remains derived
# only from the validated fixture id and this process's private temp directory.
home_dir="${TMPDIR:-/tmp}/teak-glyphrun-$home_id-$$"

case "$config_path" in
	/*) config_source=$config_path ;;
	*) config_source="$repo_root/$config_path" ;;
esac

case "$fixture_path" in
	/*) fixture_source=$fixture_path ;;
	*) fixture_source="$repo_root/$fixture_path" ;;
esac

if [ ! -f "$config_source" ]; then
	echo "missing Glyphrun config fixture: $config_source" >&2
	exit 66
fi

if [ ! -f "$fixture_source" ]; then
	echo "missing Glyphrun workspace fixture: $fixture_source" >&2
	exit 66
fi

rm -rf -- "$home_dir"
mkdir -p \
	"$home_dir/.config/teak" \
	"$home_dir/Library/Application Support/teak" \
	"$home_dir/.local/state/teak" \
	"$home_dir/workspace"
cp "$config_source" "$home_dir/.config/teak/config.toml"
cp "$config_source" "$home_dir/Library/Application Support/teak/config.toml"
cp -R "$(dirname -- "$fixture_source")/." "$home_dir/workspace/"

export HOME="$home_dir"
export XDG_CONFIG_HOME="$home_dir/.config"
export XDG_STATE_HOME="$home_dir/.local/state"
export COLORTERM=truecolor
export LANG=C
export LC_ALL=C
export TZ=UTC
unset NO_COLOR

cd "$repo_root"
if [ "$assert_mode" = "assert-first-line" ]; then
	"$repo_root/bin/teak" "$home_dir/workspace/$(basename -- "$fixture_source")"
	actual_first_line=$(sed -n '1p' "$home_dir/workspace/$(basename -- "$fixture_source")")
	if [ "$actual_first_line" != "$expected_first_line" ]; then
		echo "first-line assertion failed: got <$actual_first_line>, want <$expected_first_line>" >&2
		exit 1
	fi
	# Keep the persistence marker visible after the alternate screen closes so
	# outcomes can verify both the save report and the on-disk assertion.
	printf '%s\n' 'SAVE_ASSERT_OK Saved'
	exit 0
fi
exec "$repo_root/bin/teak" "$home_dir/workspace/$(basename -- "$fixture_source")"
