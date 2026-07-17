# Direct Upload Immutable Promotion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote verified direct uploads into immutable server-only objects before database finalization and persist only those final identities into runtime inputs.

**Architecture:** Add a conditional storage promotion capability, store source and finalized keys separately, prepare all deterministic final objects outside the database transaction, and atomically claim the batch with both identities. Rewrite every direct-upload consumer before persistence and authorize runtime access exclusively through the stored finalized key.

**Tech Stack:** Go 1.26, GORM, Fiber, Alibaba OSS Go SDK, SQLite/MySQL tests.

---

### Task 1: Conditional Storage Promotion

**Files:** `server/storage/storage.go`, `server/storage/local.go`, `server/storage/oss.go`, `server/storage/object_access_test.go`

- [ ] Add failing tests for ETag-conditional promotion, immutable destination bytes, OSS copy headers, and structured 412/409 errors.
- [ ] Run `go test ./server/storage -run 'Test(Local|OSS).*Promot' -count=1` and confirm RED.
- [ ] Add `ConditionalObjectPromoter`, stable promotion errors, local locked implementation, and OSS SDK option implementation.
- [ ] Run the focused storage tests and confirm GREEN.

### Task 2: Persist Source And Final Identity Atomically

**Files:** `server/model/pending_upload.go`, `server/model/model.go`, `server/repository/pending_upload.go`, `server/repository/pending_upload_test.go`

- [ ] Add failing migration and repository tests for `FinalizedKey`, atomic multi-row claims, mismatched final identity rejection, and finalized retry reuse.
- [ ] Run `go test ./server/model ./server/repository -run 'Test.*PendingUpload' -count=1` and confirm RED.
- [ ] Extend the model and claim transaction so source and final keys are matched and persisted together.
- [ ] Run focused model/repository tests and confirm GREEN.

### Task 3: Promotion State Machine

**Files:** `server/service/direct_upload.go`, `server/service/direct_upload_test.go`

- [ ] Add failing tests for source replacement after HEAD, orphan-final retry, source/final asserted-key reuse, old-row rejection, concurrent finalize, and all-or-none claims.
- [ ] Run `go test ./server/service -run 'Test.*DirectUpload.*(Promot|Finaliz|Reuse)' -count=1` and confirm RED.
- [ ] Derive final keys, validate or promote all objects outside the transaction, return only final runtime keys, and submit source/final claims atomically.
- [ ] Run focused service tests, including `go test -race ./server/service -run 'Test.*DirectUpload.*Concurrent' -count=1`, and confirm GREEN.

### Task 4: Rewrite Persisted Consumers

**Files:** `server/handler/upload_finalize.go`, `server/handler/task.go`, `server/handler/plan.go`, `server/handler/project.go`, `server/handler/template.go`, their tests, and current video/montage URL helpers.

- [ ] Add failing endpoint tests proving task, plan, project, template, video, and montage persistence contains no pending namespace.
- [ ] Run the focused handler tests and confirm RED.
- [ ] Return promotion mappings from legacy finalization and rewrite every request field before service persistence.
- [ ] Run focused handler tests and confirm GREEN.

### Task 5: Final-Key-Only Runtime Access And Cleanup

**Files:** `server/service/agent_bootstrap.go`, `server/agent/config_builder.go`, `server/service/media_source.go`, `server/mcp/video_tools.go`, `server/service/direct_upload.go`, and related tests.

- [ ] Add failing tests proving signing, bootstrap authorization, bounded reads, and cleanup use only `FinalizedKey` and never read the pending source.
- [ ] Run affected package tests and confirm RED.
- [ ] Update authorization/materializers and delete both pending and deterministic orphan keys with retryable cleanup semantics.
- [ ] Run affected package tests and targeted race tests and confirm GREEN.

### Task 6: Policy And Full Verification

**Files:** `server/service/direct_upload_test.go`, `server/handler/upload_prepare_test.go`

- [ ] Assert regular PUT and multipart policy resources equal the source ARN and exclude `uploads/finalized/`.
- [ ] Run `go test ./... -count=1`.
- [ ] Run targeted `go test -race` for repository/service promotion concurrency.
- [ ] Run `go vet ./server/storage ./server/repository ./server/service ./server/handler ./server/agent ./server/mcp`.
- [ ] Build `./server` and `./agent` to `/tmp`, run `git diff --check`, self-review the complete diff, and commit.
