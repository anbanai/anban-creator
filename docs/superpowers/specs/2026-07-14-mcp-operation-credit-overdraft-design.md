# MCP operation credit overdraft design

## Goal

Keep credit enforcement out of Agent and Skill business workflows. A task must
have enough credits to be created, but once creation succeeds, its operation
charges must not interrupt execution when the balance becomes negative.

## Billing boundary

- Task creation remains fail-closed. Single, batch, scheduled, and goal-mode
  task creation continue to return `ErrInsufficientCredits` when the configured
  task fee cannot be deducted.
- Every MCP operation charge may make the user's balance negative. Operation
  billing records usage; it does not authorize or interrupt an accepted task.
- Operation idempotency, transaction records, metadata, refunds, and task credit
  summaries keep their current behavior.
- An optional `task_id` remains transaction attribution metadata. Existing MCP
  request ownership validation remains where currently required, but credit
  deduction itself does not add a balance or ownership gate.

## Agent and Skill contract

Agent and Skill files must not query credit balance, preflight operation cost,
stop because the remaining balance cannot cover an operation, or ask the user
to recharge during an accepted task. Pricing estimates may remain when they are
part of the user-facing video plan, but they must be informational and must not
control workflow continuation.

The mirrored `claudecode`, `openclaw`, and `codex` distributions must describe
the same behavior. Their affected plugin manifests receive patch version bumps.

## Server behavior

MCP billing uses the explicit `DeductForMCPOperation*` methods, which perform an
atomic balance adjustment that permits a negative result and create the existing
operation transaction in the same database transaction. The shared
`DeductForOperation*` methods remain fail-closed for non-MCP surfaces such as
interactive Designer generation. Task-creation methods also continue using the
conditional deduction that rejects insufficient credits.

No Agent-side call to `get_credit_balance` is added. The MCP tool may remain
available for non-workflow account surfaces, but task workflows do not use it.

## Verification

- Add service tests proving task creation still rejects insufficient credits.
- Add service tests proving task-scoped and unscoped operations can overdraw and
  record the negative `balance_after`.
- Add contract tests that reject recharge or insufficient-balance flow control
  in Agent and Skill distributions.
- Run targeted service and agent contract tests, then `go test ./...`.
