# Creation Workflow v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add staged workflow metadata and review summaries to generated content tasks so Studio can show an editorial workflow instead of only raw files/logs.

**Architecture:** Add a small workflow domain model under `server/model`, a pure builder under `server/service`, and persist the generated workflow JSON on `tasks.workflow_status`. The backend classifies uploaded task files after workspace upload and exposes workflow metadata through existing task APIs; the frontend renders stages and review readiness using the new task field.

**Tech Stack:** Go 1.26, GORM, Fiber, React 19, TypeScript, React Query, Vitest.

---

### Task 1: Backend Workflow Model And Builder

**Files:**
- Create: `server/service/workflow_status.go`
- Test: `server/service/workflow_status_test.go`
- Modify: `server/model/constants.go`
- Modify: `server/model/task.go`

- [ ] **Step 1: Write failing tests for artifact classification and workflow building**

Create `server/service/workflow_status_test.go` with tests that call `DetermineWorkflowArtifactRole` and `BuildWorkflowStatus`.

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test ./server/service -run 'TestDetermineWorkflowArtifactRole|TestBuildWorkflowStatus'`

Expected: FAIL because the functions and model types do not exist.

- [ ] **Step 3: Implement minimal workflow model and builder**

Add file roles to `server/model/constants.go`, add `WorkflowStatus *string` to `server/model/task.go`, and implement `server/service/workflow_status.go`.

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./server/service -run 'TestDetermineWorkflowArtifactRole|TestBuildWorkflowStatus'`

Expected: PASS.

### Task 2: Persist Workflow Status After Task Execution

**Files:**
- Modify: `server/repository/repository.go`
- Modify: `server/repository/task.go`
- Modify: `server/service/task_execution.go`
- Test: `server/service/task_test.go`

- [ ] **Step 1: Write failing repository/service test**

Add a test proving that workflow status can be persisted from uploaded task files.

- [ ] **Step 2: Run focused Go test and verify failure**

Run: `go test ./server/service -run TestTaskService_RebuildWorkflowStatus`

Expected: FAIL because the service method does not exist.

- [ ] **Step 3: Add repository update method and service integration**

Add `UpdateWorkflowStatus(ctx, id, workflowStatus string) error` to `TaskRepository`, implement it, and call `RebuildWorkflowStatus` after `uploadMissingTaskFiles` in `HandleExecution`.

- [ ] **Step 4: Run focused Go test and verify pass**

Run: `go test ./server/service -run TestTaskService_RebuildWorkflowStatus`

Expected: PASS.

### Task 3: Frontend Types And Workflow Panel

**Files:**
- Modify: `studio/src/types/task.ts`
- Create: `studio/src/components/TaskWorkflowPanel.tsx`
- Test: `studio/src/components/TaskWorkflowPanel.test.tsx`
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Modify: `studio/src/pages/TasksPage.tsx`

- [ ] **Step 1: Write failing component tests**

Create tests for rendering stage labels, readiness score, warnings, and empty state.

- [ ] **Step 2: Run Vitest and verify failure**

Run: `npm test -- TaskWorkflowPanel.test.tsx --run`

Expected: FAIL because the component does not exist.

- [ ] **Step 3: Implement frontend types and panel**

Add workflow interfaces to `Task`, implement `TaskWorkflowPanel`, render it in task detail, and add readiness badge in task list.

- [ ] **Step 4: Run focused frontend test and verify pass**

Run: `npm test -- TaskWorkflowPanel.test.tsx --run`

Expected: PASS.

### Task 4: Verification And Docs Alignment

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README to Studio-first positioning**

Replace CLI-first sections with Studio/server-first commands and describe creation workflow v1.

- [ ] **Step 2: Run full verification**

Run:

```bash
go test ./...
cd studio && npm test -- --run
cd studio && npm run build
```

Expected: all pass.

- [ ] **Step 3: Commit**

Run:

```bash
git add server studio README.md docs/superpowers/plans/2026-05-01-creation-workflow-v1.md
git commit -m "feat: add creation workflow status"
```
