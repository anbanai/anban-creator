#!/bin/sh

set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "error: update-humanizer.sh must be run inside a Git working tree" >&2
  exit 1
}

plugin_script=$repo_root/harness/scripts/update-humanizer.sh
if [ ! -x "$plugin_script" ]; then
  git -C "$repo_root" submodule update --init --recursive -- harness
fi

exec "$plugin_script"
