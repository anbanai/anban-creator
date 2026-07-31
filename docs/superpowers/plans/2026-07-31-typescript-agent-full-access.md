# TypeScript Agent Full Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every TypeScript managed Agent unrestricted Claude Code tool access without emitting the `CLAUDE_SDK_CAN_USE_TOOL_SHADOWED` warning.

**Architecture:** Configure the official Claude Agent SDK with `permissionMode: "bypassPermissions"` and its required `allowDangerouslySkipPermissions: true` acknowledgement. Remove the static allowlist, deny list, and `canUseTool` callback while preserving non-permission lifecycle hooks such as the Seednote Stop quality gate.

**Tech Stack:** TypeScript, Bun test, `@anthropic-ai/claude-agent-sdk` 0.3.220

---

### Task 1: Replace the managed permission policy

**Files:**
- Modify: `agent-ts/test/runner.test.ts`
- Modify: `agent-ts/src/runner.ts`

- [x] **Step 1: Write the failing test**

Add a `buildQueryOptions` test asserting `permissionMode` is `bypassPermissions`, `allowDangerouslySkipPermissions` is `true`, and `allowedTools`, `disallowedTools`, and `canUseTool` are absent.

- [x] **Step 2: Run the test to verify it fails**

Run: `cd agent-ts && bun test test/runner.test.ts`

Expected: FAIL because the current options use `dontAsk` and include the three restrictive permission properties.

- [x] **Step 3: Write the minimal implementation**

Remove the tool lists and `isAllowedTool`, then replace the existing permission options with:

```ts
permissionMode: "bypassPermissions",
allowDangerouslySkipPermissions: true,
```

- [x] **Step 4: Run focused verification**

Run: `cd agent-ts && bun test test/runner.test.ts && bun run typecheck && bun run build`

Expected: all commands pass without TypeScript errors.

- [x] **Step 5: Run the complete TypeScript Agent test suite**

Run: `cd agent-ts && bun test`

Expected: all tests pass.
