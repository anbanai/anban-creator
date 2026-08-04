# TypeScript Managed Main Agent Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent the TypeScript managed main Agent from delegating its workflow and preserve tool-use evidence in the terminal execution result.

**Architecture:** Keep the existing `bypassPermissions` execution mode while applying the same three managed-session exclusions as the Go runtime. Track every assistant `tool_use` block in runner state and attach the total plus deterministic per-name counts to the existing result payload so Server artifact diagnostics can classify nested-only executions.

**Tech Stack:** TypeScript 5.9, Bun test runner, Anthropic Agent SDK 0.3.220, Go Server contract tests.

---

### Task 1: Establish the managed tool policy regression

**Files:**
- Modify: `agent-ts/test/runner.test.ts`
- Modify: `agent-ts/src/runner.ts`

- [x] **Step 1: Change the existing option test to require the managed exclusions**

Keep the assertions for `bypassPermissions`, no `allowedTools`, and no `canUseTool`, then replace the absent `disallowedTools` assertion with:

```ts
expect(options.disallowedTools).toEqual(["Agent", "ScheduleWakeup", "AskUserQuestion"]);
```

- [x] **Step 2: Run the focused test and verify the expected failure**

Run: `cd agent-ts && bun test test/runner.test.ts -t "keeps unrestricted access within the managed session policy"`

Expected: FAIL because `options.disallowedTools` is currently undefined.

- [x] **Step 3: Add the minimal SDK option**

Add this property beside the existing permission options in `buildQueryOptions`:

```ts
disallowedTools: ["Agent", "ScheduleWakeup", "AskUserQuestion"],
```

- [x] **Step 4: Re-run the focused test**

Run: `cd agent-ts && bun test test/runner.test.ts -t "keeps unrestricted access within the managed session policy"`

Expected: PASS.

### Task 2: Add terminal tool-use diagnostics

**Files:**
- Modify: `agent-ts/test/runner.test.ts`
- Modify: `agent-ts/src/runner.ts`
- Modify: `agent-ts/src/reporter.ts`

- [x] **Step 1: Add a failing test for multiple assistant tool calls**

Exercise the runner's exported tool-use recorder with content containing two `Agent` blocks and one `mcp__anban__write_article` block, then assert:

```ts
expect(diagnostics).toEqual({
  tool_use_count: 3,
  tool_use_summary: { Agent: 2, mcp__anban__write_article: 1 },
});
```

- [x] **Step 2: Run the focused test and verify the expected failure**

Run: `cd agent-ts && bun test test/runner.test.ts -t "counts assistant tool uses for terminal diagnostics"`

Expected: FAIL because the recorder does not exist.

- [x] **Step 3: Implement the minimal shared diagnostics state**

Add a typed state with `tool_use_count` and `tool_use_summary`, initialize it once in `runClaude`, and update it for each assistant `tool_use` block. Pass the same state through message consumption and spread its fields into every SDK result terminal payload. Add explicit optional result fields:

```ts
tool_use_count?: number;
tool_use_summary?: Record<string, number>;
```

The recorder must retain full SDK tool names because the Server classifier expects `Agent` exactly and diagnostic evidence should match progress logs.

- [x] **Step 4: Re-run the focused runner test**

Run: `cd agent-ts && bun test test/runner.test.ts`

Expected: PASS with the existing model-usage, environment, and query-option tests unchanged.

### Task 3: Verify the complete affected surface

**Files:**
- Review: `agent-ts/src/runner.ts`
- Review: `agent-ts/src/reporter.ts`
- Review: `agent-ts/test/runner.test.ts`
- Review: `server/agent/artifacts.go`
- Review: `server/agent/runtime_policy.go`

- [x] **Step 1: Run all TypeScript runtime checks**

```bash
cd agent-ts && bun test test/runner.test.ts
cd agent-ts && bun test
cd agent-ts && bun run typecheck
cd agent-ts && bun run build
```

Expected: all commands exit 0.

- [x] **Step 2: Run Server contract and repository-wide Go checks**

```bash
go test ./server/agent ./server/service
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
```

Expected: all commands exit 0.

- [x] **Step 3: Audit scope and whitespace**

Run `git diff --check`, inspect the full diff, and confirm only the design, implementation plan, runner, reporter type, and runner tests are included. Confirm both plugin manifests remain unchanged because no plugin assets changed.

- [x] **Step 4: Commit the implementation**

```bash
git add docs/superpowers/plans/2026-08-04-typescript-managed-main-agent-policy.md agent-ts/src/runner.ts agent-ts/src/reporter.ts agent-ts/test/runner.test.ts
git commit -m "fix(agent-ts): enforce managed main agent policy"
```

### Task 4: Review, merge, and prove publication

**Files:**
- Review: complete branch diff from `main` through feature HEAD

- [ ] **Step 1: Review the committed branch against the approved design**

Inspect `git diff main...HEAD`, staged cleanliness, submodule status, and the exact test evidence. Resolve every blocking or important issue before integration.

- [ ] **Step 2: Merge the named feature branch into `main`**

From the primary worktree, fetch remote state, verify `main` is not behind `origin/main`, and merge `codex/ts-main-agent-policy` without including the primary worktree's unrelated untracked files.

- [ ] **Step 3: Re-run affected checks on merged `main`**

Run the full TypeScript runtime checks, repository-wide Go tests, both Go builds, and `git diff --check` from the merged tree.

- [ ] **Step 4: Push and prove remote main**

Push `main`, then compare `git rev-parse main` with `git ls-remote origin refs/heads/main`. They must match before reporting publication.

- [ ] **Step 5: Record the production boundary**

Check for a Docker-compatible CLI. If unavailable, report that the TypeScript Article image build/publish, immutable digest rollout, and fresh production Article acceptance test remain separate from the verified source merge.
