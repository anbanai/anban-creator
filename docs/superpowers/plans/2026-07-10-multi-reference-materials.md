# Multi-Reference Materials Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user submit one unified brief plus multiple optionally annotated reference images, then let the Seednote agent analyze the request first, inspect every image, choose per-output references, generate, vision-verify, retry automatically, and expose a readable usage summary without any mid-run user decisions.

**Architecture:** Extend the existing `EntryAttachment` contract into a validated snapshot shared by AI Entry, manual tasks, and plans; materialize that snapshot identically for local, Docker, and Kubernetes executors; and reuse one controlled Studio input component across the three entry points. Resolve one capability-bearing image model descriptor per generation request, pass it through billing and generation, and update all Seednote Agent/Skill distributions so request analysis, image analysis, cross-image planning, per-page reference selection, verification, retry, and trace artifacts are automatic.

**Tech Stack:** Go 1.24 services and tests, GORM JSON fields, Fiber handlers, Bun + React 19 + TypeScript + React Hook Form + Zod + Vitest/Testing Library, YAML model routes, Markdown/TOML Agent and Skill distributions.

---

## Locked decisions and invariants

- The user supplies input once. The running task never asks for confirmation, ranking, `auto / required / excluded`, page binding, or another choice.
- The single task/plan prompt is analyzed before any image. The Agent writes a task-specific `analyze_image` prompt for each image rather than using a fixed analysis prompt.
- Every available image is analyzed before cross-image comparison or reference selection. An input image gets at most 3 analysis attempts.
- Each generated page receives only the relevant original local paths, in the same order described by the generation prompt. A page may receive zero, one, or multiple references.
- Every generated image is vision-verified. Each output image gets at most 3 generation attempts total, including the initial generation.
- Quality outranks duration and Credits. There is no cheaper mode and no skip-analysis control.
- Seednote accepts at most 16 image attachments. Each `instruction` accepts at most 1000 Unicode code points, measured in Go with `utf8.RuneCountInString` and in Studio with `Array.from(value).length`.
- Plans use snapshot semantics: omitted `input_attachments` retains the existing array, `[]` clears it, and a non-empty array replaces it. Spawned tasks clone the plan array, so later plan edits cannot mutate historical tasks.
- `ReferenceMaterialInput` is shared by Dashboard, manual task creation, and plan create/edit. It must not replace the structured video-reference UI or e-commerce `MultiImageUpload`.
- Do not modify `studio/src/components/ui/*`.
- Agent and Skill docs use bare MCP tool names such as `analyze_image` and `generate_image`.
- Claude Code, Codex, and OpenClaw copies must remain behaviorally identical. Existing plugin versions are asserted, not bumped: Claude Code `2.10.55`, Codex `2.10.49`, OpenClaw `2.7.41`.

## File responsibility map

| Area | Files | Responsibility |
|---|---|---|
| Attachment contract | `server/model/entry_attachment.go`, `server/handler/input_attachment.go` | Shared field set, normalization, type/URL/size/count/instruction validation, pending-upload finalization |
| Task/AI Entry | `server/handler/task.go`, `server/service/task.go`, `server/service/ai_entry.go` | Manual snapshot intake, slice cloning, Seednote first-image compatibility removal |
| Plan snapshot | `server/model/plan.go`, `server/handler/plan.go`, `server/service/plan.go`, `server/service/task.go` | Persist, update with omitted/empty/replace semantics, clone into each spawned task |
| Executor | `server/agent/config_builder.go` and executor call sites | Stable original indices, `index.json`, `errors.json`, identical runtime materialization |
| Studio shared input | `studio/src/types/input-attachment.ts`, `studio/src/components/ReferenceMaterialInput.tsx` | Controlled attachments, uploads, progress, retry, preview/cards, instructions, validation |
| Studio entry points | `DashboardPage.tsx`, `TasksPage.tsx`, `PlansPage.tsx`, schemas/types | Preserve existing behavior while sharing the input component |
| Image capability routing | `server/config/config.go`, `server/config.yaml`, `server/service/model_config.go`, `server/service/image.go` | Deterministic quality-ranked compatible model resolution and one resolved descriptor |
| MCP/profile | `server/mcp/image_tools.go`, `server/mcp/billing.go`, `server/mcp/tools.go` | Resolve once, preflight reference limits, bill/generate/log the same model, expose capability metadata |
| Agent/Skill | three runtime distributions | Automatic analysis, selection, verification, retries, and structured trace artifacts |
| Trace UI | `ReferenceUsageSummary.tsx`, `TaskDetailPage.tsx` | Parse exact summary file safely and show decisions without chain-of-thought |

## Stable contracts introduced by this plan

```go
type InputAttachmentValidationOptions struct {
    MaxCount     int
    AllowedTypes map[string]bool
}

func validateInputAttachments(
    ctx context.Context,
    pending service.PendingUploadRepository,
    userID string,
    attachments []model.EntryAttachment,
    options InputAttachmentValidationOptions,
) ([]model.EntryAttachment, error)
```

```go
type ResolvedImageModel struct {
    Config             *config.ImageAPIConfig `json:"-"`
    Key                string                 `json:"key,omitempty"`
    Provider           string                 `json:"provider"`
    Model              string                 `json:"model"`
    Source             string                 `json:"source,omitempty"`
    SupportsReference  bool                   `json:"supports_reference"`
    MaxReferenceImages int                    `json:"max_reference_images"`
    SelectionReason    string                 `json:"selection_reason,omitempty"`
}
```

```go
func (s *ModelConfigService) ResolveImageModelForGeneration(
    ctx context.Context,
    userID string,
    imageModelKey string,
    imageType string,
    referenceCount int,
) (*ResolvedImageModel, error)
```

```ts
export interface ReferenceUsageSummaryData {
  version: '1.0'
  inputs: Array<{
    attachment_index: number
    file_name?: string
    url?: string
    instruction?: string
    status: 'used' | 'excluded' | 'analysis_failed'
    decision_summary: string
    analysis_attempts: number
    warnings?: string[]
  }>
  outputs: Array<{
    file_name: string
    references: Array<{ attachment_index: number; purpose: string }>
    generation_attempts: number
    verification: {
      status: 'passed' | 'warning' | 'failed'
      summary: string
    }
    provider?: string
    model?: string
    selection_reason?: string
  }>
  warnings?: string[]
  model_fallback_reason?: string
}
```

### Task 1: Add the shared server attachment model and validator

**Files:**
- Modify: `server/model/entry_attachment.go:5-17`
- Create: `server/handler/input_attachment.go`
- Create: `server/handler/input_attachment_test.go`
- Modify: `server/handler/ai_entry.go:1-220`

- [ ] **Step 1: Write failing validator tests**

Create table-driven tests that prove trimming, instruction preservation, Unicode code-point limits, count/type limits, metadata validation, URL validation, and pending upload finalization:

```go
func TestValidateInputAttachmentsNormalizesAndFinalizes(t *testing.T) {
    pending := &fakePendingUploadRepository{}
    got, err := validateInputAttachments(context.Background(), pending, "user-1", []model.EntryAttachment{{
        Type: " image ", URL: " https://cdn.test/product.png ", FileName: " product.png ",
        ContentType: " image/png ", UploadID: " upload-1 ", Key: " key-1 ",
        Instruction: "  保持包装和 Logo  ",
    }}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
    require.NoError(t, err)
    require.Equal(t, "image", got[0].Type)
    require.Equal(t, "https://cdn.test/product.png", got[0].URL)
    require.Equal(t, "保持包装和 Logo", got[0].Instruction)
    require.Equal(t, []string{"https://cdn.test/product.png"}, pending.finalizedURLs)
    require.Equal(t, service.DirectUploadPurposeAIEntryAttachment, pending.purpose)
}

func TestValidateInputAttachmentsRejectsInstructionOver1000CodePoints(t *testing.T) {
    _, err := validateInputAttachments(context.Background(), nil, "user-1", []model.EntryAttachment{{
        Type: "image", URL: "https://cdn.test/product.png", FileName: "product.png",
        ContentType: "image/png", Instruction: strings.Repeat("图", 1001),
    }}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
    require.EqualError(t, err, "attachment instruction must not exceed 1000 characters")
}

func TestValidateInputAttachmentsRejectsCountAndDisallowedType(t *testing.T) {
    images := make([]model.EntryAttachment, 17)
    for i := range images {
        images[i] = model.EntryAttachment{Type: "image", URL: fmt.Sprintf("https://cdn.test/%d.png", i), ContentType: "image/png"}
    }
    _, err := validateInputAttachments(context.Background(), nil, "user-1", images,
        InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
    require.EqualError(t, err, "at most 16 attachments are allowed")

    _, err = validateInputAttachments(context.Background(), nil, "user-1", []model.EntryAttachment{{
        Type: "video", URL: "https://cdn.test/demo.mp4", ContentType: "video/mp4",
    }}, InputAttachmentValidationOptions{MaxCount: 16, AllowedTypes: map[string]bool{"image": true}})
    require.EqualError(t, err, "attachment type video is not allowed")
}
```

Reuse or move the current AI Entry metadata and fake pending-upload fixtures; do not leave duplicate validator paths.

- [ ] **Step 2: Run the focused tests and confirm the red state**

Run:

```bash
cd server
go test ./handler -run 'TestValidateInputAttachments' -count=1
```

Expected: FAIL because `Instruction`, `InputAttachmentValidationOptions`, and `validateInputAttachments` do not exist.

- [ ] **Step 3: Add the shared field and validator implementation**

Add the field to `EntryAttachment`:

```go
Instruction string `json:"instruction,omitempty"`
```

Move the current classification, metadata, extension, URL, and size helpers from `ai_entry.go` into `input_attachment.go`. The public behavior of the validator is fixed by this implementation skeleton:

```go
const (
    aiEntryMediaAttachmentMaxBytes    int64 = 50 * 1024 * 1024
    aiEntryDocumentAttachmentMaxBytes int64 = 25 * 1024 * 1024
    inputAttachmentInstructionMaxRunes      = 1000
)

type InputAttachmentValidationOptions struct {
    MaxCount     int
    AllowedTypes map[string]bool
}

func validateInputAttachments(ctx context.Context, pending service.PendingUploadRepository, userID string, attachments []model.EntryAttachment, options InputAttachmentValidationOptions) ([]model.EntryAttachment, error) {
    if options.MaxCount > 0 && len(attachments) > options.MaxCount {
        return nil, fmt.Errorf("at most %d attachments are allowed", options.MaxCount)
    }
    normalized := make([]model.EntryAttachment, len(attachments))
    urls := make([]string, 0, len(attachments))
    for i, raw := range attachments {
        a := normalizeHandlerEntryAttachment(raw)
        if utf8.RuneCountInString(a.Instruction) > inputAttachmentInstructionMaxRunes {
            return nil, fmt.Errorf("attachment instruction must not exceed %d characters", inputAttachmentInstructionMaxRunes)
        }
        if err := validateHandlerEntryAttachment(&a); err != nil {
            return nil, fmt.Errorf("attachment %d: %w", i+1, err)
        }
        if len(options.AllowedTypes) > 0 && !options.AllowedTypes[a.Type] {
            return nil, fmt.Errorf("attachment type %s is not allowed", a.Type)
        }
        if a.URL != "" {
            if !validAIEntryAttachmentURL(a.URL, pending != nil) {
                return nil, fmt.Errorf("attachment URLs must be internal file paths or http(s) URLs")
            }
            urls = append(urls, a.URL)
        }
        normalized[i] = a
    }
    if pending != nil {
        if err := finalizePendingURLs(ctx, pending, userID, service.DirectUploadPurposeAIEntryAttachment, urls); err != nil {
            return nil, err
        }
    }
    return normalized, nil
}

func normalizeHandlerEntryAttachment(a model.EntryAttachment) model.EntryAttachment {
    a.Type = strings.TrimSpace(a.Type)
    a.URL = strings.TrimSpace(a.URL)
    a.Text = strings.TrimSpace(a.Text)
    a.FileName = strings.TrimSpace(a.FileName)
    a.ContentType = strings.TrimSpace(a.ContentType)
    a.Role = strings.TrimSpace(a.Role)
    a.UploadID = strings.TrimSpace(a.UploadID)
    a.Key = strings.TrimSpace(a.Key)
    a.Instruction = strings.TrimSpace(a.Instruction)
    return a
}
```

Update `AIEntryHandler.Submit` to call the shared validator with Dashboard-compatible types:

```go
validatedAttachments, err := validateInputAttachments(c.Context(), h.pending, userID, req.Attachments, InputAttachmentValidationOptions{
    AllowedTypes: map[string]bool{"image": true, "audio": true, "video": true, "document": true, "text": true},
})
if err != nil {
    return Error(c, fiber.StatusBadRequest, err.Error())
}
req.Attachments = validatedAttachments
```

Delete the old inline loop and duplicate constants/helpers from `ai_entry.go` after moving them.

- [ ] **Step 4: Run handler tests and confirm green**

Run:

```bash
cd server
go test ./handler -run 'TestValidateInputAttachments|TestAIEntry' -count=1
```

Expected: PASS; existing AI Entry attachment type, URL, size, and pending-upload tests remain green.

- [ ] **Step 5: Commit the shared attachment validator**

```bash
git add server/model/entry_attachment.go server/handler/input_attachment.go server/handler/input_attachment_test.go server/handler/ai_entry.go
git commit -m "feat: share input attachment validation"
```

### Task 2: Persist manual-task snapshots and preserve AI Entry compatibility

**Files:**
- Modify: `server/handler/task.go:75-117`
- Modify: `server/handler/task_test.go`
- Modify: `server/service/task.go:347-375, 620-650`
- Modify: `server/service/task_test.go`
- Modify: `server/service/ai_entry.go:120-160`
- Modify: `server/service/ai_entry_test.go`

- [ ] **Step 1: Write failing manual-task and AI Entry tests**

Use the existing repository-backed test seams rather than introducing fake handlers:

- In `server/handler/task_test.go`, create a real SQLite repository with `setupTaskHandlerTestDB`, create a Seednote user/project, build `TaskService` plus `TaskHandler`, call `handler.SetRepository(repo)`, register `POST /tasks` on a Fiber app that sets `user_id`, and submit JSON with `postJSON`.
- For the accepted case, decode the returned task ID with `decodeEnvelopeRawData`, reload it through `repo.Tasks().FindByID`, and assert the stored attachment URL, filename, and instruction. For the rejected case, submit a video attachment and assert HTTP 400 and no task row.
- In `server/service/task_test.go`, use `setupTaskServiceWithEnqueuer` and `createTestProject`; call `CreateManual`, mutate the caller-owned input slice, reload the task, and prove the persisted attachment snapshot still contains the original instruction.
- In `server/service/ai_entry_test.go`, extend the existing `AIEntryService.Submit` tests. Use a local table-driven closure that creates a project with `createTestProject`, supplies a `fakeAIEntryLLM`, submits one image attachment, reloads the created task, and returns it. Assert Seednote leaves `ReferenceImageURL` empty while preserving `InputAttachments`; Article and Moments retain first-image promotion.

The new test names are:

```go
TestCreateTaskAcceptsSeednoteInputAttachments
TestCreateTaskRejectsNonImageSeednoteAttachment
TestCreateManualClonesInputAttachments
TestAIEntrySeednoteDoesNotPromoteFirstImageToReferenceImageURL
TestAIEntryArticleAndMomentsKeepFirstImageCompatibility
```

- [ ] **Step 2: Run focused tests and confirm the red state**

Run:

```bash
cd server
go test ./handler ./service -run 'TestCreateTask.*InputAttachments|TestCreateManualClonesInputAttachments|TestAIEntry.*FirstImage' -count=1
```

Expected: FAIL because the manual handler request has no `input_attachments`, the slice is not cloned, and Seednote still promotes the first image.

- [ ] **Step 3: Wire validation, cloning, and compatibility behavior**

Add to the manual create request:

```go
InputAttachments []model.EntryAttachment `json:"input_attachments,omitempty"`
```

Before invoking the task service, validate every attachment supplied through the manual-task endpoint as a Seednote-style image reference array. The AI Entry endpoint remains the separate generic multi-file path:

```go
var pending service.PendingUploadRepository
if h.repo != nil {
    pending = h.repo.PendingUploads()
}
validatedAttachments, err := validateInputAttachments(c.Context(), pending, userID, req.InputAttachments, InputAttachmentValidationOptions{
    MaxCount: 16,
    AllowedTypes: map[string]bool{"image": true},
})
if err != nil {
    return Error(c, fiber.StatusBadRequest, err.Error())
}
req.InputAttachments = validatedAttachments
```

Pass the validated slice in the existing `CreateManualParams` literal:

```go
InputAttachments: req.InputAttachments,
```

`TaskHandler.SetRepository` is already wired in `server/main.go`; reuse `h.repo.PendingUploads()` so upload finalization has the same purpose as AI Entry without changing the constructor.

Add and use the clone helper in `server/service/task.go`:

```go
func cloneEntryAttachments(in []model.EntryAttachment) []model.EntryAttachment {
    return append([]model.EntryAttachment(nil), in...)
}
```

```go
task.SetInputAttachments(cloneEntryAttachments(p.InputAttachments))
```

Trim `Instruction` wherever AI Entry normalizes attachments and include it in the structured AI-entry prompt attachment description:

```go
if attachment.Instruction != "" {
    lines = append(lines, fmt.Sprintf("- attachment %d instruction: %s", i+1, attachment.Instruction))
}
```

Narrow the legacy first-image promotion:

```go
switch project.Platform {
case model.PlatformArticle, model.PlatformMoments:
    params.ReferenceImageURL = firstImageAttachmentURL(req.Attachments)
case model.PlatformSeednote:
    // Seednote uses InputAttachments as its only new per-run reference source.
}
```

- [ ] **Step 4: Run manual task and AI Entry suites**

Run:

```bash
cd server
go test ./handler ./service -run 'TestCreateTask|TestCreateManual|TestAIEntry' -count=1
```

Expected: PASS, including old Article/Moments behavior and new Seednote snapshot behavior.

- [ ] **Step 5: Commit manual-task snapshot support**

```bash
git add server/handler/task.go server/handler/task_test.go server/service/task.go server/service/task_test.go server/service/ai_entry.go server/service/ai_entry_test.go
git commit -m "feat: persist seednote input attachments"
```

### Task 3: Add plan attachment snapshots and exact update semantics

**Files:**
- Modify: `server/model/plan.go:40-110`
- Modify: `server/handler/plan.go:78-180`
- Modify: `server/handler/plan_test.go`
- Modify: `server/service/plan.go:120-245, 274-360`
- Modify: `server/service/plan_test.go`
- Modify: `server/service/task.go:947-978`

- [ ] **Step 1: Write failing create/update/spawn tests**

Use only the existing repository-backed helpers:

- In `server/service/plan_test.go`, use `setupTestPlanService`. Add a small local helper inside the test file that creates the required user/project records, then calls `svc.Create(ctx, CreatePlanParams{...})` with an attachment slice. Reuse that concrete plan in three tests proving update omission retains, an explicit empty pointer clears, and a non-empty pointer replaces.
- Mutate the caller-owned slice after `Create` and assert the stored plan remains independent.
- In `server/service/task_test.go`, use `setupTaskServiceWithEnqueuer` and a real persisted plan, invoke the existing plan-to-task creation path twice, then mutate and persist the plan attachment data. Reload both tasks and assert both retain the original instruction.
- In `server/handler/plan_test.go`, construct the real `PlanHandler`, call `SetRepository(repo)`, and exercise create/update with Fiber JSON requests. Assert create accepts validated Seednote images, omitted update retains, and an explicit `[]` clears.

Use these test names so the focused commands are stable:

```go
TestCreatePlanClonesInputAttachments
TestUpdatePlanInputAttachmentsOmittedRetainsExisting
TestUpdatePlanInputAttachmentsEmptyClears
TestUpdatePlanInputAttachmentsNonEmptyReplaces
TestCreateFromPlanClonesAttachmentSnapshotPerTask
TestPlanHandlerInputAttachmentSemantics
```

- [ ] **Step 2: Run focused plan tests and confirm the red state**

Run:

```bash
cd server
go test ./handler ./service -run 'Test.*Plan.*InputAttachments|TestPlanSpawnClonesAttachmentSnapshotPerTask' -count=1
```

Expected: FAIL because `Plan.InputAttachments` and request/service parameters do not exist.

- [ ] **Step 3: Add the model, request, service, and spawn contracts**

Add the model field and setter:

```go
InputAttachments datatypes.JSONType[[]EntryAttachment] `gorm:"type:json" json:"input_attachments"`

func (p *Plan) SetInputAttachments(attachments []EntryAttachment) {
    p.InputAttachments = datatypes.NewJSONType(attachments)
}
```

Add handler request fields, preserving pointer semantics on update:

```go
// createPlanRequest
InputAttachments []model.EntryAttachment `json:"input_attachments,omitempty"`

// updatePlanRequest
InputAttachments *[]model.EntryAttachment `json:"input_attachments,omitempty"`
```

Validate create and non-nil update arrays through the shared image-only contract, reusing the repository already wired on `PlanHandler`:

```go
var pending service.PendingUploadRepository
if h.repo != nil {
    pending = h.repo.PendingUploads()
}
validatedAttachments, err := validateInputAttachments(c.Context(), pending, userID, req.InputAttachments, InputAttachmentValidationOptions{
    MaxCount: 16,
    AllowedTypes: map[string]bool{"image": true},
})
if err != nil {
    return Error(c, fiber.StatusBadRequest, err.Error())
}
req.InputAttachments = validatedAttachments
```

For update, preserve a nil pointer and validate only explicit arrays, including an explicit empty array:

```go
if req.InputAttachments != nil {
    validatedAttachments, err := validateInputAttachments(c.Context(), pending, userID, *req.InputAttachments, InputAttachmentValidationOptions{
        MaxCount: 16,
        AllowedTypes: map[string]bool{"image": true},
    })
    if err != nil {
        return Error(c, fiber.StatusBadRequest, err.Error())
    }
    req.InputAttachments = &validatedAttachments
}
```

Add service parameters:

```go
type CreatePlanParams struct {
    InputAttachments []model.EntryAttachment
}

type UpdatePlanParams struct {
    InputAttachments *[]model.EntryAttachment
}
```

Persist clones on create and update:

```go
plan.SetInputAttachments(cloneEntryAttachments(p.InputAttachments))
```

```go
if p.InputAttachments != nil {
    plan.SetInputAttachments(cloneEntryAttachments(*p.InputAttachments))
}
```

Copy a fresh snapshot into every task created from a plan:

```go
task.SetInputAttachments(cloneEntryAttachments(plan.InputAttachments.Data()))
```

The generic `planAPIResponse` serialization in `server/handler/video_split_contract.go:105-112` exposes the new JSON field automatically; do not add a second response DTO.

- [ ] **Step 4: Run plan and task-spawn suites**

Run:

```bash
cd server
go test ./handler ./service -run 'Test.*Plan|Test.*Spawn' -count=1
```

Expected: PASS; omitted retains, empty clears, non-empty replaces, each spawned task remains independent, and older plans deserialize with an empty attachment array.

- [ ] **Step 5: Commit plan snapshot support**

```bash
git add server/model/plan.go server/handler/plan.go server/handler/plan_test.go server/service/plan.go server/service/plan_test.go server/service/task.go server/service/task_test.go
git commit -m "feat: snapshot plan input attachments"
```

### Task 4: Make attachment materialization stable and write a failure manifest

**Files:**
- Modify: `server/agent/config_builder.go:32-41, 376-437`
- Modify: `server/agent/config_builder_test.go:241-320`
- Verify call sites: `server/agent/executor.go:505-512`
- Verify call sites: `server/agent/docker_executor.go:203-210`
- Verify call sites: `server/agent/kubernetes_executor.go:228-235`

- [ ] **Step 1: Write failing stable-index and error-manifest tests**

Extend `config_builder_test.go` with an `httptest.Server`: `/first.png` returns HTTP 500 and `/second.png` returns image bytes. Call `DownloadInputAttachments` with `store == nil`, a nil logger in one test, and two real URL attachments. Read manifests with `os.ReadFile` plus `json.Unmarshal` and assert:

```go
require.Equal(t, 1, count)
require.Equal(t, 2, index[0].AttachmentIndex)
require.Equal(t, "attachment_02_second.png", filepath.Base(index[0].Path))
require.Equal(t, "侧面", index[0].Instruction)
require.Equal(t, "up-2", index[0].UploadID)
require.Equal(t, 1, failures[0].AttachmentIndex)
require.Contains(t, failures[0].Error, "500")
require.Equal(t, "正面", failures[0].Instruction)
```

Add a second test with a resume entry at index 1 and a text-backed attachment at index 2; assert only `attachment_02_product.txt` is written. Add a third test that first leaves `errors.json`, reruns with fully successful inputs, and asserts the stale error manifest is removed while `index.json` is still rewritten.

- [ ] **Step 2: Run the focused materialization tests and confirm the red state**

Run:

```bash
cd server
go test ./agent -run 'TestDownloadInputAttachmentsKeepsOriginalIndicesAndWritesErrors|TestDownloadInputAttachmentsSkipsResumeRolesWithoutRenumbering' -count=1
```

Expected: FAIL because successful files are renumbered after failures and `errors.json` is not written.

- [ ] **Step 3: Extend manifest types and materialization logic**

Use these exact types:

```go
type MaterializedInputAttachment struct {
    AttachmentIndex int    `json:"attachment_index"`
    Type            string `json:"type,omitempty"`
    URL             string `json:"url,omitempty"`
    Text            string `json:"text,omitempty"`
    FileName        string `json:"file_name,omitempty"`
    ContentType     string `json:"content_type,omitempty"`
    Size            int64  `json:"size,omitempty"`
    Path            string `json:"path,omitempty"`
    Instruction     string `json:"instruction,omitempty"`
    UploadID        string `json:"upload_id,omitempty"`
}

type MaterializedInputAttachmentError struct {
    AttachmentIndex int    `json:"attachment_index"`
    Type            string `json:"type,omitempty"`
    URL             string `json:"url,omitempty"`
    FileName        string `json:"file_name,omitempty"`
    Instruction     string `json:"instruction,omitempty"`
    UploadID        string `json:"upload_id,omitempty"`
    Error           string `json:"error"`
}
```

Inside the existing `DownloadInputAttachments` loop, preserve `destDir`, `fetchAttachmentBytes`, `inputAttachmentFilename`, `os.WriteFile`, and original `i+1` numbering. Record failures for fetch and write errors without renumbering later successes:

```go
attachmentIndex := i + 1
name := inputAttachmentFilename(attachmentIndex, attachment)
path := filepath.Join(destDir, name)
// Fetch URL bytes or trim text exactly as the current implementation does.
// On failure append MaterializedInputAttachmentError with attachmentIndex,
// Instruction, and UploadID; otherwise write path and append the manifest row.
```

Always serialize `index` with `json.MarshalIndent` and write `index.json`, including an empty array when nothing materializes. When failures exist, serialize and write `errors.json`; otherwise remove a stale `errors.json` and ignore `os.ErrNotExist`. Every warning must be guarded by `if logger != nil`. Keep local, Docker, and Kubernetes call sites invoking this one function.

- [ ] **Step 4: Run agent materialization tests**

Run:

```bash
cd server
go test ./agent -run 'TestDownloadInputAttachments|Test.*Materialize.*Input' -count=1
```

Expected: PASS with original indices, preserved instruction/upload ID, skipped resume roles, and failure details in `errors.json`.

- [ ] **Step 5: Commit stable materialization**

```bash
git add server/agent/config_builder.go server/agent/config_builder_test.go
git commit -m "feat: trace input attachment materialization"
```

### Task 5: Build the reusable Studio reference-material input

**Files:**
- Create: `studio/src/types/input-attachment.ts`
- Modify: `studio/src/types/index.ts`
- Modify: `studio/src/lib/api/ai-entry.ts:4-24`
- Create: `studio/src/components/ReferenceMaterialInput.tsx`
- Create: `studio/src/components/ReferenceMaterialInput.test.tsx`

- [ ] **Step 1: Write failing component interaction tests**

Mock `uploadToOSS` and cover multi-select, drop, preview, annotation, append, remove, upload failure, retry, count validation, and upload-state callbacks:

```tsx
vi.mock('@/lib/direct-upload', () => ({ uploadToOSS: vi.fn() }))

const imageFile = (name: string) => new File(['image'], name, { type: 'image/png' })
const seededAttachments = (count: number): InputAttachment[] => Array.from({ length: count }, (_, index) => ({
  type: 'image', url: `/seed-${index + 1}.png`, file_name: `seed-${index + 1}.png`,
}))

function ControlledReferenceInput() {
  const [value, setValue] = useState<InputAttachment[]>([])
  return <ReferenceMaterialInput value={value} onChange={setValue} allowedTypes={['image']} maxCount={16} instructionEnabled />
}

it('uploads multiple images and emits normalized attachments with instructions', async () => {
  vi.mocked(uploadToOSS)
    .mockResolvedValueOnce({ uploadId: 'u1', key: 'k1', publicUrl: '/a.png', contentType: 'image/png', size: 10 })
    .mockResolvedValueOnce({ uploadId: 'u2', key: 'k2', publicUrl: '/b.png', contentType: 'image/png', size: 20 })
  const onChange = vi.fn()
  render(<ReferenceMaterialInput value={[]} onChange={onChange} allowedTypes={['image']} maxCount={16} instructionEnabled />)
  await userEvent.upload(screen.getByLabelText('添加参考素材'), [imageFile('a.png'), imageFile('b.png')])
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith([
    expect.objectContaining({ type: 'image', url: '/a.png', file_name: 'a.png', upload_id: 'u1', key: 'k1' }),
    expect.objectContaining({ type: 'image', url: '/b.png', file_name: 'b.png', upload_id: 'u2', key: 'k2' }),
  ]))
})

it('edits an instruction by Unicode code points and removes an attachment', async () => {
  const value: InputAttachment[] = [{ type: 'image', url: '/a.png', file_name: 'a.png', instruction: '' }]
  const onChange = vi.fn()
  render(<ReferenceMaterialInput value={value} onChange={onChange} allowedTypes={['image']} instructionEnabled instructionMaxLength={1000} />)
  await userEvent.type(screen.getByLabelText('a.png 的说明'), '只参考配色')
  expect(onChange).toHaveBeenLastCalledWith([{ ...value[0], instruction: '只参考配色' }])
  await userEvent.click(screen.getByRole('button', { name: '删除 a.png' }))
  expect(onChange).toHaveBeenLastCalledWith([])
})

it('retries a failed upload without losing successful attachments', async () => {
  vi.mocked(uploadToOSS).mockRejectedValueOnce(new Error('网络失败')).mockResolvedValueOnce({
    uploadId: 'u1', key: 'k1', publicUrl: '/a.png', contentType: 'image/png', size: 10,
  })
  render(<ControlledReferenceInput />)
  await userEvent.upload(screen.getByLabelText('添加参考素材'), imageFile('a.png'))
  expect(await screen.findByText('网络失败')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('button', { name: '重试 a.png' }))
  expect(await screen.findByAltText('a.png')).toHaveAttribute('src', '/a.png')
})

it('rejects files beyond maxCount before upload', async () => {
  render(<ReferenceMaterialInput value={seededAttachments(16)} onChange={vi.fn()} allowedTypes={['image']} maxCount={16} />)
  await userEvent.upload(screen.getByLabelText('添加参考素材'), imageFile('extra.png'))
  expect(uploadToOSS).not.toHaveBeenCalled()
  expect(screen.getByText('最多添加 16 个参考素材')).toBeInTheDocument()
})
```

- [ ] **Step 2: Run the component test and confirm the red state**

Run:

```bash
cd studio
bun test src/components/ReferenceMaterialInput.test.tsx
```

Expected: FAIL because the shared type and component do not exist.

- [ ] **Step 3: Add the shared TypeScript contract**

Create `studio/src/types/input-attachment.ts`:

```ts
export type InputAttachmentType = 'image' | 'audio' | 'video' | 'document' | 'text'

export interface InputAttachment {
  type: InputAttachmentType
  url?: string
  text?: string
  file_name?: string
  content_type?: string
  size?: number
  role?: string
  upload_id?: string
  key?: string
  instruction?: string
}
```

Export it from `studio/src/types/index.ts`. In `studio/src/lib/api/ai-entry.ts`, remove the local duplicate and import/re-export the shared type so existing callers remain source-compatible:

```ts
import type {
  InputAttachment,
  InputAttachmentType,
} from '@/types/input-attachment'

export type AIEntryAttachment = InputAttachment
export type AIEntryAttachmentType = InputAttachmentType
export type { InputAttachment, InputAttachmentType }
```

- [ ] **Step 4: Implement the controlled upload component**

Use the exact public API:

```ts
export interface ReferenceMaterialInputProps {
  value: InputAttachment[]
  onChange: (value: InputAttachment[]) => void
  allowedTypes: InputAttachmentType[]
  maxCount?: number
  instructionEnabled?: boolean
  instructionMaxLength?: number
  compact?: boolean
  hint?: string
  onUploadingChange?: (uploading: boolean) => void
}
```

Use local transient upload rows keyed by a generated ID while successful server attachments remain controlled by `value`:

```ts
type UploadRow = {
  id: string
  file: File
  progress: number
  status: 'uploading' | 'failed'
  error?: string
}

const typeFromFile = (file: File): InputAttachmentType | null => {
  if (file.type.startsWith('image/')) return 'image'
  if (file.type.startsWith('audio/')) return 'audio'
  if (file.type.startsWith('video/')) return 'video'
  if (file.type.startsWith('text/')) return 'text'
  if (/\.(pdf|docx?|pptx?|xlsx?|json|csv|md|txt)$/i.test(file.name)) return 'document'
  return null
}

const codePointLength = (value: string) => Array.from(value).length
const maxBytesForType = (type: InputAttachmentType) => type === 'document' || type === 'text'
  ? 25 * 1024 * 1024
  : 50 * 1024 * 1024
```

Before creating an upload row, reject a file whose classified type is absent from `allowedTypes`, whose size exceeds `maxBytesForType(type)`, or whose addition would exceed `maxCount`. Upload each accepted file with the existing purpose and preserve all returned metadata:

```ts
const valueRef = useRef(value)
useEffect(() => { valueRef.current = value }, [value])
useEffect(() => { onUploadingChange?.(rows.some((row) => row.status === 'uploading')) }, [rows, onUploadingChange])

const uploadFile = async (row: UploadRow) => {
  setRows((current) => current.map((item) => item.id === row.id ? { ...item, status: 'uploading', error: undefined } : item))
  try {
    const result = await uploadToOSS({
      purpose: 'ai_entry_attachment',
      file: row.file,
      onProgress: (progress) => setRows((current) => current.map((item) => item.id === row.id ? { ...item, progress } : item)),
    })
    const type = typeFromFile(row.file)
    if (!type) throw new Error(`不支持 ${row.file.name} 的文件类型`)
    const nextValue = [...valueRef.current, {
      type,
      url: result.publicUrl,
      file_name: row.file.name,
      content_type: result.contentType,
      size: result.size,
      upload_id: result.uploadId,
      key: result.key,
      instruction: instructionEnabled ? '' : undefined,
    } satisfies InputAttachment]
    valueRef.current = nextValue
    onChange(nextValue)
    setRows((current) => current.filter((item) => item.id !== row.id))
  } catch (error) {
    setRows((current) => current.map((item) => item.id === row.id ? {
      ...item, status: 'failed', error: error instanceof Error ? error.message : '上传失败，请重试',
    } : item))
  }
}
```

The rendered component must include:

```tsx
<input
  aria-label="添加参考素材"
  type="file"
  multiple
  accept={acceptForTypes(allowedTypes)}
  onChange={(event) => void addFiles(Array.from(event.target.files ?? []))}
/>
```

```tsx
{attachment.type === 'image' ? (
  <img src={attachment.url} alt={attachment.file_name || `参考图 ${index + 1}`} className="h-24 w-24 rounded-lg object-cover" />
) : (
  <div aria-label={`${attachment.file_name || `附件 ${index + 1}`} 文件卡片`}>{iconForType(attachment.type)}</div>
)}
```

```tsx
{instructionEnabled && (
  <textarea
    aria-label={`${attachment.file_name || `附件 ${index + 1}`} 的说明`}
    value={attachment.instruction ?? ''}
    placeholder="可选：告诉 AI 这张图是什么，或哪些内容需要保留、忽略。留空也会自动理解。"
    onChange={(event) => {
      const instruction = event.target.value
      if (codePointLength(instruction) <= instructionMaxLength) {
        onChange(value.map((item, itemIndex) => itemIndex === index ? { ...item, instruction } : item))
      }
    }}
  />
)}
```

Implement `dragover`/`drop` on the same drop zone, append rather than replace, validate allowed type/count before starting uploads, display per-row progress/error/retry, and call `onUploadingChange(rows.some(row => row.status === 'uploading'))` from an effect.

- [ ] **Step 5: Run component tests and the Studio type-check build**

Run:

```bash
cd studio
bun test src/components/ReferenceMaterialInput.test.tsx
bun run build
```

Expected: both commands PASS; the build confirms the shared type does not introduce duplicate or circular imports.

- [ ] **Step 6: Commit the shared component**

```bash
git add studio/src/types/input-attachment.ts studio/src/types/index.ts studio/src/lib/api/ai-entry.ts studio/src/components/ReferenceMaterialInput.tsx studio/src/components/ReferenceMaterialInput.test.tsx
git commit -m "feat: add reusable reference material input"
```

### Task 6: Migrate Dashboard without reducing attachment capability

**Files:**
- Modify: `studio/src/pages/DashboardPage.tsx:35-44, 56-62, 139-219, 251-284, 465-500`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`

- [ ] **Step 1: Write failing Dashboard integration tests**

Mock `@/components/ReferenceMaterialInput` with a `vi.hoisted` harness that stores the latest props and renders `<div data-testid="reference-material-input" />`. Keep the existing `render(<DashboardPage />)` setup. In the test, invoke the captured `onChange` with image/audio/video/document/text attachments and then submit through the existing `发送创建任务` button. Assert `api.aiEntry.submit` receives every attachment unchanged, including the image instruction. In a second test invoke `onUploadingChange(true)` inside `act(...)` and assert the existing submit button is disabled. This avoids new form helpers and verifies the page/component contract directly.

- [ ] **Step 2: Run the Dashboard test and confirm the red state**

Run:

```bash
cd studio
bun test src/pages/DashboardPage.ai-entry.test.tsx
```

Expected: FAIL because Dashboard still owns its old uploader and does not expose shared upload state.

- [ ] **Step 3: Replace only the attachment UI/state with the shared component**

Keep the existing prompt, project, platform, local-execution, navigation, error, and submission code. Replace the old file-selection/progress/tag implementation with controlled state:

```tsx
const [attachments, setAttachments] = useState<InputAttachment[]>([])
const [attachmentsUploading, setAttachmentsUploading] = useState(false)
```

```tsx
<ReferenceMaterialInput
  value={attachments}
  onChange={setAttachments}
  allowedTypes={['image', 'audio', 'video', 'document', 'text']}
  instructionEnabled
  hint="可添加图片、音频、视频、文档或文本素材；图片可填写说明。"
  onUploadingChange={setAttachmentsUploading}
/>
```

Pass `attachments` unchanged to `aiEntryApi.submit`. Keep existing reset behavior by calling `setAttachments([])` only after a successful submission. Extend the submit disabled expression:

```tsx
disabled={isSubmitting || attachmentsUploading || !prompt.trim() || !selectedProjectId}
```

Remove Dashboard-only upload helpers and direct `uploadToOSS` imports after migration.

- [ ] **Step 4: Run Dashboard and shared-component regressions**

Run:

```bash
cd studio
bun test src/pages/DashboardPage.ai-entry.test.tsx src/components/ReferenceMaterialInput.test.tsx
```

Expected: PASS with all five existing attachment classes and local execution behavior preserved.

- [ ] **Step 5: Commit the Dashboard migration**

```bash
git add studio/src/pages/DashboardPage.tsx studio/src/pages/DashboardPage.ai-entry.test.tsx
git commit -m "refactor: share dashboard reference input"
```

### Task 7: Integrate Seednote reference materials into manual task creation

**Files:**
- Modify: `studio/src/types/task.ts:1-180`
- Modify: `studio/src/lib/schemas.ts:20-198`
- Modify: `studio/src/pages/TasksPage.tsx:146-160, 364-424, 1039-1077, 1123-1145, 1336-1346`
- Modify: `studio/src/pages/TasksPage.test.tsx`

- [ ] **Step 1: Write failing TasksPage tests**

Reuse the existing `renderTasksPage` helper. Mock `@/components/ReferenceMaterialInput` with a `vi.hoisted` props harness, render the existing creation route as `/tasks?create=true&type=seednote&project_id=<seednote-project>&intent=new`, and invoke the captured `onChange` directly inside `act(...)`. Submit through the real dialog button and assert `api.tasks.create` receives the attachment snapshot. Invoke `onUploadingChange(true)` and assert creation is disabled; inspect the captured props to assert `allowedTypes` is `['image']`, `maxCount` is 16, and instruction support is enabled. Render the e-commerce creation route separately and assert its existing `MultiImageUpload` remains while the shared Seednote input is absent.

- [ ] **Step 2: Run the TasksPage test and confirm the red state**

Run:

```bash
cd studio
bun test src/pages/TasksPage.test.tsx
```

Expected: FAIL because task types/forms/payloads do not contain `input_attachments`.

- [ ] **Step 3: Add task types and schema defaults**

Import the shared type and add the field:

```ts
import type { InputAttachment } from './input-attachment'

export interface Task {
  input_attachments?: InputAttachment[]
}

export interface CreateTaskRequest {
  input_attachments?: InputAttachment[]
}
```

Add to `createTaskSchema`:

```ts
input_attachments: z.array(z.object({
  type: z.enum(['image', 'audio', 'video', 'document', 'text']),
  url: z.string().optional(),
  text: z.string().optional(),
  file_name: z.string().optional(),
  content_type: z.string().optional(),
  size: z.number().optional(),
  role: z.string().optional(),
  upload_id: z.string().optional(),
  key: z.string().optional(),
  instruction: z.string().refine((value) => Array.from(value).length <= 1000, '单张素材说明不能超过 1000 个字符').optional(),
})).max(16, '最多添加 16 张参考图片').default([]),
```

Inside the schema refinement, reject non-image entries when `data.type === 'seednote'`:

```ts
if (data.type === 'seednote' && (data.input_attachments ?? []).some((item) => item.type !== 'image')) {
  ctx.addIssue({ code: 'custom', message: '种草笔记只支持图片参考素材', path: ['input_attachments'] })
}
```

- [ ] **Step 4: Wire defaults, reset, UI, payload, and upload blocking**

Add `input_attachments: []` to form defaults and resets. Add local upload state:

```tsx
const [referenceUploading, setReferenceUploading] = useState(false)
```

Render only in the Seednote section:

```tsx
{selectedType === 'seednote' && (
  <section aria-label="Seednote 参考素材">
    <h3>参考素材</h3>
    <ReferenceMaterialInput
      value={form.watch('input_attachments')}
      onChange={(value) => form.setValue('input_attachments', value, { shouldDirty: true, shouldValidate: true })}
      allowedTypes={['image']}
      maxCount={16}
      instructionEnabled
      instructionMaxLength={1000}
      hint="AI 会先理解创作需求，再逐张分析图片并自动决定每页是否使用。"
      onUploadingChange={setReferenceUploading}
    />
  </section>
)}
```

Leave the existing e-commerce `MultiImageUpload` and video reference components untouched. Add to the request only for Seednote:

```ts
input_attachments: values.type === 'seednote' ? values.input_attachments : undefined,
```

Disable submit while `referenceUploading` is true.

- [ ] **Step 5: Run task form tests and build**

Run:

```bash
cd studio
bun test src/pages/TasksPage.test.tsx
bun run build
```

Expected: PASS; Seednote supports 0-16 images, other task forms do not leak the field, and e-commerce/video upload UX remains unchanged.

- [ ] **Step 6: Commit manual task Studio integration**

```bash
git add studio/src/types/task.ts studio/src/lib/schemas.ts studio/src/pages/TasksPage.tsx studio/src/pages/TasksPage.test.tsx
git commit -m "feat: add seednote task reference materials"
```

### Task 8: Integrate create/edit plan reference snapshots in Studio

**Files:**
- Modify: `studio/src/types/plan.ts:1-100`
- Modify: `studio/src/lib/schemas.ts:200-280`
- Modify: `studio/src/pages/PlansPage.tsx:53-104, 189-199, 235-324, 624-672, 797-801`
- Modify: `studio/src/pages/PlansPage.test.tsx`

- [ ] **Step 1: Write failing plan hydration and payload tests**

Extend the existing `PlansPage.test.tsx` API mocks and render setup. Mock `@/components/ReferenceMaterialInput` with a `vi.hoisted` props harness. For create, open the real Seednote plan dialog, invoke captured `onChange`, submit, and assert `api.plans.create` includes the snapshot. For edit, make `api.plans.list` return a Seednote plan with a saved attachment, click the existing `编辑` button, and assert the captured `value` contains that attachment and instruction. Submit once without invoking `onChange` and assert `api.plans.update` omits `input_attachments`; invoke `onChange([])` and assert an explicit clear sends `[]`; reopen and invoke `onChange([replacement])` to assert replacement. Invoke `onUploadingChange(true)` and assert the save button is disabled.

- [ ] **Step 2: Run the PlansPage test and confirm the red state**

Run:

```bash
cd studio
bun test src/pages/PlansPage.test.tsx
```

Expected: FAIL because plan types/schema/form hydration/payloads omit attachments and update mutation is typed as create.

- [ ] **Step 3: Add plan request types and schema**

```ts
import type { InputAttachment } from './input-attachment'

export interface Plan {
  input_attachments?: InputAttachment[]
}

export interface CreatePlanRequest {
  input_attachments?: InputAttachment[]
}

export interface UpdatePlanRequest {
  input_attachments?: InputAttachment[]
}
```

Add this exact field to `planSchema`:

```ts
input_attachments: z.array(z.object({
  type: z.enum(['image', 'audio', 'video', 'document', 'text']),
  url: z.string().optional(),
  text: z.string().optional(),
  file_name: z.string().optional(),
  content_type: z.string().optional(),
  size: z.number().optional(),
  role: z.string().optional(),
  upload_id: z.string().optional(),
  key: z.string().optional(),
  instruction: z.string().refine((value) => Array.from(value).length <= 1000, '单张素材说明不能超过 1000 个字符').optional(),
})).max(16, '最多添加 16 张参考图片').default([]),
```

Add this refinement to the plan schema:

```ts
if (data.type === 'seednote' && (data.input_attachments ?? []).some((item) => item.type !== 'image')) {
  ctx.addIssue({ code: 'custom', message: '种草笔记只支持图片参考素材', path: ['input_attachments'] })
}
```

- [ ] **Step 4: Hydrate, reset, render, and submit exact plan states**

Add to defaults:

```ts
input_attachments: [],
```

Add to `planToFormValues`:

```ts
input_attachments: plan.input_attachments ?? [],
```

Fix the update mutation signature:

```ts
mutationFn: ({ id, data }: { id: string; data: UpdatePlanRequest }) => plansApi.update(id, data),
```

Track upload state and render this exact Seednote-only block:

```tsx
const [referenceUploading, setReferenceUploading] = useState(false)

{selectedType === 'seednote' && (
  <section aria-label="Seednote 参考素材">
    <h3>参考素材</h3>
    <ReferenceMaterialInput
      value={form.watch('input_attachments')}
      onChange={(value) => form.setValue('input_attachments', value, { shouldDirty: true, shouldValidate: true })}
      allowedTypes={['image']}
      maxCount={16}
      instructionEnabled
      instructionMaxLength={1000}
      hint="AI 会先理解创作需求，再逐张分析图片并自动决定每页是否使用。"
      onUploadingChange={setReferenceUploading}
    />
  </section>
)}
```

When creating, always send the current Seednote array. When updating, use dirty-field state to preserve omission semantics:

```ts
const inputAttachments = values.type === 'seednote'
  ? (isEditing && !form.formState.dirtyFields.input_attachments ? undefined : values.input_attachments)
  : undefined
```

```ts
const payload: CreatePlanRequest | UpdatePlanRequest = {
  cron_expr: values.cron_expr,
  prompt: values.prompt,
  image_model_key: values.image_model_key,
  input_attachments: inputAttachments,
}
```

Clear `input_attachments` in the create-form reset, restore `plan.input_attachments ?? []` in edit hydration, and disable save while reference uploads are in progress.

- [ ] **Step 5: Run plan tests and build**

Run:

```bash
cd studio
bun test src/pages/PlansPage.test.tsx
bun run build
```

Expected: PASS; create includes the snapshot, edit restores it, untouched update omits it, explicit clear sends `[]`, replacement sends the new array, and the update mutation uses `UpdatePlanRequest`.

- [ ] **Step 6: Commit plan Studio integration**

```bash
git add studio/src/types/plan.ts studio/src/lib/schemas.ts studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx
git commit -m "feat: edit plan reference snapshots"
```

### Task 9: Add capability metadata and deterministic image-model resolution

**Files:**
- Modify: `server/config/config.go:55-66, 389-416, 1719-1738`
- Modify: `server/config.yaml:84-125, 386-394`
- Modify: `server/config/model_routes_config_test.go:70-330`
- Modify: `server/service/model_config.go:286-450`
- Modify: `server/service/model_config_resolve_test.go`
- Modify: `server/service/image.go:300-375, 476-507`
- Modify: `server/service/image_test.go:205-326`

- [ ] **Step 1: Write failing config, resolver, and image-service tests**

Extend the existing `TestSemanticModelConfigDerivesRuntimeRoutes` YAML fixture with `quality_rank` on both designer routes. Assert the derived preset copies route `QualityRank`, `SupportsReference`, and `MaxReferenceImages`; do not add a fictional config loader.

Extend `setupResolveTestService` with explicit route capabilities/quality ranks, the existing Volcengine preset, and a compatible OpenAI preset. Create every referenced user in the repository with an explicit tier. Add tests that call the exact signature below with `"content"` or `"cover"`:

```go
ResolveImageModelForGeneration(ctx, userID, imageModelKey, imageType, referenceCount)
```

Cover preferred-model retention at zero references, image-type-specific capability lookup, highest-quality compatible fallback, tier filtering, typed accessible-limit errors, conservative unknown custom models, and equal-rank tie-breaking by preset key. Add an image-service test proving a descriptor resolved for content cannot accidentally build a cover processor with a different provider/model.

- [ ] **Step 2: Run focused resolver tests and confirm the red state**

Run:

```bash
cd server
go test ./config ./service -run 'TestSemanticModelConfigDerivesRuntimeRoutes|TestResolveImageModelForGeneration|TestBuildProcessorForResolved|TestGenerateImageUsesResolvedDescriptor' -count=1
```

Expected: FAIL because quality ranks, copied capabilities, the generation resolver, and the resolved processor path do not exist.

- [ ] **Step 3: Add explicit route quality and copied preset capabilities**

Extend `ImageModelPreset` and `ImageGenerationRouteConfig` with `QualityRank`; keep copied `DesignerProviderCapabilities` on presets. During `resolveImagePresetRoutes`, copy both fields from the designer route. Validate enabled designer routes with `quality_rank > 0`. Set explicit production ranks:

```yaml
seedream:
  quality_rank: 100
gpt_image_2:
  quality_rank: 200
```

A direct/legacy preset without `provider_route` remains rank 0 with conservative zero-value capabilities.

- [ ] **Step 4: Implement the image-type-aware generation resolver**

Add `ImageReferenceLimitError`, `ResolvedImageModel`, a private candidate type, and deterministic descending quality/ascending key sorting. Implement:

```go
func (s *ModelConfigService) ResolveImageModelForGeneration(
    ctx context.Context,
    userID string,
    imageModelKey string,
    imageType string,
    referenceCount int,
) (*ResolvedImageModel, error)
```

`imageType` is required because `ImageAPIConfig.Cover` and `.Content` may resolve to different routes. Normalize empty to `"content"`; reject unsupported values. `resolvePreferredImageCandidate` must reuse strict task-key/tier rules and populate provider/model/capabilities from the selected image-type slot, not always `.Content`. Presets use their copied capabilities. System/custom configs use an image-type-specific lookup:

```go
func (s *ModelConfigService) capabilitiesForImageConfig(cfg *config.ImageAPIConfig, imageType string) (bool, int) {
    apiCfg := cfg.Content
    if imageType == "cover" {
        apiCfg = cfg.Cover
    }
    if apiCfg == nil {
        return false, 0
    }
    provider := providerKind(strings.TrimSpace(apiCfg.Provider))
    modelID := strings.TrimSpace(apiCfg.Model)
    for _, route := range s.cfg.ModelRoutes.ImageGeneration.Designer {
        if providerKind(route.Provider) == provider && route.Model == modelID {
            return route.Capabilities.SupportsReference, route.Capabilities.MaxReferenceImages
        }
    }
    return false, 0
}
```

Resolution policy: retain the preferred descriptor when no references are supplied; retain it when its selected slot supports the requested count; otherwise choose the highest-quality tier-accessible compatible preset; if none fits, return `ImageReferenceLimitError` with the maximum accessible limit or a no-capable-model error. Unknown custom routes never gain reference support by assumption.

- [ ] **Step 5: Refactor only generation to consume the resolved descriptor**

Keep the existing `buildProcessor(ctx, ch, imageType, imageModelKey)` for upload/download auxiliary behavior. Add:

```go
func (s *ImageService) buildProcessorForResolved(
    ch *model.Project,
    imageType string,
    resolved *ResolvedImageModel,
) (*image.Processor, error)
```

Build through the existing project/platform path:

```go
appCfg, err := agent.BuildAppConfig(ch, ResolveStyle(ch, nil), resolved.Config, "", false, "")
```

Select the actual platform API slot via `resolveAppImageAPI`, retaining the existing e-commerce/video fallback rules where applicable. Reject nil/incomplete descriptors. Normalize and compare the selected API provider/model with `resolved.Provider`/`resolved.Model`; return `resolved image model does not match image type configuration` on disagreement.

Change only `GenerateImage` to receive the descriptor and preserve pointer watermark semantics:

```go
func (s *ImageService) GenerateImage(
    ctx context.Context,
    userID, projectID, prompt, imageType, outputPath, refImagePath string,
    refImagePaths []string,
    taskID, size string,
    resolved *ResolvedImageModel,
    watermark *bool,
) (*ImageResult, error)
```

Use `buildProcessorForResolved(ch, imageType, resolved)`. Preserve all current retry, saving, upload, and logging behavior. `ImageResult` already contains `Provider` and `Model`; add only:

```go
SelectionReason    string `json:"selection_reason,omitempty"`
SupportsReference  bool   `json:"supports_reference"`
MaxReferenceImages int    `json:"max_reference_images"`
```

Populate those three fields from the supplied descriptor and verify the generated provider/model match it.

- [ ] **Step 6: Run config/model/image tests**

Run:

```bash
cd server
go test ./config ./service -run 'TestSemanticModelConfig|TestResolveImageModelForGeneration|TestBuildProcessorForResolved|TestGenerateImage' -count=1
```

Expected: PASS; selection is deterministic and image-type-specific, no-reference requests retain the preferred route, reference requests never silently discard references, and generation uses the supplied descriptor without re-resolution.

- [ ] **Step 7: Commit capability-aware resolution**

```bash
git add server/config/config.go server/config.yaml server/config/model_routes_config_test.go server/service/model_config.go server/service/model_config_resolve_test.go server/service/image.go server/service/image_test.go
git commit -m "feat: resolve reference-capable image models"
```

### Task 10: Resolve once across MCP preflight, billing, generation, logging, and profile

**Files:**
- Modify: `server/mcp/image_tools.go:110-310, 1194-1213`
- Modify: `server/mcp/image_tools_test.go:24-67, 305-381`
- Modify: `server/mcp/billing.go:412-443`
- Modify: `server/mcp/billing_test.go`
- Modify: `server/mcp/tools.go:20-45, 375-470`
- Modify: `server/mcp/tools_test.go:963-1018`
- Modify: `server/main.go:621-650`

- [ ] **Step 1: Add concrete MCP test seams and write failing tests**

`Services.ImageSvc` must remain the concrete `*service.ImageService` because upload/compress/download tools call its other methods. Add a narrow generation seam:

```go
type ImageGenerator interface {
    GenerateImage(
        ctx context.Context,
        userID, projectID, prompt, imageType, outputPath, refPath string,
        refPaths []string,
        taskID, size string,
        resolved *service.ResolvedImageModel,
        watermark *bool,
    ) (*service.ImageResult, error)
}
```

Add a narrow billing seam:

```go
type ImageGenerationBillingDecision struct {
    Provider       string
    Model          string
    Source         string
    Dynamic        bool
    DynamicProvider string
    DynamicModel    string
    DynamicRoute    string
}

type ImageGenerationBiller interface {
    PrepareImageGeneration(
        ctx context.Context,
        userID, taskID, imageType string,
        resolved *service.ResolvedImageModel,
    ) (ImageGenerationBillingDecision, error)
}
```

Add `ImageGenerator` and `ImageGenerationBiller` fields to `Services`. In `image_tools_test.go`, define small fakes that capture call counts and the exact descriptor pointer. Build a real task/project using the package's existing test services, call `generateImageHandler` with a real MCP request, and assert: one resolver call; resolver received the actual `imageType` and effective reference count; resolver, biller, and generator saw the same descriptor pointer; returned/logged metadata comes from that descriptor; and a typed 17-versus-16 limit error returns the real maximum. Keep schema rejection tests green.

In `billing_test.go`, replace `TestResolveImageBillingModelUsesResolvedDescriptor` with `TestResolveImageBillingModelUsesResolvedDescriptor` and call:

```go
resolveImageBillingModel(&service.ResolvedImageModel{
    Provider: "openai",
    Model:    "gpt-image-2",
    Source:   "preset:openai-gpt-image",
})
```

- [ ] **Step 2: Run focused MCP tests and confirm the red state**

Run:

```bash
cd server
go test ./mcp -run 'TestGenerateImageResolvesModelOnce|TestGenerateImageReturnsActualReferenceLimit|TestBuildAccountInfoExposesImageGenerationCapability|TestGenerateImageSchemaDoesNotExposeModelSelection|TestGenerateImageRejectsExplicitImageModelKey|TestResolveImageBillingModelUsesResolvedDescriptor' -count=1
```

Expected: FAIL because the handler cannot inject generation/billing separately, resolution lacks image type, and the profile lacks capability metadata.

- [ ] **Step 3: Resolve one descriptor before billing and generation**

Keep and update the resolver interface:

```go
type ImageModelResolver interface {
    ResolveImageModelForGeneration(
        ctx context.Context,
        userID string,
        imageModelKey string,
        imageType string,
        referenceCount int,
    ) (*service.ResolvedImageModel, error)
}
```

After task access validation and local path normalization, count effective references and call the resolver once with the actual `imageType`:

```go
referenceCount := len(refPathsForService)
if referenceCount == 0 && strings.TrimSpace(refPathForService) != "" {
    referenceCount = 1
}
resolved, err := svcs.ImageModelResolver.ResolveImageModelForGeneration(
    ctx, userID, task.ImageModelKey, imageType, referenceCount,
)
```

Return typed limit errors as user-readable tool errors. Pass the same pointer to `ImageGenerationBiller.PrepareImageGeneration`, `ImageGenerator.GenerateImage`, logs, and response. Never resolve from the key again and never expose `resolved.Key`.

- [ ] **Step 4: Implement the concrete descriptor biller**

Change the pure reader to:

```go
func resolveImageBillingModel(resolved *service.ResolvedImageModel) (provider, modelID, source string, err error) {
    if resolved == nil || resolved.Provider == "" || resolved.Model == "" {
        return "", "", "", fmt.Errorf("resolved image model is incomplete")
    }
    return resolved.Provider, resolved.Model, resolved.Source, nil
}
```

Implement a concrete biller that derives provider/model/source only from `resolved`, applies the existing dynamic/static routing and credit-deduction behavior, and returns `ImageGenerationBillingDecision` for logging and later dynamic usage charging. It must not consult task keys or model config. Main wires this biller and assigns `imageSvc` as both concrete `ImageSvc` and `ImageGenerator`.

- [ ] **Step 5: Keep the image tool schema generic**

Use provider-neutral descriptions for `ref_image_path` and `ref_image_paths`; state that the server validates capability and returns the resolved limit. Do not add `image_model_key`, provider/model selectors, `auto/required/excluded`, or any mid-run user decision field.

- [ ] **Step 6: Expose the preferred content descriptor in Seednote profile**

In `tools_test.go`, use `setupAccountInfoTest`, create a Seednote project and task, install a counting resolver fake on `svcs.ImageModelResolver`, and call the existing:

```go
info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
    "project_id": projectID,
    "task_id": taskID,
})
```

Assert `info["image_generation"]` contains provider/model/capabilities/selection reason and no key. Assert exactly one resolver call with `imageType == "content"` and `referenceCount == 0`.

Implement the profile branch with that same call. Keep project-level `reference_image_url` for backward compatibility. Remove e-commerce profile comments/fields that expose task model keys while retaining its separate product-photo workflow.

- [ ] **Step 7: Run all MCP image/billing/profile tests**

Run:

```bash
cd server
go test ./mcp -run 'TestGenerateImage|TestResolveImageBillingModel|TestBuildAccountInfo|TestProjectProfile' -count=1
```

Expected: PASS; exactly one resolution occurs, billing/generation share the same descriptor, limits are recoverable, profile resolution uses content with zero references, and model selection remains server-owned.

- [ ] **Step 8: Commit MCP routing integration**

```bash
git add server/mcp/image_tools.go server/mcp/image_tools_test.go server/mcp/billing.go server/mcp/billing_test.go server/mcp/tools.go server/mcp/tools_test.go server/main.go
git commit -m "feat: route image generation by capability"
```

### Task 11: Update the Seednote Agent/Skill workflow and all runtime distributions

**Files:**
- Modify: `claudecode/agents/seednote.md`
- Modify: `claudecode/skills/seednote/SKILL.md`
- Modify: `claudecode/skills/seednote-visual-design/SKILL.md`
- Modify: `claudecode/skills/seednote-visual-design/references/content.md`
- Modify: `codex/agents/seednote.toml`
- Modify: `codex/skills/seednote/SKILL.md`
- Modify: `codex/skills/seednote-visual-design/SKILL.md`
- Modify: `codex/skills/seednote-visual-design/references/content.md`
- Modify: `openclaw/skills/seednote/SKILL.md`
- Modify: `openclaw/skills/seednote-visual-design/SKILL.md`
- Modify: `openclaw/skills/seednote-visual-design/references/content.md`
- Modify: `server/agent/seednote_skill_contract_test.go`
- Modify: `server/agent/guizang_social_card_contract_test.go`

- [ ] **Step 1: Write failing workflow and distribution contract tests**

Use existing `repoRoot(t)` and `readRepoFile(t, path)`. Add a small concrete helper in `seednote_skill_contract_test.go`:

```go
func extractSeednoteReferenceContract(t *testing.T, body string) string {
    t.Helper()
    const start = "<!-- seednote-reference-contract:start -->"
    const end = "<!-- seednote-reference-contract:end -->"
    startAt := strings.Index(body, start)
    endAt := strings.Index(body, end)
    if startAt < 0 || endAt <= startAt {
        t.Fatalf("missing Seednote reference contract markers")
    }
    section := body[startAt+len(start) : endAt]
    return strings.Join(strings.Fields(section), " ")
}
```

For ordering, use `strings.Index` directly and fail when a later phrase appears before an earlier one. Add assertions for request-first analysis, dynamic per-image prompts, every-image analysis, cross-image conflict analysis, per-output original-path subsets, three-attempt budgets, vision verification, no mid-run user input, removed content/tail bans, and all eight artifacts. For distribution parity, compare only `extractSeednoteReferenceContract(...)` across the Claude Code, Codex, and OpenClaw Seednote Skill files; do not compare entire files because runtime-specific surrounding content is intentionally different.

- [ ] **Step 2: Run contract tests and confirm the red state**

Run:

```bash
cd server
go test ./agent -run 'TestSeednoteWorkflow|TestSeednoteVisualWorkflow|TestSeednoteRuntimeDistributions' -count=1
```

Expected: FAIL because current docs analyze references too late, ban content/tail references, and lack required artifacts/budgets.

- [ ] **Step 3: Add the exact automatic workflow contract to the Seednote Agent/Skill**

Add a normative section with these ordered phases to each runtime copy:

```markdown
<!-- seednote-reference-contract:start -->
## 多参考素材自动决策流程

1. 先读取用户统一提示词、项目资料、`.anban-creator/input-attachments/index.json` 和可选的 `errors.json`，写出 `request-analysis.json` 与 `request-analysis.md`。此阶段不得先分析图片。
2. 遍历 `index.json` 中每张可用图片。针对已完成的需求分析和该图片的可选 `instruction`，动态编写该图片独有的 `analyze_image` prompt；每张可用图片都必须分析，单张最多 3 次理解尝试。`errors.json` 中的条目必须记为 `analysis_failed`；若它是产品身份、Logo、包装、型号或核心结构的唯一证据则停止任务，其他素材能可靠补足时才可继续并记录依据。
3. 写出 `reference-analysis.json` 与 `reference-analysis.md`，记录可见事实、不确定性、需求支持点、可参考维度、必须保持、必须避免、不可推出结论，并完成同产品/系列/型号、新旧包装、角度、事实图/氛围图、Logo/文字/颜色/结构冲突分析。
4. 写出 `image-plan.md`。对每张输出图独立决定使用 0、1 或多张附件，记录附件编号、每张用途、保持项、禁止项。不得把所有素材传给所有页面；超过服务端返回的数量上限时按当页相关性排序选择子集。
5. 写出 `image-prompts.md`。调用 `generate_image` 时只传当前输出图相关的原始路径，数组顺序必须与 prompt 中“参考图 1、参考图 2”一致。不得传分析后的截图、拼图或转码替代原图。
6. 每张生成图片都使用动态编写的 `analyze_image` prompt 核验产品身份、结构、颜色、Logo、包装、文字、虚构部件、版本融合、禁止内容、页面职责和文字可读性，并写入 `image-review.md`。
7. 核验不通过时自动调整参考组合/顺序、生成 prompt、保持项/禁止项、构图复杂度或核验 prompt。每张输出图最多 3 次生成尝试，初次生成计入。不得请求用户决定。
8. 写出 `reference-usage-summary.json`。关键事实无法保证时任务失败；非关键氛围或轻微构图问题可保留并记录 warning。
<!-- seednote-reference-contract:end -->
```

Use bare tool names only.

- [ ] **Step 4: Lock trace artifact schemas and failure policy**

Document this exact JSON shape for `reference-usage-summary.json` in all Seednote Skill copies:

```json
{
  "version": "1.0",
  "inputs": [
    {
      "attachment_index": 1,
      "file_name": "attachment_01_front.png",
      "url": "https://example.invalid/front.png",
      "instruction": "保持包装和 Logo",
      "status": "used",
      "decision_summary": "正面图是产品身份和包装文字的主要证据",
      "analysis_attempts": 1,
      "warnings": []
    }
  ],
  "outputs": [
    {
      "file_name": "cover.png",
      "references": [{ "attachment_index": 1, "purpose": "保持产品身份、包装和 Logo" }],
      "generation_attempts": 2,
      "verification": { "status": "passed", "summary": "产品身份、包装和当页文字核验通过" },
      "provider": "openai",
      "model": "gpt-image-2",
      "selection_reason": "reference_compatible_fallback"
    }
  ],
  "warnings": [],
  "model_fallback_reason": "首选模型的参考图上限不足，服务端选择了兼容模型"
}
```

Document all required artifacts exactly:

```text
request-analysis.json
request-analysis.md
reference-analysis.json
reference-analysis.md
image-plan.md
image-prompts.md
image-review.md
reference-usage-summary.json
```

The docs must distinguish critical failures (unique product identity, Logo, packaging, model, or core structure evidence unavailable; identity/structure hallucination; conflicting versions fused; forbidden content; page cannot serve its role) from warning-only defects (non-critical atmosphere or composition weakness). Preserve already generated files and trace artifacts on failure so resume can continue from the failed phase.

- [ ] **Step 5: Update visual-design content rules and copy all distributions**

Remove unconditional rules that content images or tail images cannot use references. Replace them with:

```markdown
封面、内容图和尾图均不预设是否使用参考素材。每页根据 `image-plan.md` 独立选择 0、1 或多张原图；没有相关参考时使用纯文生图。项目级品牌参考图仍可作为旧数据来源，但不得覆盖本次输入附件中更具体、更新的产品事实。
```

Insert the marked normative block from Step 3 and the schema/failure-policy block from Step 4 into every applicable Claude Code, Codex, and OpenClaw distribution. The marked normative section must be text-equivalent after whitespace normalization, while surrounding runtime-specific content may differ. For `codex/agents/seednote.toml`, place the same contract inside the existing TOML multiline agent prompt; do not change TOML keys or quoting outside that prompt.

- [ ] **Step 6: Run Agent/Skill contracts and parity checks**

Run:

```bash
cd server
go test ./agent -run 'TestSeednote|TestGuizangSocialCard' -count=1
```

Expected: PASS; ordering, every-image analysis, dynamic prompts, per-page subset selection, vision verification, retry budgets, no user decisions, and distribution parity are enforced.

- [ ] **Step 7: Commit the automatic Agent/Skill workflow**

```bash
git add claudecode/agents/seednote.md claudecode/skills/seednote/SKILL.md claudecode/skills/seednote-visual-design/SKILL.md claudecode/skills/seednote-visual-design/references/content.md codex/agents/seednote.toml codex/skills/seednote/SKILL.md codex/skills/seednote-visual-design/SKILL.md codex/skills/seednote-visual-design/references/content.md openclaw/skills/seednote/SKILL.md openclaw/skills/seednote-visual-design/SKILL.md openclaw/skills/seednote-visual-design/references/content.md server/agent/seednote_skill_contract_test.go server/agent/guizang_social_card_contract_test.go
git commit -m "feat: automate seednote reference decisions"
```

### Task 12: Render reference usage safely in TaskDetail

**Files:**
- Modify: `studio/src/types/task.ts`
- Create: `studio/src/components/tasks/ReferenceUsageSummary.tsx`
- Create: `studio/src/components/tasks/ReferenceUsageSummary.test.tsx`
- Modify: `studio/src/pages/TaskDetailPage.tsx:112-127, before generated files at 1265`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`

- [ ] **Step 1: Write failing summary rendering and fallback tests**

In `ReferenceUsageSummary.test.tsx`, define literal fixtures before the tests:

```tsx
const validSummary: ReferenceUsageSummaryData = {
  version: '1.0',
  inputs: [{
    attachment_index: 1,
    file_name: 'front.png',
    instruction: '保持 Logo',
    status: 'used',
    decision_summary: '正面图是产品身份和包装文字的主要证据',
    analysis_attempts: 1,
  }],
  outputs: [{
    file_name: 'cover.png',
    references: [{ attachment_index: 1, purpose: '保持产品身份、包装和 Logo' }],
    generation_attempts: 2,
    verification: { status: 'passed', summary: '产品与文字核验通过' },
    provider: 'openai',
    model: 'gpt-image-2',
    selection_reason: 'reference_compatible_fallback',
  }],
  model_fallback_reason: '首选模型参考图上限不足',
}

const seednoteTask = taskWith({
  id: 'task-1',
  type: 'seednote',
  project_id: 'project-1',
  input_attachments: [],
})

const summaryTaskFile: TaskFile = {
  id: 'file-summary',
  task_id: 'task-1',
  role: 'artifact',
  file_name: 'reference-usage-summary.json',
  mime_type: 'application/json',
  file_size: 1024,
  url: '/tasks/task-1/files/file-summary',
  created_at: '2026-07-10T00:00:00.000Z',
}
```

If the component test is separate from `TaskDetailPage.test.tsx`, define a local complete `Task` literal instead of importing the page-local `taskWith`; in the TaskDetail integration test, reuse the existing `taskWith` helper. Mock the existing `api.tasks.downloadFileBlob`, then cover valid summary rendering, missing-summary first-input fallback, malformed-summary plan fallback, and a TaskDetail render where the summary component fails without hiding progress/generated files.

- [ ] **Step 2: Run component tests and confirm the red state**

Run:

```bash
cd studio
bun test src/components/tasks/ReferenceUsageSummary.test.tsx
```

Expected: FAIL because the type and component do not exist.

- [ ] **Step 3: Add the summary types and safe loader**

Add the `ReferenceUsageSummaryData` interface from “Stable contracts introduced by this plan” to `studio/src/types/task.ts` and export it. Use the existing API:

```ts
api.tasks.downloadFileBlob(taskId, fileId)
```

Implement exact file lookup and guarded parsing:

```tsx
const summaryFile = files.find((file) => file.file_name === 'reference-usage-summary.json')

useEffect(() => {
  let cancelled = false
  if (!summaryFile) return
  void api.tasks.downloadFileBlob(task.id, summaryFile.id)
    .then((blob) => blob.text())
    .then((text) => JSON.parse(text) as unknown)
    .then((value) => {
      if (!isReferenceUsageSummaryData(value)) throw new Error('invalid summary schema')
      if (!cancelled) setSummary(value)
    })
    .catch(() => {
      if (!cancelled) setParseFailed(true)
    })
  return () => { cancelled = true }
}, [task.id, summaryFile?.id])
```

The type guard must require `version === '1.0'`, arrays for `inputs` and `outputs`, valid status enums, numeric attempt counts, and a verification object for each output. It must ignore extra fields.

- [ ] **Step 4: Render readable decisions without exposing reasoning traces**

Render:

```tsx
const originLabel = task.plan_id ? '计划快照' : '首次输入'
```

For valid summaries, show input attachment number/name, `used`/`excluded`/`analysis_failed` badge, `decision_summary`, analysis attempts, warnings, each output's reference numbers/purposes, generation attempts, verification status/summary, provider/model/selection reason, top-level warnings, and model fallback reason. Do not render hidden prompts, chain-of-thought, or raw JSON by default.

For missing/malformed summaries, render `task.input_attachments` with origin label, filename/URL/instruction, and one of these exact warnings:

```text
未找到参考使用摘要，以下仅展示首次输入快照。
未找到参考使用摘要，以下仅展示计划快照。
参考使用摘要无法解析，以下仅展示首次输入快照。
参考使用摘要无法解析，以下仅展示计划快照。
```

Wrap the component in a non-throwing boundary at the TaskDetail integration point so summary failure never hides progress or generated files.

- [ ] **Step 5: Integrate after progress and before generated files**

Reuse the existing task-file query in `TaskDetailPage`; do not create a duplicate fetch. Insert:

```tsx
<ReferenceUsageSummary task={task} files={taskFiles ?? []} />
```

between the progress/execution section and the generated-file card section beginning near current line 1265.

- [ ] **Step 6: Run TaskDetail and component tests**

Run:

```bash
cd studio
bun test src/components/tasks/ReferenceUsageSummary.test.tsx src/pages/TaskDetailPage.test.tsx
bun run build
```

Expected: PASS; valid, missing, malformed, first-input, and plan-snapshot states render without breaking the rest of TaskDetail.

- [ ] **Step 7: Commit the trace summary UI**

```bash
git add studio/src/types/task.ts studio/src/components/tasks/ReferenceUsageSummary.tsx studio/src/components/tasks/ReferenceUsageSummary.test.tsx studio/src/pages/TaskDetailPage.tsx studio/src/pages/TaskDetailPage.test.tsx
git commit -m "feat: show reference usage summaries"
```

### Task 13: Run full regressions, parity checks, builds, and version assertions

**Files:**
- Verify: all files changed in Tasks 1-12
- Verify: runtime plugin manifests containing Claude Code, Codex, and OpenClaw versions

- [ ] **Step 1: Format all changed Go files**

Run:

```bash
gofmt -w \
  server/model/entry_attachment.go server/model/plan.go \
  server/handler/input_attachment.go server/handler/input_attachment_test.go \
  server/handler/ai_entry.go server/handler/task.go server/handler/task_test.go \
  server/handler/plan.go server/handler/plan_test.go \
  server/service/task.go server/service/task_test.go \
  server/service/ai_entry.go server/service/ai_entry_test.go \
  server/service/plan.go server/service/plan_test.go \
  server/service/model_config.go server/service/model_config_resolve_test.go \
  server/service/image.go server/service/image_test.go \
  server/config/config.go server/config/model_routes_config_test.go \
  server/agent/config_builder.go server/agent/config_builder_test.go \
  server/agent/seednote_skill_contract_test.go server/agent/guizang_social_card_contract_test.go \
  server/mcp/image_tools.go server/mcp/image_tools_test.go server/mcp/billing.go server/mcp/billing_test.go \
  server/mcp/tools.go server/mcp/tools_test.go server/main.go
```

Expected: command exits 0 and formats every Go file created or modified by Tasks 1-12.

- [ ] **Step 2: Run the full Go suite**

Run:

```bash
cd server
go test ./... -count=1
```

Expected: PASS with no package failures. A failure is returned to the owning task before proceeding; do not suppress, skip, or weaken a test.

- [ ] **Step 3: Run the full Studio suite and production build**

Run:

```bash
cd studio
bun test
bun run build
```

Expected: all Vitest files PASS and `tsc -b && vite build` exits 0.

- [ ] **Step 4: Assert runtime distribution parity and required workflow language**

Run:

```bash
rg -n 'request-analysis\.json|reference-analysis\.json|reference-usage-summary\.json|每张输入图最多 3 次理解尝试|每张输出图最多 3 次生成尝试|不得向用户发起中途确认' \
  claudecode/agents/seednote.md \
  claudecode/skills/seednote/SKILL.md \
  codex/agents/seednote.toml \
  codex/skills/seednote/SKILL.md \
  openclaw/skills/seednote/SKILL.md
```

Expected: every required phrase/artifact appears in every applicable distribution; contract tests from Task 11 remain the authoritative parity check.

- [ ] **Step 5: Assert no model selector leaks into the image tool schema**

Run:

```bash
cd server
go test ./mcp -run 'TestGenerateImageSchemaDoesNotExposeModelSelection|TestGenerateImageRejectsExplicitImageModelKey|TestResolveImageBillingModelUsesResolvedDescriptor' -count=1
```

Expected: PASS.

- [ ] **Step 6: Assert plugin versions without changing them**

Run:

```bash
rg -n '2\.10\.55|2\.10\.49|2\.7\.41' claudecode codex openclaw
```

Expected: manifests/configuration still identify Claude Code `2.10.55`, Codex `2.10.49`, and OpenClaw `2.7.41`. Do not create a version-only edit because these are already the approved versions at planning time.

- [ ] **Step 7: Execute the ten end-to-end acceptance scenarios**

Using one Seednote project, run and record task IDs for:

```text
1. Multiple product angles; cover chooses the front image.
2. Internal-structure image is used only on its matching selling-point page.
3. Scene image contributes atmosphere without replacing the product identity.
4. Old packaging is excluded.
5. All per-image instructions are empty and analysis still completes.
6. A page needing no reference uses text-to-image automatically.
7. Conflicting versions are not merged.
8. The same plan run with a new topic recomputes its reference strategy.
9. Critical image analysis failure stops instead of fabricating facts.
10. The whole run completes without a user confirmation step.
```

For each task, verify the eight required artifacts exist, `reference-usage-summary.json` matches version `1.0`, TaskDetail renders the summary, operation billing shows real analysis/generation/verification consumption, and generated files remain available after a terminal failure.

- [ ] **Step 8: Commit only verification-driven corrections, if any**

If Steps 1-7 required a concrete correction, stage only the files changed for that correction and use a focused message:

```bash
git diff --check
git status --short
git diff --name-only --diff-filter=ACMRTUXB -z | xargs -0 git add --
git commit -m "fix: close multi-reference regression gaps"
```

Expected: `git diff --check` exits 0. If no correction was needed, do not create an empty commit.

## Final specification coverage checklist

- Tasks 1-3 cover the shared attachment contract, instruction normalization/limit, all three server entry points, plan omitted/empty/replace semantics, old data compatibility, and independent plan-triggered snapshots.
- Task 4 covers stable original numbering, instruction/upload ID preservation, failure manifests, resume-role exclusion, and one materializer for local/Docker/Kubernetes.
- Tasks 5-8 cover one reusable Studio component, Dashboard attachment parity, thumbnails/cards/instructions/progress/retry/append/remove, upload blocking, Seednote 16-image rules, manual tasks, plan create/edit hydration, and separation from video/e-commerce inputs.
- Tasks 9-10 cover server-owned model selection, deterministic quality ranking, tier/capability limits, compatible fallback, conservative custom behavior, one descriptor for preflight/billing/generation/logging, generic tool schema wording, and profile capability disclosure without model keys.
- Task 11 covers request-first analysis, dynamic per-image prompts, every-image analysis, cross-image conflicts, per-output subsets, original paths, dynamic vision verification, automatic retry budgets, critical/warning failure policy, all trace artifacts, no mid-run user decisions, and three-runtime parity.
- Task 12 covers readable TaskDetail provenance and usage summaries with safe fallback and no chain-of-thought.
- Task 13 covers full regressions, builds, model-selector protections, version assertions, billing visibility, and all ten end-to-end scenarios.
