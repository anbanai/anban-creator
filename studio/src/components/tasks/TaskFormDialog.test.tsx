import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AgentPromptDropProvider } from '@/components/agent-prompt/AgentPromptDropProvider'
import { api } from '@/lib/api'
import { createTestQueryClient } from '@/test/test-utils'
import type { Project, Task } from '@/types'
import { TaskFormDialog, type TaskFormDialogProps } from './TaskFormDialog'

const toastMocks = vi.hoisted(() => ({
  error: vi.fn(),
  message: vi.fn(),
  success: vi.fn(),
}))

vi.mock('sonner', () => ({ toast: toastMocks }))

const uploadToOSSMock = vi.hoisted(() => vi.fn())

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

const fixtures = vi.hoisted(() => {
  const articleProject = {
    id: 'article-project',
    user_id: 'user-1',
    platform: 'article',
    name: '公众号项目',
    avatar_url: '',
    profile_url: '',
    keywords: '',
    visual_style: 'editorial',
    writer: 'concise',
    theme: 'clean',
    author: 'Anban',
    template_id: '',
    image_ratio: '16:9',
    max_concurrent_tasks: 1,
    config: {},
    status: 'active',
    created_at: '2026-07-01T00:00:00.000Z',
    updated_at: '2026-07-01T00:00:00.000Z',
  } as Project
  const seednoteProject = {
    ...articleProject,
    id: 'seednote-project',
    platform: 'seednote',
    name: '种草项目',
    image_ratio: '3:4',
    ecommerce_defaults: { image_model_key: 'destination-model' },
  } as Project
  const montageProject = {
    ...articleProject,
    id: 'montage-project',
    platform: 'montage',
    name: '剪辑项目',
    montage_defaults: {
      default_pipeline: 'social-short',
      preferences: { aspect_ratio: '9:16', duration_seconds: 45, style: 'clean product film' },
      delivery_targets: ['final_video'],
    },
  } as Project
  const sourceTask = {
    id: 'source-task',
    type: 'article',
    title: '源文章',
    prompt: '复制后的完整创作要求',
    status: 'completed',
    image_ratio: '16:9',
    image_model_key: 'source-model',
    input_attachments: [
      { type: 'document', upload_id: 'keep-upload', key: 'uploads/pending/keep.pdf', file_name: 'keep.pdf', content_type: 'application/pdf', size: 42 },
      { type: 'text', text: 'continue', file_name: 'resume.txt', role: 'resume_latest' },
    ],
    reference_image: {
      asset_id: '11111111-1111-4111-8111-111111111111',
      file_name: 'reference.png',
      content_type: 'image/png',
      size: 64,
      download_url: 'https://cdn.example/reference.png',
      download_expires_at: '2026-07-23T00:00:00.000Z',
    },
    watermark: true,
    article_with_cover: false,
    article_with_content_images: true,
    goal_mode: true,
    goal: '必须包含三个案例',
    project_id: articleProject.id,
    execution_profile: 'effective',
    result: null,
    published: false,
    published_at: null,
    billing_price_credits: 6000,
    created_at: '2026-07-01T00:00:00.000Z',
    started_at: '2026-07-01T00:01:00.000Z',
    completed_at: '2026-07-01T00:10:00.000Z',
  } as Task
  const createdTask = { ...sourceTask, id: 'created-task', status: 'pending' } as Task
  return { articleProject, seednoteProject, montageProject, sourceTask, createdTask }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        create: vi.fn(),
        clone: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn(),
      },
      billing: {
        ...actual.api.billing,
        wallet: vi.fn(),
        catalog: vi.fn(),
      },
      agentProfiles: {
        ...actual.api.agentProfiles,
        list: vi.fn(),
      },
      imageModels: {
        ...actual.api.imageModels,
        list: vi.fn(),
      },
    },
  }
})

function renderDialog(props: Partial<TaskFormDialogProps> = {}) {
  const onOpenChange = vi.fn()
  const onCreated = vi.fn()
  const queryClient = createTestQueryClient()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>
        <AgentPromptDropProvider>
          <TaskFormDialog
            open
            mode="create"
            initialProjectId={fixtures.articleProject.id}
            onOpenChange={onOpenChange}
            onCreated={onCreated}
            {...props}
          />
        </AgentPromptDropProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return { ...view, onOpenChange, onCreated, queryClient }
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.projects.list).mockResolvedValue([
    fixtures.articleProject,
    fixtures.seednoteProject,
    fixtures.montageProject,
  ])
  vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 100000, promotional: 0, debt: 0, balance: 100000 })
  vi.mocked(api.billing.catalog).mockResolvedValue({
    catalog_id: 'retail-test-v1',
    currency: 'credits',
    skus: [
      { id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4800, delivery: 'article_artifacts_verified' },
      { id: 'task.article.balanced', operation: 'task.article', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 6000, delivery: 'article_artifacts_verified' },
      { id: 'task.article.quality', operation: 'task.article', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 18000, delivery: 'article_artifacts_verified' },
      { id: 'task.seednote.effective', operation: 'task.seednote', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4000, delivery: 'seednote_artifacts_verified' },
      { id: 'task.seednote.balanced', operation: 'task.seednote', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 5000, delivery: 'seednote_artifacts_verified' },
      { id: 'task.viral-analysis.effective', operation: 'task.viral_analysis', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 1200, delivery: 'viral_analysis_report_verified' },
      { id: 'task.viral-analysis.balanced', operation: 'task.viral_analysis', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 1200, delivery: 'viral_analysis_report_verified' },
      { id: 'task.viral-analysis.quality', operation: 'task.viral_analysis', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 1200, delivery: 'viral_analysis_report_verified' },
      { id: 'task.montage.effective', operation: 'task.montage', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 2000, delivery: 'montage_artifacts_verified' },
    ],
  })
  vi.mocked(api.agentProfiles.list).mockResolvedValue([
    { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-v4-flash', description: '适合日常创作', min_tier: 'free', available: true },
    { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
    { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: true },
  ])
  vi.mocked(api.imageModels.list).mockResolvedValue({
    tier: 'pro',
    items: [
      { key: 'standard_image', display_name: '标准图像', min_tier: 'free', is_custom: false },
      { key: 'source-model', display_name: '源图像', min_tier: 'pro', is_custom: false },
      { key: 'destination-model', display_name: '目标图像', min_tier: 'pro', is_custom: false },
    ],
  })
  vi.mocked(api.tasks.create).mockResolvedValue(fixtures.createdTask)
  vi.mocked(api.tasks.clone).mockResolvedValue(fixtures.createdTask)
  uploadToOSSMock.mockResolvedValue({
    uploadId: 'uploaded-attachment',
    key: 'uploads/pending/attachment',
    publicUrl: '',
    contentType: 'application/pdf',
    size: 42,
  })
})

describe('TaskFormDialog', () => {
  it('creates viral analysis through the task API with only Seednote projects', async () => {
    renderDialog({ initialProjectId: undefined, initialType: 'viral_analysis' })
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })

    fireEvent.click(within(dialog).getByRole('combobox', { name: '项目上下文' }))
    expect(await screen.findByRole('option', { name: '种草项目' })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '公众号项目' })).not.toBeInTheDocument()
    expect(screen.queryByRole('option', { name: '剪辑项目' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('option', { name: '种草项目' }))

    expect(within(dialog).queryByText('封面比例')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('图像模型')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('任务参考图')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('水印')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('数量')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '添加附件' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByPlaceholderText('粘贴种草笔记链接或分享文本...'), {
      target: { value: 'https://www.xiaohongshu.com/explore/note-1' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith({
      type: 'viral_analysis',
      execution_profile: 'effective',
      prompt: 'https://www.xiaohongshu.com/explore/note-1',
      project_id: 'seednote-project',
      quantity: 1,
      input_attachments: [],
    }))
  })

  it('requires a server-backed execution profile and submits the selected exact-price profile', async () => {
    renderDialog()
    const dialog = await screen.findByRole('dialog')

    expect(await within(dialog).findByRole('button', { name: /^性价比，/ })).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).getByText(/4,800 × 1 =/)).toBeInTheDocument()
    expect(within(dialog).queryByText('在本机运行')).not.toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: /^平衡型，/ }))
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      execution_profile: 'balanced',
    })))
    expect(api.tasks.create).toHaveBeenCalledWith(expect.not.objectContaining({ execution_target: expect.anything() }))
  })

  it('retains the source execution profile when cloning', async () => {
    renderDialog({ mode: 'clone', sourceTask: { ...fixtures.sourceTask, execution_profile: 'quality' } })

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByRole('button', { name: /^极致效果，/ })).toHaveAttribute('aria-pressed', 'true')
    expect(within(dialog).getByText(/18,000 × 1 =/)).toBeInTheDocument()
  })

  it('blocks cloning when the retained execution profile is no longer available', async () => {
    vi.mocked(api.agentProfiles.list).mockResolvedValueOnce([
      { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-v4-flash', description: '适合日常创作', min_tier: 'free', available: true },
      { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
      { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: false, unavailable_reason: 'requires_enterprise' },
    ])
    renderDialog({ mode: 'clone', sourceTask: { ...fixtures.sourceTask, execution_profile: 'quality' } })

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByRole('button', { name: '克隆' })).toBeDisabled()
    expect(within(dialog).getByText('当前执行配置不可用，请重新选择。')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))
    expect(api.tasks.clone).not.toHaveBeenCalled()
  })

  it('shows the authenticated tier price and savings before task creation', async () => {
    vi.mocked(api.billing.catalog).mockResolvedValueOnce({
      catalog_id: 'retail-tiered-v1', currency: 'credits', pricing_tier: 'pro',
      skus: [{
        id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission',
        list_price_credits: 6000, price_credits: 5400, discount_credits: 600, pricing_tier: 'pro',
        delivery: 'article_artifacts_verified',
      }],
    })
    renderDialog()

    expect(await screen.findByText(/专业版任务价：5,400 × 1 =/)).toBeInTheDocument()
    expect(screen.getByText(/每个任务优惠 600 积分/)).toBeInTheDocument()
  })

  it('renders create mode with the shared operational controls and no static type panel', async () => {
    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))
    expect(document.querySelector('[data-slot="agent-prompt-input"]')).toBeInTheDocument()
    expect(screen.getByLabelText('选择附件文件')).toBeInTheDocument()
    expect(within(dialog).getByText('数量')).toBeInTheDocument()
    expect(within(dialog).getAllByRole('radio')).toHaveLength(4)
    expect(within(dialog).getByRole('radio', { name: '16:9 widescreen default' })).toBeChecked()
    const imageModelSelector = within(dialog).getAllByRole('combobox').find((element) => element.textContent?.includes('标准图像'))
    expect(imageModelSelector).toBeDefined()
    fireEvent.click(imageModelSelector!)
    expect(await screen.findByPlaceholderText('搜索图像能力...')).toBeInTheDocument()
    fireEvent.click(imageModelSelector!)
    expect(within(dialog).queryByText('任务参考图')).not.toBeInTheDocument()
    expect(within(dialog).getByText('水印')).toBeInTheDocument()
    expect(within(dialog).getByText('强目标模式')).toBeInTheDocument()
    expect(within(dialog).getByText('正文配图')).toBeInTheDocument()
    expect(within(dialog).queryByText('01 类型')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('选择项目后自动匹配任务类型')).not.toBeInTheDocument()
  })

  it('uses the selected project platform when initial type and project disagree', async () => {
    renderDialog({ initialType: 'seednote' })

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))
    expect(within(dialog).getByText('公众号文章')).toBeInTheDocument()
    expect(within(dialog).getByRole('radio', { name: '16:9 widescreen default' })).toBeChecked()
    expect(within(dialog).getByText('正文配图')).toBeInTheDocument()
    expect(within(dialog).queryByText('尾图')).not.toBeInTheDocument()
  })

  it('waits for active projects and makes the source project platform authoritative for clone defaults', async () => {
    const projectsRequest = deferred<Project[]>()
    vi.mocked(api.projects.list).mockReturnValueOnce(projectsRequest.promise)
    renderDialog({
      mode: 'clone',
      sourceTask: { ...fixtures.sourceTask, type: 'seednote' },
      initialProjectId: undefined,
    })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('')

    await act(async () => projectsRequest.resolve([
      fixtures.articleProject,
      fixtures.seednoteProject,
      fixtures.montageProject,
    ]))

    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))
    expect(within(dialog).getByText('公众号文章')).toBeInTheDocument()
    expect(within(dialog).getByText('正文配图')).toBeInTheDocument()
    expect(within(dialog).queryByText('尾图')).not.toBeInTheDocument()
  })

  it('blocks a clone whose source project is not active until an active project is selected', async () => {
    renderDialog({
      mode: 'clone',
      sourceTask: { ...fixtures.sourceTask, project_id: 'inactive-project' },
      initialProjectId: undefined,
    })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await within(dialog).findByText('源任务项目不可用，请选择一个有效项目。')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeDisabled()
    expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('选择项目')
  })

  it('hydrates clone defaults and removes resume-only attachments', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))
    expect(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('复制后的完整创作要求')
    expect(within(dialog).getByRole('radio', { name: '16:9 widescreen default' })).toBeChecked()
    expect(await within(dialog).findByText('源图像')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '预览 reference.png' })).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '预览 keep.pdf' })).toBeInTheDocument()
    expect(within(dialog).queryByText('resume.txt')).not.toBeInTheDocument()
    expect(within(dialog).getByText('仅生成正文配图；发布草稿不设封面')).toBeInTheDocument()
    expect(within(dialog).getByDisplayValue('必须包含三个案例')).toBeInTheDocument()
  })

  it('submits a full normalized clone request', async () => {
    const { onCreated, onOpenChange } = renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await waitFor(() => expect(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('复制后的完整创作要求'))
    expect(await within(dialog).findByText('源图像')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(api.tasks.clone).toHaveBeenCalledWith('source-task', expect.objectContaining({
      type: 'article',
      execution_profile: 'effective',
      project_id: 'article-project',
      prompt: '复制后的完整创作要求',
      quantity: 1,
      image_ratio: '16:9',
      image_model_key: 'source-model',
      watermark: true,
      goal_mode: true,
      goal: '必须包含三个案例',
      article_with_cover: false,
      article_with_content_images: true,
      input_attachments: [
        expect.objectContaining({
          type: 'image',
          asset_id: '11111111-1111-4111-8111-111111111111',
          file_name: 'reference.png',
        }),
        expect.objectContaining({ upload_id: 'keep-upload', key: 'uploads/pending/keep.pdf' }),
      ],
    })))
    expect(onCreated).toHaveBeenCalledWith(fixtures.createdTask, 1)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('reports the cloned quantity in its success toast', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await within(dialog).findByText('源图像')).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '3' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(toastMocks.success).toHaveBeenCalledWith('已克隆 3 个任务'))
  })

  it('shows and blocks an inherited image model that is no longer available', async () => {
    vi.mocked(api.imageModels.list).mockResolvedValue({
      tier: 'pro',
      items: [
        { key: 'standard_image', display_name: '标准图像', min_tier: 'free', is_custom: false },
        { key: 'destination-model', display_name: '目标图像', min_tier: 'pro', is_custom: false },
      ],
    })
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await within(dialog).findByText('已停用图像能力（当前任务配置）')).toBeInTheDocument()
    expect(within(dialog).getByText('当前图像能力不可用，请重新选择。')).toBeInTheDocument()
    expect(within(dialog).queryByText('source-model')).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeDisabled()
  })

  it('submits create mode and reports the submitted quantity', async () => {
    const { onCreated } = renderDialog()
    await screen.findByRole('dialog', { name: '新建任务' })
    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '创建一篇品牌文章' },
    })
    fireEvent.click(screen.getByRole('button', { name: '3' }))
    fireEvent.click(screen.getByRole('button', { name: '创建 3 个任务' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      project_id: 'article-project',
      prompt: '创建一篇品牌文章',
      quantity: 3,
      image_ratio: '16:9',
    })))
    expect(onCreated).toHaveBeenCalledWith(fixtures.createdTask, 3)
  })

  it('applies destination project defaults when a clone changes project', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await within(dialog).findByText('源图像')

    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: '种草项目' }))

    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('种草项目'))
    expect(screen.getByRole('radio', { name: '3:4 vertical default' })).toBeChecked()
    expect(await screen.findByText('目标图像')).toBeInTheDocument()
    expect(screen.getByText('尾图')).toBeInTheDocument()
    expect(screen.queryByText('仅生成正文配图；发布草稿不设封面')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '预览 keep.pdf' })).toBeInTheDocument()
    expect(screen.getByText('种草笔记仅支持图片附件，请移除其他附件后继续。')).toBeInTheDocument()
    expect(within(screen.getByRole('dialog', { name: '克隆任务' })).getByRole('button', { name: '克隆' })).toBeDisabled()
    expect(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('复制后的完整创作要求')
  })

  it('blocks an incompatible attachment that completes after switching projects', async () => {
    const uploadRequest = deferred<{
      uploadId: string
      key: string
      publicUrl: string
      contentType: string
      size: number
    }>()
    uploadToOSSMock.mockReturnValueOnce(uploadRequest.promise)
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await within(dialog).findByText('源图像')

    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['pending'], 'pending.pdf', { type: 'application/pdf' })] },
    })
    expect(await screen.findByRole('button', { name: '预览 pending.pdf' })).toBeInTheDocument()
    expect(screen.getByRole('status', { name: 'pending.pdf 状态' })).toHaveTextContent('上传中')
    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: '种草项目' }))

    expect(screen.getByRole('button', { name: '预览 pending.pdf' })).toBeInTheDocument()
    await act(async () => uploadRequest.resolve({
      uploadId: 'uploaded-after-switch',
      key: 'uploads/pending/pending.pdf',
      publicUrl: '',
      contentType: 'application/pdf',
      size: 7,
    }))
    await waitFor(() => expect(screen.getByRole('status', { name: 'pending.pdf 状态' })).toHaveTextContent('已上传'))

    expect(screen.getByText('种草笔记仅支持图片附件，请移除其他附件后继续。')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))
    expect(api.tasks.clone).not.toHaveBeenCalled()
  })

  it('rejects new non-image attachments after switching to a Seednote project', async () => {
    renderDialog({ mode: 'clone', sourceTask: { ...fixtures.sourceTask, input_attachments: [] }, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await within(dialog).findByText('源图像')

    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: '种草项目' }))
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['brief'], 'brief.pdf', { type: 'application/pdf' })] },
    })

    expect(screen.queryByRole('button', { name: '预览 brief.pdf' })).not.toBeInTheDocument()
    expect(uploadToOSSMock).not.toHaveBeenCalled()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeEnabled()
  })

  it('keeps a failed attachment visible and blocks submission after switching projects', async () => {
    uploadToOSSMock.mockRejectedValueOnce(new Error('upload failed'))
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await within(dialog).findByText('源图像')

    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['failed'], 'failed.pdf', { type: 'application/pdf' })] },
    })
    expect(await screen.findByRole('alert', { name: 'failed.pdf 状态' })).toHaveTextContent('失败')
    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: '种草项目' }))

    expect(screen.getByRole('button', { name: '预览 failed.pdf' })).toBeInTheDocument()
    expect(screen.getByRole('alert', { name: 'failed.pdf 状态' })).toHaveTextContent('失败')
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))
    expect(api.tasks.clone).not.toHaveBeenCalled()
  })

  it('preserves edited values and remains open after a rejected clone', async () => {
    vi.mocked(api.tasks.clone).mockRejectedValueOnce(new Error('clone failed'))
    const { onCreated, onOpenChange } = renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    const prompt = screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')
    await waitFor(() => expect(prompt).toHaveValue('复制后的完整创作要求'))
    expect(await within(dialog).findByText('源图像')).toBeInTheDocument()
    fireEvent.change(prompt, { target: { value: '编辑后仍需保留' } })
    fireEvent.click(screen.getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(toastMocks.error).toHaveBeenCalled())
    expect(screen.getByRole('dialog', { name: '克隆任务' })).toBeInTheDocument()
    expect(prompt).toHaveValue('编辑后仍需保留')
    expect(onCreated).not.toHaveBeenCalled()
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('fences every close path during the clone request', async () => {
    const cloneRequest = deferred<Task>()
    vi.mocked(api.tasks.clone).mockReturnValueOnce(cloneRequest.promise)
    const { onOpenChange, onCreated } = renderDialog({
      mode: 'clone',
      sourceTask: fixtures.sourceTask,
      initialProjectId: undefined,
    })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await within(dialog).findByText('源图像')).toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(api.tasks.clone).toHaveBeenCalledOnce())
    expect(within(dialog).getByRole('button', { name: '取消' })).toBeDisabled()
    expect(within(dialog).getByRole('button', { name: 'Close' })).toBeDisabled()
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }))
    expect(onOpenChange).not.toHaveBeenCalled()

    await act(async () => cloneRequest.resolve(fixtures.createdTask))
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith(fixtures.createdTask, 1))
    expect(onOpenChange).toHaveBeenCalledTimes(1)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('asks for confirmation before closing a dirty form', async () => {
    renderDialog()
    await screen.findByRole('dialog', { name: '新建任务' })
    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '尚未提交的内容' },
    })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(await screen.findByRole('alertdialog', { name: '放弃编辑？' })).toBeInTheDocument()
  })

  it('uses a label and sibling switch for goal mode without nested interactive controls', async () => {
    renderDialog()
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('公众号项目'))

    const goalModeText = within(dialog).getByText('强目标模式')
    const goalModeLabel = goalModeText.closest('label')
    expect(goalModeLabel).not.toBeNull()
    const goalModeSwitch = within(goalModeLabel!.parentElement!).getByRole('switch')
    expect(goalModeSwitch.parentElement?.closest('button')).toBeNull()
    fireEvent.click(goalModeLabel!)
    expect(goalModeSwitch).toBeChecked()
  })

  it('shows destination Montage defaults and removes incompatible article controls', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await within(dialog).findByText('源图像')
    fireEvent.click(screen.getByRole('combobox', { name: '项目上下文' }))
    fireEvent.click(await screen.findByRole('option', { name: '剪辑项目' }))

    expect(await screen.findByDisplayValue('social-short')).toBeInTheDocument()
    expect(screen.getByLabelText('时长（秒）')).toHaveValue(45)
    expect(screen.queryByText('正文配图')).not.toBeInTheDocument()
    expect(screen.queryByText('任务参考图')).not.toBeInTheDocument()
  })
})
