#!/bin/sh

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 <branch> [remote]" >&2
  exit 2
fi

branch_name=$1
remote_name=${2:-origin}

if ! git check-ref-format --branch "$branch_name" >/dev/null 2>&1; then
  echo "error: invalid superproject branch name: $branch_name" >&2
  exit 2
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "error: push-managed-submodules.sh must be run inside a Git working tree" >&2
  exit 1
}
cd "$repo_root"

managed_entries=$(git config -f .gitmodules --get-regexp '^submodule\..*\.syncPush$' 2>/dev/null || true)
if [ -z "$managed_entries" ]; then
  exit 0
fi

printf '%s\n' "$managed_entries" |
while IFS=' ' read -r key enabled; do
  case "$enabled" in
    true|yes|on|1) ;;
    *) continue ;;
  esac

  entry=${key#submodule.}
  name=${entry%.*}
  submodule_path=$(git config -f .gitmodules --get "submodule.$name.path")

  if ! git -C "$submodule_path" rev-parse --git-dir >/dev/null 2>&1; then
    printf "Skipping uninitialized managed submodule '%s' (%s)\n" "$name" "$submodule_path"
    continue
  fi

  printf "Pushing managed submodule '%s' (%s) to %s/%s\n" "$name" "$submodule_path" "$remote_name" "$branch_name"
  git -C "$submodule_path" push "$remote_name" "HEAD:refs/heads/$branch_name"
done
