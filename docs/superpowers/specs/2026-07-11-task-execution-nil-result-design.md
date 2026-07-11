# Task Execution Nil Result Design

## Problem

Agent executors may return `(nil, err)` when setup fails before an execution
result exists. `TaskService.HandleExecution` previously read `result.WorkDir`
before handling `execErr`, causing a nil-pointer panic and leaving the task in
`running` state.

## Design

Treat the execution result as optional until the executor error has been
handled. Persist and finalize partial results when present, but guard all
artifact work behind `result != nil`. Preserve cancellation precedence, then
route executor errors through `HandleExecutionFailure`. If an executor violates
its contract by returning `(nil, nil)`, convert that state into an explicit
terminal failure instead of panicking.

## Verification

Add table-driven regression coverage for `(nil, err)` and `(nil, nil)`. Both
cases must return without panic, move the task from `running` to `failed`, set
`completed_at`, and persist an actionable error message. Contract tests also
lock cancellation precedence and partial-result persistence. Run the service
tests, the full Go suite, and both production binary builds.
