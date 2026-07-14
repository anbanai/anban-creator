# Task-scoped credit overdraft design

## Goal

Keep credit enforcement out of Agent and Skill business workflows. A task must
have enough credits to be created, but once creation succeeds, its operation
charges must not interrupt execution when the balance becomes negative.

## Billing boundary

- Task creation remains fail-closed. Single, batch, scheduled, and goal-mode
  task creation continue to return `ErrInsufficientCredits` when the configured
  task fee cannot be deducted.
- An operation charge with a non-empty `task_id` may make the user's balance
  negative after the server verifies that the task belongs to that user.
- An operation charge without `task_id` remains fail-closed and cannot overdraw.
- Operation idempotency, transaction records, metadata, refunds, and task credit
  summaries keep their current behavior.
- A missing, unknown, or foreign `task_id` is rejected before any balance change.

## Agent and Skill contract

Agent and Skill files must not query credit balance, preflight operation cost,
stop because the remaining balance cannot cover an operation, or ask the user
to recharge during an accepted task. Pricing estimates may remain when they are
part of the user-facing video plan, but they must be informational and must not
control workflow continuation.

The mirrored `claudecode`, `openclaw`, and `codex` distributions must describe
the same behavior. Their affected plugin manifests receive patch version bumps.

## Server behavior

`CreditService.deductForOperation` determines whether the call is task-scoped
from its optional `task_id`. For task-scoped calls it validates ownership and
uses an atomic balance adjustment that permits a negative result. For unscoped
calls it keeps the existing conditional deduction that rejects insufficient
credits. Both paths create the existing operation transaction in the same
database transaction.

No Agent-side call to `get_credit_balance` is added. The MCP tool may remain
available for non-workflow account surfaces, but task workflows do not use it.

## Verification

- Add service tests proving task creation still rejects insufficient credits.
- Add service tests proving an owned task operation can overdraw and records the
  negative `balance_after` and `task_id`.
- Add service tests proving an unscoped operation still rejects overdraft.
- Add service tests proving a foreign task cannot trigger overdraft.
- Add contract tests that reject recharge or insufficient-balance flow control
  in Agent and Skill distributions.
- Run targeted service and agent contract tests, then `go test ./...`.
