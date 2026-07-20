# Unified Prompt UX Corrections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Correct the unified prompt input so task/plan submission, drag feedback, attachment tiles, preview, limits, and Resume OSS transport behave consistently.

**Architecture:** Keep `AgentPromptInput` as the shared UI boundary and `usePromptAttachments` as the upload lifecycle boundary. Extend the existing server attachment verifier for Resume rather than adding a second upload mechanism.

**Tech Stack:** React 19, TypeScript, Base UI/shadcn components, Vitest, Go Fiber, OSS direct upload.

---

### Task 1: Shared policy and external submit mode

**Files:**
- Modify: `studio/src/components/agent-prompt/AgentPromptInput.tsx`
- Modify: `studio/src/components/agent-prompt/attachment-admission.ts`
- Test: `studio/src/components/agent-prompt/AgentPromptInput.test.tsx`

- [x] Add failing tests showing `submitMode="external"` renders no inline submit action and Enter does not call `onSubmit`.
- [x] Add exported five-file policies with media `50 * MB` and document/text `25 * MB` defaults.
- [x] Implement `submitMode?: 'inline' | 'external'` with inline as the default.
- [x] Run `bun run test -- src/components/agent-prompt/AgentPromptInput.test.tsx` and expect all tests to pass.

### Task 2: Target-aligned drag feedback

**Files:**
- Modify: `studio/src/components/agent-prompt/AgentPromptDropProvider.tsx`
- Test: `studio/src/components/agent-prompt/AgentPromptDropProvider.test.tsx`

- [x] Add a failing test whose composer surface has a fixed DOM rect and assert the overlay uses that rect.
- [x] Store the active surface rect and refresh it on dragover, resize, and scroll.
- [x] Render one fixed overlay using the active surface coordinates and preserve global routing.
- [x] Run the provider test and expect it to pass.

### Task 3: Attachment tiles and full-screen preview

**Files:**
- Modify: `studio/src/components/agent-prompt/AgentPromptInput.tsx`
- Modify: `studio/src/components/agent-prompt/AttachmentPreviewDialog.tsx`
- Modify: `studio/src/components/agent-prompt/ImageViewerStage.tsx`
- Test: corresponding component test files.

- [x] Add failing tests for local image thumbnails, compact non-image tiles, remove/retry controls, and full-screen lightbox classes.
- [x] Replace vertical attachment rows with a horizontal, wrapping tile strip backed by `previewSource(id)`.
- [x] Make the preview shell viewport filling and move image controls to lightbox positions while preserving safe signed-URL resolution.
- [x] Run all three component test files and expect them to pass.

### Task 4: Apply one policy to all Studio surfaces

**Files:**
- Modify: `studio/src/pages/DashboardPage.tsx`
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Modify: `studio/src/pages/DesignerPage.tsx`
- Test: page tests for these surfaces.

- [x] Add failing assertions that task/plan dialogs have only their footer submit action and every surface caps attachments at five.
- [x] Pass external submit mode from task and plan forms, reuse the shared five-file policy, cap Designer at `min(providerLimit, 5)`, and set Resume to direct OSS mode.
- [x] Run page tests and expect them to pass.

### Task 5: Resume stable OSS attachment contract

**Files:**
- Modify: `studio/src/lib/api/tasks.ts`
- Modify: `server/handler/task.go`
- Modify: `server/handler/input_attachment.go`
- Modify: `server/service/task_resume.go`
- Test: `studio/src/lib/api/tasks.test.ts`, `server/handler/task_test.go`, `server/service/task_test.go`.

- [x] Add failing tests that Resume submits JSON stable identities, rejects more than five, finalizes verified uploads, and persists finalized bytes into the resume directory.
- [x] Change the Studio Resume request to `{ prompt, input_attachments }` JSON.
- [x] Validate with `InputAttachmentValidationOptions{MaxCount: 5}`, finalize through the existing immutable verifier, read bounded finalized objects, and pass them to `TaskService.Resume`.
- [x] Run Studio API plus Go handler/service tests and expect them to pass.

### Task 6: Full verification

**Files:** No production changes.

- [x] Run `go test ./...`, server/agent builds, `make vet`, full Studio tests, and Studio build.
- [x] Run Browser QA on desktop and mobile for task/plan/Resume dialogs, five-file admission, attachment tiles, and full-screen preview; defend drag positioning with explicit component geometry tests.
- [x] Run `git diff --check` and review the complete diff before reporting completion.
