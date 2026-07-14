# MCP Operation Credit Overdraft Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep task-creation credit validation while making MCP operation billing non-blocking and removing balance-gated control flow from Agent and Skill content.

**Architecture:** Task creation and non-MCP operations continue through `DeductCredits`, which conditionally updates only sufficient balances. MCP call sites use explicit `DeductForMCPOperation*` methods backed by `AdjustBalance(-amount)`, preserving transaction, metadata, attribution, idempotency, and refund behavior without broadening overdraft to interactive non-MCP surfaces. Video workflow documents retain pricing facts but remove balance checks and recharge instructions across all three plugin distributions.

**Tech Stack:** Go, GORM, SQLite-backed Go tests, Markdown plugin skills, JSON plugin manifests.

---

### Task 1: Lock the credit boundary with tests

**Files:**
- Modify: `server/service/credit_test.go`
- Verify: `server/service/credit_ecommerce_test.go`

- [x] **Step 1: Write the failing operation-overdraft tests**

Add table-driven coverage that calls `DeductForMCPOperation` with and without an optional `task_id`, starts at 50 credits, deducts 120, and expects a returned and persisted balance of -70. Assert the operation transaction keeps its optional `task_id` and `balance_after=-70`. Add a separate test proving `DeductForOperation` still rejects the same charge outside MCP.

- [x] **Step 2: Run the focused service tests and verify RED**

Run: `go test ./server/service -run 'TestDeductForMCPOperationAllowsNegativeBalance|TestDeductForOperationRejectsNegativeBalanceOutsideMCP|TestDeductForTaskWithAmount_InsufficientCredits' -count=1`

Expected: the operation-overdraft test fails with `insufficient credits`; the task-creation test passes.

- [x] **Step 3: Make MCP operation deduction unconditional**

Add explicit `DeductForMCPOperation*` entry points and route MCP billing call
sites through them. Keep the existing `DeductForOperation*` entry points
fail-closed for non-MCP callers.

```go
newBalance, err = txRepo.Users().AdjustBalance(ctx, userID, -totalCost)
if err != nil {
    return fmt.Errorf("adjust balance: %w", err)
}
```

- [x] **Step 4: Run the focused service tests and verify GREEN**

Run: `go test ./server/service -run 'TestDeductForMCPOperationAllowsNegativeBalance|TestDeductForOperationRejectsNegativeBalanceOutsideMCP|TestDeductForTaskWithAmount_InsufficientCredits' -count=1`

Expected: PASS.

### Task 2: Remove balance-gated workflow instructions

**Files:**
- Modify: `server/agent/video_contract_test.go`
- Modify: `server/mcp/video_tools_test.go`
- Modify: `server/mcp/tools.go`
- Modify: `claudecode/skills/seedance-20/references/mcp-contract.md`
- Modify: `claudecode/skills/seedance-20/references/anban-mcp-contract.md`
- Modify: `claudecode/skills/dreamina-video/references/mcp-contract.md`
- Modify the matching files under `codex/skills/` and `openclaw/skills/`

- [x] **Step 1: Add failing distribution and profile contract tests**

Add a test over all nine mirrored video contract files rejecting `balance cannot cover`, `insufficient credits`, and `recharge`. Extend the video project profile test to reject `insufficient_credit_rule` and recharge guidance in `agent_brief`.

- [x] **Step 2: Run contract tests and verify RED**

Run: `go test ./server/agent ./server/mcp -run 'TestVideoSkillsDoNotGateExecutionOnCreditBalance|TestGetProjectVideoProfile' -count=1`

Expected: FAIL because the current Skill contracts and project profile tell the Agent to stop and recharge.

- [x] **Step 3: Remove workflow gates while retaining pricing facts**

Delete only the final sentence that instructs the Agent to stop and recharge from all nine mirrored contract files. Remove `insufficient_credit_rule` from `buildVideoProjectProfile`, and make `buildProjectAgentBrief` state only that task creation charges the base service fee and `video_gen` is recorded separately at provider submission.

- [x] **Step 4: Run contract tests and verify GREEN**

Run: `go test ./server/agent ./server/mcp -run 'TestVideoSkillsDoNotGateExecutionOnCreditBalance|TestGetProjectVideoProfile' -count=1`

Expected: PASS.

### Task 3: Version and verify plugin distributions

**Files:**
- Modify: `claudecode/.claude-plugin/plugin.json`
- Modify: `claudecode/CHANGELOG.md`
- Modify: `codex/.codex-plugin/plugin.json`
- Modify: `openclaw/openclaw.plugin.json`
- Modify: `server/agent/plugin_binary_contract_test.go`

- [x] **Step 1: Patch-bump affected plugin manifests**

Bump Claude Code `2.10.59` to `2.10.60`, Codex `2.10.53` to `2.10.54`, and OpenClaw `2.7.45` to `2.7.46`. Update exact version assertions and record the Claude Code workflow-contract change in its changelog.

- [x] **Step 2: Run targeted parity and version tests**

Run: `go test ./server/agent ./server/mcp -run 'TestPluginsWireAnbanBootstrap|TestSeedance20SkillFiles|TestVideoSkillContractsUseVideoCreatorInputReferences|TestVideoSkillsDoNotGateExecutionOnCreditBalance' -count=1`

Expected: PASS.

- [x] **Step 3: Run full verification**

Run: `go test ./...`

Expected: PASS.

- [x] **Step 4: Inspect the final diff**

Run: `git diff --check && git status --short && git diff -- server/service/credit.go server/service/credit_test.go server/mcp/tools.go server/mcp/video_tools_test.go server/agent/video_contract_test.go claudecode codex openclaw`

Expected: no whitespace errors; only the intended billing boundary, workflow contracts, tests, and required plugin version metadata changed by this task.
