# Task 8 Prepared Runtime Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cleanup durably recover and exactly delete a prepared Docker container or Kubernetes Job when the process stops before dispatch identity persistence.

**Architecture:** Add a provider-neutral, lookup-only `ResolvePrepared` operation that reconstructs the deterministic desired workload, verifies its ownership and specification, and returns its exact instance ID or UID. Cleanup claims remain the authority boundary: a cleanup-token-fenced repository write binds the recovered identity, then callers reload and perform exact deletion. The database clock is authoritative for retained dispatch-claim freshness, while each provider `Prepare` call receives a relative timeout strictly shorter than the database lease. Cleanup checks the database barrier before provider lookup so a transient not-found result cannot complete cleanup while an earlier `Prepare` call can still create the workload.

**Tech Stack:** Go, GORM, Docker Engine API, Kubernetes client-go fake client, table-driven tests.

---

### Task 1: Cleanup-Fenced Identity Persistence

**Files:**
- Modify: `server/repository/repository.go`
- Modify: `server/repository/task_execution.go`
- Test: `server/repository/task_execution_test.go`

- [ ] **Step 1: Write failing repository tests**

Add tests proving a terminal execution with `cleanup_status=pending` can bind a complete recovered identity only under the current cleanup token, is idempotent for the same identity, rejects drift, and rejects stale tokens without changing persisted fields.

- [ ] **Step 2: Run the repository tests and verify RED**

Run: `go test ./server/repository -run 'TestTaskExecutionSetCleanupRuntimeIdentity' -count=1`

Expected: build failure because `SetCleanupRuntimeIdentity` does not exist.

- [ ] **Step 3: Implement the fenced repository method**

Add `SetCleanupRuntimeIdentity(ctx, id, token, identity) (bool, error)` to `TaskExecutionRepository`. Update only rows with `cleanup_status=pending` and the matching `cleanup_token`; reuse non-empty-member conflict predicates and return `ErrRuntimeIdentityConflict` for a matching lease whose persisted identity drifts.

- [ ] **Step 4: Run the repository tests and verify GREEN**

Run: `go test ./server/repository -run 'TestTaskExecutionSetCleanupRuntimeIdentity' -count=1`

Expected: PASS.

### Task 2: Lookup-Only Provider Recovery

**Files:**
- Modify: `server/agent/runtime_dispatcher.go`
- Modify: `server/agent/docker_dispatcher.go`
- Modify: `server/agent/kubernetes_dispatcher.go`
- Test: `server/agent/docker_dispatcher_test.go`
- Test: `server/agent/kubernetes_executor_test.go`
- Test: `server/agent/runtime_dispatcher_test.go`

- [ ] **Step 1: Write failing Docker and Kubernetes recovery tests**

Cover successful exact ID/UID recovery, not-found classification, replacement ID/UID rejection, spec/ownership mismatch rejection, and absence of create/start/update calls.

- [ ] **Step 2: Run focused dispatcher tests and verify RED**

Run: `go test ./server/agent -run 'Test(Docker|Kubernetes).*ResolvePrepared' -count=1`

Expected: build failure because `ResolvePrepared` does not exist.

- [ ] **Step 3: Implement `ResolvePrepared`**

Extend `RuntimeDispatcher` with `ResolvePrepared(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error)`. Docker performs image and container inspection, rebuilds the desired spec, and verifies the existing container before returning its exact ID. Kubernetes gets the deterministic Job, verifies the complete desired Job and ownership labels, and returns the Job UID. Neither implementation creates, starts, unsuspends, updates, or deletes anything.

- [ ] **Step 4: Run focused dispatcher tests and verify GREEN**

Run: `go test ./server/agent -run 'Test(Docker|Kubernetes).*ResolvePrepared' -count=1`

Expected: PASS.

### Task 3: Database-Authoritative Dispatch-Creation Barrier

**Files:**
- Modify: `server/service/task_dispatch.go`
- Test: `server/service/task_dispatch_test.go`

- [ ] **Step 1: Write failing dispatch timeout and database-clock tests**

Prove `Prepare` receives a relative timeout strictly shorter than the dispatch lease, and prove claim freshness is determined entirely by database time rather than the application host clock.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `go test ./server/repository ./server/service -run 'Test.*Dispatch.*(DatabaseClock|RelativeTimeout)' -count=1`

Expected: FAIL because the current request context is passed directly and cleanup compares the persisted timestamp with application time.

- [ ] **Step 3: Bound preparation to the durable claim**

Refresh the exact-token claim with database time, reload and verify ownership, then call `Prepare` with a relative timeout shorter than the lease. All claim-freshness predicates use database time. Ambiguous provider errors and terminal dispatch failures retain the claim fields until cleanup so authoritative absence cannot be inferred during an in-flight creation window.

- [ ] **Step 4: Run the focused test and verify GREEN**

Run: `go test ./server/repository ./server/service -run 'Test.*Dispatch.*(DatabaseClock|RelativeTimeout)' -count=1`

Expected: PASS.

### Task 4: Shared Recover-Persist-Reload Cleanup

**Files:**
- Modify: `server/service/task_execution_reconcile.go`
- Modify: `server/service/task_execution_complete.go`
- Modify: `server/agent/runtime_reconciler.go`
- Test: `server/service/task_execution_complete_test.go`
- Test: `server/agent/runtime_reconciler_test.go`

- [ ] **Step 1: Write failing cleanup recovery tests**

Cover cancellation and active-deadline terminalization with empty identity, recovery and durable persistence before delete, recovery failure, persistence failure, exact replacement rejection, and lookup-only behavior. Add a deterministic race where recovery returns not found while a claimed `Prepare` remains blocked; cleanup must remain pending and must not be marked done.

- [ ] **Step 2: Run focused cleanup tests and verify RED**

Run: `go test ./server/service ./server/agent -run 'Test.*Cleanup.*Recover|Test.*Prepared.*Race|Test.*ActiveDeadline.*Prepared' -count=1`

Expected: FAIL because cleanup deletes the stale empty execution directly.

- [ ] **Step 3: Implement the shared cleanup preparation method**

Add a TaskService method used through `RuntimeReconcileService` that validates the current cleanup token, checks the database-authoritative dispatch barrier before provider lookup, reloads the Task, calls `ResolvePrepared` only for incomplete identity after the barrier expires, persists recovered identity with `SetCleanupRuntimeIdentity`, and reloads the authoritative execution. Return whether a workload exists so authoritative absence can complete cleanup without calling `Delete` on an empty identity.

- [ ] **Step 4: Route both cleanup callers through the method**

Update cancellation cleanup and reconciler cleanup to resolve, persist, and reload before exact `Delete`. On recovery, persistence, or delete errors, call `FailCleanup`; never complete cleanup from a transient not-found result.

- [ ] **Step 5: Run focused cleanup tests and verify GREEN**

Run: `go test ./server/service ./server/agent -run 'Test.*Cleanup.*Recover|Test.*Prepared.*Race|Test.*ActiveDeadline.*Prepared' -count=1`

Expected: PASS.

### Task 5: Verification And Review

**Files:**
- Verify all modified Task 8 files.

- [ ] **Step 1: Format and run focused race tests**

Run: `gofmt -w <modified-go-files>`

Run: `go test -race ./server/repository ./server/service ./server/agent -count=1`

Expected: PASS.

- [ ] **Step 2: Run repository-required verification**

Run: `go test ./... -count=1`

Run: `go vet ./...`

Run: `go build -o /tmp/anban-creator-server ./server`

Run: `go build -o /tmp/anban ./agent`

Run: `git diff --check`

Expected: all commands exit 0.

- [ ] **Step 3: Re-run Task 8 spec and quality review**

Confirm the crash window, cancellation window, active deadline, persistence fence, replacement rejection, and no-create recovery requirements against the final diff.

- [ ] **Step 4: Commit the correction**

Run: `git add <modified-files> && git commit -m 'fix(runtime): recover prepared workload identity during cleanup'`

Expected: a new commit after `71fa8d98` with a clean worktree.
