import { describe, expect, it } from 'vitest'

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
import type { CreditBalance, Plan, Project, SignInStatus, Task } from '@/types'

function task(overrides: Partial<Task>): Task {
  return {
    id: 'task-1',
    type: 'seednote',
    title: '任务',
    prompt: '写一篇内容',
    status: 'completed',
    progress: 100,
    error_message: null,
    plan_id: null,
    project_id: 'project-1',
    result: { files: null, output: '' },
    published: false,
    published_at: null,
    created_at: '2026-07-06T01:00:00.000Z',
    started_at: '',
    completed_at: '',
    ...overrides,
  }
}

function project(overrides: Partial<Project> = {}): Project {
  return {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'seednote',
    name: '小红书账号',
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

function plan(overrides: Partial<Plan>): Plan {
  return {
    id: 'plan-1',
    type: 'seednote',
    title: '每日选题',
    description: '',
    cron_expr: '0 9 * * *',
    prompt: '每日发布',
    status: 'active',
    next_run_at: '2026-07-06T09:00:00.000Z',
    project_id: 'project-1',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
    ...overrides,
  }
}

describe('command center rules', () => {
  it('aggregates operational signals from existing frontend data', () => {
    const signals = buildCommandCenterSignals({
      now: new Date('2026-07-06T02:00:00.000Z'),
      tasks: [
        task({ id: 'running', status: 'running', progress: 42 }),
        task({ id: 'failed', status: 'failed', error_message: '模型超时' }),
        task({ id: 'approval', publish_approval_state: 'pending' }),
      ],
      plans: [
        plan({ id: 'soon', title: '上午发布', next_run_at: '2026-07-06T08:00:00.000Z' }),
        plan({ id: 'later', title: '下周发布', next_run_at: '2026-07-20T08:00:00.000Z' }),
      ],
      projects: [project()],
      creditsBalance: { balance: 80 } satisfies CreditBalance,
      signInStatus: { signed_in_today: false } satisfies SignInStatus,
      apiKeysReady: true,
      modelConfigReady: true,
      localExecutorReady: true,
    })

    expect(signals.runningTasks).toHaveLength(1)
    expect(signals.failedTasks).toHaveLength(1)
    expect(signals.pendingApprovalTasks).toHaveLength(1)
    expect(signals.upcomingPlans.map((item) => item.id)).toEqual(['soon'])
    expect(signals.creditRisk.level).toBe('low')
    expect(signals.readiness.projectsReady).toBe(true)
  })

  it('prioritizes recovery and approval before general creation actions', () => {
    const signals = buildCommandCenterSignals({
      now: new Date('2026-07-06T02:00:00.000Z'),
      tasks: [
        task({ id: 'failed', status: 'failed', title: '失败任务', error_message: '执行失败' }),
        task({ id: 'approval', title: '待审批任务', publish_approval_state: 'pending' }),
      ],
      plans: [],
      projects: [project()],
      creditsBalance: { balance: 80 },
      signInStatus: { signed_in_today: false },
      apiKeysReady: true,
      modelConfigReady: true,
      localExecutorReady: true,
    })

    expect(buildNextBestActions(signals).map((action) => action.id)).toEqual([
      'recover-failed-task',
      'review-publish-approval',
      'claim-daily-credits',
      'create-task',
    ])
  })

  it('builds first-run actions when no project exists', () => {
    const signals = buildCommandCenterSignals({
      now: new Date('2026-07-06T02:00:00.000Z'),
      tasks: [],
      plans: [],
      projects: [],
      creditsBalance: { balance: 1000 },
      signInStatus: { signed_in_today: true },
      apiKeysReady: true,
      modelConfigReady: true,
      localExecutorReady: true,
    })

    expect(signals.readiness.projectsReady).toBe(false)
    expect(buildNextBestActions(signals)[0]).toMatchObject({
      id: 'create-first-project',
      href: '/projects?return_to=%2Ftasks&create=true&type=seednote',
    })
  })

  it('standardizes task creation and return-intent URLs', () => {
    expect(createTaskHref({ type: 'article', projectId: 'project-1', intent: 'schedule' })).toBe(
      '/tasks?create=true&type=article&project_id=project-1&intent=schedule',
    )
    expect(projectsReturnHref({ type: 'videocreator', intent: 'new' })).toBe(
      '/projects?return_to=%2Ftasks&create=true&type=videocreator&intent=new',
    )
    expect(projectCreatedReturnHref({
      returnTo: '/tasks',
      type: 'videocreator',
      projectId: 'project-1',
      intent: 'new',
    })).toBe('/tasks?create=true&type=videocreator&project_id=project-1&intent=new')
    expect(projectCreatedReturnHref({
      returnTo: '//evil.example/path',
      type: 'article',
      projectId: 'project-1',
    })).toBe('/tasks?create=true&type=article&project_id=project-1')
    expect(projectCreatedReturnHref({
      returnTo: '/tasks-evil?x=1',
      type: 'article',
      projectId: 'project-1',
    })).toBe('/tasks?create=true&type=article&project_id=project-1')
    expect(createTaskHref({ type: 'moments', projectId: 'moments-1', intent: 'new' })).toBe(
      '/tasks?create=true&type=moments&project_id=moments-1&intent=new',
    )
  })

  it('parses create intent params without leaking invalid values into forms', () => {
    expect(parseCreationIntent(new URLSearchParams('create=true&type=article&project_id=project-1&intent=schedule'))).toEqual({
      shouldCreate: true,
      type: 'article',
      projectId: 'project-1',
      intent: 'schedule',
    })
    expect(parseCreationIntent(new URLSearchParams('create=true&type=bad&intent=unknown'))).toEqual({
      shouldCreate: true,
      type: undefined,
      projectId: undefined,
      intent: undefined,
    })
    expect(parseCreationIntent(new URLSearchParams('create=true&type=moments&project_id=moments-1&intent=new'))).toEqual({
      shouldCreate: true,
      type: 'moments',
      projectId: 'moments-1',
      intent: 'new',
    })
  })

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
})
