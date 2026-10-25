# User Media Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an account-level image and video library with independent AI descriptions, secure MCP discovery, and confirmed reuse in Studio creation.

**Architecture:** Add user media metadata beside immutable `assets`, and reuse the existing durable image-analysis job queue for both image and native-video descriptions. A Server-owned library service handles lifecycle and identity-scoped search; Studio uses authenticated APIs and existing attachment IDs for both direct selection and confirmed interactive cards.

**Tech Stack:** Go, GORM, Fiber, Asynq, existing model clients/provider-cost recorder, MCP Go SDK, React 19, TypeScript, TanStack Query, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-25-user-media-library-design.md`

## Global Constraints

- `assets` remain immutable and owned by one user; library metadata is a separate record.
- `ContentAsset` continues to represent reusable content/topic assets and is not reused for uploaded media.
- The MCP handler may authenticate, validate, invoke one capability, and encode its result; it must not orchestrate user workflows.
- User IDs and current project IDs come from authenticated Server identity, never from MCP arguments.
- Media is reusable across projects; a project tag only changes candidate ordering.
- Analysis descriptions and user usage notes are stored and edited independently.
- Media analysis records provider/model/token usage and does not settle or debit user credits.
- Library removal is soft; it must not delete storage objects or invalidate historical task references.
- New creation commits must revalidate media ownership, analysis readiness, and removal state.

## Review Focus

- A queued or failed video is never returned as an AI candidate or committed as a selected library item; cover in Tasks 2, 4, and 6.
- A foreign user's media ID or project ID never yields metadata, preview URLs, or an accepted task attachment; cover in Tasks 3 and 4.
- Re-analysis cannot overwrite user usage notes, even when the job retries or an old generation finishes late; cover in Tasks 2 and 3.
- Soft removal prevents new selection but leaves a previously-created task's attachment usable; cover in Tasks 3 and 4.
- Repeated/duplicate filenames and reordered candidates preserve the selected stable ID; cover in Tasks 4 and 6.

---

### Task 1: Media domain and upload contract

**Files:**
- Create: `server/model/user_media_asset.go`
- Create: `server/repository/user_media_asset.go`
- Modify: `server/model/model.go`
- Modify: `server/repository/repository.go`
- Modify: `server/service/direct_upload.go`
- Modify: `server/handler/upload.go`
- Test: `server/model/user_media_asset_test.go`
- Test: `server/repository/user_media_asset_test.go`
- Test: `server/service/direct_upload_test.go`
- Test: `server/handler/upload_prepare_test.go`

**Interfaces:**
- Produces `model.UserMediaAsset` with `AssetID`, `UserID`, optional `ProjectID`, status, AI description/use cases, user usage notes, soft-removal timestamp, and timestamps.
- Produces repository methods to create, find owned/active records, update atomically, list/filter, and search ready candidates with project-first ordering.
- Produces direct-upload purpose `media_library`, restricted to images and videos and the existing attachment limits.

- [ ] Write migration/model tests for media-library fields, statuses, and model auto-migration.
- [ ] Run the focused model tests and verify they fail because the model/table is absent.
- [ ] Add the model and repository contract/implementation; ensure every list/search query scopes by user and active status.
- [ ] Write upload-policy tests accepting supported images/videos and rejecting other types, empty files, and over-limit files.
- [ ] Run upload tests and verify they fail because `media_library` is not a registered purpose.
- [ ] Register the purpose and include it in the authenticated upload preparation path.
- [ ] Run `cd server && go test ./model ./repository ./service ./handler -run 'UserMedia|MediaLibrary|DirectUpload' -count=1`.
- [ ] Commit as `feat: add user media library persistence and upload purpose`.

### Task 2: Durable image and native-video analysis

**Files:**
- Modify: `server/model/image_analysis.go`
- Modify: `server/service/image_analysis.go`
- Modify: `server/main.go`
- Test: `server/service/image_analysis_test.go`
- Test: `server/handler/image_analysis_test.go`

**Interfaces:**
- Consumes Task 1's media model/repository.
- Produces an image-analysis subject/kind for user media and `ImageAnalysisService.CreateUserMediaJob`, `RetryUserMedia`, and result/status transitions.
- Extends `ImageAnalysisConfig` with native-video client/provider/model configuration; video calls use `CompleteWithVideoURLResult` and a short-lived storage URL.
- Produces structured AI fields `description` and `recommended_uses`; media status moves through `queued`, `running`, `available`, and `failed`.

- [ ] Write failing tests for queued image/video jobs, image and native-video dispatch, persisted description/use cases, unsupported video route failure, retry generation, late-generation invalidation, and note preservation.
- [ ] Run focused tests and verify each fails at the missing user-media subject/dispatch behavior.
- [ ] Extend the durable worker to lock/update user-media state with its analysis job and to use the configured native-video route for video assets.
- [ ] Reuse provider usage recording with a unique job/generation/attempt/stage request ID and `image` or `video` media kind; do not call any wallet service.
- [ ] Wire image and video model clients in `main.go` and retain existing recovery scheduling.
- [ ] Run `cd server && go test ./service ./handler -run 'ImageAnalysis|UserMediaAnalysis' -count=1`.
- [ ] Commit as `feat: analyze user media asynchronously`.

### Task 3: Authenticated media-library lifecycle API

**Files:**
- Create: `server/service/user_media_library.go`
- Create: `server/service/user_media_library_test.go`
- Create: `server/handler/user_media_library.go`
- Create: `server/handler/user_media_library_test.go`
- Modify: `server/router/router.go`
- Modify: `server/main.go`

**Interfaces:**
- Consumes Tasks 1-2.
- Produces API operations: `POST /api/v1/media-assets` (finalize upload and enqueue analysis), `GET /api/v1/media-assets` (search/type/project/status/IDs filters), `GET /api/v1/media-assets/:id`, `PATCH /api/v1/media-assets/:id` (usage notes/project), `POST /api/v1/media-assets/:id/reanalyze`, and `DELETE /api/v1/media-assets/:id` (soft removal).
- Responses include stable media ID, type, safe short-lived preview URL, analysis state/error, AI description/use cases, user notes, project tag, and timestamps.

- [ ] Write failing service/handler tests for upload finalization, project ownership, list filtering, signed preview URLs, note editing, retry, soft removal, foreign IDs, and history-preserving behavior.
- [ ] Run focused tests and verify failures occur for missing lifecycle behavior.
- [ ] Implement a thin handler and service-owned lifecycle; project filters never broaden account scope.
- [ ] Ensure retry resets only analysis-generated fields and soft removal never calls storage deletion.
- [ ] Run `cd server && go test ./service ./handler -run 'UserMediaLibrary|MediaAsset' -count=1`.
- [ ] Commit as `feat: expose user media library API`.

### Task 4: MCP candidate search and creation validation

**Files:**
- Modify: `server/mcp/tools.go`
- Create: `server/mcp/user_media_tools.go`
- Create: `server/mcp/user_media_tools_test.go`
- Modify: `server/mcp/mcp.go`
- Modify: `server/service/interactive.go`
- Modify: `server/service/assistant_commit.go`
- Test: `server/service/assistant_commit_test.go`

**Interfaces:**
- Consumes Task 3's media library service.
- Produces `search_user_media_assets({query, media_type, limit})`; the tool has no user/project ID inputs, resolves scope from current interactive or execution identity, returns ready media only, and puts project-tag matches first.
- MCP results include `asset_id` and instruct interactive agents to use `media_asset:<asset_id>` as the question option label so Studio can render and return a stable ID.
- Assistant creation commit accepts a media-library `asset_id` only when owner matches, status is available, and `removed_at` is null.

- [ ] Write failing MCP tests for registration, identity-required behavior, no caller identity override, per-user isolation, ready-only results, project-first sorting, and bounded query input.
- [ ] Run focused MCP tests and verify they fail for the absent tool.
- [ ] Implement the atomic read-only tool and current-session/current-execution scope resolution.
- [ ] Write failing creation-commit tests for valid library IDs, foreign IDs, queued/failed IDs, removed IDs, and duplicate filenames with distinct IDs.
- [ ] Run commit tests and verify they fail because library purpose/readiness is not yet accepted.
- [ ] Add Server-side creation validation; keep frozen task attachments valid after later soft removal.
- [ ] Run `cd server && go test ./mcp ./service -run 'UserMedia|AssistantCreation' -count=1`.
- [ ] Commit as `feat: expose scoped media candidates to creation agents`.

### Task 5: Studio library page and navigation

**Files:**
- Create: `studio/src/lib/api/user-media.ts`
- Create: `studio/src/lib/api/user-media.test.ts`
- Create: `studio/src/pages/UserMediaLibraryPage.tsx`
- Create: `studio/src/pages/UserMediaLibraryPage.test.tsx`
- Modify: `studio/src/App.tsx`
- Modify: `studio/src/lib/navigation.ts`
- Modify: `studio/src/lib/navigation.test.ts`

**Interfaces:**
- Consumes Task 3's HTTP contract.
- Produces typed API methods for list, upload/finalize, fetch by ID, update metadata, retry, and remove.
- Produces `/media-assets` navigation and page workflows while leaving `/content-assets` unchanged.

- [ ] Write failing navigation and page tests for route presence, upload results, search/type/project filters, status states, note editing, retry, remove, and cross-project tags.
- [ ] Run focused Studio tests and verify they fail because the page/API are absent.
- [ ] Implement the typed API and dense library management view using existing upload, table, dialog, and query patterns.
- [ ] Run `cd studio && bun run test -- src/lib/api/user-media.test.ts src/pages/UserMediaLibraryPage.test.tsx src/lib/navigation.test.ts`.
- [ ] Commit as `feat: add Studio user media library`.

### Task 6: Chat picker and interactive candidate cards

**Files:**
- Modify: `studio/src/components/agent-prompt/AgentPromptInput.tsx`
- Modify: `studio/src/components/agent-prompt/AgentPromptInput.test.tsx`
- Modify: `studio/src/components/dsh-user-questions/DshQuestionComposer.tsx`
- Modify: `studio/src/components/dsh-user-questions/DshQuestionComposer.test.tsx`
- Modify: `studio/src/pages/AssistantWorkspace.tsx` (question parsing and creation-submit path)
- Modify: `studio/src/pages/AssistantWorkspace.test.tsx`

**Interfaces:**
- Consumes Task 5's typed API and Task 4's `media_asset:<asset_id>` option-label contract.
- The creation composer `+` menu offers local upload and library selection. Library selection creates uploaded `PromptAttachment` values with stable `assetId` and does not call OSS upload.
- Interactive candidate options resolve through `GET /media-assets/:id`, render as single/multi-select media cards, and submit stable labels. On successful answer submission, selected ready media is added to the composer; skipping adds none.
- Creation draft serialization carries `asset_id`; Server validation remains authoritative.

- [ ] Write failing component tests for menu actions, multi-select library selection, no re-upload, candidate-card preview/details, skip/reject, stable ID answers, and attachment serialization into the draft.
- [ ] Run focused tests and verify they fail because the picker/card behaviors are absent.
- [ ] Implement the picker, structured card presentation, answer-to-attachment conversion, and creation-submit integration.
- [ ] Run `cd studio && bun run test -- src/components/agent-prompt/AgentPromptInput.test.tsx src/components/dsh-user-questions/DshQuestionComposer.test.tsx src/pages/AssistantWorkspace.test.tsx`.
- [ ] Commit as `feat: add media library selection to creation chat`.

### Task 7: Full verification and branch review

**Files:** all files changed by Tasks 1-6.

**Interfaces:**
- Verifies the full Go/Studio/API/MCP contract from the design spec.

- [ ] Run `cd server && go test ./...`.
- [ ] Run `cd server && go build -o /tmp/anban-creator-server .`.
- [ ] Run `cd studio && bun run test`.
- [ ] Run `cd studio && bun run build`.
- [ ] Review the whole branch for identity leaks, stale-generation races, remove/history behavior, and UI card overlap; fix any findings with a failing regression test first.
- [ ] Start the Studio dev server on an available port and report its URL.
