# Unified Task And Plan Input UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the separate task-reference uploader with one ordered Prompt-material flow, let Cloud Agents decide file usage, and show catalog-derived credit multipliers directly on execution-profile choices.

**Architecture:** Extend ordinary input attachments with a server-verified asset identity so legacy reference assets can enter the same ordered material contract without trusting URLs or keys. Preserve one array order from Studio through persistence and bootstrap `index.json`, and make Agent instructions resolve global and per-type ordinals. Derive comparative execution multipliers only from current task-type catalog prices; exact SKU prices remain authoritative.

**Tech Stack:** Go, Fiber v3, GORM, React 19, TypeScript, React Hook Form, TanStack Query, Base UI/shadcn, Tailwind CSS v4, Vitest, Testing Library, Bun, in-app Browser.

---

## File Map

- `server/model/entry_attachment.go`: asset-backed ordinary attachment identity.
- `server/handler/input_attachment.go`: ownership, purpose, MIME, and source validation.
- `server/service/plan_reference_migrate.go`: active-plan reference conversion.
- `server/service/task_retry.go`: historical task-reference clone conversion.
- `server/service/agent_bootstrap.go`: asset materialization and `type_index`.
- `studio/src/components/agent-prompt/attachment-order.ts`: ordinal labels and Prompt-reference detection.
- `studio/src/components/agent-prompt/usePromptAttachments.ts`: stable ordered state and `move`.
- `studio/src/components/agent-prompt/AgentPromptInput.tsx`: numbered tiles and reorder UI.
- `studio/src/lib/pricing.ts`: catalog-derived price/multiplier presentation.
- `studio/src/components/tasks/ExecutionProfileSelector.tsx`: multiplier and exact-price cards.
- `studio/src/components/tasks/TaskFormDialog.tsx`: one-uploader task/clone hierarchy.
- `studio/src/pages/PlansPage.tsx`: one-uploader plan create/edit hierarchy.
- `plugins/agents/{article,seednote}.{md,toml}`: ordered material semantics.
- `plugins/.claude-plugin/plugin.json` and `plugins/.codex-plugin/plugin.json`: synchronized patch version.

## Task 1: Support Verified Asset-Backed Attachments

**Files:**
- Modify: `server/model/entry_attachment.go`
- Modify: `server/handler/input_attachment.go`
- Modify: `server/handler/input_attachment_test.go`

- [ ] **Step 1: Write failing validator tests**

Add table cases proving an owned finalized image with purpose `task_reference` is accepted, while another user's asset, missing asset, non-image asset, disallowed purpose, and `asset_id` combined with `url`, `key`, or `upload_id` are rejected. Assert output order is unchanged.

```go
got, err := validateInputAttachments(ctx, store, repo, userID, []model.EntryAttachment{
    {Type: "text", Text: "first", FileName: "brief.txt"},
    {AssetID: ownedReference.ID},
}, InputAttachmentValidationOptions{
    AllowedTypes: allAgentAttachmentTypes,
    AllowedAssetPurposes: []string{service.DirectUploadPurposeTaskReference},
})
require.NoError(t, err)
require.Equal(t, "brief.txt", got[0].FileName)
require.Equal(t, ownedReference.ID, got[1].AssetID)
```

- [ ] **Step 2: Run the failing tests**

Run: `go test ./server/handler -run 'TestValidateInputAttachments.*Asset' -count=1`

Expected: FAIL because `EntryAttachment.AssetID` and `AllowedAssetPurposes` are undefined.

- [ ] **Step 3: Implement the minimal contract**

```go
type EntryAttachment struct {
    AssetID string `json:"asset_id,omitempty"`
    // Existing fields remain unchanged.
}

type InputAttachmentValidationOptions struct {
    MaxCount int
    MaxBytes int64
    AllowedTypes map[string]bool
    AllowedAssetPurposes []string
    normalizeExistingKey func(model.EntryAttachment) (model.EntryAttachment, error)
}
```

When `AssetID` is present, reject every other identity source, load the asset, verify user, finalized state, allowed purpose, and image metadata, then rebuild the descriptor from server-owned metadata. Caller URL/key values never establish identity.

- [ ] **Step 4: Run focused tests**

Run: `go test ./server/handler -run 'TestValidateInputAttachments' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add server/model/entry_attachment.go server/handler/input_attachment.go server/handler/input_attachment_test.go
git commit -m "feat(server): support verified asset input attachments"
```

## Task 2: Migrate Active Plan References

**Files:**
- Create: `server/service/plan_reference_migrate.go`
- Create: `server/service/plan_reference_migrate_test.go`
- Modify: `server/main.go`
- Modify: `server/main_test.go`

- [ ] **Step 1: Write failing migration tests**

Cover reference-only, reference plus attachments, already-present asset, inactive plan, invalid asset, rollback, and a second run. Assert valid active plans prepend one asset attachment and clear the dedicated field atomically.

```go
require.NoError(t, MigratePlanReferenceAttachments(ctx, db, &log))
require.NoError(t, db.First(&got, "id = ?", plan.ID).Error)
require.Equal(t, reference.ID, got.InputAttachments.Data()[0].AssetID)
require.Empty(t, got.ReferenceImageAssetID)
```

- [ ] **Step 2: Verify failure**

Run: `go test ./server/service -run TestMigratePlanReferenceAttachments -count=1`

Expected: FAIL because the migration function does not exist.

- [ ] **Step 3: Implement the transactional migration**

Implement `func MigratePlanReferenceAttachments(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error`. Lock each active plan with a reference, validate the asset, prepend an asset-backed image unless already present, persist JSON, and clear the reference in one transaction. Return before clearing on any error.

- [ ] **Step 4: Wire startup ordering**

Call the migration after `model.AutoMigrate` and before scheduler/router startup. Extend `server/main_test.go` to prove failure aborts startup.

- [ ] **Step 5: Run tests**

Run: `go test ./server/service ./server -run 'TestMigratePlanReferenceAttachments|TestMigrateModels' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/service/plan_reference_migrate.go server/service/plan_reference_migrate_test.go server/main.go server/main_test.go
git commit -m "feat(server): migrate plan references to materials"
```

## Task 3: Preserve Order Through Clone And Bootstrap

**Files:**
- Modify: `server/service/task_retry.go`
- Modify: `server/service/task_test.go`
- Modify: `server/service/agent_bootstrap.go`
- Modify: `server/service/agent_bootstrap_test.go`

- [ ] **Step 1: Write failing clone tests**

Assert a historical direct task reference is prepended once, existing material order remains, and the clone has no dedicated task reference. Assert a project-snapshot-only reference is not duplicated.

```go
got := clones[0].InputAttachments.Data()
require.Equal(t, taskReference.ID, got[0].AssetID)
require.Equal(t, "brief.txt", got[1].FileName)
require.Empty(t, clones[0].ReferenceImageAssetID)
```

- [ ] **Step 2: Write failing bootstrap tests**

For `[document, image, image, text]`, assert `index` values `1..4`, `type_index` values `1,1,2,1`, matching ordinal filenames, and signed asset-backed downloads from server storage identity.

- [ ] **Step 3: Verify failure**

Run: `go test ./server/service -run 'TestClone.*Reference.*Attachment|TestAgentBootstrap.*TypeIndex|TestAgentBootstrap.*AssetAttachment' -count=1`

Expected: FAIL on clone conversion and missing `type_index`.

- [ ] **Step 4: Implement conversion and indexing**

Add a duplicate-safe helper for direct task references. In bootstrap, keep per-type counts:

```go
typeCounts := map[string]int{}
typeCounts[attachment.Type]++
entry.TypeIndex = typeCounts[attachment.Type]
```

Resolve `AssetID` through repository ownership and allowed-purpose checks before signing storage.

- [ ] **Step 5: Run tests**

Run: `go test ./server/service -run 'TestClone|TestAgentBootstrap' -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add server/service/task_retry.go server/service/task_test.go server/service/agent_bootstrap.go server/service/agent_bootstrap_test.go
git commit -m "feat(agent): preserve ordered task materials"
```

## Task 4: Add Stable Studio Material Ordering

**Files:**
- Modify: `studio/src/types/input-attachment.ts`
- Create: `studio/src/components/agent-prompt/attachment-order.ts`
- Create: `studio/src/components/agent-prompt/attachment-order.test.ts`
- Modify: `studio/src/components/agent-prompt/usePromptAttachments.ts`
- Modify: `studio/src/components/agent-prompt/usePromptAttachments.test.tsx`

- [ ] **Step 1: Write failing utility tests**

```ts
expect(materialOrdinals([doc, imageA, imageB])).toEqual([
  { index: 1, typeIndex: 1, label: '文档 1' },
  { index: 2, typeIndex: 1, label: '图 1' },
  { index: 3, typeIndex: 2, label: '图 2' },
])
expect(hasOrdinalMaterialReference('用第一张图和附件 3')).toBe(true)
expect(hasOrdinalMaterialReference('按品牌风格生成')).toBe(false)
```

- [ ] **Step 2: Write failing controller tests**

Assert `move(id, targetIndex)` preserves IDs, previews, and upload attempts while changing serialization order. Resolve upload promises out of order and assert selection order remains. Prove `asset_id` hydrates and serializes without upload ID/key.

- [ ] **Step 3: Verify failure**

Run: `cd studio && bun run test -- src/components/agent-prompt/attachment-order.test.ts src/components/agent-prompt/usePromptAttachments.test.tsx`

Expected: FAIL because the utility and `move` do not exist.

- [ ] **Step 4: Implement minimal helpers**

```ts
export interface PromptAttachmentsController {
  move: (id: string, targetIndex: number) => void
}
export function materialOrdinals(items: readonly Pick<PromptAttachment, 'type'>[]): MaterialOrdinal[]
export function hasOrdinalMaterialReference(prompt: string): boolean
```

Clamp indices, no-op unchanged moves, use one immutable splice, and preserve attachment object identity.

- [ ] **Step 5: Run tests**

Run the Step 3 command again.

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add studio/src/types/input-attachment.ts studio/src/components/agent-prompt/attachment-order.ts studio/src/components/agent-prompt/attachment-order.test.ts studio/src/components/agent-prompt/usePromptAttachments.ts studio/src/components/agent-prompt/usePromptAttachments.test.tsx
git commit -m "feat(studio): add ordered prompt materials"
```

## Task 5: Render Ordinals And Reorder Controls

**Files:**
- Modify: `studio/src/components/agent-prompt/AgentPromptInput.tsx`
- Modify: `studio/src/components/agent-prompt/AgentPromptInput.test.tsx`

- [ ] **Step 1: Write failing interaction tests**

Assert visible `图 1`, `图 2`, `文档 1`; keyboard and pointer reorder; focus retention; live announcement; and a warning after reorder/delete only when Prompt contains ordinal language.

```ts
fireEvent.click(screen.getByRole('button', { name: '将 图 2 前移' }))
expect(controller.move).toHaveBeenCalledWith('image-2', 0)
expect(screen.getByRole('status')).toHaveTextContent('素材顺序已变化')
```

- [ ] **Step 2: Verify failure**

Run: `cd studio && bun run test -- src/components/agent-prompt/AgentPromptInput.test.tsx`

Expected: FAIL because ordinals and move controls are absent.

- [ ] **Step 3: Implement fixed-size numbered tiles**

Use Lucide `GripVertical`, `ArrowLeft`, and `ArrowRight` with tooltips. Keep the existing `size-20` footprint. Include ordinals in visible text and accessible names; keep dragged ID in React state.

- [ ] **Step 4: Add the non-blocking warning**

```tsx
<p role="status" className="text-xs text-amber-700">
  素材顺序已变化，请检查提示词中的图片或附件编号。
</p>
```

Do not rewrite Prompt and do not block submission.

- [ ] **Step 5: Run tests**

Run the Step 2 command again.

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add studio/src/components/agent-prompt/AgentPromptInput.tsx studio/src/components/agent-prompt/AgentPromptInput.test.tsx
git commit -m "feat(studio): expose prompt material order"
```

## Task 6: Show Catalog-Derived Multipliers

**Files:**
- Modify: `studio/src/lib/pricing.ts`
- Modify: `studio/src/lib/pricing.test.ts`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.tsx`
- Modify: `studio/src/components/tasks/ExecutionProfileSelector.test.tsx`

- [ ] **Step 1: Write failing pricing tests**

Cover `4,800 / 6,000 / 18,000` as `1x / 1.25x / 3.75x`, tier discounts, equal prices, unavailable profiles, and missing SKU.

```ts
expect(executionProfilePriceOptions(profiles, catalog, 'article')).toMatchObject([
  { id: 'effective', priceCredits: 4800, multiplierLabel: '1x' },
  { id: 'balanced', priceCredits: 6000, multiplierLabel: '1.25x' },
  { id: 'quality', priceCredits: 18000, multiplierLabel: '3.75x' },
])
```

- [ ] **Step 2: Write failing selector tests**

Pass catalog, task type, and price unit. Assert card text/accessibility includes multiplier and exact price; missing prices show `价格暂不可用`.

- [ ] **Step 3: Verify failure**

Run: `cd studio && bun run test -- src/lib/pricing.test.ts src/components/tasks/ExecutionProfileSelector.test.tsx`

Expected: FAIL on missing helper/props.

- [ ] **Step 4: Implement presentation**

```ts
export function executionProfilePriceOptions(
  profiles: readonly AgentExecutionProfileCapability[],
  catalog: BillingCatalog | undefined,
  taskType: string,
): ExecutionProfilePriceOption[]
```

Use the lowest defined current price among available profiles as `1x`; format ratios to two decimals maximum. Add optional `catalog`, `taskType`, and `priceUnit: 'task' | 'run'` selector props.

- [ ] **Step 5: Run tests**

Run the Step 3 command again.

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add studio/src/lib/pricing.ts studio/src/lib/pricing.test.ts studio/src/components/tasks/ExecutionProfileSelector.tsx studio/src/components/tasks/ExecutionProfileSelector.test.tsx
git commit -m "feat(studio): show execution credit multipliers"
```

## Task 7: Unify The Task Create/Clone Form

**Files:**
- Modify: `studio/src/lib/task-form.ts`
- Modify: `studio/src/lib/task-form.test.ts`
- Modify: `studio/src/lib/schemas.ts`
- Modify: `studio/src/lib/schemas.test.ts`
- Modify: `studio/src/components/tasks/TaskFormDialog.tsx`
- Modify: `studio/src/components/tasks/TaskFormDialog.test.tsx`
- Modify: `studio/src/pages/TasksPage.ux.contract.test.ts`

- [ ] **Step 1: Write failing defaults/request tests**

Assert new requests omit `reference_image`. Assert a historical direct task reference becomes the first asset-backed input attachment, while a project-snapshot-only reference does not. Preserve resume filtering and existing order.

- [ ] **Step 2: Write failing dialog UX tests**

Assert one upload input, no `任务参考图`, Prompt/materials before profiles, multiplier plus exact price on each profile, and advanced controls collapsed unless cloned non-defaults or validation require attention.

- [ ] **Step 3: Verify failure**

Run: `cd studio && bun run test -- src/lib/task-form.test.ts src/lib/schemas.test.ts src/components/tasks/TaskFormDialog.test.tsx src/pages/TasksPage.ux.contract.test.ts`

Expected: FAIL on the old reference field/uploader and layout.

- [ ] **Step 4: Remove task-reference writes**

Remove `reference_image` from task form values/schema/request generation while retaining historical response types. Hydrate a direct historical reference as `{type:'image', asset_id, file_name, content_type, size}`.

- [ ] **Step 5: Apply the approved hierarchy**

Render project, Prompt/materials, execution profiles, generation settings, then sticky price/footer. Use the existing `Collapsible` components for advanced settings and pass catalog/type/`priceUnit="task"` to the selector.

- [ ] **Step 6: Run tests**

Run the Step 3 command again.

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add studio/src/lib/task-form.ts studio/src/lib/task-form.test.ts studio/src/lib/schemas.ts studio/src/lib/schemas.test.ts studio/src/components/tasks/TaskFormDialog.tsx studio/src/components/tasks/TaskFormDialog.test.tsx studio/src/pages/TasksPage.ux.contract.test.ts
git commit -m "refactor(studio): unify task prompt materials"
```

## Task 8: Unify The Plan Create/Edit Form

**Files:**
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/types/plan.ts`

- [ ] **Step 1: Write failing plan tests**

Assert one uploader, no `reference_image` payload, migrated asset order, profile multipliers/per-run prices, schedule before Prompt, untouched edit omission, and exact array submission after reorder/removal.

- [ ] **Step 2: Verify failure**

Run: `cd studio && bun run test -- src/pages/PlansPage.test.tsx`

Expected: FAIL because dedicated reference state and UI remain.

- [ ] **Step 3: Remove reference state/payload code**

Delete `ReferenceAssetUpload`, `referenceSelectionFromValue`, `referenceUploading`, `referenceTouchedRef`, and reference payload branches. Retain response typing only for rolling-deployment reads.

- [ ] **Step 4: Apply shared hierarchy/pricing**

Keep schedule after project, then Prompt/materials, profiles, generation settings, advanced controls, and sticky per-run footer. Pass `priceUnit="run"`.

- [ ] **Step 5: Run tests**

Run the Step 2 command again.

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx studio/src/types/plan.ts
git commit -m "refactor(studio): unify plan prompt materials"
```

## Task 9: Update Agent Material Semantics

**Files:**
- Modify: `plugins/agents/article.md`
- Modify: `plugins/agents/article.toml`
- Modify: `plugins/agents/seednote.md`
- Modify: `plugins/agents/seednote.toml`
- Modify: directly contradictory `plugins/skills/*/SKILL.md` only if found
- Modify: `plugins/.claude-plugin/plugin.json`
- Modify: `plugins/.codex-plugin/plugin.json`
- Modify: `server/mcp/tools_test.go`

- [ ] **Step 1: Write a failing contract test**

Require matching native version `4.0.9`, paired Agent instructions to read `index.json`, use `index` and `type_index`, let Prompt semantics choose usage, and never assume upload position creates a primary reference.

- [ ] **Step 2: Verify failure**

Run: `go test ./server/mcp -run 'Test.*InputAttachment|Test.*Plugin.*Version' -count=1`

Expected: FAIL on instructions and version `4.0.8`.

- [ ] **Step 3: Update Agent instructions**

Add equivalent rules to `.md` and `.toml` pairs:

```text
Treat .anban-creator/input-attachments/index.json as the ordered source of truth.
"The Nth material" resolves by index; "the Nth image" resolves by image type_index.
Use the Prompt and optional per-file instruction to decide relevant files and tools.
Do not infer a primary reference from upload position alone.
```

Update a Skill only if it currently contradicts these rules.

- [ ] **Step 4: Patch bump both manifests**

Change both native versions from `4.0.8` to `4.0.9`.

- [ ] **Step 5: Run contract tests**

Run the Step 2 command again.

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add plugins/agents plugins/skills plugins/.claude-plugin/plugin.json plugins/.codex-plugin/plugin.json server/mcp/tools_test.go
git commit -m "feat(plugin): define ordered material semantics"
```

## Task 10: Full Verification And Browser QA

**Files:**
- Modify only files required by failures attributable to this feature.
- Do not commit screenshots, traces, or temporary Browser artifacts.

- [ ] **Step 1: Run focused Go suites**

Run: `go test ./server/handler -run 'TestValidateInputAttachments|TestCreateTask|TestCreatePlan' -count=1`

Run: `go test ./server/service -run 'TestMigratePlanReferenceAttachments|TestClone|TestAgentBootstrap' -count=1`

Run: `go test ./server/mcp -run 'Test.*InputAttachment|Test.*Plugin.*Version' -count=1`

Expected: PASS.

- [ ] **Step 2: Run full Go verification**

Run: `go test ./...`

Run: `go build -o /tmp/anban-creator-server ./server`

Run: `go build -o /tmp/anban ./agent`

Expected: PASS. Isolate and rerun transient SQLite locking failures serially before attribution.

- [ ] **Step 3: Run focused Studio suites**

Run: `cd studio && bun run test -- src/components/agent-prompt/attachment-order.test.ts src/components/agent-prompt/usePromptAttachments.test.tsx src/components/agent-prompt/AgentPromptInput.test.tsx src/components/tasks/ExecutionProfileSelector.test.tsx src/components/tasks/TaskFormDialog.test.tsx src/pages/PlansPage.test.tsx src/pages/TasksPage.ux.contract.test.ts`

Expected: PASS.

- [ ] **Step 4: Run full Studio verification**

```bash
export PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH
cd studio
bun run test
bun run build
```

Expected: full Vitest suite and `tsc -b && vite build` PASS.

- [ ] **Step 5: Browser-test target flows**

Start Studio on an unused port. Test `/tasks -> create -> mixed upload -> reorder -> profile -> exact total`, `/plans -> create/edit -> schedule -> reorder -> per-run price`, and historical task clone conversion.

- [ ] **Step 6: Verify desktop and mobile rendering**

With the in-app Browser at desktop and approximately `390x844`, check page identity, nonblank render, no framework overlay, console health, screenshots, reorder/profile interactions, fixed tile size, no overlap, advanced collapse, sticky-footer clearance, and exact price/multiplier text.

- [ ] **Step 7: Review the complete branch diff**

```bash
git status --short
git diff --check $(git merge-base HEAD main)..HEAD
git diff --stat $(git merge-base HEAD main)..HEAD
git log --oneline $(git merge-base HEAD main)..HEAD
```

Expected: only planned changes; no secrets, Browser artifacts, or unrelated workspace edits.

- [ ] **Step 8: Commit verification corrections only if needed**

```bash
git add -u -- server studio plugins
git commit -m "fix: complete unified material UX verification"
```

Skip this commit if no corrections are required.
