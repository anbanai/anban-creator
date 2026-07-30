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
    plan_id: null,
    project_id: 'project-1',
    result: null,
    published: false,
    published_at: null,
    billing_price_credits: 6000,
    created_at: '2026-07-01T00:00:00.000Z',
    started_at: '',
    completed_at: '',
    ...overrides,
    execution_profile: overrides.execution_profile ?? 'cost_effective',
  }
}

describe('studio business UX helpers', () => {
  it('builds direct dashboard blockers in business priority order', () => {
    expect(
      buildDashboardBlocker({
        projectsLoading: false,
        projectsError: false,
        activeProjectCount: 0,
        apiKeysReady: true,
      }),
    ).toMatchObject({
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
    })).toBeNull()
  })

  it('derives task defaults from the selected project', () => {
    expect(
      getProjectCreationDefaults(
        project({
          platform: 'ecommerce',
          image_ratio: '1:1',
          ecommerce_defaults: {
            default_selected_modules: { main_image: 2 },
            target_platform: 'tmall',
            image_model_key: 'gpt-image',
            brand_brief: '极简',
          },
        }),
      ),
    ).toMatchObject({
      type: 'ecommerce',
      imageRatio: '1:1',
      imageModelKey: 'gpt-image',
      selectedModules: { main_image: 2 },
      targetPlatform: 'tmall',
    })
  })

  it('calculates batch creation cost from the immutable task SKU', () => {
    expect(
      taskCreationCostPreview({
        catalog: {
          catalog_id: 'retail-v1',
          currency: 'credits',
          skus: [{
            id: 'task.article.v1',
            operation: 'task.article',
            charge_policy: 'task_admission',
            price_credits: 6000,
            delivery: 'article_artifacts_verified',
          }],
        },
        type: 'article',
        quantity: 2,
        balance: 30000,
      }),
    ).toMatchObject({
      priceAvailable: true,
      baseCost: 6000,
      billableQuantity: 2,
      totalCost: 12000,
      remaining: 18000,
      insufficient: false,
    })
  })

  it('marks task creation pricing unavailable when no active SKU can be resolved', () => {
    expect(taskCreationCostPreview({
      catalog: undefined,
      type: 'article',
      quantity: 1,
      balance: 30000,
    })).toMatchObject({
      priceAvailable: false,
      baseCost: 0,
      totalCost: 0,
      remaining: 30000,
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

    expect(
      buildProjectReadinessSummary(
        project({
          config: { enable_publishing: true, require_publish_approval: true },
          visual_style: '写实',
          writer: '犀利',
          theme: '简洁',
        }),
        stats,
      ),
    ).toEqual({
      tone: 'ready',
      headline: '创作配置已就绪',
      details: ['发布需审核', '写作已配置', '视觉已配置', '成功率 80%'],
    })
  })

  it('summarizes Montage defaults instead of image visual readiness', () => {
    expect(buildProjectReadinessSummary(project({
      platform: 'montage',
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: { duration_seconds: 45 },
      },
    }))).toEqual({
      tone: 'ready',
      headline: '创作配置已就绪',
      details: ['Pipeline social-short', '默认 45 秒'],
    })
  })

  it('labels the primary task action signal', () => {
    expect(taskActionSignal(task({ status: 'failed', error_message: '模型超时' }))).toMatchObject({
      label: '查看失败原因',
      tone: 'risk',
    })
    expect(taskActionSignal(task({ status: 'failed' }))).toMatchObject({
      label: '查看任务状态',
      hint: '未返回失败详情',
      tone: 'risk',
    })
    expect(taskActionSignal(task({ publish_approval_state: 'pending' }))).toMatchObject({
      label: '处理发布审批',
      tone: 'publishing',
    })
  })

  it('builds settings readiness as dependency checklist items', () => {
    expect(
      buildSettingsReadinessItems({
        isDesktopApp: false,
        apiKeyCount: 0,
        hasPassword: false,
      }).map((item) => [item.id, item.ready, item.actionLabel]),
    ).toEqual([
      ['execution', true, '查看执行说明'],
      ['model-key', false, '创建密钥'],
      ['publishing', false, '检查发布'],
      ['account-security', false, '设置密码'],
    ])
  })
})
