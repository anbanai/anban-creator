# Composer Parameter Density Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the composer control to `创作参数` and arrange short parameter choices in compact horizontal rows while keeping long-form selectors vertical.

**Architecture:** Keep `TaskComposerParameters` and `DesignerGenerationToolbar` as the two popover owners. Apply a consistent fixed-label/minmax-controls grid to compact sections, while leaving `ExecutionProfileSelector` and image capability groups unchanged and vertical.

**Tech Stack:** React 19, TypeScript, Tailwind CSS v4, Base UI/shadcn, Vitest, Testing Library, Vite.

---

### Task 1: Compact Shared Task Parameters

**Files:**
- Modify: `studio/src/components/tasks/TaskComposerParameters.test.tsx`
- Modify: `studio/src/components/tasks/TaskComposerParameters.tsx`
- Modify: `studio/src/components/ImageGenerationToolbar.test.tsx`
- Modify: `studio/src/components/ImageGenerationToolbar.tsx`

- [x] **Step 1: Write failing accessible-name and layout assertions**

Update the shared parameter test to look up the trigger and dialog by `创作参数`, keep the execution group vertical, and assert the ratio and quantity sections use compact label-and-controls grids:

```tsx
const trigger = screen.getByRole('button', { name: /创作参数：/ })
fireEvent.click(trigger)
const popover = screen.getByRole('dialog', { name: '创作参数' })
expect(within(popover).getByRole('group', { name: 'Agent 执行配置' })).toHaveClass('grid-cols-1')
expect(within(popover).getByText('任务数量').closest('section')).toHaveClass(
  'grid-cols-[4.5rem_minmax(0,1fr)]',
)
```

Add the ratio-row assertion to `ImageGenerationToolbar.test.tsx`:

```tsx
expect(screen.getByText('图片比例').closest('section')).toHaveClass(
  'grid-cols-[4.5rem_minmax(0,1fr)]',
)
```

- [x] **Step 2: Run targeted tests and verify RED**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/tasks/TaskComposerParameters.test.tsx src/components/ImageGenerationToolbar.test.tsx
```

Expected: FAIL because the accessible name is still `创作设置` and compact sections do not have the grid class.

- [x] **Step 3: Implement the shared compact layout**

In `TaskComposerParameters.tsx`, replace all trigger/dialog text with `创作参数` and render quantity as a stable two-column row:

```tsx
aria-label={`创作参数：${summary}`}
<span>创作参数</span>
<PopoverTitle className="sr-only">创作参数</PopoverTitle>

<section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-2">
  <h3 className="font-medium">{quantity.label}</h3>
  <div className="flex min-w-0 justify-end">
    <QuantityStepper {...quantity} />
  </div>
</section>
```

In `ImageGenerationToolbar.tsx`, make only the short ratio section horizontal:

```tsx
<section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-2">
  <h3 className="pt-1 font-medium">图片比例</h3>
  <ToggleGroup className="flex min-w-0 flex-wrap justify-start" ...>
```

Keep the image-capability section and `ExecutionProfileSelector` vertical.

- [x] **Step 4: Run targeted tests and verify GREEN**

Run the Step 2 command again. Expected: both test files pass.

- [x] **Step 5: Commit the shared component change**

```bash
git add studio/src/components/tasks/TaskComposerParameters.tsx studio/src/components/tasks/TaskComposerParameters.test.tsx studio/src/components/ImageGenerationToolbar.tsx studio/src/components/ImageGenerationToolbar.test.tsx
git commit -m "feat(studio): compact shared composer parameters"
```

### Task 2: Compact Designer Parameters

**Files:**
- Modify: `studio/src/components/designer/DesignerGenerationToolbar.test.tsx`
- Modify: `studio/src/components/designer/DesignerGenerationToolbar.tsx`

- [x] **Step 1: Write failing Designer assertions**

Change the expected trigger/dialog name to `创作参数` and assert size, quality, output format, and quantity use compact rows while image capabilities remain vertical:

```tsx
fireEvent.click(screen.getByRole('button', { name: /^创作参数：/ }))
const dialog = screen.getByRole('dialog', { name: '创作参数' })
expect(within(dialog).getByRole('group', { name: '图像能力' })).toHaveClass('flex-col')
for (const label of ['尺寸', '质量', '输出格式']) {
  expect(within(dialog).getByText(label).closest('section')).toHaveClass(
    'grid-cols-[4.5rem_minmax(0,1fr)]',
  )
}
expect(within(dialog).getByText('图片数量').closest('section')).toHaveClass(
  'grid-cols-[4.5rem_minmax(0,1fr)]',
)
```

- [x] **Step 2: Run the Designer test and verify RED**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/designer/DesignerGenerationToolbar.test.tsx
```

Expected: FAIL on the old accessible name and vertical short sections.

- [x] **Step 3: Implement the Designer compact rows**

Rename the trigger and title to `创作参数`. Keep the capability section unchanged and use the same row grid for size, quality, output format, and image quantity:

```tsx
<section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-2">
  <h3 className="pt-1 font-medium">尺寸</h3>
  <ToggleGroup className="flex min-w-0 flex-wrap justify-start" ...>
```

For quantity:

```tsx
<section className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-2">
  <h3 className="font-medium">图片数量</h3>
  <div className="flex min-w-0 justify-end">
    <QuantityStepper label="图片数量" ... />
  </div>
</section>
```

- [x] **Step 4: Run the Designer test and verify GREEN**

Run the Step 2 command again. Expected: all Designer toolbar tests pass.

- [x] **Step 5: Commit the Designer change**

```bash
git add studio/src/components/designer/DesignerGenerationToolbar.tsx studio/src/components/designer/DesignerGenerationToolbar.test.tsx
git commit -m "feat(studio): compact designer parameters"
```

### Task 3: Align Page-Level Contracts

**Files:**
- Modify: `studio/src/components/tasks/TaskFormDialog.test.tsx`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`
- Modify: `studio/src/pages/DesignerPage.billing.test.tsx`
- Modify: `studio/src/pages/DesignerPage.provider-contract.test.ts`
- Modify: `studio/src/pages/DesignerPage.reference-drop.test.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`

- [x] **Step 1: Update page tests to the approved public label**

Replace page-level role queries and expected accessible names from `创作设置：` to `创作参数：`. Do not change project labels, execution-profile labels, image group labels, or quantity labels.

- [x] **Step 2: Run the affected page tests**

Run:

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test -- src/components/tasks/TaskFormDialog.test.tsx src/pages/DashboardPage.ai-entry.test.tsx src/pages/DesignerPage.billing.test.tsx src/pages/DesignerPage.provider-contract.test.ts src/pages/DesignerPage.reference-drop.test.tsx src/pages/PlansPage.test.tsx src/pages/TaskDetailPage.test.tsx src/pages/TasksPage.test.tsx
```

Expected: all affected page tests pass with the new public label.

- [x] **Step 3: Confirm no stale production or test label remains**

Run:

```bash
rg -n "创作设置" studio/src
```

Expected: no matches.

- [x] **Step 4: Commit page-contract updates**

```bash
git add studio/src/components/tasks/TaskFormDialog.test.tsx studio/src/pages/DashboardPage.ai-entry.test.tsx studio/src/pages/DesignerPage.billing.test.tsx studio/src/pages/DesignerPage.provider-contract.test.ts studio/src/pages/DesignerPage.reference-drop.test.tsx studio/src/pages/PlansPage.test.tsx studio/src/pages/TaskDetailPage.test.tsx studio/src/pages/TasksPage.test.tsx
git commit -m "test(studio): align composer parameter labels"
```

### Task 4: Full Verification And Visual QA

**Files:**
- Review: all files in `git diff 0d1456ef..HEAD`

- [x] **Step 1: Run full Studio verification**

```bash
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run test
cd studio && PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH bun run build
```

Expected: all tests pass and the Vite production build exits 0.

- [x] **Step 2: Run repository hygiene checks**

```bash
git diff --check 0d1456ef
git status --short --branch
```

Expected: no whitespace errors; only the two unrelated July plan drafts remain outside the intended change.

- [x] **Step 3: Verify desktop rendering**

At `http://localhost:58442/` with a 1440x768 viewport:

- Open `创作参数`.
- Confirm execution profiles and image capabilities remain vertical.
- Confirm image ratio and task quantity render as compact horizontal rows.
- Confirm the common parameter set fits without internal scrolling.

- [x] **Step 4: Verify Designer and mobile rendering**

At `/designer` with 1440x768 and 390x844 viewports:

- Open `创作参数`.
- Confirm size, quality, output format, and image quantity use compact rows.
- Confirm controls wrap without overlap or horizontal overflow.
- Change one short option and quantity, then confirm the trigger summary updates.
- Check page identity, DOM content, framework overlay absence, console health, and screenshots.

- [x] **Step 5: Review and hand off**

Review the complete range against `docs/superpowers/specs/2026-08-04-composer-parameter-density-design.md`, report any remaining browser or provider E2E gap, and leave unrelated drafts untouched.
