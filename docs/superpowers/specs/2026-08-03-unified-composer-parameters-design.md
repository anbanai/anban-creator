# Unified Composer Parameters Design

**Date:** 2026-08-03

## Goal

Give the Studio homepage, task creation, plan creation, and Designer the same
Prompt-centered parameter grammar without pretending that different business
operations have identical fields. Project, execution, image, and quantity
controls live in one horizontal toolbar below the Prompt. Each surface includes
only the controls that have a real meaning for that operation.

This change also separates the retail prices of Standard and Professional image
capabilities and removes the Designer's misleading disabled quantity stepper
when the selected capability can only produce one image.

## Product Decisions

1. Composer parameters render below the Prompt in a horizontal toolbar. The
   stable order is project, execution configuration, image settings, then
   quantity. The toolbar wraps in that order when it cannot fit on one line.
2. Project remains the primary choice. Its compact trigger and selection panel
   retain avatar, name, description, and localized content type.
3. Task quantity and image quantity are different concepts. Task-producing
   surfaces use the label `任务数量`; Designer uses `图片数量`.
4. Quantity keeps the current Designer stepper interaction: decrement button,
   current value, and increment button. It is not replaced by a row of numbered
   options.
5. A quantity with a maximum of one renders as static limit information rather
   than two disabled buttons that imply an unavailable interaction.
6. A plan execution creates exactly one task. Plans therefore do not expose a
   task-quantity control.
7. Standard and Professional image capability list prices become 300 and 500
   credits respectively. Existing tier rates remain unchanged, so an Enterprise
   user sees 240 and 400 credits at the current 80 percent rate.
8. Explicit homepage image settings and task quantity are authoritative. The AI
   intent parser may fill omitted creative details but must not override fields
   selected by the user.

## Surface Matrix

| Surface | Project | Execution | Image settings | Quantity |
| --- | --- | --- | --- | --- |
| Homepage | Project context | Agent execution profile | Semantic ratio and image capability | `任务数量`, 1-5 where supported; otherwise static one |
| Create/clone task | Project context | Agent execution profile | Semantic ratio and image capability | `任务数量`, 1-5 where the task type supports batching |
| Create/edit plan | Project context | Agent execution profile | Semantic ratio and image capability | None; one task per run |
| Designer | Optional project context | Image capability is the execution choice | Capability, size, quality, and output format | `图片数量`, 1 through the supported batch maximum |

Agent execution profiles do not appear in Designer because the selected image
capability is its execution route. Designer sizes remain provider-backed fixed
presets; task and plan image ratios remain semantic business ratios. The common
toolbar is a presentation contract, not a reason to merge these domain models.

## Information Architecture

### Shared Composer Toolbar

`AgentPromptInput` continues to own the Prompt, ordered attachments, status, and
submission action. Its bottom addon becomes the single settings location for
the four Studio surfaces. Callers compose controls through the existing toolbar
slots instead of placing project or execution selectors above or below the
composer.

The toolbar uses compact triggers. Opening a trigger shows the existing complete
selection content:

- Project shows the full project identity and search behavior.
- Execution configuration shows profile description, access tier, exact price,
  multiplier, availability, and unavailable reason.
- Task image settings show semantic ratio and image capability.
- Designer image settings show capability-backed size, quality, output format,
  and other supported provider options.
- Quantity shows its semantic label, current value, and bounds.

The selected project content type remains visible in the project control. The
backend `Project.Platform` and task or plan `type` fields remain distinct; the
UI must not introduce a compatibility field that combines them.

### Page-Specific Content

Task-only advanced fields remain below the Prompt. This includes watermark,
goal mode, article image composition, Seednote image composition, ecommerce
modules, and Montage inputs. The existing project-snapshot notice may remain
outside the composer because it is immutable context information, not a
selectable parameter.

Plan schedule remains outside the Prompt toolbar and above the Prompt because
it controls when the operation runs rather than what the Agent creates. The
redundant disabled content-type selector is replaced by derived content-type
metadata while the submitted `type` continues to be set from the selected
project platform.

Designer keeps its full-bleed canvas. Only the project trigger moves from the
top context bar into the bottom toolbar; the image-generation settings trigger
and submit status remain in that toolbar.

## Quantity Contract

### Task Quantity

The existing standalone task quantity section is removed. Homepage and task
creation use a compact `任务数量` control in the Prompt toolbar. The range is
1-5 and the default is one. Task types that already force one task, including
Montage and ecommerce module-based generation, show a static one or omit the
control when the operation has another authoritative quantity model.

The submitted quantity continues to price and create independent tasks. Cost
preview, insufficient-credit checks, submit labels, success messages, and
navigation use the selected task count.

### Designer Image Quantity

Designer labels the field `图片数量`. The UI limit comes from the selected
capability, but it must never advertise a value the Server cannot quote and
charge. The current fixed-SKU backend accepts only `n=1`, and the canonical
Standard and Professional capability configuration both declare
`max_batch: 1`. Therefore this change renders `图片数量 1 · 当前能力上限` as
static information and does not claim that batch generation works.

Supporting `n>1` is a separate billing and execution feature. It requires a
defined per-image or batch SKU settlement rule, an atomic charge contract,
provider result-count validation, and retry/idempotency behavior. Merely raising
`max_batch` in configuration is explicitly out of scope.

## Homepage Request Contract

The homepage currently sends project, Prompt, attachments, and execution
profile while the AI Entry service forces quantity to one and derives image
ratio from intent parsing. The Studio request gains explicit optional fields:

```json
{
  "quantity": 2,
  "image_ratio": "3:4",
  "image_capability_key": "professional"
}
```

The handler validates quantity in the inclusive range 1-5, validates the
semantic ratio, and passes the capability key through the normal image
capability authorization path. The service uses explicit values when present.
Intent-derived values only fill missing values for non-Studio callers or older
request sources that legitimately omit the optional fields.

AI Entry returns the created task collection rather than assuming one task.
Studio navigates directly to the task detail when exactly one task was created
and to the task list with a success message when multiple tasks were created.
The iLink conversation adapter is updated to consume the collection and retain
its current single-task default.

No duplicate task-creation implementation is added to the handler. AI Entry
continues to call `TaskService.CreateManual`, which owns quantity limits,
catalog admission, task creation, and settlement.

## Image Capability Pricing

`server/billing/products.yaml` changes only these list prices:

- `image.standard`: 300 credits.
- `image.professional`: 500 credits.

Tier rates remain `free: 100`, `pro: 90`, and `enterprise: 80`. The published
catalog remains content-addressed and immutable. Changing the YAML creates a new
catalog identity and tier-price rows; existing quotes, charges, task snapshots,
and historical ledger evidence retain their original catalog and price.

Studio continues to display the current user's resolved price returned by the
Server, not the YAML list price and not a client-side discount calculation.
Standard and Professional must remain visibly distinct on every shared task
image toolbar and in Designer.

## Component Boundaries

### `AgentPromptInput`

- Keeps layout and accessibility for the Prompt and attachments.
- Accepts an ordered group of compact bottom-toolbar controls.
- Does not fetch projects, profiles, capabilities, or prices itself.
- Preserves stable toolbar order and wrapping without moving the submit action.

### Project Control

- Reuses `ProjectContextControl` data and selection behavior.
- Adds a compact toolbar-trigger presentation.
- Preserves loading, empty, no-project, read-only, create-project, search, and
  complete identity states.

### Execution Control

- Adds a compact trigger around the existing execution-profile selection data.
- Reuses `ExecutionProfileSelector` card content inside the popover.
- Keeps catalog pricing and availability authoritative.

### Quantity Control

- Is a presentational component with `label`, `value`, `min`, `max`, and
  `onChange`.
- Uses the existing stepper visual language.
- Renders a static limit state when `min === max`.
- Contains no task, image, billing, or provider logic.

### Page Owners

Dashboard, `TaskFormDialog`, `PlansPage`, and `DesignerPage` own their domain
state and assemble the toolbar controls. Shared controls remove visual drift,
while each page remains responsible for its request type and validation.

## Error Handling

- Project, profile, image capability, or price load failures keep their current
  explicit unavailable states and block submission when required.
- A stale or unauthorized image capability is rejected by the Server; Studio
  refreshes capabilities and asks the user to select again.
- Homepage quantity outside 1-5 returns a structured bad-request response and
  never reaches task creation.
- Homepage batch creation is atomic under the existing manual-task service
  transaction. A partial task collection is not reported as success.
- Missing Designer batch billing never degrades into an estimated charge. The
  UI remains at the Server-supported maximum of one.
- Compact popovers keep disabled reasons visible rather than silently removing
  unavailable execution profiles or image capabilities.

## Accessibility And Responsive Behavior

Toolbar controls have semantic accessible names that include the control type
and current selection, such as `项目：安伴品牌`, `执行配置：平衡型`,
`任务数量：2`, and `图片数量：1，当前能力上限`.

The toolbar is horizontal at available desktop widths and wraps from left to
right on narrow widths. Submit remains aligned at the trailing edge and never
gets covered by wrapping controls. Popovers fit within the viewport and return
focus to their trigger. Quantity buttons have explicit increment and decrement
names and announce the new value.

## Verification

### Studio Tests

- All four surfaces render composer controls in the approved order.
- Project and execution selectors no longer render as separate blocks outside
  the Prompt on homepage, task, and plan forms.
- Project popovers preserve full project identity and all loading/empty states.
- Execution popovers preserve exact catalog price, multiplier, tier, and
  unavailable reason.
- Homepage and task creation label and submit `任务数量`; Designer labels
  `图片数量`.
- Task quantity stepper enforces 1-5 and updates total cost and submit feedback.
- Designer with `maxBatch=1` renders static limit information with no disabled
  increment or decrement buttons.
- Plans expose no quantity control and still create one task per run.
- Homepage submits explicit ratio, capability, and quantity and handles one-task
  versus multi-task navigation.
- Desktop and mobile component tests defend toolbar order, wrapping, and submit
  action stability.

### Go Tests

- Billing catalog contract expects Standard 300 and Professional 500 list
  prices and the correct tier-adjusted values.
- Catalog publication creates a new immutable identity and preserves historical
  price evidence.
- AI Entry validates quantity, ratio, and capability and gives explicit fields
  precedence over parsed intent.
- AI Entry creates exactly the requested number of tasks and returns the full
  collection.
- iLink retains its one-task default against the updated result contract.
- Designer continues to reject `n>1` until a separately specified batch billing
  contract exists.

### Fresh Verification

- Run targeted Studio tests for the shared controls and four page owners.
- Run the full Studio test suite and `bun run build`.
- Run targeted billing, AI Entry, task, plan, and Designer Go tests.
- Run `go test ./...` and build the Server binary to `/tmp`.
- Use the in-app Browser to exercise homepage, task, plan, and Designer flows at
  desktop and mobile viewports, including popovers, wrapping, console health,
  one-task submission, and multi-task submission.

## Non-Goals

- Enabling Designer batch generation or defining batch image settlement.
- Merging `Project.Platform` with task or plan type.
- Moving task workflow orchestration into MCP handlers.
- Changing task execution-profile prices or time-of-day pricing.
- Changing project-level reference assets or ordered Prompt attachment
  semantics.
