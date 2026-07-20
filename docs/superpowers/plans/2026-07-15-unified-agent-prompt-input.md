# Unified Agent Prompt Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace every Studio primary Prompt entry with one extensible composer that supports OSS-backed attachments, paste, local/global drop, preview, project context, clone restoration, resume append semantics, and Designer references.

**Architecture:** Build the Server contract first: verified key-first attachment snapshots, authenticated key-to-signed-URL resolution, clone overrides, resume OSS keys, and Designer reference registration. Then add a controlled `AgentPromptInput` built from the focused AI Elements Prompt Input interaction patterns and existing shadcn primitives, with adapters for direct-upload, resume, and Designer; migrate the six entry surfaces only after the shared behavior is covered.

**Tech Stack:** Go 1.24, Fiber v3, GORM, OSS storage/STSvended direct upload, React 19, TypeScript, Vite 8, Tailwind CSS v4, shadcn Base Nova, TanStack Query, React Hook Form, Vitest, Testing Library, Browser/Playwright.

---

## File Structure

### Server

- `server/service/direct_upload.go`: verify pending upload identity from `upload_id + key`, finalize new uploads, and allow same-user finalized attachments to be reused.
- `server/service/direct_upload_test.go`: key-first identity and cross-user/key/purpose rejection tests.
- `server/handler/input_attachment.go`: normalize storage attachments to verified key-first `EntryAttachment` values.
- `server/handler/input_attachment_test.go`: Handler contract tests for new and inherited attachments.
- `server/handler/upload.go`: authenticated signed download URL endpoint.
- `server/handler/upload_prepare_test.go`: request validation, ownership, owner-resource fallback, and signing tests.
- `server/router/router.go`: register `/uploads/resolve-download-url` and `/designer/register-reference`.
- `server/main.go`: give Upload/Designer handlers the repository/storage dependencies needed for ownership validation.
- `server/handler/task.go`: accept clone input overrides and broaden common attachment types.
- `server/handler/task_test.go`: create/clone key-first payload tests.
- `server/service/task_retry.go`: clone frozen configuration with final Prompt/attachment overrides.
- `server/service/task_test.go`: clone snapshot and source relationship tests.
- `server/service/task_resume.go`: persist resume files in OSS-backed storage and retain object keys.
- `server/handler/designer.go`: register an OSS direct upload as a Designer reference.
- `server/service/designer.go`: validate/read the registered key and return the existing `file_id` shape.
- `server/handler/designer_test.go`: Designer upload registration ownership tests.
- `server/handler/video_split_contract.go`: expose key-first attachment data without turning signed URLs into model state.

### Studio shared code

- `studio/src/types/input-attachment.ts`: key-first API attachment types and UI-only `PromptAttachment` state.
- `studio/src/lib/api/uploads.ts`: resolve download URL client.
- `studio/src/lib/api/uploads.test.ts`: resolve request/response tests.
- `studio/src/lib/direct-upload.ts`: stop returning/persisting `publicUrl` as attachment identity.
- `studio/src/lib/direct-upload.test.ts`: key-first direct upload result tests.
- `studio/src/components/agent-prompt/attachment-admission.ts`: pure type/size/count/dedupe admission.
- `studio/src/components/agent-prompt/attachment-admission.test.ts`: table-driven admission tests.
- `studio/src/components/agent-prompt/AgentPromptDropProvider.tsx`: active composer registration and document-level drag state.
- `studio/src/components/agent-prompt/AgentPromptDropProvider.test.tsx`: focus/Dialog routing and drag-depth tests.
- `studio/src/components/agent-prompt/usePromptAttachments.ts`: upload queue, retries, Object URLs, and serialization adapters.
- `studio/src/components/agent-prompt/usePromptAttachments.test.tsx`: upload lifecycle and cleanup tests.
- `studio/src/components/agent-prompt/AttachmentPreviewDialog.tsx`: image/media/PDF/text preview and download fallback.
- `studio/src/components/agent-prompt/AttachmentPreviewDialog.test.tsx`: renderer and signed URL refresh tests.
- `studio/src/components/agent-prompt/AgentPromptInput.tsx`: shared controlled composer.
- `studio/src/components/agent-prompt/AgentPromptInput.test.tsx`: keyboard, paste, file picker, layout, slots, and submit tests.
- `studio/src/components/agent-prompt/ProjectContextControl.tsx`: selectable/read-only/none project context.
- `studio/src/components/agent-prompt/ProjectContextControl.test.tsx`: context modes and project selection tests.

### Studio entry surfaces

- `studio/src/App.tsx`: mount `AgentPromptDropProvider` once inside the protected route element.
- `studio/src/pages/DashboardPage.tsx` and `DashboardPage.ai-entry.test.tsx`: replace split textarea/material input.
- `studio/src/pages/TasksPage.tsx` and `TasksPage.test.tsx`: use composer in task creation.
- `studio/src/pages/PlansPage.tsx` and `PlansPage.test.tsx`: use composer in plan creation/edit.
- `studio/src/pages/TaskDetailPage.tsx` and `TaskDetailPage.test.tsx`: use composer for fresh resume and restored clone Dialogs.
- `studio/src/lib/api/tasks.ts` and `studio/src/lib/api/tasks.test.ts`: key-first resume and clone payloads.
- `studio/src/pages/DesignerPage.tsx`, `DesignerPage.reference-drop.test.tsx`, and `DesignerPage.provider-contract.test.ts`: use composer, project context, and OSS reference registration.
- `studio/src/lib/api/designer.ts` and `studio/src/lib/api/designer.test.ts`: register reference by key.
- Remove after migration: `studio/src/components/designer/DesignerPromptBar.tsx`, `DesignerReferenceDock.tsx`, their obsolete tests, and `ReferenceMaterialInput.tsx` only when no remaining imports exist.

## Task 1: Verified Key-First Attachment Identity

**Files:**
- Modify: `server/service/direct_upload.go`
- Modify: `server/service/direct_upload_test.go`
- Modify: `server/handler/input_attachment.go`
- Modify: `server/handler/input_attachment_test.go`

- [ ] **Step 1: Write failing service tests for pending and finalized upload identity**

Add table cases proving a matching pending upload is finalized, a matching finalized upload is reusable, and cross-user, wrong-purpose, mismatched-key, expired, and unknown upload IDs fail:

```go
func TestResolveDirectUploadAttachment(t *testing.T) {
    now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
    repo := &fakePendingUploadRepo{uploads: map[string]*model.PendingUpload{
        "up-1": {
            ID: "up-1", UserID: "user-1",
            Purpose: DirectUploadPurposeAIEntryAttachment,
            Key: "uploads/pending/user-1/up-1/input.png",
            Status: model.PendingUploadStatusPending,
            ExpiresAt: now.Add(time.Hour),
        },
    }}

    got, err := ResolveDirectUploadAttachment(
        context.Background(), repo, "user-1",
        []string{DirectUploadPurposeAIEntryAttachment},
        "up-1", "uploads/pending/user-1/up-1/input.png", now,
    )
    if err != nil { t.Fatal(err) }
    if got.Key != "uploads/pending/user-1/up-1/input.png" { t.Fatalf("key = %q", got.Key) }
    if got.UploadID != "up-1" { t.Fatalf("upload id = %q", got.UploadID) }
}
```

- [ ] **Step 2: Run the service test and verify RED**

Run: `go test ./server/service -run TestResolveDirectUploadAttachment -count=1`

Expected: FAIL because `ResolveDirectUploadAttachment` does not exist.

- [ ] **Step 3: Implement the verified identity helper**

Add a result type and helper. Treat Server repository values as authoritative; finalize only pending rows and permit already-finalized rows for plan editing and cloning:

```go
type VerifiedDirectUpload struct {
    UploadID   string
    Key        string
    FileName   string
    ContentType string
    Size       int64
}

func ResolveDirectUploadAttachment(
    ctx context.Context,
    repo PendingUploadRepository,
    userID string,
    allowedPurposes []string,
    uploadID string,
    assertedKey string,
    now time.Time,
) (*VerifiedDirectUpload, error) {
    upload, err := repo.FindPendingUploadByID(ctx, strings.TrimSpace(uploadID))
    if err != nil { return nil, ErrPendingUploadAccessDenied }
    if upload.UserID != userID || upload.Key != strings.TrimSpace(assertedKey) ||
        !directUploadPurposeAllowed(upload.Purpose, allowedPurposes) {
        return nil, ErrPendingUploadAccessDenied
    }
    switch upload.Status {
    case model.PendingUploadStatusPending:
        if !upload.ExpiresAt.After(now) { return nil, ErrPendingUploadExpired }
        if err := repo.FinalizePendingUploads(ctx, []string{upload.ID}, now); err != nil { return nil, err }
    case model.PendingUploadStatusFinalized:
    default:
        return nil, ErrPendingUploadAccessDenied
    }
    return &VerifiedDirectUpload{
        UploadID: upload.ID, Key: upload.Key, FileName: upload.FileName,
        ContentType: upload.ContentType, Size: upload.Size,
    }, nil
}
```

- [ ] **Step 4: Write the failing Handler test for key-first normalization**

Submit an attachment with deliberately forged metadata and assert the returned normalized attachment contains Server metadata, `Key`, and `UploadID`, with empty `URL`:

```go
got, err := validateInputAttachments(ctx, pending, "user-1", []model.EntryAttachment{{
    Type: "image", UploadID: "up-1",
    Key: "uploads/pending/user-1/up-1/input.png",
    URL: "https://attacker.example/file.png",
    FileName: "forged.png", Size: 1,
}}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: allAgentAttachmentTypes})
if err != nil { t.Fatal(err) }
if got[0].URL != "" || got[0].Key != pending.uploads["up-1"].Key { t.Fatalf("got %#v", got[0]) }
```

- [ ] **Step 5: Run the Handler test and verify RED**

Run: `go test ./server/handler -run TestValidateInputAttachmentsPersistsVerifiedKey -count=1`

Expected: FAIL because current normalization strips `UploadID` and `Key` and retains URL identity.

- [ ] **Step 6: Implement key-first Handler normalization**

For storage files require `upload_id + key`, call `ResolveDirectUploadAttachment`, copy verified metadata, keep only user instructions from the request, and retain the current URL-only branch only for existing stored data read paths. Define the shared allowlist once:

```go
var allAgentAttachmentTypes = map[string]bool{
    "image": true, "audio": true, "video": true,
    "document": true, "text": true,
}
```

- [ ] **Step 7: Run focused tests and commit**

Run: `go test ./server/service ./server/handler -run 'TestResolveDirectUploadAttachment|TestValidateInputAttachments' -count=1`

Expected: PASS.

```bash
git add server/service/direct_upload.go server/service/direct_upload_test.go server/handler/input_attachment.go server/handler/input_attachment_test.go
git commit -m "feat(storage): persist verified attachment keys"
```

## Task 2: Authenticated Key-to-Signed-URL Resolution

**Files:**
- Modify: `server/handler/upload.go`
- Modify: `server/handler/upload_prepare_test.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/plan.go`
- Modify: `server/handler/plan_test.go`
- Modify: `server/handler/video_split_contract.go`
- Modify: `server/router/router.go`
- Modify: `server/main.go`
- Create: `studio/src/lib/api/uploads.ts`
- Create: `studio/src/lib/api/uploads.test.ts`

- [ ] **Step 1: Write failing Handler tests for signing**

Cover pending/finalized upload ownership and task/plan owner fallback. Assert the storage fake receives only the verified key and the response returns `url` plus `expires_at`. Add negative cases for cross-user, mismatched key, unrelated task/plan, and external key.

```go
req := httptest.NewRequest(http.MethodPost, "/uploads/resolve-download-url",
    strings.NewReader(`{"upload_id":"up-1","key":"uploads/pending/user-1/up-1/input.png"}`))
req.Header.Set("Content-Type", "application/json")
resp, err := app.Test(req)
if err != nil { t.Fatal(err) }
if resp.StatusCode != fiber.StatusOK { t.Fatalf("status = %d", resp.StatusCode) }
if store.signedKeys[0] != "uploads/pending/user-1/up-1/input.png" { t.Fatal(store.signedKeys) }
```

- [ ] **Step 2: Run the Handler test and verify RED**

Run: `go test ./server/handler -run TestResolveAttachmentDownloadURL -count=1`

Expected: FAIL because the endpoint and method are absent.

- [ ] **Step 3: Implement `UploadHandler.ResolveDownloadURL`**

Extend `UploadHandler` with the aggregate repository for task/plan ownership checks. Parse this request:

```go
type resolveDownloadURLRequest struct {
    UploadID string `json:"upload_id"`
    Key      string `json:"key"`
    OwnerType string `json:"owner_type"`
    OwnerID   string `json:"owner_id"`
}
```

Verification order:

1. If `upload_id` is present, load the pending upload and require current user + exact key + pending/finalized status.
2. Otherwise require `owner_type` (`task` or `plan`) and `owner_id`, load the resource, require current user, derive each attachment key from `Key` or a Server-owned legacy URL, and require an exact match.
3. Call `store.DownloadURL(ctx, key, service.DefaultSignedURLTTL)` and return the expiry timestamp.

- [ ] **Step 4: Register and wire the endpoint**

Register after the existing prepare route:

```go
uploads.Post("/resolve-download-url", svc.UploadHandler.ResolveDownloadURL)
```

Construct the handler with `repo` and `store` in `server/main.go`; keep `Prepare` behavior unchanged.

- [ ] **Step 5: Expose keys for current URL-only task/plan attachments**

Write Handler response tests first: a Server-owned legacy attachment URL must produce an API attachment with a derived `key`, while an external URL must not. Add storage-aware response serialization that checks `store.IsOwnedURL` before `storage.StorageKeyFromURL`. Give `TaskHandler` a `SetStore` matching `PlanHandler`, pass the active store from `server/main.go`, and update all `taskAPIResponse`/`planAPIResponse` call sites. This is a read-only response migration; it never mutates the model row and never emits a signed URL.

- [ ] **Step 6: Write the failing Studio API test**

```ts
await uploadsApi.resolveDownloadUrl({ upload_id: 'up-1', key: 'uploads/pending/user-1/up-1/input.png' })
expect(post).toHaveBeenCalledWith('/uploads/resolve-download-url', {
  upload_id: 'up-1', key: 'uploads/pending/user-1/up-1/input.png',
})
```

- [ ] **Step 7: Run the Studio API test and verify RED**

Run: `cd studio && bun run test -- src/lib/api/uploads.test.ts`

Expected: FAIL because `uploadsApi` does not exist.

- [ ] **Step 8: Implement the Studio client and run focused tests**

```ts
export interface ResolveDownloadUrlRequest {
  upload_id?: string
  key: string
  owner_type?: 'task' | 'plan'
  owner_id?: string
}

export const uploadsApi = {
  resolveDownloadUrl: (data: ResolveDownloadUrlRequest) =>
    unwrap<{ url: string; expires_at: string }>(http.post('/uploads/resolve-download-url', data)),
}
```

Run:

```bash
go test ./server/handler -run TestResolveAttachmentDownloadURL -count=1
cd studio && bun run test -- src/lib/api/uploads.test.ts
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add server/handler/upload.go server/handler/upload_prepare_test.go server/handler/task.go server/handler/task_test.go server/handler/plan.go server/handler/plan_test.go server/handler/video_split_contract.go server/router/router.go server/main.go studio/src/lib/api/uploads.ts studio/src/lib/api/uploads.test.ts
git commit -m "feat(storage): resolve signed attachment URLs"
```

## Task 3: Clone, Resume, and Designer Server Contracts

**Files:**
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/service/task_retry.go`
- Modify: `server/service/task_test.go`
- Modify: `server/service/task_resume.go`
- Modify: `server/handler/designer.go`
- Modify: `server/handler/designer_test.go`
- Modify: `server/service/designer.go`
- Modify: `server/router/router.go`

- [ ] **Step 1: Write failing clone override tests**

Handler test: `POST /tasks/:id/clone` with `prompt` and a verified key-first attachment returns a clone with exactly that final input snapshot. Service test: overrides do not change frozen project snapshot, type, model, ecommerce/video/montage configuration, `InputSourceTaskID`, or billing path.

```go
type CloneTaskParams struct {
    Prompt *string
    InputAttachments *[]model.EntryAttachment
}
```

- [ ] **Step 2: Run clone tests and verify RED**

Run: `go test ./server/handler ./server/service -run 'TestCloneTaskAcceptsFinalInputSnapshot|TestTaskServiceCloneAppliesInputOverrides' -count=1`

Expected: FAIL because clone accepts no body or override parameters.

- [ ] **Step 3: Implement clone parameters**

Bind an optional JSON body, validate Prompt length and attachments through the same Handler helper, and call `TaskService.Clone(ctx, id, params)`. In the service, default to source values only when the corresponding pointer is nil; preserve all non-input frozen fields.

- [ ] **Step 4: Write and run failing resume OSS-key test**

Assert `persistResumeInputs` stores `EntryAttachment.Key` for an OSS upload and does not store a signed URL:

Run: `go test ./server/service -run TestTaskServiceResumePersistsObjectKey -count=1`

Expected: FAIL because current resume attachments store the storage URL only.

- [ ] **Step 5: Implement resume key persistence**

Use the existing deterministic resume object key, call storage upload, and store that key in `EntryAttachment.Key`. Keep resume roles and empty-input rejection unchanged.

- [ ] **Step 6: Write failing Designer registration tests**

Assert `POST /designer/register-reference` accepts a same-user finalized `designer_reference` upload, rejects an `ai_entry_attachment` or another user, reads the verified key through storage, and returns `file_id`.

- [ ] **Step 7: Run Designer tests and verify RED**

Run: `go test ./server/handler -run TestRegisterDesignerReference -count=1`

Expected: FAIL because the endpoint is absent.

- [ ] **Step 8: Implement Designer registration**

Add:

```go
type registerDesignerReferenceRequest struct {
    UploadID string `json:"upload_id"`
    Key string `json:"key"`
}
```

Resolve using allowed purpose `designer_reference`, read bytes from the verified key, and reuse the existing Designer reference-file registration logic without re-uploading the bytes to a second OSS key.

- [ ] **Step 9: Run focused server tests and commit**

Run:

```bash
go test ./server/handler ./server/service -run 'Clone|Resume|RegisterDesignerReference' -count=1
```

Expected: PASS.

```bash
git add server/handler/task.go server/handler/task_test.go server/service/task_retry.go server/service/task_test.go server/service/task_resume.go server/handler/designer.go server/handler/designer_test.go server/service/designer.go server/router/router.go
git commit -m "feat(tasks): support editable clone inputs"
```

## Task 4: Studio Attachment Domain and Adapters

**Files:**
- Modify: `studio/src/types/input-attachment.ts`
- Modify: `studio/src/lib/direct-upload.ts`
- Modify: `studio/src/lib/direct-upload.test.ts`
- Create: `studio/src/components/agent-prompt/attachment-admission.ts`
- Create: `studio/src/components/agent-prompt/attachment-admission.test.ts`
- Create: `studio/src/components/agent-prompt/usePromptAttachments.ts`
- Create: `studio/src/components/agent-prompt/usePromptAttachments.test.tsx`

- [ ] **Step 1: Write failing type/admission tests**

Test image/audio/video/document/text classification, 50 MB media and 25 MB document limits, configurable count, stable dedupe, and per-file rejection reasons. Use real `File` values rather than mocks.

```ts
const result = admitPromptFiles({
  current: [attachmentFromFile(first)],
  incoming: [duplicate, pdf, unsupported],
  policy: DEFAULT_AGENT_ATTACHMENT_POLICY,
})
expect(result.accepted.map((item) => item.fileName)).toEqual(['brief.pdf'])
expect(result.rejections.map((item) => item.reason)).toEqual(['duplicate', 'unsupported_type'])
```

- [ ] **Step 2: Run admission tests and verify RED**

Run: `cd studio && bun run test -- src/components/agent-prompt/attachment-admission.test.ts`

Expected: FAIL because the domain module does not exist.

- [ ] **Step 3: Implement key-first types and pure admission**

Keep API `InputAttachment` snake_case. Add UI-only camelCase `PromptAttachment`, `AgentPromptValue`, policy and rejection types. Do not place preview URLs in either persistent type. Define the page adapter boundary explicitly:

```ts
export interface PromptAttachmentPolicy {
  allowedTypes: InputAttachmentType[]
  maxCount: number
  maxBytes: Partial<Record<InputAttachmentType, number>>
}

export interface PromptAttachmentAdapter {
  mode: 'direct' | 'local' | 'designer'
  purpose?: 'ai_entry_attachment' | 'designer_reference'
}
```

`direct` uploads immediately and serializes key-first attachments; `local` keeps resume files in memory for multipart submit; `designer` uploads with `designer_reference` and later registers the returned identity.

- [ ] **Step 4: Update direct upload serialization tests and implementation**

Keep `UploadToOSSResult.publicUrl` because existing specialized video, ecommerce, montage, project, and template inputs still consume it. Add a composer conversion test proving `toInputAttachments()` takes only `uploadId`, `key`, `contentType`, size, name, type, and instruction; `publicUrl` must not appear in the new attachment snapshot.

- [ ] **Step 5: Write failing hook tests**

Cover concurrent order preservation, upload progress, retry, removal, inherited attachment conversion, direct-upload serialization, local resume serialization, Designer serialization, and Object URL cleanup.

- [ ] **Step 6: Run hook tests and verify RED**

Run: `cd studio && bun run test -- src/components/agent-prompt/usePromptAttachments.test.tsx`

Expected: FAIL because the hook does not exist.

- [ ] **Step 7: Implement `usePromptAttachments`**

Expose:

```ts
interface PromptAttachmentController {
  attachments: PromptAttachment[]
  addFiles(files: File[]): void
  retry(id: string): void
  remove(id: string): void
  clear(): void
  uploading: boolean
  hasFailures: boolean
  toInputAttachments(): InputAttachment[]
  localFiles(): File[]
}
```

Adapters decide whether files upload immediately (`ai_entry_attachment`), remain local until submit (resume), or upload using `designer_reference`. The hook owns and releases local Object URLs.

- [ ] **Step 8: Run tests and commit**

Run: `cd studio && bun run test -- src/lib/direct-upload.test.ts src/components/agent-prompt/attachment-admission.test.ts src/components/agent-prompt/usePromptAttachments.test.tsx`

Expected: PASS.

```bash
git add studio/src/types/input-attachment.ts studio/src/lib/direct-upload.ts studio/src/lib/direct-upload.test.ts studio/src/components/agent-prompt
git commit -m "feat(studio): add prompt attachment domain"
```

## Task 5: Global Drop Coordination

**Files:**
- Create: `studio/src/components/agent-prompt/AgentPromptDropProvider.tsx`
- Create: `studio/src/components/agent-prompt/AgentPromptDropProvider.test.tsx`
- Modify: `studio/src/App.tsx`

- [ ] **Step 1: Write failing provider tests**

Render two registered targets and verify the last-focused target receives files; render one inside an open Dialog and verify it wins. Test nested dragenter/leaves, unsupported drags, Escape, unmount, full capacity, and exactly one drop delivery.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd studio && bun run test -- src/components/agent-prompt/AgentPromptDropProvider.test.tsx`

Expected: FAIL because the provider does not exist.

- [ ] **Step 3: Implement provider/context registration**

Expose `registerTarget`, `focusTarget`, and `useAgentPromptDropTarget`. Attach document listeners once. Track drag depth in refs and render one portal overlay with semantic tokens and `pointer-events-none`.

- [ ] **Step 4: Mount once in the authenticated layout**

Wrap the existing route outlet, not each page. Ensure sign-in/public routes do not create a drop target unless a composer registers.

- [ ] **Step 5: Run tests and commit**

Run: `cd studio && bun run test -- src/components/agent-prompt/AgentPromptDropProvider.test.tsx`

Expected: PASS.

```bash
git add studio/src/components/agent-prompt/AgentPromptDropProvider.tsx studio/src/components/agent-prompt/AgentPromptDropProvider.test.tsx studio/src/App.tsx
git commit -m "feat(studio): route global file drops to prompt inputs"
```

## Task 6: Shared Composer and Project Context

**Files:**
- Create: `studio/src/components/agent-prompt/AgentPromptInput.tsx`
- Create: `studio/src/components/agent-prompt/AgentPromptInput.test.tsx`
- Create: `studio/src/components/agent-prompt/ProjectContextControl.tsx`
- Create: `studio/src/components/agent-prompt/ProjectContextControl.test.tsx`

- [ ] **Step 1: Refresh official component docs without installing AI Elements wholesale**

Run:

```bash
cd studio
bunx --bun shadcn@latest docs input-group button dialog tooltip popover
curl -L -sS https://ai-sdk.dev/elements/components/prompt-input.md > /tmp/ai-elements-prompt-input.md
```

Expected: docs URLs and the current Prompt Input source documentation. Do not run bare `ai-elements`; do not add chat/model/voice dependencies.

- [ ] **Step 2: Write failing composer tests**

Cover stable structure order, `+` file input, paste files vs text, local drop, auto-grow style, visible-area max, Enter submit, Shift+Enter newline, IME composition, pending/failed disablement, slots, status, custom submit icon/label, and drop-target focus registration.

```ts
fireEvent.keyDown(textarea, { key: 'Enter', shiftKey: true })
expect(onSubmit).not.toHaveBeenCalled()
fireEvent.keyDown(textarea, { key: 'Enter', isComposing: false })
expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ prompt: '写一篇内容' }))
```

- [ ] **Step 3: Run composer tests and verify RED**

Run: `cd studio && bun run test -- src/components/agent-prompt/AgentPromptInput.test.tsx`

Expected: FAIL because the component does not exist.

- [ ] **Step 4: Implement the composer**

Compose installed shadcn `InputGroup`, `Button`, `Tooltip`, and `Popover`. Reuse the focused AI Elements patterns for textarea autosize, hidden file input, file paste/drop, and form submission. Preserve one DOM order:

```tsx
<section data-slot="agent-prompt-input">
  {contextBar && <header data-slot="agent-prompt-context">{contextBar}</header>}
  <div data-slot="agent-prompt-surface">
    <PromptAttachmentList />
    <textarea data-slot="agent-prompt-textarea" />
    <footer>
      <div><AddFilesButton />{leadingTools}</div>
      <div>{status}{trailingTools}<SubmitButton /></div>
    </footer>
  </div>
</section>
```

Use one shared min-height token/class and `max-height: min(..., calc(100dvh - ...))`; do not add compact/large variants.

- [ ] **Step 5: Write failing project-context tests**

Cover searchable active projects, create-project navigation, no-project option, read-only project, and selected project label.

- [ ] **Step 6: Implement `ProjectContextControl` and run tests**

Run: `cd studio && bun run test -- src/components/agent-prompt/AgentPromptInput.test.tsx src/components/agent-prompt/ProjectContextControl.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add studio/src/components/agent-prompt/AgentPromptInput.tsx studio/src/components/agent-prompt/AgentPromptInput.test.tsx studio/src/components/agent-prompt/ProjectContextControl.tsx studio/src/components/agent-prompt/ProjectContextControl.test.tsx
git commit -m "feat(studio): add unified agent prompt input"
```

## Task 7: Attachment Preview

**Files:**
- Create: `studio/src/components/agent-prompt/AttachmentPreviewDialog.tsx`
- Create: `studio/src/components/agent-prompt/AttachmentPreviewDialog.test.tsx`
- Modify: `studio/src/components/designer/ImagePreview.tsx` to consume the shared image-viewer controls instead of retaining a second zoom implementation

- [ ] **Step 1: Write failing preview tests**

Test local images, signed remote images, next/previous, zoom boundaries, download, video, audio, PDF iframe/object, text-size limit, office-document fallback, Escape/focus return, and a single signature refresh after expiry.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd studio && bun run test -- src/components/agent-prompt/AttachmentPreviewDialog.test.tsx`

Expected: FAIL because the preview Dialog does not exist.

- [ ] **Step 3: Implement preview renderers**

Use the installed shadcn Dialog and Slider plus the proven zoom/navigation behavior already covered in `components/designer/ImagePreview.tsx`; extract that behavior into the shared preview instead of adding another dependency. Remote renderers call `uploadsApi.resolveDownloadUrl`; no signed URL enters `PromptAttachment` or form state.

- [ ] **Step 4: Run preview tests and build**

Run:

```bash
cd studio
bun run test -- src/components/agent-prompt/AttachmentPreviewDialog.test.tsx
bun run build
```

Expected: PASS and Vite build succeeds.

- [ ] **Step 5: Commit**

```bash
git add studio/src/components/agent-prompt/AttachmentPreviewDialog.tsx studio/src/components/agent-prompt/AttachmentPreviewDialog.test.tsx studio/src/components/designer/ImagePreview.tsx
git commit -m "feat(studio): preview prompt attachments"
```

## Task 8: Dashboard, Task Creation, and Plans

**Files:**
- Modify: `studio/src/pages/DashboardPage.tsx`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/lib/schemas.test.ts`
- Modify: `server/handler/task.go`
- Modify: `server/handler/plan.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/plan_test.go`

- [ ] **Step 1: Write failing page integration tests**

Assert all three pages render `data-slot="agent-prompt-input"`, submit key-first attachments, disable while upload runs, and expose project selection in the context bar. Plan edit must retain omitted/empty/replace behavior.

- [ ] **Step 2: Run page tests and verify RED**

Run:

```bash
cd studio
bun run test -- src/pages/DashboardPage.ai-entry.test.tsx src/pages/TasksPage.test.tsx src/pages/PlansPage.test.tsx
```

Expected: FAIL because the pages still use separate inputs.

- [ ] **Step 3: Broaden Server task/plan attachment allowlists with failing tests first**

Add Handler table tests for image/audio/video/document/text, wrong MIME/extension, count, size, and tenant ownership. Run them to observe the current image-only rejection, then switch task/plan validation to `allAgentAttachmentTypes`.

- [ ] **Step 4: Migrate Dashboard**

Replace the textarea plus detached `ReferenceMaterialInput` with `AgentPromptInput`. Pass project/content/execution context through `contextBar`; serialize through the direct-upload adapter.

- [ ] **Step 5: Migrate task creation**

Bind `prompt` and `input_attachments` through React Hook Form with a controlled composer. Use the same component for normal, video creator/editor, and montage main briefs while leaving structured source inputs intact.

- [ ] **Step 6: Migrate plan creation/edit**

Bind Prompt and attachments to the plan form, including edit prefill. Keep the scheduler, model, image, video, and montage controls outside the composer.

- [ ] **Step 7: Run focused tests and commit**

Run:

```bash
go test ./server/handler -run 'CreateTask|Plan.*Attachment' -count=1
cd studio && bun run test -- src/pages/DashboardPage.ai-entry.test.tsx src/pages/TasksPage.test.tsx src/pages/PlansPage.test.tsx src/lib/schemas.test.ts
```

Expected: PASS.

```bash
git add server/handler/task.go server/handler/plan.go server/handler/task_test.go server/handler/plan_test.go studio/src/pages/DashboardPage.tsx studio/src/pages/DashboardPage.ai-entry.test.tsx studio/src/pages/TasksPage.tsx studio/src/pages/TasksPage.test.tsx studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx studio/src/lib/schemas.ts studio/src/lib/schemas.test.ts
git commit -m "feat(studio): unify task and plan prompt entry"
```

## Task 9: Resume and Clone Dialogs

**Files:**
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`
- Modify: `studio/src/lib/api/tasks.ts`
- Modify: `studio/src/lib/api/tasks.test.ts`

- [ ] **Step 1: Write failing resume tests**

Open resume twice and assert both times the composer starts with empty Prompt/attachments. Submit text/files and assert multipart data still targets the same task and no clone navigation occurs.

- [ ] **Step 2: Write failing clone tests**

Assert clicking clone opens a Dialog instead of posting immediately, restores source Prompt and only non-resume attachments, displays read-only project context, permits edit/remove/add, and posts the final full snapshot.

- [ ] **Step 3: Run tests and verify RED**

Run: `cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx src/lib/api/tasks.test.ts`

Expected: FAIL because clone is immediate and resume uses bespoke controls.

- [ ] **Step 4: Implement API request shapes**

```ts
export interface CloneTaskRequest {
  prompt: string
  input_attachments: InputAttachment[]
}

clone: (id: string, data: CloneTaskRequest) =>
  unwrap<Task>(http.post(`/tasks/${id}/clone`, data))
```

Keep resume multipart, but ensure API receives local files from the resume adapter rather than a page-owned file state.

- [ ] **Step 5: Replace both Dialog bodies with `AgentPromptInput`**

Use separate controlled state instances so clone prefill cannot leak into resume. Clear resume state on close/success. Derive clone initial attachments once from `task.input_attachments`, filtering resume roles.

- [ ] **Step 6: Run tests and commit**

Run: `cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx src/lib/api/tasks.test.ts`

Expected: PASS.

```bash
git add studio/src/pages/TaskDetailPage.tsx studio/src/pages/TaskDetailPage.test.tsx studio/src/lib/api/tasks.ts studio/src/lib/api/tasks.test.ts
git commit -m "feat(studio): unify resume and clone inputs"
```

## Task 10: Designer Integration

**Files:**
- Modify: `studio/src/pages/DesignerPage.tsx`
- Modify: `studio/src/pages/DesignerPage.reference-drop.test.tsx`
- Modify: `studio/src/pages/DesignerPage.provider-contract.test.ts`
- Modify: `studio/src/lib/api/designer.ts`
- Modify: `studio/src/lib/api/designer.test.ts`
- Modify: `studio/src/components/designer/DesignerToolbar.tsx`
- Remove after green: `studio/src/components/designer/DesignerPromptBar.tsx`
- Remove after green: `studio/src/components/designer/DesignerReferenceDock.tsx`
- Remove obsolete dock/prompt tests after equivalent coverage exists

- [ ] **Step 1: Write failing Designer integration tests**

Assert the shared composer floats in the existing canvas frame, reference images appear inside it, provider caps govern its policy, project selection includes “无项目”, selected `project_id` reaches generate/history, and `designer_reference` uploads register by `upload_id + key` before generate.

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
cd studio
bun run test -- src/pages/DesignerPage.reference-drop.test.tsx src/pages/DesignerPage.provider-contract.test.ts
```

Expected: FAIL because Designer still uses its own prompt/reference components and sends empty project IDs.

- [ ] **Step 3: Add Designer API registration**

```ts
registerReference: (data: { upload_id: string; key: string }) =>
  unwrap<{ file_id: string; filename: string; size: number }>(
    http.post('/designer/register-reference', data),
  )
```

- [ ] **Step 4: Migrate Designer state and submission**

Store one `AgentPromptValue` for generate mode. Use a Designer attachment policy that accepts images only and follows `maxReferenceImages`. On submit, upload local files with `designer_reference`, register each key, and send ordered `reference_file_ids`. In edit mode, preserve mask/source semantics while using shared labels/icons/status props.

- [ ] **Step 5: Remove duplicate reference UI and page-level drag state**

Delete the sidebar material dock and old full-page drag handler only after the global provider and shared composer tests cover all prior admission cases. Keep pure provider capability normalization.

- [ ] **Step 6: Run focused tests and commit**

Run:

```bash
go test ./server/handler -run TestRegisterDesignerReference -count=1
cd studio && bun run test -- src/pages/DesignerPage.reference-drop.test.tsx src/pages/DesignerPage.provider-contract.test.ts src/components/agent-prompt
```

Expected: PASS.

```bash
git add server/handler/designer.go server/handler/designer_test.go server/service/designer.go server/router/router.go studio/src/pages/DesignerPage.tsx studio/src/pages/DesignerPage.reference-drop.test.tsx studio/src/pages/DesignerPage.provider-contract.test.ts studio/src/lib/api/designer.ts studio/src/lib/api/designer.test.ts studio/src/components/designer/DesignerToolbar.tsx studio/src/components/designer/DesignerPromptBar.tsx studio/src/components/designer/DesignerReferenceDock.tsx studio/src/components/designer/DesignerReferenceDock.test.tsx
git commit -m "feat(designer): use unified prompt input"
```

## Task 11: Legacy Cleanup and End-to-End Verification

**Files:**
- Remove: `studio/src/components/ReferenceMaterialInput.tsx`
- Remove: `studio/src/components/ReferenceMaterialInput.test.tsx`
- Modify: exact imports/tests reported by the Step 1 searches, without staging unrelated files

- [ ] **Step 1: Prove no old primary prompt implementation remains**

Run:

```bash
rg -n "ReferenceMaterialInput|DesignerPromptBar|DesignerReferenceDock|resumeFileInputRef" studio/src
rg -n "<textarea|<Textarea|<Input" studio/src/pages/DashboardPage.tsx studio/src/pages/TasksPage.tsx studio/src/pages/PlansPage.tsx studio/src/pages/TaskDetailPage.tsx studio/src/pages/DesignerPage.tsx
```

Expected: no legacy primary Prompt/attachment imports; remaining textareas are only specialized business fields explicitly outside scope.

- [ ] **Step 2: Remove confirmed-dead code and run focused tests**

Run: `cd studio && bun run test -- src/components/agent-prompt src/pages/DashboardPage.ai-entry.test.tsx src/pages/TasksPage.test.tsx src/pages/PlansPage.test.tsx src/pages/TaskDetailPage.test.tsx src/pages/DesignerPage.reference-drop.test.tsx`

Expected: PASS.

- [ ] **Step 3: Run formatting/static checks**

Run:

```bash
gofmt -w server/service/direct_upload.go server/handler/input_attachment.go server/handler/upload.go server/handler/task.go server/service/task_retry.go server/service/task_resume.go server/handler/designer.go server/service/designer.go
make vet
cd studio && bunx tsc -b
```

Expected: PASS with no new warnings.

- [ ] **Step 4: Run complete automated verification**

Run:

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test
cd studio && bun run build
```

Expected: all tests pass and both Go binaries plus Studio production bundle build.

- [ ] **Step 5: Start Studio and Server for browser verification**

Use the repository's normal development configuration. If the default Vite port is occupied, choose another and record it. Do not substitute local file URLs for the OSS key/signing flow.

- [ ] **Step 6: Run Browser checks at desktop and mobile widths**

Use the in-app Browser plugin first. Verify:

- homepage project menu, Prompt growth, `+`, paste, component drop, and document drop;
- task and plan Dialog max-height behavior;
- resume opens blank every time;
- clone restores and edits the full original snapshot;
- Designer project/no-project selection and provider reference caps;
- image zoom/download, media playback, PDF, office fallback;
- signed URL expiry refresh and no signed URL in submitted JSON;
- no overlap, clipped text, blank preview, console error, or failed network request.

Capture desktop and mobile screenshots for the final report.

- [ ] **Step 7: Commit cleanup**

```bash
git add -A studio/src/components/ReferenceMaterialInput.tsx studio/src/components/ReferenceMaterialInput.test.tsx
git commit -m "refactor(studio): remove legacy prompt inputs"
```

## Self-Review Coverage

- Component structure, autosize, slots, project bar, `+`, paste, local/global drop: Tasks 4-6.
- OSS direct upload, verified key persistence, Server signed full URL: Tasks 1-4 and 7.
- Images/media/PDF/office preview: Task 7.
- Dashboard, task, plan, resume, clone, Designer: Tasks 8-10.
- Clone restore vs resume blank semantics: Tasks 3 and 9.
- Designer project ownership and reference caps: Tasks 3 and 10.
- Specialized video/ecommerce/montage inputs retained: Task 8 regression tests.
- Accessibility, responsive layout, browser verification, full tests/builds: Tasks 6, 7, and 11.
