# Studio Restrained UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Studio more restrained by improving business flow and ease of use, while preserving tasteful visual warmth where it helps hierarchy and feedback.

**Architecture:** Add focused frontend helpers for readiness, project defaults, cost preview, and action summaries. Use those helpers in Dashboard, Tasks, Projects, and Settings so pages share business rules without adding backend APIs or global state.

**Tech Stack:** React 19, TypeScript, Vite 8, Bun, TanStack Query, React Router, Tailwind CSS v4, shadcn/Base UI components, Vitest.

---

## File Structure

- Create `studio/src/lib/studio-ux.ts`: shared business-UX helpers for Dashboard blockers, project-derived task defaults, creation cost preview, project readiness summaries, task action signals, and settings readiness items.
- Create `studio/src/lib/studio-ux.test.ts`: table-driven tests for the helper functions.
- Modify `studio/src/pages/DashboardPage.tsx`: use readiness helper to show direct setup blockers while preserving the single AI entry.
- Modify `studio/src/pages/DashboardPage.ai-entry.test.tsx`: add no-project and missing-model readiness tests.
- Modify `studio/src/pages/TasksPage.tsx`: apply project defaults via helper, simplify creation sheet step chrome, show explicit creation blockers, and tune recovery/list action signals.
- Modify `studio/src/pages/TasksPage.ux.contract.test.ts`: protect the quieter creation path and recovery workbench copy.
- Modify `studio/src/components/ProjectCard.tsx`: replace repeated badge stack with concise project readiness summary and a primary create action for active projects.
- Modify `studio/src/pages/ProjectsPage.tsx`: pass the create-task action to `ProjectCard` and keep platform-specific field disclosure.
- Create `studio/src/components/ProjectCard.test.tsx`: verify summary and create action behavior without snapshot testing.
- Modify `studio/src/pages/ProjectsPage.layout.test.ts`: keep existing layout contracts and add platform-specific relevance assertions.
- Modify `studio/src/pages/SettingsPage.tsx`: render readiness as actionable checklist items while retaining detailed sections.
- Modify `studio/src/pages/SettingsPage.ux.contract.test.ts`: assert checklist semantics and dependency impact copy.

## Task 1: Shared Business UX Helpers

**Files:**
- Create: `studio/src/lib/studio-ux.ts`
- Create: `studio/src/lib/studio-ux.test.ts`

- [ ] **Step 1: Write helper tests**

Create `studio/src/lib/studio-ux.test.ts`:

```ts
import { describe, expect, it } from 'vitest'

import {
  buildDashboardBlocker,
  buildProjectReadinessSummary,
  buildSettingsReadinessItems,
  getProjectCreationDefaults,
  taskActionSignal,
  taskCreationCostPreview,
} from './studio-ux'
import type { Project, ProjectStats, Task } from '@/types'

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'article',
    name: '公众号项目',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    reference_image_url: '',
    image_ratio: '',
    max_concurrent_tasks: 1,
    config: {},
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
    ...overrides,
  }
}

function task(overrides: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    type: 'article',
    title: '任务',
    prompt: '写文章',
    status: 'completed',
    progress: 100,
    error: null,
    plan_id: null,
    project_id: 'project-1',
    result: { files: null, output: '' },
    published: false,
    published_at: null,
    created_at: '2026-07-01T00:00:00.000Z',
    started_at: '',
    completed_at: '',
    ...overrides,
  }
}

describe('studio business UX helpers', () => {
  it('builds direct dashboard blockers in business priority order', () => {
    expect(buildDashboardBlocker({
      projectsLoading: false,
      projectsError: false,
      activeProjectCount: 0,
      apiKeysReady: true,
      modelConfigReady: true,
    })).toMatchObject({
      id: 'no-project',
      message: '先创建一个项目，再开始创作。',
      actionLabel: '创建项目',
      actionHref: '/projects?return_to=%2Ftasks&create=true&type=seednote&intent=new',
      blocking: true,
    })

    expect(buildDashboardBlocker({
      projectsLoading: false,
      projectsError: false,
      activeProjectCount: 1,
      apiKeysReady: true,
      modelConfigReady: false,
    })).toMatchObject({
      id: 'model-config',
      actionHref: '/settings#model-key-settings',
      blocking: true,
    })
  })

  it('derives task defaults from the selected project', () => {
    expect(getProjectCreationDefaults(project({
      platform: 'ecommerce',
      image_ratio: '1:1',
      ecommerce_defaults: {
        default_selected_modules: { main_image: 2 },
        target_platform: 'tmall',
        image_model_key: 'gpt-image',
        brand_brief: '极简',
      },
    }))).toMatchObject({
      type: 'ecommerce',
      imageRatio: '1:1',
      imageModelKey: 'gpt-image',
      selectedModules: { main_image: 2 },
      targetPlatform: 'tmall',
    })
  })

  it('calculates creation cost with local execution and goal multiplier', () => {
    expect(taskCreationCostPreview({
      pricing: {
        task_costs: { article: 4000 },
        model_costs: {},
        recharge_tiers: [],
        income: { daily_sign_in: 100, register_bonus: 1000, invite_reward: 1000 },
        agent_runtime_reserve: { article: 500 },
      },
      type: 'article',
      quantity: 2,
      goalMode: true,
      runLocally: false,
      balance: 30000,
    })).toMatchObject({
      baseCost: 4000,
      billableQuantity: 2,
      multiplier: 3,
      totalCost: 24000,
      runtimeReserve: 3000,
      creationTotal: 27000,
      remaining: 3000,
      insufficient: false,
    })
  })

  it('summarizes project readiness without repeating every badge', () => {
    const stats: ProjectStats = {
      total_tasks: 10,
      completed_tasks: 8,
      failed_tasks: 2,
      running_tasks: 0,
      pending_tasks: 0,
      success_rate: 0.8,
      last_activity_at: '2026-07-01T00:00:00.000Z',
    }
    expect(buildProjectReadinessSummary(project({
      config: { enable_publishing: true, require_publish_approval: true },
      visual_style: '写实',
      writer: '犀利',
      theme: '简洁',
    }), stats)).toEqual({
      tone: 'ready',
      headline: '创作配置已就绪',
      details: ['发布需审核', '视觉已配置', '写作已配置', '成功率 80%'],
    })
  })

  it('labels the primary task action signal', () => {
    expect(taskActionSignal(task({ status: 'failed', error: '模型超时' }))).toMatchObject({
      label: '查看失败原因',
      tone: 'risk',
    })
    expect(taskActionSignal(task({ publish_approval_state: 'pending' }))).toMatchObject({
      label: '处理发布审批',
      tone: 'publishing',
    })
  })

  it('builds settings readiness as dependency checklist items', () => {
    expect(buildSettingsReadinessItems({
      isDesktopApp: false,
      apiKeyCount: 0,
      hasPassword: false,
    }).map((item) => [item.id, item.ready, item.actionLabel])).toEqual([
      ['execution', true, '查看执行说明'],
      ['model-key', false, '创建密钥'],
      ['publishing', false, '检查发布'],
      ['account-security', false, '设置密码'],
    ])
  })
})
```

- [ ] **Step 2: Run helper tests to verify they fail**

Run:

```bash
cd studio && bun run test -- src/lib/studio-ux.test.ts
```

Expected: FAIL because `studio/src/lib/studio-ux.ts` does not exist.

- [ ] **Step 3: Implement helpers**

Create `studio/src/lib/studio-ux.ts`:

```ts
import type { CreditPricing } from '@/types/credits'
import type { Project, ProjectStats, Task, TaskType } from '@/types'
import { projectsReturnHref } from '@/lib/command-center'
import { contentTypeLabel, platformDefaultRatio } from '@/lib/labels'
import { agentRuntimeReserveFor, taskCostFor } from '@/lib/pricing'
import { workflowReadinessLabel } from '@/lib/workflow-readiness'

export interface DashboardBlocker {
  id: 'no-project' | 'api-key' | 'model-config'
  message: string
  actionLabel: string
  actionHref: string
  blocking: boolean
}

export function buildDashboardBlocker({
  projectsLoading,
  projectsError,
  activeProjectCount,
  apiKeysReady,
  modelConfigReady,
}: {
  projectsLoading: boolean
  projectsError: boolean
  activeProjectCount: number
  apiKeysReady?: boolean | null
  modelConfigReady?: boolean | null
}): DashboardBlocker | null {
  if (projectsLoading || projectsError) return null
  if (activeProjectCount === 0) {
    return {
      id: 'no-project',
      message: '先创建一个项目，再开始创作。',
      actionLabel: '创建项目',
      actionHref: projectsReturnHref({ type: 'seednote', intent: 'new' }),
      blocking: true,
    }
  }
  if (apiKeysReady === false) {
    return {
      id: 'api-key',
      message: '平台密钥未就绪，先补齐接入能力。',
      actionLabel: '去设置',
      actionHref: '/settings#model-key-settings',
      blocking: true,
    }
  }
  if (modelConfigReady === false) {
    return {
      id: 'model-config',
      message: '模型配置未就绪，先选择可用模型。',
      actionLabel: '去设置',
      actionHref: '/settings#model-key-settings',
      blocking: true,
    }
  }
  return null
}

export interface ProjectCreationDefaults {
  type: TaskType
  imageRatio: string
  imageModelKey: string
  selectedModules: Record<string, number>
  targetPlatform: string
}

export function getProjectCreationDefaults(project?: Project | null): ProjectCreationDefaults {
  const type = (project?.platform || 'seednote') as TaskType
  return {
    type,
    imageRatio: project?.image_ratio || platformDefaultRatio[type] || '',
    imageModelKey: project?.ecommerce_defaults?.image_model_key || '',
    selectedModules: project?.ecommerce_defaults?.default_selected_modules || {},
    targetPlatform: project?.ecommerce_defaults?.target_platform || '',
  }
}

export interface CreationCostPreview {
  baseCost: number
  billableQuantity: number
  multiplier: number
  totalCost: number
  runtimeReserve: number
  creationTotal: number
  remaining: number
  insufficient: boolean
}

export function taskCreationCostPreview({
  pricing,
  type,
  quantity,
  goalMode,
  runLocally,
  balance,
}: {
  pricing?: CreditPricing
  type: string
  quantity: number
  goalMode: boolean
  runLocally: boolean
  balance: number
}): CreationCostPreview {
  const isEcommerce = type === 'ecommerce'
  const baseCost = taskCostFor(pricing, type)
  const billableQuantity = isEcommerce ? 1 : quantity
  const multiplier = isEcommerce ? 1 : goalMode ? 3 : 1
  const totalCost = baseCost * billableQuantity * multiplier
  const runtimeReserve = runLocally ? 0 : agentRuntimeReserveFor(pricing, type) * billableQuantity * multiplier
  const creationTotal = totalCost + runtimeReserve
  const remaining = balance - creationTotal
  return {
    baseCost,
    billableQuantity,
    multiplier,
    totalCost,
    runtimeReserve,
    creationTotal,
    remaining,
    insufficient: remaining < 0,
  }
}

export interface ProjectReadinessSummary {
  tone: 'ready' | 'attention' | 'archived'
  headline: string
  details: string[]
}

export function buildProjectReadinessSummary(project: Project, stats?: ProjectStats): ProjectReadinessSummary {
  if (project.status === 'archived') {
    return { tone: 'archived', headline: '项目已归档', details: ['恢复后可继续创建任务'] }
  }

  const details: string[] = []
  if (project.platform === 'article') {
    details.push(project.config.enable_publishing
      ? project.config.require_publish_approval ? '发布需审核' : '自动入草稿箱'
      : '发布未启用')
    details.push(project.writer || project.theme || project.author ? '写作已配置' : '补写作配置')
  }
  if (project.platform !== 'video') {
    details.push(project.visual_style ? '视觉已配置' : '补视觉配置')
  }
  if (project.platform === 'ecommerce') {
    details.push(project.ecommerce_defaults?.target_platform ? `投放 ${project.ecommerce_defaults.target_platform}` : '补投放平台')
  }
  if (project.platform === 'video') {
    details.push(project.video_defaults?.model_key ? '视频默认已配置' : '补视频模型')
  }
  if (stats && stats.total_tasks > 0) {
    details.push(`成功率 ${(stats.success_rate * 100).toFixed(0)}%`)
  }

  const needsAttention = details.some((item) => item.startsWith('补') || item.includes('未启用'))
  return {
    tone: needsAttention ? 'attention' : 'ready',
    headline: needsAttention ? '还有配置可补齐' : '创作配置已就绪',
    details,
  }
}

export interface TaskActionSignal {
  label: string
  hint: string
  tone: 'risk' | 'publishing' | 'running' | 'success' | 'neutral'
}

export function taskActionSignal(task: Task): TaskActionSignal {
  if (task.status === 'failed') {
    return { label: '查看失败原因', hint: task.error || '进入详情后可重试或克隆', tone: 'risk' }
  }
  if (task.publish_approval_state === 'pending') {
    return { label: '处理发布审批', hint: '审核后放行到公众号草稿箱', tone: 'publishing' }
  }
  if (task.status === 'running' || task.status === 'pending') {
    return { label: task.status === 'running' ? '查看运行进度' : '等待执行', hint: `${task.progress ?? 0}%`, tone: 'running' }
  }
  const readiness = workflowReadinessLabel(task.workflow_status)
  if (task.status === 'completed') {
    return { label: readiness || '查看产物', hint: task.published ? '已标记发布' : '可下载、发布或复用', tone: 'success' }
  }
  return { label: contentTypeLabel[task.type] || '查看任务', hint: '进入详情', tone: 'neutral' }
}

export interface SettingsReadinessListItem {
  id: 'execution' | 'model-key' | 'publishing' | 'account-security'
  title: string
  status: string
  impact: string
  actionLabel: string
  href: string
  ready: boolean
}

export function buildSettingsReadinessItems({
  isDesktopApp,
  apiKeyCount,
  hasPassword,
}: {
  isDesktopApp: boolean
  apiKeyCount: number
  hasPassword: boolean
}): SettingsReadinessListItem[] {
  return [
    {
      id: 'execution',
      title: '执行环境',
      status: isDesktopApp ? '桌面执行器可检查' : '浏览器模式',
      impact: isDesktopApp ? '影响本地 ffmpeg、任务认领和本机运行。' : '当前任务默认走云端执行。',
      actionLabel: isDesktopApp ? '检查执行器' : '查看执行说明',
      href: '#execution-settings',
      ready: true,
    },
    {
      id: 'model-key',
      title: '模型与密钥',
      status: apiKeyCount > 0 ? `${apiKeyCount} 个平台密钥` : '需要创建密钥',
      impact: '影响 Agent 接入、模型调用和生成能力。',
      actionLabel: apiKeyCount > 0 ? '检查模型' : '创建密钥',
      href: '#model-key-settings',
      ready: apiKeyCount > 0,
    },
    {
      id: 'publishing',
      title: '发布渠道',
      status: '需要检查',
      impact: '影响公众号草稿箱、通知和发布审批。',
      actionLabel: '检查发布',
      href: '#publishing-settings',
      ready: false,
    },
    {
      id: 'account-security',
      title: '账号安全',
      status: hasPassword ? '已设置密码' : '需要设置密码',
      impact: '影响账号资料、配额和登录凭证安全。',
      actionLabel: hasPassword ? '查看账号' : '设置密码',
      href: '#account-security-settings',
      ready: hasPassword,
    },
  ]
}
```

- [ ] **Step 4: Run helper tests to verify they pass**

Run:

```bash
cd studio && bun run test -- src/lib/studio-ux.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit helper slice**

```bash
git add studio/src/lib/studio-ux.ts studio/src/lib/studio-ux.test.ts
git commit -m "feat: add studio business ux helpers"
```

## Task 2: Dashboard Readiness Blockers

**Files:**
- Modify: `studio/src/pages/DashboardPage.tsx`
- Modify: `studio/src/pages/DashboardPage.ai-entry.test.tsx`

- [ ] **Step 1: Add Dashboard tests for business blockers**

In `studio/src/pages/DashboardPage.ai-entry.test.tsx`, add tests inside `describe('DashboardPage AI entry', () => { ... })`:

```ts
  it('sends first-time users to project creation from the AI entry', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([])

    render(<DashboardPage />)

    expect(await screen.findByText('先创建一个项目，再开始创作。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '创建项目' })).toHaveAttribute(
      'href',
      '/projects?return_to=%2Ftasks&create=true&type=seednote&intent=new',
    )
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
  })

  it('blocks creation when model configuration is not ready', async () => {
    vi.mocked(api.modelConfig.get).mockResolvedValueOnce({})

    render(<DashboardPage />)

    expect(await screen.findByText('模型配置未就绪，先选择可用模型。')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '去设置' })).toHaveAttribute('href', '/settings#model-key-settings')
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
  })
```

- [ ] **Step 2: Run Dashboard tests to verify they fail**

Run:

```bash
cd studio && bun run test -- src/pages/DashboardPage.ai-entry.test.tsx
```

Expected: FAIL because Dashboard does not fetch model/key readiness and does not use `buildDashboardBlocker()`.

- [ ] **Step 3: Implement Dashboard readiness**

Modify imports in `studio/src/pages/DashboardPage.tsx`:

```ts
import { hasUsableModelConfig, projectsReturnHref } from '@/lib/command-center'
import { buildDashboardBlocker } from '@/lib/studio-ux'
```

Add readiness queries after the local executor query:

```ts
  const { data: apiKeysResponse } = useQuery({
    queryKey: queryKeys.apiKeys.all,
    queryFn: () => api.apiKeys.list(),
    staleTime: 60_000,
  })

  const { data: modelConfig } = useQuery({
    queryKey: queryKeys.modelConfig.all,
    queryFn: () => api.modelConfig.get(),
    staleTime: 60_000,
  })
```

Add blocker calculation near `showProjectBlocker`:

```ts
  const dashboardBlocker = buildDashboardBlocker({
    projectsLoading,
    projectsError,
    activeProjectCount: activeProjects.length,
    apiKeysReady: apiKeysResponse ? (apiKeysResponse.items || []).length > 0 : null,
    modelConfigReady: modelConfig ? hasUsableModelConfig(modelConfig) : null,
  })
```

Replace `canSubmit`:

```ts
  const canSubmit = Boolean(selectedProject) && !dashboardBlocker?.blocking && !submitMutation.isPending && uploading.length === 0
```

Replace the existing `showProjectBlocker` block with:

```tsx
        {dashboardBlocker && !entryError && (
          <div className="mx-auto flex w-full max-w-4xl items-center justify-between gap-3 rounded-lg border border-border bg-background px-4 py-3 text-sm">
            <span className="min-w-0 text-muted-foreground">{dashboardBlocker.message}</span>
            <Link className="shrink-0 font-medium text-primary hover:text-primary/80" to={dashboardBlocker.actionHref}>
              {dashboardBlocker.actionLabel}
            </Link>
          </div>
        )}
```

Keep `projectsReturnHref` import only if another Dashboard path still uses it after the replacement. Remove it when unused.

- [ ] **Step 4: Run Dashboard tests to verify they pass**

Run:

```bash
cd studio && bun run test -- src/pages/DashboardPage.ai-entry.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit Dashboard slice**

```bash
git add studio/src/pages/DashboardPage.tsx studio/src/pages/DashboardPage.ai-entry.test.tsx
git commit -m "feat: surface dashboard readiness blockers"
```

## Task 3: Task Creation Defaults And Blockers

**Files:**
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/TasksPage.ux.contract.test.ts`

- [ ] **Step 1: Update task creation contract test**

In `studio/src/pages/TasksPage.ux.contract.test.ts`, update the compact sheet test to require the quiet structure and project defaults:

```ts
  it('uses a project-aware compact sheet for task creation', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain('SheetContent')
    expect(source).toContain('任务创建路径')
    expect(source).toContain('类型')
    expect(source).toContain('项目')
    expect(source).toContain('目标/提示词')
    expect(source).toContain('图片/高级')
    expect(source).toContain('基础任务费预估')
    expect(source).toContain('getProjectCreationDefaults')
    expect(source).toContain('taskCreationCostPreview')
    expect(source).toContain('creationBlocker')
  })
```

- [ ] **Step 2: Run task contract test to verify it fails**

Run:

```bash
cd studio && bun run test -- src/pages/TasksPage.ux.contract.test.ts
```

Expected: FAIL because `TasksPage.tsx` does not yet use the new helpers or `creationBlocker`.

- [ ] **Step 3: Use project defaults helper**

Modify imports in `studio/src/pages/TasksPage.tsx`:

```ts
import { getProjectCreationDefaults, taskActionSignal, taskCreationCostPreview } from '@/lib/studio-ux'
```

In `openCreate()`, replace the repeated default derivation with:

```ts
    const selectedIntentProject = createIntent.projectId ? projectMap[createIntent.projectId] : undefined
    const defaults = getProjectCreationDefaults(selectedIntentProject)
    form.reset({
      type: createIntent.type ?? defaults.type,
      prompt: '',
      project_id: selectedIntentProject?.id ?? '',
      image_ratio: defaults.imageRatio as CreateTaskFormValues['image_ratio'],
      image_model_key: defaults.imageModelKey,
      product_photos: [],
      selected_modules: defaults.selectedModules,
      target_platform: defaults.targetPlatform,
      selling_points: '',
      language: '',
      video_input: defaults.type === 'video' ? initialVideoInput('') : undefined,
    })
    setProjectImageRatio(defaults.imageRatio)
```

Inside `ProjectSelector` `onChange`, replace manual default blocks with:

```ts
                          const ch = projects.find((c) => c.id === id)
                          const defaults = getProjectCreationDefaults(ch)
                          setProjectImageRatio(defaults.imageRatio)
                          form.setValue('type', defaults.type)
                          form.setValue('image_ratio', defaults.imageRatio as CreateTaskFormValues['image_ratio'], { shouldDirty: false })
                          form.setValue('selected_modules', defaults.selectedModules, { shouldDirty: false })
                          form.setValue('target_platform', defaults.targetPlatform, { shouldDirty: false })
                          form.setValue('image_model_key', defaults.imageModelKey, { shouldDirty: false })
                          form.setValue('video_input', defaults.type === 'video' ? initialVideoInput(form.getValues('prompt') || '') : undefined, { shouldDirty: false })
```

- [ ] **Step 4: Use cost preview and creation blocker**

Before `return (` in `TasksPage`, add:

```ts
  const costPreview = taskCreationCostPreview({
    pricing,
    type: watchedType,
    quantity,
    goalMode,
    runLocally,
    balance: creditsBalance?.balance ?? 0,
  })

  const creationBlocker =
    costPreview.insufficient
      ? { message: '积分不足，补充积分后再创建。', href: '/credits', actionLabel: '查看积分' }
      : watchedType !== 'ecommerce' && goalMode && !goalText.trim()
        ? { message: '强目标模式需要填写目标条件。', href: '', actionLabel: '' }
        : watchedType === 'ecommerce' && (!watchedProductPhotos || watchedProductPhotos.length === 0)
          ? { message: '电商出图需要先上传产品图。', href: '', actionLabel: '' }
          : null
```

In the cost display IIFE, replace local calculations with `costPreview` values:

```tsx
                  <div className="space-y-1 rounded-md border border-border bg-muted/50 p-3 text-sm">
                    <p className="text-muted-foreground">
                      基础任务费：{costPreview.baseCost} × {costPreview.billableQuantity}
                      {costPreview.multiplier > 1 && ` × ${costPreview.multiplier}`} = <span className="font-medium text-foreground">{costPreview.totalCost}</span> 积分
                      {costPreview.multiplier > 1 && <span className="ml-1 text-xs text-amber-600">（含目标重试）</span>}
                    </p>
                    {runLocally ? (
                      <p className="text-xs text-muted-foreground">本机运行使用你的 Claude Code 环境，不收平台 Claude Code 运行预留。</p>
                    ) : (
                      <p className="text-muted-foreground">
                        Claude Code 运行预留：<span className="font-medium text-foreground">{costPreview.runtimeReserve.toLocaleString()}</span> 积分
                        <span className="ml-1 text-xs text-muted-foreground">完成后按实际 token 多退少补</span>
                      </p>
                    )}
                    <p className="text-muted-foreground">
                      余额：{(creditsBalance?.balance ?? 0).toLocaleString()} →{' '}
                      <span className={`font-medium ${costPreview.remaining < 0 ? 'text-red-500' : 'text-foreground'}`}>
                        {costPreview.remaining.toLocaleString()}
                      </span>
                    </p>
                    {creationBlocker && (
                      <p className="text-sm font-medium text-red-500">
                        {creationBlocker.href ? <Link to={creationBlocker.href}>{creationBlocker.message}</Link> : creationBlocker.message}
                      </p>
                    )}
                  </div>
```

Replace the submit button disabled calculation with:

```tsx
              disabled={Boolean(creationBlocker)}
```

- [ ] **Step 5: Quiet the visible step chrome without removing path semantics**

Keep `SheetDescription` unchanged. Replace the five-column pill grid with a single restrained line:

```tsx
            <p className="pt-2 text-[11px] text-muted-foreground">
              类型 / 项目 / 目标/提示词 / 图片/高级 / 基础任务费预估
            </p>
```

Keep the section labels `01 类型`, `02 项目`, `03 目标/提示词`, `04 图片/高级`, and `05 基础任务费预估` for scanability.

- [ ] **Step 6: Run task contract test**

Run:

```bash
cd studio && bun run test -- src/pages/TasksPage.ux.contract.test.ts src/lib/studio-ux.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit task creation slice**

```bash
git add studio/src/pages/TasksPage.tsx studio/src/pages/TasksPage.ux.contract.test.ts
git commit -m "feat: streamline task creation defaults"
```

## Task 4: Task Recovery And List Action Signals

**Files:**
- Modify: `studio/src/pages/TasksPage.tsx`
- Modify: `studio/src/pages/TasksPage.ux.contract.test.ts`

- [ ] **Step 1: Extend recovery contract test**

In `studio/src/pages/TasksPage.ux.contract.test.ts`, add:

```ts
  it('keeps task rows focused on the primary business action', () => {
    const source = readFileSync(join(here, 'TasksPage.tsx'), 'utf8')

    expect(source).toContain('taskActionSignal')
    expect(source).toContain('处理发布审批')
    expect(source).toContain('查看失败原因')
    expect(source).toContain('可下载、发布或复用')
  })
```

- [ ] **Step 2: Run task contract test to verify it fails**

Run:

```bash
cd studio && bun run test -- src/pages/TasksPage.ux.contract.test.ts
```

Expected: FAIL until the row uses `taskActionSignal`.

- [ ] **Step 3: Use action signal in task rows**

Inside `filteredTasks.map((task) => { ... })`, add:

```ts
            const actionSignal = taskActionSignal(task)
```

Replace the status chip cluster after the status badge with a single action signal:

```tsx
                          <Badge
                            variant={actionSignal.tone === 'risk' ? 'destructive' : actionSignal.tone === 'success' ? 'secondary' : 'outline'}
                            className="text-[10px]"
                          >
                            {actionSignal.label}
                          </Badge>
```

Move local execution, approval rejected, workflow readiness, and published state to the secondary metadata row:

```tsx
                        <span>{actionSignal.hint}</span>
                        {(task.execution_target === 'local' || task.execution_target === 'local_claimed') && (
                          <span>本地{task.execution_target === 'local' ? '待认领' : '运行中'}</span>
                        )}
                        {task.publish_approval_state === 'rejected' && <span>已驳回发布</span>}
```

Keep the `标记发布` button for completed tasks, but place it after the metadata row so it does not compete with the title.

- [ ] **Step 4: Make recovery links more action-specific**

Update `TaskQueueLink` descriptions in `TasksPage.tsx`:

```tsx
          <TaskQueueLink
            to="/tasks?status=running"
            icon={Clock3}
            label="运行队列"
            value={queueStats.active}
            description="查看正在推进或等待执行的任务"
          />
          <TaskQueueLink
            to="/tasks?status=failed"
            icon={AlertTriangle}
            label="失败待恢复"
            value={queueStats.failed}
            description="进入详情查看失败原因，重试或克隆"
            urgent={queueStats.failed > 0}
          />
          <TaskQueueLink
            to="/tasks?status=completed"
            icon={Send}
            label="待发布确认"
            value={queueStats.approval}
            description="审核后放行到公众号草稿箱"
          />
          <TaskQueueLink
            to="/tasks?status=completed"
            icon={CheckCircle2}
            label="最近完成"
            value={queueStats.completed}
            description="下载、发布标记或复用配置"
          />
```

- [ ] **Step 5: Run recovery contract test**

Run:

```bash
cd studio && bun run test -- src/pages/TasksPage.ux.contract.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit task recovery slice**

```bash
git add studio/src/pages/TasksPage.tsx studio/src/pages/TasksPage.ux.contract.test.ts
git commit -m "feat: clarify task recovery actions"
```

## Task 5: Project Operating Cards

**Files:**
- Modify: `studio/src/components/ProjectCard.tsx`
- Modify: `studio/src/pages/ProjectsPage.tsx`
- Create: `studio/src/components/ProjectCard.test.tsx`
- Modify: `studio/src/pages/ProjectsPage.layout.test.ts`

- [ ] **Step 1: Write ProjectCard behavior test**

Create `studio/src/components/ProjectCard.test.tsx`:

```tsx
import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ProjectCard } from './ProjectCard'
import { render } from '@/test/test-utils'
import type { Project, ProjectStats } from '@/types'

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'article',
    name: '公众号项目',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '写实',
    writer: '犀利',
    theme: '简洁',
    author: 'Anban',
    template_id: '',
    reference_image_url: '',
    image_ratio: '',
    max_concurrent_tasks: 1,
    config: { enable_publishing: true, require_publish_approval: true },
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
    ...overrides,
  }
}

describe('ProjectCard', () => {
  it('shows a compact readiness summary and primary create action', () => {
    const onCreateTask = vi.fn()
    const stats: ProjectStats = {
      total_tasks: 10,
      completed_tasks: 8,
      failed_tasks: 2,
      running_tasks: 0,
      pending_tasks: 0,
      success_rate: 0.8,
      last_activity_at: '2026-07-01T00:00:00.000Z',
    }

    render(<ProjectCard project={project()} stats={stats} onCreateTask={onCreateTask} />)

    expect(screen.getByText('创作配置已就绪')).toBeInTheDocument()
    expect(screen.getByText('发布需审核')).toBeInTheDocument()
    expect(screen.getByText('视觉已配置')).toBeInTheDocument()
    expect(screen.getByText('写作已配置')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '用此项目创建任务' }))
    expect(onCreateTask).toHaveBeenCalledWith(expect.objectContaining({ id: 'project-1' }))
  })
})
```

- [ ] **Step 2: Run ProjectCard test to verify it fails**

Run:

```bash
cd studio && bun run test -- src/components/ProjectCard.test.tsx
```

Expected: FAIL because `onCreateTask` and readiness summary are not implemented.

- [ ] **Step 3: Implement ProjectCard summary and create action**

In `studio/src/components/ProjectCard.tsx`, replace `buildOperatingBadges` with `buildProjectReadinessSummary` import:

```ts
import { buildProjectReadinessSummary } from '@/lib/studio-ux'
```

Extend props:

```ts
  onCreateTask?: (project: Project) => void
```

Inside component:

```ts
  const readiness = buildProjectReadinessSummary(project, stats)
```

Replace the `operatingBadges` rendering with:

```tsx
      <div className="mt-3 rounded-md border border-border bg-muted/30 px-3 py-2">
        <p className="text-xs font-medium text-foreground">{readiness.headline}</p>
        <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {readiness.details.map((detail) => (
            <span key={detail}>{detail}</span>
          ))}
        </div>
      </div>
```

At the start of the action row:

```tsx
        {project.status === 'active' && onCreateTask && (
          <Button variant="secondary" size="xs" onClick={() => onCreateTask(project)} aria-label="用此项目创建任务">
            创建任务
          </Button>
        )}
```

- [ ] **Step 4: Wire ProjectsPage create action**

In `studio/src/pages/ProjectsPage.tsx`, add import:

```ts
import { createTaskHref, parseCreationIntent, projectCreatedReturnHref } from '@/lib/command-center'
```

Pass prop to `ProjectCard`:

```tsx
              onCreateTask={(project) => navigate(createTaskHref({
                type: project.platform,
                projectId: project.id,
                intent: 'new',
              }))}
```

- [ ] **Step 5: Add layout contract assertions**

In `studio/src/pages/ProjectsPage.layout.test.ts`, add:

```ts
  it('keeps project cards focused on creation readiness', () => {
    const cardSource = readFileSync(join(here, '../components/ProjectCard.tsx'), 'utf8')
    const pageSource = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(cardSource).toContain('buildProjectReadinessSummary')
    expect(cardSource).toContain('用此项目创建任务')
    expect(pageSource).toContain('createTaskHref')
  })
```

- [ ] **Step 6: Run Project tests**

Run:

```bash
cd studio && bun run test -- src/components/ProjectCard.test.tsx src/pages/ProjectsPage.layout.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit project slice**

```bash
git add studio/src/components/ProjectCard.tsx studio/src/components/ProjectCard.test.tsx studio/src/pages/ProjectsPage.tsx studio/src/pages/ProjectsPage.layout.test.ts
git commit -m "feat: summarize project creation readiness"
```

## Task 6: Settings Readiness Checklist

**Files:**
- Modify: `studio/src/pages/SettingsPage.tsx`
- Modify: `studio/src/pages/SettingsPage.ux.contract.test.ts`

- [ ] **Step 1: Update Settings contract test**

In `studio/src/pages/SettingsPage.ux.contract.test.ts`, replace the test body with:

```ts
    expect(source).toContain('接入就绪中心')
    expect(source).toContain('执行环境')
    expect(source).toContain('模型与密钥')
    expect(source).toContain('发布渠道')
    expect(source).toContain('账号安全')
    expect(source).toContain('SettingsReadinessItem')
    expect(source).toContain('buildSettingsReadinessItems')
    expect(source).toContain('impact')
    expect(source).toContain('actionLabel')
```

- [ ] **Step 2: Run Settings contract test to verify it fails**

Run:

```bash
cd studio && bun run test -- src/pages/SettingsPage.ux.contract.test.ts
```

Expected: FAIL because Settings does not yet use `buildSettingsReadinessItems`.

- [ ] **Step 3: Render checklist from helper**

In `studio/src/pages/SettingsPage.tsx`, import helper:

```ts
import { buildSettingsReadinessItems, type SettingsReadinessListItem } from '@/lib/studio-ux'
```

Inside `SettingsPage`, before `return`:

```ts
  const readinessItems = buildSettingsReadinessItems({
    isDesktopApp: isDesktop(),
    apiKeyCount: apiKeys.length,
    hasPassword: Boolean(user?.has_password),
  })
```

Replace the current four hard-coded `SettingsReadinessItem` grid children with:

```tsx
            {readinessItems.map((item) => (
              <SettingsReadinessItem key={item.id} item={item} />
            ))}
```

Replace `SettingsReadinessItem` props with one typed item:

```tsx
function SettingsReadinessItem({ item }: { item: SettingsReadinessListItem }) {
  return (
    <a
      href={item.href}
      className="flex items-start gap-3 rounded-lg border border-border bg-background p-3 transition-colors hover:border-primary/30 hover:bg-accent"
    >
      <span className={`mt-0.5 flex size-8 items-center justify-center rounded-lg ${item.ready ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}>
        {item.ready ? <CheckCircle2 className="size-4" /> : <AlertCircle className="size-4" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center justify-between gap-2">
          <span className="truncate text-sm font-medium text-foreground">{item.title}</span>
          <span className="shrink-0 text-[11px] font-medium text-primary">{item.actionLabel}</span>
        </span>
        <span className="mt-1 block text-xs font-medium text-foreground">{item.status}</span>
        <span className="mt-0.5 block line-clamp-2 text-xs text-muted-foreground">{item.impact}</span>
      </span>
    </a>
  )
}
```

Update lucide import:

```ts
import { AlertCircle, CheckCircle2, Loader2 } from 'lucide-react'
```

Remove unused `RadioTower`, `ShieldCheck`, `Terminal`, `WandSparkles`, and `type LucideIcon` imports.

- [ ] **Step 4: Run Settings contract test**

Run:

```bash
cd studio && bun run test -- src/pages/SettingsPage.ux.contract.test.ts src/lib/studio-ux.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit Settings slice**

```bash
git add studio/src/pages/SettingsPage.tsx studio/src/pages/SettingsPage.ux.contract.test.ts
git commit -m "feat: make settings readiness actionable"
```

## Task 7: Full Studio Verification

**Files:**
- No source edits expected.

- [ ] **Step 1: Run focused UX tests**

Run:

```bash
cd studio && bun run test -- src/lib/studio-ux.test.ts src/pages/DashboardPage.ai-entry.test.tsx src/pages/TasksPage.ux.contract.test.ts src/components/ProjectCard.test.tsx src/pages/ProjectsPage.layout.test.ts src/pages/SettingsPage.ux.contract.test.ts
```

Expected: PASS.

- [ ] **Step 2: Run full Studio tests**

Run:

```bash
cd studio && bun run test
```

Expected: PASS.

- [ ] **Step 3: Build Studio**

Run:

```bash
cd studio && bun run build
```

Expected: PASS with Vite build output and no TypeScript errors.

- [ ] **Step 4: Manual browser verification**

Start dev server:

```bash
cd studio && bun run dev -- --host 127.0.0.1
```

Inspect in browser:

- `/`: Dashboard still centers the AI entry, shows no passive dashboard panels, and shows a direct blocker when no project or model config is missing.
- `/tasks`: recovery workbench appears before list, creation sheet keeps path semantics, project selection pre-fills type/defaults, and cost blocker disables submit with visible reason.
- `/projects`: cards show concise readiness summary and active projects expose "创建任务"; project edit dialog keeps platform-relevant fields.
- `/settings`: readiness checklist shows status, impact, and shortest action; detailed management sections remain available below.

Expected: no clipped text, no mobile overflow at 390px width, no console errors.

- [ ] **Step 5: Commit verification-only adjustments if browser QA finds fixes**

Only run this if Step 4 required source changes:

```bash
git add studio/src
git commit -m "fix: polish restrained studio ux verification"
```

Expected: commit contains only fixes discovered during browser verification.

## Self-Review Notes

Spec coverage:

- Dashboard first-run and readiness blockers: Task 2.
- Project-aware task defaults and relevant-only controls: Task 1 and Task 3.
- Task recovery, publish approval, and row action clarity: Task 4.
- Project readiness and platform-specific configuration: Task 5.
- Settings as actionable readiness checklist: Task 6.
- Shared business UI helpers and tests: Task 1.
- Verification and build gates: Task 7.

Execution should preserve existing visual warmth and business capabilities while reducing decision cost and repeated status expression.
