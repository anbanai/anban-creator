import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import DashboardPage from './DashboardPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({
    user: {
      nickname: '测试用户',
      invite_code: 'INVITE',
      invite_count: 0,
      max_invites: 3,
    },
  }),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

vi.mock('@/lib/tauri', () => ({
  isDesktop: () => true,
  getLocalExecutorStatus: vi.fn().mockResolvedValue({
    state: 'running_idle',
    available: true,
    running: true,
    reason: '',
  }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      credits: {
        ...actual.api.credits,
        balance: vi.fn().mockResolvedValue({ balance: 80 }),
        signInStatus: vi.fn().mockResolvedValue({ signed_in_today: false }),
        pricing: vi.fn().mockResolvedValue({
          task_costs: {},
          model_costs: {},
          recharge_tiers: [
            { key: 'basic', label: '基础包', price_cny: 10, credits: 10000, bonus_credits: 0, enabled: true },
            { key: 'standard', label: '标准包', price_cny: 50, credits: 52000, bonus_credits: 2000, enabled: true },
            { key: 'pro', label: '进阶包', price_cny: 100, credits: 110000, bonus_credits: 10000, enabled: true },
          ],
          income: { daily_sign_in: 100, register_bonus: 1000, invite_reward: 1000 },
        }),
        signIn: vi.fn().mockResolvedValue({ balance: 180 }),
      },
      plans: {
        ...actual.api.plans,
        list: vi.fn().mockResolvedValue({
          items: [{
            id: 'plan-1',
            type: 'article',
            title: '上午发布',
            description: '',
            cron_expr: '0 9 * * *',
            prompt: '每日内容',
            status: 'active',
            next_run_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
            project_id: 'project-1',
            created_at: '2026-07-01T00:00:00.000Z',
            updated_at: '2026-07-01T00:00:00.000Z',
          }],
          total: 1,
        }),
      },
      tasks: {
        ...actual.api.tasks,
        list: vi.fn().mockResolvedValue({
          items: [
            {
              id: 'failed-task',
              type: 'article',
              title: '失败文章',
              prompt: '失败任务',
              status: 'failed',
              progress: 0,
              error: '模型超时',
              plan_id: null,
              project_id: 'project-1',
              result: { files: null, output: '' },
              published: false,
              published_at: null,
              created_at: new Date().toISOString(),
              started_at: '',
              completed_at: '',
            },
            {
              id: 'approval-task',
              type: 'article',
              title: '待审批文章',
              prompt: '审批任务',
              status: 'completed',
              progress: 100,
              error: null,
              plan_id: null,
              project_id: 'project-1',
              result: { files: null, output: '' },
              published: false,
              published_at: null,
              publish_approval_state: 'pending',
              created_at: new Date().toISOString(),
              started_at: '',
              completed_at: '',
            },
          ],
          total: 2,
        }),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue([{
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
          config: { enable_publishing: true },
          status: 'active',
          created_at: '2026-07-01T00:00:00.000Z',
          updated_at: '2026-07-01T00:00:00.000Z',
        }]),
      },
      apiKeys: {
        ...actual.api.apiKeys,
        list: vi.fn().mockResolvedValue({ items: [{ id: 'key-1' }] }),
      },
      modelConfig: {
        ...actual.api.modelConfig,
        get: vi.fn().mockResolvedValue({ text: { model: 'gpt-5' }, image: null }),
      },
    },
  }
})

describe('DashboardPage command center', () => {
  it('surfaces operational signals and next best actions', async () => {
    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '今日指挥中心' })).toBeInTheDocument()
    expect(await screen.findByText('今日创作态势')).toBeInTheDocument()
    expect(await screen.findByText('失败待恢复')).toBeInTheDocument()
    expect(await screen.findByText('待发布确认')).toBeInTheDocument()
    expect(await screen.findByText('平台密钥')).toBeInTheDocument()
    expect(await screen.findByText('密钥可用于 Agent 接入')).toBeInTheDocument()
    expect(await screen.findByText('模型配置')).toBeInTheDocument()
    expect(await screen.findByText('模型策略已配置')).toBeInTheDocument()
    expect(await screen.findByText('执行环境')).toBeInTheDocument()
    expect(await screen.findByText('执行环境可用')).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: /恢复失败任务/ })).toHaveAttribute('href', '/tasks/failed-task')
    expect(await screen.findByRole('link', { name: /处理发布审批/ })).toHaveAttribute('href', '/tasks/approval-task')
    expect(api.projects.list).toHaveBeenCalledWith({ status: 'active' })
    expect(api.apiKeys.list).toHaveBeenCalled()
    expect(api.modelConfig.get).toHaveBeenCalled()
  })
})
