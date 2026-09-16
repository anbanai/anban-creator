# Creator Miniapp Repository Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move `miniapp/` into the private `anbanai/creator-miniapp` repository with its relevant Git history and remove monorepo-only coupling from `anban-creator`.

**Architecture:** Generate a path-filtered branch whose root is the current `miniapp/` tree, validate it in a sibling checkout, and publish it as the private repository's `main` branch. Only after that push succeeds, delete the original tree and replace main-repository source coupling with documentation links and repository-local tests.

**Tech Stack:** Git subtree, GitHub CLI, Node.js/npm, Vue 3, uni-app, Go

**Spec:** `docs/superpowers/specs/2026-09-16-creator-miniapp-repository-split-design.md`

## Global Constraints

- The destination is the private repository `anbanai/creator-miniapp`.
- Preserve commits that changed `miniapp/`; rewritten hashes are expected.
- Never commit ignored `node_modules/` or `dist/` content.
- The new repository must install, test, type-check, and build standalone.
- Do not alter Server API behavior or WeChat Mini Program authentication.
- Leave the unrelated untracked `codex/` directory untouched.

---

### Task 1: Build And Verify The Filtered Repository

**Files:**
- Source: `miniapp/**`
- Create: `../creator-miniapp/README.md`
- Create: `../creator-miniapp/.github/workflows/ci.yml`

**Interfaces:**
- Consumes: tracked `miniapp/` files and their reachable history
- Produces: standalone local repository `../creator-miniapp` on `main`

- [ ] **Step 1: Confirm the destinations are unused**

Run `git status --short --branch`, `test ! -e ../creator-miniapp`, and `gh repo view anbanai/creator-miniapp --json nameWithOwner,visibility`.

Expected: only known user changes exist, the local path is absent, and GitHub reports no such repository.

- [ ] **Step 2: Generate and inspect filtered history**

```bash
git subtree split --prefix=miniapp -b codex/creator-miniapp-history
git log --oneline codex/creator-miniapp-history --max-count=20
git ls-tree --name-only codex/creator-miniapp-history
```

Expected: historical Miniapp commits exist; `package.json`, `src/`, and `scripts/` are at the root.

- [ ] **Step 3: Create the sibling checkout**

```bash
git clone --single-branch --branch codex/creator-miniapp-history . ../creator-miniapp
git -C ../creator-miniapp branch -m main
git -C ../creator-miniapp remote remove origin
```

Expected: the sibling checkout is clean on `main`.

- [ ] **Step 4: Add `README.md`**

Document that this is the private Vue 3/uni-app client and include the exact standalone commands `npm ci`, `npm test`, `npm run type-check`, `npm run dev:mp-weixin`, and `npm run build:mp-weixin`.

- [ ] **Step 5: Add `.github/workflows/ci.yml`**

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:
permissions:
  contents: read
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: npm
      - run: npm ci
      - run: npm test
      - run: npm run type-check
      - run: npm run build:mp-weixin
```

- [ ] **Step 6: Verify ignored outputs and the standalone build**

```bash
git -C ../creator-miniapp ls-files | rg '(^|/)(node_modules|dist)/'
cd ../creator-miniapp
npm ci
npm test
npm run type-check
npm run build:mp-weixin
```

Expected: the tracked-output search has no matches; all npm commands pass and produce `dist/build/mp-weixin/`.

- [ ] **Step 7: Commit repository-owned files**

```bash
git -C ../creator-miniapp add README.md .github/workflows/ci.yml
git -C ../creator-miniapp commit -m "chore: establish standalone miniapp repository"
```

### Task 2: Publish The Private Repository

**Files:** None

**Interfaces:**
- Consumes: verified local `../creator-miniapp/main`
- Produces: private `anbanai/creator-miniapp` with tracked `origin/main`

- [ ] **Step 1: Create and configure the private remote**

```bash
gh repo create anbanai/creator-miniapp --private --description "Anban Creator WeChat Mini Program client"
git -C ../creator-miniapp remote add origin https://github.com/anbanai/creator-miniapp.git
git -C ../creator-miniapp push -u origin main
```

- [ ] **Step 2: Verify remote state and preserved history**

```bash
gh repo view anbanai/creator-miniapp --json nameWithOwner,visibility,defaultBranchRef,url
git -C ../creator-miniapp status --short --branch
git -C ../creator-miniapp log --oneline --all -- src scripts package.json | head -20
```

Expected: visibility is `PRIVATE`, default branch is `main`, the checkout is clean, and multiple historical commits are present.

### Task 3: Remove Miniapp Ownership From The Main Repository

**Files:**
- Delete: `miniapp/**`
- Modify: `server/agent/connection_guide_contract_test.go`
- Modify: `.dockerignore`
- Modify: `README.md`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: published private Miniapp repository
- Produces: no tracked `miniapp/` source or tests that open removed files

- [ ] **Step 1: Establish the Server test baseline**

Run `go test ./server/agent -run 'TestConnectionGuide' -count=1`.

Expected: PASS.

- [ ] **Step 2: Remove the tree and prove the stale dependency**

```bash
git rm -r miniapp
go test ./server/agent -run 'TestConnectionGuide' -count=1
```

Expected: FAIL because the test still opens Miniapp source files.

- [ ] **Step 3: Remove only the cross-repository cases**

Delete these two entries from `TestConnectionGuidesUseFixedPluginEndpoint` in `server/agent/connection_guide_contract_test.go`:

```go
{relPath: "miniapp/src/pages/connect/claude-code.vue", mode: claudePluginUserConfig},
{relPath: "miniapp/src/pages/connect/codex.vue", mode: codexEnvironment},
```

Keep the Studio cases, scanner, and table-driven unit tests. The migrated `scripts/parity-test.mjs` already checks both Miniapp connection pages.

- [ ] **Step 4: Update ownership documentation**

- `.dockerignore`: remove `/miniapp/` and change its nearby comment to "Frontend and desktop sources".
- `README.md`: replace the `miniapp/` layout row with a private companion-repository link; clarify deployment wording.
- `CLAUDE.md`: point the Miniapp client surface to `https://github.com/anbanai/creator-miniapp` and remove Miniapp from the monorepo language summary.

- [ ] **Step 5: Run focused and full verification**

```bash
gofmt -w server/agent/connection_guide_contract_test.go
go test ./server/agent -run 'TestConnectionGuide' -count=1
go test ./...
go build -o /tmp/anban-creator-server ./server
```

Expected: all commands pass.

- [ ] **Step 6: Audit paths and commit**

```bash
rg -n --hidden -S 'miniapp/' --glob '!docs/superpowers/**' --glob '!.git/**' .
git ls-files miniapp
git status --short
git add .dockerignore README.md CLAUDE.md server/agent/connection_guide_contract_test.go
git commit -m "chore: move miniapp to standalone repository"
```

Expected: no tracked Miniapp files remain, remaining terminology is intentional, and `codex/` stays untracked.

### Task 4: Final Cross-Repository Verification

**Files:** None

**Interfaces:**
- Consumes: committed main repository and published split repository
- Produces: final completion evidence

- [ ] **Step 1: Confirm source differences are only standalone metadata**

Run `git diff --stat codex/creator-miniapp-history ../creator-miniapp/main -- .`.

Expected: only `README.md` and `.github/workflows/ci.yml` differ from the split snapshot.

- [ ] **Step 2: Confirm both repositories and GitHub**

```bash
git status --short --branch
git -C ../creator-miniapp status --short --branch
gh repo view anbanai/creator-miniapp --json visibility,defaultBranchRef,url
```

Expected: the main repository has only pre-existing unrelated state, the Miniapp repository tracks a clean `origin/main`, and GitHub reports private visibility with `main` as default.
