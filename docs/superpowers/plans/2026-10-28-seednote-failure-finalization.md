# Seednote Failure Finalization Implementation Plan

> **For agentic workers:** Execute task-by-task with test-driven development. Server protocol diagnostics can run independently; Runtime and Harness stopping contracts are integrated together.

**Goal:** End a Seednote execution promptly and honestly when image review is unavailable, retaining generated artifacts and reporting a failed outcome.

**Architecture:** Keep execution completion in the Runtime/Server. A validated failure artifact permits stopping but never passes successful delivery validation. Harness retains creative retry decisions with finite budgets; MCP remains an atomic transport.

**Tech Stack:** TypeScript/Bun Claude Runtime, Go/OpenAI-compatible image understanding, generated Claude/Codex/DSH Agent Packs.

**Spec:** `docs/superpowers/specs/2026-10-28-seednote-stuck-investigation.md`.

## Constraints

- Work only on `codex/seednote-failure-finalization` in an isolated worktree; preserve unrelated work.
- No production route changes without actual upstream response evidence; no task restart or deployment.
- Never promote workspace failure evidence to a trusted platform error or waive quality validation for success.
- Keep initial failure-stop change scoped to Seednote; preserve unrelated Agent stopping behavior.
- Use Pack sources and generators; bump both plugin manifests and affected Pack version.

## Task 1: Runtime failure stop and finalization

Files: `runtime-claude/src/runner.ts`, `main.ts`, new `failure-state.ts`; tests in `test/runner.test.ts`, `test/main.test.ts`.

- [x] Reproduce using a valid failure record with version `1.0`, status `recoverable_failure`, stage/resume `image_generation`, code `image_review_unavailable`, nonempty message. Invoke the real composed Seednote Stop hook; assert no block, no success metadata, then run finalization and assert failure plus retained artifact manifest. Watch this fail before editing implementation.
- [x] Test malformed, missing-field, oversized, file/output symlink records remain blocked and cannot bypass successful delivery checks.
- [x] Extract the existing strict failure reader for reuse; validate the output boundary and read a bounded regular file. Use it for both Seednote stop decisions and `applyFailureState` while preserving trusted identity classification.
- [x] Keep success validation strict. Skip success-only completion metadata on validated failed Seednote stops; let existing upload/report path persist the failed result.
- [x] Run focused Runtime tests, then all Runtime tests, typecheck and build.

## Task 2: Image understanding protocol and deadline

Files: `server/service/model_client.go`, `model_client_test.go`, `task_image_operations.go` and relevant tests.

- [x] Add `httptest` cases for 200 HTML, redirected HTML, JSON success, HTTP auth/rate/server failures, and shorter/longer parent deadline. Require safe protocol diagnostics with no body, key, image bytes, query or URL userinfo exposure. Watch regressions fail.
- [x] Classify non-JSON image understanding replies as `provider_protocol_error`, preserving distinguishable HTTP error outcomes and logging safe upstream metadata in the service.
- [x] Bound image understanding by the earlier parent/route deadline. Keep ordinary text/video behavior outside this change.
- [x] Run targeted Go tests, full Go suite and Server build.

## Task 3: Harness retry/progress contract

Files: Seednote Pack Agent sources, `hooks/completion-metadata.sh`, generated distributions/catalog, plugin manifests, and version/native-Hook regression tests.

- [x] Read `harness/docs/plugin-development.md` and the reference contract. Add precise guidance: output review at most three attempts including initial call, no retries for same protocol/identity fault, no input variants/sleep after terminal failure; preserve original images for recovery.
- [x] State in the owning Agent that failed review/delivery stages remain active; finish required per-image evidence, then report an overall failure and stop. An old failure record must not abort recovery. Skills do not own lifecycle changes.
- [x] Run the real Seednote quality gate with valid failure artifacts to prove it allows failed exit, while normal success checks remain strict; regenerate Pack distributions and verify clean generation.
- [x] Bump manifests/Pack and run Pack checks, relevant MCP contract tests and full Go suite. Review generated drift and commit the Harness change before recording its gitlink in the parent.

## Final verification

- [x] Review all changes against the spec; no unrelated formatting or framework additions.
- [x] Record exact tests and any environment limitation. Commit a reviewable branch; do not deploy or merge.


## Implementation record — 2026-10-28

- Parent base: `ad854ca1a005423d423a48c6f505ebaabe8b4864`; Harness base: `3743ef56a507620efe32f20acd61c1f9e8ae3b44`. Both use branch `codex/seednote-failure-finalization` in the isolated worktree. The investigation document records the earlier checkout observed during diagnosis.
- Failure-state reading is shared by Stop and finalization. A bounded, validated regular file allows failed Seednote termination; finalization still downgrades an SDK success to workflow failure, preserves artifacts, and retains the trusted platform-error boundary.
- Review caught and corrected the separate native completion Hook, stale success metadata submission, an internal `output` directory symlink, and ambiguous recovery wording. Regression tests failed before the corresponding fixes. The native Hook suppresses success metadata while any Seednote failure record exists; Runtime never submits completion metadata for an unsuccessful Seednote result.
- Retry guidance stays in the owning Agent, alongside its existing per-image/overall quality policy, rather than duplicating workflow decisions in the visual Skill. The three native Agent sources and generated distributions agree. Each output image allows at most three review calls for transient failures; protocol and identity faults stop retries. Remaining image evidence and subsequent recovery remain possible.
- Image understanding now exposes a stable safe error code and HTTP metadata, logs host/path only on the Server, and uses the earlier parent/route deadline. It never includes response HTML, image data, credentials, query strings, or internal endpoints in the Agent-facing error. Text/video calls and MCP workflow ownership are unchanged.
- Plugin version: `4.2.98`; Seednote Pack: `2.0.11`. Version assertions are aligned, including two stale Harness assertions that still expected `4.2.94` on its `4.2.97` base.

## Verification results

- Runtime focused suite: `bun test test/main.test.ts test/runner.test.ts` — **98 passed**. Tests cover failed Stop, retained manifest, SDK success downgrade, stale completion metadata, malformed/missing/oversized/symlink failure records, and existing success behavior.
- Runtime typecheck and build passed under Node `24.22.0`. Final full suite: **256 passed, 1 failed** out of 257. The unchanged Reporter test `aborts structured progress during retry backoff` expects one attempt after a 10 ms sleep, while production retry jitter ranges from 0–249 ms; it intermittently sees two. Its isolated suite then passed **9/9**. An earlier parallel full run also timed out in two unchanged Hypit dependency-install tests; both passed in subsequent full runs. These unrelated tests were not modified. Logs: `/tmp/anban-seednote-runtime-verified.log`, `/tmp/anban-seednote-reporter-isolated.log`.
- `go test ./agent -run TestSeednoteNativeCompletionHookSkipsFailedDelivery -count=1` passed against the real Hook, covering failed exit and recovery. Agent and Agent Pack packages also passed in the full Go run.
- Focused service tests `Test(ChatCompletionUsage|ImageUnderstanding|TaskImage)` passed. HTML, redirect, malformed JSON, API status, secret redaction, structured diagnostics, and earlier parent/route deadlines are covered. Relevant MCP capability-boundary and Seednote tests passed.
- `go test ./...` completed with **12 existing WeChat publication service failures**; all other packages passed. Replacing all four changed service files with `ad854ca1` versions through `go -overlay` reproduced the same 12 tests and 15 assertion failures (after UUID/timestamp normalization). The final full run has exactly the same failure-name set. Logs: `/tmp/anban-seednote-go-full-final.log`, `/tmp/anban-seednote-service-baseline.log`; overlay: `/tmp/anban-seednote-service-baseline-overlay.json`.
- `go build -o /tmp/anban-seednote-creator-server .` passed.
- Harness audit, typecheck, build, and tests passed: **476 passed, 1 skipped**. Packed artifact verification and DSH profile smoke passed. The first pack check failed because the local pnpm cache lacked `@deepseek-ai/dsh-commands` metadata; an isolated temporary install populated the cache, after which the unchanged check passed. No dependency declarations were changed for this environment issue.
- `make agent-pack-check` passed; generated catalog digest is `3b8551768e539277f1d5c44d6e142d368b8c7aeb8933cbf9a5a602319e42ebda`. Parent and Harness `git diff --check` passed. Independent code review found no remaining important issue after the three corrections above.
- Installed Claude CLI plugin validation rejects existing `displayName` / marketplace `description` fields on both current and baseline manifests. This pre-existing CLI/schema compatibility issue was not folded into the fix.

No production configuration, running task, billing, or deployment was changed. The original source of the HTML response remains unconfirmed; the new Server diagnostics support that investigation after deployment. No paid model end-to-end task was run. Changes are committed locally in the Harness first and then the parent repository; no merge or push is part of this work.
