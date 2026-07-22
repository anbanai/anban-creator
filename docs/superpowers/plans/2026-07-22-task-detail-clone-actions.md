# Task Detail Clone And Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make task-detail actions visible, keep resume as the only prompt-only workflow, and make clone open the same full editable form as task creation with every source parameter prefilled.

**Architecture:** Extract the large task-creation dialog from `TasksPage` into one reusable `TaskFormDialog` backed by pure form-default and request-mapping helpers. Extend the existing clone endpoint with an optional full override contract while retaining the active exact-clone branch used by bulk clone; record both source task and source project for narrowly authorized immutable input reuse.

**Tech Stack:** React 19, TypeScript, React Hook Form, Zod, TanStack Query, Base UI/shadcn components, Vitest/Testing Library, Go Fiber v3, GORM, MySQL migration SQL.

---

## File Map

- Create `studio/src/lib/task-form.ts`: pure create defaults, clone defaults, resume-attachment filtering, and form-to-request mapping.
- Create `studio/src/lib/task-form.test.ts`: table-driven coverage for every task type and project-switch defaults.
- Create `studio/src/components/tasks/ImageAspectRatioField.tsx`: visual four-option ratio control.
- Create `studio/src/components/tasks/ImageAspectRatioField.test.tsx`: accessibility and selection contract.
- Create `studio/src/components/tasks/TaskFormDialog.tsx`: the single create/clone dialog, form state, uploads, cost preview, and mode-specific submission.
- Create `studio/src/components/tasks/TaskFormDialog.test.tsx`: shared rendering, clone prefill, project switching, error retention, and submit payload tests.
- Modify `studio/src/pages/TasksPage.tsx`: remove inline form code and render `TaskFormDialog` in create mode.
- Modify `studio/src/pages/TasksPage.test.tsx`: protect the create-page integration after extraction.
- Modify `studio/src/pages/TaskDetailPage.tsx`: visible header actions, published checkbox, and clone-mode dialog.
- Modify `studio/src/pages/TaskDetailPage.test.tsx`: visible-action, resume isolation, published toggle, and clone integration tests.
- Modify `studio/src/types/task.ts`: declare persisted task configuration fields and full clone request types.
- Modify `studio/src/lib/api/tasks.ts`: send full clone overrides.
- Modify `studio/src/lib/api/tasks.test.ts`: protect clone request serialization.
- Modify `server/model/task.go`: persist internal source-project provenance.
- Modify `server/service/task.go`: carry source-project provenance into new tasks.
- Modify `server/service/task_retry.go`: support exact clone and full editable overrides.
- Modify `server/service/task_test.go`: cover all override fields, quantity, and exact-clone behavior.
- Modify `server/handler/task.go`: bind and validate the full clone request using shared creation validation.
- Modify `server/handler/task_test.go`: cover request validation, authorization, billing, and response behavior.
- Modify `server/service/agent_bootstrap.go`: authorize source objects against the recorded source project.
- Modify `server/service/agent_bootstrap_test.go`: cover same-project, cross-project, and cross-user prefixes.
- Create `server/migrations/20260722_clone_input_source_project.sql`: add the source-project column and index.
- Modify `server/migrations/migrations_test.go`: assert migration/model parity.

### Task 1: Persist Narrow Clone Input Provenance

**Files:**
- Modify: `server/model/task.go`
- Modify: `server/service/task.go`
- Modify: `server/service/task_retry.go`
- Modify: `server/service/agent_bootstrap.go`
- Test: `server/service/task_test.go`
- Test: `server/service/agent_bootstrap_test.go`
- Create: `server/migrations/20260722_clone_input_source_project.sql`
- Modify: `server/migrations/migrations_test.go`

- [ ] **Step 1: Write failing service and bootstrap tests**

Add a clone assertion that both root source identifiers survive a clone:

```go
if clone.InputSourceTaskID != src.ID {
    t.Fatalf("input source task = %q, want %q", clone.InputSourceTaskID, src.ID)
}
if clone.InputSourceProjectID != src.ProjectID {
    t.Fatalf("input source project = %q, want %q", clone.InputSourceProjectID, src.ProjectID)
}
```

Add a cross-project bootstrap case whose destination is `project-2`, whose recorded source is `project-1`, and whose only accepted task-scoped key is:

```go
task := &model.Task{
    ID: "clone-task", UserID: "user-1", ProjectID: "project-2",
    InputSourceTaskID: "source-task", InputSourceProjectID: "project-1",
}
allowedKey := "uploads/users/user-1/projects/project-1/tasks/source-task/inputs/reference.png"
```

Reject keys using another user, another source project, another source task, or the destination project combined with the source task ID.

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
go test ./server/service -run 'TestTaskService_Clone|TestBootstrapDownloadSigningAllowsExplicitCloneInputSource' -count=1
```

Expected: compilation fails because `InputSourceProjectID` does not exist.

- [ ] **Step 3: Add the model, service, and authorization implementation**

Add the internal-only model field:

```go
InputSourceProjectID string `gorm:"type:char(36);index" json:"-"`
```

Add the matching `CreateManualParams` field and assign it when constructing `model.Task`. In exact clone, preserve the root pair when present; otherwise seed both fields from `src.ID` and `src.ProjectID`.

Authorize only the recorded tuple:

```go
if sourceTaskID := strings.TrimSpace(task.InputSourceTaskID); sourceTaskID != "" {
    sourceProjectID := strings.TrimSpace(task.InputSourceProjectID)
    if sourceProjectID == "" {
        sourceProjectID = task.ProjectID
    }
    sourcePrefix := path.Join(
        "uploads/users", task.UserID, "projects", sourceProjectID, "tasks", sourceTaskID,
    ) + "/"
    if strings.HasPrefix(key, sourcePrefix) {
        return nil
    }
}
```

The empty source-project fallback remains required for already persisted clones created by the active exact-clone path before this column exists.

- [ ] **Step 4: Add and test the release migration**

Create:

```sql
ALTER TABLE `tasks`
  ADD COLUMN `input_source_project_id` char(36) NOT NULL DEFAULT '',
  ADD KEY `idx_tasks_input_source_project_id` (`input_source_project_id`);
```

Add a migration contract test that reads the file and asserts both fragments plus GORM field/index parity.

- [ ] **Step 5: Run focused tests and verify GREEN**

Run:

```bash
go test ./server/service ./server/migrations -run 'TestTaskService_Clone|TestBootstrapDownloadSigningAllowsExplicitCloneInputSource|TestCloneInputSourceProjectMigration' -count=1
```

Expected: all selected tests pass.

- [ ] **Step 6: Commit**

```bash
git add server/model/task.go server/service/task.go server/service/task_retry.go server/service/task_test.go server/service/agent_bootstrap.go server/service/agent_bootstrap_test.go server/migrations/20260722_clone_input_source_project.sql server/migrations/migrations_test.go
git commit -m "feat(server): track clone input source project"
```

### Task 2: Add Full Editable Clone Overrides

**Files:**
- Modify: `server/handler/task.go`
- Modify: `server/service/task_retry.go`
- Test: `server/handler/task_test.go`
- Test: `server/service/task_test.go`

- [ ] **Step 1: Write failing service tests for full overrides**

Add a table-driven test that creates source and destination projects, then calls:

```go
prompt := "edited prompt"
skipReference := false
watermark := true
hasContent := false
hasTail := true
articleCover := false
articleContent := true
overrides := &service.CloneTaskOverrides{
    ProjectID: destination.ID,
    Quantity: 2,
    Prompt: prompt,
    ImageRatio: "16:9",
    ImageModelKey: "seedream-4",
    SkipRefImage: &skipReference,
    ReferenceImageAssetID: "asset-2",
    InputAttachments: []model.EntryAttachment{{Type: "text", Text: "brief", Role: "brief"}},
    Watermark: &watermark,
    Goal: "include evidence",
    GoalMode: true,
    HasContentImage: &hasContent,
    HasTailImage: &hasTail,
    ArticleWithCover: &articleCover,
    ArticleWithContentImages: &articleContent,
    Ecommerce: &model.EcommerceConfig{TargetPlatform: "tmall"},
    MontageInput: &model.MontageInput{Brief: "edited montage"},
    ExecutionTarget: model.ExecutionTargetCloud,
}
tasks, err := svc.Clone(ctx, src.ID, service.CloneTaskParams{Overrides: overrides})
```

Assert two tasks, destination project/type/current snapshot, every supplied field, and source task/project provenance. Keep the existing no-overrides test unchanged to prove exact clone still works.

- [ ] **Step 2: Run the service test and verify RED**

```bash
go test ./server/service -run 'TestTaskService_Clone' -count=1
```

Expected: compilation fails because `CloneTaskOverrides` and multi-task clone results do not exist.

- [ ] **Step 3: Implement service override branching**

Define:

```go
type CloneTaskParams struct {
    Prompt           *string
    InputAttachments *[]model.EntryAttachment
    Overrides        *CloneTaskOverrides
}

type CloneTaskOverrides struct {
    ProjectID string
    Quantity int
    Prompt string
    ImageRatio string
    ImageModelKey string
    SkipRefImage *bool
    ReferenceImageAssetID string
    InputAttachments []model.EntryAttachment
    Watermark *bool
    Goal string
    GoalMode bool
    HasContentImage *bool
    HasTailImage *bool
    ArticleWithCover *bool
    ArticleWithContentImages *bool
    Ecommerce *model.EcommerceConfig
    MontageInput *model.MontageInput
    ExecutionTarget string
}
```

Change `Clone` to return `[]*model.Task`. Keep the existing source-copy parameter construction when `Overrides == nil`; otherwise construct `CreateManualParams` from the validated overrides, current destination project snapshot, requested quantity, and root input source pair. Update the single-clone handler to return `tasks[0]` and the bulk-clone handler to record `tasks[0].ID`; both reject an unexpected empty result.

- [ ] **Step 4: Write failing handler request tests**

Post a complete JSON body containing `project_id`, `quantity`, all shared fields, and one type-specific field. Assert the service-created task contains the edited values. Add cases for inactive/foreign project, quantity `0` and `6`, unavailable model, invalid goal, unsafe attachment, inaccessible reference asset, and insufficient balance. Assert no task row is created for failures.

Also post the current `{prompt,input_attachments}` body and assert it remains the exact-clone branch.

- [ ] **Step 5: Run handler tests and verify RED**

```bash
go test ./server/handler -run 'TestCloneTask' -count=1
```

Expected: full override values are ignored or rejected by the existing narrow request contract.

- [ ] **Step 6: Implement full handler binding and shared validation**

Extend `cloneTaskRequest` with the same task-specific fields as `createTaskRequest`, using `project_id != ""` as the explicit full-override discriminator. Extract shared request preparation helpers from `Create` for:

```go
func (h *TaskHandler) resolveTaskCreationInput(
    c fiber.Ctx,
    userID string,
    req createTaskRequest,
    source *model.Task,
) (*preparedTaskCreation, error)
```

The prepared value contains the resolved project/snapshot, validated reference asset, normalized/finalized attachments, ecommerce config, validated Montage input, and scalar fields. `Create` calls it with `source=nil`; full clone calls it with the owned source task so reused source assets are permitted only through that provenance. Pass `CloneTaskOverrides` to the service and return the first task while preserving the current API envelope.

- [ ] **Step 7: Run handler and service tests and verify GREEN**

```bash
go test ./server/handler ./server/service -run 'TestCloneTask|TestTaskService_Clone' -count=1
```

Expected: all selected tests pass.

- [ ] **Step 8: Commit**

```bash
git add server/handler/task.go server/handler/task_test.go server/service/task_retry.go server/service/task_test.go
git commit -m "feat(server): support editable task clones"
```

### Task 3: Define Pure Shared Task Form Mapping

**Files:**
- Create: `studio/src/lib/task-form.ts`
- Create: `studio/src/lib/task-form.test.ts`
- Modify: `studio/src/types/task.ts`
- Modify: `studio/src/lib/api/tasks.ts`
- Modify: `studio/src/lib/api/tasks.test.ts`

- [ ] **Step 1: Write failing mapping and API tests**

Define tests for `createTaskFormDefaults(project)`, `cloneTaskFormDefaults(task)`, and `taskFormValuesToRequest(values)`. The clone table covers article, seednote, ecommerce, and Montage tasks. Assert:

```ts
expect(cloneTaskFormDefaults(task)).toMatchObject({
  project_id: task.project_id,
  type: task.type,
  quantity: 1,
  prompt: task.prompt,
  image_ratio: task.image_ratio,
  image_model_key: task.image_model_key,
  watermark: task.watermark,
  goal: task.goal,
  goal_mode: task.goal_mode,
  has_content_image: task.has_content_image,
  has_tail_image: task.has_tail_image,
  article_with_cover: task.article_with_cover,
  article_with_content_images: task.article_with_content_images,
})
```

Assert `resume_latest` and `resume_file` attachments are absent, source arrays are not mutated, ecommerce/Montage objects are copied, and `api.tasks.clone(id, request)` posts the complete request unchanged.

- [ ] **Step 2: Run tests and verify RED**

```bash
cd studio && bun run test -- src/lib/task-form.test.ts src/lib/api/tasks.test.ts
```

Expected: imports or assertions fail because the helpers and full clone request do not exist.

- [ ] **Step 3: Add persisted fields and form contracts**

Extend `Task` with:

```ts
watermark?: boolean
has_content_image?: boolean
has_tail_image?: boolean
article_with_cover?: boolean
article_with_content_images?: boolean
```

Export:

```ts
export type CloneTaskRequest = CreateTaskRequest

export interface TaskFormDefaults extends CreateTaskFormValues {
  quantity: number
  watermark: boolean
  goal: string
  goal_mode: boolean
  has_content_image: boolean
  has_tail_image: boolean
  article_with_cover: boolean
  article_with_content_images: boolean
}
```

Implement pure default/mapping helpers. Clone attachments must be shallow-copied and filtered by role. Reference images map to `{asset_id: task.reference_image.asset_id}`. Project changes use `getProjectCreationDefaults(project)` and reset type-specific fields to that project's defaults.

- [ ] **Step 4: Update the clone API type and verify GREEN**

Change `CloneTaskRequest` in `studio/src/lib/api/tasks.ts` to the shared exported type and keep:

```ts
clone: (id: string, data: CloneTaskRequest) =>
  unwrap<Task>(http.post(`/tasks/${id}/clone`, data))
```

Run:

```bash
cd studio && bun run test -- src/lib/task-form.test.ts src/lib/api/tasks.test.ts
```

Expected: all selected tests pass.

- [ ] **Step 5: Commit**

```bash
git add studio/src/lib/task-form.ts studio/src/lib/task-form.test.ts studio/src/types/task.ts studio/src/lib/api/tasks.ts studio/src/lib/api/tasks.test.ts
git commit -m "feat(studio): define shared task form mapping"
```

### Task 4: Add the Visual Aspect Ratio Control

**Files:**
- Create: `studio/src/components/tasks/ImageAspectRatioField.tsx`
- Create: `studio/src/components/tasks/ImageAspectRatioField.test.tsx`

- [ ] **Step 1: Write the failing component test**

Render with value `3:4`, default `3:4`, and an `onChange` spy. Assert four radio options, accessible names `3:4 vertical default`, `1:1 square`, `4:3 horizontal`, and `16:9 widescreen`; assert only `3:4` is checked; click `16:9` and assert `onChange('16:9')`.

- [ ] **Step 2: Run the test and verify RED**

```bash
cd studio && bun run test -- src/components/tasks/ImageAspectRatioField.test.tsx
```

Expected: module import fails.

- [ ] **Step 3: Implement the stable radio group**

Export:

```tsx
export function ImageAspectRatioField({
  value,
  defaultValue,
  onChange,
}: {
  value: '' | '3:4' | '1:1' | '4:3' | '16:9'
  defaultValue: string
  onChange: (value: '3:4' | '1:1' | '4:3' | '16:9') => void
})
```

Use a `role="radiogroup"` container and four fixed-size buttons with `role="radio"`, `aria-checked`, a CSS `aspect-ratio` preview, visible ratio/orientation text, and a `Default` suffix only for `defaultValue`. Use `rounded-md`, stable grid tracks, and no viewport-scaled type.

- [ ] **Step 4: Run the test and verify GREEN**

```bash
cd studio && bun run test -- src/components/tasks/ImageAspectRatioField.test.tsx
```

Expected: test passes.

- [ ] **Step 5: Commit**

```bash
git add studio/src/components/tasks/ImageAspectRatioField.tsx studio/src/components/tasks/ImageAspectRatioField.test.tsx
git commit -m "feat(studio): add visual image ratio control"
```

### Task 5: Extract One Create/Clone Task Form Dialog

**Files:**
- Create: `studio/src/components/tasks/TaskFormDialog.tsx`
- Create: `studio/src/components/tasks/TaskFormDialog.test.tsx`
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`

- [ ] **Step 1: Write failing shared-dialog tests**

Render create and clone modes with the same project/model/billing mocks. Assert both modes expose project, prompt, attachments, quantity, visual ratio control, searchable image model, reference image, watermark, goal mode, and type-specific controls. Assert the removed `01 Type` panel is absent.

For clone mode, assert copied defaults render and submitting calls the clone API:

```ts
expect(api.tasks.clone).toHaveBeenCalledWith('source-task', expect.objectContaining({
  project_id: 'project-1',
  prompt: 'source prompt',
  quantity: 1,
  image_ratio: '16:9',
  image_model_key: 'seedream-4',
}))
```

Change the project and assert its platform/default ratio/model/module values replace incompatible source values. Reject submission and assert the dialog remains open with the edited prompt.

- [ ] **Step 2: Run tests and verify RED**

```bash
cd studio && bun run test -- src/components/tasks/TaskFormDialog.test.tsx
```

Expected: module import fails.

- [ ] **Step 3: Extract the dialog without changing create behavior**

Move the form, attachment lifecycle, upload guards, billing preview, image model query, local executor state, dirty-close confirmation, and type-specific panels out of `TasksPage`. Define:

```tsx
interface TaskFormDialogProps {
  open: boolean
  mode: 'create' | 'clone'
  sourceTask?: Task
  initialProjectId?: string
  initialType?: TaskType
  onOpenChange: (open: boolean) => void
  onCreated: (task: Task, quantity: number) => void
}
```

The component queries active projects, wallet, catalog, and model options with the existing query keys. It calls `api.tasks.create` in create mode and `api.tasks.clone(sourceTask.id, request)` in clone mode. It uses `TaskFormDefaults` as the only reset source and `taskFormValuesToRequest` as the only payload builder.

- [ ] **Step 4: Apply the approved dialog UX**

Remove the static type panel. Keep the project selector and type badge in the prompt context bar. Replace the ratio `Select` with `ImageAspectRatioField`. Put ratio and `ImageModelSelector` in:

```tsx
<div className="grid gap-4 md:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
  <ImageAspectRatioField
    value={ratioField.value || ''}
    defaultValue={projectImageRatio || platformDefaultRatio[watchedType] || '3:4'}
    onChange={ratioField.onChange}
  />
  <ImageModelSelector
    options={imageModelOptions}
    value={modelField.value || ''}
    onChange={modelField.onChange}
    disabled={imageModelsLoading}
  />
</div>
```

Keep all remaining controls and pricing text behaviorally identical.

- [ ] **Step 5: Replace the inline `TasksPage` dialog**

Keep list filters, bulk actions, and cards in `TasksPage`. Replace its form state and inline dialog with:

```tsx
<TaskFormDialog
  open={modalOpen}
  mode="create"
  initialProjectId={createIntent.projectId}
  initialType={createIntent.type}
  onOpenChange={setModalOpen}
  onCreated={(task) => navigate(`/tasks/${task.id}`)}
/>
```

Update page tests to assert `New task` opens the shared dialog and successful creation navigates to the created task.

- [ ] **Step 6: Run component and page tests and verify GREEN**

```bash
cd studio && bun run test -- src/components/tasks/TaskFormDialog.test.tsx src/pages/TasksPage.test.tsx
```

Expected: all selected tests pass with no React act warnings.

- [ ] **Step 7: Commit**

```bash
git add studio/src/components/tasks/TaskFormDialog.tsx studio/src/components/tasks/TaskFormDialog.test.tsx studio/src/pages/TasksPage.tsx studio/src/pages/TasksPage.test.tsx
git commit -m "refactor(studio): share task creation dialog"
```

### Task 6: Expose Task Detail Actions and Full Clone Dialog

**Files:**
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`

- [ ] **Step 1: Write failing visible-action tests**

For a completed unpublished task, assert visible buttons `Continue`, `Clone task`, `Delete`, and an unchecked `Published` checkbox. Assert no `More task actions` trigger exists. For completed published, assert the checkbox is checked. For cancelled, assert `Continue`, `Clone task`, and `Delete`. For failed, assert the contextual `Add information and continue` action plus header `Clone task` and `Delete`, with no duplicate header resume button. For pending/running, assert only the valid cancellation control.

- [ ] **Step 2: Write failing published-checkbox tests**

Click checked and unchecked states and assert:

```ts
expect(api.tasks.markPublished).toHaveBeenCalledWith('task-1', true)
expect(api.tasks.markPublished).toHaveBeenCalledWith('task-1', false)
```

Use a deferred mutation to prove the checkbox is disabled while pending. Reject it and assert the server-backed checked state remains unchanged and an error toast appears.

- [ ] **Step 3: Write failing clone/resume isolation tests**

Click `Clone task` and assert the full shared dialog contains project, quantity, ratio, and model controls prefilled from the source. Click `Continue` separately and assert only `ResumeTaskDialog` appears with the empty supplemental prompt. Preserve the existing regression assertion that the latest composer value reaches `api.tasks.resume`.

- [ ] **Step 4: Run the page test and verify RED**

```bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx
```

Expected: tests fail because actions remain in the overflow menu and clone is prompt-only.

- [ ] **Step 5: Implement the approved header and dialog behavior**

Delete `CloneTaskDialog`, its snapshot state, and dropdown imports. Render the completed-state checkbox using the existing checkbox primitive and label it `Published`. Render visible `Button` controls in the approved order, with clone as outline and delete as destructive outline. Keep responsive wrapping.

Render:

```tsx
<TaskFormDialog
  open={showCloneDialog}
  mode="clone"
  sourceTask={task}
  onOpenChange={setShowCloneDialog}
  onCreated={(cloned, quantity) => {
    toast.success(quantity > 1 ? `${quantity} tasks cloned` : 'Task cloned')
    navigate(`/tasks/${cloned.id}`)
  }}
/>
```

Keep `ResumeTaskDialog` unchanged except for imports affected by removing clone-only code.

- [ ] **Step 6: Run the page test and verify GREEN**

```bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx
```

Expected: all page tests pass with the request-level resume regression intact.

- [ ] **Step 7: Commit**

```bash
git add studio/src/pages/TaskDetailPage.tsx studio/src/pages/TaskDetailPage.test.tsx
git commit -m "feat(studio): expose task detail actions"
```

### Task 7: Full Verification and Rendered QA

**Files:**
- Modify only files required to fix failures attributable to Tasks 1-6.

- [ ] **Step 1: Format and run focused verification**

```bash
gofmt -w server/model/task.go server/service/task.go server/service/task_retry.go server/service/agent_bootstrap.go server/handler/task.go server/service/task_test.go server/service/agent_bootstrap_test.go server/handler/task_test.go server/migrations/migrations_test.go
go test ./server/migrations ./server/service ./server/handler -count=1
cd studio && bun run test -- src/lib/task-form.test.ts src/lib/api/tasks.test.ts src/components/tasks/ImageAspectRatioField.test.tsx src/components/tasks/TaskFormDialog.test.tsx src/pages/TasksPage.test.tsx src/pages/TaskDetailPage.test.tsx
```

Expected: all commands exit `0` with no unexpected warnings.

- [ ] **Step 2: Run repository-wide verification**

Use the bundled Studio runtime if host Node fails:

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run build
```

Expected: all tests and builds exit `0`.

- [ ] **Step 3: Run Browser QA**

Start the Studio dev server with Bun and use the in-app Browser. The flow under test is: terminal task detail -> inspect visible header actions -> toggle published -> open resume -> close -> open full clone -> verify copied settings -> switch project -> submit or cancel -> open delete confirmation.

Check desktop and mobile viewports for page identity, meaningful DOM, no framework overlay, console health, no overlap, stable action wrapping, correct checkbox state, ratio selection, searchable model control, and preserved form values after a simulated API error. Capture screenshots for desktop header, clone image settings, and mobile header.

- [ ] **Step 4: Review the final diff**

```bash
git diff --check
git status --short
git diff --stat HEAD~6..HEAD
```

Expected: no whitespace errors; only files named in this plan plus failure-driven corrections are changed; unrelated user files remain untouched.
