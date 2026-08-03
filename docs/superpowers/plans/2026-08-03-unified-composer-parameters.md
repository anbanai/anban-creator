# Unified Composer Parameters Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put project, execution, image, and semantic quantity controls in a shared horizontal Prompt toolbar across homepage, task, plan, and Designer surfaces while separating image capability prices to 300/500 credits.

**Architecture:** Keep `AgentPromptInput` as the layout host and add small presentational toolbar controls rather than a cross-domain form model. Each page continues to own its state and request type. Extend AI Entry only for homepage fields that must become explicit; keep task creation, capability authorization, and billing in existing services.

**Tech Stack:** React 19, TypeScript, React Hook Form, TanStack Query, Base UI/shadcn components, Vitest/Testing Library, Go Fiber v3, GORM, embedded YAML billing catalogs.

**Approved design:** `docs/superpowers/specs/2026-08-03-unified-composer-parameters-design.md`

---

## File Map

**Shared Studio controls**

- Create `studio/src/components/agent-prompt/QuantityStepper.tsx`: semantic decrement/value/increment control and static one-value state.
- Create `studio/src/components/agent-prompt/QuantityStepper.test.tsx`: range, accessibility, and static-limit coverage.
- Create `studio/src/components/agent-prompt/ComposerQuantityControl.tsx`: compact Prompt-toolbar trigger around `QuantityStepper`.
- Create `studio/src/components/agent-prompt/ComposerQuantityControl.test.tsx`: popover trigger and value-change coverage.
- Create `studio/src/components/tasks/ExecutionProfileToolbar.tsx`: compact execution-profile trigger that reuses `ExecutionProfileSelector` inside a popover.
- Create `studio/src/components/tasks/ExecutionProfileToolbar.test.tsx`: selected summary, price, availability, and selection coverage.
- Modify `studio/src/components/agent-prompt/ProjectContextControl.tsx`: allow compact readonly rendering and toolbar-sized wrapper styling without losing complete project options.
- Modify `studio/src/components/agent-prompt/ProjectContextControl.test.tsx`: compact readonly and complete popover identity coverage.
- Modify `studio/src/components/agent-prompt/ProjectIdentity.tsx`: keep avatar, name, description, and localized content type visible in compact toolbar identity.
- Modify `studio/src/components/agent-prompt/ProjectIdentity.test.tsx`: defend the complete compact identity contract.

**Backend and API**

- Modify `server/billing/products.yaml`: change the two image list prices.
- Modify `server/billing/catalog_contract_test.go`: defend distinct Standard and Professional prices.
- Modify `server/service/ai_entry.go`: accept explicit quantity, ratio, and capability; return a task collection.
- Modify `server/service/ai_entry_test.go`: explicit-field precedence, batch creation, references, and result collection.
- Modify `server/service/image_capability_resolver.go`: return typed unavailable/access-denied errors for capability selection.
- Modify `server/service/image_capability_resolver_test.go`: distinguish unknown, disabled, and tier-inaccessible capabilities.
- Modify `server/service/task.go`: authorize and canonicalize image capability keys inside `CreateManual`.
- Modify `server/service/task_capability_test.go`: prove manual creation rejects stale and unauthorized keys before persistence.
- Modify `server/handler/image_capability.go` and `server/handler/image_capability_test.go`: provide one structured stale/access-denied response contract.
- Modify `server/handler/ai_entry.go`: normalize and validate request fields at the HTTP boundary.
- Modify `server/handler/ai_entry_test.go`: forwarding and invalid-quantity coverage.
- Modify `server/handler/task.go` and `server/handler/task_test.go`: map create-time capability races to the structured response.
- Modify `server/handler/plan.go` and `server/handler/plan_test.go`: map plan capability validation to the structured response.
- Modify `server/handler/designer.go` and create `server/handler/designer_capability_test.go`: reuse and defend the structured response for quote/generate capability failures.
- Modify `server/service/ilink_conversation.go` and `server/service/ilink_conversation_test.go`: consume the task collection while preserving one-task iLink behavior.
- Modify `studio/src/lib/api/ai-entry.ts`: mirror the request and response contract.
- Create `studio/src/lib/api/ai-entry.test.ts`: defend the exact request and plural response transport.

**Page owners**

- Modify `studio/src/pages/DashboardPage.tsx` and `DashboardPage.ai-entry.test.tsx`: add explicit image settings and task quantity to the Prompt toolbar and request.
- Modify `studio/src/components/tasks/TaskFormDialog.tsx` and `.test.tsx`: move project, execution, image, and task quantity controls into the Prompt toolbar.
- Modify `studio/src/pages/PlansPage.tsx` and `.test.tsx`: move project, execution, and image settings into the Prompt toolbar; retain schedule outside; remove the redundant visible type selector.
- Modify `studio/src/components/designer/DesignerGenerationToolbar.tsx` and `.test.tsx`: reuse `QuantityStepper` and render a static limit at one.
- Modify `studio/src/pages/DesignerPage.tsx` and affected Designer page tests: move project into the bottom toolbar before image settings.

### Task 1: Publish Distinct Image Capability Prices

**Files:**
- Modify: `server/billing/products.yaml:124-135`
- Modify: `server/billing/catalog_contract_test.go:363-372`
- Test: `server/billing/catalog_contract_test.go`
- Test: `server/service/billing_catalog_test.go`

- [ ] **Step 1: Change the catalog contract expectations first**

Update the expected fixed SKU entries in
`initialRetailCatalogContractError`, which is exercised by
`TestContentAddressedRetailCatalog`:

```go
"image.standard": {
    operation: "image.generate", chargePolicy: "image_operation",
    priceCredits: 300, route: "image_generation.capabilities.standard",
    delivery: "persisted_image",
},
"image.professional": {
    operation: "image.generate", chargePolicy: "image_operation",
    priceCredits: 500, route: "image_generation.capabilities.professional",
    delivery: "persisted_image",
},
```

Add a tier-price assertion using the existing production-bundle helper and
`PriceForTier`:

```go
func TestImageCapabilityTierPricesRemainDistinct(t *testing.T) {
    products := loadProductionBundle(t).Products
    byID := make(map[string]SKUConfig, len(products.SKUs))
    for _, sku := range products.SKUs { byID[sku.ID] = sku }
    standard, ok := byID["image.standard"]
    if !ok { t.Fatal("missing image.standard") }
    professional, ok := byID["image.professional"]
    if !ok { t.Fatal("missing image.professional") }
    for _, tt := range []struct {
        tier string
        standard int64
        professional int64
    }{
        {"free", 300, 500},
        {"pro", 270, 450},
        {"enterprise", 240, 400},
    } {
        gotStandard, ok := products.PriceForTier(standard.PriceCredits, tt.tier)
        if !ok || gotStandard != tt.standard { t.Fatalf("%s standard = %d, %v", tt.tier, gotStandard, ok) }
        gotProfessional, ok := products.PriceForTier(professional.PriceCredits, tt.tier)
        if !ok || gotProfessional != tt.professional { t.Fatalf("%s professional = %d, %v", tt.tier, gotProfessional, ok) }
    }
}
```

- [ ] **Step 2: Run the billing contract test and verify RED**

Run:

```bash
go test ./server/billing -run 'TestContentAddressedRetailCatalog|TestImageCapabilityTierPricesRemainDistinct' -count=1
```

Expected: FAIL because `image.standard` still has list price 500 and Enterprise price 400.

- [ ] **Step 3: Change the two list prices**

In `server/billing/products.yaml`:

```yaml
  - id: "image.standard"
    operation: "image.generate"
    charge_policy: "image_operation"
    price_credits: 300
    route: "image_generation.capabilities.standard"
    delivery: "persisted_image"
  - id: "image.professional"
    operation: "image.generate"
    charge_policy: "image_operation"
    price_credits: 500
    route: "image_generation.capabilities.professional"
    delivery: "persisted_image"
```

- [ ] **Step 4: Verify catalog publication and pricing tests GREEN**

Run:

```bash
go test ./server/billing ./server/service -run 'TestContentAddressedRetailCatalog|TestImageCapabilityTierPrices|TestBillingCatalog.*Tier|TestBillingCatalog.*Publish' -count=1
```

Expected: PASS. Confirm the tests prove a new content-addressed catalog is published rather than mutating historical rows.

- [ ] **Step 5: Commit the pricing change**

```bash
git add server/billing/products.yaml server/billing/catalog_contract_test.go
git commit -m "feat(billing): separate image capability prices"
```

### Task 2: Add Shared Quantity And Execution Toolbar Controls

**Files:**
- Create: `studio/src/components/agent-prompt/QuantityStepper.tsx`
- Create: `studio/src/components/agent-prompt/QuantityStepper.test.tsx`
- Create: `studio/src/components/agent-prompt/ComposerQuantityControl.tsx`
- Create: `studio/src/components/agent-prompt/ComposerQuantityControl.test.tsx`
- Create: `studio/src/components/tasks/ExecutionProfileToolbar.tsx`
- Create: `studio/src/components/tasks/ExecutionProfileToolbar.test.tsx`
- Modify: `studio/src/components/agent-prompt/ProjectContextControl.tsx`
- Modify: `studio/src/components/agent-prompt/ProjectContextControl.test.tsx`
- Modify: `studio/src/components/agent-prompt/ProjectIdentity.tsx`
- Modify: `studio/src/components/agent-prompt/ProjectIdentity.test.tsx`

- [ ] **Step 1: Write failing quantity component tests**

Create tests that define the semantic API:

```tsx
it('announces and clamps task quantity changes', () => {
  const onChange = vi.fn()
  render(<QuantityStepper label="任务数量" value={2} min={1} max={5} onChange={onChange} />)
  fireEvent.click(screen.getByRole('button', { name: '增加任务数量' }))
  fireEvent.click(screen.getByRole('button', { name: '减少任务数量' }))
  expect(onChange).toHaveBeenNthCalledWith(1, 3)
  expect(onChange).toHaveBeenNthCalledWith(2, 1)
  expect(screen.getByText('2')).toHaveAttribute('aria-label', '任务数量：2')
})

it('renders a static image limit instead of disabled controls', () => {
  render(<QuantityStepper label="图片数量" value={1} min={1} max={1} onChange={vi.fn()} />)
  expect(screen.getByText('图片数量 1 · 当前能力上限')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: /图片数量/ })).not.toBeInTheDocument()
})
```

Create `ComposerQuantityControl.test.tsx`:

```tsx
it('shows task quantity in the toolbar and edits it with the shared stepper', async () => {
  const onChange = vi.fn()
  render(<ComposerQuantityControl label="任务数量" value={2} min={1} max={5} onChange={onChange} />)
  fireEvent.click(screen.getByRole('button', { name: '任务数量：2' }))
  fireEvent.click(await screen.findByRole('button', { name: '增加任务数量' }))
  expect(onChange).toHaveBeenCalledWith(3)
})
```

- [ ] **Step 2: Write failing execution-toolbar and compact-project tests**

Define `ExecutionProfileToolbar` as a controlled wrapper:

```tsx
it('keeps the compact trigger and complete profile cards in sync', async () => {
  const onChange = vi.fn()
  render(<ExecutionProfileToolbar profiles={profiles} value="effective" onChange={onChange} catalog={catalog} taskType="article" />)
  const trigger = screen.getByRole('button', { name: /执行配置：性价比/ })
  expect(trigger).toHaveTextContent('性价比')
  expect(trigger).toHaveTextContent('4,800')
  fireEvent.click(trigger)
  fireEvent.click(await screen.findByRole('button', { name: /^平衡型，/ }))
  expect(onChange).toHaveBeenCalledWith('balanced')
})
```

Add this to `ProjectContextControl.test.tsx`:

```tsx
it('renders readonly project identity in compact toolbar form', () => {
  render(<ProjectContextControl mode="readonly" project={projects[0]} compact />)
  expect(screen.getByText('Morning Brief')).toBeInTheDocument()
  expect(screen.getByText('Daily editorial briefing')).toBeInTheDocument()
  expect(screen.getByText('公众号图文项目')).toBeInTheDocument()
  expect(document.querySelector('[data-slot="project-context-control"]')).toHaveAttribute('data-compact', 'true')
})

it('names the compact project trigger with the current selection', () => {
  render(<ProjectContextControl mode="select" projects={projects} value={projects[0].id} onValueChange={vi.fn()} compact />)
  expect(screen.getByRole('combobox', { name: '项目：Morning Brief' })).toBeInTheDocument()
})
```

- [ ] **Step 3: Run all new component tests and verify RED**

Run:

```bash
cd studio && bun run test -- src/components/agent-prompt/QuantityStepper.test.tsx src/components/agent-prompt/ComposerQuantityControl.test.tsx src/components/tasks/ExecutionProfileToolbar.test.tsx src/components/agent-prompt/ProjectContextControl.test.tsx
```

Expected: FAIL because the three new components and compact readonly prop do not exist.

- [ ] **Step 4: Implement `QuantityStepper` and `ComposerQuantityControl` minimally**

Use the existing button and popover primitives:

```tsx
export interface QuantityStepperProps {
  label: string
  value: number
  min: number
  max: number
  onChange: (value: number) => void
  disabled?: boolean
}

export function QuantityStepper({ label, value, min, max, onChange, disabled = false }: QuantityStepperProps) {
  if (min === max) {
    return <p aria-label={`${label}：${value}，当前能力上限`} className="text-sm text-muted-foreground">{label} {value} · 当前能力上限</p>
  }
  return (
    <section className="flex items-center justify-between gap-3">
      <h3 className="font-medium">{label}</h3>
      <div className="flex items-center gap-2">
        <Button type="button" variant="outline" size="icon-sm" aria-label={`减少${label}`} disabled={disabled || value <= min} onClick={() => onChange(Math.max(min, value - 1))}><MinusIcon /></Button>
        <span className="w-8 text-center text-sm font-medium tabular-nums" aria-label={`${label}：${value}`}>{value}</span>
        <Button type="button" variant="outline" size="icon-sm" aria-label={`增加${label}`} disabled={disabled || value >= max} onClick={() => onChange(Math.min(max, value + 1))}><PlusIcon /></Button>
      </div>
    </section>
  )
}
```

`ComposerQuantityControl` uses a ghost `Button` trigger containing the label and value, and a viewport-bounded `PopoverContent` containing `QuantityStepper`. For `min === max`, render the static `QuantityStepper` directly with no trigger.

- [ ] **Step 5: Implement the execution toolbar and compact readonly project state**

`ExecutionProfileToolbar` must compute the selected profile and existing `executionProfilePriceInfo`, render a compact trigger, and reuse the full selector:

```tsx
<Popover>
  <PopoverTrigger render={<Button type="button" variant="ghost" size="sm" aria-label={`执行配置：${selected?.display_name ?? '未选择'}`} />}>
    <GaugeIcon />
    <span>{selected?.display_name ?? '选择执行配置'}</span>
    {pricing?.price !== undefined ? <span>{pricing.price.toLocaleString()} 积分</span> : null}
    <ChevronDownIcon />
  </PopoverTrigger>
  <PopoverContent align="start" className="w-[min(42rem,calc(100vw-2rem))] p-4">
    <ExecutionProfileSelector {...selectorProps} />
  </PopoverContent>
</Popover>
```

Add `compact?: boolean` to readonly project props, render `ProjectIdentity` with that value, and add `data-compact` for both select and readonly modes. In `ProjectIdentity`, compact changes spacing only: use a compact two-line layout while still rendering the avatar, name, localized type badge, and description/fallback. Remove the current `!compact` conditions around the type badge and description. For select mode, replace the default static `aria-label="项目上下文"` with `项目：${selected.name}` or `项目：未选择`; retain an explicitly supplied `ariaLabel` override. Do not change full option rendering or hide project identity fields to make the toolbar shorter.

- [ ] **Step 6: Run component tests and verify GREEN**

Run the Step 3 command again.

Expected: PASS with no React accessibility or nested-button warnings.

- [ ] **Step 7: Commit shared controls**

```bash
git add studio/src/components/agent-prompt/QuantityStepper.tsx studio/src/components/agent-prompt/QuantityStepper.test.tsx studio/src/components/agent-prompt/ComposerQuantityControl.tsx studio/src/components/agent-prompt/ComposerQuantityControl.test.tsx studio/src/components/tasks/ExecutionProfileToolbar.tsx studio/src/components/tasks/ExecutionProfileToolbar.test.tsx studio/src/components/agent-prompt/ProjectContextControl.tsx studio/src/components/agent-prompt/ProjectContextControl.test.tsx studio/src/components/agent-prompt/ProjectIdentity.tsx studio/src/components/agent-prompt/ProjectIdentity.test.tsx
git commit -m "feat(studio): add composer parameter controls"
```

### Task 3: Extend AI Entry With Explicit Parameters And Task Collections

**Files:**
- Modify: `server/service/ai_entry.go:24-39,99-224`
- Modify: `server/service/ai_entry_test.go`
- Modify: `server/service/image_capability_resolver.go:13-137`
- Modify: `server/service/image_capability_resolver_test.go`
- Modify: `server/service/task.go:492-621`
- Modify: `server/service/task_capability_test.go`
- Modify: `server/handler/image_capability.go`
- Modify: `server/handler/image_capability_test.go`
- Modify: `server/handler/ai_entry.go:30-67`
- Modify: `server/handler/ai_entry_test.go`
- Modify: `server/handler/task.go`
- Modify: `server/handler/task_test.go`
- Modify: `server/handler/plan.go`
- Modify: `server/handler/plan_test.go`
- Modify: `server/handler/designer.go`
- Create: `server/handler/designer_capability_test.go`
- Modify: `server/service/ilink_conversation.go:71-105`
- Modify: `server/service/ilink_conversation_test.go`
- Modify: `studio/src/lib/api/ai-entry.ts:13-25`
- Create: `studio/src/lib/api/ai-entry.test.ts`

- [ ] **Step 1: Write failing handler contract tests**

Extend the successful forwarding body with:

```json
"quantity": 2,
"image_ratio": "3:4",
"image_capability_key": "professional"
```

Assert:

```go
if submitter.req.Quantity == nil || *submitter.req.Quantity != 2 || submitter.req.ImageRatio != "3:4" || submitter.req.ImageCapabilityKey != "professional" {
    t.Fatalf("explicit fields = %#v", submitter.req)
}
```

Add a table test for `quantity: 0` and `quantity: 6` that expects HTTP 400 with `quantity must be between 1 and 5` and verifies the submitter was not called. Add a separate omitted-quantity case that reaches the submitter with `Quantity == nil`; the service, not JSON zero-value decoding, normalizes omission to one.

Add handler mapping tests for typed capability errors:

```go
{err: service.ErrImageCapabilityUnavailable, status: fiber.StatusConflict, code: 46011, msg: "image_capability_stale"},
{err: service.ErrImageCapabilityAccessDenied, status: fiber.StatusForbidden, code: 46012, msg: "image_capability_access_denied"},
```

Add the same response assertions to task create/clone, plan create/update, and
Designer quote/generate handler tests so every Studio capability control has the
same recovery signal.

- [ ] **Step 2: Write failing service tests for precedence and batch results**

Add a service test where the LLM returns conflicting values but the request wins:

```go
quantity := 2
result, err := entrySvc.Submit(ctx, AIEntrySubmitRequest{
    UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "write",
    Quantity: &quantity, ImageRatio: "16:9", ImageCapabilityKey: "professional",
})
if err != nil { t.Fatal(err) }
if len(result.Tasks) != 2 { t.Fatalf("tasks = %d", len(result.Tasks)) }
for _, task := range result.Tasks {
    if task.ImageRatio != "16:9" || task.ImageCapabilityKey != "professional" {
        t.Fatalf("explicit image fields lost: %#v", task)
    }
}
```

Configure the test task service with a capability registry that authorizes `professional`; do not bypass production authorization. Keep the existing unsafe-LLM-field test and change it to assert that LLM-provided capability keys remain ignored when the request omitted the field.

In `server/service/task_capability_test.go`, add table cases that call `CreateManual` with an unknown key, a configured but disabled key, and an Enterprise-only key as a Free user. Assert the first two wrap `ErrImageCapabilityUnavailable`, the tier case wraps `ErrImageCapabilityAccessDenied`, no task is persisted, and no enqueue occurs. Add a valid-key case that proves the resolver's canonical key is stored. These tests defend the shared service boundary used by AI Entry and ordinary task creation.

Add an iLink test asserting its omitted quantity defaults to one and the success reply uses `result.Tasks[0]`.

- [ ] **Step 3: Run targeted Go tests and verify RED**

Run:

```bash
go test ./server/handler ./server/service -run 'TestAIEntry|TestIlinkConversation.*Create|Test.*ImageCapabilitySelection' -count=1
```

Expected: FAIL because the request fields and `Tasks` result do not exist.

- [ ] **Step 4: Implement request normalization and service precedence**

Update the service types:

```go
type AIEntrySubmitRequest struct {
    UserID string `json:"-"`
    Channel string `json:"channel"`
    ProjectID string `json:"project_id"`
    ExecutionProfile string `json:"execution_profile"`
    Text string `json:"text"`
    Attachments []model.EntryAttachment `json:"attachments,omitempty"`
    Quantity *int `json:"quantity,omitempty"`
    ImageRatio string `json:"image_ratio,omitempty"`
    ImageCapabilityKey string `json:"image_capability_key,omitempty"`
    ExecutionTarget string `json:"execution_target,omitempty"`
}

type AIEntrySubmitResult struct {
    Status string `json:"status"`
    Tasks []*model.Task `json:"tasks,omitempty"`
    Message string `json:"message,omitempty"`
    ActionURL string `json:"action_url,omitempty"`
}
```

In the handler, trim the image fields and reject a non-nil quantity outside 1-5. Do not replace a nil quantity in the handler: the pointer is the presence bit that distinguishes omission from explicit JSON zero. In `AIEntryService.Submit`, normalize nil to one and independently reject non-nil values outside 1-5 so non-HTTP callers have the same invariant. Platform-specific ratio validation stays in `AIEntryService`/`TaskService`, where the selected project platform is available, keeping the Fiber handler thin.

Build manual parameters with explicit precedence:

```go
imageRatio := strings.TrimSpace(req.ImageRatio)
if imageRatio == "" {
    imageRatio = normalizeAIEntryImageRatio(intent.ImageRatio)
}
params := CreateManualParams{
    UserID: req.UserID, ProjectID: req.ProjectID,
    ExecutionProfile: req.ExecutionProfile, Prompt: prompt,
    Quantity: normalizedQuantity, ImageRatio: imageRatio,
    ImageCapabilityKey: strings.TrimSpace(req.ImageCapabilityKey),
    InputAttachments: normalizeEntryAttachments(req.Attachments),
    ExecutionTarget: normalizeAIEntryExecutionTarget(req.ExecutionTarget),
    ProjectSnapshot: &projectSnapshot,
}
```

Move image capability enforcement to the shared task service boundary. Add typed sentinels in `image_capability_resolver.go` and wrap them from `resolveRoute`:

```go
var (
    ErrImageCapabilityUnavailable = errors.New("image capability unavailable")
    ErrImageCapabilityAccessDenied = errors.New("image capability access denied")
)
```

Unknown and disabled keys wrap `ErrImageCapabilityUnavailable`; insufficient tier wraps `ErrImageCapabilityAccessDenied`. In `TaskService.CreateManual`, after ecommerce defaults are merged and before any task is built or persisted, call `ResolvePublicImageCapability` when the resolver is configured and `PreserveFrozenConfig` is false, then store the returned canonical key. Update the stale comment that currently says capability validation happens only upstream.

Change `ValidateImageCapabilityKey` to wrap the same sentinels, then add one handler helper in `image_capability.go`:

```go
func respondImageCapabilitySelectionError(c fiber.Ctx, err error) (bool, error) {
    switch {
    case errors.Is(err, service.ErrImageCapabilityUnavailable), errors.Is(err, service.ErrDesignerCapabilityUnavailable):
        return true, c.Status(fiber.StatusConflict).JSON(Response{Code: 46011, Msg: "image_capability_stale"})
    case errors.Is(err, service.ErrImageCapabilityAccessDenied), errors.Is(err, service.ErrDesignerCapabilityAccessDenied):
        return true, c.Status(fiber.StatusForbidden).JSON(Response{Code: 46012, Msg: "image_capability_access_denied"})
    default:
        return false, nil
    }
}
```

`AIEntryService.Submit` must return these typed errors to the handler instead of converting them into a successful HTTP envelope with `status: error`. AI Entry, task create/clone, plan create/update, and Designer quote/generate call the helper before their generic error mapping. The helper never returns resolver internals and gives all four Studio surfaces the same stable refetch signal.

After `CreateManual`, attach the presented reference view to every returned task, return `Tasks: tasks`, and format `已创建 %d 个任务。` when count exceeds one. Do not retain the obsolete singular response field after Studio and iLink consumers are updated.

- [ ] **Step 5: Update Studio and iLink consumers**

In `studio/src/lib/api/ai-entry.ts`:

```ts
export interface AIEntrySubmitRequest {
  channel: 'studio' | 'ilink' | string
  project_id: string
  text: string
  attachments?: AIEntryAttachment[]
  execution_profile: AgentExecutionProfileID
  quantity?: number
  image_ratio?: string
  image_capability_key?: string
}

export interface AIEntrySubmitResult {
  status: AIEntryStatus
  tasks?: Task[]
  message?: string
  action_url?: string
}
```

In iLink, require `len(result.Tasks) > 0`, select the first task, and keep the request quantity omitted so it defaults to one.

Create `studio/src/lib/api/ai-entry.test.ts` unconditionally with this transport contract:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest'

import { http } from '@/lib/http-client'
import { aiEntryApi } from './ai-entry'

describe('aiEntryApi', () => {
  afterEach(() => vi.restoreAllMocks())

  it('posts explicit composer parameters and unwraps plural tasks', async () => {
    const payload = {
      channel: 'studio', project_id: 'project-1', text: 'write', execution_profile: 'effective',
      quantity: 2, image_ratio: '3:4', image_capability_key: 'professional',
    } as const
    const tasks = [{ id: 'task-1' }, { id: 'task-2' }]
    const post = vi.spyOn(http, 'post').mockResolvedValue({
      data: { code: 0, msg: 'success', data: { status: 'created', tasks } },
    } as never)

    await expect(aiEntryApi.submit(payload)).resolves.toMatchObject({ status: 'created', tasks })
    expect(post).toHaveBeenCalledWith('/ai-entry/submit', payload)
  })
})
```

- [ ] **Step 6: Run targeted tests and verify GREEN**

Run the Step 3 command plus:

```bash
cd studio && bun run test -- src/lib/api/ai-entry.test.ts
```

Expected: PASS. The dedicated API test must exist and assert the exact POST payload and plural response unwrapping.

- [ ] **Step 7: Commit the AI Entry contract**

```bash
git add server/service/ai_entry.go server/service/ai_entry_test.go server/service/image_capability_resolver.go server/service/image_capability_resolver_test.go server/service/task.go server/service/task_capability_test.go server/handler/image_capability.go server/handler/image_capability_test.go server/handler/ai_entry.go server/handler/ai_entry_test.go server/handler/task.go server/handler/task_test.go server/handler/plan.go server/handler/plan_test.go server/handler/designer.go server/handler/designer_capability_test.go server/service/ilink_conversation.go server/service/ilink_conversation_test.go studio/src/lib/api/ai-entry.ts studio/src/lib/api/ai-entry.test.ts
git commit -m "feat: accept explicit AI entry parameters"
```

### Task 4: Unify Homepage Composer Parameters

**Files:**
- Modify: `studio/src/pages/DashboardPage.tsx`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`

- [ ] **Step 1: Write failing homepage interaction tests**

Extend the existing successful submit test to assert the bottom toolbar order and request:

```tsx
const composer = document.querySelector('[data-slot="agent-prompt-input"]')!
const project = within(composer).getByRole('combobox', { name: /^项目：Morning Brief/ })
const execution = within(composer).getByRole('button', { name: /^执行配置：/ })
const image = within(composer).getByRole('button', { name: /^图像设置：/ })
const quantity = within(composer).getByRole('button', { name: '任务数量：1' })
expect(project.compareDocumentPosition(execution) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
expect(execution.compareDocumentPosition(image) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
expect(image.compareDocumentPosition(quantity) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

fireEvent.click(within(composer).getByRole('button', { name: '任务数量：1' }))
fireEvent.click(await screen.findByRole('button', { name: '增加任务数量' }))
// Select 3:4 and Professional through the existing image settings popover.
```

Then assert:

```tsx
await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith(expect.objectContaining({
  quantity: 2,
  image_ratio: '3:4',
  image_capability_key: 'professional',
})))
```

Add result routing cases: one returned task navigates to `/tasks/:id`; two tasks navigate to `/tasks` and show the server success message. Add stale (`code: 46011`) and access-denied (`code: 46012`) rejection cases; assert `queryClient.invalidateQueries({ queryKey: queryKeys.imageCapabilities.all })` runs, the invalid selection is cleared, and Studio asks the user to select an image capability again. Resolve the refreshed capability query and assert the default is not automatically reselected and submit stays blocked until the user makes a new selection.

- [ ] **Step 2: Run the homepage test and verify RED**

```bash
cd studio && bun run test -- src/pages/DashboardPage.ai-entry.test.tsx
```

Expected: FAIL because homepage lacks image settings, quantity, compact execution, and plural result handling.

- [ ] **Step 3: Add homepage state and catalog-driven defaults**

Use `useImageCapabilities`, platform configs, and `normalizeImageRatio`. Add state:

```tsx
const [quantity, setQuantity] = useState(1)
const [imageRatio, setImageRatio] = useState<ImageRatio>('auto')
const [imageCapabilityKey, setImageCapabilityKey] = useState('')
const [imageCapabilityNeedsReselection, setImageCapabilityNeedsReselection] = useState(false)
const taskQuantityMax = selectedProject && ['article', 'seednote', 'moments'].includes(selectedProject.platform) ? 5 : 1
```

When project changes, clear `imageCapabilityNeedsReselection`, set the semantic ratio from the project default, clamp quantity to `taskQuantityMax`, and choose the current catalog default capability without overwriting a still-valid selection. Catalog-only refresh may choose a default only when `imageCapabilityNeedsReselection` is false. An explicit capability selection clears the latch.

- [ ] **Step 4: Assemble the horizontal toolbar and submit explicit fields**

Pass a single flex wrapper to `leadingTools` in this exact order:

```tsx
<ProjectContextControl mode="select" compact {...projectProps} />
<ExecutionProfileToolbar {...executionProps} />
<ImageGenerationToolbar {...imageProps} />
<ComposerQuantityControl label="任务数量" value={quantity} min={1} max={taskQuantityMax} onChange={setQuantity} />
```

Remove the separate execution-profile block below the Prompt. Submit:

```tsx
quantity,
image_ratio: imageRatio,
image_capability_key: imageCapabilityKey,
```

Handle `result.tasks?.length` as specified by the tests. In the mutation error handler, use `getApiErrorCode` to detect `46011` or `46012`, set `imageCapabilityNeedsReselection` before clearing `imageCapabilityKey`, invalidate the image-capability query, and show `图像能力已变化，请重新选择`; do not retry submission automatically. Disable submit while the latch is true or no capability is selected.

- [ ] **Step 5: Verify homepage tests GREEN**

Run the Step 2 command.

Expected: PASS, including project changes, unavailable profiles/capabilities, one-task routing, and multi-task routing.

- [ ] **Step 6: Commit the homepage change**

```bash
git add studio/src/pages/DashboardPage.tsx studio/src/pages/DashboardPage.ai-entry.test.tsx
git commit -m "feat(studio): unify homepage composer parameters"
```

### Task 5: Move Task Form Controls Into The Prompt Toolbar

**Files:**
- Modify: `studio/src/components/tasks/TaskFormDialog.tsx`
- Modify: `studio/src/components/tasks/TaskFormDialog.test.tsx`

- [ ] **Step 1: Write failing task-form structure and semantics tests**

Update `renders create mode with the shared operational controls` to assert:

```tsx
const composer = dialog.querySelector('[data-slot="agent-prompt-input"]')!
expect(within(composer).getByRole('combobox', { name: /^项目：Morning Brief/ })).toBeInTheDocument()
expect(within(composer).getByRole('button', { name: /^执行配置：/ })).toBeInTheDocument()
expect(within(composer).getByRole('button', { name: /^图像设置：/ })).toBeInTheDocument()
expect(within(composer).getByRole('button', { name: '任务数量：1' })).toBeInTheDocument()
expect(within(dialog).queryByText(/^数量$/)).not.toBeInTheDocument()
```

`AgentPromptInput` already exposes `data-slot="agent-prompt-input"`. Query it with `dialog.querySelector('[data-slot="agent-prompt-input"]')!` and scope with `within`; do not add a second test-only selector. Add a test that changes task quantity to two and verifies the existing `quantity: 2` request and cost total.

Switch the form to Montage and assert its `briefField` composer contains project
and execution controls, contains no image/quantity control, and no separate
project context bar or execution block remains. Add `46011` and `46012` submit
rejection cases that resolve the refreshed catalog and prove capability remains
empty and submit remains blocked until explicit reselection.

- [ ] **Step 2: Run task-form tests and verify RED**

```bash
cd studio && bun run test -- src/components/tasks/TaskFormDialog.test.tsx
```

Expected: FAIL because project is in the top context bar, execution is a separate form block, and quantity is standalone.

- [ ] **Step 3: Recompose normal task controls**

For every non-viral `AgentPromptInput`, including the composer passed to
`MontageCreationPanel.briefField`, remove `contextBar={projectContextBar}` and
use `leadingTools` containing:

```tsx
const taskQuantityMax = ['article', 'seednote', 'moments'].includes(watchedType) ? 5 : 1

<div data-slot="composer-settings" className="flex min-w-0 flex-wrap items-center gap-1">
  {projectContextControl}
  <ExecutionProfileToolbar profiles={executionProfilesQuery.data ?? []} value={watchedExecutionProfile} onChange={(value) => setFormValue('execution_profile', value)} loading={executionProfilesQuery.isLoading} catalog={billingCatalog} taskType={watchedType} />
  {!isMontageTask ? <ImageGenerationToolbar {...imageToolbarProps} /> : null}
  {!isMontageTask ? <ComposerQuantityControl label="任务数量" value={quantity} min={1} max={taskQuantityMax} onChange={(value) => setFormValue('quantity', value)} /> : null}
</div>
```

Keep the attachment add button before these controls. Remove the separate execution `FormField` and standalone quantity section. Preserve the existing cost preview and footer submit label.

Montage uses project and execution only in its Prompt toolbar; its own source,
pipeline, duration, subtitle, voiceover, style, music, and delivery inputs remain
below through `MontageCreationPanel`. Ecommerce shows the shared static
one-task state. Viral analysis keeps its specialized textarea and places its
compact project/execution controls immediately below that textarea. All three
keep quantity authoritative at one and expose no enabled stepper.

Add `imageCapabilityNeedsReselection` to `TaskFormDialog`. On API code `46011`
or `46012`, set the latch, clear the selected key, invalidate
`queryKeys.imageCapabilities.all`, show the reselection message, and block
submit. Catalog refresh cannot apply a default while latched; project change or
an explicit capability selection clears the latch.

- [ ] **Step 4: Verify task-form tests GREEN**

Run the Step 2 command.

Expected: PASS for create, clone, project switching, quantity, cost preview, image settings, and special task types.

- [ ] **Step 5: Commit the task form change**

```bash
git add studio/src/components/tasks/TaskFormDialog.tsx studio/src/components/tasks/TaskFormDialog.test.tsx
git commit -m "feat(studio): move task parameters into composer"
```

### Task 6: Move Plan Controls Into The Prompt Toolbar

**Files:**
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`

- [ ] **Step 1: Write failing plan structure tests**

In the create-plan dialog test, assert the composer contains project, execution, and image settings but no quantity:

```tsx
const dialog = await screen.findByRole('dialog', { name: '新建计划' })
const composer = dialog.querySelector('[data-slot="agent-prompt-input"]')!
expect(within(composer).getByRole('combobox', { name: /^项目：Morning Brief/ })).toBeInTheDocument()
expect(within(composer).getByRole('button', { name: /^执行配置：/ })).toBeInTheDocument()
expect(within(composer).getByRole('button', { name: /^图像设置：/ })).toBeInTheDocument()
expect(within(composer).queryByText(/任务数量|图片数量/)).not.toBeInTheDocument()
expect(within(dialog).getByText('排期设置')).toBeInTheDocument()
expect(within(dialog).queryByLabelText('内容类型')).not.toBeInTheDocument()
```

Add edit coverage for compact readonly project identity in the composer. Add
`46011` and `46012` create/update rejection cases that resolve the refreshed
catalog and prove image capability remains empty and save stays blocked until
explicit reselection.

- [ ] **Step 2: Run plan tests and verify RED**

```bash
cd studio && bun run test -- src/pages/PlansPage.test.tsx
```

Expected: FAIL because project, type, and execution are separate fields above the Prompt.

- [ ] **Step 3: Recompose plan controls and preserve scheduling**

Move project selection callbacks into a compact `ProjectContextControl` passed through `leadingTools`. For editing, use:

```tsx
<ProjectContextControl mode="readonly" project={selectedProject ?? null} compact />
```

Place `ExecutionProfileToolbar` after project and `ImageGenerationToolbar` after execution. Remove the visible content-type `Select`; continue setting `form.type` from `project.platform` and submitting it unchanged. Keep `SchedulePicker` and `TaskTimePricingNotice` outside and above `promptComposer`. Keep plans quantity-free.

Add `imageCapabilityNeedsReselection` to the plan dialog state and use the same
code `46011`/`46012` recovery invariant as Dashboard: latch, clear, invalidate,
message, and block save. Catalog refresh cannot restore a default while latched;
project change or explicit selection clears it.

- [ ] **Step 4: Verify plan tests GREEN**

Run the Step 2 command.

Expected: PASS for create/edit, derived type, schedule validity, time pricing, image settings, and exact per-run execution price.

- [ ] **Step 5: Commit the plan form change**

```bash
git add studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx
git commit -m "feat(studio): unify plan composer parameters"
```

### Task 7: Align Designer Project And Quantity Presentation

**Files:**
- Modify: `studio/src/components/designer/DesignerGenerationToolbar.tsx`
- Modify: `studio/src/components/designer/DesignerGenerationToolbar.test.tsx`
- Modify: `studio/src/pages/DesignerPage.tsx`
- Modify: `studio/src/pages/DesignerPage.provider-contract.test.ts`
- Modify: `studio/src/pages/DesignerPage.billing.test.tsx`
- Modify: `studio/src/pages/DesignerPage.reference-drop.test.tsx`

- [ ] **Step 1: Write failing Designer toolbar tests**

Replace the current disabled-stepper expectation for `maxBatch: 1` with:

```tsx
fireEvent.click(screen.getByRole('button', { name: /图像设置：/ }))
expect(screen.getByText('图片数量 1 · 当前能力上限')).toBeInTheDocument()
expect(screen.queryByRole('button', { name: '增加图片数量' })).not.toBeInTheDocument()
expect(screen.queryByRole('button', { name: '减少图片数量' })).not.toBeInTheDocument()
```

Keep a component-only fixture with `maxBatch: 3` and assert the shared stepper calls `onSettingsChange({ n: 2 })`; this proves the presentation is capability-driven without claiming Server batch billing is enabled in production.

In the provider contract test, assert project is inside the Prompt bottom addon and no `contextBar` prop is supplied.

Add quote/generate rejection cases for API codes `46011` and `46012`. Resolve
the refreshed capability catalog and assert the invalid capability remains
empty and Generate stays blocked until the user explicitly selects again.

- [ ] **Step 2: Run Designer tests and verify RED**

```bash
cd studio && bun run test -- src/components/designer/DesignerGenerationToolbar.test.tsx src/pages/DesignerPage.provider-contract.test.ts src/pages/DesignerPage.billing.test.tsx src/pages/DesignerPage.reference-drop.test.tsx
```

Expected: FAIL because Designer still renders disabled plus/minus buttons at one and project remains in the top context bar.

- [ ] **Step 3: Reuse the quantity stepper and move project**

Replace the quantity section in `DesignerGenerationToolbar` with:

```tsx
<QuantityStepper
  label="图片数量"
  value={settings.n}
  min={1}
  max={maxBatch}
  onChange={(n) => onSettingsChange({ n })}
  disabled={disabled}
/>
```

In `DesignerPage`, remove `contextBar={contextBar}` and pass a flex `leadingTools` group in this order:

```tsx
<ProjectContextControl mode="select" compact {...projectProps} />
<DesignerGenerationToolbar {...generationToolbarProps} />
```

Add `imageCapabilityNeedsReselection` to `DesignerPage` and apply the same
latch/clear/invalidate/message/block invariant. Catalog refresh cannot restore a
default while latched; project change or explicit capability selection clears
the latch.

Do not raise canonical `max_batch`, remove the Server's `n == 1` check, or multiply billing in this task.

- [ ] **Step 4: Verify Designer tests GREEN**

Run the Step 2 command.

Expected: PASS with static `图片数量 1 · 当前能力上限`, project selection in the bottom toolbar, and unchanged generation request `n: 1`.

- [ ] **Step 5: Commit the Designer change**

```bash
git add studio/src/components/designer/DesignerGenerationToolbar.tsx studio/src/components/designer/DesignerGenerationToolbar.test.tsx studio/src/pages/DesignerPage.tsx studio/src/pages/DesignerPage.provider-contract.test.ts studio/src/pages/DesignerPage.billing.test.tsx studio/src/pages/DesignerPage.reference-drop.test.tsx
git commit -m "feat(studio): align designer composer controls"
```

### Task 8: Cross-Surface Verification And Browser QA

**Files:**
- Modify only if a failing verification identifies a regression in files already listed above.
- Do not add screenshots, traces, or temporary browser scripts to the repository.

- [ ] **Step 1: Run focused Studio regression tests**

```bash
cd studio && bun run test -- \
  src/components/agent-prompt/QuantityStepper.test.tsx \
  src/components/agent-prompt/ComposerQuantityControl.test.tsx \
  src/components/agent-prompt/ProjectContextControl.test.tsx \
  src/components/tasks/ExecutionProfileToolbar.test.tsx \
  src/components/tasks/TaskFormDialog.test.tsx \
  src/components/designer/DesignerGenerationToolbar.test.tsx \
  src/pages/DashboardPage.ai-entry.test.tsx \
  src/pages/PlansPage.test.tsx \
  src/pages/DesignerPage.provider-contract.test.ts \
  src/pages/DesignerPage.billing.test.tsx \
  src/pages/DesignerPage.reference-drop.test.tsx
```

Expected: PASS with zero unhandled promise rejections or React warnings.

- [ ] **Step 2: Run focused Go regression tests**

```bash
go test ./server/billing ./server/handler ./server/service -run 'TestContentAddressedRetailCatalog|TestImageCapabilityTierPrices|TestBillingCatalog|TestAIEntry|TestIlinkConversation.*Create|Test.*ImageCapabilitySelection|TestDesigner.*Batch' -count=1
```

Expected: PASS and retain explicit coverage that Designer rejects `n>1`.

- [ ] **Step 3: Run full repository verification**

Use the bundled Node runtime if host Bun hits the known macOS runtime issue:

```bash
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bash -lc 'cd studio && bun run test && bun run build'
go test ./...
go build -o /tmp/anban-creator-server ./server
```

Expected: full Studio tests PASS, TypeScript/Vite build PASS, all Go tests PASS, and the Server binary builds successfully.

- [ ] **Step 4: Start Studio and define browser flows**

Start the local development server on an unused port. The flows under test are:

```text
Homepage -> select project/profile/image settings/task quantity -> submit -> one or multiple task navigation
Tasks -> open create dialog -> change all bottom-toolbar controls -> cost updates -> submit
Plans -> open create/edit dialog -> change bottom-toolbar controls -> schedule remains valid -> submit
Designer -> select project -> open image settings -> static image limit -> generate one image
```

- [ ] **Step 5: Run in-app Browser desktop and mobile QA**

Use the Browser plugin at `1440x900` and `390x844`. For every flow verify:

```text
page identity
meaningful nonblank DOM
no Vite/framework overlay
no relevant console errors or warnings
project -> execution -> image -> quantity order where applicable
horizontal desktop toolbar and ordered mobile wrapping
no overlap, clipping, horizontal page overflow, or hidden submit action
popover selection updates the compact trigger
task quantity and image quantity labels remain distinct
```

Capture screenshots outside the repository for the four desktop surfaces and at least homepage/task mobile wrapping.

- [ ] **Step 6: Review the complete diff**

```bash
git fetch origin main
git diff --check
git status --short
git diff origin/main --check
git diff --stat origin/main
git log --oneline origin/main..HEAD
```

Expected: both whitespace checks are silent; the explicit `origin/main` diff includes every implementation commit plus any current worktree fixes, and contains only planned source/tests/docs. Pre-existing unrelated untracked files remain unstaged. Confirm no plugin manifest bump is needed because this plan does not modify `plugins/`.

- [ ] **Step 7: Commit any verification-only fixes**

If Step 1-6 required changes, stage only the planned implementation surfaces
that actually changed and commit:

```bash
git add \
  server/billing/products.yaml server/billing/catalog_contract_test.go \
  server/service/ai_entry.go server/service/ai_entry_test.go \
  server/service/image_capability_resolver.go server/service/image_capability_resolver_test.go \
  server/service/task.go server/service/task_capability_test.go \
  server/handler/image_capability.go server/handler/image_capability_test.go \
  server/handler/ai_entry.go server/handler/ai_entry_test.go \
  server/handler/task.go server/handler/task_test.go \
  server/handler/plan.go server/handler/plan_test.go \
  server/handler/designer.go server/handler/designer_capability_test.go \
  server/service/ilink_conversation.go server/service/ilink_conversation_test.go \
  studio/src/lib/api/ai-entry.ts studio/src/lib/api/ai-entry.test.ts \
  studio/src/components/agent-prompt/QuantityStepper.tsx \
  studio/src/components/agent-prompt/QuantityStepper.test.tsx \
  studio/src/components/agent-prompt/ComposerQuantityControl.tsx \
  studio/src/components/agent-prompt/ComposerQuantityControl.test.tsx \
  studio/src/components/agent-prompt/ProjectContextControl.tsx \
  studio/src/components/agent-prompt/ProjectContextControl.test.tsx \
  studio/src/components/agent-prompt/ProjectIdentity.tsx \
  studio/src/components/agent-prompt/ProjectIdentity.test.tsx \
  studio/src/components/tasks/ExecutionProfileToolbar.tsx \
  studio/src/components/tasks/ExecutionProfileToolbar.test.tsx \
  studio/src/components/tasks/TaskFormDialog.tsx \
  studio/src/components/tasks/TaskFormDialog.test.tsx \
  studio/src/components/designer/DesignerGenerationToolbar.tsx \
  studio/src/components/designer/DesignerGenerationToolbar.test.tsx \
  studio/src/pages/DashboardPage.tsx studio/src/pages/DashboardPage.ai-entry.test.tsx \
  studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx \
  studio/src/pages/DesignerPage.tsx studio/src/pages/DesignerPage.provider-contract.test.ts \
  studio/src/pages/DesignerPage.billing.test.tsx studio/src/pages/DesignerPage.reference-drop.test.tsx
git commit -m "fix: complete composer parameter verification"
```

If no files changed, do not create an empty commit.
