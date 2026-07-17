# Montage Studio and Project Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the existing Montage contract so users can create configured Montage projects, inherit project defaults into tasks and plans, upload source assets, and submit the full stable Montage input from Studio.

**Architecture:** Extend the generic project API with the existing `MontageDefaults` model. Keep Studio concerns split into project defaults, deterministic form normalization, and execution-specific source asset input, while leaving the current Montage runtime, agent, execution target, and artifact contracts unchanged.

**Tech Stack:** Go, Fiber v3, GORM, React 19, TypeScript, React Hook Form, Zod, TanStack Query, Vitest, Testing Library, Bun.

---

## File Structure

- `server/service/project.go` and `server/service/project_test.go`: platform acceptance, defaults validation, and update semantics.
- `server/handler/project.go` and `server/handler/project_test.go`: request mapping and HTTP persistence.
- `studio/src/lib/montage-form.ts` and its test: project-default inheritance and submit normalization.
- `studio/src/components/montage/MontageProjectDefaultsPanel.tsx` and its test: project-only defaults editor.
- `studio/src/pages/ProjectsPage.tsx` and its test: project creation/editing integration.
- `studio/src/lib/studio-ux.ts` and its test: Montage-specific readiness summary.
- `studio/src/lib/direct-upload.ts`, `studio/src/components/ReferenceMaterialInput.tsx`, and its test: typed upload-purpose reuse.
- `studio/src/components/montage/MontageSourceAssetInput.tsx` and its test: attachment-to-Montage asset adapter.
- `studio/src/components/montage/MontageCreationPanel.tsx` and its test: complete stable execution input.
- `studio/src/pages/TasksPage.tsx`, `studio/src/pages/PlansPage.tsx`, and their tests: defaults inheritance and upload blocking.

### Task 1: Complete the server project contract

**Files:**
- Modify: `server/service/project.go`
- Modify: `server/service/project_test.go`
- Modify: `server/handler/project.go`
- Modify: `server/handler/project_test.go`

- [ ] **Step 1: Write failing service tests**

Add tests that create and update a Montage project with every stable default,
reject defaults on an article project, and table-test durations `-1`, `0`, `600`,
and `601`:

```go
project := &model.Project{Platform: model.PlatformMontage, Name: "launch"}
project.SetMontageDefaults(model.MontageDefaults{
    DefaultPipeline: "social-short",
    Preferences: model.MontagePreferences{
        AspectRatio: "9:16", DurationSeconds: 45, Style: "documentary",
        MusicPrompt: "minimal electronic", SubtitleMode: "burned-in",
        VoiceoverMode: "narrated",
    },
    AssetGuidance: "prefer uploaded footage",
    DeliveryTargets: []string{"final_video", "subtitles"},
})
project.MontageDefaultsSet = true
created, err := svc.Create(ctx, user.ID, project)
if err != nil { t.Fatal(err) }
if got := created.MontageDefaults.Data().DefaultPipeline; got != "social-short" {
    t.Fatalf("DefaultPipeline = %q", got)
}
```

- [ ] **Step 2: Run the service tests and verify failure**

Run: `go test ./server/service -run 'TestProjectServiceMontage' -count=1`

Expected: FAIL because `montage` is absent from `validPlatforms` and defaults
are not applied or validated.

- [ ] **Step 3: Implement platform acceptance and defaults validation**

Add `model.PlatformMontage: true` and a focused validator:

```go
func validateProjectMontageDefaults(project *model.Project) error {
    if !project.MontageDefaultsSet { return nil }
    if !model.IsMontagePlatform(project.Platform) {
        return errors.New("montage_defaults can only be set on montage projects")
    }
    duration := project.MontageDefaults.Data().Preferences.DurationSeconds
    if duration < 0 || duration > 600 {
        return errors.New("montage duration_seconds must be between 0 and 600")
    }
    return nil
}
```

Call it before create persistence. On update, validate against the effective
platform and copy `MontageDefaults` only when `MontageDefaultsSet` is true.

- [ ] **Step 4: Write failing handler mapping and HTTP tests**

Test `projectRequest.toProject()`, POST creation, and PUT update:

```go
req := projectRequest{
    Platform: model.PlatformMontage,
    MontageDefaults: &model.MontageDefaults{
        DefaultPipeline: "social-short",
        Preferences: model.MontagePreferences{AspectRatio: "9:16", DurationSeconds: 45},
        AssetGuidance: "prefer source footage",
        DeliveryTargets: []string{"final_video"},
    },
}
project := req.toProject()
if !project.MontageDefaultsSet { t.Fatal("MontageDefaultsSet = false") }
```

- [ ] **Step 5: Run handler tests and verify failure**

Run: `go test ./server/handler -run 'TestProject(RequestMaps|Handler_.*Montage)' -count=1`

Expected: FAIL because `projectRequest` has no `MontageDefaults` field.

- [ ] **Step 6: Map Montage defaults in the handler**

```go
MontageDefaults *model.MontageDefaults `json:"montage_defaults,omitempty"`

if req.MontageDefaults != nil {
    p.SetMontageDefaults(*req.MontageDefaults)
    p.MontageDefaultsSet = true
}
```

Missing update fields preserve current defaults; a present empty object clears
them.

- [ ] **Step 7: Run focused server tests**

Run: `go test ./server/service ./server/handler -run 'Montage|ProjectRequestMapsMontage' -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add server/service/project.go server/service/project_test.go server/handler/project.go server/handler/project_test.go
git commit -m "feat: support Montage project defaults"
```

### Task 2: Add deterministic project-default inheritance

**Files:**
- Modify: `studio/src/lib/montage-form.ts`
- Modify: `studio/src/lib/montage-form.test.ts`

- [ ] **Step 1: Write failing helper tests**

```ts
it('initializes input from project defaults', () => {
  expect(initialMontageInput('', undefined, {
    default_pipeline: 'social-short',
    preferences: { aspect_ratio: '16:9', duration_seconds: 45, style: 'clean' },
    delivery_targets: ['final_video'],
  })).toMatchObject({
    pipeline_key: 'social-short',
    preferences: { aspect_ratio: '16:9', duration_seconds: 45, style: 'clean' },
    delivery_targets: ['final_video'],
  })
})

it('keeps explicit input ahead of project defaults', () => {
  const result = initialMontageInput('brief', {
    pipeline_key: 'manual', preferences: { duration_seconds: 15 }, delivery_targets: [],
  }, {
    default_pipeline: 'project',
    preferences: { aspect_ratio: '9:16', duration_seconds: 45 },
    delivery_targets: ['final_video'],
  })
  expect(result.pipeline_key).toBe('manual')
  expect(result.preferences?.aspect_ratio).toBe('9:16')
  expect(result.preferences?.duration_seconds).toBe(15)
  expect(result.delivery_targets).toEqual([])
})
```

- [ ] **Step 2: Run tests and verify failure**

Run: `cd studio && bun run test -- src/lib/montage-form.test.ts`

Expected: FAIL because the helper has no project-defaults parameter.

- [ ] **Step 3: Implement field-aware merging**

```ts
export function initialMontageInput(
  brief = '', input?: Partial<MontageInput>, defaults?: MontageProjectDefaults,
): MontageFormInput {
  return {
    brief: input?.brief ?? brief,
    pipeline_key: input?.pipeline_key ?? defaults?.default_pipeline ?? '',
    source_assets: input?.source_assets ?? [],
    preferences: {
      aspect_ratio: input?.preferences?.aspect_ratio ?? defaults?.preferences?.aspect_ratio ?? '9:16',
      duration_seconds: input?.preferences?.duration_seconds ?? defaults?.preferences?.duration_seconds ?? 30,
      style: input?.preferences?.style ?? defaults?.preferences?.style ?? '',
      music_prompt: input?.preferences?.music_prompt ?? defaults?.preferences?.music_prompt ?? '',
      subtitle_mode: input?.preferences?.subtitle_mode ?? defaults?.preferences?.subtitle_mode ?? '',
      voiceover_mode: input?.preferences?.voiceover_mode ?? defaults?.preferences?.voiceover_mode ?? '',
    },
    delivery_targets: input?.delivery_targets ?? defaults?.delivery_targets ?? [],
    advanced: input?.advanced,
  }
}
```

- [ ] **Step 4: Run helper tests**

Run: `cd studio && bun run test -- src/lib/montage-form.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit the helper contract**

```bash
git add studio/src/lib/montage-form.ts studio/src/lib/montage-form.test.ts
git commit -m "feat: inherit Montage project defaults"
```

### Task 3: Build the project defaults editor

**Files:**
- Create: `studio/src/components/montage/MontageProjectDefaultsPanel.tsx`
- Create: `studio/src/components/montage/MontageProjectDefaultsPanel.test.tsx`

- [ ] **Step 1: Write a failing controlled-panel test**

Use a React Hook Form harness and edit all stable fields:

```tsx
fireEvent.change(screen.getByLabelText('默认 Pipeline'), { target: { value: 'social-short' } })
fireEvent.change(screen.getByLabelText('默认时长（秒）'), { target: { value: '45' } })
fireEvent.change(screen.getByLabelText('音乐提示'), { target: { value: 'minimal electronic' } })
fireEvent.change(screen.getByLabelText('字幕模式'), { target: { value: 'burned-in' } })
fireEvent.change(screen.getByLabelText('配音模式'), { target: { value: 'narrated' } })
fireEvent.change(screen.getByLabelText('素材使用说明'), { target: { value: '优先使用实拍素材' } })
expect(screen.getByTestId('montage-defaults')).toHaveTextContent('"duration_seconds":45')
```

- [ ] **Step 2: Run the test and verify failure**

Run: `cd studio && bun run test -- src/components/montage/MontageProjectDefaultsPanel.test.tsx`

Expected: FAIL because the component does not exist.

- [ ] **Step 3: Implement the focused panel**

Use `FormField`, `Input`, `Textarea`, and `TagInput` for these exact paths:

```text
montage_defaults.default_pipeline
montage_defaults.preferences.aspect_ratio
montage_defaults.preferences.duration_seconds
montage_defaults.preferences.style
montage_defaults.preferences.music_prompt
montage_defaults.preferences.subtitle_mode
montage_defaults.preferences.voiceover_mode
montage_defaults.asset_guidance
montage_defaults.delivery_targets
```

The public prop is `form: UseFormReturn<ProjectFormValues>`. Use a two-column
grid for scalar fields and full-width text areas for prompts and guidance.

- [ ] **Step 4: Run the panel test**

Run: `cd studio && bun run test -- src/components/montage/MontageProjectDefaultsPanel.test.tsx`

Expected: PASS.

- [ ] **Step 5: Commit the project defaults panel**

```bash
git add studio/src/components/montage/MontageProjectDefaultsPanel.tsx studio/src/components/montage/MontageProjectDefaultsPanel.test.tsx
git commit -m "feat: add Montage project defaults panel"
```

### Task 4: Integrate Montage into ProjectsPage

**Files:**
- Modify: `studio/src/pages/ProjectsPage.tsx`
- Modify: `studio/src/pages/ProjectsPage.test.tsx`
- Modify: `studio/src/lib/studio-ux.ts`
- Modify: `studio/src/lib/studio-ux.test.ts`

- [ ] **Step 1: Write failing create, edit, and readiness tests**

Open project creation, select Montage, edit defaults, submit, and assert:

```ts
expect(api.projects.create).toHaveBeenCalledWith(expect.objectContaining({
  platform: 'montage',
  montage_defaults: {
    default_pipeline: 'social-short',
    preferences: expect.objectContaining({ aspect_ratio: '9:16', duration_seconds: 45 }),
    asset_guidance: '优先使用实拍素材',
    delivery_targets: ['final_video'],
  },
}))
```

For edit, restore saved defaults and retain untouched values. For readiness,
assert a Montage project reports `Pipeline social-short` and never
`补视觉配置`.

- [ ] **Step 2: Run tests and verify failure**

Run: `cd studio && bun run test -- src/pages/ProjectsPage.test.tsx src/lib/studio-ux.test.ts`

Expected: FAIL because Montage is absent from the selector and form.

- [ ] **Step 3: Add Montage project form state and payload**

Add `{ value: 'montage', label: 'Montage' }`, creation-intent support,
`isMontage`, and these defaults:

```ts
montage_defaults: {
  default_pipeline: '',
  preferences: {
    aspect_ratio: '9:16', duration_seconds: 30, style: '', music_prompt: '',
    subtitle_mode: '', voiceover_mode: '',
  },
  asset_guidance: '',
  delivery_targets: [],
},
```

Restore them in `projectToForm`, render `MontageProjectDefaultsPanel`, omit the
image-specific visual style field for Montage, and send normalized
`montage_defaults` only for Montage.

- [ ] **Step 4: Add Montage readiness logic**

```ts
if (project.platform === 'montage') {
  const pipeline = project.montage_defaults?.default_pipeline
  details.push(pipeline ? `Pipeline ${pipeline}` : '使用系统默认 Pipeline')
  details.push(project.montage_defaults?.preferences?.duration_seconds
    ? `默认 ${project.montage_defaults.preferences.duration_seconds} 秒`
    : '使用系统默认时长')
} else if (!isVideoPlatform(project.platform)) {
  details.push(project.visual_style ? '视觉已配置' : '补视觉配置')
}
```

- [ ] **Step 5: Run focused project tests**

Run: `cd studio && bun run test -- src/pages/ProjectsPage.test.tsx src/pages/ProjectsPage.layout.test.ts src/lib/studio-ux.test.ts`

Expected: PASS.

- [ ] **Step 6: Commit the project UI integration**

```bash
git add studio/src/pages/ProjectsPage.tsx studio/src/pages/ProjectsPage.test.tsx studio/src/lib/studio-ux.ts studio/src/lib/studio-ux.test.ts
git commit -m "feat: create Montage projects in Studio"
```

### Task 5: Add source assets and complete the execution panel

**Files:**
- Modify: `studio/src/lib/direct-upload.ts`
- Modify: `studio/src/components/ReferenceMaterialInput.tsx`
- Modify: `studio/src/components/ReferenceMaterialInput.test.tsx`
- Create: `studio/src/components/montage/MontageSourceAssetInput.tsx`
- Create: `studio/src/components/montage/MontageSourceAssetInput.test.tsx`
- Modify: `studio/src/components/montage/MontageCreationPanel.tsx`
- Modify: `studio/src/components/montage/MontageCreationPanel.test.tsx`

- [ ] **Step 1: Write a failing upload-purpose test**

Pass `uploadPurpose="montage_asset"` to the controlled reference input and
assert `uploadToOSS` receives it. Keep the existing default-purpose assertion.

- [ ] **Step 2: Run the test and verify failure**

Run: `cd studio && bun run test -- src/components/ReferenceMaterialInput.test.tsx`

Expected: FAIL because the purpose is hardcoded.

- [ ] **Step 3: Parameterize the shared uploader**

Add `'montage_asset'` to `DirectUploadPurpose` and:

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
  uploadPurpose?: DirectUploadPurpose
}

const result = await uploadToOSS({
  purpose: uploadPurpose,
  file: row.file,
  onProgress: (progress) => {
    updateRows((current) => current.map((item) => (
      item.id === row.id ? { ...item, progress } : item
    )))
  },
})
```

Destructure `uploadPurpose = 'ai_entry_attachment'` in the component parameters;
the other parameter defaults remain unchanged.

- [ ] **Step 4: Write failing asset adapter tests**

Test seeded assets, upload mapping, removal, metadata, and upload state:

```ts
expect(onChange).toHaveBeenLastCalledWith([expect.objectContaining({
  type: 'video_url', url: '/source.mp4', file_name: 'source.mp4',
  mime_type: 'video/mp4', file_size: 42,
})])
expect(onUploadingChange).toHaveBeenCalledWith(true)
expect(onUploadingChange).toHaveBeenLastCalledWith(false)
```

- [ ] **Step 5: Run the adapter test and verify failure**

Run: `cd studio && bun run test -- src/components/montage/MontageSourceAssetInput.test.tsx`

Expected: FAIL because the adapter does not exist.

- [ ] **Step 6: Implement `MontageSourceAssetInput`**

Use explicit type maps:

```ts
const toAttachmentType = {
  image_url: 'image', video_url: 'video', audio_url: 'audio',
  document_url: 'document', text: 'text',
} as const
const toMontageType = {
  image: 'image_url', video: 'video_url', audio: 'audio_url',
  document: 'document_url', text: 'text',
} as const
```

Render `ReferenceMaterialInput` with all five file types,
`uploadPurpose="montage_asset"`, `maxCount={20}`, and the upload-state callback.

- [ ] **Step 7: Write failing complete-panel tests**

Edit music prompt, subtitle mode, voiceover mode, delivery targets, and mocked
source assets. Assert no `advanced` or execution-target control appears.

- [ ] **Step 8: Expand `MontageCreationPanel`**

Add `onUploadingChange?: (uploading: boolean) => void`, render the source asset
adapter, use `TagInput` for delivery targets, and bind every stable preference.

- [ ] **Step 9: Run component tests**

Run: `cd studio && bun run test -- src/components/ReferenceMaterialInput.test.tsx src/components/montage/MontageSourceAssetInput.test.tsx src/components/montage/MontageCreationPanel.test.tsx`

Expected: PASS.

- [ ] **Step 10: Commit the source asset input**

```bash
git add studio/src/lib/direct-upload.ts studio/src/components/ReferenceMaterialInput.tsx studio/src/components/ReferenceMaterialInput.test.tsx studio/src/components/montage/MontageSourceAssetInput.tsx studio/src/components/montage/MontageSourceAssetInput.test.tsx studio/src/components/montage/MontageCreationPanel.tsx studio/src/components/montage/MontageCreationPanel.test.tsx
git commit -m "feat: add Montage source asset input"
```

### Task 6: Wire defaults and uploads through tasks and plans

**Files:**
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`

- [ ] **Step 1: Write failing TasksPage tests**

Select a Montage project with saved defaults, assert prefilled controls, add a
source asset, submit, and verify:

```ts
expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
  project_id: 'project-montage',
  montage_input: expect.objectContaining({
    pipeline_key: 'social-short',
    source_assets: [expect.objectContaining({ type: 'video_url', url: '/source.mp4' })],
    preferences: expect.objectContaining({ duration_seconds: 45 }),
    delivery_targets: ['final_video'],
  }),
}))
```

Also assert submit is disabled while the panel reports an active upload.

- [ ] **Step 2: Write failing PlansPage tests**

Mirror task creation for `api.plans.create`. Add an edit case proving saved plan
input wins over current project defaults.

- [ ] **Step 3: Run tests and verify failure**

Run: `cd studio && bun run test -- src/pages/TasksPage.test.tsx src/pages/PlansPage.test.tsx`

Expected: FAIL because project defaults are not passed to the helper and upload
state is not tracked.

- [ ] **Step 4: Wire TasksPage**

For creation intent and project selection, use:

```ts
initialMontageInput(form.getValues('prompt') || '', undefined, project.montage_defaults)
```

Track `montageUploading`, pass its setter to the panel, disable submit while
true, and reset it when the modal closes or platform changes.

- [ ] **Step 5: Wire PlansPage**

Apply the same creation-only merge and upload blocking. Keep edit initialization
based only on saved `plan.montage_input`.

- [ ] **Step 6: Run page and contract tests**

Run: `cd studio && bun run test -- src/pages/TasksPage.test.tsx src/pages/PlansPage.test.tsx src/pages/MontageUx.contract.test.ts`

Expected: PASS.

- [ ] **Step 7: Format and run focused verification**

```bash
gofmt -w server/service/project.go server/service/project_test.go server/handler/project.go server/handler/project_test.go
git diff --check
go test ./server/service ./server/handler ./server/model ./server/mcp
cd studio && bun run test -- src/lib/montage-form.test.ts src/components/montage src/pages/ProjectsPage.test.tsx src/pages/TasksPage.test.tsx src/pages/PlansPage.test.tsx
```

Expected: all commands exit 0 and `git diff --check` prints nothing.

- [ ] **Step 8: Run full verification**

```bash
go test ./...
go build -o /tmp/anban-creator-server ./server
go build -o /tmp/anban ./agent
cd studio && bun run test
cd studio && bun run build
```

Expected: all commands exit 0.

- [ ] **Step 9: Commit task/plan wiring**

```bash
git add studio/src/pages/TasksPage.tsx studio/src/pages/TasksPage.test.tsx studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx
git commit -m "feat: complete Montage Studio workflow"
```

- [ ] **Step 10: Review final branch scope**

```bash
git status --short
git log --oneline --decorate main..HEAD
git diff --stat main...HEAD
```

Expected: only Montage design, plan, tests, and implementation are committed;
user-owned billing documents and submodule changes remain untouched.
