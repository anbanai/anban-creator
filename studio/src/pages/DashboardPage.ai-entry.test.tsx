import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DashboardPage from './DashboardPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import type { APIKey } from '@/types'

const navigateMock = vi.fn()
const uploadToOSSMock = vi.hoisted(() => vi.fn())
const toastSuccessMock = vi.hoisted(() => vi.fn())

const apiKey: APIKey = {
  id: 'key-1',
  user_id: 'user-1',
  name: '测试密钥',
  key_prefix: 'anban_test',
  last_used_at: null,
  created_at: '2026-08-03T00:00:00Z',
}

const {
  articleProject,
  seednoteProject,
  momentsProject,
  ecommerceProject,
  montageProject,
  executionProfiles,
  billingCatalog,
  platformConfigs,
  imageCapabilities,
  createdTask,
} = vi.hoisted(() => {
  const articleProject = {
    id: 'project-1',
    user_id: 'user-1',
    platform: 'article',
    name: '公众号项目',
    description: '品牌公众号内容',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: '',
    writer: '',
    theme: '',
    author: '',

    image_ratio: '16:9',
    max_concurrent_tasks: 1,
    config: { wechat_publish_mode: 'manual' },
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
  } as const

  return {
    articleProject,
    seednoteProject: {
      ...articleProject,
      id: 'project-2',
      platform: 'seednote',
      name: '种草项目',
      description: '种草笔记内容',
      image_ratio: '3:4',
      config: {},
    } as const,
    momentsProject: {
      ...articleProject,
      id: 'project-4',
      platform: 'moments',
      name: '朋友圈项目',
      description: '朋友圈内容',
      image_ratio: '1:1',
      config: {},
    } as const,
    ecommerceProject: {
      ...articleProject,
      id: 'project-3',
      platform: 'ecommerce',
      name: '电商项目',
      description: '电商图片内容',
      image_ratio: '4:3',
      config: {},
    } as const,
    montageProject: {
      ...articleProject,
      id: 'project-5',
      platform: 'montage',
      name: '短片项目',
      description: 'Montage 短片内容',
      image_ratio: '9:16',
      config: {},
      montage_defaults: {
        default_pipeline: 'social-short',
        preferences: { duration_seconds: 30 },
        delivery_targets: ['final_video'] as string[],
      },
    } as const,
    executionProfiles: [
      {
        id: 'effective',
        display_name: '性价比',
        provider: 'deepseek',
        model_name: 'deepseek-flash',
        description: '适合日常创作和批量任务',
        min_tier: 'free',
        available: true,
      },
      {
        id: 'balanced',
        display_name: '平衡型',
        provider: 'volcengine_ark',
        model_name: 'doubao-seed-evolving',
        description: '兼顾质量与成本',
        min_tier: 'pro',
        available: true,
      },
    ] as const,
    billingCatalog: {
      catalog_id: 'catalog-1',
      currency: 'credits',
      skus: [
        { id: 'task.article.effective', operation: 'task.article', charge_policy: 'task_admission', price_credits: 4000, execution_profile: 'effective', delivery: 'task' },
        { id: 'task.article.balanced', operation: 'task.article', charge_policy: 'task_admission', price_credits: 6000, execution_profile: 'balanced', delivery: 'task' },
        { id: 'task.seednote.effective', operation: 'task.seednote', charge_policy: 'task_admission', price_credits: 3200, execution_profile: 'effective', delivery: 'task' },
        { id: 'task.seednote.balanced', operation: 'task.seednote', charge_policy: 'task_admission', price_credits: 4000, execution_profile: 'balanced', delivery: 'task' },
        { id: 'task.montage.effective', operation: 'task.montage', charge_policy: 'task_admission', price_credits: 1600, execution_profile: 'effective', delivery: 'task' },
        { id: 'task.montage.balanced', operation: 'task.montage', charge_policy: 'task_admission', price_credits: 2000, execution_profile: 'balanced', delivery: 'task' },
      ],
    } as const,
    platformConfigs: [
      { id: 'article', label: '公众号文章', badge_variant: 'default', supports_publishing: true, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '16:9', supported_image_ratios: ['16:9', '4:3', '3:4', '1:1'], fields: [] },
      { id: 'seednote', label: '种草笔记', badge_variant: 'default', supports_publishing: false, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '3:4', supported_image_ratios: ['3:4', '1:1', '4:3'], fields: [] },
      { id: 'moments', label: '朋友圈', badge_variant: 'default', supports_publishing: false, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '1:1', supported_image_ratios: ['1:1', '3:4'], fields: [] },
      { id: 'ecommerce', label: '电商图', badge_variant: 'default', supports_publishing: false, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '4:3', supported_image_ratios: ['4:3', '1:1', '3:4', '16:9'], fields: [] },
      { id: 'montage', label: '智能剪辑', badge_variant: 'default', supports_publishing: false, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '9:16', supported_image_ratios: ['9:16', '16:9', '1:1'], fields: [] },
    ] as const,
    imageCapabilities: {
      tier: 'pro',
      default_capability: 'standard',
      items: [
        { key: 'standard', display_name: 'Standard', description: '日常图片生成', enabled: true, price_available: true, price_credits: 300 },
        { key: 'professional', display_name: 'Professional', description: '复杂高质量构图', enabled: true, price_available: true, price_credits: 500 },
      ],
    } as const,
    createdTask: {
      id: 'task-ai-1',
      type: 'article',
      prompt: '帮我写一篇新品发布公众号文章',
      status: 'pending',
      plan_id: null,
      project_id: 'project-1',
      execution_profile: 'effective',
      billing_price_credits: 6000,
      created_at: '2026-07-07T00:00:00.000Z',
      started_at: '',
      completed_at: '',
    } as const,
  }
})

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useNavigate: () => navigateMock,
  }
})

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

vi.mock('sonner', () => ({ toast: { error: vi.fn(), message: vi.fn(), success: toastSuccessMock } }))

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      aiEntry: {
        submit: vi.fn().mockResolvedValue({
          status: 'created',
          message: '已创建任务',
          tasks: [createdTask],
        }),
      },
      billing: {
        ...actual.api.billing,
        wallet: vi.fn().mockResolvedValue({ paid: 800, promotional: 0, debt: 0, balance: 800 }),
        catalog: vi.fn().mockResolvedValue(billingCatalog),
      },
      agentProfiles: {
        ...actual.api.agentProfiles,
        list: vi.fn().mockResolvedValue(executionProfiles),
      },
      plans: {
        ...actual.api.plans,
        list: vi.fn().mockResolvedValue({ items: [], total: 0 }),
      },
      tasks: {
        ...actual.api.tasks,
        list: vi.fn().mockResolvedValue({ items: [], total: 0 }),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue([{ ...articleProject }]),
        platformConfigs: vi.fn().mockResolvedValue(platformConfigs),
      },
      imageCapabilities: {
        ...actual.api.imageCapabilities,
        list: vi.fn().mockResolvedValue(imageCapabilities),
      },
      apiKeys: {
        ...actual.api.apiKeys,
        list: vi.fn().mockResolvedValue({ items: [{ id: 'key-1' }] }),
      },
      templates: {
        ...actual.api.templates,
        list: vi.fn().mockResolvedValue({
          items: [{
            id: 'template-1', type: 'seednote', name: '居家前后对比', category: '家居家装',
            thumbnail_url: '', prompt: '模板视觉 Prompt', visibility: 'public', sort_order: 0,
            is_active: true, created_at: '2026-08-02T00:00:00Z', updated_at: '2026-08-02T00:00:00Z',
          }],
          total: 1,
        }),
      },
    },
  }
})

async function getParametersTrigger(container: HTMLElement = document.body) {
  return within(container).findByRole('button', { name: /^创作参数：/ })
}

async function openParameters(container: HTMLElement = document.body) {
  const trigger = await getParametersTrigger(container)
  if (trigger.getAttribute('aria-expanded') !== 'true') fireEvent.click(trigger)
  return waitFor(() => {
    const popover = document.querySelector<HTMLElement>('[data-slot="popover-content"][data-open]')
    expect(popover).toBeInTheDocument()
    return popover!
  })
}

async function closeParameters() {
  const popover = document.querySelector<HTMLElement>('[data-slot="popover-content"][data-open]')
  if (!popover) return
  fireEvent.keyDown(popover, { key: 'Escape' })
  await waitFor(() => expect(document.querySelector('[data-slot="popover-content"][data-open]')).not.toBeInTheDocument())
}

async function expectParameterSummary(...parts: string[]) {
  const trigger = await getParametersTrigger()
  await waitFor(() => {
    for (const part of parts) expect(trigger).toHaveAccessibleName(expect.stringContaining(part))
  })
  return trigger
}

describe('DashboardPage AI entry', () => {
  beforeEach(() => {
    navigateMock.mockClear()
    toastSuccessMock.mockClear()
    uploadToOSSMock.mockClear()
    vi.mocked(api.aiEntry.submit).mockReset().mockResolvedValue({
      status: 'created',
      message: '已创建任务',
      tasks: [{ ...createdTask }],
    })
    vi.mocked(api.projects.list).mockReset().mockResolvedValue([{ ...articleProject }])
    vi.mocked(api.projects.platformConfigs).mockReset().mockResolvedValue(platformConfigs.map((config) => ({
      ...config,
      supported_image_ratios: [...config.supported_image_ratios],
      fields: [...config.fields],
    })))
    vi.mocked(api.billing.catalog).mockReset().mockResolvedValue({ ...billingCatalog, skus: [...billingCatalog.skus] })
    vi.mocked(api.agentProfiles.list).mockReset().mockResolvedValue([...executionProfiles])
    vi.mocked(api.imageCapabilities.list).mockReset().mockResolvedValue({
      ...imageCapabilities,
      items: [...imageCapabilities.items],
    })
    vi.mocked(api.apiKeys.list).mockReset().mockResolvedValue({ items: [{ ...apiKey }] })
    uploadToOSSMock.mockImplementation(async ({ file }: { file: File }) => ({
      uploadId: `upload-${file.name}`,
      key: `uploads/pending/user/${file.name}`,
      publicUrl: `https://cdn.example/${file.name}?signed=secret`,
      contentType: file.type,
      size: file.size,
    }))
  })

  it('renders ordered composer parameters and submits explicit selections', async () => {
    render(<DashboardPage />)

    const composer = document.querySelector<HTMLElement>('[data-slot="agent-prompt-input"]')
    expect(composer).toBeInTheDocument()
    const parametersControl = await getParametersTrigger(composer!)
    const projectControl = await within(composer!).findByRole('combobox', { name: '项目：公众号项目' })
    expect(projectControl.compareDocumentPosition(parametersControl) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(composer!).queryByRole('button', { name: /^执行配置：/ })).not.toBeInTheDocument()
    expect(within(composer!).queryByRole('button', { name: /^图像设置：/ })).not.toBeInTheDocument()
    expect(within(composer!).queryByRole('button', { name: '任务数量：1' })).not.toBeInTheDocument()
    expect(projectControl.closest('[data-slot="project-context-control"]')).toHaveAttribute('data-compact', 'true')
    expect(screen.queryByRole('group', { name: 'Agent 执行配置' })).not.toBeInTheDocument()
    expect(screen.queryByText('执行配置')).not.toBeInTheDocument()
    expect(screen.queryByText('deepseek-flash')).not.toBeInTheDocument()

    const parameters = await openParameters(composer!)
    fireEvent.click(within(parameters).getByRole('button', { name: /^平衡型，Pro 版及以上/ }))
    fireEvent.click(within(parameters).getByRole('button', { name: '增加任务数量' }))
    fireEvent.click(within(parameters).getByRole('button', { name: '3:4' }))
    fireEvent.click(within(parameters).getByText('Professional'))
    await closeParameters()

    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '写一篇新品介绍' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith({
      channel: 'studio',
      project_id: 'project-1',
      text: '写一篇新品介绍',
      execution_profile: 'balanced',
      attachments: [],
      quantity: 2,
      image_ratio: '3:4',
      image_capability_key: 'professional',
    }))
  })

  it('creates a Montage task with the selected video ratio and image capability', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([{ ...montageProject }])
    render(<DashboardPage />)

    expect(await screen.findByRole('combobox', { name: '项目：短片项目' })).toBeInTheDocument()
    const parameters = await openParameters()
    expect(within(parameters).getByText('视频比例')).toBeInTheDocument()
	  expect(within(parameters).getByRole('button', { name: '9:16' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(parameters).getByLabelText('任务数量：1，当前能力上限')).toHaveTextContent('任务数量 1 · 当前能力上限')
    await closeParameters()
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '做一条新品发布短片' },
    })
    const submit = screen.getByRole('button', { name: '发送创建任务' })
    await waitFor(() => expect(submit).toBeEnabled())
    fireEvent.click(submit)

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith({
      channel: 'studio',
      project_id: 'project-5',
      text: '做一条新品发布短片',
      execution_profile: 'effective',
      attachments: [],
      quantity: 1,
	  image_ratio: '9:16',
	  image_capability_key: 'standard',
    }))
	  expect(api.imageCapabilities.list).toHaveBeenCalledTimes(1)
  })

  it('waits for platform defaults before enabling project choice or submission', async () => {
    const resolvedPlatformConfigs = platformConfigs.map((config) => ({
      ...config,
      supported_image_ratios: [...config.supported_image_ratios],
      fields: [...config.fields],
    }))
    let resolvePlatformConfigs!: (value: typeof resolvedPlatformConfigs) => void
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...articleProject, image_ratio: '' },
      { ...seednoteProject, image_ratio: '' },
    ])
    vi.mocked(api.projects.platformConfigs).mockImplementationOnce(() => new Promise((resolve) => {
      resolvePlatformConfigs = resolve
    }))
    render(<DashboardPage />)

    const projectControl = await screen.findByRole('combobox', { name: '项目：公众号项目' })
    const submit = screen.getByRole('button', { name: '发送创建任务' })
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '按平台默认比例创作' },
    })

    expect(projectControl).toBeDisabled()
    expect(submit).toBeDisabled()
    fireEvent.click(submit)
    expect(api.aiEntry.submit).not.toHaveBeenCalled()

    await act(async () => {
      resolvePlatformConfigs(resolvedPlatformConfigs)
      await Promise.resolve()
    })

    await waitFor(() => expect(projectControl).toBeEnabled())
    expect(projectControl).toHaveTextContent('公众号项目')
    await expectParameterSummary('16:9', 'Standard')

    fireEvent.click(projectControl)
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))
    await expectParameterSummary('3:4', 'Standard')
    fireEvent.click(submit)

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith(expect.objectContaining({
      project_id: 'project-2',
      image_ratio: '3:4',
    })))
  })

  it('keeps creation blocked and retries when platform defaults fail to load', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([
      { ...articleProject, image_ratio: '' },
    ])
    vi.mocked(api.projects.platformConfigs).mockRejectedValueOnce(new Error('platform defaults unavailable'))
    render(<DashboardPage />)

    expect(await screen.findByText('加载失败')).toBeInTheDocument()
    const projectControl = screen.getByRole('combobox', { name: '项目：公众号项目' })
    const submit = screen.getByRole('button', { name: '发送创建任务' })
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '按平台默认比例创作' },
    })

    expect(projectControl).toBeDisabled()
    expect(submit).toBeDisabled()
    expect(api.aiEntry.submit).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(api.projects.platformConfigs).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(projectControl).toBeEnabled())
    expect(projectControl).toHaveTextContent('公众号项目')
    await expectParameterSummary('16:9', 'Standard')
  })

  it('waits for API-key readiness before enabling submission', async () => {
    let resolveApiKeys!: (value: { items: APIKey[] }) => void
    vi.mocked(api.apiKeys.list).mockImplementationOnce(() => new Promise((resolve) => {
      resolveApiKeys = resolve
    }))
    render(<DashboardPage />)

    await waitFor(async () => expect(await getParametersTrigger()).toBeEnabled())
    const submit = screen.getByRole('button', { name: '发送创建任务' })
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '等待 API Key 状态' },
    })

    expect(submit).toBeDisabled()
    expect(api.aiEntry.submit).not.toHaveBeenCalled()

    await act(async () => {
      resolveApiKeys({ items: [{ ...apiKey }] })
      await Promise.resolve()
    })

    await waitFor(() => expect(submit).toBeEnabled())
  })

  it('blocks and retries when API-key readiness fails to load', async () => {
    vi.mocked(api.apiKeys.list).mockRejectedValueOnce(new Error('API keys unavailable'))
    render(<DashboardPage />)

    expect(await screen.findByText('加载失败')).toBeInTheDocument()
    const submit = screen.getByRole('button', { name: '发送创建任务' })
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '恢复 API Key 后创建' },
    })

    expect(submit).toBeDisabled()
    expect(api.aiEntry.submit).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(api.apiKeys.list).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(submit).toBeEnabled())
    expect(screen.queryByText('加载失败')).not.toBeInTheDocument()
  })

  it.each(['image-capabilities', 'execution-profiles', 'billing-catalog'] as const)(
    'recovers all required composer queries after %s initially fails',
    async (failedQuery) => {
      if (failedQuery === 'image-capabilities') {
        vi.mocked(api.imageCapabilities.list).mockRejectedValueOnce(new Error('image capabilities unavailable'))
      } else if (failedQuery === 'execution-profiles') {
        vi.mocked(api.agentProfiles.list).mockRejectedValueOnce(new Error('execution profiles unavailable'))
      } else {
        vi.mocked(api.billing.catalog).mockRejectedValueOnce(new Error('billing catalog unavailable'))
      }
      render(<DashboardPage />)

      expect(await screen.findByText('加载失败')).toBeInTheDocument()
      const submit = screen.getByRole('button', { name: '发送创建任务' })
      const affectedControl = await getParametersTrigger()
      fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
        target: { value: '恢复后创建任务' },
      })

      expect(affectedControl).toBeEnabled()
      expect(submit).toBeDisabled()
      expect(api.aiEntry.submit).not.toHaveBeenCalled()

      fireEvent.click(screen.getByRole('button', { name: '重试' }))

      await waitFor(() => {
        expect(api.projects.list).toHaveBeenCalledTimes(2)
        expect(api.projects.platformConfigs).toHaveBeenCalledTimes(2)
        expect(api.imageCapabilities.list).toHaveBeenCalledTimes(2)
        expect(api.agentProfiles.list).toHaveBeenCalledTimes(2)
        expect(api.billing.catalog).toHaveBeenCalledTimes(2)
      })
      await waitFor(() => expect(affectedControl).toBeEnabled())
      await waitFor(() => expect(submit).toBeEnabled())
      expect(screen.queryByText('加载失败')).not.toBeInTheDocument()
    },
  )

  it('blocks cached image capabilities after their recovery refetch fails', async () => {
    vi.mocked(api.imageCapabilities.list)
      .mockReset()
      .mockResolvedValueOnce({ ...imageCapabilities, items: [...imageCapabilities.items] })
      .mockRejectedValueOnce(new Error('cached image capabilities are stale'))
      .mockResolvedValue({ ...imageCapabilities, items: [...imageCapabilities.items] })
    vi.mocked(api.billing.catalog)
      .mockReset()
      .mockRejectedValueOnce(new Error('billing catalog unavailable'))
      .mockResolvedValue({ ...billingCatalog, skus: [...billingCatalog.skus] })
    render(<DashboardPage />)

    expect(await screen.findByText('加载失败')).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '缓存失效时不可提交' },
    })
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(api.imageCapabilities.list).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(api.billing.catalog).toHaveBeenCalledTimes(2))
    expect(await getParametersTrigger()).toBeEnabled()
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
    expect(screen.getByText('加载失败')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(api.imageCapabilities.list).toHaveBeenCalledTimes(3))
    await waitFor(async () => expect(await getParametersTrigger()).toBeEnabled())
    await waitFor(() => expect(screen.getByRole('button', { name: '发送创建任务' })).toBeEnabled())
  })

  it('blocks cached projects after their recovery refetch fails', async () => {
    vi.mocked(api.projects.list)
      .mockReset()
      .mockResolvedValueOnce([{ ...articleProject }])
      .mockRejectedValueOnce(new Error('cached projects are stale'))
      .mockResolvedValue([{ ...articleProject }])
    vi.mocked(api.billing.catalog)
      .mockReset()
      .mockRejectedValueOnce(new Error('billing catalog unavailable'))
      .mockResolvedValue({ ...billingCatalog, skus: [...billingCatalog.skus] })
    render(<DashboardPage />)

    expect(await screen.findByText('加载失败')).toBeInTheDocument()
    const submit = screen.getByRole('button', { name: '发送创建任务' })
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '项目缓存失效时不可提交' },
    })
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(api.projects.list).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(api.billing.catalog).toHaveBeenCalledTimes(2))
    expect(submit).toBeDisabled()
    expect(screen.getByText('加载失败')).toBeInTheDocument()
    expect(api.aiEntry.submit).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(api.projects.list).toHaveBeenCalledTimes(3))
    await waitFor(() => expect(submit).toBeEnabled())
  })

  it.each([
    '创建任务失败：image capability "professional" requires enterprise tier',
    '创建任务失败：unknown image capability key "professional"',
    '创建任务失败：图片能力 “professional” 未授权。',
  ])('retains composer input and requires reselection after an image capability rejection: %s', async (message) => {
    const file = new File(['reference'], 'keep-reference.png', { type: 'image/png' })
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({
      status: 'error',
      message,
    })
    render(<DashboardPage />)

    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files: [file] } })
    expect(await screen.findByText(file.name)).toBeInTheDocument()
    const parameters = await openParameters()
    fireEvent.click(within(parameters).getByText('Professional'))
    await closeParameters()
    fireEvent.change(prompt, { target: { value: '保留这段 Prompt 和附件' } })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    expect(await screen.findByText(/请重新选择图片能力/)).toBeInTheDocument()
    expect(prompt).toHaveValue('保留这段 Prompt 和附件')
    expect(screen.getByText(file.name)).toBeInTheDocument()
    await waitFor(() => expect(api.imageCapabilities.list).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
    await expectParameterSummary('16:9')

    const recoveredParameters = await openParameters()
    fireEvent.click(within(recoveredParameters).getByText('Standard'))
    await closeParameters()

    await waitFor(() => expect(screen.getByRole('button', { name: '发送创建任务' })).toBeEnabled())
    expect(api.aiEntry.submit).toHaveBeenCalledTimes(1)
  })

  it.each([
    '创建任务失败：图片能力服务暂不可用。',
    '创建任务失败：image capability provider is disabled',
    '创建任务失败：image capability runtime is unauthorized',
  ])('keeps the selected capability retryable for an operational failure: %s', async (message) => {
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({ status: 'error', message })
    render(<DashboardPage />)

    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    const retryParameters = await openParameters()
    fireEvent.click(within(retryParameters).getByText('Professional'))
    await closeParameters()
    fireEvent.change(prompt, { target: { value: '服务恢复后直接重试' } })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    expect(await screen.findByText(message)).toBeInTheDocument()
    expect(screen.queryByText(/请重新选择图片能力/)).not.toBeInTheDocument()
    expect(prompt).toHaveValue('服务恢复后直接重试')
    await expectParameterSummary('16:9', 'Professional')
    expect(api.imageCapabilities.list).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeEnabled()
  })

  it('does not force image capability reselection for unrelated AI Entry errors', async () => {
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({
      status: 'error',
      message: 'AI 入口服务繁忙，请稍后重试。',
    })
    render(<DashboardPage />)

    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    fireEvent.change(prompt, { target: { value: '普通错误仍可重试' } })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    expect(await screen.findByText('AI 入口服务繁忙，请稍后重试。')).toBeInTheDocument()
    expect(prompt).toHaveValue('普通错误仍可重试')
    expect(api.imageCapabilities.list).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeEnabled()
  })

  it('disables submission when the selected profile has no SKU after the project changes', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...articleProject },
      { ...seednoteProject },
    ])
    vi.mocked(api.billing.catalog).mockResolvedValueOnce({
      ...billingCatalog,
      skus: billingCatalog.skus.filter((sku) => sku.operation === 'task.article'),
    })
    render(<DashboardPage />)

    const executionParameters = await openParameters()
    fireEvent.click(within(executionParameters).getByRole('button', { name: /^平衡型，Pro 版及以上/ }))
    await closeParameters()
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '写一篇种草笔记' },
    })
    fireEvent.click(screen.getByRole('combobox', { name: '项目：公众号项目' }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))

    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
    expect(api.aiEntry.submit).not.toHaveBeenCalled()
  })

  it('sends first-time users to project creation from the AI entry', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    const projectControl = await screen.findByRole('combobox', { name: '项目：未选择' })
    expect(projectControl).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /创建第一个项目/ })).not.toBeInTheDocument()
    fireEvent.click(projectControl)
    expect(await screen.findByRole('link', { name: '创建项目' })).toHaveAttribute(
      'href',
      '/projects?return_to=%2Ftasks&create=true&type=seednote&intent=new',
    )
    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
  })

  it('shows an operator-owned error when the server internal model is unavailable', async () => {
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({
      status: 'needs_configuration',
      message: 'AI 入口意图解析模型暂不可用，请联系管理员。',
      action_url: '',
    })

    render(<DashboardPage />)
    fireEvent.change(await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '写一篇新品介绍' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    expect(await screen.findByText('AI 入口意图解析模型暂不可用，请联系管理员。')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '补充配置' })).not.toBeInTheDocument()
  })

  it('renders a Codex-style AI entry and submits every shared attachment unchanged', async () => {
    const files = [
      new File(['image'], 'product.png', { type: 'image/png' }),
      new File(['audio'], 'voice.mp3', { type: 'audio/mpeg' }),
      new File(['video'], 'demo.mp4', { type: 'video/mp4' }),
      new File(['pdf'], 'brief.pdf', { type: 'application/pdf' }),
      new File(['notes'], 'notes.txt', { type: 'text/plain' }),
    ]

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '首页项目选择' })).not.toBeInTheDocument()
    expect(await screen.findByRole('combobox', { name: '项目：公众号项目' })).toHaveTextContent('公众号项目')
    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    expect(screen.getByRole('combobox', { name: '项目：公众号项目' })).toHaveTextContent('公众号')
    expect(screen.queryByRole('link', { name: /新建创作任务/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /安排自动计划/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /管理项目配置/ })).not.toBeInTheDocument()
    expect(screen.queryByText('今日创作态势')).not.toBeInTheDocument()
    expect(screen.queryByText('接入状态')).not.toBeInTheDocument()
    expect(screen.queryByText('下一步')).not.toBeInTheDocument()
    expect(screen.queryByText('最近任务')).not.toBeInTheDocument()
    await waitFor(() => expect(screen.queryByText('还没有任务')).not.toBeInTheDocument())
    fireEvent.change(screen.getByLabelText('选择附件文件'), { target: { files } })
    await waitFor(() => expect(uploadToOSSMock).toHaveBeenCalledTimes(5))
    expect(screen.getByLabelText('选择附件文件')).toBeDisabled()
    await screen.findByText('product.png')
    fireEvent.change(prompt, { target: { value: '帮我写一篇新品发布公众号文章' } })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith({
      channel: 'studio',
      project_id: 'project-1',
      text: '帮我写一篇新品发布公众号文章',
      execution_profile: 'effective',
      quantity: 1,
      image_ratio: '16:9',
      image_capability_key: 'standard',
      attachments: files.map((file, index) => ({
        type: (['image', 'audio', 'video', 'document', 'text'] as const)[index],
        upload_id: `upload-${file.name}`,
        key: `uploads/pending/user/${file.name}`,
        file_name: file.name,
        content_type: file.type,
        size: file.size,
      })),
    }))
    expect(navigateMock).toHaveBeenCalledWith('/tasks/task-ai-1')
  })

  it('exposes the selected project identity in the composer control name', async () => {
    render(<DashboardPage />)

    expect(await screen.findByRole('combobox', { name: '项目：公众号项目' })).toBeInTheDocument()
  })

  it('keeps supported task quantities and project image defaults while clamping single-task projects', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...articleProject },
      { ...seednoteProject },
      { ...momentsProject },
      { ...ecommerceProject },
    ])
    render(<DashboardPage />)

    const selectProject = async (name: RegExp) => {
      fireEvent.click(await screen.findByRole('combobox', { name: /^项目：/ }))
      fireEvent.click(await screen.findByRole('option', { name }))
    }

    await expectParameterSummary('16:9', 'Standard')
    const quantityParameters = await openParameters()
    fireEvent.click(within(quantityParameters).getByText('Professional'))
    const increment = within(quantityParameters).getByRole('button', { name: '增加任务数量' })
    fireEvent.click(increment)
    fireEvent.click(increment)
    fireEvent.click(increment)
    fireEvent.click(increment)
    expect(increment).toBeDisabled()
    await closeParameters()

    await selectProject(/种草项目/)
    await expectParameterSummary('任务数量 5', '3:4', 'Professional')

    await selectProject(/朋友圈项目/)
    await expectParameterSummary('任务数量 5', '1:1', 'Professional')

    await selectProject(/电商项目/)
    await expectParameterSummary('任务数量 1', '4:3', 'Professional')
    const ecommerceParameters = await openParameters()
    expect(within(ecommerceParameters).getByLabelText('任务数量：1，当前能力上限')).toHaveTextContent('任务数量 1 · 当前能力上限')
    expect(within(ecommerceParameters).queryByRole('button', { name: '增加任务数量' })).not.toBeInTheDocument()
  })

  it('routes multiple created tasks to the task list and shows the server message', async () => {
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({
      status: 'created',
      message: '已创建 2 个任务',
      tasks: [
        { ...createdTask },
        { ...createdTask, id: 'task-ai-2' },
      ],
    })
    render(<DashboardPage />)

    fireEvent.change(await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '创建两篇文章' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    await waitFor(() => expect(navigateMock).toHaveBeenCalledWith('/tasks'))
    expect(toastSuccessMock).toHaveBeenCalledWith('已创建 2 个任务')
  })

  it.each(['empty', 'missing'] as const)('handles a %s created task collection without crashing', async (variant) => {
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({
      status: 'created',
      message: '没有创建任何任务',
      ...(variant === 'empty' ? { tasks: [] } : {}),
    })
    render(<DashboardPage />)

    fireEvent.change(await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '创建文章' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    expect(await screen.findByText('没有创建任何任务')).toBeInTheDocument()
    expect(navigateMock).not.toHaveBeenCalled()
  })

  it('disables submission while the shared reference input is uploading', async () => {
    let resolveUpload!: (value: unknown) => void
    uploadToOSSMock.mockImplementationOnce(() => new Promise((resolve) => { resolveUpload = resolve }))
    render(<DashboardPage />)

    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    fireEvent.change(prompt, { target: { value: '写一篇新品介绍' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '发送创建任务' })).toBeEnabled())

    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['image'], 'pending.png', { type: 'image/png' })] },
    })

    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
    await act(async () => {
      resolveUpload({ uploadId: 'pending', key: 'uploads/pending/pending.png', publicUrl: '', contentType: 'image/png', size: 5 })
      await Promise.resolve()
    })
  })

  it('uses the selected project from the composer menu when multiple projects exist', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...articleProject },
      { ...seednoteProject },
    ])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: '首页项目选择' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /新建创作任务/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /安排自动计划/ })).not.toBeInTheDocument()

    fireEvent.click(await screen.findByRole('combobox', { name: '项目：公众号项目' }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))
    expect(screen.getByRole('combobox', { name: '项目：种草项目' })).toHaveTextContent('种草项目')

    fireEvent.change(await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '写一篇小红书种草笔记' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    await waitFor(() => expect(api.aiEntry.submit).toHaveBeenCalledWith(expect.objectContaining({
      project_id: 'project-2',
      text: '写一篇小红书种草笔记',
    })))
  })

  it('选择小红书项目后用模板覆盖 Prompt 并保留附件', async () => {
    vi.mocked(api.projects.list).mockResolvedValue([{ ...seednoteProject }])
    render(<DashboardPage />)

    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目：种草项目' })).toHaveTextContent('种草项目'))
    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    fireEvent.change(prompt, { target: { value: '已有需求' } })
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['image'], 'keep.png', { type: 'image/png' })] },
    })
    expect(await screen.findByText('keep.png')).toBeInTheDocument()

    fireEvent.click(await screen.findByRole('button', { name: /居家前后对比/ }))

    expect(prompt).toHaveValue('模板视觉 Prompt')
    expect(screen.getByText('keep.png')).toBeInTheDocument()
  })

  it('does not render legacy shortcut cards for project types that cannot create plans', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...ecommerceProject },
    ])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    expect(await screen.findByRole('combobox', { name: '项目：电商项目' })).toHaveTextContent('电商项目')
    expect(screen.queryByRole('link', { name: /新建创作任务/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /安排自动计划/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /管理项目配置/ })).not.toBeInTheDocument()
  })

  it('shows needs-configuration errors without navigating', async () => {
    vi.mocked(api.aiEntry.submit).mockResolvedValueOnce({
      status: 'needs_configuration',
      message: '请先上传产品图',
      action_url: '/tasks?create=true&type=ecommerce&project_id=project-1&intent=new',
    })

    render(<DashboardPage />)
    fireEvent.change(await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '做一套保温杯主图' },
    })
    fireEvent.click(screen.getByRole('button', { name: '发送创建任务' }))

    expect(await screen.findByText('请先上传产品图')).toBeInTheDocument()
    expect(await screen.findByRole('link', { name: '补充配置' })).toHaveAttribute('href', '/tasks?create=true&type=ecommerce&project_id=project-1&intent=new')
    expect(navigateMock).not.toHaveBeenCalledWith('/tasks/task-ai-1')
  })
})
