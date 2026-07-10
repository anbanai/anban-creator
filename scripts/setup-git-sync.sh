#!/bin/sh

set -eu

if ! git rev-parse --show-toplevel >/dev/null 2>&1; then
  echo "error: setup-git-sync.sh must be run inside a Git working tree" >&2
  exit 1
fi

git config --local core.hooksPath .githooks
git config --local submodule.recurse true
git config --local fetch.recurseSubmodules on-demand
git config --local push.recurseSubmodules no
git config --local status.submoduleSummary true
git config --local diff.submodule log

printf '%s\n' "Git sync configured: recursive pull enabled, native recursive push disabled, hooks path set to .githooks"
