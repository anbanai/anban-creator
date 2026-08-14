# Seednote Server MCP External Research Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make authenticated Anban Server MCP tools the canonical managed Seednote Xiaohongshu research path and remove Agent Reach, Python, and mcporter from Seednote runtimes.

**Architecture:** Managed Seednote Jobs call the existing atomic Seednote tools on the authenticated Anban MCP connection. Server `SeednoteCapabilityService` continues to call the independently deployed `sidecar-seednote` at `http://sidecar-seednote:18060`; the Agent Job never connects to the sidecar directly. Workflow sequencing and fallback stay in the Seednote Agent and `seednote-research` Skill.

**Tech Stack:** Go 1.24, Model Context Protocol Go SDK, React-independent plugin Markdown/TOML, Agent Pack generator, TypeScript/Bun Agent runtime, Docker, Kubernetes/ACK, Git submodules.

---

## Working Tree Constraint

`plugins/skills/humanizer` already has a user-owned gitlink change. Do not stage,
reset, checkout, or commit it. Every plugin commit command below names the
intended files explicitly. Before and after each plugin operation, run:

```bash
git -C plugins status --short
```

The expected unrelated line remains:

```text
 M skills/humanizer
```

## File Map

- `server/agent/runtime_policy.go`: managed Go runtime Skill and MCP tool readiness inventory.
- `agent-ts/src/runner.ts`: managed TypeScript runtime Skill readiness inventory.
- `server/mcp/seednote_tools.go`: existing canonical atomic Server MCP tools; behavior stays in place.
- `plugins/packs/seednote/agent-pack.yaml`: canonical Seednote Pack Skill membership.
- `plugins/packs/seednote/agent.claude.md`: canonical Claude Seednote workflow.
- `plugins/packs/seednote/agent.codex.toml`: canonical Codex Seednote workflow.
- `plugins/skills/seednote-research/SKILL.md`: research sequencing, fallback, provenance, and read-only rules.
- `plugins/skills/anban-setup/SKILL.md`: operator preflight and login recovery.
- `plugins/agents/seednote.md`, `plugins/agents/seednote.toml`: generated native Agent distributions.
- `server/agentpack/catalog.generated.json`: generated Server Pack catalog.
- `deploy/docker/Dockerfile.agent-seednote`, `deploy/docker/Dockerfile.agent-seednote-ts`: managed runtime contents.
- `.gitmodules`, `Makefile`, `scripts/update-agent-reach.sh`: obsolete Agent Reach source/update contract.
- `server/agent/*_test.go`, `agent-ts/test/runner.test.ts`, `server/git_sync_test.go`, `deploy/docker/runtime-smoke_test.go`: executable contract coverage.

### Task 1: Require The Canonical Server MCP And Remove Agent Reach From Runtime Readiness

**Files:**
- Modify: `server/agent/runtime_policy_test.go:113-144,257-294`
- Modify: `server/agent/runtime_policy.go:228-245,286-315`
- Modify: `agent-ts/test/runner.test.ts:38-44`
- Modify: `agent-ts/src/runner.ts:332-337`

- [ ] **Step 1: Write the failing Go readiness expectations**

Replace the Seednote MCP expectation in `runtime_policy_test.go` with:

```go
func TestManagedRequiredMCPToolsRequiresSeednoteResearchTools(t *testing.T) {
	got := managedRequiredMCPTools("seednote")
	want := []string{
		"analyze_image",
		"check_seednote_login_status",
		"finalize_task_title",
		"generate_image",
		"get_project_profile",
		"get_seednote_feed_detail",
		"get_seednote_login_qrcode",
		"get_seednote_user_profile",
		"list_project_titles",
		"search_seednote_feeds",
		"submit_agent_feedback",
		"update_task_progress",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managedRequiredMCPTools(seednote) = %#v, want %#v", got, want)
	}
}
```

In `TestValidateManagedPluginInitRequiresTaskSkills`, make the Seednote fixture:

```go
{
	taskType: "seednote",
	skills: []any{
		"anban:seednote-research",
		"anban:seednote-viral-analysis",
		"anban:seednote-writing",
		"anban:seednote-visual-design",
	},
	missing: "anban:seednote-research",
},
```

- [ ] **Step 2: Write the failing TypeScript Skill readiness test**

Add to `describe("validateManagedInit")`:

```ts
test("requires Seednote phase skills without Agent Reach", () => {
  const message = {
    type: "system" as const,
    subtype: "init" as const,
    mcp_servers: [{ name: "anban", status: "connected" as const }],
    plugins: [{ name: "anban", path: "/anbanai" }],
    skills: [
      "anban:seednote-research",
      "anban:seednote-viral-analysis",
      "anban:seednote-writing",
      "anban:seednote-visual-design",
    ],
  };
  expect(() => validateManagedInit(message, "seednote")).not.toThrow();
  message.skills = message.skills.slice(1);
  expect(() => validateManagedInit(message, "seednote")).toThrow("anban:seednote-research");
});
```

- [ ] **Step 3: Run the focused tests and confirm the old inventories fail**

Run:

```bash
go test ./server/agent -run 'TestManagedRequiredMCPToolsRequiresSeednoteResearchTools|TestValidateManagedPluginInitRequiresTaskSkills' -count=1
cd agent-ts && bun test test/runner.test.ts -t 'requires Seednote phase skills without Agent Reach'
```

Expected: Go fails because the five Seednote external-data tools are absent;
TypeScript fails because `anban:agent-reach` is still required.

- [ ] **Step 4: Implement the minimal Go inventory change**

Return only the four remaining Seednote Skills from
`managedRequiredPluginSkills`. Add the five existing Seednote tools to
`managedRequiredMCPTools` in lexical order within the full list:

```go
case "seednote":
	return []string{
		"anban:seednote-research",
		"anban:seednote-viral-analysis",
		"anban:seednote-writing",
		"anban:seednote-visual-design",
	}
```

Use the exact `want` list from Step 1 for `managedRequiredMCPTools("seednote")`.
Do not add routing or orchestration to `server/mcp/seednote_tools.go`.

- [ ] **Step 5: Implement the TypeScript inventory change**

Replace `requiredSkills` with:

```ts
function requiredSkills(taskType: string): string[] {
  if (taskType === "seednote") return ["anban:seednote-research", "anban:seednote-viral-analysis", "anban:seednote-writing", "anban:seednote-visual-design"];
  if (taskType === "article" || taskType === "ecommerce") return ["anban:humanizer"];
  if (taskType === "live-slicer") return ["anban:live-slice", "anban:capcut-draft"];
  return [];
}
```

- [ ] **Step 6: Run focused verification**

```bash
go test ./server/agent -run 'TestManagedRequiredMCPToolsRequiresSeednoteResearchTools|TestValidateManagedMCPStatusRequiresConnectedSeednoteTools|TestValidateManagedPluginInitRequiresTaskSkills' -count=1
cd agent-ts && bun test test/runner.test.ts && bun run typecheck
```

Expected: PASS.

- [ ] **Step 7: Commit the runtime readiness change**

```bash
git add server/agent/runtime_policy.go server/agent/runtime_policy_test.go agent-ts/src/runner.ts agent-ts/test/runner.test.ts
git commit -m "refactor(agent): require Seednote MCP research tools"
```

### Task 2: Switch The Plugin Workflow To Server MCP

**Files:**
- Modify: `server/agent/seednote_skill_contract_test.go:449-635`
- Modify: `server/agent/skill_best_practices_test.go:88-108,165-185`
- Modify: `server/agent/seednote_context_contract_test.go:31-45`
- Modify: `plugins/packs/seednote/agent-pack.yaml:6-11`
- Modify: `plugins/packs/seednote/agent.claude.md`
- Modify: `plugins/packs/seednote/agent.codex.toml`
- Modify: `plugins/skills/seednote-research/SKILL.md`
- Modify: `plugins/skills/anban-setup/SKILL.md:65-78`
- Delete: `plugins/skills/agent-reach/SKILL.md`
- Delete: `plugins/skills/agent-reach/references/examples.md`
- Modify: `plugins/README.md`
- Modify: `plugins/docs/codex-installation.md`
- Modify: `plugins/CHANGELOG.md`
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.codex-plugin/plugin.json`

- [ ] **Step 1: Replace the parent workflow contract tests first**

Replace the Agent Reach tests in `seednote_skill_contract_test.go` with tests
that require these terms in both generated Seednote Agents and the research
Skill:

```go
required := []string{
	"check_seednote_login_status",
	"get_seednote_login_qrcode",
	"search_seednote_feeds",
	"get_seednote_feed_detail",
	"get_seednote_user_profile",
	"data_source=xiaohongshu-mcp",
	"token_source",
	"missing_fields",
	"fallback_reason",
	"原创模式不得",
	"output/failure-state.json",
}
forbidden := []string{
	"Agent-Reach", "agent-reach", "active_backend", "backend_command_family",
	"mcporter", "OpenCLI", "xhs-cli", "http://sidecar-seednote:18060", "localhost:18060",
}
```

Keep the existing assertions that prohibit write operations and require the
replicate-mode recoverable failure. Add an assertion that
`plugins/skills/agent-reach` does not exist.

In `skill_best_practices_test.go`, remove the Agent Reach upstream-index
requirements and change the Seednote owned-Skill list to:

```go
"seednote": {"seednote-research", "seednote-viral-analysis", "seednote-writing", "seednote-visual-design"},
```

In `seednote_context_contract_test.go`, remove `"  - agent-reach"` from the
required frontmatter list and add it to the banned phrases.

- [ ] **Step 2: Run the tests and confirm the old plugin fails the new contract**

```bash
go test ./server/agent -run 'TestSeednote|TestAgentReach|TestClaudeSeednote|TestSkillUpstreamIndex|TestClaudeCodePluginAgentsDeclareOwnedSkills' -count=1
```

Expected: FAIL on existing Agent Reach terms, Skill directory, and old Skill
membership.

- [ ] **Step 3: Rewrite the canonical Seednote research entry**

Replace the external-data and backend-command sections of
`plugins/skills/seednote-research/SKILL.md` with this contract:

```markdown
## 外部数据入口

托管 Seednote 的小红书真实外部数据统一通过 Anban Server MCP 获取。禁止调用
Agent-Reach、OpenCLI、mcporter、xhs-cli、xiaohongshu-mcp 原始端点或自定义 HTTP
客户端。

1. 先调用 `check_seednote_login_status`。
2. 只有 `available=true` 且 `logged_in=true` 才调用搜索、详情或用户资料工具。
3. 原创模式不可用或未登录时继续保守选题，不得伪造热门数据。
4. 复刻模式仅有外部 ID/链接且无法获取源内容时，写
   `output/failure-state.json` 并从 research 恢复。
5. 工具传输失败只重试一次；再次失败后进入对应降级或可恢复失败。

## Anban MCP 小红书工具

| 工具 | 用途 |
| --- | --- |
| `check_seednote_login_status` | 返回 sidecar 可用性和登录状态 |
| `get_seednote_login_qrcode` | 仅用于运维恢复登录，不在托管任务中自动登录 |
| `search_seednote_feeds` | 按关键词搜索并返回真实互动字段、feed_id、xsec_token |
| `get_seednote_feed_detail` | 获取正文、图片、互动和评论 |
| `get_seednote_user_profile` | 获取公开用户资料和笔记列表 |

研究产物必须记录：

```text
data_source=<xiaohongshu-mcp|task_topic|topic_pool|project_context>
mcp_tools_used=<实际调用工具列表；未调用写 none>
available=<true|false|unknown>
logged_in=<true|false|unknown>
token_source=<search|profile|signed_url|missing>
missing_fields=<缺失字段列表>
fallback_reason=<无降级写 none>
```
```

Preserve the existing CES scoring, topic-pool priority, read-only boundary,
artifact paths, and rule that `feed_id`/`xsec_token` must come from real tool
output.

- [ ] **Step 4: Rewrite the canonical Claude and Codex Agent workflows**

In both Pack-owned Agent sources:

- remove the `agent-reach` Skill declaration/config;
- replace every doctor/backend instruction with the login-status -> search ->
  detail Server MCP sequence;
- preserve original-mode fallback and replicate-mode recovery;
- replace Agent Reach provenance with the fields from Step 3;
- keep the prohibition on JavaScript, Python, custom HTTP, direct sidecar, and
  external CLI clients.

The tool-boundary paragraph must read equivalently to:

```markdown
- 小红书真实外部数据必须使用 Claude Code/Codex 注入的 Anban MCP 工具：
  `check_seednote_login_status`、`get_seednote_login_qrcode`（仅运维恢复）、
  `search_seednote_feeds`、`get_seednote_feed_detail`、
  `get_seednote_user_profile`。
- 禁止使用 Agent-Reach、OpenCLI、mcporter、xhs-cli、直接访问
  xiaohongshu-mcp，或编写自定义 HTTP 客户端。
```

Update `agent-pack.yaml` to:

```yaml
agent:
  name: seednote
  claude_source: agent.claude.md
  codex_source: agent.codex.toml
  skills: [seednote-research, seednote-viral-analysis, seednote-writing, seednote-visual-design]
  max_turns: 20
```

- [ ] **Step 5: Update setup, distribution docs, manifests, and changelog**

Replace the `anban-setup` Agent Reach preflight with an Anban MCP login-status
preflight and operator-only QR recovery. Delete `plugins/skills/agent-reach`.
Remove its README source-index row and change the Codex installation count from
29 to 28.

Bump both native manifests from `4.1.4` to `4.1.5`. Add at the top of the
changelog:

```markdown
## [4.1.5] - 2026-08-11

### Changed

- Routed managed Seednote Xiaohongshu research through authenticated Anban MCP
  tools backed by the deployed xiaohongshu-mcp service.

### Removed

- Removed the Agent Reach Skill and managed-runtime command routing contract.
```

- [ ] **Step 6: Regenerate native Agents and the Server catalog**

From the parent repository root:

```bash
make agent-pack-generate
make agent-pack-check
```

Expected: generated `plugins/agents/seednote.md`,
`plugins/agents/seednote.toml`, and `server/agentpack/catalog.generated.json`
contain no Agent Reach Skill and `agent-pack-check` passes.

- [ ] **Step 7: Run the focused parent contract tests**

```bash
go test ./server/agent -run 'TestSeednote|TestClaudeSeednote|TestSkillUpstreamIndex|TestClaudeCodePluginAgentsDeclareOwnedSkills' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit only the plugin child changes**

First confirm `skills/humanizer` is still the only unrelated plugin change.
Then stage only these paths:

```bash
git -C plugins add .claude-plugin/plugin.json .codex-plugin/plugin.json CHANGELOG.md README.md docs/codex-installation.md agents/seednote.md agents/seednote.toml packs/seednote/agent-pack.yaml packs/seednote/agent.claude.md packs/seednote/agent.codex.toml skills/anban-setup/SKILL.md skills/seednote-research/SKILL.md
git -C plugins add -u skills/agent-reach
git -C plugins diff --cached --check
git -C plugins commit -m "refactor(seednote): use authenticated MCP research"
```

Expected: `git -C plugins status --short` still shows only the pre-existing
` M skills/humanizer` line after the commit.

### Task 3: Synchronize The Parent Pack Contract And Plugin Gitlink

**Files:**
- Modify: `server/agentpack/catalog.generated.json`
- Modify: `server/agent/seednote_skill_contract_test.go`
- Modify: `server/agent/skill_best_practices_test.go`
- Modify: `server/agent/seednote_context_contract_test.go`
- Modify: `server/agent/unified_plugin_contract_test.go:57-68`
- Modify: `plugins` gitlink

- [ ] **Step 1: Update the plugin version contract**

Change `unified_plugin_contract_test.go` to require version `4.1.5` and describe
the direct authenticated Seednote MCP research contract in its failure message.

- [ ] **Step 2: Verify generated catalog content and digest**

```bash
make agent-pack-check
rg -n 'agent-reach|Agent-Reach|mcporter|active_backend' server/agentpack/catalog.generated.json plugins/agents/seednote.md plugins/agents/seednote.toml
```

Expected: `make agent-pack-check` passes and `rg` returns no matches.

- [ ] **Step 3: Run parent Pack and plugin contract tests**

```bash
go test ./server/agentpack ./server/agent -run 'Test.*(Seednote|Plugin|Skill|Catalog|Pack).*' -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit parent tests, generated catalog, and child gitlink**

```bash
git add plugins server/agentpack/catalog.generated.json server/agent/seednote_skill_contract_test.go server/agent/skill_best_practices_test.go server/agent/seednote_context_contract_test.go server/agent/unified_plugin_contract_test.go
git diff --cached --check
git commit -m "refactor(seednote): make Server MCP the research boundary"
```

Do not push the parent yet. A release push must publish the plugin child commit
before the parent gitlink.

### Task 4: Remove Agent Reach And Python From Seednote Runtime Images

**Files:**
- Modify: `server/agent/docker_runtime_contract_test.go:310-350,501-541`
- Modify: `server/agent/montage_contract_test.go:29-45`
- Modify: `server/agent/runtime_names.go:13-48`
- Modify: `deploy/docker/Dockerfile.agent-seednote`
- Modify: `deploy/docker/Dockerfile.agent-seednote-ts`
- Modify: `deploy/docker/runtime-smoke.sh:492-496`
- Modify: `Makefile:20-26,70-73,166-191,260-295`
- Modify: `.gitmodules`
- Delete: `third_party/Agent-Reach` gitlink
- Delete: `scripts/update-agent-reach.sh`
- Modify: `server/git_sync_test.go:38-102`

- [ ] **Step 1: Change Docker contracts to require a minimal Seednote runtime**

In `TestAgentDockerfilesSeparateArticleAndSeednoteDependencies`, require the
shared Node/Claude/plugin runtime in both files and use this forbidden list for
Seednote as well as Article:

```go
forbidden := []string{
	"Agent-Reach", "AGENT_REACH", "python3", "python3-venv", "pip",
	"venv", "mcporter", "third_party/Agent-Reach", "OpenCLI", "xhs-cli",
}
```

Replace `TestAgentReachIsBuildInstalledAndRuntimeReadOnly` and
`TestAgentReachSubmodulePathIsDeclared` with:

```go
func TestSeednoteRuntimeUsesContentPATHWithoutExternalRouter(t *testing.T) {
	if got := containerRuntimePath(model.PlatformSeednote); got != ContainerContentRuntimePath {
		t.Fatalf("seednote runtime PATH = %q, want %q", got, ContainerContentRuntimePath)
	}
	for _, name := range []string{"Dockerfile.agent-seednote", "Dockerfile.agent-seednote-ts"} {
		body := readTextFile(t, filepath.Join(repositoryRoot(t), "deploy", "docker", name))
		for _, forbidden := range []string{"Agent-Reach", "AGENT_REACH", "python3", "pip", "venv", "mcporter"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains removed dependency %q", name, forbidden)
			}
		}
	}
}

func TestAgentReachSubmoduleIsRemoved(t *testing.T) {
	gitmodules := readTextFile(t, filepath.Join(repositoryRoot(t), ".gitmodules"))
	if strings.Contains(gitmodules, "Agent-Reach") {
		t.Fatal(".gitmodules still declares Agent Reach")
	}
}
```

Update `TestContainerRuntimePathUsesPackRuntimeAdapter` to compare Seednote
against `ContainerContentRuntimePath`.

- [ ] **Step 2: Run the focused tests and confirm they fail**

```bash
go test ./server/agent -run 'TestAgentDockerfilesSeparateArticleAndSeednoteDependencies|TestSeednoteRuntimeUsesContentPATHWithoutExternalRouter|TestAgentReachSubmoduleIsRemoved|TestContainerRuntimePathUsesPackRuntimeAdapter' -count=1
```

Expected: FAIL on the current Dockerfiles, runtime PATH, and `.gitmodules`.

- [ ] **Step 3: Minimize the Go Seednote Dockerfile**

Delete the Agent Reach-only block beginning at `# Seednote-only dependencies`
through the yt-dlp config/template block. Keep the independent Seednote
Dockerfile, plugin installation, UID/GID, `tini`, `/run/secrets/anban`,
`WORKDIR`, entrypoint, and command unchanged.

- [ ] **Step 4: Minimize the TypeScript Seednote Dockerfile**

Change the package install line to:

```dockerfile
apt-get install -y --no-install-recommends ca-certificates curl git jq tini; \
```

Delete `MCPORTER_VERSION`, `AGENT_REACH_VENV`, npm global mcporter install,
the Agent Reach copy/install block, and the Agent Reach PATH override. Preserve
the built TypeScript runtime, plugin tree, non-root user, and entrypoint.

- [ ] **Step 5: Remove runtime PATH specialization and build dependencies**

Delete `ContainerAgentReachVenvPath` and `ContainerSeednoteRuntimePath` from
`runtime_names.go`, and remove the `case "seednote"` branch so standard Pack
adapters return `ContainerContentRuntimePath`.

In the Makefile:

- remove `agent-reach-update` from `.PHONY` and delete its target;
- update Seednote comments to describe the independent Seednote workflow image;
- initialize only `third_party/claude-agent-sdk-go` for the Go image;
- remove submodule initialization from the TypeScript Seednote image target;
- remove the Agent Reach help line.

Remove `third_party/Agent-Reach` from `runtime-smoke.sh`'s submodule list.

- [ ] **Step 6: Remove the submodule and updater**

```bash
git rm third_party/Agent-Reach scripts/update-agent-reach.sh
```

Remove its block from `.gitmodules`. Delete only
`TestUpdateAgentReachFastForwardsDetachedSubmodule`,
`TestUpdateAgentReachRejectsDirtySubmodule`, and
`newAgentReachUpdateFixture` from `server/git_sync_test.go`; retain shared test
helpers used by the remaining git-sync tests.

- [ ] **Step 7: Run focused runtime verification**

```bash
go test ./server/agent ./server -run 'Test.*(Docker|Runtime|GitSync|AgentReach|Seednote).*' -count=1
cd agent-ts && bun test && bun run typecheck && bun run build
```

Expected: PASS.

- [ ] **Step 8: Commit runtime cleanup**

```bash
git add .gitmodules Makefile deploy/docker/Dockerfile.agent-seednote deploy/docker/Dockerfile.agent-seednote-ts deploy/docker/runtime-smoke.sh server/agent/docker_runtime_contract_test.go server/agent/montage_contract_test.go server/agent/runtime_names.go server/git_sync_test.go
git diff --cached --check
git commit -m "refactor(runtime): remove Agent Reach from Seednote images"
```

### Task 5: Remove Current Documentation Claims About Agent Reach Packaging

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `CLAUDE.md`
- Modify: `docs/montage-upgrade.md`

- [ ] **Step 1: Add a documentation scan contract**

Use this current-surface scan; historical specs, plans, changelogs, and Git
history are intentionally excluded:

```bash
rg -n 'Agent-Reach|agent-reach|AGENT_REACH|mcporter|Python/Agent-Reach' README.md AGENTS.md CLAUDE.md docs/montage-upgrade.md Makefile deploy/docker deploy/k8s server agent agent-ts plugins --glob '!plugins/CHANGELOG.md' --glob '!server/agent/runtime_policy.go' --glob '!**/*_test.go' --glob '!agent-ts/test/**'
```

Expected before edits: matches in current runtime and contributor docs.

- [ ] **Step 2: Update runtime descriptions**

Describe `creator-agent-seednote` as the independent Seednote workflow image,
not a Python/Agent Reach profile. Keep `creator-agent-montage` documented as the
Python/OpenMontage profile. Update build-target comments accordingly.

In operational docs, state that Xiaohongshu research flows through authenticated
Anban Server MCP tools backed by the separately deployed `sidecar-seednote`.
Do not describe direct Agent Job access to `http://sidecar-seednote:18060`.

- [ ] **Step 3: Re-run the current-surface scan**

```bash
rg -n 'Agent-Reach|agent-reach|AGENT_REACH|mcporter|Python/Agent-Reach' README.md AGENTS.md CLAUDE.md docs/montage-upgrade.md Makefile deploy/docker deploy/k8s server agent agent-ts plugins --glob '!plugins/CHANGELOG.md' --glob '!server/agent/runtime_policy.go' --glob '!**/*_test.go' --glob '!agent-ts/test/**'
```

Expected: no matches. The plugin changelog may retain historical release
entries.

- [ ] **Step 4: Commit documentation cleanup**

```bash
git add README.md AGENTS.md CLAUDE.md docs/montage-upgrade.md
git diff --cached --check
git commit -m "docs: describe direct Seednote MCP research"
```

### Task 6: Full Verification And Release Evidence

**Files:**
- Verify: all changed parent files
- Verify: all changed plugin child files
- Verify: `deploy/k8s/ack-sidecar-ilink.yaml` and `deploy/k8s/ack-sidecar-seednote.yaml`
- Verify: `server/Deployment.yaml`

- [ ] **Step 1: Verify the plugin child without staging the Humanizer change**

```bash
git -C plugins status --short
git -C plugins show --stat --oneline HEAD
make agent-pack-check
```

Expected: the new plugin commit contains only Seednote/plugin distribution
changes; `skills/humanizer` remains an unrelated working-tree change;
`agent-pack-check` passes.

- [ ] **Step 2: Run all code and contract tests**

```bash
go test ./server/mcp ./server/service ./server/agent ./server/agentpack -count=1
go test ./... -count=1
cd agent-ts && bun test && bun run typecheck && bun run build
```

Expected: all commands exit 0. If parallel Go execution reports SQLite locking
or stale-process interference, stop the stale process and rerun `go test -p 1
./... -count=1` before attributing a product failure.

- [ ] **Step 3: Build repository binaries**

```bash
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: both commands exit 0.

- [ ] **Step 4: Build and inspect both Seednote images**

```bash
make docker-seednote-agent-image
make docker-seednote-agent-ts-image
docker run --rm --entrypoint sh creator-agent-seednote:latest -c '! command -v python3 && ! command -v agent-reach && ! command -v mcporter'
docker run --rm --entrypoint sh creator-agent-seednote-ts:latest -c '! command -v python3 && ! command -v agent-reach && ! command -v mcporter'
```

Expected: both builds and both negative dependency checks exit 0. If Docker is
unavailable, report image verification as an explicit remaining gap; do not
claim image-level completion from source tests alone.

- [ ] **Step 5: Verify ACK wiring remains unchanged**

```bash
go test ./server -run 'TestACK.*Sidecars|Test.*Kubernetes.*Seednote' -count=1
rg -n 'ANBAN_SEEDNOTE_BASE_URL|http://sidecar-seednote:18060' server/Deployment.yaml deploy/k8s/ack-sidecars.md
```

Expected: tests pass, Server still points at the in-cluster sidecar, and no
Agent Job configuration points directly at that endpoint.

- [ ] **Step 6: Review the complete parent diff and commit graph**

```bash
git status --short
git log --oneline --decorate -8
git diff origin/main...HEAD -- . ':!docs/superpowers/plans/2026-07-22-managed-mcp-request-timeout.md' ':!docs/superpowers/plans/2026-07-31-claude-runtime-controls.md'
git -C plugins log --oneline --decorate -3
```

Expected: only the approved Seednote MCP migration plus the already committed
design/plan are in scope. Existing unrelated work remains unstaged.

### Task 7: Publish And Prove Production Cutover When Authorized

**Files:**
- Deploy: `deploy/docker/Dockerfile.agent-seednote` or `deploy/docker/Dockerfile.agent-seednote-ts`
- Deploy: `server/Deployment.yaml`
- Observe: fresh Seednote execution record and ACK Pod/Job image IDs

- [ ] **Step 1: Publish the plugin child before the parent gitlink**

```bash
git -C plugins push origin HEAD:main
git -C plugins ls-remote origin refs/heads/main
```

Expected: `origin/main` contains the exact child SHA recorded by the parent
gitlink. Do not push the parent until this is proven.

- [ ] **Step 2: Push the reviewed parent branch**

Run the repository's review-before-merge workflow, then push only after the full
diff and tests are accepted. Expected: remote parent commit references the
already published plugin SHA.

- [ ] **Step 3: Publish the selected Seednote runtime by immutable digest**

Build from the matching Dockerfile, push to ACR with a non-reused commit tag,
resolve the registry digest, and set Yunxiao `seednote_agent_image_repo` to:

```text
registry.cn-hangzhou.aliyuncs.com/anban/creator-agent-seednote@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Expected: the digest is immutable and contains neither Python, Agent Reach, nor
mcporter.

- [ ] **Step 4: Roll the Server image mapping and verify ACK**

Deploy `server/Deployment.yaml` through Yunxiao with the new
`seednote_agent_image_repo`, wait for Server rollout, and verify the Server Pod
environment resolves `ANBAN_AGENT_IMAGE_SEEDNOTE` to the new digest. Verify the
existing `seednote` sidecar Deployment is healthy and logged in; do not rebuild
it unless its own digest changes.

- [ ] **Step 5: Run a fresh managed Seednote acceptance task**

Acceptance evidence must show:

```text
new Seednote runtime image digest
check_seednote_login_status tool call
search_seednote_feeds tool call
get_seednote_feed_detail tool call
real feed_id and xsec_token provenance
data_source=xiaohongshu-mcp
no agent-reach, mcporter, OpenCLI, or direct sidecar command
successful original output or specified recoverable replicate failure
```

Use persisted execution/runtime diagnostics and ACK Job `imageID` as the
authoritative production proof. Pipeline success alone is not sufficient.
