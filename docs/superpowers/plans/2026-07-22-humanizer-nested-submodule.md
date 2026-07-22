# Humanizer Nested Submodule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the duplicated Humanizer source with the official repository nested at `plugins/skills/humanizer`, while preserving offline, pinned Agent image distribution.

**Architecture:** Creator Skills owns the nested Humanizer gitlink and its manual update command. Anban Writer removes the old root-level Humanizer submodule, keeps a thin update-command wrapper, and validates the nested checkout through its existing Agent contract suite. Docker continues to install only the local Anban plugin.

**Tech Stack:** Git submodules, POSIX shell, Make, Go contract tests, Claude Code and Codex plugin manifests

---

### Task 1: Define the Nested Ownership Contract

**Files:**
- Modify: `server/agent/humanizer_contract_test.go`
- Modify: `server/agent/skill_best_practices_test.go`
- Modify: `.dockerignore`

- [ ] **Step 1: Change the Humanizer contract test to require the new boundary**

Replace the mirror-normalization assertions with checks that:

```go
rootModules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
if strings.Contains(rootModules, `submodule "third_party/Humanizer"`) {
	t.Fatal("Anban Writer must not retain the old root Humanizer submodule")
}

pluginModules := readRepoFile(t, filepath.Join(root, "plugins", ".gitmodules"))
for _, want := range []string{
	`[submodule "skills/humanizer"]`,
	"path = skills/humanizer",
	"url = https://github.com/blader/humanizer.git",
	"branch = main",
} {
	if !strings.Contains(pluginModules, want) {
		t.Fatalf("Creator Skills .gitmodules missing %q", want)
	}
}
```

Read `plugins/skills/humanizer/SKILL.md` directly and require the official
frontmatter fields `name: humanizer`, `version: 2.8.2`, `license: MIT`, and
`compatibility: any-agent`.

- [ ] **Step 2: Require Creator Skills to own the updater**

Update the command contract to require:

```go
script := readRepoFile(t, filepath.Join(root, "plugins", "scripts", "update-humanizer.sh"))
for _, want := range []string{
	"submodule_name=skills/humanizer",
	"git -C \"$submodule_path\" fetch --prune origin \"$branch\"",
	"git -C \"$submodule_path\" checkout --detach \"origin/$branch\"",
	"git diff --submodule=log",
} {
	if !strings.Contains(script, want) {
		t.Fatalf("Creator Skills updater missing %q", want)
	}
}
```

Require the root `scripts/update-humanizer.sh` to delegate to the Creator Skills
script and require `plugins/Makefile` to expose `humanizer-update`.

- [ ] **Step 3: Treat the complete upstream subtree as an external boundary**

Change the Humanizer path helper to recognize every path below
`plugins/skills/humanizer`, and use it in the auxiliary README test:

```go
func isUpstreamHumanizerPath(path string) bool {
	clean := filepath.ToSlash(path)
	return strings.Contains(clean, "/plugins/skills/humanizer/")
}
```

Continue skipping Anban-authored Skill lint only for that exact subtree.

- [ ] **Step 4: Exclude nested Git administrative files from Docker context**

Add these rules to `.dockerignore`:

```dockerignore
**/.git
**/.git/
```

- [ ] **Step 5: Run the focused tests and confirm they fail for the old layout**

Run:

```bash
go test ./server/agent -run 'TestHumanizer|TestDistributedSkillsDoNotShipAuxiliaryReadmes' -count=1
```

Expected: FAIL because Creator Skills does not yet declare the nested Humanizer
submodule and still contains the normalized ordinary file.

### Task 2: Convert Humanizer Inside Creator Skills

**Files:**
- Create: `plugins/.gitmodules`
- Create: `plugins/Makefile`
- Create: `plugins/scripts/update-humanizer.sh`
- Replace: `plugins/skills/humanizer/` ordinary directory with a Git submodule
- Modify: `plugins/README.md`
- Modify: `plugins/docs/plugin-development.md`
- Modify: `plugins/CHANGELOG.md`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.claude-plugin/marketplace.json`
- Modify: `plugins/.codex-plugin/plugin.json`

- [ ] **Step 1: Replace the ordinary Skill with the official nested submodule**

Inside the Creator Skills repository, remove only the tracked mirror and add the
official repository at the same path:

```bash
git -C plugins rm -r skills/humanizer
git -C plugins submodule add -b main https://github.com/blader/humanizer.git skills/humanizer
git -C plugins/skills/humanizer checkout --detach 1b48564898e999219882660237fde01bf4843a0f
git -C plugins add .gitmodules skills/humanizer
```

Expected: `git -C plugins ls-files -s skills/humanizer` reports mode `160000`
at the reviewed upstream SHA.

- [ ] **Step 2: Add the manual update script**

Create `plugins/scripts/update-humanizer.sh` as a strict POSIX shell command
that resolves the Creator Skills root, initializes the nested checkout if
needed, rejects a dirty Humanizer worktree, fetches `origin/main`, checks out
the fetched commit detached, verifies `SKILL.md`, and prints old/new SHAs plus:

```sh
git -C "$repo_root" diff --submodule=log -- "$submodule_path"
printf '%s\n' 'Review the upstream diff, run plugin validation, and bump both native manifest versions before release.'
```

Create `plugins/Makefile` with:

```make
.PHONY: humanizer-update

humanizer-update:
	@scripts/update-humanizer.sh
```

- [ ] **Step 3: Update provenance and development documentation**

Document `plugins/skills/humanizer` as the official nested submodule, replace
the old root `third_party/Humanizer` and mirror-sync instructions, and state the
manual update flow:

```bash
make humanizer-update
git add .gitmodules skills/humanizer
```

Retain the requirements to review the upstream diff, bump both native manifests,
update the changelog, and run parent Agent contract tests.

- [ ] **Step 4: Publish a patch release of Creator Skills**

Bump the Claude manifest, Claude marketplace entry, and Codex manifest from
`2.12.0` to `2.12.1`. Add a changelog entry describing the nested official
Humanizer source and offline distribution contract.

- [ ] **Step 5: Verify and commit the child repository**

Run:

```bash
git -C plugins diff --check
git -C plugins submodule status skills/humanizer
git -C plugins ls-files -s skills/humanizer
git -C plugins add .gitmodules Makefile scripts/update-humanizer.sh README.md docs/plugin-development.md CHANGELOG.md .claude-plugin/plugin.json .claude-plugin/marketplace.json .codex-plugin/plugin.json skills/humanizer
git -C plugins diff --cached --check
git -C plugins commit -m "build: manage Humanizer as nested submodule"
git -C plugins push origin HEAD:main
```

Expected: the child push succeeds before Anban Writer records its new gitlink.

### Task 3: Remove the Old Parent Source and Delegate Updates

**Files:**
- Modify: `.gitmodules`
- Delete: `third_party/Humanizer` gitlink
- Modify: `scripts/update-humanizer.sh`
- Modify: `plugins` gitlink

- [ ] **Step 1: Remove the old root submodule declaration and gitlink**

Remove only the `third_party/Humanizer` section from `.gitmodules`, then run:

```bash
git rm -f third_party/Humanizer
git add .gitmodules
```

Do not run `git submodule deinit`; linked worktrees share submodule metadata.

- [ ] **Step 2: Replace the root updater with a thin delegation wrapper**

Use:

```sh
#!/bin/sh

set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || {
  echo "error: update-humanizer.sh must be run inside a Git working tree" >&2
  exit 1
}

exec "$repo_root/plugins/scripts/update-humanizer.sh"
```

This preserves `make humanizer-update` without retaining update ownership in
Anban Writer.

- [ ] **Step 3: Stage and audit only the parent boundary change**

Run:

```bash
git add .dockerignore .gitmodules scripts/update-humanizer.sh server/agent/humanizer_contract_test.go server/agent/skill_best_practices_test.go plugins
git diff --cached --check
git diff --cached --name-status
```

Expected: no Montage runtime files or other pre-existing working-tree changes
are staged.

### Task 4: Verify Runtime and Plugin Contracts

**Files:**
- Test: `server/agent/humanizer_contract_test.go`
- Test: `server/agent/skill_best_practices_test.go`
- Test: `server/agent/runtime_policy_test.go`
- Test: `server/agent/docker_runtime_contract_test.go`

- [ ] **Step 1: Run focused Humanizer and runtime tests**

Run:

```bash
go test ./server/agent -run 'TestHumanizer|TestDistributedSkillsDoNotShipAuxiliaryReadmes|TestValidateManagedPluginInitRequiresTaskSkills' -count=1
```

Expected: PASS.

- [ ] **Step 2: Validate both native plugin surfaces**

Run:

```bash
claude plugin validate --strict plugins
codex_plugin_test_root=$(mktemp -d "${TMPDIR:-/tmp}/anban-codex-plugin.XXXXXX")
CODEX_HOME="$codex_plugin_test_root" codex plugin marketplace add ./plugins --json
CODEX_HOME="$codex_plugin_test_root" codex plugin add anban@anbanai --json
test -f "$codex_plugin_test_root/plugins/cache/anbanai/anban/2.12.1/skills/humanizer/SKILL.md"
```

Expected: strict Claude validation and isolated Codex marketplace installation
exit 0, and Codex installs the nested Skill through the Anban namespace.

- [ ] **Step 3: Run full Go tests and builds**

Run:

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all commands exit 0.

- [ ] **Step 4: Verify image inputs and build when Docker is available**

Run:

```bash
git submodule status --recursive
docker build -f deploy/docker/Dockerfile.agent-article -t creator-agent-article:humanizer-review .
```

Expected: Humanizer is initialized at the pinned nested SHA and the image build
completes without `npx skills add` or a standalone Humanizer plugin install. If
Docker is unavailable, record that exact verification gap rather than claiming
an image build passed.

### Task 5: Commit, Review, and Merge to Main

**Files:**
- Commit: the audited Anban Writer parent changes only

- [ ] **Step 1: Commit the parent change**

Run:

```bash
git commit -m "build: nest official Humanizer skill"
```

- [ ] **Step 2: Perform independent code review**

Review the Creator Skills child commit and the Anban Writer parent commit
against both approved design specs. Fix all Critical and Important findings,
rerun affected verification, and push any amended child commit before updating
the parent gitlink.

- [ ] **Step 3: Merge the reviewed branch into local main**

Verify the merge base and use a normal non-fast-forward merge from the isolated
implementation branch. Preserve all unrelated dirty work in the original main
workspace. Do not push Anban Writer `main` unless separately requested.

- [ ] **Step 4: Prove final mainline state**

Run:

```bash
git branch --show-current
git log -1 --oneline
git submodule status --recursive
git status --short --branch
```

Expected: local branch is `main`, the reviewed parent commit is an ancestor of
`HEAD`, nested Humanizer points to the reviewed official SHA, and unrelated
Montage work remains unstaged.
