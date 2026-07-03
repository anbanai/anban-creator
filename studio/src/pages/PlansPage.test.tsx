import { screen, waitFor, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import PlansPage from './PlansPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

const { errorMock } = vi.hoisted(() => ({ errorMock: vi.fn() }))

vi.mock('sonner', () => ({ toast: { error: errorMock, success: vi.fn() } }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const { mockPlans, mockProjects } =
    await vi.importActual<typeof import('@/test/mocks/handlers')>('@/test/mocks/handlers')
  return {
    ...actual,
    api: {
      ...actual.api,
      plans: {
        ...actual.api.plans,
        list: vi.fn().mockResolvedValue(mockPlans),
        pause: vi.fn(),
        resume: vi.fn(),
        delete: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue(mockProjects),
      },
      credits: {
        ...actual.api.credits,
        pricing: vi.fn().mockResolvedValue({
          task_costs: {},
          model_costs: {},
          income: { daily_sign_in: 0, register_bonus: 0, invite_reward: 0 },
        }),
        balance: vi.fn().mockResolvedValue({ balance: 0 }),
      },
    },
  }
})

describe('PlansPage — mutation failure feedback (no silent failure)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.plans.list).mockResolvedValue({
      items: [{
        id: 'plan-1',
        type: 'article',
        title: '测试计划',
        description: '',
        cron_expr: '0 9 * * 1',
        prompt: '',
        status: 'active',
        next_run_at: '2025-01-20T09:00:00Z',
        project_id: 'ch-1',
        created_at: '2025-01-10T00:00:00Z',
        updated_at: '2025-01-10T00:00:00Z',
      }],
      total: 1,
    })
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'ch-1',
      user_id: '1',
      platform: 'article',
      name: '测试项目',
      avatar_url: '',
      profile_url: 'https://mp.weixin.qq.com/test',
      instructions: '测试定位',
      keywords: '测试',
      visual_style: '',
      writer: '',
      theme: '',
      author: '作者',
      template_id: '',
      reference_image_url: '',
      image_ratio: '16:9',
      max_concurrent_tasks: 2,
      config: { wechat_app_id: 'wx123' },
      status: 'active',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }])
    vi.mocked(api.credits.pricing).mockResolvedValue({
      task_costs: {},
      model_costs: {},
      income: { daily_sign_in: 1024, register_bonus: 4096, invite_reward: 2048 },
    })
    vi.mocked(api.credits.balance).mockResolvedValue({ balance: 0 })
  })

  it('shows an error toast when pausing a plan fails (was previously silent)', async () => {
    // pause is wired through useSubmitLock().submit(mutateAsync) with no catch —
    // before this fix, a rejection surfaced nothing to the user. Now the server's
    // reason (response.data.msg) is surfaced via getApiErrorMessage.
    vi.mocked(api.plans.pause).mockRejectedValueOnce({
      response: { data: { msg: '排期冲突，请检查现有计划' } },
    })

    render(<PlansPage />)

    // mockPlans[0] is an active plan → the 暂停 button renders.
    const pauseBtn = await screen.findByRole('button', { name: '暂停' }, { timeout: 5000 })
    fireEvent.click(pauseBtn)

    await waitFor(() => {
      expect(errorMock).toHaveBeenCalledWith('排期冲突，请检查现有计划')
    })
  })

  it('does not offer e-commerce as a plan content type', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    const [, typeSelect] = screen.getAllByRole('combobox')
    fireEvent.click(typeSelect)

    expect(await screen.findByRole('option', { name: '公众号文章' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: '种草笔记' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '电商出图' })).not.toBeInTheDocument()
  })
})
