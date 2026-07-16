# Task Detail Balanced Context Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore a compact middle layer of task context and the established Studio visual language without returning metadata, configuration, materials, and logs to the main page as expanded panels.

**Architecture:** Add a focused `TaskContextSummary` that derives four compact entries from data already loaded by `TaskDetailPage`. Move details-tab ownership to `TaskDetailPage` so summary items open the sheet directly on a target tab, while `TaskDetailsSheet` remains responsible for the complete secondary views and their visual grouping.

**Tech Stack:** React 19, TypeScript 6, Vite 8, Tailwind CSS v4, shadcn/Base UI, Lucide React, TanStack Query, Vitest, Testing Library, in-app Browser.

**Design spec:** `docs/superpowers/specs/2026-07-17-task-detail-balanced-context-design.md`

---

## File Map

- Create `studio/src/components/tasks/TaskContextSummary.tsx`
  - Derives and renders the four summary entries plus the general details command.
  - Emits a requested details tab and performs no network request.
- Create `studio/src/components/tasks/TaskContextSummary.test.tsx`
  - Covers snapshot precedence, fallbacks, file/log status, responsive structure, and tab callbacks.
- Modify `studio/src/components/tasks/TaskDetailsSheet.tsx`
  - Exports the shared tab type.
  - Accepts controlled tab state.
  - Fixes the matching-variant desktop width override.
  - Restores header context, warm active state, grouped overview/configuration/material/log surfaces, and compact log empty state.
- Modify `studio/src/components/tasks/TaskDetailsSheet.test.tsx`
  - Uses a stateful test harness for the new controlled tab contract.
  - Covers header context, width override, grouped regions, tab callbacks, and existing log/material behaviors.
- Modify `studio/src/pages/TaskDetailPage.tsx`
  - Owns the selected details tab and task-reset behavior.
  - Renders `TaskContextSummary` after results/deliverables/analytics.
  - Replaces the standalone ghost details button with the summary component.
- Modify `studio/src/pages/TaskDetailPage.test.tsx`
  - Covers direct summary-to-tab navigation, ordering, credit-dialog return, task switching, and existing result workflows.

Do not change server APIs, task types, result contracts, query keys, specialized result components, or plugin assets.

---

### Task 1: Add The Task Context Summary

**Files:**
- Create: `studio/src/components/tasks/TaskContextSummary.tsx`
- Create: `studio/src/components/tasks/TaskContextSummary.test.tsx`
- Modify: `studio/src/components/tasks/TaskDetailsSheet.tsx:27`

- [ ] **Step 1: Export the shared details-tab type**

Change the existing private type declaration to:

```ts
export type TaskDetailsTab = 'overview' | 'configuration' | 'materials' | 'logs'
```

This is a type-only contract change and does not alter sheet behavior.

- [ ] **Step 2: Write failing summary rendering and interaction tests**

Create `studio/src/components/tasks/TaskContextSummary.test.tsx` with fixtures that use an immutable snapshot, one input attachment, one `reference-usage-summary.json` file, live logs, and net credits.

```tsx
import { fireEvent, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import type { Project, Task, TaskFile } from '@/types'
import { TaskContextSummary } from './TaskContextSummary'

const project: Project = {
  id: 'project-1',
  user_id: 'user-1',
  platform: 'article',
  name: '当前项目名称',
  avatar_url: '',
  profile_url: '',
  keywords: '',
  visual_style: '当前项目视觉',
  writer: '',
  theme: '',
  author: '',
  template_id: '',
  reference_image_url: '',
  image_ratio: '1:1',
  max_concurrent_tasks: 1,
  config: {},
  status: 'active',
  created_at: '2026-07-17T01:00:00.000Z',
  updated_at: '2026-07-17T01:00:00.000Z',
}

const task: Task = {
  id: 'task-1',
  type: 'article',
  title: '新茶上市内容创作',
  prompt: '写一篇新茶上市文章',
  status: 'running',
  project_id: project.id,
  project_snapshot: {
    project_name: '茶小茶',
    platform: 'article',
    visual_style: '清新茶感摄影',
    image_ratio: '3:4',
  },
  input_attachments: [{ type: 'image', file_name: 'tea-reference.jpg' }],
  plan_id: null,
  result: null,
  published: false,
  published_at: null,
  created_at: '2026-07-17T01:00:00.000Z',
  started_at: '2026-07-17T01:03:00.000Z',
  completed_at: '',
}

const files: TaskFile[] = [{
  id: 'summary-file',
  task_id: task.id,
  state: 'published',
  role: 'metadata',
  file_name: 'reference-usage-summary.json',
  mime_type: 'application/json',
  file_size: 128,
  url: '/summary.json',
  created_at: '2026-07-17T01:05:00.000Z',
}]

describe('TaskContextSummary', () => {
  it('shows snapshot context, material status, live logs, and responsive structure', () => {
    render(
      <TaskContextSummary
        task={task}
        project={project}
        files={files}
        logs={['已读取参考素材', '正在优化标题']}
        progressDescription="正在优化标题与段落结构"
        netConsumedCredits={2835}
        sseError={null}
        onOpenTab={vi.fn()}
      />,
    )

    const summary = screen.getByRole('region', { name: '任务上下文' })
    expect(within(summary).getByText('茶小茶')).toBeInTheDocument()
    expect(within(summary).getByText('2,835 积分')).toBeInTheDocument()
    expect(within(summary).getByText('公众号文章')).toBeInTheDocument()
    expect(within(summary).getByText('清新茶感摄影 · 3:4')).toBeInTheDocument()
    expect(within(summary).getByText('1 项输入')).toBeInTheDocument()
    expect(within(summary).getByText('已生成使用结论')).toBeInTheDocument()
    expect(within(summary).getByText('2 条 · 实时')).toBeInTheDocument()
    expect(within(summary).getByText('正在优化标题与段落结构')).toBeInTheDocument()
    expect(within(summary).getByTestId('task-context-grid')).toHaveClass(
      'grid-cols-2',
      'lg:grid-cols-4',
    )
  })

  it('opens the matching tab from every summary action and Overview from More details', () => {
    const onOpenTab = vi.fn()
    render(
      <TaskContextSummary
        task={task}
        project={project}
        files={files}
        logs={[]}
        progressDescription={null}
        netConsumedCredits={0}
        sseError={null}
        onOpenTab={onOpenTab}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '打开任务概览' }))
    fireEvent.click(screen.getByRole('button', { name: '打开创作配置' }))
    fireEvent.click(screen.getByRole('button', { name: '打开参考素材' }))
    fireEvent.click(screen.getByRole('button', { name: '打开执行日志' }))
    fireEvent.click(screen.getByRole('button', { name: '更多详情' }))

    expect(onOpenTab.mock.calls.map(([tab]) => tab)).toEqual([
      'overview',
      'configuration',
      'materials',
      'logs',
      'overview',
    ])
  })

  it('uses compact honest fallbacks without parsing the summary file', () => {
    render(
      <TaskContextSummary
        task={{ ...task, status: 'completed', project_snapshot: undefined, input_attachments: [] }}
        project={{ ...project, visual_style: '', image_ratio: '' }}
        files={[]}
        logs={[]}
        progressDescription={null}
        netConsumedCredits={0}
        sseError={null}
        onOpenTab={vi.fn()}
      />,
    )

    expect(screen.getByText('0 项输入')).toBeInTheDocument()
    expect(screen.getByText('仅任务输入')).toBeInTheDocument()
    expect(screen.getByText('0 条 · 已结束')).toBeInTheDocument()
    expect(screen.getAllByText('未设置').length).toBeGreaterThan(0)
  })

  it('marks an interrupted running log connection', () => {
    render(
      <TaskContextSummary
        task={task}
        project={project}
        files={[]}
        logs={['最后一条日志']}
        progressDescription={null}
        netConsumedCredits={0}
        sseError="实时连接已中断"
        onOpenTab={vi.fn()}
      />,
    )

    expect(screen.getByText('1 条 · 连接中断')).toBeInTheDocument()
    expect(screen.getByText('最后一条日志')).toBeInTheDocument()
  })
})
```

- [ ] **Step 3: Run the summary tests and verify RED**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- src/components/tasks/TaskContextSummary.test.tsx
```

Expected: FAIL because `TaskContextSummary.tsx` does not exist.

- [ ] **Step 4: Implement the summary component**

Create `studio/src/components/tasks/TaskContextSummary.tsx` with this contract and derivation logic:

```tsx
import {
  ChevronRight,
  FolderKanban,
  Images,
  Info,
  ScrollText,
  SlidersHorizontal,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { contentTypeLabel } from '@/lib/labels'
import { cn } from '@/lib/utils'
import type { Project, Task, TaskFile } from '@/types'
import type { TaskDetailsTab } from './TaskDetailsSheet'

export interface TaskContextSummaryProps {
  task: Task
  project?: Project
  files: TaskFile[]
  logs: string[]
  progressDescription: string | null
  netConsumedCredits: number
  sseError: string | null
  onOpenTab: (tab: TaskDetailsTab) => void
}

interface SummaryItemProps {
  label: string
  value: string
  detail: string
  ariaLabel: string
  icon: LucideIcon
  onClick: () => void
  index: number
}

function normalizeLogLine(value: string): string {
  return value.trim().replace(/^[-#*\s]+/, '').slice(0, 80)
}

function SummaryItem({
  label,
  value,
  detail,
  ariaLabel,
  icon: Icon,
  onClick,
  index,
}: SummaryItemProps) {
  return (
    <Button
      type="button"
      variant="ghost"
      aria-label={ariaLabel}
      onClick={onClick}
      className={cn(
        'h-auto min-h-24 min-w-0 justify-start rounded-none px-4 py-3 text-left',
        index < 2 && 'border-b border-border lg:border-b-0',
        index % 2 === 0 && 'border-r border-border',
        index === 1 && 'lg:border-r lg:border-border',
      )}
    >
      <span className="flex min-w-0 items-start gap-3">
        <span className="rounded-md bg-primary/10 p-2 text-primary">
          <Icon aria-hidden="true" />
        </span>
        <span className="flex min-w-0 flex-col gap-1">
          <span className="text-xs font-medium text-muted-foreground">{label}</span>
          <span className="truncate text-sm font-semibold text-foreground">{value}</span>
          <span className="truncate text-xs font-normal text-muted-foreground">{detail}</span>
        </span>
      </span>
    </Button>
  )
}

export function TaskContextSummary({
  task,
  project,
  files,
  logs,
  progressDescription,
  netConsumedCredits,
  sseError,
  onOpenTab,
}: TaskContextSummaryProps) {
  const hasSnapshot = Boolean(task.project_snapshot?.platform)
  const projectName = hasSnapshot
    ? task.project_snapshot?.project_name || '未设置'
    : project?.name || '未设置'
  const platform = hasSnapshot
    ? task.project_snapshot?.platform || task.type
    : project?.platform || task.type
  const visualStyle = hasSnapshot
    ? task.project_snapshot?.visual_style || ''
    : task.overrides?.visual_style || project?.visual_style || ''
  const imageRatio = task.image_ratio
    || (hasSnapshot ? task.project_snapshot?.image_ratio : project?.image_ratio)
    || ''
  const configurationDetail = [visualStyle, imageRatio].filter(Boolean).join(' · ') || '未设置'
  const inputCount = task.input_attachments?.length ?? 0
  const hasReferenceSummary = files.some(
    (file) => file.file_name === 'reference-usage-summary.json',
  )
  const logState = sseError
    ? '连接中断'
    : task.status === 'running'
      ? '实时'
      : task.status === 'pending'
        ? '等待执行'
        : '已结束'
  const latestLog = [...logs].reverse().find((line) => line.trim())
  const logDetail = task.status === 'running' && progressDescription
    ? progressDescription
    : latestLog
      ? normalizeLogLine(latestLog)
      : '暂无日志'
  const items = [
    {
      tab: 'overview' as const,
      label: '任务概览',
      value: projectName,
      detail: `${task.plan_id ? '计划任务' : '手动创建'} · ${netConsumedCredits.toLocaleString()} 积分`,
      ariaLabel: '打开任务概览',
      icon: FolderKanban,
    },
    {
      tab: 'configuration' as const,
      label: '创作配置',
      value: contentTypeLabel[platform] || platform,
      detail: configurationDetail,
      ariaLabel: '打开创作配置',
      icon: SlidersHorizontal,
    },
    {
      tab: 'materials' as const,
      label: '参考素材',
      value: `${inputCount} 项输入`,
      detail: hasReferenceSummary ? '已生成使用结论' : '仅任务输入',
      ariaLabel: '打开参考素材',
      icon: Images,
    },
    {
      tab: 'logs' as const,
      label: '执行日志',
      value: `${logs.length} 条 · ${logState}`,
      detail: logDetail,
      ariaLabel: '打开执行日志',
      icon: ScrollText,
    },
  ]

  return (
    <Card role="region" aria-label="任务上下文" size="sm" className="rounded-lg py-0">
      <CardContent
        data-testid="task-context-grid"
        className="grid grid-cols-2 p-0 lg:grid-cols-4"
      >
        {items.map((item, index) => (
          <SummaryItem
            key={item.tab}
            label={item.label}
            value={item.value}
            detail={item.detail}
            ariaLabel={item.ariaLabel}
            icon={item.icon}
            onClick={() => onOpenTab(item.tab)}
            index={index}
          />
        ))}
      </CardContent>
      <CardFooter className="px-2 py-1">
        <Button
          type="button"
          variant="ghost"
          className="w-full justify-start"
          onClick={() => onOpenTab('overview')}
        >
          <Info data-icon="inline-start" />
          更多详情
          <ChevronRight data-icon="inline-end" className="ml-auto" />
        </Button>
      </CardFooter>
    </Card>
  )
}

export default TaskContextSummary
```

- [ ] **Step 5: Run focused tests and verify GREEN**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- src/components/tasks/TaskContextSummary.test.tsx
```

Expected: all summary tests PASS with no console errors.

- [ ] **Step 6: Commit the summary component**

```bash
git add \
  studio/src/components/tasks/TaskContextSummary.tsx \
  studio/src/components/tasks/TaskContextSummary.test.tsx \
  studio/src/components/tasks/TaskDetailsSheet.tsx
git commit -m "feat(studio): add task context summary"
```

---

### Task 2: Make Details Navigation Controlled And Fix Sheet Geometry

**Files:**
- Modify: `studio/src/components/tasks/TaskDetailsSheet.tsx:1-280`
- Modify: `studio/src/components/tasks/TaskDetailsSheet.test.tsx:1-351`

- [ ] **Step 1: Write failing controlled-tab, header, and desktop-width tests**

Add `selectedTab` and `onTabChange` to the test props, and add a test harness that updates selected state while still spying on callbacks:

```tsx
import { createRef, useEffect, useState } from 'react'
import type { TaskDetailsTab } from './TaskDetailsSheet'

function ControlledTaskDetailsSheet(props: TaskDetailsSheetProps) {
  const [selectedTab, setSelectedTab] = useState<TaskDetailsTab>(props.selectedTab)

  useEffect(() => {
    setSelectedTab(props.selectedTab)
  }, [props.selectedTab, props.task.id])

  return (
    <TaskDetailsSheet
      {...props}
      selectedTab={selectedTab}
      onTabChange={(tab) => {
        props.onTabChange(tab)
        setSelectedTab(tab)
      }}
    />
  )
}
```

Update `createSheetProps` with:

```tsx
selectedTab: 'overview',
onTabChange: vi.fn(),
```

Render `ControlledTaskDetailsSheet` instead of `TaskDetailsSheet` in interaction tests. Add these assertions:

```tsx
it('renders the controlled tab and reports tab changes', () => {
  const props = createSheetProps({ selectedTab: 'logs' })
  render(<ControlledTaskDetailsSheet {...props} />)

  expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')
  fireEvent.click(screen.getByRole('tab', { name: '配置' }))
  expect(props.onTabChange).toHaveBeenCalledWith('configuration')
})

it('shows task context in the header and overrides desktop width at the same variant layer', () => {
  render(<ControlledTaskDetailsSheet {...createSheetProps()} />)

  const sheet = screen.getByRole('dialog', { name: '任务详情' })
  expect(sheet).toHaveClass(
    'data-[side=right]:w-full',
    'data-[side=right]:sm:max-w-xl',
  )
  expect(sheet).not.toHaveClass(
    'data-[side=right]:w-3/4',
    'data-[side=right]:sm:max-w-sm',
  )
  expect(screen.getByText('夏日选题')).toBeInTheDocument()
  expect(screen.getByText('创建时项目名称')).toBeInTheDocument()
  expect(screen.getByText('已完成')).toBeInTheDocument()
})
```

Delete the old internal-reset test from `TaskDetailsSheet.test.tsx`; page integration will own and test task-ID reset in Task 4.

- [ ] **Step 2: Run the sheet tests and verify RED**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- src/components/tasks/TaskDetailsSheet.test.tsx
```

Expected: FAIL because controlled props, the contextual header, and the matching desktop-width variant are not implemented.

- [ ] **Step 3: Implement the controlled sheet contract**

Remove `useState` from the React import but keep `useEffect` for log following. Extend the props:

```ts
export interface TaskDetailsSheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  selectedTab: TaskDetailsTab
  onTabChange: (tab: TaskDetailsTab) => void
  task: Task
  project?: Project
  files: TaskFile[]
  netConsumedCredits: number
  showCreditDetails: boolean
  onOpenCreditDetails: () => void
  logs: string[]
  sseError: string | null
  autoScrollLogs: boolean
  onToggleAutoScroll: () => void
  onCopyLogs: () => void
  onReconnectLogs: () => void
  logContainerRef: RefObject<HTMLDivElement | null>
}
```

Remove the internal `tab` state and task-ID effect. Derive header context once:

```tsx
const hasSnapshot = Boolean(props.task.project_snapshot?.platform)
const projectName = hasSnapshot
  ? props.task.project_snapshot?.project_name || '未设置项目'
  : props.project?.name || '未设置项目'
const taskTitle = props.task.title || props.task.topic || props.task.prompt
```

Replace the sheet shell, header, and tabs wiring with:

```tsx
<Sheet open={props.open} onOpenChange={props.onOpenChange}>
  <SheetContent
    side="right"
    className="gap-0 overflow-hidden p-0 data-[side=right]:w-full data-[side=right]:sm:max-w-xl"
  >
    <SheetHeader className="shrink-0 border-b border-border px-4 py-3 pr-12">
      <SheetTitle>任务详情</SheetTitle>
      <SheetDescription className="truncate pr-2">{taskTitle}</SheetDescription>
      <div className="flex min-w-0 items-center gap-2 pt-1">
        <Badge variant="outline">{taskStatusLabel[props.task.status] || props.task.status}</Badge>
        <span className="truncate text-xs text-muted-foreground">{projectName}</span>
      </div>
    </SheetHeader>
    <Tabs
      value={props.selectedTab}
      onValueChange={(value) => props.onTabChange(value as TaskDetailsTab)}
      className="min-h-0 flex-1 gap-0 overflow-hidden"
    >
      <TabsList
        variant="line"
        className="w-full shrink-0 justify-start overflow-x-auto border-b border-border px-4 py-2"
      >
        <TabsTrigger className="data-active:text-primary data-active:after:bg-primary" value="overview">
          概览
        </TabsTrigger>
        <TabsTrigger className="data-active:text-primary data-active:after:bg-primary" value="configuration">
          配置
        </TabsTrigger>
        <TabsTrigger className="data-active:text-primary data-active:after:bg-primary" value="materials">
          素材
        </TabsTrigger>
        <TabsTrigger className="data-active:text-primary data-active:after:bg-primary" value="logs">
          日志
        </TabsTrigger>
      </TabsList>
      <TabsContent value="overview" className="min-h-0 overflow-y-auto p-4">
        <TaskOverviewDetails
          task={props.task}
          project={props.project}
          netConsumedCredits={props.netConsumedCredits}
          showCreditDetails={props.showCreditDetails}
          onOpenCreditDetails={props.onOpenCreditDetails}
        />
      </TabsContent>
      <TabsContent value="configuration" className="min-h-0 overflow-y-auto p-4">
        <TaskConfigurationDetails task={props.task} project={props.project} />
      </TabsContent>
      <TabsContent value="materials" className="min-h-0 overflow-y-auto p-4">
        <ErrorBoundary
          key={`task-details-materials-${props.task.id}`}
          fallback={<CompactMaterialError />}
        >
          <ReferenceUsageSummary variant="compact" task={props.task} files={props.files} />
        </ErrorBoundary>
      </TabsContent>
      <TabsContent value="logs" className="min-h-0 overflow-y-auto p-4">
        <TaskLogDetails
          logs={props.logs}
          logMarkdown={logMarkdown}
          sseError={props.sseError}
          autoScrollLogs={props.autoScrollLogs}
          onToggleAutoScroll={props.onToggleAutoScroll}
          onCopyLogs={props.onCopyLogs}
          onReconnectLogs={props.onReconnectLogs}
          logContainerRef={props.logContainerRef}
        />
      </TabsContent>
    </Tabs>
  </SheetContent>
</Sheet>
```

Import `Badge` and `taskStatusLabel`. Do not add task actions to the header.

- [ ] **Step 4: Run the sheet tests and verify GREEN**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- src/components/tasks/TaskDetailsSheet.test.tsx
```

Expected: all sheet and configuration tests PASS.

- [ ] **Step 5: Commit controlled navigation and geometry**

```bash
git add \
  studio/src/components/tasks/TaskDetailsSheet.tsx \
  studio/src/components/tasks/TaskDetailsSheet.test.tsx
git commit -m "refactor(studio): control task details navigation"
```

---

### Task 3: Restore Grouped Sheet Visual Hierarchy

**Files:**
- Modify: `studio/src/components/tasks/TaskDetailsSheet.tsx:47-276`
- Modify: `studio/src/components/tasks/TaskDetailsSheet.test.tsx:88-351`

- [ ] **Step 1: Write failing semantic grouping tests**

Add tests that assert the restored groups without coupling to raw color values:

```tsx
it('groups Overview into timing and project-credit surfaces', () => {
  render(<ControlledTaskDetailsSheet {...createSheetProps()} />)

  expect(screen.getByRole('region', { name: '任务时间与来源' })).toBeInTheDocument()
  expect(screen.getByRole('region', { name: '项目与积分' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '查看明细' })).toBeInTheDocument()
})

it('places configuration, materials, and logs in named grouped surfaces', () => {
  render(<ControlledTaskDetailsSheet {...createSheetProps()} />)

  fireEvent.click(screen.getByRole('tab', { name: '配置' }))
  expect(screen.getByRole('region', { name: '创作配置详情' })).toBeInTheDocument()

  fireEvent.click(screen.getByRole('tab', { name: '素材' }))
  expect(screen.getByRole('region', { name: '参考素材详情' })).toBeInTheDocument()

  fireEvent.click(screen.getByRole('tab', { name: '日志' }))
  expect(screen.getByRole('region', { name: '执行动态' })).toBeInTheDocument()
})

it('keeps the empty log surface compact', () => {
  render(<ControlledTaskDetailsSheet {...createSheetProps({ logs: [] })} />)
  fireEvent.click(screen.getByRole('tab', { name: '日志' }))

  expect(screen.getByText('等待输出中...')).toBeInTheDocument()
  expect(screen.getByRole('region', { name: '执行动态' })).toHaveClass('rounded-lg', 'border')
  expect(screen.getByRole('button', { name: '复制日志' })).toBeDisabled()
})
```

- [ ] **Step 2: Run the sheet tests and verify RED**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- src/components/tasks/TaskDetailsSheet.test.tsx
```

Expected: FAIL because the named grouped regions do not exist.

- [ ] **Step 3: Add a reusable sheet section wrapper**

Inside `TaskDetailsSheet.tsx`, add:

```tsx
interface TaskDetailsSectionProps {
  label: string
  title: string
  icon: LucideIcon
  children: ReactNode
}

function TaskDetailsSection({ label, title, icon: Icon, children }: TaskDetailsSectionProps) {
  return (
    <section aria-label={label} className="rounded-lg border border-border bg-card/60 p-4">
      <div className="mb-4 flex items-center gap-2">
        <span className="rounded-md bg-primary/10 p-2 text-primary">
          <Icon aria-hidden="true" />
        </span>
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
      </div>
      {children}
    </section>
  )
}
```

Import `ReactNode`, `CalendarClock`, `FolderKanban`, `Images`, `Settings2`, and `ScrollText` from the existing libraries, plus `LucideIcon` as a type from `lucide-react`. Keep icon colors semantic.

- [ ] **Step 4: Split Overview into two grouped regions**

Replace the flat `TaskOverviewDetails` list with two `TaskDetailsSection` blocks. Timing rows contain created, started, completed, and source. Project and credits contain project plus the existing credit action.

Use this row renderer in both groups:

```tsx
function DetailRows({ rows }: { rows: Array<[string, string]> }) {
  return (
    <dl className="flex flex-col">
      {rows.map(([label, value]) => (
        <div
          key={label}
          className="grid grid-cols-[6rem_minmax(0,1fr)] gap-3 border-b border-border py-3 first:pt-0 last:border-b-0 last:pb-0"
        >
          <dt className="text-xs text-muted-foreground">{label}</dt>
          <dd className="min-w-0 break-words text-sm text-foreground">{value}</dd>
        </div>
      ))}
    </dl>
  )
}
```

The second section must retain:

```tsx
<div className="flex items-center justify-between gap-3">
  <div className="flex min-w-0 flex-col gap-1">
    <p className="text-xs text-muted-foreground">积分消耗</p>
    <p className="text-sm font-medium tabular-nums">
      {netConsumedCredits.toLocaleString()}
    </p>
  </div>
  {showCreditDetails ? (
    <Button size="sm" variant="ghost" onClick={onOpenCreditDetails}>
      <ReceiptText data-icon="inline-start" />
      查看明细
    </Button>
  ) : null}
</div>
```

- [ ] **Step 5: Wrap Configuration, Materials, and Logs without nesting cards**

Inside each existing `TabsContent`, wrap the current content once:

```tsx
<TaskDetailsSection label="创作配置详情" title="创作配置" icon={Settings2}>
  <TaskConfigurationDetails task={props.task} project={props.project} />
</TaskDetailsSection>
```

```tsx
<TaskDetailsSection label="参考素材详情" title="参考素材" icon={Images}>
  <ErrorBoundary
    key={`task-details-materials-${props.task.id}`}
    fallback={<CompactMaterialError />}
  >
    <ReferenceUsageSummary variant="compact" task={props.task} files={props.files} />
  </ErrorBoundary>
</TaskDetailsSection>
```

```tsx
<TaskDetailsSection label="执行动态" title="执行动态" icon={ScrollText}>
  <TaskLogDetails
    logs={props.logs}
    logMarkdown={logMarkdown}
    sseError={props.sseError}
    autoScrollLogs={props.autoScrollLogs}
    onToggleAutoScroll={props.onToggleAutoScroll}
    onCopyLogs={props.onCopyLogs}
    onReconnectLogs={props.onReconnectLogs}
    logContainerRef={props.logContainerRef}
  />
</TaskDetailsSection>
```

Keep each `TabsContent` as the scrolling owner with `p-4`. Do not wrap `TaskDetailsSection` in another Card.

- [ ] **Step 6: Compact the log body**

Change the log body container from the flat border-y treatment to:

```tsx
<div
  ref={logContainerRef}
  className="min-h-28 flex-1 overflow-y-auto rounded-md bg-muted/30 p-3"
>
```

Change the empty component to:

```tsx
<Empty className="min-h-28 border-0 p-3">
  <EmptyHeader>
    <EmptyMedia variant="icon"><ScrollText /></EmptyMedia>
    <EmptyTitle>等待输出中...</EmptyTitle>
    <EmptyDescription>0 条日志</EmptyDescription>
  </EmptyHeader>
</Empty>
```

- [ ] **Step 7: Run focused tests and verify GREEN**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- src/components/tasks/TaskDetailsSheet.test.tsx \
  src/components/tasks/ReferenceUsageSummary.test.tsx
```

Expected: all sheet, configuration, material, and log tests PASS.

- [ ] **Step 8: Commit the restored sheet hierarchy**

```bash
git add \
  studio/src/components/tasks/TaskDetailsSheet.tsx \
  studio/src/components/tasks/TaskDetailsSheet.test.tsx
git commit -m "style(studio): restore task detail visual hierarchy"
```

---

### Task 4: Integrate Summary Navigation Into The Task Page

**Files:**
- Modify: `studio/src/pages/TaskDetailPage.tsx:1-1140`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx:1-740`

- [ ] **Step 1: Make the mocked route ID mutable in page tests**

Add a hoisted mutable route fixture above the router mock:

```tsx
const routeState = vi.hoisted(() => ({ taskId: 'task-1' }))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useParams: () => ({ id: routeState.taskId }),
    useNavigate: () => mockNavigate,
  }
})
```

Reset it in `beforeEach`:

```tsx
routeState.taskId = 'task-1'
```

- [ ] **Step 2: Write failing direct-navigation, ordering, and reset tests**

Add a running-task test:

```tsx
it('shows balanced context after results and opens target tabs in one action', async () => {
  mockTask(taskWith({
    status: 'running',
    progress: 42,
    progress_log: '已读取参考素材\n正在写作正文',
    latest_progress: {
      stage: 'writing',
      title: '正在写作正文',
      description: '正在优化标题与段落结构',
      percent: 42,
    },
    input_attachments: [{ type: 'image', file_name: 'tea-reference.jpg' }],
    project_snapshot: {
      project_name: '茶小茶',
      platform: 'article',
      visual_style: '清新茶感摄影',
      image_ratio: '3:4',
    },
    result: null,
    completed_at: '',
  }))

  render(<TaskDetailPage />)

  const resultHeading = await screen.findByRole('heading', { name: '任务结果' })
  const context = screen.getByRole('region', { name: '任务上下文' })
  expect(resultHeading.compareDocumentPosition(context) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  expect(within(context).getByText('茶小茶')).toBeInTheDocument()

  fireEvent.click(within(context).getByRole('button', { name: '打开执行日志' }))
  expect(await screen.findByRole('heading', { name: '任务详情' })).toBeInTheDocument()
  expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')
  expect(screen.getByRole('heading', { name: '阶段日志' })).toBeInTheDocument()
})
```

Update the existing Seednote ordering test so the order is generated files, analytics, then task context:

```tsx
const context = screen.getByRole('region', { name: '任务上下文' })
expect(generatedHeading.compareDocumentPosition(analyticsHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
expect(analyticsHeading.compareDocumentPosition(context) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
```

Add task-switch reset coverage:

```tsx
it('resets the controlled details tab when the route changes tasks', async () => {
  mockTask(taskWith({ id: 'task-1', status: 'running', result: null, completed_at: '' }))
  const view = render(<TaskDetailPage />)

  fireEvent.click(await screen.findByRole('button', { name: '打开执行日志' }))
  expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')

  routeState.taskId = 'task-2'
  vi.mocked(api.tasks.get).mockResolvedValue(taskWith({
    id: 'task-2',
    title: '第二个任务',
    status: 'running',
    result: null,
    completed_at: '',
  }))
  view.rerender(<TaskDetailPage />)

  await waitFor(() => expect(api.tasks.get).toHaveBeenCalledWith('task-2'))
  expect(screen.getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
})
```

- [ ] **Step 3: Run page tests and verify RED**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- src/pages/TaskDetailPage.test.tsx
```

Expected: FAIL because the context summary, direct tab selection, page-owned reset, and new ordering do not exist.

- [ ] **Step 4: Add page-owned details state and transition helper**

Add imports:

```tsx
import { TaskContextSummary } from '@/components/tasks/TaskContextSummary'
import {
  TaskDetailsSheet,
  type TaskDetailsTab,
} from '@/components/tasks/TaskDetailsSheet'
```

Remove `Info` from the page's Lucide import after deleting the standalone details button.

Add state beside `showTaskDetails`:

```tsx
const [taskDetailsTab, setTaskDetailsTab] = useState<TaskDetailsTab>('overview')
```

After task data is available to hooks, add the reset effect:

```tsx
useEffect(() => {
  setTaskDetailsTab('overview')
}, [task?.id])
```

Add the shared transition:

```tsx
function openTaskDetails(tab: TaskDetailsTab) {
  setTaskDetailsTab(tab)
  setShowTaskDetails(true)
}
```

- [ ] **Step 5: Control the existing sheet**

Pass these props to `TaskDetailsSheet`:

```tsx
selectedTab={taskDetailsTab}
onTabChange={setTaskDetailsTab}
```

Keep the existing credit transition. It closes and reopens the sheet without changing `taskDetailsTab`, so the originating context is preserved.

- [ ] **Step 6: Replace the ghost details button with the summary**

After Seednote analytics and before the resume dialog, render:

```tsx
<TaskContextSummary
  task={task}
  project={project}
  files={publishedFiles}
  logs={displayLogs}
  progressDescription={progressDescription}
  netConsumedCredits={netConsumedCredits}
  sseError={sseError}
  onOpenTab={openTaskDetails}
/>
```

Delete the old standalone `Button` containing `更多详情`. The general command now lives in the summary footer and calls `openTaskDetails('overview')`.

- [ ] **Step 7: Update existing page helpers and credit-return assertions**

Keep `openTaskDetails` in the test file for tests that need the general command, but make it assert the supplied controlled tab after clicking:

```tsx
async function openTaskDetails(tab?: '概览' | '配置' | '素材' | '日志') {
  fireEvent.click(await screen.findByRole('button', { name: '更多详情' }))
  expect(await screen.findByRole('heading', { name: '任务详情' })).toBeInTheDocument()
  if (tab) {
    fireEvent.click(screen.getByRole('tab', { name: tab }))
  }
}
```

Retain the existing credit-dialog test. Add an assertion before opening credits that Overview is selected and after closing that the same tab remains selected.

- [ ] **Step 8: Run all affected tests and verify GREEN**

Run:

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- \
  src/components/tasks/TaskContextSummary.test.tsx \
  src/components/tasks/TaskDetailsSheet.test.tsx \
  src/components/tasks/ReferenceUsageSummary.test.tsx \
  src/pages/TaskDetailPage.test.tsx
```

Expected: all affected tests PASS.

- [ ] **Step 9: Commit page integration**

```bash
git add \
  studio/src/pages/TaskDetailPage.tsx \
  studio/src/pages/TaskDetailPage.test.tsx
git commit -m "feat(studio): rebalance task detail context"
```

---

### Task 5: Browser Fidelity, Full Verification, And Review

**Files:**
- Modify only if browser QA finds a concrete issue:
  - `studio/src/components/tasks/TaskContextSummary.tsx`
  - `studio/src/components/tasks/TaskContextSummary.test.tsx`
  - `studio/src/components/tasks/TaskDetailsSheet.tsx`
  - `studio/src/components/tasks/TaskDetailsSheet.test.tsx`
  - `studio/src/pages/TaskDetailPage.tsx`
  - `studio/src/pages/TaskDetailPage.test.tsx`

- [ ] **Step 1: Run focused verification from a clean working tree**

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test -- \
  src/components/tasks/TaskContextSummary.test.tsx \
  src/components/tasks/TaskDetailsSheet.test.tsx \
  src/components/tasks/ReferenceUsageSummary.test.tsx \
  src/pages/TaskDetailPage.test.tsx
```

Expected: all affected tests PASS.

- [ ] **Step 2: Run the complete Studio test suite**

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run test
```

Expected: every test file and test passes with zero failures.

- [ ] **Step 3: Run the production build**

```bash
cd studio
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run build
```

Expected: `tsc -b && vite build` exits `0`. The existing Vite large-chunk advisory is non-blocking unless the changed task-detail chunk materially regresses.

- [ ] **Step 4: Start the development server and use the in-app Browser**

```bash
cd studio
VITE_API_BASE_URL=/api/v1 \
PATH=/Users/medivh/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:$PATH \
  bun run dev --host 127.0.0.1 --port 5173
```

Use the in-app Browser first. Validate a real local task or temporary non-repository fixtures representing:

- Running task with progress, pending result destination, one material, and live logs.
- Completed task with workflow review and generated files.
- Failed task with recovery action and collected failure files.
- Publish-approval task with approval actions and generated files.

Do not commit mock servers, screenshots, traces, or fixture scripts.

- [ ] **Step 5: Verify the target interaction loop**

For desktop and mobile:

1. Confirm status/progress or recovery renders before results.
2. Confirm results/deliverables render before the context summary.
3. Click Overview, Configuration, Materials, and Logs summary items.
4. After each click, confirm the matching tab is selected and its grouped content is visible.
5. Open More Details and confirm Overview is selected.
6. Open credit details, close it, and confirm the sheet and selected tab are restored.
7. Toggle log following, copy logs, and exercise reconnect when the fixture exposes an SSE error.
8. Close the sheet and confirm focus returns to the originating summary item.

- [ ] **Step 6: Verify responsive geometry and console health**

Desktop viewport: `1440x1000`.

- Sheet bounding width is `576px`.
- Summary is one four-column grouped surface.
- No nested-card appearance, clipped controls, or horizontal overflow.

Mobile viewport: `390x844`.

- Sheet bounding width is `390px`.
- Summary is a stable two-by-two grid.
- Tabs and close control do not overlap.
- Document scroll width equals viewport width.

For both viewports, confirm no relevant console errors or framework overlay.

- [ ] **Step 7: Compare against the accepted concept**

Accepted visual companion artifact:

```text
.superpowers/brainstorm/74807-1784168086/content/task-detail-rebalance-options.html
```

Capture current implementation screenshots outside the repository. Use `view_image` on the accepted A1 concept and implementation screenshots. Record a five-point fidelity ledger covering:

1. Result-first order.
2. Summary density and responsive grid.
3. Warm accent, icons, borders, and radii.
4. Desktop sheet width and mobile full-width behavior.
5. Grouped Overview, Configuration, Materials, and Logs content.

Fix every material mismatch before continuing.

- [ ] **Step 8: Commit browser-driven fixes if any**

If Browser QA required source changes:

```bash
git add \
  studio/src/components/tasks/TaskContextSummary.tsx \
  studio/src/components/tasks/TaskContextSummary.test.tsx \
  studio/src/components/tasks/TaskDetailsSheet.tsx \
  studio/src/components/tasks/TaskDetailsSheet.test.tsx \
  studio/src/pages/TaskDetailPage.tsx \
  studio/src/pages/TaskDetailPage.test.tsx
git commit -m "fix(studio): polish balanced task detail ux"
```

If no source changed, do not create an empty commit.

- [ ] **Step 9: Run final hygiene checks**

```bash
git diff --check
git status --short --branch
git diff --stat HEAD~4..HEAD
```

Expected:

- `git diff --check` exits `0` with no output.
- Only intentional feature files are changed or committed.
- Existing unrelated `claudecode` work remains unstaged and unchanged.
- No `.superpowers/`, screenshot, mock, trace, or build-output files are staged.

- [ ] **Step 10: Request a whole-range code review**

Review from the pre-feature base through `HEAD` against:

```text
docs/superpowers/specs/2026-07-17-task-detail-balanced-context-design.md
```

The review must prioritize behavioral regressions, direct-tab state, task-switch reset, result ordering, sheet width variants, accessibility, and missing tests. Fix all Critical and Important findings, rerun the complete Studio suite and build, and request re-review until no blocking findings remain.

---

## Completion Gate

Before presenting branch integration options, verify all of the following with fresh evidence:

- Focused tests pass.
- Full Studio tests pass.
- Studio production build passes.
- Desktop and mobile Browser QA passes.
- Accepted concept and implementation screenshots have been inspected.
- Whole-range review has no Critical or Important findings.
- `git diff --check` is clean.
- Unrelated `claudecode` changes remain untouched.
