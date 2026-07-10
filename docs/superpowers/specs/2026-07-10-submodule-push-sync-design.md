# Submodule Pull/Push Sync Design

## Problem

The repository currently enables `push.recurseSubmodules=on-demand` in local Git configuration. The three owned plugin submodules (`claudecode`, `codex`, and `openclaw`) are commonly checked out at detached HEADs because the superproject pins exact commits. Native recursive push runs a default push inside each submodule, reports `Everything up-to-date` because detached HEAD is not a branch, and then aborts because the pinned commits are not reachable from any remote ref.

The desired command surface is intentionally simple:

- `git pull` updates the superproject and initialized submodules.
- `git push` pushes the owned plugin submodule commits first and the superproject second.
- Third-party submodules are never pushed.

## Decision

Disable Git's native recursive push and install a tracked `pre-push` hook. The hook invokes a tested script that explicitly pushes each owned submodule's current commit to the same branch being pushed by the superproject. Explicit `HEAD:refs/heads/<branch>` refspecs work for both attached and detached submodule HEADs.

Use `.gitmodules` metadata to opt owned submodules into coordinated pushes. This keeps `third_party/OpenMontage` and future third-party dependencies outside the push set without hard-coding repository names in shell logic.

## Pull Behavior

Keep Git's standard submodule pull behavior:

- `submodule.recurse=true`
- `fetch.recurseSubmodules=on-demand`

This preserves the core submodule invariant: the superproject decides the exact submodule commit. Pulling submodule branches independently would allow the working tree to drift away from the superproject gitlinks, so no custom post-pull hook will do branch pulls.

## Push Behavior

A tracked `.githooks/pre-push` hook will:

1. Ignore recursive invocations from submodule pushes.
2. Identify the checked-out superproject branch.
3. Invoke `scripts/push-managed-submodules.sh` before Git sends the superproject refs.

The push script will:

1. Read only submodules with `syncPush = true` in `.gitmodules`.
2. Skip an opted-in submodule when it is not initialized.
3. Reject dirty submodule working trees because uncommitted work cannot be pushed.
4. Reject a submodule whose current HEAD is not the gitlink recorded by the superproject index, preventing the parent push from publishing a stale pointer.
5. Push the exact submodule commit using `git push <remote> HEAD:refs/heads/<superproject-branch>`.
6. Stop immediately on the first real push failure.

The hook intentionally does not infer a detached HEAD's former branch. Mapping all coordinated repositories to the branch selected by the superproject is deterministic and avoids pushing to an arbitrary nearby branch.

## Installation

A tracked `scripts/setup-git-sync.sh` installer will configure the current clone:

- `core.hooksPath=.githooks`
- `submodule.recurse=true`
- `fetch.recurseSubmodules=on-demand`
- `push.recurseSubmodules=no`
- existing submodule status and diff summaries

Setting recursive push explicitly to `no`, rather than merely unsetting it, prevents a global `on-demand` setting from re-enabling the broken behavior.

## Testing

Go integration tests will create temporary bare remotes and repositories to verify real Git behavior:

- the installer disables native recursive push and installs the tracked hook path;
- detached submodule HEADs are pushed to the superproject branch;
- unmarked third-party submodules are not pushed;
- dirty or stale-gitlink submodules stop the root push with actionable errors;
- the tracked hook delegates to the push script and includes a recursion guard.

Tests use local file remotes only and never touch the developer's configured remotes.
