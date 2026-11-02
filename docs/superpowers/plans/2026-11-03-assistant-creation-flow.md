# Assistant creation flow implementation

The user approved implementing the 2026-11-03 creation-flow review. Production testing showed duplicate ready banners and summaries, verbose question history, omitted plan requirements/constraints, malformed stage payload errors without actionable detail, and an assistant that remains disabled after a transient startup recovery failure.

## Global Constraints

- Preserve the existing single-column conversation and Server-owned confirmation, revision, generation, and idempotency boundaries. Only the user's final Studio action may create a Task.
- One latest plan card and one visible final action area. A ready plan means ready for user review, not permission to create or publish.
- Preserve all user decisions and raw history through disclosure; do not use fuzzy string matching, English-text filters, or invented message classification to hide prose.
- Make only changes directly required by this plan. No new frameworks, schema migration, publication behavior, shared checkout edits, or production deployment.
- Add behavior regression tests first and observe their expected failure. Use existing patterns and complete the repository-required tests/builds for changed surfaces.
- Work only in /Users/bbfisher/.codex/worktrees/assistant-creation-flow/anban-creator. Preserve unrelated changes. Local commits are allowed; no push/merge/deploy is required by this implementation request.
- Harness is a separate repository. Read docs/ANBAN-SKILL-SPEC.md and harness/docs/plugin-development.md before editing it; edit canonical sources, generate distributions, bump both native manifest versions together, and commit the submodule before its parent gitlink.

### Task 1: Make plan errors actionable and startup recovery bounded

Own server files and tests only (except the task report). Read AGENTS.md first.

1. Reproduce lost creation-plan error details through the actual service/MCP boundary. Add tests for wrong string/array types, an unknown plan field, malformed/null plan, and a valid realistic plan containing goal/content_type/topic/angle/expression_form/profile_context/temporary_overrides/requirements/constraints. Keep unknown-field rejection and ready validation strict.
2. Return safe field-level errors through the existing InteractionError blocking-issues structure and MCP error result. Preserve only field/type/reason information; do not echo full input, arbitrary scalar values, credentials, or internal database errors. Reuse normal Go JSON decoding diagnostics where possible; avoid building a general JSON-schema engine. Do not implement repair orchestration inside MCP.
3. Replace permanent disable-on-first-startup-failure with bounded startup recovery for interactive sessions. Keep the interactive service disabled until both orphan cleanup and durable session recovery succeed. Use a small fixed retry policy (three attempts, five seconds between failed attempts), cancellable context, and a per-attempt timeout. Run this during startup before opening the listener, so no recovery attempt can clean up an admitted active session. Preserve fatal configuration errors. Ensure the final failure remains disabled and has an accurate non-secret reason; bootstrap may expose a generic initialization-not-ready hint.
4. Add deterministic tests covering failure then success, attempts exhausted, cancellation, cleanup/recovery ordering, and no further cleanup after success. No real sleeps in tests; test the production retry flow through a small injected operation/wait boundary rather than copying its loop into tests.
5. Prefer a small focused helper over duplicating retry loops across container/kubernetes/process branches. Do not add a background scheduler or repeat startup cleanup after readiness.
6. Run focused tests through red/green, then server go test ./... and go build -o /tmp/anban-assistant-flow-server . once before committing. If missing harness assets block baseline tests, report exact paths to the controller without changing unrelated code.

Deliver a local commit and task-1-report.md with changes, red/green evidence, final commands and results, and unresolved evidence gaps.

### Task 2: Unify creation confirmation and information display

Own studio files and tests only. Use Bun. Read AGENTS.md and inspect current patterns before edits.

1. In creation mode, do not show the recovery banner for the normal needs_confirmation state when the current plan card carries that state. Preserve reconnect/error/reconciliation banners and non-creation behavior.
2. Keep a single latest plan card. During creation confirmation replace the disabled composer with one persistent action area containing 修改需求 and 开始创作, aligned with the card. Remove the duplicate card-footer action controls for this mode. Keep meaningful headings/accessible labels and clear readiness/blocked messages. Keyboard focus should move to the new plan's review heading without stealing focus repeatedly on unrelated rerenders.
3. 修改需求 resumes editing using the existing server continue-editing flow and preserves a collapsed view of the last plan. Its old revision cannot still be confirmed. Preserve existing revision/conflict/idempotency behavior and test draft revisions, duplicate clicks, and reconnection to the latest draft.
4. Render answered question batches as concise decision records (short header + selected/custom answer), with a disclosure for the full original question and answer. Keep custom answers intact; no generated extra answer bubble. History must remain accessible without showing every full question by default.
5. Expand the plan summary contract to include requirements and constraints and other existing meaningful plan fields where needed for a complete review (expected result, supplied assets, missing inputs, delegated decisions). Keep key requirements/constraints and temporary overrides visible; collapse inherited profile context and ordinary execution settings. Preserve review/profile mode summaries. Avoid guessing semantic equivalence; do not implement fuzzy deduplication.
6. Keep technical routing metadata out of the displayed original user message while preserving it for execution. Inspect the existing transport/snapshot contract and choose the smallest reliable way to distinguish original user text from appended execution context; don't strip arbitrary user prose by regex. If a Server contract change is unavoidable, explain the interface to the controller before editing Server files.
7. Ensure error state maps switching/context failures to appropriate copy rather than an invented unsent-message error. Handle only known operation states, not blanket wording heuristics.
8. Add behavior tests for a single final action area, no ready recovery banner, full plan requirements/constraints, disclosure of historical answers, continued editing, latest revision recovery, original user text, and accurate non-message failures. Run focused red/green, all Studio tests, and build once before committing.

Deliver a local commit and task-2-report.md with evidence. The controller will do a browser visual pass of desktop and narrow layouts using test data without submitting a production task.

### Task 3: Align planner workflow with a single final review

Own harness assets and directly necessary consuming contract tests only. Coordinate before touching server files already changed by Task 1. Do not introduce runtime filtering without evidence of the originating event type.

1. Read the mandatory skill/plugin documents, inspect canonical creation-planner Pack sources and their generated Claude/Codex/DSH distribution, and identify the actual editable skill source.
2. Make explicit current user intent take precedence over historical example topics. Ask only for genuinely missing choices; inherit stable profile constraints and avoid repeating already answered decisions.
3. Once enough information exists, stage payload.plan for review (ready=true requests Server readiness). Do not first ask a separate full-plan confirmation using AskUserQuestion. Only Studio's 开始创作 action creates the task; staging and model prose cannot grant that authorization.
4. After successful staging respond with one short handoff sentence, not the same complete summary or raw draft metadata. On field validation errors use the field-level diagnostic for one focused correction; preserve the confirmed requirements, and report one concise blocker if still rejected. Do not expose internal reasoning/debug guesses. Show a short valid payload example aligned with the real Server contract and state that missing opaque identity metadata is supplied by authenticated context.
5. Verify creation-planning is actually included and discoverable in the built DSH preset. Reproduce any discovered packaging defect before fixing it. The production model's claim of a Skill-loading failure is not itself evidence. Do not add speculative adapters or content-language filters.
6. Generate distribution assets with documented commands, bump both plugin manifests by the same patch version, run relevant harness generation/package checks and MCP contract tests. Tests should exercise validation or packaging behavior; do not add assertions that merely grep new instruction prose. Use a fresh agent walkthrough with the revised instructions to check the one-review handoff if a live provider test is unavailable, and label the limits honestly.
7. Commit harness changes on a local codex/assistant-creation-flow branch, then commit the parent gitlink plus any directly relevant tests/docs. No push.

Deliver task-3-report.md with both commit IDs, commands/results, and any unverified live-provider behavior.

## Final integration

- Inspect the full diff and task reports, then obtain an independent whole-branch review. Fix actionable findings and verify the changed surfaces again only where fixes or new concerns require it.
- Run remaining required full verification on the integrated result, including Go tests/build and Studio tests/build if previous runs predate cross-surface changes. Test changed runtime-dsh code if any.
- Exercise desktop and narrow layouts in the browser with a deterministic mock API, including ready, edit, answered history, malformed stage diagnostics, and task handoff. Save screenshots and remove temporary mocks/servers when finished.
- Leave a reviewable local branch/worktree and a concise result with tests and remaining rollout/evidence limitations. No production claim without a deployment and actual production verification.
