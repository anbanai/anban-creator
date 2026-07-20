# Agent Runtime Billing and Failure Artifact Collection Design

Date: 2026-07-15

## Summary

Task execution has two independent accounting responsibilities:

1. Charge the fixed task service fee when the task is accepted, then charge the actual Claude Code runtime usage after execution.
2. Collect every eligible file produced by the managed Agent, regardless of whether the execution succeeds or fails.

The current code already reports Claude Code usage and runs workspace artifact upload after `Runner.Run` returns. The bugs are downstream policy choices: runtime settlement intentionally ignores the reported usage, and cloud finalization discards the uploaded manifest when the execution fails.

This design restores actual runtime settlement without restoring an upfront runtime reserve. It also introduces an attempt-scoped `collected` artifact state so failed-attempt files remain visible and downloadable without replacing a successful delivery set.

## Goals

- Keep the fixed task service fee and charge actual Claude Code runtime usage separately.
- Use Claude Code's reported cost and token usage instead of a fixed runtime charge.
- Make runtime settlement idempotent across cloud, local, retry, and finalizer replay paths.
- Charge consumed Claude Code runtime even when the task outcome is failed.
- Run artifact collection after every managed Agent outcome that returns control to the Runner.
- Preserve all eligible files from failed attempts, including `failure-state.json`.
- Keep failed-attempt files out of completion validation, automatic publishing, and the current successful delivery set.
- Preserve previous successful task files when a later attempt fails.

## Non-Goals

- Reintroducing an upfront Claude Code runtime reserve.
- Blocking an already accepted task because its final runtime charge exceeds the remaining balance.
- Charging fabricated runtime usage when Claude Code did not return cost or token evidence.
- Uploading runtime internals, dotfiles, dependency directories, or files already excluded by task-specific collection rules.
- Replacing the managed Runner collection phase with a Claude Code `Stop` Hook.
- Treating partial failed-attempt files as successful publishable deliverables.

## Root Causes

### Runtime billing

`ExecutionResult` already carries `total_cost_usd` and input, output, cache-read, and cache-creation token counts. All terminal execution paths call `SettleAgentRuntime`, but that method currently ignores the result and only clears legacy billing locks. Tests explicitly enforce the platform-paid behavior, so the fixed-only charge is intentional current behavior rather than missing data propagation.

### Failure artifacts

The Agent's post-run artifact phase scans `output/`, uploads matching files to OSS, and submits an execution manifest whether `ExecutionResult.Success` is true or false. Cloud files remain `pending` until durable finalization. The finalizer publishes the manifest for a successful execution and calls `DiscardCurrentExecution` for every failed execution. Discard changes all pending rows to `superseded`, so `failure-state.json` is used to fail validation and then immediately made invisible.

## Runtime Billing Design

### Charging model

Task creation continues to deduct only the configured fixed service fee. No runtime reserve is required.

At terminal settlement, `SettleAgentRuntime` calculates the actual Claude Code runtime credits and records a separate `agent_runtime` transaction:

- Operation ID: `agent_runtime:<task_id>`
- Task ID: the owning task
- Type: `agent_runtime`
- Amount: negative actual runtime credits
- Description: Claude Code runtime usage charge

The operation ID makes settlement idempotent when the same result is processed by progress reporting, local completion, cloud completion, or durable finalizer replay.

### Usage source

Settlement uses the strongest available evidence in this order:

1. Positive `total_cost_usd` from the Claude Code result. This is the authoritative provider-calculated cost for the token usage, including cache pricing.
2. Reported token usage calculated against the configured Anthropic/Claude token model price. The fallback includes input, output, cache-read, and cache-creation tokens.

The transaction metadata records provider, model, session ID, turns, API duration, all token categories, total cost in USD when present, billing source, currency conversion, tier multiplier, user multiplier, final credits, and the price snapshot used for the calculation.

If neither cost nor positive token usage is present, settlement emits a structured warning and creates no runtime charge. It must not invent a fixed runtime cost or block terminal task finalization.

### Balance and failure semantics

Runtime settlement uses the same post-acceptance overdraft policy as MCP operations. The exact charge is persisted even if it makes the user's balance negative.

Actual runtime usage is charged for both successful and failed executions because the provider cost has already occurred. Existing task refund behavior remains scoped to the fixed task service fee:

- Successful task: fixed service fee plus actual runtime fee remain charged.
- Normal failed or cancelled task: existing policy may refund the fixed service fee; the runtime fee remains charged.
- Goal-mode behavior remains governed by its current fixed-fee refund rules; runtime usage remains charged.

Settlement and fixed-fee refund transactions remain independently idempotent.

### Configuration

`CreditService` retains access to the full server configuration so it can use model prices, currency rates, credits per CNY, minimum charge, tier multipliers, and user multipliers. `agent_runtime_reserve` remains legacy-only and is not restored as an active configuration requirement.

## Artifact Collection Design

### Lifecycle hook

Artifact collection stays in the managed Runner's unconditional post-run finalization phase. This is the reliable after-run lifecycle hook for Anban tasks: it runs after success, recoverable failure, maximum-turn failure, and protocol-level unsuccessful results whenever the Runner regains control.

A Claude Code `Stop` Hook is not used for uploading. Stop hooks can block or repeat normal model termination and are not guaranteed for every process or protocol failure. The existing Runner phase also owns the server credentials, execution identity, bounded finalization context, OSS preparation, upload, and manifest reporting.

The collection phase continues to:

1. Select the result work directory.
2. Prefer and require a real `output/` directory for Kubernetes jobs.
3. Apply existing directory, file, and task-type filters.
4. Upload every selected regular file to execution-scoped OSS storage.
5. Submit one manifest for the execution.
6. Run independently of `ExecutionResult.Success`.

An upload failure remains an execution failure because files cannot be claimed as collected without a durable manifest.

### Artifact states

Add `collected` to the task file state model:

- `pending`: uploaded for the current running execution but not finalized.
- `published`: current successful delivery files.
- `collected`: durable files from a failed, cancelled, or timed-out execution.
- `superseded`: files no longer part of a visible or current set.

Add the corresponding `collected` task execution manifest status. Database check constraints and migration tests must accept the new values on existing and fresh databases.

### Finalization

Cloud finalization selects the artifact transition from the terminal execution status:

- Succeeded: `PublishCurrentExecution` changes this attempt's pending files to `published` and supersedes the previous published delivery set.
- Failed, cancelled, or timed out: `CollectCurrentExecution` changes this attempt's pending files to `collected` and leaves the existing published delivery set unchanged.

`CollectCurrentExecution` uses the same task-then-execution lock order and current-execution guard as publish/discard. It is atomic and idempotent, resolves every pending row, and records the manifest as collected. A stale attempt cannot collect files into another task attempt.

Pre-start failures with no uploaded files remain valid terminal executions; finalization records the manifest outcome without fabricating task files.

### Read boundaries

Internal delivery consumers continue to read only `published` files. This includes:

- completion and artifact validation;
- workflow-status reconstruction;
- automatic publishing and draft extraction;
- video production state;
- task resumption inputs unless explicitly selected by the user;
- successful-delivery ZIP and bulk export.

User-facing task file inspection reads both `published` and `collected` files. The API retains `execution_id` and `state` so the Studio can distinguish current deliverables from failed-attempt artifacts. A collected file can be downloaded by ID after the normal task ownership check. Collected files do not satisfy successful-delivery checks.

This separation guarantees that a failed retry can expose `failure-state.json`, `image-review.md`, and partial outputs without replacing or contaminating a previous successful result.

### Local and non-job execution

Local/direct task artifact uploads without an execution-scoped pending manifest remain immediately visible under their existing behavior. Server-hosted workspace collection also continues to run before terminal status handling. The new `collected` transition is required for execution-scoped cloud manifests, where the current pending/discard policy causes the observed data loss.

## Error Handling

- Runtime calculation errors caused by invalid configured pricing are returned from settlement so durable cloud finalization can retry rather than silently undercharge.
- Missing runtime usage evidence is not a pricing error; it is logged and skipped without inventing cost.
- Duplicate runtime settlement returns the existing outcome without changing the balance again.
- Artifact upload or manifest persistence failure is reported as an artifact failure and does not mark files collected.
- Artifact collect/publish state conflicts fail the durable finalization stage and are retried under the existing finalization lease.
- File download continues to require task ownership and storage access checks for both published and collected files.

## Testing Strategy

### Runtime billing tests

- Task creation deducts the fixed service fee only.
- Settlement from `total_cost_usd` deducts the calculated actual credits and stores complete metadata.
- Settlement falls back to input/output/cache token pricing when total cost is absent.
- Settlement permits a negative balance.
- Duplicate settlement does not double-charge.
- Failed execution usage is charged while the fixed service fee refund remains independent.
- Missing usage creates no fabricated charge and does not block finalization.
- Cloud and local completion paths converge on the same idempotent settlement.

### Artifact tests

- The Runner uploads `failure-state.json` and other output files for an unsuccessful result.
- Failed execution collection changes pending rows to collected and preserves existing published rows.
- Successful execution publication continues to replace the published set.
- Collection is idempotent and rejects stale execution identities.
- Publish/collect races leave no pending rows and preserve one valid manifest outcome.
- Cloud completion retains files for failed, cancelled, and timed-out executions.
- User-facing file listing and single-file download include collected files.
- Workflow validation, publishing, and delivery export ignore collected files.
- Schema migration accepts the collected task-file and manifest states.

### Verification

- Run targeted Agent, repository, service, handler, and billing tests.
- Run `go test ./...`.
- Build the server and Agent binaries with explicit `/tmp` output paths.
- Run Studio tests/build only if the implementation changes Studio rendering or TypeScript contracts.

## Rollout

The change is backward compatible for existing rows:

- Existing `published`, `pending`, and `superseded` task files retain their meanings.
- Existing `discarded` execution manifests remain hidden audit history.
- New failed execution manifests use `collected` after deployment.
- Existing legacy runtime reserve rows remain governed by the current legacy refund helpers; new tasks do not create reserves.

No plugin distribution asset changes are required, so plugin manifest versions do not change.
