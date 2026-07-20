#!/bin/sh

set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "error: update-agent-reach.sh must be run inside a Git working tree" >&2
  exit 1
}
cd "$repo_root"

submodule_name=third_party/Agent-Reach
submodule_path=$(git config -f .gitmodules --get "submodule.$submodule_name.path" 2>/dev/null) || {
  echo "error: .gitmodules does not declare $submodule_name" >&2
  exit 1
}
branch=$(git config -f .gitmodules --get "submodule.$submodule_name.branch" 2>/dev/null || true)
if [ -z "$branch" ]; then
  branch=main
fi

if ! git -C "$submodule_path" rev-parse --git-dir >/dev/null 2>&1; then
  git submodule update --init --depth 1 -- "$submodule_path"
fi

if [ -n "$(git -C "$submodule_path" status --porcelain)" ]; then
  echo "error: $submodule_path has local changes; commit or discard them before updating" >&2
  exit 1
fi

remote=origin
git -C "$submodule_path" fetch --prune "$remote" "$branch"
if git -C "$submodule_path" show-ref --verify --quiet "refs/heads/$branch"; then
  git -C "$submodule_path" checkout "$branch"
else
  git -C "$submodule_path" checkout -b "$branch" --track "$remote/$branch"
fi
git -C "$submodule_path" pull --ff-only "$remote" "$branch"

revision=$(git -C "$submodule_path" rev-parse HEAD)
printf 'Agent-Reach updated to %s (%s)\n' "$revision" "$branch"
printf 'The superproject now records this update as a change to %s.\n' "$submodule_path"
