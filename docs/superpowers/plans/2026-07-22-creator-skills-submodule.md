# Creator Skills Submodule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the current `plugins/` tree as a fresh `creator-skills` repository and replace it with a managed submodule at the same path.

**Architecture:** Build and push an independent snapshot before changing the parent repository. After the remote SHA is proven, preserve the original directory in a temporary backup, add the verified repository as a submodule, and commit only the submodule boundary change.

**Tech Stack:** Git, POSIX shell, Go contract tests

---

### Task 1: Publish the Fresh Creator Skills Repository

**Files:**
- Snapshot: `plugins/**`
- Remote: `https://github.com/anbanai/creator-skills.git`

- [ ] **Step 1: Reconfirm the destination is empty and capture current source status**

Run:

```bash
git ls-remote --heads https://github.com/anbanai/creator-skills.git
git status --short -- plugins
```

Expected: no remote heads; the second command lists every current plugin change that must appear in the snapshot.

- [ ] **Step 2: Create an isolated byte-for-byte working-tree snapshot**

Run:

```bash
snapshot_root=$(mktemp -d "${TMPDIR:-/tmp}/creator-skills-snapshot.XXXXXX")
mkdir "$snapshot_root/repo"
rsync -a --exclude='.git' plugins/ "$snapshot_root/repo/"
diff -ru --exclude='.git' plugins "$snapshot_root/repo"
```

Expected: `diff` exits 0 with no output.

- [ ] **Step 3: Create the first child-repository commit**

Run:

```bash
git -C "$snapshot_root/repo" init -b main
git -C "$snapshot_root/repo" add -A
git -C "$snapshot_root/repo" diff --cached --check
git -C "$snapshot_root/repo" commit -m "feat: publish Anban creator skills"
creator_sha=$(git -C "$snapshot_root/repo" rev-parse HEAD)
```

Expected: one root commit on `main`; `creator_sha` resolves to its commit SHA.

- [ ] **Step 4: Push and verify the child repository**

Run:

```bash
git -C "$snapshot_root/repo" remote add origin https://github.com/anbanai/creator-skills.git
git -C "$snapshot_root/repo" push -u origin main
remote_creator_sha=$(git ls-remote https://github.com/anbanai/creator-skills.git refs/heads/main | awk '{print $1}')
test "$creator_sha" = "$remote_creator_sha"
```

Expected: push succeeds and the local and remote SHAs are identical.

### Task 2: Convert the Parent Directory to a Managed Submodule

**Files:**
- Modify: `.gitmodules`
- Replace: `plugins/` ordinary tree with a Git submodule

- [ ] **Step 1: Preserve the original directory and remove only its parent index entries**

Run:

```bash
backup_root=$(mktemp -d "${TMPDIR:-/tmp}/creator-skills-backup.XXXXXX")
mv plugins "$backup_root/plugins"
git rm -r --cached plugins
```

Expected: the original files remain under the explicit backup path and only `plugins/**` removals are staged.

- [ ] **Step 2: Add the verified child repository at the original path**

Run:

```bash
git submodule add -b main --name plugins https://github.com/anbanai/creator-skills.git plugins
git -C plugins checkout "$creator_sha"
git config -f .gitmodules submodule.plugins.syncPush true
git config -f .gitmodules submodule.plugins.syncRemote origin
git submodule sync -- plugins
```

Expected: `plugins` is a gitlink pinned to `creator_sha`, with `main` configured as its update branch.

- [ ] **Step 3: Prove the cloned child matches the preserved source**

Run:

```bash
diff -ru --exclude='.git' "$backup_root/plugins" plugins
test "$(git -C plugins rev-parse HEAD)" = "$creator_sha"
test -z "$(git -C plugins status --porcelain)"
```

Expected: no content differences and a clean child working tree at the published SHA.

- [ ] **Step 4: Audit and commit only the submodule boundary**

Run:

```bash
git add .gitmodules plugins
git diff --cached --check
git diff --cached --name-status
git commit -m "build: manage creator skills as submodule"
```

Expected: the staged change contains `.gitmodules`, deletion of the old ordinary `plugins/**` entries, and one `plugins` gitlink; unrelated parent files are excluded.

### Task 3: Verify Repository and Runtime Contracts

**Files:**
- Test: `server/agent/unified_plugin_contract_test.go`
- Test: `server/agent/montage_contract_test.go`

- [ ] **Step 1: Verify Git and remote contracts**

Run:

```bash
git ls-tree HEAD plugins
git submodule status plugins
git config -f .gitmodules --get-regexp '^submodule\.plugins\.'
git ls-remote https://github.com/anbanai/creator-skills.git refs/heads/main
scripts/push-managed-submodules.sh main HEAD
```

Expected: tree mode `160000`, all reported SHAs equal `creator_sha`, and the managed-push check succeeds.

- [ ] **Step 2: Run targeted plugin contract tests**

Run:

```bash
go test ./server/agent -run 'TestUnifiedPluginLayout|TestMontagePluginContractsAreDistributed|TestMontageSkillMirrorsStayInSync|TestMontagePluginManifestsAdvertiseSupport' -count=1
```

Expected: package exits 0.

- [ ] **Step 3: Run the full Go suite**

Run:

```bash
go test ./...
```

Expected: all packages exit 0.

- [ ] **Step 4: Final workspace audit**

Run:

```bash
git status --short --branch
git diff --check
git -C plugins status --short --branch
```

Expected: the child is clean; pre-existing unrelated parent changes remain present and unstaged.
