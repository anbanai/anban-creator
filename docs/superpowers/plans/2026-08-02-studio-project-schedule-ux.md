# Studio Project Selection and Schedule UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put a complete project identity first in Studio creation flows and replace the fragmented plan scheduler with one validated, readable control.

**Architecture:** Extend the existing `ProjectContextControl` around a reusable project identity row, then make the legacy `ProjectSelector` a data-loading adapter over that same control. Move plan project selection into a first-class form field and keep cron serialization internal to a controlled `SchedulePicker` that reports weekly-day validity to React Hook Form.

**Tech Stack:** React 19, TypeScript, React Hook Form, TanStack Query, Base UI/shadcn components, Tailwind CSS v4, Vitest, Testing Library, Bun, Vite.

---

### Task 1: Complete Project Identity Component

**Files:**
- Create: `studio/src/components/agent-prompt/ProjectIdentity.tsx`
- Create: `studio/src/components/agent-prompt/ProjectIdentity.test.tsx`
- Modify: `studio/src/components/agent-prompt/ProjectContextControl.tsx`
- Modify: `studio/src/components/agent-prompt/ProjectContextControl.test.tsx`

- [ ] **Step 1: Write failing identity tests**

Add tests that render a project with `avatar_url`, `description`, and `platform`, assert the avatar/name/description/localized type, then rerender with missing avatar and description and assert the project-name initial and fallback description. Add a failed-image test that fires `error` on the image and verifies the initial fallback.

```tsx
const project = {
  id: 'tea', name: '一壶不事二茶', platform: 'seednote',
  avatar_url: '/tea.png', description: '分享中国茶文化与日常茶生活',
}
render(<ProjectIdentity project={project} />)
expect(screen.getByRole('img', { name: '一壶不事二茶' })).toHaveAttribute('src', '/tea.png')
expect(screen.getByText('分享中国茶文化与日常茶生活')).toBeInTheDocument()
expect(screen.getByText('种草笔记')).toBeInTheDocument()
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run: `cd studio && bun run test -- src/components/agent-prompt/ProjectIdentity.test.tsx`

Expected: FAIL because `ProjectIdentity` does not exist.

- [ ] **Step 3: Implement the reusable identity**

Define the shared data shape and variants in `ProjectIdentity.tsx`:

```tsx
export interface ProjectIdentityProject {
  id: string
  name: string
  platform?: string
  avatar_url?: string
  description?: string
}

export function ProjectIdentity({
  project,
  compact = false,
  showType = true,
}: {
  project: ProjectIdentityProject
  compact?: boolean
  showType?: boolean
}) {
  // Render Avatar with image/error fallback, a min-w-0 text column,
  // localized platform label, and stable full/compact dimensions.
}
```

Use the existing Avatar and Badge primitives, `platformLabels`, `cn`, one-line truncation, and `project.name.trim().charAt(0) || '项'`. The description fallback is `${platformLabel}项目`.

- [ ] **Step 4: Extend `ProjectContextControl` tests before implementation**

Expand `ProjectContextProject` fixtures with avatar and description. Assert that the trigger and open options render complete identities, search by description finds a project, `compact` changes the trigger without removing details from the popup, and read-only mode renders a complete identity.

Add `compact?: boolean` and `ariaLabel?: string` expectations while retaining the default accessible name `项目上下文`.

- [ ] **Step 5: Run control tests and verify failure**

Run: `cd studio && bun run test -- src/components/agent-prompt/ProjectContextControl.test.tsx`

Expected: FAIL on missing complete identity, description search, and compact support.

- [ ] **Step 6: Integrate the identity into `ProjectContextControl`**

Extend `ProjectContextProject` from `ProjectIdentityProject`. Render `ProjectIdentity` in the selected trigger, each project option, and read-only mode. Make combobox search text include name and description:

```tsx
itemToStringValue={(item) => item.kind === 'project'
  ? `${item.name} ${item.description ?? ''}`.trim()
  : item.name}
```

Keep no-project items textual, preserve grouping, loading, disabled, create-project, and empty states, and expose `compact` plus `ariaLabel` only as presentation/accessibility props.

- [ ] **Step 7: Run both component suites**

Run: `cd studio && bun run test -- src/components/agent-prompt/ProjectIdentity.test.tsx src/components/agent-prompt/ProjectContextControl.test.tsx`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add studio/src/components/agent-prompt/ProjectIdentity.tsx studio/src/components/agent-prompt/ProjectIdentity.test.tsx studio/src/components/agent-prompt/ProjectContextControl.tsx studio/src/components/agent-prompt/ProjectContextControl.test.tsx
git commit -m "feat(studio): add complete project identity selector"
```

### Task 2: Replace Every Interactive Studio Project Selector

**Files:**
- Modify: `studio/src/components/ProjectSelector.tsx`
- Create: `studio/src/components/ProjectSelector.test.tsx`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`
- Modify: `studio/src/components/tasks/TaskFormDialog.test.tsx`
- Modify: `studio/src/pages/DesignerPage.reference-drop.test.tsx`
- Modify: `studio/src/pages/TasksPage.test.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`
- Modify: `studio/src/pages/TimelinePage.tsx`

- [ ] **Step 1: Write a failing adapter test**

Mock `api.projects.list` with complete Project objects. Assert that `ProjectSelector` renders a compact complete trigger, opens full options, forwards both project ID and platform, clears to `('', '')`, and respects `excludePlatforms`.

```tsx
fireEvent.click(await screen.findByRole('combobox', { name: '筛选项目' }))
fireEvent.click(screen.getByRole('option', { name: /一壶不事二茶/ }))
expect(onChange).toHaveBeenCalledWith('tea', 'seednote')
```

- [ ] **Step 2: Run the adapter test and verify failure**

Run: `cd studio && bun run test -- src/components/ProjectSelector.test.tsx`

Expected: FAIL because the legacy generic Combobox does not render project identities or the new accessible label.

- [ ] **Step 3: Rebuild `ProjectSelector` as an adapter**

Keep its TanStack Query and filtering responsibilities, but render:

```tsx
<ProjectContextControl
  mode="select"
  projects={visibleProjects}
  value={value || null}
  allowNoProject
  noProjectLabel="全部项目"
  compact
  ariaLabel="筛选项目"
  disabled={disabled}
  loading={isLoading}
  onValueChange={(id, project) => onChange(id ?? '', project?.platform ?? '')}
/>
```

Add a configurable no-project item label to `ProjectContextControl`; do not duplicate project rendering in this adapter.

- [ ] **Step 4: Update surface contract tests**

For Dashboard, task creation, plan creation, and Designer, assert that selected projects expose name, description, and type. For Tasks, Plans, and Timeline filters, assert the `筛选项目` combobox and complete open option. Keep existing behavioral assertions for query parameters and form defaults.

- [ ] **Step 5: Run all affected suites**

Run: `cd studio && bun run test -- src/components/ProjectSelector.test.tsx src/pages/DashboardPage.ai-entry.test.tsx src/components/tasks/TaskFormDialog.test.tsx src/pages/DesignerPage.reference-drop.test.tsx src/pages/TasksPage.test.tsx src/pages/PlansPage.test.tsx`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add studio/src/components/ProjectSelector.tsx studio/src/components/ProjectSelector.test.tsx studio/src/pages/DashboardPage.ai-entry.test.tsx studio/src/components/tasks/TaskFormDialog.test.tsx studio/src/pages/DesignerPage.reference-drop.test.tsx studio/src/pages/TasksPage.test.tsx studio/src/pages/PlansPage.test.tsx studio/src/pages/TimelinePage.tsx
git commit -m "refactor(studio): unify project selectors"
```

### Task 3: Put Project First in the Plan Form

**Files:**
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`

- [ ] **Step 1: Write failing form-order and duplication tests**

Open the create dialog and compare DOM positions for labeled fields:

```tsx
const project = within(dialog).getByText('项目', { selector: 'label' })
const type = within(dialog).getByText('内容类型', { selector: 'label' })
expect(project.compareDocumentPosition(type) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
expect(within(dialog).getAllByRole('combobox', { name: '项目上下文' })).toHaveLength(1)
```

Assert that selecting a project still sets content type and image ratio, and that edit mode renders one complete read-only project identity before content type.

- [ ] **Step 2: Run the plan suite and verify failure**

Run: `cd studio && bun run test -- src/pages/PlansPage.test.tsx`

Expected: FAIL because project selection is currently inside the later prompt context bar.

- [ ] **Step 3: Move the project control into the form**

Insert a `project_id` `FormField` before `type`. Use the existing selection callback unchanged so type, ratio, agent input, and Montage defaults retain current behavior. Render the read-only complete identity for edits. Remove `contextBar` from `AgentPromptInput` and remove the duplicate helper text `内容类型随所选项目自动确定`; use concise content-type description only when it adds information.

- [ ] **Step 4: Run plan tests**

Run: `cd studio && bun run test -- src/pages/PlansPage.test.tsx`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx
git commit -m "fix(studio): put project first in plan creation"
```

### Task 4: Consolidate and Validate Plan Scheduling

**Files:**
- Modify: `studio/src/components/SchedulePicker.tsx`
- Modify: `studio/src/components/SchedulePicker.test.tsx`
- Modify: `studio/src/pages/PlansPage.tsx`
- Modify: `studio/src/pages/PlansPage.test.tsx`

- [ ] **Step 1: Write failing schedule behavior tests**

Cover daily and weekly parsing, frequency switching, stable weekday order, time updates, summary text, and empty-week validation. Use a validity callback rather than exposing cron parsing to the page:

```tsx
const onValidityChange = vi.fn()
render(<SchedulePicker value="0 20 * * 1,3,5" onChange={onChange} onValidityChange={onValidityChange} />)
expect(screen.getByText('每周一、三、五 20:00 自动执行')).toBeInTheDocument()
// Deselect all active weekdays.
expect(screen.getByRole('alert')).toHaveTextContent('请至少选择一天')
expect(onValidityChange).toHaveBeenLastCalledWith(false)
```

- [ ] **Step 2: Run schedule tests and verify failure**

Run: `cd studio && bun run test -- src/components/SchedulePicker.test.tsx`

Expected: FAIL on integrated structure, validation callback, and alert.

- [ ] **Step 3: Implement the consolidated controlled scheduler**

Add `onValidityChange?: (valid: boolean) => void`. Render one bordered container with a full-width segmented frequency control, a stable seven-column weekday grid, labeled `TimePicker`, summary, and inline error. Do not call `onChange` with an empty weekly weekday field. Notify validity whenever frequency or days change.

Use responsive labels in each weekday button:

```tsx
<span className="hidden sm:inline">周一</span>
<span className="sm:hidden">一</span>
```

- [ ] **Step 4: Add page-level submission validation first**

In `PlansPage.test.tsx`, select weekly, clear every day, submit, and assert `api.plans.create` is not called. Then select Monday and assert submission uses the expected cron.

- [ ] **Step 5: Wire validity and pricing into the schedule field**

Track `scheduleValid` in `PlansPage`. Pass `onValidityChange={setScheduleValid}`, set/clear the React Hook Form `cron_expr` error, and prevent submission while invalid. Move `TaskTimePricingNotice` into the schedule container through a `footer` or `pricingNotice` ReactNode prop so schedule summary and price feedback form one visual unit without importing billing logic into `SchedulePicker`.

- [ ] **Step 6: Run schedule and plan suites**

Run: `cd studio && bun run test -- src/components/SchedulePicker.test.tsx src/pages/PlansPage.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add studio/src/components/SchedulePicker.tsx studio/src/components/SchedulePicker.test.tsx studio/src/pages/PlansPage.tsx studio/src/pages/PlansPage.test.tsx
git commit -m "fix(studio): simplify and validate plan scheduling"
```

### Task 5: Full Verification and Browser QA

**Files:**
- Modify only files needed to fix verification findings from Tasks 1-4.

- [ ] **Step 1: Run the full Studio tests**

Run: `cd studio && bun run test`

Expected: all Vitest suites PASS with no unhandled errors.

- [ ] **Step 2: Run the production build**

Run: `cd studio && bun run build`

Expected: `tsc -b && vite build` exits 0.

- [ ] **Step 3: Start the Studio development server**

Run: `cd studio && bun run dev --host 127.0.0.1`

Expected: Vite prints an available local URL. Keep the process running for QA.

- [ ] **Step 4: Verify desktop interactions in the in-app browser**

At approximately 1440x900, verify Dashboard, task create, plan create/edit, Designer, Tasks filter, Plans filter, and Timeline filter. Confirm complete identities, search by description, keyboard selection, correct project/type updates, schedule validation, pricing feedback, and no duplicate project control.

- [ ] **Step 5: Verify mobile interactions in the in-app browser**

At approximately 390x844, verify the plan dialog and every project selector. Confirm name/description truncation, no horizontal overflow, single-row weekday tracks with short labels, touch-sized controls, visible validation, and no overlap.

- [ ] **Step 6: Run final focused regression after browser fixes**

Run: `cd studio && bun run test -- src/components/agent-prompt/ProjectIdentity.test.tsx src/components/agent-prompt/ProjectContextControl.test.tsx src/components/ProjectSelector.test.tsx src/components/SchedulePicker.test.tsx src/pages/PlansPage.test.tsx`

Expected: PASS.

- [ ] **Step 7: Commit verification fixes, if any**

```bash
git add studio/src
git commit -m "test(studio): verify project and schedule UX"
```

Skip this commit when browser QA required no code changes.
