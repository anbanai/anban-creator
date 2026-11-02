# Consistent creation draft confirmation

Approved in chat on 2026-10-30; implementation requested on a new branch.

## Problem and outcome

A creation draft can be stored as ready while its `missing_inputs` list is nonempty. Confirmation rejects it, rewrites the failure as a revision conflict, and records that misleading receipt. Refresh returns the same ready draft. Studio also blocks input whenever any draft exists, silently reloads a draft during confirmation, and implements continue-discussing only in local state.

Server must own eligibility. Agent owns semantic clarification, Studio presents Server decisions, and task creation remains an explicit user confirmation with durable duplicate protection.

## Required behavior

- `missing_inputs` contains only necessary inputs that block execution. User-approved placeholders, later supplements, out-of-scope deliverables, and research the execution agent can perform are retained in existing requirements/constraints/temporary overrides instead. Server never infers this classification from prose.
- One creation-plan validation capability is reused by staging, snapshots, and confirmation. A parseable but incomplete draft is saved with `ready=false` and structured blocking issues. Malformed/unknown-field payloads are rejected without replacing the previous draft. Agent `ready=true` is only a request for confirmation eligibility.
- Eligibility additionally requires the latest draft of the mode, pending confirmation state, and no in-progress/completed commit. Older drafts never become eligible when a newer draft is withdrawn or incomplete.
- Snapshot recovery applies the same validation to historical drafts and preserves actionable failures. It does not revive invalid drafts as ready.
- Studio renders incomplete drafts as editable drafts with issues, and enables creation only for a server-approved current draft. SSE and snapshot updates must converge. Preserve local composer and question answers during recovery.
- Confirm exactly the `draft_id` and `revision` displayed. Do not refresh and substitute a different draft. Bind retry identity to draft plus final options; retain it after uncertain outcomes.
- Continue-discussing is a durable Server operation against an explicit draft/version. Retain content, withdraw confirmation eligibility, and restore editing across refresh. New staging produces a new revision. Confirm, continue, and replacement staging must be fenced in the database across Server instances; reusing existing confirmation states is preferred.
- Missing-input and invalid-content failures have their own actionable errors. Revision conflict is reserved for stale identity/version/state. Known pre-creation failures are not ambiguous creation outcomes. Successful receipts return the existing task, while uncertain outcomes retain reconciliation protection.
- Legacy generic-conflict receipts may be reclassified only when current immutable draft validation proves a pre-creation failure and no created task/result contradicts it. Never automatically rerun task creation during recovery. Re-staging a corrected draft must recover the active workflow rather than leave it blocked by an older failure.
- After successful confirmation create at most one task per accepted draft, preserve final execution settings and constraints, and mark the interaction complete.

## Shared HTTP contract

Keep existing endpoints and receipt fields. Add `blocking_issues?: Array<{code: string, field: string, message: string}>` to draft views and structured interaction errors. Extend stage receipts with the authoritative draft view so tool callers receive effective readiness and issues. Creation `available_actions` are derived from the same view. A non-ready draft allows editing; only an eligible pending draft offers `confirm_draft`.

Creation snapshots and draft views additionally expose optional `state_sequence` from the existing database `CommandSequence`. Claim, withdrawal, known-failure release and completion update this sequence and append their draft event atomically; snapshot reads capture the interaction, drafts and receipt under the same transaction fence. This orders same-revision transitions across Server instances without a schema migration or wall-clock assumptions. Studio accepts a released claim only from newer authoritative state and keeps attempt-option issues separate from immutable draft readiness. Event IDs remain contiguous.

Add `POST /assistant/interactions/:id/continue` accepting `{draft_id, revision}` and returning an interaction snapshot. The mutation is idempotent for the withdrawn revision and rejects attempts to withdraw a newer or already claimed draft. It does not send an Agent message or require an available runtime.

Use `assistant_creation_inputs_missing` (422), `assistant_draft_invalid` (422), `assistant_draft_not_ready` (409), the existing `assistant_draft_revision_conflict` (409), and existing uncertain/reconciliation codes. Invalid/missing-input errors offer `continue_editing`; stale versions offer `reload_snapshot`. Return structured issues and a snapshot where available. Existing unrelated business errors retain their established semantics.

## Scope and verification

Only the creation interaction workflow and necessary shared plumbing change. Preserve profile/review behavior. No production mutation, deployment, or push is part of this request. Use an isolated worktree and `codex/assistant-confirmation-consistency` branch. Preserve unrelated files.

Tests cover missing-input staging and recovery; incomplete versus malformed payloads; allowed supplements carried into one created task; repeated confirmations; stale drafts and cross-instance competing operations; durable continue-discussing; legacy failure receipt recovery; and UI snapshot/event agreement and exact-version commits.

Run focused regression tests during implementation, full Go tests plus Server build, full Studio tests plus build, affected MCP boundary checks, and plugin generation/distribution checks. Harness changes follow both mandatory guides and synchronized version updates.
