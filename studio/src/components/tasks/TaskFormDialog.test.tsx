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
const useAgentPacksMock = vi.hoisted(() => vi.fn())

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((next) => { resolve = next })
  return { promise, resolve }
}

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

vi.mock('@/hooks/useAgentPacks', () => ({ useAgentPacks: useAgentPacksMock }))

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
    portrait_reference_image: {
      asset_id: '11111111-1111-4111-8111-111111111111',
      file_name: 'portrait.png',
      content_type: 'image/png',
      size: 8,
      download_url: 'https://cdn.example/portrait.png',
      download_expires_at: '2026-09-13T12:00:00Z',
    },

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
    ecommerce_defaults: { image_capability_key: 'destination-capability' },
  } as Project
  const montageProject = {
    ...articleProject,
    id: 'montage-project',
    platform: 'montage',
    name: '剪辑项目',
    image_ratio: '9:16',
    montage_defaults: {
      default_pipeline: 'cinematic',
      preferences: { duration_seconds: 45, style: 'clean product film' },
      delivery_targets: ['final_video'],
    },
  } as Project
  const momentsProject = {
    ...articleProject,
    id: 'moments-project',
    platform: 'moments',
    name: '朋友圈项目',
    image_ratio: '1:1',
  } as Project
  const ecommerceProject = {
    ...articleProject,
    id: 'ecommerce-project',
    platform: 'ecommerce',
    name: '电商项目',
    image_ratio: '4:3',
  } as Project
  const sourceTask = {
    id: 'source-task',
    type: 'article',
    title: '源文章',
    prompt: '复制后的完整创作要求',
    status: 'completed',
    image_ratio: '16:9',
    image_capability_key: 'source-capability',
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
    project_id: articleProject.id,
    execution_profile: 'effective',
    billing_price_credits: 6000,
    created_at: '2026-07-01T00:00:00.000Z',
    started_at: '2026-07-01T00:01:00.000Z',
    completed_at: '2026-07-01T00:10:00.000Z',
  } as Task
  const createdTask = { ...sourceTask, id: 'created-task', status: 'pending' } as Task
  return { articleProject, seednoteProject, momentsProject, ecommerceProject, montageProject, sourceTask, createdTask }
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
        platformConfigs: vi.fn().mockResolvedValue([
          { id: 'article', default_image_ratio: '16:9', supported_image_ratios: ['16:9', '4:3', '1:1'], fields: [] },
          { id: 'seednote', default_image_ratio: '3:4', supported_image_ratios: ['3:4', '1:1', '4:3'], fields: [] },
          { id: 'moments', default_image_ratio: '3:4', supported_image_ratios: ['3:4', '1:1'], fields: [] },
          { id: 'ecommerce', default_image_ratio: '1:1', supported_image_ratios: ['1:1', '3:4', '4:3', '16:9'], fields: [] },
          { id: 'montage', default_image_ratio: '9:16', supported_image_ratios: ['9:16', '16:9', '1:1'], fields: [] },
        ]),
      },
      templates: {
        ...actual.api.templates,
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
      imageCapabilities: {
        ...actual.api.imageCapabilities,
        list: vi.fn(),
      },
      montageCapabilities: {
        ...actual.api.montageCapabilities,
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

async function findImageSettings(dialog: HTMLElement, summary: string) {
  const trigger = await within(dialog).findByRole('button', { name: /^创作参数：/ })
  for (const part of summary.split(' · ')) {
    expect(trigger).toHaveAccessibleName(expect.stringContaining(part))
  }
  return trigger
}

async function openTaskParameters(dialog: HTMLElement) {
  const trigger = await within(dialog).findByRole('button', { name: /^创作参数：/ })
  if (trigger.getAttribute('aria-expanded') !== 'true') fireEvent.click(trigger)
  return waitFor(() => {
    const popover = document.querySelector<HTMLElement>('[data-slot="popover-content"][data-open]')
    expect(popover).toBeInTheDocument()
    return popover!
  })
}

async function closeOpenPopover() {
  const popover = document.querySelector<HTMLElement>('[data-slot="popover-content"][data-open]')
  if (!popover) return
  fireEvent.keyDown(popover, { key: 'Escape' })
  await waitFor(() => expect(document.querySelector('[data-slot="popover-content"][data-open]')).not.toBeInTheDocument())
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.projects.list).mockResolvedValue([
    fixtures.articleProject,
    fixtures.seednoteProject,
    fixtures.momentsProject,
    fixtures.ecommerceProject,
    fixtures.montageProject,
  ])
  vi.mocked(api.templates.list).mockResolvedValue({
    items: [{
      id: 'template-1', type: 'seednote', name: '清透说明书', category: '美妆护肤',
      thumbnail_url: '', prompt: '模板视觉 Prompt', visibility: 'public', sort_order: 0,
      is_active: true, created_at: '2026-08-02T00:00:00Z', updated_at: '2026-08-02T00:00:00Z',
    }],
    total: 1,
  })
  vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 100000, promotional: 0, debt: 0, balance: 100000 })
  vi.mocked(api.billing.catalog).mockResolvedValue({
    catalog_id: 'retail-test-v1',
    currency: 'credits',
    task_time_pricing: { timezone: 'Asia/Shanghai', peak_windows: [{ start: '09:00', end: '12:00' }, { start: '14:00', end: '18:00' }], off_peak_windows: [{ start: '00:00', end: '09:00' }, { start: '12:00', end: '14:00' }, { start: '18:00', end: '24:00' }], off_peak_rate_percent: 80, current_period: 'peak', server_time: '2026-07-31T10:00:00+08:00', next_transition_at: '2026-07-31T12:00:00+08:00' },
    skus: [
      { id: 'task.article.effective', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4800, peak_price_credits: 4800, off_peak_price_credits: 3840, delivery: 'article_artifacts_verified' },
      { id: 'task.article.balanced', operation: 'task.article', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 6000, delivery: 'article_artifacts_verified' },
      { id: 'task.article.quality', operation: 'task.article', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 18000, delivery: 'article_artifacts_verified' },
      { id: 'task.seednote.effective', operation: 'task.seednote', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 4000, delivery: 'seednote_artifacts_verified' },
      { id: 'task.seednote.balanced', operation: 'task.seednote', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 5000, delivery: 'seednote_artifacts_verified' },
      { id: 'task.moments.effective', operation: 'task.moments', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 3000, delivery: 'moments_artifacts_verified' },
      { id: 'task.ecommerce.effective', operation: 'task.ecommerce', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 3000, delivery: 'ecommerce_artifacts_verified' },
      { id: 'task.viral-analysis.effective', operation: 'task.viral_analysis', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 1200, delivery: 'viral_analysis_report_verified' },
      { id: 'task.viral-analysis.balanced', operation: 'task.viral_analysis', execution_profile: 'balanced', charge_policy: 'task_admission', price_credits: 1200, delivery: 'viral_analysis_report_verified' },
      { id: 'task.viral-analysis.quality', operation: 'task.viral_analysis', execution_profile: 'quality', charge_policy: 'task_admission', price_credits: 1200, delivery: 'viral_analysis_report_verified' },
      { id: 'task.montage.effective', operation: 'task.montage', execution_profile: 'effective', charge_policy: 'task_admission', price_credits: 2000, delivery: 'montage_artifacts_verified' },
    ],
  })
  vi.mocked(api.agentProfiles.list).mockResolvedValue([
    { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-flash', description: '适合日常创作', min_tier: 'free', available: true },
    { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
    { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: true },
  ])
  useAgentPacksMock.mockReturnValue({ data: { packs: [] } })
  vi.mocked(api.imageCapabilities.list).mockResolvedValue({
    tier: 'pro',
    default_capability: 'standard',
    items: [
      { key: 'standard', display_name: '标准图像', min_tier: 'free', enabled: true, price_available: true },
      { key: 'source-capability', display_name: '源图像', min_tier: 'pro', enabled: true, price_available: true },
      { key: 'destination-capability', display_name: '目标图像', min_tier: 'pro', enabled: true, price_available: true },
    ],
  })
  vi.mocked(api.montageCapabilities.list).mockResolvedValue({
    enabled: true,
    default_pipeline: 'cinematic',
    max_duration_seconds: 600,
    max_assets: 20,
    items: [
      {
        key: 'cinematic', display_name: '电影感制作', description: '品牌片、预告片与情绪叙事',
        best_for: ['品牌发布'], source_hint: '可使用视频、图片，也可仅根据创意说明生成',
        output_hint: '一条完整成片', source_requirement: 'optional', output_mode: 'single',
        recommended_duration_seconds: 30,
      },
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
  it('keeps the task form usable when the optional Agent Pack catalog is unavailable', async () => {
    useAgentPacksMock.mockReturnValueOnce({ data: { items: [] } })

    renderDialog()

    expect(await screen.findByRole('dialog', { name: '新建任务' })).toBeInTheDocument()
  })

  it('keeps the article portrait opt-in and submits the project reference choice', async () => {
    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    const portraitSwitch = await within(dialog).findByRole('switch', { name: '使用人物图' })
    expect(portraitSwitch).toHaveAttribute('aria-checked', 'true')
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '使用作者人物图生成文章封面' },
    })
    fireEvent.submit(document.getElementById('task-create-form')!)

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      use_portrait_reference: true,
    })))
  })

  it('disables article portrait when the project has no configured image', async () => {
    vi.mocked(api.projects.list).mockResolvedValueOnce([{ ...fixtures.articleProject, portrait_reference_image: null }])
    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    expect(await within(dialog).findByRole('switch', { name: '使用人物图' })).toHaveAttribute('aria-disabled', 'true')
  })

  it('clears the article portrait when cover generation is turned off', async () => {
    renderDialog()
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    const portraitSwitch = await within(dialog).findByRole('switch', { name: '使用人物图' })

    expect(portraitSwitch).toHaveAttribute('aria-checked', 'true')
    fireEvent.click(within(dialog).getByRole('switch', { name: '生成封面图' }))

    expect(portraitSwitch).toHaveAttribute('aria-disabled', 'true')
    expect(portraitSwitch).toHaveAttribute('aria-checked', 'false')
  })

  it('blocks an article portrait when the selected image capability has no reference slots', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValueOnce({
      tier: 'pro',
      default_capability: 'standard',
      items: [{
        key: 'standard',
        display_name: '标准图像',
        enabled: true,
        price_available: true,
        generation_features: { quality_levels: [], size_presets: ['1:1'], default_size: '1:1', max_batch: 1, max_reference_images: 0, supports_reference: true, supports_mask: false, output_formats: ['png'], has_background: false, has_compression: false, watermark: false },
      }],
    })
    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    const portraitSwitch = await within(dialog).findByRole('switch', { name: '使用人物图' })
    fireEvent.click(portraitSwitch)
    fireEvent.click(portraitSwitch)

    expect(within(dialog).getByText('当前图像能力不支持人物参考，请更换图像能力。')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
  })

  it('在任务创建和克隆时用模板覆盖非空 Prompt 且保留附件', async () => {
    const createView = renderDialog({ initialProjectId: fixtures.seednoteProject.id })
    const createDialog = await screen.findByRole('dialog', { name: '新建任务' })
    const createPrompt = within(createDialog).getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')
    fireEvent.change(createPrompt, { target: { value: '已有创建要求' } })
    fireEvent.click(await within(createDialog).findByRole('button', { name: /清透说明书/ }))
    expect(createPrompt).toHaveValue('模板视觉 Prompt')
    createView.unmount()

    renderDialog({
      mode: 'clone',
      initialProjectId: undefined,
      sourceTask: {
        ...fixtures.sourceTask,
        type: 'seednote',
        project_id: fixtures.seednoteProject.id,
        prompt: '已有克隆要求',
        input_attachments: [{
          type: 'image', upload_id: 'image-upload', key: 'uploads/pending/image.png',
          file_name: 'image.png', content_type: 'image/png', size: 42,
        }],
      },
    })
    const cloneDialog = await screen.findByRole('dialog', { name: '克隆任务' })
    const clonePrompt = within(cloneDialog).getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')
    await waitFor(() => expect(clonePrompt).toHaveValue('已有克隆要求'))
    expect(within(cloneDialog).getAllByText('image.png').length).toBeGreaterThan(0)
    fireEvent.click(await within(cloneDialog).findByRole('button', { name: /清透说明书/ }))
    expect(clonePrompt).toHaveValue('模板视觉 Prompt')
    expect(within(cloneDialog).getAllByText('image.png').length).toBeGreaterThan(0)
  })

  it('shows configuration-driven off-peak windows and current task prices', async () => {
    renderDialog()
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    expect(await within(dialog).findByText('当前选择为高峰时段 · 预计 4800 积分/次')).toBeInTheDocument()
    expect(within(dialog).getByText('Asia/Shanghai 低峰：00:00–09:00、12:00–14:00、18:00–24:00；低峰价格为高峰会员价的 80%')).toBeInTheDocument()
    expect(within(dialog).getByText('高峰 4800 积分 · 低峰 3840 积分 · 低峰可节省 960 积分')).toBeInTheDocument()
  })

  it('creates viral analysis through the task API with only Seednote projects', async () => {
    renderDialog({ initialProjectId: undefined, initialType: 'viral_analysis' })
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })

    const projectControl = within(dialog).getByRole('combobox', { name: /^项目/ })
    await waitFor(() => expect(projectControl).toBeEnabled())
    fireEvent.click(projectControl)
    expect(await screen.findByRole('option', { name: /种草项目/ })).toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /公众号项目/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('option', { name: /剪辑项目/ })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('option', { name: /种草项目/ }))

    const viralPrompt = dialog.querySelector<HTMLElement>('[data-slot="viral-analysis-prompt"]')
    expect(viralPrompt).toBeInTheDocument()
    const viralTextarea = within(viralPrompt!).getByRole('textbox', { name: '源笔记链接或分享文本' })
    const viralProject = within(viralPrompt!).getByRole('combobox', { name: '项目：种草项目' })
    const viralParameters = within(viralPrompt!).getByRole('button', { name: /^创作参数：/ })
    expect(viralProject.closest('[data-slot="project-context-control"]')).toHaveAttribute('data-compact', 'true')
    expect(viralTextarea.compareDocumentPosition(viralProject) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(viralProject.compareDocumentPosition(viralParameters) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    expect(within(dialog).queryByText('参考模板')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: /清透说明书/ })).not.toBeInTheDocument()
    expect(api.templates.list).not.toHaveBeenCalled()

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
      agent_input: {},
    }))
  })

  it('does not load templates when cloning a viral analysis task', async () => {
    renderDialog({
      mode: 'clone',
      initialProjectId: undefined,
      sourceTask: {
        ...fixtures.sourceTask,
        type: 'viral_analysis',
        project_id: fixtures.seednoteProject.id,
        prompt: 'https://www.xiaohongshu.com/explore/source-note',
        input_attachments: [],
      },
    })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await within(dialog).findByPlaceholderText('粘贴种草笔记链接或分享文本...')).toHaveValue('https://www.xiaohongshu.com/explore/source-note')
    expect(within(dialog).queryByText('参考模板')).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: /清透说明书/ })).not.toBeInTheDocument()
    expect(api.templates.list).not.toHaveBeenCalled()
  })

  it('requires a server-backed execution profile and submits the selected exact-price profile', async () => {
    renderDialog()
    const dialog = await screen.findByRole('dialog')

    const executionControl = await within(dialog).findByRole('button', { name: /^创作参数：.*性价比/ })
    expect(within(dialog).getByText(/4,800 × 1 =/)).toBeInTheDocument()
    expect(within(dialog).queryByText('在本机运行')).not.toBeInTheDocument()

    fireEvent.click(executionControl)
    fireEvent.click(await screen.findByRole('button', { name: /^平衡型，/ }))
    await closeOpenPopover()
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      execution_profile: 'balanced',
    })))
    expect(api.tasks.create).toHaveBeenCalledWith(expect.not.objectContaining({ execution_target: expect.anything() }))
  })

  it('retains the source execution profile when cloning', async () => {
    renderDialog({ mode: 'clone', sourceTask: { ...fixtures.sourceTask, execution_profile: 'quality' } })

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByRole('button', { name: /^创作参数：.*极致效果/ })).toBeInTheDocument()
    expect(within(dialog).getByText(/18,000 × 1 =/)).toBeInTheDocument()
  })

  it('blocks cloning when the retained execution profile is no longer available', async () => {
    vi.mocked(api.agentProfiles.list).mockResolvedValueOnce([
      { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-flash', description: '适合日常创作', min_tier: 'free', available: true },
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
    const composer = dialog.querySelector<HTMLElement>('[data-slot="agent-prompt-input"]')
    expect(composer).toBeInTheDocument()
    const parametersControl = await within(composer!).findByRole('button', { name: /^创作参数：/ })
    const projectControl = await within(composer!).findByRole('combobox', { name: '项目：公众号项目' })
    expect(projectControl.compareDocumentPosition(parametersControl) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(composer!).queryByRole('button', { name: /^执行配置：/ })).not.toBeInTheDocument()
    expect(within(composer!).queryByRole('button', { name: /^图像设置：/ })).not.toBeInTheDocument()
    expect(within(composer!).queryByRole('button', { name: '任务数量：1' })).not.toBeInTheDocument()
    expect(projectControl.closest('[data-slot="project-context-control"]')).toHaveAttribute('data-compact', 'true')
    expect(composer!.querySelector('[data-slot="agent-prompt-context"]')).not.toBeInTheDocument()
    expect(screen.getByLabelText('选择附件文件')).toBeInTheDocument()
    expect(within(dialog).queryByText('数量', { exact: true })).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('group', { name: 'Agent 执行配置' })).not.toBeInTheDocument()
    expect(within(dialog).queryByRole('radio')).not.toBeInTheDocument()
    const parameters = await openTaskParameters(dialog)
    const ratioGroup = within(parameters).getByRole('group', { name: '图片比例' })
    expect(within(ratioGroup).getByRole('button', { name: '智能适配' })).toBeInTheDocument()
    expect(within(ratioGroup).getByRole('button', { name: '16:9' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(ratioGroup).getByRole('button', { name: '4:3' })).toBeInTheDocument()
    expect(within(ratioGroup).getByRole('button', { name: '1:1' })).toBeInTheDocument()
    expect(within(ratioGroup).queryByRole('button', { name: '3:4' })).not.toBeInTheDocument()
    expect(within(parameters).getByRole('group', { name: '图像能力' })).toBeInTheDocument()
    expect(within(dialog).queryByText('任务参考图')).not.toBeInTheDocument()
    expect(within(dialog).getByText('水印')).toBeInTheDocument()
    expect(within(dialog).getByText('仅在所选图像能力支持水印时生效')).toBeInTheDocument()
    expect(within(dialog).queryByText(/火山引擎/)).not.toBeInTheDocument()
    expect(within(dialog).queryByText('强目标模式')).not.toBeInTheDocument()
    expect(within(dialog).getByText('正文配图')).toBeInTheDocument()
    expect(within(dialog).queryByText('01 类型')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('选择项目后自动匹配任务类型')).not.toBeInTheDocument()
  })

  it('shows the catalog default capability when the form value is empty', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValueOnce({
      tier: 'pro',
      default_capability: 'catalog-default',
      items: [
        { key: 'first-sorted', display_name: '首个排序能力', min_tier: 'free', sort_order: 1, enabled: true, price_available: true },
        { key: 'catalog-default', display_name: '目录默认能力', min_tier: 'free', sort_order: 2, enabled: true, price_available: true },
      ],
    })

    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    expect(await findImageSettings(dialog, '16:9 · 目录默认能力')).toBeInTheDocument()
  })

  it('submits the catalog default capability shown for an empty form value', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValueOnce({
      tier: 'pro',
      default_capability: 'catalog-default',
      items: [
        { key: 'catalog-default', display_name: '目录默认能力', min_tier: 'free', enabled: true, price_available: true },
      ],
    })

    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    await findImageSettings(dialog, '16:9 · 目录默认能力')
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '创建一篇品牌文章' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      image_capability_key: 'catalog-default',
    })))
  })

  it('uses the selected project platform when initial type and project disagree', async () => {
    renderDialog({ initialType: 'seednote' })

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    const projectControl = await within(dialog).findByRole('combobox', { name: '项目：公众号项目' })
    expect(projectControl).toHaveTextContent('公众号项目')
    expect(await findImageSettings(dialog, '16:9 · 标准图像')).toBeInTheDocument()
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
      fixtures.momentsProject,
      fixtures.ecommerceProject,
      fixtures.montageProject,
    ]))

    const projectControl = await within(dialog).findByRole('combobox', { name: '项目：公众号项目' })
    expect(projectControl).toHaveTextContent('公众号项目')
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
    expect(within(dialog).getByRole('combobox', { name: '项目：未选择' })).toHaveTextContent('选择项目')
  })

  it('hydrates clone defaults and removes resume-only attachments', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目：公众号项目' })).toHaveTextContent('公众号项目'))
    expect(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('复制后的完整创作要求')
    expect(await findImageSettings(dialog, '16:9 · 源图像')).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '预览 reference.png' })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '预览 keep.pdf' })).toBeInTheDocument()
    expect(within(dialog).queryByText('resume.txt')).not.toBeInTheDocument()
    expect(within(dialog).getByText('仅生成正文配图；发布草稿不设封面')).toBeInTheDocument()
  })

  it('submits a full normalized clone request', async () => {
    const { onCreated, onOpenChange } = renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await waitFor(() => expect(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('复制后的完整创作要求'))
    expect(await findImageSettings(dialog, '16:9 · 源图像')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(api.tasks.clone).toHaveBeenCalledWith('source-task', expect.objectContaining({
      type: 'article',
      execution_profile: 'effective',
      project_id: 'article-project',
      prompt: '复制后的完整创作要求',
      quantity: 1,
      image_ratio: '16:9',
      image_capability_key: 'source-capability',
      watermark: true,
      article_with_cover: false,
      article_with_content_images: true,
      input_attachments: [
        expect.objectContaining({ upload_id: 'keep-upload', key: 'uploads/pending/keep.pdf' }),
      ],
    })))
    expect(onCreated).toHaveBeenCalledWith(fixtures.createdTask, 1)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('reports the cloned quantity in its success toast', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await findImageSettings(dialog, '16:9 · 源图像')).toBeInTheDocument()

    const quantityParameters = await openTaskParameters(dialog)
    const increment = within(quantityParameters).getByRole('button', { name: '增加任务数量' })
    fireEvent.click(increment)
    fireEvent.click(increment)
    await closeOpenPopover()
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(toastMocks.success).toHaveBeenCalledWith('已克隆 3 个任务'))
  })

  it('shows and blocks an inherited image model that is no longer available', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValue({
      tier: 'pro',
      default_capability: 'standard',
      items: [
        { key: 'standard', display_name: '标准图像', min_tier: 'free' },
        { key: 'destination-capability', display_name: '目标图像', min_tier: 'pro' },
      ],
    })
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await findImageSettings(dialog, '16:9 · 已停用图像能力（当前任务配置）')).toBeInTheDocument()
    expect(within(dialog).getByText('当前图像能力不可用，请重新选择。')).toBeInTheDocument()
    expect(within(dialog).queryByText('source-capability')).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeDisabled()
  })

  it('blocks an inherited image capability whose price is unavailable', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValue({
      tier: 'pro',
      default_capability: 'standard',
      items: [
        { key: 'standard', display_name: '标准图像', enabled: true, price_available: true },
        { key: 'source-capability', display_name: '源图像', enabled: true, price_available: false },
      ],
    })
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await within(dialog).findByText('当前图像能力不可用，请重新选择。')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeDisabled()
  })

  it('blocks an inherited image capability whose enabled flag is missing', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValue({
      tier: 'pro',
      default_capability: 'standard',
      items: [
        { key: 'standard', display_name: '标准图像', enabled: true, price_available: true },
        { key: 'source-capability', display_name: '源图像', price_available: true },
      ],
    })
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    expect(await within(dialog).findByText('当前图像能力不可用，请重新选择。')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeDisabled()
  })

  it('shows an image capability query failure and blocks submission', async () => {
    vi.mocked(api.imageCapabilities.list).mockRejectedValueOnce(new Error('capability unavailable'))
    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    expect(await within(dialog).findByText('图像能力暂时无法加载，请稍后重试。')).toBeInTheDocument()
    const failedParameters = await openTaskParameters(dialog)
    for (const button of within(within(failedParameters).getByRole('group', { name: '图片比例' })).getAllByRole('button')) {
      expect(button).toBeDisabled()
    }
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
  })

  it('keeps submission blocked while required image capabilities are loading', async () => {
    const capabilitiesRequest = deferred<Awaited<ReturnType<typeof api.imageCapabilities.list>>>()
    vi.mocked(api.imageCapabilities.list).mockReturnValueOnce(capabilitiesRequest.promise)
    renderDialog()

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    expect(await within(dialog).findByText('正在加载图像能力，请稍候。')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()

    await act(async () => capabilitiesRequest.resolve({
      tier: 'pro',
      default_capability: 'standard',
      items: [{ key: 'standard', display_name: '标准图像', min_tier: 'free', enabled: true, price_available: true }],
    }))
  })

  it('does not filter task business ratios by generation fixed-size presets', async () => {
    vi.mocked(api.imageCapabilities.list).mockResolvedValue({
      tier: 'pro',
      default_capability: 'standard',
      items: [
        { key: 'standard', display_name: '标准图像', enabled: true, price_available: true },
        {
          key: 'source-capability',
          display_name: '源图像',
          enabled: true,
          price_available: true,
          generation_features: { quality_levels: [], size_presets: ['1:1'], default_size: '1:1', max_batch: 1, max_reference_images: 1, supports_reference: true, supports_mask: false, output_formats: ['png'], has_background: false, has_compression: false, watermark: false },
        },
      ],
    })
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })

    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    const imageSettings = await findImageSettings(dialog, '16:9 · 源图像')
    fireEvent.click(imageSettings)
    const ratioGroup = await screen.findByRole('group', { name: '图片比例' })
    expect(within(ratioGroup).getByRole('button', { name: '16:9' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.queryByText('当前图像能力不支持所选比例，请重新选择比例或智能适配。')).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '克隆' })).toBeEnabled()
  })

  it('submits explicit auto instead of falling back to the project default ratio', async () => {
    renderDialog()
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    const imageSettings = await findImageSettings(dialog, '16:9 · 标准图像')
    fireEvent.click(imageSettings)
    fireEvent.click(within(await screen.findByRole('group', { name: '图片比例' })).getByRole('button', { name: '智能适配' }))
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '让 Agent 根据内容决定比例' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '创建' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      image_ratio: 'auto',
    })))
  })

  it('submits create mode and reports the submitted quantity', async () => {
    const { onCreated } = renderDialog()
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目：公众号项目' })).toHaveTextContent('公众号项目'))
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '创建一篇品牌文章' },
    })
    const createParameters = await openTaskParameters(dialog)
    fireEvent.click(within(createParameters).getByRole('button', { name: '增加任务数量' }))
    await closeOpenPopover()

    expect(within(dialog).getByText('9,600').closest('p')).toHaveTextContent('固定任务价：4,800 × 2 = 9,600 积分')
    expect(within(dialog).getByText('90,400').closest('p')).toHaveTextContent('余额：100,000 → 90,400')
    fireEvent.click(within(dialog).getByRole('button', { name: '创建 2 个任务' }))

    await waitFor(() => expect(api.tasks.create).toHaveBeenCalledWith(expect.objectContaining({
      project_id: 'article-project',
      prompt: '创建一篇品牌文章',
      quantity: 2,
      image_ratio: '16:9',
    })))
    expect(onCreated).toHaveBeenCalledWith(fixtures.createdTask, 2)
  })

  it('keeps supported task quantities up to five and clamps authoritative task types to one', async () => {
    renderDialog()
    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    const selectProject = async (name: RegExp) => {
      fireEvent.click(await within(dialog).findByRole('combobox', { name: /^项目：/ }))
      fireEvent.click(await screen.findByRole('option', { name }))
    }

    const batchParameters = await openTaskParameters(dialog)
    const increment = within(batchParameters).getByRole('button', { name: '增加任务数量' })
    fireEvent.click(increment)
    fireEvent.click(increment)
    fireEvent.click(increment)
    fireEvent.click(increment)
    expect(increment).toBeDisabled()
    await closeOpenPopover()

    await selectProject(/种草项目/)
    expect(await within(dialog).findByRole('button', { name: /^创作参数：.*任务数量 5/ })).toBeInTheDocument()
    await selectProject(/朋友圈项目/)
    expect(await within(dialog).findByRole('button', { name: /^创作参数：.*任务数量 5/ })).toBeInTheDocument()

    await selectProject(/电商项目/)
    expect(await within(dialog).findByRole('button', { name: /^创作参数：.*任务数量 1/ })).toBeInTheDocument()
    const ecommerceParameters = await openTaskParameters(dialog)
    expect(within(ecommerceParameters).getByLabelText('任务数量：1，当前能力上限')).toHaveTextContent('任务数量 1 · 当前能力上限')
    expect(within(ecommerceParameters).queryByRole('button', { name: '增加任务数量' })).not.toBeInTheDocument()
    await closeOpenPopover()

    await selectProject(/剪辑项目/)
    expect(await within(dialog).findByRole('button', { name: /^创作参数：.*任务数量 1/ })).toBeInTheDocument()
    const montageParameters = await openTaskParameters(dialog)
	  expect(within(montageParameters).getByRole('group', { name: '视频比例' })).toBeInTheDocument()
	  expect(within(montageParameters).getByRole('button', { name: '9:16' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(montageParameters).getByLabelText('任务数量：1，当前能力上限')).toBeInTheDocument()
    expect(within(montageParameters).queryByRole('button', { name: '增加任务数量' })).not.toBeInTheDocument()
  })

  it('blocks Montage submission when the video type catalog is unavailable', async () => {
    vi.mocked(api.montageCapabilities.list).mockRejectedValueOnce(new Error('catalog unavailable'))
    renderDialog({ initialProjectId: fixtures.montageProject.id, initialType: 'montage' })

    const dialog = await screen.findByRole('dialog', { name: '新建任务' })
    expect(await within(dialog).findByText('视频类型加载失败，暂时无法创建视频')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '创建' })).toBeDisabled()
  })

  it('applies destination project defaults when a clone changes project', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await findImageSettings(dialog, '16:9 · 源图像')

    fireEvent.click(screen.getByRole('combobox', { name: /^项目：/ }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))

    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目：种草项目' })).toHaveTextContent('种草项目'))
    expect(await findImageSettings(dialog, '3:4 · 目标图像')).toBeInTheDocument()
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
    await findImageSettings(dialog, '16:9 · 源图像')

    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['pending'], 'pending.pdf', { type: 'application/pdf' })] },
    })
    expect(await screen.findByRole('button', { name: '预览 pending.pdf' })).toBeInTheDocument()
    expect(screen.getByRole('status', { name: 'pending.pdf 状态' })).toHaveTextContent('上传中')
    fireEvent.click(screen.getByRole('combobox', { name: /^项目：/ }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))

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
    await findImageSettings(dialog, '16:9 · 源图像')

    fireEvent.click(screen.getByRole('combobox', { name: /^项目：/ }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))
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
    await findImageSettings(dialog, '16:9 · 源图像')

    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [new File(['failed'], 'failed.pdf', { type: 'application/pdf' })] },
    })
    expect(await screen.findByRole('alert', { name: 'failed.pdf 状态' })).toHaveTextContent('失败')
    fireEvent.click(screen.getByRole('combobox', { name: /^项目：/ }))
    fireEvent.click(await screen.findByRole('option', { name: /种草项目/ }))

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
    expect(await findImageSettings(dialog, '16:9 · 源图像')).toBeInTheDocument()
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
    expect(await findImageSettings(dialog, '16:9 · 源图像')).toBeInTheDocument()
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
    await waitFor(() => expect(screen.getByRole('combobox', { name: '项目：公众号项目' })).toHaveTextContent('公众号项目'))
    fireEvent.change(screen.getByPlaceholderText('描述创作目标、内容要求和素材使用方式...'), {
      target: { value: '尚未提交的内容' },
    })
    fireEvent.click(screen.getByRole('button', { name: '取消' }))
    expect(await screen.findByRole('alertdialog', { name: '放弃编辑？' })).toBeInTheDocument()
  })

  it('shows destination Montage defaults and removes incompatible article controls', async () => {
    renderDialog({ mode: 'clone', sourceTask: fixtures.sourceTask, initialProjectId: undefined })
    const dialog = await screen.findByRole('dialog', { name: '克隆任务' })
    await findImageSettings(dialog, '16:9 · 源图像')
    fireEvent.click(screen.getByRole('combobox', { name: /^项目：/ }))
    fireEvent.click(await screen.findByRole('option', { name: /剪辑项目/ }))

    expect(await screen.findByRole('radio', { name: /电影感制作/ })).toBeChecked()
    expect(screen.getByLabelText('目标时长（秒）')).toHaveValue(45)
    expect(screen.queryByText('正文配图')).not.toBeInTheDocument()
    expect(screen.queryByText('任务参考图')).not.toBeInTheDocument()
  })
})
