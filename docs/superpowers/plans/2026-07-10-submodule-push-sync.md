# Submodule Pull/Push Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ordinary `git pull` update submodules and ordinary `git push` publish owned submodule commits before the superproject without using Git's broken detached-HEAD recursive push mode.

**Architecture:** A tracked setup script installs repository-local Git configuration, while a tracked `pre-push` hook delegates to a focused shell script. Owned submodules opt in through `.gitmodules`; integration tests exercise the scripts against temporary real Git repositories and bare remotes.

**Tech Stack:** Git hooks, POSIX shell, Go integration tests using `os/exec`.

---

## File Structure

Create:

- `.githooks/pre-push` - guarded hook entry point that delegates coordinated submodule pushes.
- `scripts/push-managed-submodules.sh` - validates and pushes opted-in submodule commits.
- `scripts/setup-git-sync.sh` - installs local hook and pull/push configuration.
- `server/git_sync_test.go` - real-repository integration coverage for setup and push behavior.

Modify:

- `.gitmodules` - mark the three owned plugin submodules with `syncPush = true` and `syncRemote = origin`.
- `Makefile` - expose an idempotent `git-sync-setup` target.

### Task 1: Installer contract

**Files:**
- Test: `server/git_sync_test.go`
- Create: `scripts/setup-git-sync.sh`
- Modify: `Makefile`

- [ ] **Step 1: Write the failing installer test**

Add a Go test that initializes a temporary repository, runs the repository's setup script from that temporary working directory, and asserts:

```go
want := map[string]string{
    "core.hooksPath":             ".githooks",
    "submodule.recurse":          "true",
    "fetch.recurseSubmodules":    "on-demand",
    "push.recurseSubmodules":     "no",
    "status.submoduleSummary":    "true",
    "diff.submodule":             "log",
}
```

- [ ] **Step 2: Verify the installer test fails**

Run:

```bash
go test ./server -run TestSetupGitSync -count=1
```

Expected: FAIL because `scripts/setup-git-sync.sh` does not exist.

- [ ] **Step 3: Add the minimal installer**

Create an executable script whose configuration body is:

```sh
git config --local core.hooksPath .githooks
git config --local submodule.recurse true
git config --local fetch.recurseSubmodules on-demand
git config --local push.recurseSubmodules no
git config --local status.submoduleSummary true
git config --local diff.submodule log
```

Add a `git-sync-setup` Make target that runs `scripts/setup-git-sync.sh`.

- [ ] **Step 4: Verify the installer test passes**

Run:

```bash
go test ./server -run TestSetupGitSync -count=1
```

Expected: PASS.

### Task 2: Detached submodule push

**Files:**
- Test: `server/git_sync_test.go`
- Create: `scripts/push-managed-submodules.sh`
- Modify: `.gitmodules`

- [ ] **Step 1: Write the failing detached-HEAD integration test**

Create a temporary bare submodule remote, seed `main`, add it to a temporary superproject, create and record a detached submodule commit, set `submodule.plugin.syncPush=true`, and run:

```go
runGit(t, superproject, scriptPath, "main")
```

Assert `refs/heads/main` in the bare submodule remote equals the detached commit.

- [ ] **Step 2: Verify the push test fails**

Run:

```bash
go test ./server -run TestPushManagedSubmodulesPushesDetachedHead -count=1
```

Expected: FAIL because `scripts/push-managed-submodules.sh` does not exist.

- [ ] **Step 3: Implement the minimal push script**

The executable script accepts `<destination-branch> [superproject-commit]`, enumerates the pushed commit's `.gitmodules` entries where `syncPush` is true, validates clean/commit-aligned state, and pushes through each submodule's configured `syncRemote` (default `origin`):

```sh
git -C "$submodule_path" push "$submodule_remote" "HEAD:refs/heads/$destination_branch"
```

Mark `claudecode`, `codex`, and `openclaw` with `syncPush = true` and `syncRemote = origin`; do not mark `third_party/OpenMontage`.

- [ ] **Step 4: Verify the push test passes**

Run:

```bash
go test ./server -run TestPushManagedSubmodulesPushesDetachedHead -count=1
```

Expected: PASS.

### Task 3: Safety contracts

**Files:**
- Test: `server/git_sync_test.go`
- Modify: `scripts/push-managed-submodules.sh`

- [ ] **Step 1: Add failing tests for unmarked, dirty, and stale submodules**

Add tests that assert:

```text
unmarked submodule: remote branch remains unchanged
dirty marked submodule: script fails and reports "uncommitted changes"
stale gitlink: script fails and reports "does not match the superproject gitlink"
```

- [ ] **Step 2: Run tests and verify expected failures**

Run:

```bash
go test ./server -run 'TestPushManagedSubmodules(SkipsUnmarked|RejectsDirty|RejectsStaleGitlink)' -count=1
```

Expected: at least the dirty and stale cases FAIL until validation is implemented.

- [ ] **Step 3: Implement minimal safety validation**

Use `git status --porcelain`, `git rev-parse <pushed-commit>:<path>`, and `git -C <path> rev-parse HEAD`. Validate the full managed set before starting any push, emit actionable stderr messages, and exit nonzero before any unsafe push.

- [ ] **Step 4: Verify safety tests pass**

Run the same focused command and expect PASS.

### Task 4: Hook delegation

**Files:**
- Test: `server/git_sync_test.go`
- Create: `.githooks/pre-push`

- [ ] **Step 1: Write the failing hook contract test**

Assert the hook is executable, exits immediately when `ANBAN_SUBMODULE_PUSH_ACTIVE=1`, resolves the repository root, parses Git's standard-input ref list, ignores tags and non-current branches, and delegates using the remote destination branch plus the exact local commit.

- [ ] **Step 2: Verify the hook test fails**

Run:

```bash
go test ./server -run TestPrePushHookContract -count=1
```

Expected: FAIL because `.githooks/pre-push` does not exist.

- [ ] **Step 3: Implement the guarded hook**

The hook must export `ANBAN_SUBMODULE_PUSH_ACTIVE=1` before invoking the submodule push script. It rejects an explicit detached-`HEAD` branch push with a clear message, but leaves unrelated tag-only pushes alone.

- [ ] **Step 4: Verify the hook test passes**

Run the same focused command and expect PASS.

### Task 5: Install and verify

**Files:**
- Modify local Git config only through `scripts/setup-git-sync.sh`

- [ ] **Step 1: Run all Git sync tests**

```bash
go test ./server -run 'Test(SetupGitSync|PushManagedSubmodules|PrePushHook)' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run the full Go suite**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Run shell syntax checks**

```bash
sh -n scripts/setup-git-sync.sh scripts/push-managed-submodules.sh .githooks/pre-push
```

Expected: no output and exit code 0.

- [ ] **Step 4: Install the configuration in the primary checkout**

```bash
scripts/setup-git-sync.sh
```

Expected output states that native recursive push is disabled and `.githooks` is active.

- [ ] **Step 5: Verify effective configuration**

```bash
git config --get core.hooksPath
git config --get submodule.recurse
git config --get push.recurseSubmodules
```

Expected values: `.githooks`, `true`, and `no`.
