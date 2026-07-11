# Task Execution Nil Result Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent `content:generate` tasks from panicking when an executor returns no result.

**Architecture:** Keep `ExecutionResult` optional during post-execution finalization. Guard partial-result processing, preserve cancellation precedence, then convert executor errors and missing-result contract violations into the existing terminal failure path.

**Tech Stack:** Go, GORM test repository, standard `testing` package

---

### Task 1: Reproduce Nil Executor Results

**Files:**
- Test: `server/service/task_execution_decouple_test.go`

- [x] Add a table-driven test using `fakeTaskExecutor` for `(nil, errors.New("executor setup failed"))` and `(nil, nil)`.
- [x] Assert `HandleExecution` does not panic or return an orchestration error.
- [x] Assert the persisted task is `failed`, has `completed_at`, and contains the original or synthesized error.
- [x] Run `go test ./server/service -run TestHandleExecutionNilResult -count=1` and confirm it fails with the current nil-pointer panic.

### Task 2: Make Result Handling Nil-Safe

**Files:**
- Modify: `server/service/task_execution.go`

- [x] Guard workspace and remote-artifact processing with `result != nil`.
- [x] Keep cancellation handling before ordinary executor failure handling.
- [x] Normalize `(nil, nil)` to `executor returned nil result without error`, then route it and ordinary executor errors through one failure branch.
- [x] Run `go test ./server/service -run TestHandleExecutionNilResult -count=1` and confirm both cases pass.

### Task 3: Verify the Changed Surface

**Files:**
- Verify: `server/service/task_execution.go`
- Verify: `server/service/task_execution_decouple_test.go`

- [x] Run `gofmt` on the changed Go files.
- [x] Run `go test ./server/service -count=1`.
- [x] Run `go test ./... -count=1`.
- [x] Run `go build -o /tmp/anban-creator-server ./server`.
- [x] Run `go build -o /tmp/anban ./agent`.

### Task 4: Lock Preservation Contracts And Merge Hygiene

**Files:**
- Modify: `server/service/task_execution_decouple_test.go`
- Modify: `.gitignore`

- [x] Assert cancellation wins when an executor returns a nil result, with and without an executor error.
- [x] Assert a partial execution result and workspace artifact are persisted before the task is failed.
- [x] Ignore the obsolete local `/wcflink/` checkout, which was intentionally removed as a submodule in commit `0381192`.
- [x] Run the targeted contract tests and `go test -race` for the nil-result regression.
