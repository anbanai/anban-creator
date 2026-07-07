import { screen, waitFor, fireEvent, within } from '@testing-library/react'
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
        create: vi.fn(),
        update: vi.fn(),
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
      video: {
        ...actual.api.video,
        estimate: vi.fn().mockResolvedValue({
          available_models: [{ key: 'seedance-2.0-mini', display_name: 'Seedance Mini' }],
          resolved_config: {
            purpose: 'planting',
            model_key: 'seedance-2.0-mini',
            resolution: '720p',
            ratio: '9:16',
            duration: 10,
          },
          estimated_credits: 2480,
          balance: 200000,
          min_balance: 0,
          meets_min_balance: true,
        }),
      },
    },
  }
})

describe('PlansPage — mutation failure feedback (no silent failure)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.history.pushState({}, '', '/')
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
      recharge_tiers: [
        { key: 'basic', label: '基础包', price_cny: 10, credits: 10000, bonus_credits: 0, enabled: true },
        { key: 'standard', label: '标准包', price_cny: 50, credits: 52000, bonus_credits: 2000, enabled: true },
        { key: 'pro', label: '进阶包', price_cny: 100, credits: 110000, bonus_credits: 10000, enabled: true },
      ],
      income: { daily_sign_in: 100, register_bonus: 1000, invite_reward: 1000 },
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

  it('locks the project selector when editing a video plan because project changes are immutable', async () => {
    vi.mocked(api.plans.list).mockResolvedValue({
      items: [{
        id: 'plan-video-1',
        type: 'video',
        title: '视频计划',
        description: '',
        cron_expr: '0 9 * * 1',
        prompt: '生成新品视频',
        status: 'active',
        next_run_at: '2025-01-20T09:00:00Z',
        project_id: 'video-project-1',
        video_config: {
          purpose: 'planting',
          model_key: 'seedance-2.0-mini',
          resolution: '720p',
          ratio: '9:16',
          duration: 10,
          references: [],
        },
        created_at: '2025-01-10T00:00:00Z',
        updated_at: '2025-01-10T00:00:00Z',
      }],
      total: 1,
    })
    vi.mocked(api.projects.list).mockResolvedValue([{
      id: 'video-project-1',
      user_id: '1',
      platform: 'video',
      name: '视频项目',
      avatar_url: '',
      profile_url: '',
      instructions: '视频定位',
      keywords: '',
      visual_style: '',
      writer: '',
      theme: '',
      author: '',
      template_id: '',
      reference_image_url: '',
      image_ratio: '9:16',
      video_defaults: {
        purpose: 'planting',
        model_key: 'seedance-2.0-mini',
        resolution: '720p',
        ratio: '9:16',
        duration: 10,
        watermark: false,
        preflight: true,
      },
      video_model_policy: {
        allowed_models: ['seedance-2.0-mini'],
        default_model: 'seedance-2.0-mini',
        max_resolution: '720p',
        max_duration: 10,
      },
      max_concurrent_tasks: 2,
      config: {},
      status: 'active',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    }])

    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))

    const [projectSelector] = await screen.findAllByRole('combobox')
    expect(projectSelector).toBeDisabled()
  })

  it('highlights a plan addressed by the timeline highlight parameter', async () => {
    window.history.pushState({}, '', '/plans?highlight=plan-1')

    render(<PlansPage />)

    const highlightedPlan = await screen.findByTestId('plan-card-plan-1')
    expect(highlightedPlan).toHaveAttribute('data-highlighted', 'true')
  })

  it('opens create dialog from URL intent with the project context preselected', async () => {
    window.history.pushState({}, '', '/plans?create=true&type=article&project_id=ch-1&intent=schedule')

    render(<PlansPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(dialog).toBeInTheDocument()
    expect(within(dialog).getByText('测试项目')).toBeInTheDocument()
  })

  it('uses backend-matching fallback pricing for article plans when pricing omits task costs', async () => {
    window.history.pushState({}, '', '/plans?create=true&type=article&project_id=ch-1&intent=schedule')

    render(<PlansPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(await within(dialog).findByText(/每次执行基础任务费：4000 =/)).toBeInTheDocument()
  })
})
