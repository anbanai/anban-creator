#!/bin/sh

set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 <destination-branch> [superproject-commit]" >&2
  exit 2
fi

destination_branch=$1
superproject_revision=${2:-HEAD}

if ! git check-ref-format --branch "$destination_branch" >/dev/null 2>&1; then
  echo "error: invalid destination branch name: $destination_branch" >&2
  exit 2
fi

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "error: push-managed-submodules.sh must be run inside a Git working tree" >&2
  exit 1
}
cd "$repo_root"

superproject_commit=$(git rev-parse --verify "${superproject_revision}^{commit}" 2>/dev/null) || {
  echo "error: cannot resolve superproject commit: $superproject_revision" >&2
  exit 2
}
gitmodules_blob="$superproject_commit:.gitmodules"
if ! git cat-file -e "$gitmodules_blob" 2>/dev/null; then
  exit 0
fi

managed_entries=$(git config --blob "$gitmodules_blob" --get-regexp '^submodule\..*\.syncPush$' 2>/dev/null || true)
if [ -z "$managed_entries" ]; then
  exit 0
fi

list_managed_submodules() {
  printf '%s\n' "$managed_entries" |
  while IFS=' ' read -r key enabled; do
    case "$enabled" in
      true|yes|on|1) ;;
      *) continue ;;
    esac

    entry=${key#submodule.}
    name=${entry%.*}
    submodule_path=$(git config --blob "$gitmodules_blob" --get "submodule.$name.path" 2>/dev/null) || {
      printf "error: managed submodule '%s' has no path in the pushed .gitmodules\n" "$name" >&2
      exit 1
    }
    submodule_remote=$(git config --blob "$gitmodules_blob" --get "submodule.$name.syncRemote" 2>/dev/null || true)
    if [ -z "$submodule_remote" ]; then
      submodule_remote=origin
    fi

    printf '%s\t%s\t%s\n' "$name" "$submodule_path" "$submodule_remote"
  done
}

tab=$(printf '\t')
plan_file=$(mktemp "${TMPDIR:-/tmp}/anban-managed-submodules.XXXXXX") || {
  echo "error: cannot create temporary managed-submodule plan" >&2
  exit 1
}
trap 'rm -f "$plan_file"' 0 HUP INT TERM

if ! list_managed_submodules > "$plan_file"; then
  exit 1
fi

# Validate every managed submodule before pushing any of them so a local error
# cannot leave only part of the managed set updated remotely.
while IFS="$tab" read -r name submodule_path submodule_remote; do
  if ! git -C "$submodule_path" rev-parse --git-dir >/dev/null 2>&1; then
    printf "Skipping uninitialized managed submodule '%s' (%s)\n" "$name" "$submodule_path"
    continue
  fi

  if [ -n "$(git -C "$submodule_path" status --porcelain)" ]; then
    printf "error: managed submodule '%s' (%s) has uncommitted changes; commit or discard them before pushing\n" "$name" "$submodule_path" >&2
    exit 1
  fi

  if ! git -C "$submodule_path" remote get-url "$submodule_remote" >/dev/null 2>&1; then
    printf "error: managed submodule '%s' (%s) has no configured remote named '%s'; update submodule.%s.syncRemote in .gitmodules or add the remote\n" \
      "$name" "$submodule_path" "$submodule_remote" "$name" >&2
    exit 1
  fi

  submodule_head=$(git -C "$submodule_path" rev-parse HEAD)
  recorded_head=$(git rev-parse --verify "$superproject_commit:$submodule_path" 2>/dev/null || true)
  if [ -z "$recorded_head" ] || [ "$submodule_head" != "$recorded_head" ]; then
    printf "error: managed submodule '%s' HEAD %s does not match the superproject gitlink in the pushed commit %s (%s); commit the updated gitlink before pushing\n" \
      "$name" "$submodule_head" "$superproject_commit" "${recorded_head:-<missing>}" >&2
    exit 1
  fi
done < "$plan_file"

while IFS="$tab" read -r name submodule_path submodule_remote; do
  if ! git -C "$submodule_path" rev-parse --git-dir >/dev/null 2>&1; then
    continue
  fi

  printf "Pushing managed submodule '%s' (%s) to %s/%s\n" "$name" "$submodule_path" "$submodule_remote" "$destination_branch"
  git -C "$submodule_path" push "$submodule_remote" "HEAD:refs/heads/$destination_branch"
done < "$plan_file"
