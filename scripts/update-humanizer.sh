#!/bin/sh

set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "error: update-humanizer.sh must be run inside a Git working tree" >&2
  exit 1
}
cd "$repo_root"

submodule_name=third_party/Humanizer
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

git -C "$submodule_path" fetch --prune origin "$branch"
git -C "$submodule_path" checkout --detach "origin/$branch"

source_skill=$submodule_path/SKILL.md
test -f "$source_skill" || {
  echo "error: upstream Humanizer does not contain SKILL.md" >&2
  exit 1
}

for distro in claudecode codex; do
  skill_dir=$distro/skills/humanizer
  destination=$skill_dir/SKILL.md
  test -d "$skill_dir" || {
    echo "error: missing bundled Humanizer directory for $distro" >&2
    exit 1
  }
  rm -rf "$skill_dir/references"
  cp "$source_skill" "$destination"
done

revision=$(git -C "$submodule_path" rev-parse HEAD)
version=$(sed -n 's/^version:[[:space:]]*//p' "$source_skill" | head -n 1)
printf 'Humanizer %s synchronized from %s\n' "$version" "$revision"
printf 'Review the upstream diff and bump all affected plugin manifest versions before release.\n'
