# Project Creator Chat UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Project Creator replies easier to answer and keep runtime progress out of the conversation history while preserving useful errors and optional structured questions.

**Architecture:** Update the canonical Project Creator DSH Agent Pack prompt and its plugin guidance so the model sends one concise user-facing response, proactively considers contextual question cards, and never narrates internal tool work. Keep event state internally for recovery, but render successful runtime activities only as one live status near the composer; render actionable failures and stops in the transcript.

**Tech Stack:** DSH Agent Pack YAML, generated Claude/Codex assets, Go Agent Pack generator, React 19, TypeScript, Vitest, Bun.

**Spec:** `docs/project-creator.md` and the user request in the current conversation.

## Global Constraints

- Preserve the Server-owned draft, confirmation, profile, memory, and session boundaries.
- Keep question batches optional and limited to one focused topic; offer custom text with contextual options.
- Update both plugin manifests and plugin version metadata for distributed Agent Pack changes.
- Use the Agent Pack source-of-truth workflow; do not hand-edit generated DSH presets.

## Review Focus

- An Agent using a question card should not be instructed to ask the same question again in prose.
- Completed turns and successful tool calls should not remain as transcript entries after refresh/replay.
- Tool or turn errors must remain visible and actionable after successful activity rows are hidden.
- Profile updates must remain scoped to changes and preserve confirmed snapshot dimensions.
- Other assistant modes should retain generic live progress without exposing raw tool names.

---

### Task 1: Clarify Project Creator Conversation Policy

**Files:**
- Modify: `harness/packs/project-creator/agent.dsh.yml`
- Modify: `harness/docs/plugin-development.md`
- Modify: `harness/CHANGELOG.md`
- Modify: `harness/package.json`, `harness/pnpm-lock.yaml`, `harness/.claude-plugin/plugin.json`, `harness/.claude-plugin/marketplace.json`, `harness/.codex-plugin/plugin.json`
- Generate: `harness/agents/project-creator.md`, `harness/agents/project-creator.toml`, `harness/packs/project-creator/agent.claude.md`, `harness/packs/project-creator/agent.codex.toml`, `harness/dsh/presets/project-creator/agent.cordis.yml`, `harness/packs/project-creator/agent-pack.yaml`

- [x] Update Pack source with concise response shape, no narration of Skill/tool work, proactive-but-optional `AskUserQuestion` selection, and one topic per response.
- [x] Verify exact generated source parity through `agent-pack-check`.
- [x] Run `make agent-pack-generate` and `make agent-pack-check` from the repository root.
- [x] Update plugin release metadata and changelog, then run Harness contract checks.

### Task 2: Separate Runtime Status from Conversation History

**Files:**
- Modify: `studio/src/pages/ChatPage.tsx`
- Test: `studio/src/pages/ChatPage.test.tsx`
- Modify: `docs/project-creator.md`

- [x] Change transcript assertions so successful turn and tool activity events do not render as historical chat rows.
- [x] Verify one generic live status remains near the composer while work is active, without raw tool names.
- [x] Verify failure and stopped activity remains visible, question cards remain visible, and completed messages/questions/drafts retain their order.
- [x] Implement rendering and status changes, then run targeted ChatPage tests.
- [x] Update the UX contract to state that routine processing status is ephemeral and separate from conversation content.

### Task 3: Final Verification

- [x] Run `cd studio && bun run test -- src/pages/ChatPage.test.tsx`.
- [x] Run `cd studio && bun run build`.
- [x] Run `make agent-pack-check` and the Harness tests that validate Project Creator packaging and behavior.
- [x] Review the final diff and verify generated plugin versions are aligned.

`pnpm run verify:pack` could not complete because the local offline pnpm metadata cache lacks DSH package registry metadata; source verification and the Agent Pack checks passed.
