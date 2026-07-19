#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
	echo "usage: run-teak.sh <home-id> <config> <file>" >&2
	exit 64
fi

home_id=$1
config_path=$2
fixture_path=$3

case "$home_id" in
	"" | *[!A-Za-z0-9_-]*)
		echo "invalid Glyphrun home id: $home_id" >&2
		exit 64
		;;
esac

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
home_dir="${TMPDIR:-/tmp}/teak-glyphrun-$home_id"

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
exec "$repo_root/bin/teak" "$home_dir/workspace/$(basename -- "$fixture_source")"
