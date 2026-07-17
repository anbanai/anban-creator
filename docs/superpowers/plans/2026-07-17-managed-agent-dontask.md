# Managed Agent DontAsk Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every managed Claude Code plugin agent explicitly headless with official `permissionMode: dontAsk` plus a deterministic no-question behavioral contract.

**Architecture:** Claude Code CLI 2.1.208 loads plugin agents natively through `--agent`, so agent frontmatter owns `dontAsk`. The server-owned allowlist, deny list, hooks, and permission callback remain the security boundary. Agent prompts own business defaults so denied interaction cannot turn into a textual question followed by premature completion.

**Tech Stack:** Claude Code plugin agent Markdown, Go contract tests, JSON plugin manifest

---

### Task 1: Update the official frontmatter contract tests

**Files:**
- Modify: `server/agent/claude_plugin_best_practices_test.go`

- [ ] **Step 1: Write the failing frontmatter test**

Add `permissionMode` to the supported field map, remove it from `ignoredForPluginAgents`, and require every file in `claudecode/agents/*.md` to declare `dontAsk`:

```go
allowed := map[string]bool{
    "name": true, "description": true, "model": true, "effort": true,
    "maxTurns": true, "tools": true, "disallowedTools": true,
    "skills": true, "memory": true, "background": true, "isolation": true,
    "color": true, "permissionMode": true,
}
ignoredForPluginAgents := map[string]bool{
    "hooks": true, "mcpServers": true,
}

if got := frontmatterString(fm["permissionMode"]); got != "dontAsk" {
    t.Fatalf("%s permissionMode = %q, want dontAsk for managed zero-interaction execution", path, got)
}
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
go test ./server/agent -run TestClaudeCodePluginAgentsUseOnlySupportedFrontmatterFields -count=1
```

Expected: FAIL on the first agent missing `permissionMode`.

- [ ] **Step 3: Add a behavioral contract test**

Add a table-driven test that reads every managed agent and requires these exact contract anchors:

```go
for _, want := range []string{
    "全自动执行契约",
    "不得调用 `AskUserQuestion`",
    "不得在文本中向用户提问",
    "任务输入 → 项目默认 → 服务端默认 → 能力注册表推荐",
    "结构化失败诊断",
} {
    if !strings.Contains(body, want) {
        t.Fatalf("%s missing autonomous contract %q", path, want)
    }
}
```

- [ ] **Step 4: Run the package test and confirm the new test fails**

```bash
go test ./server/agent -run 'TestClaudeCodePluginAgents(UseOnlySupportedFrontmatterFields|DeclareAutonomousExecution)' -count=1
```

Expected: FAIL because agent bodies do not yet share the contract.

### Task 2: Declare dontAsk and deterministic behavior in every agent

**Files:**
- Modify: `claudecode/agents/designer.md`
- Modify: `claudecode/agents/ecommerce.md`
- Modify: `claudecode/agents/live-slicer.md`
- Modify: `claudecode/agents/moments.md`
- Modify: `claudecode/agents/montage.md`
- Modify: `claudecode/agents/seednote.md`
- Modify: `claudecode/agents/videocreator.md`
- Modify: `claudecode/agents/videoeditor.md`
- Modify: `claudecode/agents/wechatarticle.md`

- [ ] **Step 1: Add the official permission mode**

Add this field to each YAML frontmatter:

```yaml
permissionMode: dontAsk
```

- [ ] **Step 2: Add the shared behavioral contract**

Add this section immediately after each agent role or introduction:

```markdown
## 全自动执行契约

- 这是平台托管的零交互任务；不得调用 `AskUserQuestion`，不得在文本中向用户提问，也不得因等待选择而结束当前执行。
- 缺失选择固定按“任务输入 → 项目默认 → 服务端默认 → 能力注册表推荐”解析，并把采用的默认值和回退原因写入任务产物或进度记录。
- 只要候选路径仍在已配置的 provider、能力、预算与安全边界内，就自动选择最优可用路径继续执行。
- 认证失败、无必需能力、硬预算冲突、素材损坏或交付约束不可满足时，写入结构化失败诊断并终止；不得询问替代方案。
```

- [ ] **Step 3: Run focused contract tests**

```bash
go test ./server/agent -run 'TestClaudeCodePluginAgents(UseOnlySupportedFrontmatterFields|DeclareAutonomousExecution)' -count=1
```

Expected: PASS.

- [ ] **Step 4: Keep the changes uncommitted until plugin release metadata is updated**

The agent files live in the managed `claudecode` submodule. Do not stage the superproject gitlink yet; Task 4 commits the complete plugin release first, then records its new SHA in the root repository.

### Task 3: Encode full-run preauthorization for Montage

**Files:**
- Modify: `claudecode/agents/montage.md`
- Modify: `claudecode/skills/montage/SKILL.md`
- Modify: `openclaw/skills/montage/SKILL.md`
- Modify: `codex/skills/montage/SKILL.md`
- Modify: `server/agent/montage_contract_test.go`

- [ ] **Step 1: Write the failing Montage contract test**

```go
for _, want := range []string{
    `"approval_policy"`,
    `"mode": "auto"`,
    `"source": "anban_managed_task"`,
    `"scope": "full_run"`,
    "自动批准常规 creative gate",
    "不得跳过 checkpoint",
} {
    if !strings.Contains(agentText+skillText, want) {
        t.Fatalf("Montage autonomous contract missing %q", want)
    }
}
```

- [ ] **Step 2: Run the test and verify it fails**

```bash
go test ./server/agent -run TestMontageManagedApprovalPolicy -count=1
```

Expected: FAIL with the first missing approval-policy anchor.

- [ ] **Step 3: Extend the adapter manifest and workflow instructions**

Add this field to the documented `montage-project.json`:

```json
"approval_policy": {
  "mode": "auto",
  "source": "anban_managed_task",
  "scope": "full_run"
}
```

Require each upstream human gate to retain its checkpoint and decision log while recording Anban managed-task preauthorization. Authentication, capability, budget, safety, source corruption, and impossible-delivery blockers write `failure-diagnosis.md` and terminate. Apply the same stable Montage manifest contract to the OpenClaw and Codex skill mirrors.

- [ ] **Step 4: Run focused tests**

```bash
go test ./server/agent -run 'TestMontageManagedApprovalPolicy|TestClaudeCodePluginAgents' -count=1
```

Expected: PASS.

### Task 4: Publish the plugin contract change

**Files:**
- Modify: `claudecode/.claude-plugin/plugin.json`
- Modify: `claudecode/CHANGELOG.md`
- Modify: `openclaw/openclaw.plugin.json`
- Modify: `codex/.codex-plugin/plugin.json`
- Modify: `server/agent/docker_runtime_contract_test.go`

- [ ] **Step 1: Bump the Claude plugin patch version**

Increment the patch version in all three affected plugin manifests. Add a Claude changelog entry describing managed `dontAsk` and Montage full-run preauthorization.

- [ ] **Step 2: Keep the runtime on a compatible Claude Code release**

Keep the Docker image pinned to Claude Code `2.1.208` or newer:

```go
if !strings.Contains(body, "ARG CLAUDE_CODE_VERSION=2.1.208") {
    t.Fatal("managed image must pin a Claude Code release with agent-level dontAsk")
}
```

- [ ] **Step 3: Run verification**

```bash
go test ./server/agent -count=1
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 4: Commit each managed plugin repository**

```bash
git -C claudecode add agents skills/montage .claude-plugin/plugin.json CHANGELOG.md
git -C claudecode commit -m "feat: make managed workflows zero interaction"
git -C openclaw add skills/montage openclaw.plugin.json
git -C openclaw commit -m "feat: preauthorize managed montage runs"
git -C codex add skills/montage .codex-plugin/plugin.json
git -C codex commit -m "feat: preauthorize managed montage runs"
```

- [ ] **Step 5: Commit root tests and managed gitlinks**

```bash
git add claudecode openclaw codex server/agent/claude_plugin_best_practices_test.go server/agent/montage_contract_test.go server/agent/docker_runtime_contract_test.go
git commit -m "feat(agent): enforce autonomous plugin contracts"
```
