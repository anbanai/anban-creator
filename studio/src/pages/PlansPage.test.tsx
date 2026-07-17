import { act, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import PlansPage from './PlansPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import type { ReferenceMaterialInputProps } from '@/components/ReferenceMaterialInput'
import type { InputAttachment, Plan, Project } from '@/types'

const { errorMock } = vi.hoisted(() => ({ errorMock: vi.fn() }))

vi.mock('sonner', () => ({ toast: { error: errorMock, success: vi.fn() } }))

const referenceMaterialInputHarness = vi.hoisted(() => ({
  props: undefined as ReferenceMaterialInputProps | undefined,
}))

vi.mock('@/components/ReferenceMaterialInput', () => ({
  ReferenceMaterialInput: (props: ReferenceMaterialInputProps) => {
    referenceMaterialInputHarness.props = props
    return <div />
  },
}))

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
      videoCreator: {
        ...actual.api.videoCreator,
        estimate: vi.fn().mockResolvedValue({
          available_models: [{ key: 'seedance-2.0-mini', display_name: 'Seedance Mini' }],
          resolved_creator_config: {
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
      agent_runtime_reserve: { article: 4000 },
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
        type: 'videocreator',
        title: '视频计划',
        description: '',
        cron_expr: '0 9 * * 1',
        prompt: '生成新品视频',
        status: 'active',
        next_run_at: '2025-01-20T09:00:00Z',
        project_id: 'video-project-1',
        video_creator_config: {
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
      platform: 'videocreator',
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
    vi.mocked(api.credits.balance).mockResolvedValueOnce({ balance: 5000 })
    window.history.pushState({}, '', '/plans?create=true&type=article&project_id=ch-1&intent=schedule')

    render(<PlansPage />)

    const dialog = await screen.findByRole('dialog', { name: '新建计划' })
    expect(await within(dialog).findByText(/每次执行基础任务费：4000 =/)).toBeInTheDocument()
    expect(within(dialog).getByText(/余额：5,000 →/)).toBeInTheDocument()
    expect(within(dialog).getByText('1,000')).toBeInTheDocument()
    expect(within(dialog).queryByText(/运行预留/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText(/积分不足/)).not.toBeInTheDocument()
  })
})


describe('PlansPage Seednote reference snapshots', () => {
  const savedAttachment: InputAttachment = {
    type: 'image',
    url: '/saved-product.png',
    file_name: 'saved-product.png',
    content_type: 'image/png',
    upload_id: 'upload-saved',
    key: 'uploads/saved-product.png',
    instruction: '保留包装、Logo 和瓶身比例',
  }
  const replacementAttachment: InputAttachment = {
    type: 'image',
    url: '/replacement.png',
    file_name: 'replacement.png',
    content_type: 'image/png',
    upload_id: 'upload-replacement',
    key: 'uploads/replacement.png',
    instruction: '使用新版包装',
  }
  const seednoteProject = {
    id: 'seednote-project-1',
    user_id: '1',
    platform: 'seednote',
    name: '种草项目',
    avatar_url: '',
    profile_url: '',
    instructions: '面向敏感肌用户',
    keywords: '护肤',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    reference_image_url: '',
    image_ratio: '3:4',
    max_concurrent_tasks: 2,
    config: {},
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  } as Project
  const seednotePlan = {
    id: 'seednote-plan-1',
    type: 'seednote',
    title: '每日种草计划',
    description: '',
    cron_expr: '0 9 * * 1,3,5',
    prompt: '围绕敏感肌保湿创作',
    status: 'active',
    next_run_at: '2025-01-20T09:00:00Z',
    project_id: seednoteProject.id,
    input_attachments: [savedAttachment],
    created_at: '2025-01-10T00:00:00Z',
    updated_at: '2025-01-10T00:00:00Z',
  } as Plan

  beforeEach(() => {
    vi.clearAllMocks()
    window.history.pushState({}, '', '/')
    referenceMaterialInputHarness.props = undefined
    vi.mocked(api.projects.list).mockResolvedValue([seednoteProject])
    vi.mocked(api.plans.list).mockResolvedValue({ items: [seednotePlan], total: 1 })
    vi.mocked(api.plans.create).mockResolvedValue(seednotePlan)
    vi.mocked(api.plans.update).mockResolvedValue(seednotePlan)
    vi.mocked(api.credits.balance).mockResolvedValue({ balance: 10000 })
  })

  it('creates a Seednote plan with the current reference snapshot', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    expect(await screen.findByRole('dialog', { name: '新建计划' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props).toBeDefined())
    expect(screen.getByRole('region', { name: 'Seednote 参考素材' })).toBeInTheDocument()
    expect(referenceMaterialInputHarness.props).toEqual(expect.objectContaining({
      allowedTypes: ['image'],
      maxCount: 16,
      instructionEnabled: true,
      instructionMaxLength: 1000,
    }))

    act(() => {
      referenceMaterialInputHarness.props?.onChange([savedAttachment])
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => {
      expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
        type: 'seednote',
        input_attachments: [savedAttachment],
      }))
    })
  })

  it('hydrates edit snapshots and preserves omit, clear, and replace update semantics', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    expect(await screen.findByRole('dialog', { name: '编辑计划' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props?.value).toEqual([savedAttachment]))

    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledTimes(1))
    expect(vi.mocked(api.plans.update).mock.calls[0][1]).not.toHaveProperty('input_attachments')

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    await waitFor(() => expect(referenceMaterialInputHarness.props?.value).toEqual([savedAttachment]))
    act(() => {
      referenceMaterialInputHarness.props?.onChange([])
    })
    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledTimes(2))
    expect(vi.mocked(api.plans.update).mock.calls[1][1]).toEqual(expect.objectContaining({
      input_attachments: [],
    }))

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    await waitFor(() => expect(referenceMaterialInputHarness.props?.value).toEqual([savedAttachment]))
    act(() => {
      referenceMaterialInputHarness.props?.onChange([replacementAttachment])
    })
    fireEvent.click(screen.getByRole('button', { name: '更新' }))
    await waitFor(() => expect(api.plans.update).toHaveBeenCalledTimes(3))
    expect(vi.mocked(api.plans.update).mock.calls[2][1]).toEqual(expect.objectContaining({
      input_attachments: [replacementAttachment],
    }))
  })

  it('disables plan save while reference images are uploading', async () => {
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '新建计划' }))
    expect(await screen.findByRole('dialog', { name: '新建计划' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props).toBeDefined())

    act(() => {
      referenceMaterialInputHarness.props?.onUploadingChange?.(true)
    })

    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
  })
})

describe('PlansPage Montage input', () => {
  const montageProject = {
    id: 'project-montage',
    user_id: '1',
    platform: 'montage',
    name: 'Montage 项目',
    avatar_url: '',
    profile_url: '',
    instructions: '新品短视频',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',
    template_id: '',
    reference_image_url: '',
    image_ratio: '16:9',
    montage_defaults: {
      default_pipeline: 'project-pipeline',
      preferences: {
        aspect_ratio: '16:9',
        duration_seconds: 45,
        style: 'project style',
        music_prompt: 'project music',
        subtitle_mode: 'burned-in',
        voiceover_mode: 'narrated',
      },
      asset_guidance: '优先使用实拍素材',
      delivery_targets: ['final_video'],
    },
    max_concurrent_tasks: 2,
    config: {},
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
  } as Project
  const savedMontagePlan = {
    id: 'montage-plan-1',
    type: 'montage',
    title: 'Montage 周计划',
    description: '',
    cron_expr: '0 9 * * 1',
    prompt: '',
    status: 'active',
    next_run_at: '2025-01-20T09:00:00Z',
    project_id: montageProject.id,
    montage_input: {
      brief: '保存的 brief',
      pipeline_key: 'saved-pipeline',
      source_assets: [],
      preferences: { aspect_ratio: '9:16', duration_seconds: 12 },
      delivery_targets: [],
    },
    created_at: '2025-01-10T00:00:00Z',
    updated_at: '2025-01-10T00:00:00Z',
  } as Plan

  beforeEach(() => {
    vi.clearAllMocks()
    window.history.pushState({}, '', '/')
    referenceMaterialInputHarness.props = undefined
    vi.mocked(api.projects.list).mockResolvedValue([montageProject])
    vi.mocked(api.plans.list).mockResolvedValue({ items: [], total: 0 })
    vi.mocked(api.plans.create).mockResolvedValue(savedMontagePlan)
    vi.mocked(api.plans.update).mockResolvedValue(savedMontagePlan)
    vi.mocked(api.credits.balance).mockResolvedValue({ balance: 10000 })
  })

  it('inherits project defaults and creates a plan with complete Montage input', async () => {
    window.history.pushState({}, '', `/plans?create=true&type=montage&project_id=${montageProject.id}&intent=schedule`)
    render(<PlansPage />)

    expect(await screen.findByRole('dialog', { name: '新建计划' })).toBeInTheDocument()
    expect(await screen.findByDisplayValue('project-pipeline')).toBeInTheDocument()
    expect(screen.getByLabelText('时长（秒）')).toHaveValue(45)
    await waitFor(() => expect(referenceMaterialInputHarness.props?.uploadPurpose).toBe('montage_asset'))

    fireEvent.change(screen.getByPlaceholderText('描述这次要生产的视频内容、素材用途、节奏和交付目标'), {
      target: { value: '每周新品发布短片' },
    })
    act(() => {
      referenceMaterialInputHarness.props?.onChange([{
        type: 'video',
        url: '/source.mp4',
        file_name: 'source.mp4',
        content_type: 'video/mp4',
        size: 42,
      }])
    })
    fireEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.plans.create).toHaveBeenCalledWith(expect.objectContaining({
      type: 'montage',
      project_id: montageProject.id,
      montage_input: expect.objectContaining({
        brief: '每周新品发布短片',
        pipeline_key: 'project-pipeline',
        source_assets: [expect.objectContaining({ type: 'video_url', url: '/source.mp4' })],
        preferences: {
          aspect_ratio: '16:9',
          duration_seconds: 45,
          style: 'project style',
          music_prompt: 'project music',
          subtitle_mode: 'burned-in',
          voiceover_mode: 'narrated',
        },
        delivery_targets: ['final_video'],
      }),
    })))
  })

  it('keeps saved Montage input ahead of current project defaults when editing', async () => {
    vi.mocked(api.plans.list).mockResolvedValue({ items: [savedMontagePlan], total: 1 })
    render(<PlansPage />)

    fireEvent.click(await screen.findByRole('button', { name: '编辑' }))
    expect(await screen.findByRole('dialog', { name: '编辑计划' })).toBeInTheDocument()
    expect(screen.getByDisplayValue('saved-pipeline')).toBeInTheDocument()
    expect(screen.getByLabelText('时长（秒）')).toHaveValue(12)
    expect(screen.getByDisplayValue('9:16')).toBeInTheDocument()
    expect(screen.queryByText('final_video')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '更新' }))

    await waitFor(() => expect(api.plans.update).toHaveBeenCalledWith(
      savedMontagePlan.id,
      expect.objectContaining({
        montage_input: expect.objectContaining({
          pipeline_key: 'saved-pipeline',
          preferences: expect.objectContaining({ duration_seconds: 12, aspect_ratio: '9:16' }),
          delivery_targets: [],
        }),
      }),
    ))
  })

  it('blocks Montage plan save while source assets are uploading', async () => {
    window.history.pushState({}, '', `/plans?create=true&type=montage&project_id=${montageProject.id}&intent=schedule`)
    render(<PlansPage />)

    expect(await screen.findByRole('dialog', { name: '新建计划' })).toBeInTheDocument()
    await waitFor(() => expect(referenceMaterialInputHarness.props?.uploadPurpose).toBe('montage_asset'))

    act(() => {
      referenceMaterialInputHarness.props?.onUploadingChange?.(true)
    })

    expect(screen.getByRole('button', { name: '创建' })).toBeDisabled()
  })
})
