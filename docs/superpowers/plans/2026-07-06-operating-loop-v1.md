# Operating Loop v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Studio feel like one creator operating loop from setup readiness through project operation, task recovery, output review, publish approval, reuse, and lightweight insight.

**Architecture:** This plan keeps the existing client-side aggregation model and avoids a new backend aggregate endpoint. `studio/src/lib/command-center.ts` becomes the shared action/readiness model; Dashboard and the command palette consume the same signals; Projects, Tasks, and Task Detail present consistent operating semantics over existing APIs and task/project fields.

**Tech Stack:** React 19, TypeScript, Vite 8, TanStack Query, React Router, shadcn/Base UI components, Vitest, Testing Library, Bun.

---

## File Structure

- Modify `studio/src/lib/command-center.ts`: shared readiness statuses, model-config readiness helper, setup action ranking, and richer `readiness.checks`.
- Modify `studio/src/lib/command-center.test.ts`: unit coverage for unknown/not-ready setup states and action priority.
- Modify `studio/src/pages/DashboardPage.tsx`: query API keys, model config, and desktop local executor status; render readiness tiles from shared checks.
- Modify `studio/src/pages/DashboardPage.command-center.test.tsx`: verify Dashboard renders real readiness states and still links to recovery and approval tasks.
- Modify `studio/src/components/GlobalCommandPalette.tsx`: feed the same readiness inputs into command-center signals when the palette opens.
- Modify `studio/src/components/GlobalCommandPalette.actions.test.tsx`: verify setup actions appear before generic creation when API/model readiness is missing.
- Modify `studio/src/components/ProjectCard.tsx`: show compact operating badges for publishing, approval mode, defaults, and success rate.
- Modify `studio/src/components/ProjectCard.test.tsx`: cover project operating badges and existing task/plan links.
- Create `studio/src/lib/workflow-readiness.ts`: shared workflow parsing and readiness label helpers.
- Create `studio/src/lib/workflow-readiness.test.ts`: unit coverage for object, JSON string, invalid workflow, and readiness labels.
- Modify `studio/src/components/TaskWorkflowPanel.tsx`: use shared workflow helpers while preserving `parseWorkflow` export.
- Modify `studio/src/pages/TasksPage.tsx`: use shared readiness labels and align publish approval wording with Dashboard and Task Detail.
- Modify `studio/src/pages/TasksPage.ux.contract.test.ts`: assert the recovery workspace and publish approval copy stay aligned.
- Modify `studio/src/pages/TasksPage.test.tsx`: verify approval tasks surface in the recovery queue and badge copy.
- Modify `studio/src/pages/TaskDetailPage.tsx`: render workflow stages in the main production narrative before review/files/logs for terminal tasks.
- Modify `studio/src/pages/TaskDetailPage.test.tsx`: verify completed tasks show workflow stages before review/files and running tasks keep the compact live-progress card.

## Task 1: Shared Command Center Readiness Model

**Files:**
- Modify: `studio/src/lib/command-center.ts`
- Modify: `studio/src/lib/command-center.test.ts`

- [ ] **Step 1: Write failing tests for explicit readiness states**

Add these imports and tests to `studio/src/lib/command-center.test.ts`.

```ts
  it('represents setup readiness as ready, not-ready, or unknown', () => {
    const signals = buildCommandCenterSignals({
      now: new Date('2026-07-06T02:00:00.000Z'),
      tasks: [],
      plans: [],
      projects: [project({ config: { enable_publishing: true, require_publish_approval: true } })],
      creditsBalance: { balance: 1000 },
      signInStatus: { signed_in_today: true },
      apiKeysReady: null,
      modelConfigReady: false,
      localExecutorReady: true,
    })

    expect(signals.readiness.projectsReady).toBe(true)
    expect(signals.readiness.publishingReady).toBe(true)
    expect(signals.readiness.checks.projects.status).toBe('ready')
    expect(signals.readiness.checks.apiKeys.status).toBe('unknown')
    expect(signals.readiness.checks.modelConfig.status).toBe('not_ready')
    expect(signals.readiness.checks.localExecutor.status).toBe('ready')
    expect(signals.readiness.checks.publishing.description).toContain('发布需审核')
  })

  it('places setup review before generic creation when keys or model config are not ready', () => {
    const signals = buildCommandCenterSignals({
      now: new Date('2026-07-06T02:00:00.000Z'),
      tasks: [],
      plans: [],
      projects: [project()],
      creditsBalance: { balance: 1000 },
      signInStatus: { signed_in_today: true },
      apiKeysReady: false,
      modelConfigReady: null,
      localExecutorReady: true,
    })

    expect(buildNextBestActions(signals).map((action) => action.id)).toEqual([
      'connect-settings',
      'create-task',
    ])
  })

  it('detects usable model config from text or image model settings', () => {
    const empty: ModelConfigLike = {}
    const textReady: ModelConfigLike = { text: { model: 'gpt-5' } }
    const imageReady: ModelConfigLike = { image: { provider: 'openai' } }

    expect(hasUsableModelConfig(empty)).toBe(false)
    expect(hasUsableModelConfig(textReady)).toBe(true)
    expect(hasUsableModelConfig(imageReady)).toBe(true)
  })
```

Update the import list in the same test file.

```ts
import {
  buildCommandCenterSignals,
  buildNextBestActions,
  hasUsableModelConfig,
  projectCreatedReturnHref,
  createTaskHref,
  parseCreationIntent,
  projectsReturnHref,
  type ModelConfigLike,
} from './command-center'
```

For the existing tests that do not exercise setup readiness, add these three fields to their `buildCommandCenterSignals` inputs so the old priority assertions remain about recovery, approval, credits, and creation.

```ts
      apiKeysReady: true,
      modelConfigReady: true,
      localExecutorReady: true,
```

- [ ] **Step 2: Run the focused unit test and verify it fails**

Run:

```bash
cd studio && bun run test -- src/lib/command-center.test.ts
```

Expected: FAIL because `apiKeysReady`, `modelConfigReady`, `localExecutorReady`, `readiness.checks`, `ModelConfigLike`, and `hasUsableModelConfig` do not exist yet.

- [ ] **Step 3: Add readiness statuses and model config helper**

In `studio/src/lib/command-center.ts`, replace the current `CommandCenterReadiness` interface with this block and add the helper types/functions near the constants.

```ts
export type ReadinessStatus = 'ready' | 'not_ready' | 'unknown'

export interface CommandCenterReadinessCheck {
  status: ReadinessStatus
  label: string
  description: string
  href: string
}

export interface CommandCenterReadiness {
  projectsReady: boolean
  localExecutorKnown: boolean
  modelConfigKnown: boolean
  apiKeysKnown: boolean
  publishingReady: boolean
  checks: {
    projects: CommandCenterReadinessCheck
    apiKeys: CommandCenterReadinessCheck
    modelConfig: CommandCenterReadinessCheck
    localExecutor: CommandCenterReadinessCheck
    publishing: CommandCenterReadinessCheck
  }
}

export interface ModelConfigLike {
  text?: {
    endpoint?: string
    api_key?: string
    model?: string
  } | null
  image?: {
    provider?: string
    endpoint?: string
    api_key?: string
    model?: string
  } | null
}

export function hasUsableModelConfig(config?: ModelConfigLike | null) {
  return Boolean(
    config?.text?.model ||
    config?.text?.endpoint ||
    config?.text?.api_key ||
    config?.image?.provider ||
    config?.image?.model ||
    config?.image?.endpoint ||
    config?.image?.api_key,
  )
}

function readinessStatus(ready?: boolean | null, legacyKnown?: boolean): ReadinessStatus {
  if (ready === true) return 'ready'
  if (ready === false) return 'not_ready'
  if (legacyKnown === true) return 'ready'
  if (legacyKnown === false) return 'unknown'
  return 'unknown'
}
```

Extend `BuildCommandCenterSignalsInput`.

```ts
  localExecutorReady?: boolean | null
  modelConfigReady?: boolean | null
  apiKeysReady?: boolean | null
```

- [ ] **Step 4: Build readiness checks inside `buildCommandCenterSignals`**

Inside `buildCommandCenterSignals`, after `creditRisk`, add this block.

```ts
  const publishableProjects = projects.filter((project) => project.config.enable_publishing)
  const approvalProjects = publishableProjects.filter((project) => project.config.require_publish_approval)
  const projectStatus: ReadinessStatus = projects.length > 0 ? 'ready' : 'not_ready'
  const publishingStatus: ReadinessStatus =
    projects.length === 0 ? 'unknown' : publishableProjects.length > 0 ? 'ready' : 'not_ready'
  const apiKeysStatus = readinessStatus(input.apiKeysReady, input.apiKeysKnown)
  const modelConfigStatus = readinessStatus(input.modelConfigReady, input.modelConfigKnown)
  const localExecutorStatus = readinessStatus(input.localExecutorReady, input.localExecutorKnown)

  const checks: CommandCenterReadiness['checks'] = {
    projects: {
      status: projectStatus,
      label: '项目定位',
      description: projects.length > 0 ? `${projects.length} 个项目可用` : '先创建账号/项目',
      href: projects.length > 0 ? '/projects' : projectsReturnHref({ type: 'seednote', intent: 'new' }),
    },
    apiKeys: {
      status: apiKeysStatus,
      label: '平台密钥',
      description:
        apiKeysStatus === 'ready'
          ? '密钥可用于 Agent 接入'
          : apiKeysStatus === 'not_ready'
            ? '需要创建平台密钥'
            : '正在检查密钥状态',
      href: '/settings',
    },
    modelConfig: {
      status: modelConfigStatus,
      label: '模型配置',
      description:
        modelConfigStatus === 'ready'
          ? '模型策略已配置'
          : modelConfigStatus === 'not_ready'
            ? '需要配置文本或图像模型'
            : '正在检查模型配置',
      href: '/settings',
    },
    localExecutor: {
      status: localExecutorStatus,
      label: '执行环境',
      description:
        localExecutorStatus === 'ready'
          ? '执行环境可用'
          : localExecutorStatus === 'not_ready'
            ? '本地执行器未就绪，可走云端'
            : '正在检查桌面执行状态',
      href: '/settings',
    },
    publishing: {
      status: publishingStatus,
      label: '发布能力',
      description:
        publishingStatus === 'ready'
          ? approvalProjects.length > 0
            ? `${publishableProjects.length} 个项目可发布，${approvalProjects.length} 个发布需审核`
            : `${publishableProjects.length} 个项目可发布到草稿箱`
          : publishingStatus === 'not_ready'
            ? '需要检查发布配置'
            : '创建项目后检查发布配置',
      href: '/settings',
    },
  }
```

Replace the returned `readiness` object with this block.

```ts
    readiness: {
      projectsReady: checks.projects.status === 'ready',
      localExecutorKnown: checks.localExecutor.status !== 'unknown',
      modelConfigKnown: checks.modelConfig.status === 'ready',
      apiKeysKnown: checks.apiKeys.status === 'ready',
      publishingReady: checks.publishing.status === 'ready',
      checks,
    },
```

- [ ] **Step 5: Update setup action priority**

In `buildNextBestActions`, move the settings action before credit/upcoming/create. Use this exact setup flag after `defaultProject`.

```ts
  const setupNeedsAttention =
    signals.readiness.checks.apiKeys.status !== 'ready' ||
    signals.readiness.checks.modelConfig.status !== 'ready'
```

Place this action block after the first-project block and before the credit-risk block.

```ts
  if (setupNeedsAttention && signals.readiness.projectsReady) {
    actions.push({
      id: 'connect-settings',
      label: '检查接入设置',
      description: '确认平台密钥和模型配置后再创建任务',
      href: '/settings',
      kind: 'neutral',
    })
  }
```

Remove the old `connect-settings` block at the bottom of the function.

- [ ] **Step 6: Run the focused unit test and commit**

Run:

```bash
cd studio && bun run test -- src/lib/command-center.test.ts
```

Expected: PASS.

Commit:

```bash
git add studio/src/lib/command-center.ts studio/src/lib/command-center.test.ts
git commit -m "feat: model operating readiness signals"
```

## Task 2: Dashboard And Command Palette Use Real Readiness Inputs

**Files:**
- Modify: `studio/src/pages/DashboardPage.tsx`
- Modify: `studio/src/pages/DashboardPage.command-center.test.tsx`
- Modify: `studio/src/components/GlobalCommandPalette.tsx`
- Modify: `studio/src/components/GlobalCommandPalette.actions.test.tsx`

- [ ] **Step 1: Write failing Dashboard readiness test**

In `studio/src/pages/DashboardPage.command-center.test.tsx`, add a Tauri mock near the existing mocks.

```ts
vi.mock('@/lib/tauri', () => ({
  isDesktop: () => true,
  getLocalExecutorStatus: vi.fn().mockResolvedValue({
    state: 'running_idle',
    available: true,
    running: true,
    reason: '',
  }),
}))
```

Add `modelConfig` to the mocked API object.

```ts
      modelConfig: {
        ...actual.api.modelConfig,
        get: vi.fn().mockResolvedValue({ text: { model: 'gpt-5' }, image: null }),
      },
```

Extend the existing test expectations.

```ts
    expect(await screen.findByText('平台密钥')).toBeInTheDocument()
    expect(await screen.findByText('密钥可用于 Agent 接入')).toBeInTheDocument()
    expect(await screen.findByText('模型配置')).toBeInTheDocument()
    expect(await screen.findByText('模型策略已配置')).toBeInTheDocument()
    expect(await screen.findByText('执行环境')).toBeInTheDocument()
    expect(await screen.findByText('执行环境可用')).toBeInTheDocument()
    expect(api.apiKeys.list).toHaveBeenCalled()
    expect(api.modelConfig.get).toHaveBeenCalled()
```

- [ ] **Step 2: Write failing command palette setup-action test**

In `studio/src/components/GlobalCommandPalette.actions.test.tsx`, add `apiKeys` and `modelConfig` mocks that return missing readiness.

```ts
      apiKeys: {
        ...actual.api.apiKeys,
        list: vi.fn().mockResolvedValue({ items: [] }),
      },
      modelConfig: {
        ...actual.api.modelConfig,
        get: vi.fn().mockResolvedValue({ text: null, image: null }),
      },
```

Add a test after the existing one.

```ts
  it('surfaces setup review before generic creation when readiness is missing', async () => {
    renderWithProviders(<GlobalCommandPalette />)

    act(() => commandPaletteStore.open())

    expect(await screen.findByText('检查接入设置')).toBeInTheDocument()
    expect(await screen.findByText('新建公众号文章')).toBeInTheDocument()
  })
```

- [ ] **Step 3: Run the focused page/component tests and verify they fail**

Run:

```bash
cd studio && bun run test -- src/pages/DashboardPage.command-center.test.tsx src/components/GlobalCommandPalette.actions.test.tsx
```

Expected: FAIL because Dashboard and command palette do not query model config/API keys for command-center readiness yet.

- [ ] **Step 4: Feed readiness inputs into Dashboard**

In `studio/src/pages/DashboardPage.tsx`, update imports.

```ts
import { queryKeys } from '@/lib/query-keys'
import { getLocalExecutorStatus, isDesktop } from '@/lib/tauri'
import { buildCommandCenterSignals, buildNextBestActions, createTaskHref, hasUsableModelConfig, projectsReturnHref, type CommandCenterSignalKind, type ReadinessStatus } from '@/lib/command-center'
```

Add these queries after the projects query.

```ts
  const desktopMode = isDesktop()

  const { data: apiKeys = [], isLoading: apiKeysLoading } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: async () => {
      const data = await api.apiKeys.list()
      return data.items || []
    },
  })

  const { data: modelConfig, isLoading: modelConfigLoading } = useQuery({
    queryKey: queryKeys.modelConfig.all,
    queryFn: () => api.modelConfig.get(),
  })

  const { data: localExecutorStatus, isLoading: localExecutorLoading } = useQuery({
    queryKey: ['dashboard', 'local-executor-status'],
    queryFn: getLocalExecutorStatus,
    enabled: desktopMode,
    staleTime: 30_000,
  })
```

Update `commandSignals`.

```ts
  const commandSignals = useMemo(() => buildCommandCenterSignals({
    tasks,
    plans,
    projects,
    creditsBalance,
    signInStatus,
    apiKeysReady: apiKeysLoading ? null : apiKeys.length > 0,
    modelConfigReady: modelConfigLoading ? null : hasUsableModelConfig(modelConfig),
    localExecutorReady: desktopMode
      ? localExecutorLoading
        ? null
        : Boolean(localExecutorStatus?.available)
      : true,
  }), [
    tasks,
    plans,
    projects,
    creditsBalance,
    signInStatus,
    apiKeysLoading,
    apiKeys.length,
    modelConfigLoading,
    modelConfig,
    desktopMode,
    localExecutorLoading,
    localExecutorStatus,
  ])
```

Replace the four hardcoded readiness tiles with this mapped block.

```tsx
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-5">
              {[
                commandSignals.readiness.checks.projects,
                commandSignals.readiness.checks.apiKeys,
                commandSignals.readiness.checks.modelConfig,
                commandSignals.readiness.checks.localExecutor,
                commandSignals.readiness.checks.publishing,
              ].map((item) => (
                <ReadinessItem
                  key={item.label}
                  status={item.status}
                  label={item.label}
                  description={item.description}
                  href={item.href}
                />
              ))}
            </div>
```

Update `ReadinessItem`.

```tsx
function ReadinessItem({
  status,
  label,
  description,
  href,
}: {
  status: ReadinessStatus
  label: string
  description: string
  href: string
}) {
  const ready = status === 'ready'
  const unknown = status === 'unknown'
  return (
    <Link
      to={href}
      className="flex items-start gap-3 rounded-lg border border-border bg-background p-3 transition-colors hover:border-primary/30 hover:bg-accent"
    >
      <span className={cn(
        'mt-0.5 flex size-6 items-center justify-center rounded-full bg-muted text-muted-foreground',
        ready && 'bg-primary/10 text-primary',
        !ready && !unknown && 'bg-destructive/10 text-destructive',
      )}>
        {ready ? <CheckCircle2 className="size-3.5" /> : <Settings className="size-3.5" />}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium text-foreground">{label}</span>
        <span className="mt-0.5 block line-clamp-2 text-xs text-muted-foreground">{description}</span>
      </span>
    </Link>
  )
}
```

- [ ] **Step 5: Feed readiness inputs into GlobalCommandPalette**

In `studio/src/components/GlobalCommandPalette.tsx`, update imports.

```ts
import { buildCommandCenterSignals, buildNextBestActions, createTaskHref, hasUsableModelConfig, projectsReturnHref } from '@/lib/command-center'
import { queryKeys } from '@/lib/query-keys'
```

Add these queries after the credits/sign-in queries.

```ts
  const { data: apiKeys = [] } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: async () => {
      const data = await api.apiKeys.list()
      return data.items || []
    },
    enabled: open,
  })
  const { data: modelConfig } = useQuery({
    queryKey: queryKeys.modelConfig.all,
    queryFn: () => api.modelConfig.get(),
    enabled: open,
  })
```

Update the `signals` memo.

```ts
  const signals = useMemo(() => buildCommandCenterSignals({
    tasks,
    plans,
    projects,
    creditsBalance,
    signInStatus,
    apiKeysReady: apiKeys.length > 0,
    modelConfigReady: hasUsableModelConfig(modelConfig),
    localExecutorReady: true,
  }), [tasks, plans, projects, creditsBalance, signInStatus, apiKeys.length, modelConfig])
```

- [ ] **Step 6: Run focused tests and commit**

Run:

```bash
cd studio && bun run test -- src/pages/DashboardPage.command-center.test.tsx src/components/GlobalCommandPalette.actions.test.tsx
```

Expected: PASS.

Commit:

```bash
git add studio/src/pages/DashboardPage.tsx studio/src/pages/DashboardPage.command-center.test.tsx studio/src/components/GlobalCommandPalette.tsx studio/src/components/GlobalCommandPalette.actions.test.tsx
git commit -m "feat: connect readiness to action surfaces"
```

## Task 3: Project Cards As Operating Units

**Files:**
- Modify: `studio/src/components/ProjectCard.tsx`
- Modify: `studio/src/components/ProjectCard.test.tsx`

- [ ] **Step 1: Write failing project-card badge test**

In `studio/src/components/ProjectCard.test.tsx`, add this test.

```tsx
  it('summarizes publishing mode, defaults, and success rate as operating badges', () => {
    render(
      <ProjectCard
        project={{
          ...project,
          visual_style: '',
          writer: 'dan-koe',
          theme: 'autumn-warm',
          author: '安般',
          config: { enable_publishing: true, require_publish_approval: true },
        }}
        stats={{
          total_tasks: 7,
          completed_tasks: 6,
          failed_tasks: 1,
          running_tasks: 0,
          pending_tasks: 0,
          success_rate: 0.86,
          last_activity_at: '2026-07-06T08:00:00.000Z',
        }}
      />,
    )

    expect(screen.getByText('公众号草稿箱')).toBeInTheDocument()
    expect(screen.getByText('发布需审核')).toBeInTheDocument()
    expect(screen.getByText('视觉未配置')).toBeInTheDocument()
    expect(screen.getByText('写作已配置')).toBeInTheDocument()
    expect(screen.getByText('成功率 86%')).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the focused project-card test and verify it fails**

Run:

```bash
cd studio && bun run test -- src/components/ProjectCard.test.tsx
```

Expected: FAIL because the operating badges are not rendered.

- [ ] **Step 3: Add project operating badge helpers**

In `studio/src/components/ProjectCard.tsx`, add this helper above `export function ProjectCard`.

```tsx
function buildOperatingBadges(project: Project, stats?: ProjectStats) {
  const badges: Array<{ label: string; variant: 'secondary' | 'outline' | 'destructive' }> = []

  if (project.config.enable_publishing) {
    badges.push({ label: '公众号草稿箱', variant: 'secondary' })
    badges.push({
      label: project.config.require_publish_approval ? '发布需审核' : '自动入草稿箱',
      variant: project.config.require_publish_approval ? 'outline' : 'secondary',
    })
  } else if (project.platform === 'article') {
    badges.push({ label: '发布未启用', variant: 'outline' })
  }

  badges.push({
    label: project.visual_style ? '视觉已配置' : '视觉未配置',
    variant: project.visual_style ? 'secondary' : 'outline',
  })

  if (project.platform === 'article') {
    badges.push({
      label: project.writer || project.theme || project.author ? '写作已配置' : '写作未配置',
      variant: project.writer || project.theme || project.author ? 'secondary' : 'outline',
    })
  }

  if (project.platform === 'ecommerce' && project.ecommerce_defaults?.target_platform) {
    badges.push({ label: `投放 ${project.ecommerce_defaults.target_platform}`, variant: 'secondary' })
  }

  if (project.platform === 'video' && project.video_defaults?.model_key) {
    badges.push({ label: '视频默认已配置', variant: 'secondary' })
  }

  if (stats && stats.total_tasks > 0) {
    badges.push({ label: `成功率 ${(stats.success_rate * 100).toFixed(0)}%`, variant: stats.success_rate >= 0.6 ? 'secondary' : 'destructive' })
  }

  return badges
}
```

- [ ] **Step 4: Render badges in the card**

Inside `ProjectCard`, add this constant after `canCreatePlan`.

```ts
  const operatingBadges = buildOperatingBadges(project, stats)
```

Render this block after the positioning paragraph and before stats.

```tsx
      {operatingBadges.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-1.5">
          {operatingBadges.map((badge) => (
            <Badge key={badge.label} variant={badge.variant} className="text-[10px]">
              {badge.label}
            </Badge>
          ))}
        </div>
      )}
```

- [ ] **Step 5: Run the focused project-card test and commit**

Run:

```bash
cd studio && bun run test -- src/components/ProjectCard.test.tsx
```

Expected: PASS.

Commit:

```bash
git add studio/src/components/ProjectCard.tsx studio/src/components/ProjectCard.test.tsx
git commit -m "feat: show project operating badges"
```

## Task 4: Shared Workflow Readiness And Task List Language

**Files:**
- Create: `studio/src/lib/workflow-readiness.ts`
- Create: `studio/src/lib/workflow-readiness.test.ts`
- Modify: `studio/src/components/TaskWorkflowPanel.tsx`
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/TasksPage.ux.contract.test.ts`
- Modify: `studio/src/pages/TasksPage.test.tsx`

- [ ] **Step 1: Add workflow readiness helper tests**

Create `studio/src/lib/workflow-readiness.test.ts`.

```ts
import { describe, expect, it } from 'vitest'
import { parseWorkflowStatus, workflowReadinessLabel } from './workflow-readiness'
import type { WorkflowStatus } from '@/types'

const workflow: WorkflowStatus = {
  version: 'creation_workflow_v1',
  current_stage: 'review',
  stages: [{ key: 'review', label: '质量复盘', status: 'completed' }],
  review: {
    overall_score: 72,
    readiness: 'ready_with_minor_edits',
    risks: ['标题偏弱'],
    next_actions: ['改标题'],
    strengths: ['结构完整'],
  },
}

describe('workflow readiness helpers', () => {
  it('parses workflow objects and JSON strings', () => {
    expect(parseWorkflowStatus(workflow)?.current_stage).toBe('review')
    expect(parseWorkflowStatus(JSON.stringify(workflow))?.review?.overall_score).toBe(72)
  })

  it('returns null for invalid workflow strings', () => {
    expect(parseWorkflowStatus('broken-json')).toBeNull()
  })

  it('standardizes review readiness labels', () => {
    expect(workflowReadinessLabel({ ...workflow, review: { ...workflow.review!, readiness: 'ready' } })).toBe('可发布')
    expect(workflowReadinessLabel(workflow)).toBe('建议修改')
    expect(workflowReadinessLabel({ ...workflow, review: { ...workflow.review!, readiness: 'needs_revision' } })).toBe('需重做')
    expect(workflowReadinessLabel({ ...workflow, review: null })).toBe('')
  })
})
```

- [ ] **Step 2: Extend task page tests for approval language**

In `studio/src/pages/TasksPage.test.tsx`, add an approval task fixture inside `fixtures`.

```ts
  const approvalTask = {
    id: 'approval-task',
    type: 'article',
    title: '待确认草稿',
    prompt: '审批任务',
    status: 'completed',
    progress: 100,
    error: null,
    plan_id: null,
    project_id: project.id,
    result: { files: null, output: '' },
    published: false,
    published_at: null,
    publish_approval_state: 'pending',
    workflow_status: {
      version: 'creation_workflow_v1',
      current_stage: 'review',
      stages: [{ key: 'review', label: '质量复盘', status: 'completed' }],
      review: { overall_score: 88, readiness: 'ready', risks: [], next_actions: [], strengths: [] },
    },
    created_at: '2026-07-06T02:00:00.000Z',
    started_at: '',
    completed_at: '2026-07-06T02:10:00.000Z',
  }
```

Return it from the fixture object.

```ts
  return { project, failedTask, approvalTask }
```

Add this test.

```tsx
  it('uses the same publish approval and readiness labels in the recovery queue and task cards', async () => {
    vi.mocked(api.tasks.list).mockResolvedValue({
      items: [fixtures.failedTask as Task, fixtures.approvalTask as Task],
      total: 2,
    })

    renderTasksPage()

    expect(await screen.findByRole('link', { name: /待发布确认/ })).toBeInTheDocument()
    expect(await screen.findByText('待发布确认')).toBeInTheDocument()
    expect(await screen.findByText('可发布')).toBeInTheDocument()
  })
```

In `studio/src/pages/TasksPage.ux.contract.test.ts`, change the approval copy assertion.

```ts
    expect(source).toContain('待发布确认')
    expect(source).toContain('放行到公众号草稿箱')
```

- [ ] **Step 3: Run focused tests and verify they fail**

Run:

```bash
cd studio && bun run test -- src/lib/workflow-readiness.test.ts src/pages/TasksPage.test.tsx src/pages/TasksPage.ux.contract.test.ts
```

Expected: FAIL because the helper file is missing and task list approval copy still uses older wording.

- [ ] **Step 4: Create shared workflow readiness helper**

Create `studio/src/lib/workflow-readiness.ts`.

```ts
import type { WorkflowStatus } from '@/types'

export function parseWorkflowStatus(workflow?: WorkflowStatus | string | null): WorkflowStatus | null {
  if (!workflow) return null
  if (typeof workflow !== 'string') return workflow
  try {
    return JSON.parse(workflow) as WorkflowStatus
  } catch {
    return null
  }
}

export function readinessValueLabel(readiness?: string) {
  switch (readiness) {
    case 'ready':
      return '可发布'
    case 'ready_with_minor_edits':
      return '建议修改'
    case 'needs_revision':
      return '需重做'
    default:
      return readiness || ''
  }
}

export function workflowReadinessLabel(workflow: WorkflowStatus | string | null | undefined) {
  const parsed = parseWorkflowStatus(workflow)
  return readinessValueLabel(parsed?.review?.readiness)
}
```

- [ ] **Step 5: Use the helper in TaskWorkflowPanel and TasksPage**

In `studio/src/components/TaskWorkflowPanel.tsx`, add this import.

```ts
import { parseWorkflowStatus, readinessValueLabel } from '@/lib/workflow-readiness'
```

Replace the existing `parseWorkflow` and `readinessLabel` functions with:

```ts
export function parseWorkflow(workflow?: WorkflowStatus | string | null): WorkflowStatus | null {
  return parseWorkflowStatus(workflow)
}
```

Replace `readinessLabel(data.review.readiness)` with:

```tsx
              {readinessValueLabel(data.review.readiness) || '未评估'}
```

In `studio/src/pages/TasksPage.tsx`, add this import.

```ts
import { workflowReadinessLabel } from '@/lib/workflow-readiness'
```

Remove the local `workflowReadinessLabel` function from `TasksPage.tsx`.

Change the recovery tile description for approval.

```tsx
            description="审核后放行到公众号草稿箱"
```

Change the task badge copy for pending approval.

```tsx
                              待发布确认
```

- [ ] **Step 6: Run focused tests and commit**

Run:

```bash
cd studio && bun run test -- src/lib/workflow-readiness.test.ts src/components/TaskWorkflowPanel.test.tsx src/pages/TasksPage.test.tsx src/pages/TasksPage.ux.contract.test.ts
```

Expected: PASS.

Commit:

```bash
git add studio/src/lib/workflow-readiness.ts studio/src/lib/workflow-readiness.test.ts studio/src/components/TaskWorkflowPanel.tsx studio/src/pages/TasksPage.tsx studio/src/pages/TasksPage.ux.contract.test.ts studio/src/pages/TasksPage.test.tsx
git commit -m "feat: align workflow and approval labels"
```

## Task 5: Task Detail Production Narrative

**Files:**
- Modify: `studio/src/pages/TaskDetailPage.tsx`
- Modify: `studio/src/pages/TaskDetailPage.test.tsx`

- [ ] **Step 1: Write failing production-narrative test**

In `studio/src/pages/TaskDetailPage.test.tsx`, add this test after `hides the progress card after completion`.

```tsx
  it('shows terminal workflow stages before review and generated files', async () => {
    mockTask(taskWith({
      status: 'completed',
      progress: 100,
      workflow_status: {
        version: 'creation_workflow_v1',
        current_stage: 'review',
        stages: [
          { key: 'draft', label: '初稿', status: 'completed', artifact_paths: ['03-draft.md'] },
          { key: 'review', label: '质量复盘', status: 'completed', artifact_paths: ['review.json'] },
        ],
        review: {
          overall_score: 91,
          readiness: 'ready',
          risks: ['标题可微调'],
          next_actions: ['发布前改标题'],
          strengths: ['结构完整'],
        },
      },
      result: { files: null, output: '' },
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'file-1',
      task_id: 'task-1',
      role: 'output',
      file_name: 'article.html',
      mime_type: 'text/html',
      file_size: 1024,
      url: '/api/v1/files/file-1',
      created_at: '2026-07-06T03:00:00Z',
    }])

    render(<TaskDetailPage />)

    const stages = await screen.findByText('创作进度')
    const review = await screen.findByText('发布前检查')
    const parameters = await screen.findByText('项目参数')
    const files = await screen.findByText('生成文件 (1)')
    expect(stages.compareDocumentPosition(review) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(review.compareDocumentPosition(parameters) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(review.compareDocumentPosition(files) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByText('03-draft.md')).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run focused task-detail test and verify it fails**

Run:

```bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx
```

Expected: FAIL because `WorkflowStageProgress` is not rendered for completed tasks in the main body.

- [ ] **Step 3: Import and render workflow stages for terminal tasks**

In `studio/src/pages/TaskDetailPage.tsx`, update the workflow import.

```ts
import { WorkflowReviewSummary, WorkflowStageProgress } from '@/components/TaskWorkflowPanel'
```

After `const showProjectParameters = Boolean(snapshot?.platform || project)`, add:

```ts
  const showWorkflowStages = Boolean(task.workflow_status) && task.status !== 'pending' && task.status !== 'running'
```

After the publish-approval cards and before the `Details (stats)` block, add:

```tsx
      {showWorkflowStages && (
        <WorkflowStageProgress workflow={task.workflow_status} />
      )}
```

Keep the review summary immediately after that block.

```tsx
      {task.status === 'completed' && (
        <WorkflowReviewSummary workflow={task.workflow_status} />
      )}
```

Remove the existing later `WorkflowReviewSummary` block that currently appears after the non-completed status card. With this placement, the terminal narrative order is status/actions, approval gate, workflow stages, review, stats, project parameters, files, logs, and credit details.

- [ ] **Step 4: Keep running tasks on compact progress only**

The existing running-task test should still assert these lines.

```tsx
    expect(screen.queryByText('创作进度')).not.toBeInTheDocument()
    expect(screen.queryByText('当前阶段')).not.toBeInTheDocument()
```

If the new render block accidentally displays workflow stages for running tasks, keep the `showWorkflowStages` predicate exactly as:

```ts
  const showWorkflowStages = Boolean(task.workflow_status) && task.status !== 'pending' && task.status !== 'running'
```

- [ ] **Step 5: Run focused task-detail test and commit**

Run:

```bash
cd studio && bun run test -- src/pages/TaskDetailPage.test.tsx
```

Expected: PASS.

Commit:

```bash
git add studio/src/pages/TaskDetailPage.tsx studio/src/pages/TaskDetailPage.test.tsx
git commit -m "feat: show task production narrative"
```

## Task 6: Full Studio Verification

**Files:**
- No source files changed in this task.

- [ ] **Step 1: Run the Operating Loop focused test suite**

Run:

```bash
cd studio && bun run test -- src/lib/command-center.test.ts src/lib/workflow-readiness.test.ts src/pages/DashboardPage.command-center.test.tsx src/components/GlobalCommandPalette.actions.test.tsx src/components/ProjectCard.test.tsx src/components/TaskWorkflowPanel.test.tsx src/pages/TasksPage.test.tsx src/pages/TasksPage.ux.contract.test.ts src/pages/TaskDetailPage.test.tsx
```

Expected: PASS.

- [ ] **Step 2: Run the full Studio test suite**

Run:

```bash
cd studio && bun run test
```

Expected: PASS.

- [ ] **Step 3: Run the Studio production build**

Run:

```bash
cd studio && bun run build
```

Expected: PASS with `tsc -b` completing before the Vite build.

- [ ] **Step 4: Inspect changed files**

Run:

```bash
git status --short
git diff --stat HEAD
```

Expected: only files from this plan are modified, and the diff is limited to Studio operating-loop work.

- [ ] **Step 5: Confirm no extra verification commit is needed**

Run:

```bash
git status --short
```

Expected: no uncommitted source changes remain after the task-level commits. If a verification run exposed a source issue, return to the task that owns that file, apply the smallest fix there, rerun that task's focused test, and use that task's commit command.

## Self-Review

Spec coverage:

- Dashboard next-action center: covered by Tasks 1 and 2.
- Projects as operating units: covered by Task 3.
- Tasks recovery and publishing semantics: covered by Task 4.
- Task Detail production narrative with workflow stages and review: covered by Task 5.
- Settings readiness and credit risk connected to Dashboard/actions: readiness is covered by Tasks 1 and 2; existing credit risk remains in `buildCommandCenterSignals` and is preserved by Task 1 tests.
- Backend posture: no new backend endpoint or service is introduced.
- Plugin distribution parity: no `claudecode/`, `openclaw/`, or `codex/` plugin asset changes are part of this plan.

Plan quality checks:

- Every code task starts with a failing test, runs the focused test, implements the smallest source change, reruns the test, and commits.
- All paths are exact repository paths.
- Type names are consistent across tasks: `ReadinessStatus`, `CommandCenterReadinessCheck`, `ModelConfigLike`, `hasUsableModelConfig`, `parseWorkflowStatus`, `workflowReadinessLabel`, and `readinessValueLabel`.
- The plan avoids broad rewrites and keeps the implementation inside existing Studio patterns.
