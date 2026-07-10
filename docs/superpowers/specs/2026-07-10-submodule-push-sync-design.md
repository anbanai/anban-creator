# Submodule Pull/Push Sync Design

## Problem

The repository currently enables `push.recurseSubmodules=on-demand` in local Git configuration. The three owned plugin submodules (`claudecode`, `codex`, and `openclaw`) are commonly checked out at detached HEADs because the superproject pins exact commits. Native recursive push runs a default push inside each submodule, reports `Everything up-to-date` because detached HEAD is not a branch, and then aborts because the pinned commits are not reachable from any remote ref.

The desired command surface is intentionally simple:

- `git pull` updates the superproject and initialized submodules.
- `git push` pushes the owned plugin submodule commits first and the superproject second.
- Third-party submodules are never pushed.

## Decision

Disable Git's native recursive push and install a tracked `pre-push` hook. The hook reads Git's standard ref update list and invokes a tested script only when the checked-out branch (or its explicit `HEAD` alias) is actually being pushed. The destination branch comes from the remote ref, so refspecs such as `main:release` push the managed submodules to `release`. Explicit `HEAD:refs/heads/<branch>` refspecs work for both attached and detached submodule HEADs.

Use `.gitmodules` metadata to opt owned submodules into coordinated pushes and select each submodule's own push remote. This keeps `third_party/OpenMontage` and future third-party dependencies outside the push set without hard-coding repository names in shell logic, and it avoids assuming that the superproject and every submodule use the same remote name.

## Pull Behavior

Keep Git's standard submodule pull behavior:

- `submodule.recurse=true`
- `fetch.recurseSubmodules=on-demand`

This preserves the core submodule invariant: the superproject decides the exact submodule commit. Pulling submodule branches independently would allow the working tree to drift away from the superproject gitlinks, so no custom post-pull hook will do branch pulls.

## Push Behavior

A tracked `.githooks/pre-push` hook will:

1. Ignore recursive invocations from submodule pushes.
2. Parse the `<local-ref> <local-oid> <remote-ref> <remote-oid>` records supplied on standard input by Git.
3. Ignore tag-only pushes and pushes of branches other than the checked-out branch.
4. Map the checked-out local branch or explicit `HEAD` to the destination branch named by `remote-ref`.
5. Invoke `scripts/push-managed-submodules.sh` with that destination branch and the exact superproject commit being pushed, before Git sends the superproject refs.

The push script will:

1. Read only submodules with `syncPush = true` from the `.gitmodules` blob in the exact superproject commit being pushed.
2. Skip an opted-in submodule when it is not initialized.
3. Reject dirty submodule working trees because uncommitted work cannot be pushed.
4. Reject a submodule whose current HEAD is not the gitlink recorded by the exact superproject commit being pushed, rather than trusting uncommitted index state.
5. Resolve the push remote from `syncRemote` for that submodule, defaulting to `origin` when the metadata is absent.
6. Validate every managed submodule before pushing any of them, preventing local validation failures from producing a partial multi-repository push.
7. Push the exact submodule commit using `git push <submodule-remote> HEAD:refs/heads/<destination-branch>`.
8. Stop immediately on the first real push failure.

The hook intentionally does not infer a detached superproject HEAD's former branch. An explicit detached `HEAD` push to a branch is rejected, while unrelated tag-only pushes remain unaffected. Mapping all coordinated repositories to the remote branch selected by the superproject refspec is deterministic and avoids pushing to an arbitrary nearby branch.

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
- refspec destinations are honored while tag-only and non-current-branch pushes do not trigger submodule pushes;
- each submodule uses its configured push remote rather than the superproject remote name;
- unmarked third-party submodules are not pushed;
- dirty or pushed-commit gitlink mismatches stop the root push with actionable errors before any managed remote is updated;
- the tracked hook delegates to the push script and includes a recursion guard.

Tests use local file remotes only and never touch the developer's configured remotes.
