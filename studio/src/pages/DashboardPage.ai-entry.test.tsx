import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DashboardPage from './DashboardPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'

const navigateMock = vi.fn()
const uploadToOSSMock = vi.hoisted(() => vi.fn())
const toastSuccessMock = vi.hoisted(() => vi.fn())

const {
  articleProject,
  seednoteProject,
  momentsProject,
  ecommerceProject,
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
    config: { enable_publishing: true },
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
    executionProfiles: [
      {
        id: 'effective',
        display_name: '性价比',
        provider: 'deepseek',
        model_name: 'deepseek-v4-flash',
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
      ],
    } as const,
    platformConfigs: [
      { id: 'article', label: '公众号文章', badge_variant: 'default', supports_publishing: true, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '16:9', supported_image_ratios: ['16:9', '4:3', '3:4', '1:1'], fields: [] },
      { id: 'seednote', label: '种草笔记', badge_variant: 'default', supports_publishing: false, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '3:4', supported_image_ratios: ['3:4', '1:1', '4:3'], fields: [] },
      { id: 'moments', label: '朋友圈', badge_variant: 'default', supports_publishing: false, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '1:1', supported_image_ratios: ['1:1', '3:4'], fields: [] },
      { id: 'ecommerce', label: '电商图', badge_variant: 'default', supports_publishing: false, supports_auto_fetch: false, profile_url_pattern: '', default_image_ratio: '4:3', supported_image_ratios: ['4:3', '1:1', '3:4', '16:9'], fields: [] },
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
      progress: 0,
      plan_id: null,
      project_id: 'project-1',
      execution_profile: 'effective',
      result: null,
      published: false,
      published_at: null,
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

describe('DashboardPage AI entry', () => {
  beforeEach(() => {
    navigateMock.mockClear()
    toastSuccessMock.mockClear()
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
    vi.mocked(api.imageCapabilities.list).mockReset().mockResolvedValue({
      ...imageCapabilities,
      items: [...imageCapabilities.items],
    })
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
    const projectControl = await within(composer!).findByRole('combobox', { name: '项目上下文' })
    const executionControl = await within(composer!).findByRole('button', { name: /^执行配置：/ })
    const imageControl = await within(composer!).findByRole('button', { name: /^图像设置：/ })
    const quantityControl = within(composer!).getByRole('button', { name: '任务数量：1' })

    for (const [left, right] of [
      [projectControl, executionControl],
      [executionControl, imageControl],
      [imageControl, quantityControl],
    ]) {
      expect(left.compareDocumentPosition(right) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }
    expect(projectControl).toHaveTextContent('公众号项目')
    expect(projectControl.closest('[data-slot="project-context-control"]')).toHaveAttribute('data-compact', 'true')
    expect(screen.queryByRole('group', { name: 'Agent 执行配置' })).not.toBeInTheDocument()
    expect(screen.queryByText('执行配置')).not.toBeInTheDocument()
    expect(screen.queryByText('deepseek-v4-flash')).not.toBeInTheDocument()

    fireEvent.click(executionControl)
    fireEvent.click(await screen.findByRole('button', { name: /^平衡型，Pro 版及以上/ }))
    fireEvent.keyDown(document, { key: 'Escape' })

    fireEvent.click(quantityControl)
    fireEvent.click(await screen.findByRole('button', { name: '增加任务数量' }))
    fireEvent.keyDown(document, { key: 'Escape' })

    fireEvent.click(screen.getByRole('button', { name: /^图像设置：/ }))
    fireEvent.click(await screen.findByText('3:4'))
    fireEvent.click(screen.getByText('Professional'))
    fireEvent.keyDown(document, { key: 'Escape' })

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

    fireEvent.click(await screen.findByRole('button', { name: /^执行配置：/ }))
    fireEvent.click(await screen.findByRole('button', { name: /^平衡型，Pro 版及以上/ }))
    fireEvent.change(screen.getByPlaceholderText('描述你想创作的内容、目标和素材要求...'), {
      target: { value: '写一篇种草笔记' },
    })
    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))

    expect(screen.getByRole('button', { name: '发送创建任务' })).toBeDisabled()
    expect(api.aiEntry.submit).not.toHaveBeenCalled()
  })

  it('sends first-time users to project creation from the AI entry', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([])

    render(<DashboardPage />)

    expect(await screen.findByRole('heading', { name: '首页' })).toBeInTheDocument()
    const projectControl = await screen.findByRole('combobox', { name: '项目上下文' })
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
    expect(await screen.findByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目')
    const prompt = await screen.findByPlaceholderText('描述你想创作的内容、目标和素材要求...')
    expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号')
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

  it('keeps supported task quantities and project image defaults while clamping single-task projects', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([
      { ...articleProject },
      { ...seednoteProject },
      { ...momentsProject },
      { ...ecommerceProject },
    ])
    render(<DashboardPage />)

    const selectProject = async (name: RegExp) => {
      fireEvent.click(await screen.findByRole('combobox', { name: '项目上下文' }))
      fireEvent.click(await screen.findByRole('option', { name }))
    }

    expect(await screen.findByRole('button', { name: '图像设置：16:9 · Standard' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '图像设置：16:9 · Standard' }))
    fireEvent.click(await screen.findByText('Professional'))
    fireEvent.keyDown(document, { key: 'Escape' })

    fireEvent.click(screen.getByRole('button', { name: '任务数量：1' }))
    const increment = await screen.findByRole('button', { name: '增加任务数量' })
    fireEvent.click(increment)
    fireEvent.click(increment)
    fireEvent.click(increment)
    fireEvent.click(increment)
    expect(increment).toBeDisabled()
    fireEvent.keyDown(document, { key: 'Escape' })

    await selectProject(/种草项目/)
    expect(await screen.findByRole('button', { name: '任务数量：5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '图像设置：3:4 · Professional' })).toBeInTheDocument()

    await selectProject(/朋友圈项目/)
    expect(await screen.findByRole('button', { name: '任务数量：5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '图像设置：1:1 · Professional' })).toBeInTheDocument()

    await selectProject(/电商项目/)
    expect(await screen.findByLabelText('任务数量：1，当前能力上限')).toHaveTextContent('任务数量 1 · 当前能力上限')
    expect(screen.queryByRole('button', { name: '增加任务数量' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '图像设置：4:3 · Professional' })).toBeInTheDocument()
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

    fireEvent.click(await screen.findByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))
    expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('种草项目')

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

    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('种草项目'))
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
    expect(await screen.findByRole('combobox', { name: '项目上下文' })).toHaveTextContent('电商项目')
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
