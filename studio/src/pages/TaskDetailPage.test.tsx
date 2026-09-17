import { act, fireEvent, render as renderWithoutProviders, screen, waitFor, within } from '@testing-library/react'
import type { PropsWithChildren } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { ThemeProvider } from 'next-themes'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TaskDetailPage from './TaskDetailPage'
import { createTestQueryClient, render } from '@/test/test-utils'
import { mockBillingCatalog, mockBillingWallet, mockProjectDetail, mockProjects, mockTasks } from '@/test/mocks/handlers'
import type { Task, TaskFile } from '@/types'
import { api } from '@/lib/api'
import { AgentPromptDropProvider } from '@/components/agent-prompt/AgentPromptDropProvider'

const mockNavigate = vi.fn()
const routeState = vi.hoisted(() => ({ taskId: 'task-1' }))
const mockStreamTaskProgress = vi.hoisted(() => vi.fn<typeof import('@/lib/sse').streamTaskProgress>())
const uploadToOSSMock = vi.hoisted(() => vi.fn())
const resolveDownloadUrlMock = vi.hoisted(() => vi.fn())
const toastSuccessMock = vi.hoisted(() => vi.fn())
const toastErrorMock = vi.hoisted(() => vi.fn())

vi.mock('sonner', () => ({
  toast: { error: toastErrorMock, message: vi.fn(), success: toastSuccessMock },
}))

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

vi.mock('@/lib/api/uploads', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api/uploads')>('@/lib/api/uploads')
  return {
    ...actual,
		uploadsApi: { ...actual.uploadsApi, resolveDownloadUrl: resolveDownloadUrlMock },
	}
})

const mockReferenceUsageSummary = vi.hoisted(() => vi.fn(({ task }: {
  task: { title?: string; input_attachments?: Array<{ file_name?: string }> }
}) => {
  if (task.title === '触发摘要组件失败') {
    throw new Error('summary render failed')
  }
  return (
    <div>
      <p>未生成素材使用结论，仅展示任务输入。</p>
      {task.input_attachments?.map((attachment) => (
        <p key={attachment.file_name}>{attachment.file_name}</p>
      ))}
    </div>
  )
}))

vi.mock('@/components/tasks/ReferenceUsageSummary', () => ({
  default: mockReferenceUsageSummary,
  ReferenceUsageSummary: mockReferenceUsageSummary,
}))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return {
    ...actual,
    useParams: () => ({ id: routeState.taskId }),
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ token: 'test-token' }),
}))

vi.mock('@/lib/sse', async () => {
  const actual = await vi.importActual<typeof import('@/lib/sse')>('@/lib/sse')
  return {
    ...actual,
    streamTaskProgress: mockStreamTaskProgress,
  }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  const { mockProjectDetail } = await vi.importActual<typeof import('@/test/mocks/handlers')>('@/test/mocks/handlers')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        get: vi.fn(),
        cancel: vi.fn(),
        clone: vi.fn(),
        resume: vi.fn(),
        delete: vi.fn(),
        getWechatPublication: vi.fn(),
        reconcileWechat: vi.fn(),
        publishWechat: vi.fn(),
        selectWechatArticle: vi.fn(),
        files: vi.fn().mockResolvedValue([]),
        downloadZipBlob: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        get: vi.fn().mockResolvedValue(mockProjectDetail),
        list: vi.fn(),
        platformConfigs: vi.fn().mockResolvedValue([]),
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
      seednoteAnalytics: {
        ...actual.api.seednoteAnalytics,
        getByTask: vi.fn(),
      },
      wechatAnalytics: {
        ...actual.api.wechatAnalytics,
        getByTask: vi.fn(),
      },
      channelsAnalytics: {
        ...actual.api.channelsAnalytics,
        getByTask: vi.fn(),
      },
      feedback: {
        ...actual.api.feedback,
        getTask: vi.fn(),
        saveTask: vi.fn(),
      },
    },
  }
})

function mockTask(task: Task) {
  vi.mocked(api.tasks.get).mockResolvedValue(task)
}

function taskWith(overrides: Partial<Task>): Task {
  const status = overrides.status ?? mockTasks.items[0].status
  const stageState = status === 'running' ? 'active'
    : status === 'completed' ? 'complete'
      : status === 'failed' ? 'failed'
        : status === 'cancelled' ? 'cancelled' : 'pending'
  return {
    ...mockTasks.items[0],
    progress_log: undefined,
    lifecycle: {
      version: 1,
      revision: 1,
      execution_id: 'execution-1',
      updated_at: '2026-09-17T08:00:00Z',
      stages: [{ id: 'creation', title: '完成创作', source: 'agent', kind: 'work', state: stageState }],
    },
    workflow_status: {
      version: '1',
      current_stage: 'writing',
      stages: [
        { key: 'writing', label: '写作', status: 'running' },
        { key: 'review', label: '复盘', status: 'pending' },
      ],
    },
    ...overrides,
  }
}

function renderWithCachedTasks(...tasks: Task[]) {
  const queryClient = createTestQueryClient()
  queryClient.setQueryDefaults(['task'], { gcTime: Infinity, staleTime: Infinity })
  tasks.forEach((task) => queryClient.setQueryData(['task', task.id], task))

  function CachedTaskProviders({ children }: PropsWithChildren) {
    return (
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
            <AgentPromptDropProvider>{children}</AgentPromptDropProvider>
          </ThemeProvider>
        </BrowserRouter>
      </QueryClientProvider>
    )
  }

  return {
    ...renderWithoutProviders(<TaskDetailPage />, { wrapper: CachedTaskProviders }),
    queryClient,
  }
}

function waitForAbort(signal: AbortSignal) {
  if (signal.aborted) return Promise.resolve()
  return new Promise<void>((resolve) => signal.addEventListener('abort', () => resolve(), { once: true }))
}

function textOccurrences(container: HTMLElement, text: string) {
  return Math.max(0, (container.textContent?.split(text).length ?? 1) - 1)
}

async function openTaskDetails(tab?: '概览' | '配置' | '素材' | '日志') {
  fireEvent.click(await screen.findByRole('button', { name: '更多详情' }))
  expect(await screen.findByRole('heading', { name: '任务详情' })).toBeInTheDocument()
  if (tab) {
    fireEvent.click(screen.getByRole('tab', { name: tab }))
  }
  expect(screen.getByRole('tab', { name: tab ?? '概览' })).toHaveAttribute('aria-selected', 'true')
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, reject, resolve }
}

async function openResumeDialog() {
  fireEvent.click(await screen.findByRole('button', { name: '继续执行' }))
  const title = await screen.findByRole('heading', { name: '继续执行此任务' })
  return title.closest('[data-slot="dialog-content"]') as HTMLElement
}

async function openCloneDialog() {
  fireEvent.click(await screen.findByRole('button', { name: '克隆任务' }))
  const title = await screen.findByRole('heading', { name: '克隆任务' })
  return title.closest('[data-slot="dialog-content"]') as HTMLElement
}

describe('TaskDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    routeState.taskId = 'task-1'
    mockStreamTaskProgress.mockImplementation(async function* (_taskId, _token, signal) {
      if (signal) await waitForAbort(signal)
    })
    vi.mocked(api.tasks.files).mockResolvedValue([])
    vi.mocked(api.tasks.clone).mockResolvedValue(taskWith({ id: 'task-clone', status: 'pending' }))
    vi.mocked(api.tasks.resume).mockResolvedValue(taskWith({ id: 'task-1', status: 'pending' }))
    vi.mocked(api.tasks.cancel).mockResolvedValue(undefined)
    vi.mocked(api.tasks.delete).mockResolvedValue(undefined)
    vi.mocked(api.tasks.getWechatPublication).mockResolvedValue({
      id: 'publication-1',
      task_id: 'task-1',
      project_id: 'ch-1',
      source: 'wechat_console',
      status: 'drafted',
      draft_media_id: 'draft-media-1',
      draft_title: '测试文章',
    })
    vi.mocked(api.tasks.downloadZipBlob).mockResolvedValue(new Blob(['zip'], { type: 'application/zip' }))
    vi.mocked(api.feedback.getTask).mockResolvedValue(null)
    vi.mocked(api.feedback.saveTask).mockResolvedValue({
      id: 'feedback-1', task_id: 'task-1', user_id: 'user-1', rating: 4, content: '', created_at: '', updated_at: '',
    })
    vi.mocked(api.projects.get).mockResolvedValue(mockProjectDetail)
    vi.mocked(api.projects.list).mockResolvedValue(mockProjects)
    vi.mocked(api.billing.wallet).mockResolvedValue(mockBillingWallet)
    vi.mocked(api.billing.catalog).mockResolvedValue(mockBillingCatalog)
    vi.mocked(api.agentProfiles.list).mockResolvedValue([
      { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-flash', description: '适合日常创作', min_tier: 'free', available: true },
      { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
      { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: true },
    ])
    vi.mocked(api.imageCapabilities.list).mockResolvedValue({
      tier: 'pro',
      default_capability: 'standard',
      items: [
      {
        key: 'standard', display_name: '标准图像', min_tier: 'free', enabled: true,
        price_available: true, price_credits: 500,
        generation_features: {
          quality_levels: [], size_presets: ['1:1', '3:4', '16:9'], default_size: '1:1',
          max_batch: 1, max_reference_images: 1, supports_reference: true, supports_mask: false,
          output_formats: ['png'], has_background: false, has_compression: false, watermark: false,
        },
      },
      {
        key: 'source-capability', display_name: '源图像', min_tier: 'pro', enabled: true,
        price_available: true, price_credits: 500,
        generation_features: {
          quality_levels: [], size_presets: ['1:1', '3:4', '16:9'], default_size: '1:1',
          max_batch: 1, max_reference_images: 1, supports_reference: true, supports_mask: false,
          output_formats: ['png'], has_background: false, has_compression: false, watermark: false,
        },
      },
      ],
    })
    vi.mocked(api.seednoteAnalytics.getByTask).mockResolvedValue({ series: [] })
    vi.mocked(api.wechatAnalytics.getByTask).mockResolvedValue({ series: [] })
    vi.mocked(api.channelsAnalytics.getByTask).mockResolvedValue({ series: [] })
    uploadToOSSMock.mockImplementation(async ({ file }: { file: File }) => ({
      uploadId: `upload-${file.name}`,
      key: `uploads/pending/user-1/upload-${file.name}/${file.name}`,
      publicUrl: `/api/v1/files/uploads/pending/user-1/upload-${file.name}/${file.name}`,
      contentType: file.type,
      size: file.size,
    }))
    resolveDownloadUrlMock.mockResolvedValue({
      url: 'https://cdn.example.com/signed-input.png',
      expires_at: '2026-07-17T12:00:00Z',
    })
  })

  it('renders independent terminal outcome warnings and sanitized provider diagnostics', async () => {
    mockTask(taskWith({
      status: 'completed',
      outcome: {
        core_delivery: { status: 'complete' },
        visual: { status: 'partial' },
        review: { status: 'warning' },
        publication: { status: 'skipped' },
        warnings: [
          { code: 'visual_partial', stage: 'visual', message: '请求的正文配图未全部生成。' },
          { code: 'artifact_upload_failed', stage: 'artifact_upload', message: '文件 output/img_01.png 上传失败：HTTP 503' },
        ],
        diagnostic: {
          provider: 'deepseek',
          provider_code: 'content_exists_risk',
          http_status: 400,
          stage: 'writing',
          content_direction: 'unknown',
          recoverable: true,
          resume_point: 'provider_request',
          request_id: 'request-400',
          summary: '供应商内容安全策略拒绝了请求。供应商未披露具体片段，也未说明发生在输入还是输出。',
        },
      },
    }))

    render(<TaskDetailPage />)

    fireEvent.click(await screen.findByRole('button', { name: '完成创作，已完成' }))
    expect(screen.getByLabelText('内容验收结果')).toHaveTextContent('有警告')
    expect(screen.getByLabelText('内容验收结果')).toHaveTextContent('请求的正文配图未全部生成。')
    expect(screen.getByLabelText('内容验收结果')).toHaveTextContent('文件 output/img_01.png 上传失败：HTTP 503')
    expect(screen.queryByText('内容已完成，发布待处理')).not.toBeInTheDocument()
    await openTaskDetails('概览')
    const provider = screen.getByRole('region', { name: '供应商诊断' })
    expect(provider).toHaveTextContent('deepseek')
    expect(provider).toHaveTextContent('HTTP 400')
    expect(provider).toHaveTextContent('content_exists_risk')
    expect(provider).toHaveTextContent('写作')
    expect(provider).toHaveTextContent('供应商未说明')
    expect(provider).toHaveTextContent('请求 ID 指纹')
    expect(provider).toHaveTextContent('request-400')
    expect(provider).toHaveTextContent('供应商未披露具体片段')
  })

  it('shows the active lifecycle stage without percentage or tool noise', async () => {
    mockTask(taskWith({
      status: 'running',
      progress_log: '准备素材\nUsing tool: Read',
      lifecycle: {
        version: 1, revision: 3, execution_id: 'execution-1', updated_at: '2026-09-17T08:00:00Z',
        stages: [
          { id: 'research', title: '研究素材', source: 'agent', kind: 'work', state: 'complete' },
          { id: 'writing', title: '正在写作正文', source: 'agent', kind: 'work', state: 'active', latest_update: '正在优化标题与段落结构' },
          { id: 'review', title: '质量复核', source: 'agent', kind: 'work', state: 'pending' },
        ],
      },
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    const progressSection = (await screen.findByRole('button', { name: '正在写作正文，进行中' })).closest('section') as HTMLElement
    expect(within(progressSection).getByText('正在优化标题与段落结构')).toBeInTheDocument()
    expect(within(progressSection).queryByText('Using tool: Read')).not.toBeInTheDocument()
    expect(within(progressSection).queryByText(/%/)).not.toBeInTheDocument()
    expect(screen.queryByText('创作进度')).not.toBeInTheDocument()
    expect(screen.queryByText('当前阶段')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '任务结果' })).toBeInTheDocument()
    expect(screen.getByText('结果生成后将在这里显示')).toBeInTheDocument()
    expect(screen.queryByText('任务配置')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '执行动态' })).not.toBeInTheDocument()
    expect(screen.queryByText('未生成素材使用结论，仅展示任务输入。')).not.toBeInTheDocument()
  })

  it('shows the frozen task profile without exposing provider or model details', async () => {
    mockTask(taskWith({
      status: 'completed',
      execution_profile: 'quality',
      agent_profile_snapshot: {
        schema_version: 3,
        profile_id: 'quality',
        display_name: '极致效果',
        provider: 'moonshot',
        protocol: 'anthropic',
        envs: {
          ANTHROPIC_MODEL: 'kimi-k2.7-code',
          CLAUDE_CODE_EFFORT_LEVEL: 'high',
        },
        model_usage_aliases: { 'kimi-k2.7-code': 'kimi-k2.7-code' },
      },
      agent_profile_fingerprint: 'a'.repeat(64),
    }))

    render(<TaskDetailPage />)
    await openTaskDetails('配置')

    expect(screen.getByText('极致效果')).toBeInTheDocument()
    expect(screen.getByText('推理强度').nextElementSibling).toHaveTextContent('high')
    expect(screen.queryByText('moonshot')).not.toBeInTheDocument()
    expect(screen.queryByText('kimi-k2.7-code')).not.toBeInTheDocument()
    expect(screen.queryByText('kimi-k3[1m]')).not.toBeInTheDocument()
  })

  it('shows balanced context after results and opens logs in one action', async () => {
    mockTask(taskWith({
      status: 'running',
      progress_log: '已读取参考素材\n正在写作正文',
      lifecycle: {
        version: 1, revision: 2, execution_id: 'execution-1', updated_at: '2026-09-17T08:00:00Z',
        stages: [{ id: 'writing', title: '正在写作正文', source: 'agent', kind: 'work', state: 'active', latest_update: '正在优化标题与段落结构' }],
      },
      input_attachments: [{ type: 'image', file_name: 'tea-reference.jpg' }],
      project_snapshot: {
        project_name: '茶小茶',
        platform: 'article',
        visual_style: '清新茶感摄影',
        image_ratio: '3:4',
      },
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    const resultHeading = await screen.findByRole('heading', { name: '任务结果' })
    const context = screen.getByRole('region', { name: '任务上下文' })
    expect(resultHeading.compareDocumentPosition(context) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(context).getByText('茶小茶')).toBeInTheDocument()
    expect(within(context).getByText('公众号文章')).toBeInTheDocument()
    expect(within(context).getByText('清新茶感摄影 · 3:4')).toBeInTheDocument()
    expect(within(context).getByText('1 项输入')).toBeInTheDocument()
    expect(within(context).getByText('2 条 · 实时')).toBeInTheDocument()

    fireEvent.click(within(context).getByRole('button', { name: '打开执行日志' }))
    expect(await screen.findByRole('heading', { name: '任务详情' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')
    const logSection = screen.getByRole('region', { name: '执行动态' })
    expect(within(logSection).getByRole('heading', { name: '执行动态' })).toBeInTheDocument()
    expect(logSection).toHaveTextContent('已读取参考素材')
    expect(logSection).toHaveTextContent('正在写作正文')
  })

  it('restores automatic log following whenever the log view is opened', async () => {
    mockTask(taskWith({
      status: 'running',
      progress_log: '已读取参考素材\n正在写作正文',
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    const context = await screen.findByRole('region', { name: '任务上下文' })
    fireEvent.click(within(context).getByRole('button', { name: '打开执行日志' }))
    expect(screen.getByRole('button', { name: '暂停自动跟随' })).toHaveTextContent('自动跟随中')

    fireEvent.click(screen.getByRole('button', { name: '暂停自动跟随' }))
    expect(screen.getByRole('button', { name: '开启自动跟随' })).toHaveTextContent('跟随已暂停')

    fireEvent.click(screen.getByRole('tab', { name: '概览' }))
    fireEvent.click(screen.getByRole('tab', { name: '日志' }))
    expect(screen.getByRole('button', { name: '暂停自动跟随' })).toHaveTextContent('自动跟随中')
  })

  it('opens Overview from the general More details command', async () => {
    mockTask(taskWith({ status: 'completed' }))

    render(<TaskDetailPage />)

    await openTaskDetails()
  })

  it('closes task details on a route change and reopens on Overview', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'running', completed_at: '' }))
    const view = render(<TaskDetailPage />)

    const context = await screen.findByRole('region', { name: '任务上下文' })
    fireEvent.click(within(context).getByRole('button', { name: '打开执行日志' }))
    expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')

    routeState.taskId = 'task-2'
    vi.mocked(api.tasks.get).mockResolvedValue(taskWith({
      id: 'task-2',
      title: '第二个任务',
      status: 'running',
      completed_at: '',
    }))
    view.rerender(<TaskDetailPage />)

    await waitFor(() => expect(api.tasks.get).toHaveBeenCalledWith('task-2'))
    await waitFor(() => expect(screen.queryByRole('heading', { name: '任务详情' })).not.toBeInTheDocument())
    await openTaskDetails()
  })

  it('discards task A resume input before task B can submit it after a route switch', async () => {
    const taskA = taskWith({
      id: 'task-1',
      title: '失败任务 A',
      status: 'failed',
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '失败任务 B',
      status: 'failed',
    })
    const view = renderWithCachedTasks(taskA, taskB)

    const taskADialog = await openResumeDialog()
    fireEvent.change(within(taskADialog).getByLabelText('继续任务要求'), {
      target: { value: '只属于任务 A 的补充要求' },
    })

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)

    expect(await screen.findAllByText('失败任务 B')).not.toHaveLength(0)
    await waitFor(() => {
      expect(screen.queryByRole('heading', { name: '继续执行此任务' })).not.toBeInTheDocument()
    })

    const taskBDialog = await openResumeDialog()
    expect(within(taskBDialog).getByLabelText('继续任务要求')).toHaveValue('')
    const submitButton = within(taskBDialog).getByLabelText('提交并继续')
    expect(submitButton).toBeDisabled()
    fireEvent.click(submitButton)
    expect(api.tasks.resume).not.toHaveBeenCalled()
  })

  it('closes task A clone form when the route switches to task B', async () => {
    const taskA = taskWith({
      id: 'task-1',
      title: '已完成任务 A',
      prompt: '任务 A 的原始要求',
      status: 'completed',
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '已完成任务 B',
      prompt: '任务 B 的原始要求',
      status: 'completed',
    })
    const view = renderWithCachedTasks(taskA, taskB)

    const taskADialog = await openCloneDialog()
    await waitFor(() => {
      expect(within(taskADialog).getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('任务 A 的原始要求')
    })

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)

    expect(await screen.findAllByText('已完成任务 B')).not.toHaveLength(0)
    await waitFor(() => {
      expect(screen.queryByRole('heading', { name: '克隆任务' })).not.toBeInTheDocument()
    })

    const taskBDialog = await openCloneDialog()
    await waitFor(() => {
      expect(within(taskBDialog).getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('任务 B 的原始要求')
    })
    expect(api.tasks.clone).not.toHaveBeenCalled()
  })

  it('ignores a late clone success after navigating away from its source task', async () => {
    const taskA = taskWith({
      id: 'task-1',
      title: '已完成任务 A',
      status: 'completed',
      project_id: 'ch-1',
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '已完成任务 B',
      status: 'completed',
      project_id: 'ch-1',
    })
    const request = deferred<Task>()
    vi.mocked(api.tasks.clone).mockReturnValue(request.promise)
    const view = renderWithCachedTasks(taskA, taskB)
    const invalidateQueries = vi.spyOn(view.queryClient, 'invalidateQueries')

    const dialog = await openCloneDialog()
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目：测试项目' })).toHaveTextContent('测试项目'))
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))
    await waitFor(() => expect(api.tasks.clone).toHaveBeenCalledTimes(1))

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)
    expect(await screen.findAllByText('已完成任务 B')).not.toHaveLength(0)
    await waitFor(() => expect(screen.queryByRole('heading', { name: '克隆任务' })).not.toBeInTheDocument())

    await act(async () => {
      request.resolve(taskWith({ id: 'late-clone', status: 'pending' }))
      await request.promise
    })

    expect(invalidateQueries).toHaveBeenCalledWith({ queryKey: ['tasks'] })
    expect(mockNavigate).not.toHaveBeenCalledWith('/tasks/late-clone')
    expect(toastSuccessMock).not.toHaveBeenCalled()
  })

  it('ignores a late clone error after navigating away from its source task', async () => {
    const taskA = taskWith({ id: 'task-1', title: '已完成任务 A', status: 'completed', project_id: 'ch-1' })
    const taskB = taskWith({ id: 'task-2', title: '已完成任务 B', status: 'completed', project_id: 'ch-1' })
    const request = deferred<Task>()
    vi.mocked(api.tasks.clone).mockReturnValue(request.promise)
    const view = renderWithCachedTasks(taskA, taskB)

    const dialog = await openCloneDialog()
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目：测试项目' })).toHaveTextContent('测试项目'))
    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))
    await waitFor(() => expect(api.tasks.clone).toHaveBeenCalledTimes(1))

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)
    expect(await screen.findAllByText('已完成任务 B')).not.toHaveLength(0)
    await waitFor(() => expect(screen.queryByRole('heading', { name: '克隆任务' })).not.toBeInTheDocument())

    await act(async () => {
      request.reject(new Error('task A clone failed'))
      await request.promise.catch(() => {})
    })

    expect(toastErrorMock).not.toHaveBeenCalled()
  })

  it('ignores a late resume error after navigating away from its source task', async () => {
    const taskA = taskWith({ id: 'task-1', title: '失败任务 A', status: 'failed' })
    const taskB = taskWith({ id: 'task-2', title: '失败任务 B', status: 'failed' })
    const request = deferred<Task>()
    vi.mocked(api.tasks.resume).mockReturnValue(request.promise)
    const view = renderWithCachedTasks(taskA, taskB)

    const dialog = await openResumeDialog()
    fireEvent.change(within(dialog).getByLabelText('继续任务要求'), { target: { value: '继续任务 A' } })
    fireEvent.click(within(dialog).getByLabelText('提交并继续'))
    await waitFor(() => expect(api.tasks.resume).toHaveBeenCalledWith('task-1', {
      prompt: '继续任务 A',
      input_attachments: [],
    }))

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)
    expect(await screen.findAllByText('失败任务 B')).not.toHaveLength(0)
    await waitFor(() => expect(screen.queryByRole('heading', { name: '继续执行此任务' })).not.toBeInTheDocument())

    await act(async () => {
      request.reject(new Error('task A resume failed'))
      await request.promise.catch(() => {})
    })

    expect(toastErrorMock).not.toHaveBeenCalled()
  })

  it('does not navigate or abort the active task when an earlier delete settles late', async () => {
    const taskA = taskWith({ id: 'task-1', title: '已完成任务 A', status: 'completed' })
    const taskB = taskWith({ id: 'task-2', title: '运行中任务 B', status: 'running' })
    const deleteRequest = deferred<void>()
    const streamSignals = new Map<string, AbortSignal>()
    vi.mocked(api.tasks.delete).mockReturnValue(deleteRequest.promise)
    mockStreamTaskProgress.mockImplementation(async function* (taskId, _token, signal) {
      if (!signal) return
      streamSignals.set(taskId, signal)
      await waitForAbort(signal)
    })
    const view = renderWithCachedTasks(taskA, taskB)

    fireEvent.click(await screen.findByRole('button', { name: '删除任务' }))
    fireEvent.click(await screen.findByRole('button', { name: '确定删除' }))
    await waitFor(() => expect(api.tasks.delete).toHaveBeenCalledWith('task-1'))

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)
    expect(await screen.findByRole('heading', { name: '运行中任务 B' })).toBeInTheDocument()
    await waitFor(() => expect(streamSignals.get('task-2')).toBeDefined())

    await act(async () => {
      deleteRequest.resolve()
      await deleteRequest.promise
    })

    expect(mockNavigate).not.toHaveBeenCalledWith('/tasks')
    expect(toastSuccessMock).not.toHaveBeenCalledWith('任务已删除')
    expect(streamSignals.get('task-2')).toHaveProperty('aborted', false)
  })

  it('drops running task SSE state when switching directly to a cached completed task', async () => {
    const streamSignals = new Map<string, AbortSignal>()
    mockStreamTaskProgress.mockImplementation(async function* (taskId, _token, signal) {
      if (!signal) return
      streamSignals.set(taskId, signal)
      if (taskId !== 'task-1') return
      yield { event: 'output', data: 'A 实时日志' }
      await waitForAbort(signal)
      yield { event: 'output', data: 'A 终止后的迟到日志' }
    })
    const taskA = taskWith({
      id: 'task-1',
      status: 'running',
      progress_log: 'A 持久化日志',
      completed_at: '',
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '已完成任务 B',
      status: 'completed',
      progress_log: 'B 持久化日志',
    })
    const view = renderWithCachedTasks(taskA, taskB)

    await waitFor(() => {
      expect(screen.getByRole('region', { name: '任务上下文' })).toHaveTextContent('A 实时日志')
    })

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)

    await waitFor(() => {
      const context = screen.getByRole('region', { name: '任务上下文' })
      expect(context).toHaveTextContent('B 持久化日志')
      expect(context).not.toHaveTextContent('A 实时日志')
      expect(context).not.toHaveTextContent('A 终止后的迟到日志')
    })
    expect(streamSignals.get('task-1')).toHaveProperty('aborted', true)

    fireEvent.click(screen.getByRole('button', { name: '打开执行日志' }))
    const logSection = await screen.findByRole('region', { name: '执行动态' })
    expect(logSection).toHaveTextContent('B 持久化日志')
    expect(logSection).not.toHaveTextContent('A 实时日志')
    expect(logSection).not.toHaveTextContent('A 终止后的迟到日志')
    expect(within(logSection).queryByRole('button', { name: '重新连接' })).not.toBeInTheDocument()
  })

  it('restarts SSE and ignores late events when switching directly between cached running tasks', async () => {
    const streamSignals = new Map<string, AbortSignal>()
    mockStreamTaskProgress.mockImplementation(async function* (taskId, _token, signal) {
      if (!signal) return
      streamSignals.set(taskId, signal)
      yield { event: 'output', data: `${taskId} 实时日志` }
      await waitForAbort(signal)
      if (taskId === 'task-1') {
        yield { event: 'output', data: 'task-1 终止后的迟到日志' }
      }
    })
    const taskA = taskWith({
      id: 'task-1',
      status: 'running',
      progress_log: 'task-1 持久化日志',
      completed_at: '',
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '运行任务 B',
      status: 'running',
      progress_log: 'task-2 持久化日志',
      completed_at: '',
    })
    const view = renderWithCachedTasks(taskA, taskB)

    await waitFor(() => {
      expect(screen.getByRole('region', { name: '任务上下文' })).toHaveTextContent('task-1 实时日志')
    })

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)

    await waitFor(() => {
      expect(streamSignals.get('task-1')).toHaveProperty('aborted', true)
      expect(mockStreamTaskProgress).toHaveBeenCalledWith('task-2', 'test-token', expect.anything())
    })
    await waitFor(() => {
      const context = screen.getByRole('region', { name: '任务上下文' })
      expect(context).toHaveTextContent('task-2 实时日志')
      expect(context).not.toHaveTextContent('task-1 实时日志')
      expect(context).not.toHaveTextContent('task-1 终止后的迟到日志')
    })
  })

  it('shows reconnect only for the active running task and clears it for terminal or pending tasks', async () => {
    vi.useFakeTimers()
    try {
      mockStreamTaskProgress.mockImplementation(async function* () {
        throw new Error('SSE unavailable')
      })
      const taskA = taskWith({
        id: 'task-1',
        status: 'running',
        progress_log: 'A 持久化日志',
        completed_at: '',
      })
      const taskB = taskWith({
        id: 'task-2',
        title: '已完成任务 B',
        status: 'completed',
        progress_log: 'B 终态日志',
      })
      const taskC = taskWith({
        id: 'task-3',
        title: '等待任务 C',
        status: 'pending',
        progress_log: 'C 等待日志',
        completed_at: '',
      })
      vi.mocked(api.tasks.get).mockResolvedValue(taskA)
      const view = renderWithCachedTasks(taskA, taskB, taskC)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(12_001)
      })
      expect(mockStreamTaskProgress).toHaveBeenCalledTimes(4)
      expect(screen.getByRole('region', { name: '任务上下文' })).toHaveTextContent('连接中断')

      fireEvent.click(screen.getByRole('button', { name: '打开执行日志' }))
      expect(screen.getByRole('button', { name: '重新连接' })).toBeInTheDocument()

      vi.useRealTimers()
      routeState.taskId = 'task-2'
      view.rerender(<TaskDetailPage />)
      await waitFor(() => expect(screen.queryByRole('heading', { name: '任务详情' })).not.toBeInTheDocument())
      await openTaskDetails()
      expect(screen.queryByRole('button', { name: '重新连接' })).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('tab', { name: '日志' }))
      expect(screen.getByRole('region', { name: '执行动态' })).toHaveTextContent('B 终态日志')
      expect(screen.queryByRole('button', { name: '重新连接' })).not.toBeInTheDocument()

      routeState.taskId = 'task-3'
      view.rerender(<TaskDetailPage />)
      await waitFor(() => expect(screen.queryByRole('heading', { name: '任务详情' })).not.toBeInTheDocument())
      await openTaskDetails()
      expect(screen.queryByRole('button', { name: '重新连接' })).not.toBeInTheDocument()
      fireEvent.click(screen.getByRole('tab', { name: '日志' }))
      expect(screen.getByRole('region', { name: '执行动态' })).toHaveTextContent('C 等待日志')
      expect(screen.queryByRole('button', { name: '重新连接' })).not.toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('reconciles the first polling replay with persisted log lines', async () => {
    mockStreamTaskProgress.mockImplementation(async function* (_taskId, _token, signal) {
      if (!signal) return
      yield {
        event: 'log',
        data: JSON.stringify('第一条持久化日志\n第二条持久化日志\n第三条新增日志'),
      }
      await waitForAbort(signal)
    })
    const task = taskWith({
      id: 'task-1',
      status: 'running',
      progress_log: '第一条持久化日志\n第二条持久化日志',
      completed_at: '',
    })

    renderWithCachedTasks(task)

    await waitFor(() => {
      expect(screen.getByRole('region', { name: '任务上下文' })).toHaveTextContent('3 条 · 实时')
    })
    fireEvent.click(screen.getByRole('button', { name: '打开执行日志' }))
    const logSection = await screen.findByRole('region', { name: '执行动态' })
    expect(textOccurrences(logSection, '第一条持久化日志')).toBe(1)
    expect(textOccurrences(logSection, '第二条持久化日志')).toBe(1)
    expect(textOccurrences(logSection, '第三条新增日志')).toBe(1)
  })

  it('rotates a timed-out stream and deduplicates the reconnect replay', async () => {
    mockStreamTaskProgress.mockImplementation(async function* (_taskId, _token, signal) {
      if (!signal) return
      if (mockStreamTaskProgress.mock.calls.length === 1) {
        yield { event: 'timeout', data: '{}' }
        return
      }
      yield {
        event: 'log',
        data: JSON.stringify('第一条持久化日志\n第二条持久化日志'),
      }
      await waitForAbort(signal)
    })
    const task = taskWith({
      id: 'task-1',
      status: 'running',
      progress_log: '第一条持久化日志\n第二条持久化日志',
      completed_at: '',
    })

    renderWithCachedTasks(task)

    await waitFor(() => expect(mockStreamTaskProgress).toHaveBeenCalledTimes(2))
    const context = screen.getByRole('region', { name: '任务上下文' })
    expect(context).toHaveTextContent('2 条 · 实时')
    expect(context).not.toHaveTextContent('连接中断')

    fireEvent.click(screen.getByRole('button', { name: '打开执行日志' }))
    const logSection = await screen.findByRole('region', { name: '执行动态' })
    expect(textOccurrences(logSection, '第一条持久化日志')).toBe(1)
    expect(textOccurrences(logSection, '第二条持久化日志')).toBe(1)
  })

  it('does not reconnect after a terminal event and clean EOF', async () => {
    mockStreamTaskProgress.mockImplementation(async function* () {
      yield { event: 'completed', data: JSON.stringify({ status: 'completed' }) }
    })
    const task = taskWith({
      id: 'task-1',
      status: 'running',
      completed_at: '',
    })
    vi.mocked(api.tasks.get).mockResolvedValue(task)

    renderWithCachedTasks(task)

    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(mockStreamTaskProgress).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('region', { name: '任务上下文' })).toHaveTextContent('任务完成')
  })

  it('retries unexpected clean EOF and exposes reconnect after the retry budget', async () => {
    vi.useFakeTimers()
    try {
      mockStreamTaskProgress.mockImplementation(async function* () {})
      const task = taskWith({
        id: 'task-1',
        status: 'running',
        progress_log: '已有持久化日志',
        completed_at: '',
      })
      vi.mocked(api.tasks.get).mockResolvedValue(task)

      renderWithCachedTasks(task)

      await act(async () => {
        await vi.advanceTimersByTimeAsync(12_001)
      })
      expect(mockStreamTaskProgress).toHaveBeenCalledTimes(4)
      expect(screen.getByRole('region', { name: '任务上下文' })).toHaveTextContent('连接中断')

      fireEvent.click(screen.getByRole('button', { name: '打开执行日志' }))
      expect(screen.getByRole('button', { name: '重新连接' })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('opens configuration, materials, and raw Markdown logs from More details', async () => {
    const writeText = vi.fn()
    Object.assign(navigator, { clipboard: { writeText } })
    mockTask(taskWith({
      status: 'pending',
      progress_log: '## 阶段日志\n- 已完成选题',
      lifecycle: {
        version: 1, revision: 1, execution_id: 'execution-1', updated_at: '2026-09-17T08:00:00Z',
        stages: [{ id: 'writing', title: '正在写作正文', source: 'agent', kind: 'work', state: 'pending' }],
      },
      input_attachments: [{ type: 'image', file_name: 'brief-reference.png' }],
      project_snapshot: {
        project_name: '创建时项目快照',
        platform: 'article',
        visual_style: '明亮纪实摄影',
        image_ratio: '3:2',
        author: '安班编辑部',
        writer: '真诚叙事',
        theme: '简约留白',
      },
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByText('正在写作正文')).toBeInTheDocument()
    expect(screen.queryByText('未生成素材使用结论，仅展示任务输入。')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '阶段日志' })).not.toBeInTheDocument()
    const context = screen.getByRole('region', { name: '任务上下文' })
    expect(within(context).getByText('创建时项目快照')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '项目快照' })).not.toBeInTheDocument()

    await openTaskDetails('素材')
    expect(screen.getByText('未生成素材使用结论，仅展示任务输入。')).toBeInTheDocument()
    expect(screen.getByText('brief-reference.png')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '日志' }))
    expect(screen.getByRole('heading', { name: '阶段日志' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '复制日志' }))
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith('## 阶段日志\n- 已完成选题')
    })

    fireEvent.click(screen.getByRole('tab', { name: '配置' }))
    expect(within(screen.getByRole('tabpanel')).getByText('创建时项目快照')).toBeInTheDocument()
    expect(screen.getByText('明亮纪实摄影')).toBeInTheDocument()
    expect(screen.getByText('3:2')).toBeInTheDocument()
  })

  it('keeps the lifecycle rail after completion without a percentage', async () => {
    mockTask(taskWith({
      status: 'completed',
      progress_log: '任务完成',
    }))

    render(<TaskDetailPage />)

    await screen.findByRole('button', { name: '完成创作，已完成' })
    expect(screen.getByRole('heading', { name: '执行进展' })).toBeInTheDocument()
    expect(screen.queryByText(/100%/)).not.toBeInTheDocument()
    expect(screen.queryByText('任务执行成功')).not.toBeInTheDocument()
    expect(screen.queryByText('结果生成后将在这里显示')).not.toBeInTheDocument()
  })

  it('shows review summary without terminal workflow stage grid', async () => {
    mockTask(taskWith({
      status: 'completed',
      workflow_status: {
        version: 'creation_workflow_v1',
        current_stage: 'review',
        stages: [
          { key: 'draft', label: '初稿', status: 'completed', artifact_paths: ['03-draft.md'] },
          { key: 'review', label: '质量复盘', status: 'completed', artifact_paths: ['review.json'] },
        ],
        review: {
          overall_score: 91,
          readiness: 'ready',
          risks: ['标题可微调'],
          next_actions: ['发布前改标题'],
          strengths: ['结构完整'],
        },
      },
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'file-1',
      task_id: 'task-1',
      state: 'delivered',
      role: 'output',
      file_name: 'article.html',
      mime_type: 'text/html',
      file_size: 1024,
      url: '/api/v1/files/file-1',
      is_deliverable: true,
      created_at: '2026-07-06T03:00:00Z',
    }])

    render(<TaskDetailPage />)

    fireEvent.click(await screen.findByRole('button', { name: '完成创作，已完成' }))
    const review = await screen.findByText('内容验收')
    const files = await screen.findByText('交付成果 (1)')
    const moreDetails = await screen.findByRole('button', { name: /更多详情/ })
    expect(review.compareDocumentPosition(files) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(files.compareDocumentPosition(moreDetails) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByText('创作进度')).not.toBeInTheDocument()
    expect(screen.queryByText('当前阶段')).not.toBeInTheDocument()
    expect(screen.queryByText('03-draft.md')).not.toBeInTheDocument()
  })


  it('keeps task status and generated files visible when the reference summary crashes', async () => {
    mockTask(taskWith({
      id: 'task-1',
      title: '触发摘要组件失败',
      status: 'completed',
      input_attachments: [{ type: 'image', file_name: 'front.png' }],
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([
      {
        id: 'file-summary',
        task_id: 'task-1',
        state: 'retained',
        role: 'artifact',
        file_name: 'reference-usage-summary.json',
        mime_type: 'application/json',
        file_size: 1024,
        url: '',
        is_deliverable: false,
        created_at: '2026-07-10T00:00:00Z',
      },
      {
        id: 'file-1',
        task_id: 'task-1',
        state: 'delivered',
        role: 'output',
        file_name: 'article.html',
        mime_type: 'text/html',
        file_size: 1024,
        url: '/api/v1/files/file-1',
        is_deliverable: true,
        created_at: '2026-07-10T00:00:01Z',
      },
    ])

    render(<TaskDetailPage />)

    const taskStatus = await screen.findByText('已完成', { selector: '[data-slot="badge"]' })
    expect(taskStatus).toBeInTheDocument()
    const filesHeading = await screen.findByText('交付成果 (1)')
    expect(filesHeading).toBeInTheDocument()
    expect(screen.queryByText('参考素材摘要暂时无法显示')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /预览 article\.html/ })).toBeInTheDocument()

    await openTaskDetails('素材')
    expect(await screen.findByText('参考素材摘要暂时无法显示')).toBeInTheDocument()
    expect(taskStatus).toBeInTheDocument()
    expect(screen.getByText('交付成果 (1)')).toBeInTheDocument()
  })

  it('separates collected failure artifacts from generated files', async () => {
    mockTask(taskWith({
      status: 'failed',
    }))
    const now = '2026-07-15T03:00:00Z'
    const files: TaskFile[] = [
      {
        id: 'published-1',
        task_id: 'task-1',
        execution_id: 'execution-success',
        state: 'delivered',
        role: 'content',
        file_name: 'content.md',
        mime_type: 'text/markdown',
        file_size: 128,
        url: '/content.md',
        is_deliverable: true,
        created_at: now,
      },
      {
        id: 'collected-1',
        task_id: 'task-1',
        execution_id: 'execution-failed',
        state: 'retained',
        role: 'other',
        file_name: 'failure-state.json',
        mime_type: 'application/json',
        file_size: 96,
        url: '',
        is_deliverable: false,
        created_at: now,
      },
    ]
    vi.mocked(api.tasks.files).mockResolvedValue(files)

    render(<TaskDetailPage />)

    const generatedHeading = await screen.findByText('交付成果 (1)')
    const failedHeading = await screen.findByText('已保留产物 (1)')
    const generatedSection = generatedHeading.closest('[data-slot="card"]') as HTMLElement
    const failedSection = failedHeading.closest('[data-slot="card"]') as HTMLElement
    expect(within(generatedSection).getByTitle('content.md')).toBeInTheDocument()
    expect(within(generatedSection).queryByTitle('failure-state.json')).not.toBeInTheDocument()
    expect(within(failedSection).getByTitle('failure-state.json')).toBeInTheDocument()
    expect(within(failedSection).queryByTitle('content.md')).not.toBeInTheDocument()
  })

  it('distinguishes a failed continuation from its preserved historical delivery', async () => {
    mockTask(taskWith({
      status: 'failed',
      error_message: 'runtime_failed',
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'prior-delivery',
      task_id: 'task-1',
      execution_id: 'prior-successful-execution',
      state: 'delivered',
      role: 'content',
      file_name: 'content.md',
      mime_type: 'text/markdown',
      file_size: 128,
      url: '/content.md',
      is_deliverable: true,
      created_at: '2026-09-08T14:04:42Z',
    }])

    render(<TaskDetailPage />)

    expect(await screen.findByText('执行失败')).toBeInTheDocument()
    expect(screen.getByText('runtime_failed')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /下载交付成果/ })).toBeEnabled()
  })

  it('shows additional files below deliverables and keeps ZIP scoped to deliverables', async () => {
    mockTask(taskWith({
      status: 'completed',
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([
      {
        id: 'deliverable-1',
        task_id: 'task-1',
        state: 'delivered',
        role: 'content',
        file_name: 'content.md',
        mime_type: 'text/markdown',
        file_size: 128,
        url: '/content.md',
        is_deliverable: true,
        delivery_role: 'final_markdown',
        preview_url: '/api/v1/tasks/task-1/files/deliverable-1/preview',
        download_url: '/api/v1/tasks/task-1/files/deliverable-1/download',
        created_at: '2026-07-15T03:00:00Z',
      },
      {
        id: 'process-1',
        task_id: 'task-1',
        state: 'delivered',
        role: 'review',
        file_name: 'review.json',
        mime_type: 'application/json',
        file_size: 96,
        url: '',
        is_deliverable: false,
        preview_url: '/api/v1/tasks/task-1/files/process-1/preview',
        created_at: '2026-07-15T03:00:01Z',
      },
    ])

    render(<TaskDetailPage />)

    const deliverables = await screen.findByRole('heading', { name: '交付成果 (1)' })
    const previewOnly = await screen.findByLabelText('仅支持预览')
    expect(deliverables.compareDocumentPosition(previewOnly) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByRole('button', { name: '下载 review.json' })).toBeDisabled()
    expect(screen.queryByText(/过程文件/)).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /下载交付成果/ }))
    await waitFor(() => expect(api.tasks.downloadZipBlob).toHaveBeenCalledWith('task-1'))
  })

  it('places Seednote analytics after generated deliverables without requiring published state', async () => {
    mockTask(taskWith({
      type: 'seednote',
      status: 'completed',
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'seednote-output',
      task_id: 'task-1',
      state: 'delivered',
      role: 'content',
      file_name: 'content.md',
      mime_type: 'text/markdown',
      file_size: 128,
      url: '/content.md',
      is_deliverable: true,
      created_at: '2026-07-15T03:00:00Z',
    }])

    render(<TaskDetailPage />)

    const generatedHeading = await screen.findByText('交付成果 (1)')
    const analyticsHeading = await screen.findByText('种草笔记数据')
    const context = screen.getByRole('region', { name: '任务上下文' })
    expect(generatedHeading.compareDocumentPosition(analyticsHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(analyticsHeading.compareDocumentPosition(context) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('shows the WeChat publication lifecycle without URL binding', async () => {
    mockTask(taskWith({
      type: 'article',
      status: 'completed',
      lifecycle: {
        version: 1,
        revision: 4,
        execution_id: 'execution-1',
        updated_at: '2026-09-17T08:00:00Z',
        stages: [
          { id: 'creation', title: '完成创作', source: 'agent', kind: 'work', state: 'complete' },
          { id: 'system_draft', title: '创建公众号草稿', source: 'server', kind: 'draft', state: 'complete', latest_update: '草稿已进入公众号后台' },
          { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'pending' },
        ],
      },
    }))

    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'article-output',
      task_id: 'task-1',
      state: 'delivered',
      role: 'html',
      file_name: '05-article.html',
      mime_type: 'text/html',
      file_size: 128,
      is_deliverable: true,
      url: '/api/v1/tasks/task-1/files/article-output/preview',
      preview_url: '/api/v1/tasks/task-1/files/article-output/preview',
      download_url: '/api/v1/tasks/task-1/files/article-output/download',
      created_at: '2026-07-15T03:00:00Z',
    }])

    render(<TaskDetailPage />)

    const draftStatus = await screen.findByRole('button', { name: '创建公众号草稿，已完成' })
    const deliveryHeading = await screen.findByText('交付成果 (1)')
    expect(draftStatus.compareDocumentPosition(deliveryHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByRole('button', { name: '正式发布' })).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: '公众号文章链接' })).not.toBeInTheDocument()
  })

  it('keeps streaming lifecycle events after creative completion while publication is pending', async () => {
    mockTask(taskWith({
      type: 'article',
      status: 'completed',
      lifecycle: {
        version: 1,
        revision: 4,
        execution_id: 'execution-1',
        updated_at: '2026-09-17T08:00:00Z',
        stages: [
          { id: 'creation', title: '完成创作', source: 'agent', kind: 'work', state: 'complete' },
          { id: 'system_draft', title: '创建公众号草稿', source: 'server', kind: 'draft', state: 'complete' },
          { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'pending' },
        ],
      },
    }))

    render(<TaskDetailPage />)

    await screen.findByRole('button', { name: '正式发布' })
    await waitFor(() => expect(mockStreamTaskProgress).toHaveBeenCalledWith('task-1', 'test-token', expect.any(AbortSignal)))
  })

  it('uses only the common delivery ZIP action for ecommerce files', async () => {
    mockTask(taskWith({
      type: 'ecommerce',
      status: 'completed',
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'ecommerce-output',
      task_id: 'task-1',
      state: 'delivered',
      role: 'copywriting',
      file_name: 'copywriting.md',
      mime_type: 'text/markdown',
      file_size: 128,
      is_deliverable: true,
      url: '/api/v1/tasks/task-1/files/ecommerce-output/preview',
      preview_url: '/api/v1/tasks/task-1/files/ecommerce-output/preview',
      download_url: '/api/v1/tasks/task-1/files/ecommerce-output/download',
      created_at: '2026-07-15T03:00:00Z',
    }])

    render(<TaskDetailPage />)

    await screen.findByText('交付成果 (1)')
    expect(screen.getAllByRole('button', { name: /下载交付成果|整包下载/ })).toHaveLength(1)
    expect(screen.queryByRole('button', { name: '整包下载' })).not.toBeInTheDocument()
  })

  it('waits for project configuration before querying WeChat publication', async () => {
    mockTask(taskWith({
      type: 'article',
      status: 'completed',
    }))
    let resolveProject!: (value: typeof mockProjectDetail) => void
    vi.mocked(api.projects.get).mockImplementation(() => new Promise((resolve) => {
      resolveProject = resolve
    }))

    render(<TaskDetailPage />)

    await screen.findByRole('heading', { name: '测试任务' })
    expect(api.tasks.getWechatPublication).not.toHaveBeenCalled()

    resolveProject(mockProjectDetail)
    await waitFor(() => expect(api.tasks.getWechatPublication).toHaveBeenCalledWith('task-1'))
  })

  it('shows Channels link binding for a completed montage task', async () => {
    mockTask(taskWith({
      type: 'montage',
      status: 'completed',
    }))
    vi.mocked(api.channelsAnalytics.getByTask).mockResolvedValue({ series: [] })

    render(<TaskDetailPage />)

    expect(await screen.findByText('视频号数据')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '视频号视频链接' })).toBeInTheDocument()
  })

  it('keeps completed delivery controls available without a runtime balance lock', async () => {
    mockTask(taskWith({
      status: 'completed',
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([
      {
        id: 'file-1',
        task_id: 'task-1',
        state: 'delivered',
        role: 'output',
        file_name: 'article.html',
        mime_type: 'text/html',
        file_size: 1024,
        url: '/api/v1/files/file-1',
        is_deliverable: true,
        created_at: '2026-07-06T03:00:00Z',
      },
    ])

    render(<TaskDetailPage />)

    expect(await screen.findByText('交付成果 (1)')).toBeInTheDocument()
    expect(screen.queryByText('交付已锁定')).not.toBeInTheDocument()
    const zipButton = screen.getByRole('button', { name: /下载交付成果/ })
    expect(zipButton).toBeEnabled()
    expect(screen.getByRole('button', { name: /预览 article\.html/ })).toBeEnabled()
    expect(screen.getByRole('button', { name: /下载 article\.html/ })).toBeEnabled()
  })

  it('does not load shared clone-form data until a terminal task opens the clone dialog', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'completed', project_id: 'ch-1' }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(api.projects.list).not.toHaveBeenCalled()
    expect(api.billing.wallet).not.toHaveBeenCalled()
    expect(api.billing.catalog).not.toHaveBeenCalled()
    expect(api.imageCapabilities.list).not.toHaveBeenCalled()

    await openCloneDialog()

    await waitFor(() => expect(api.projects.list).toHaveBeenCalledTimes(1))
    expect(api.billing.wallet).toHaveBeenCalledTimes(1)
    expect(api.billing.catalog).toHaveBeenCalledTimes(1)
    expect(api.imageCapabilities.list).toHaveBeenCalledTimes(1)
  })

  it('opens the shared full clone form with editable source defaults and navigates to the first created task', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'completed',
      prompt: '原始任务要求',
      project_id: 'ch-1',
      image_ratio: '16:9',
      image_capability_key: 'source-capability',
    }))

    render(<TaskDetailPage />)

    const dialog = await openCloneDialog()
    expect(api.tasks.clone).not.toHaveBeenCalled()
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目：测试项目' })).toHaveTextContent('测试项目'))
    expect(within(dialog).getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('原始任务要求')
    expect(within(dialog).queryByText('数量', { exact: true })).not.toBeInTheDocument()
    expect(await within(dialog).findByRole('button', {
      name: /^创作参数：.*16:9 源图像.*任务数量 1/,
    })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '继续执行此任务' })).not.toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(api.tasks.clone).toHaveBeenCalledWith('task-1', expect.objectContaining({
      project_id: 'ch-1',
      quantity: 1,
      prompt: '原始任务要求',
      image_ratio: '16:9',
      image_capability_key: 'source-capability',
    })))
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/tasks/task-clone'))
    expect(toastSuccessMock).toHaveBeenCalledTimes(1)
  })

  it('keeps Continue isolated to the prompt-only resume dialog', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'cancelled' }))

    render(<TaskDetailPage />)

    fireEvent.click(await screen.findByRole('button', { name: '继续执行' }))
    const dialog = await screen.findByRole('dialog', { name: '继续执行此任务' })
    expect(within(dialog).getByLabelText('继续任务要求')).toHaveValue('')
    expect(screen.queryByRole('dialog', { name: '克隆任务' })).not.toBeInTheDocument()
    expect(api.tasks.clone).not.toHaveBeenCalled()
  })

  it('shows only immutable-history actions for a completed task', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'completed' }))

    render(<TaskDetailPage />)

    await screen.findByRole('button', { name: '克隆任务' })
    expect(screen.queryByRole('button', { name: '继续执行' })).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '已发布' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除任务' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多任务操作' })).not.toBeInTheDocument()
  })

  it('keeps task feedback behind a completed-task icon and lazy-loads it on open', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'completed' }))

    render(<TaskDetailPage />)

    const feedbackButton = await screen.findByRole('button', { name: '人工评价' })
    expect(screen.queryByText('评价只对当前任务生效')).not.toBeInTheDocument()
    expect(api.feedback.getTask).not.toHaveBeenCalled()

    fireEvent.click(feedbackButton)

    expect(await screen.findByRole('dialog', { name: '人工评价' })).toBeInTheDocument()
    await waitFor(() => expect(api.feedback.getTask).toHaveBeenCalledWith('task-1'))
  })

  it('hides the task feedback icon for non-completed tasks', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'running' }))

    render(<TaskDetailPage />)

    await screen.findByRole('button', { name: '完成创作，进行中' })
    expect(screen.queryByText(/%/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '人工评价' })).not.toBeInTheDocument()
    expect(api.feedback.getTask).not.toHaveBeenCalled()
  })

  it('closes feedback when a completed task leaves the completed state', async () => {
    const task = taskWith({ id: 'task-1', status: 'completed' })
    const { queryClient } = renderWithCachedTasks(task)

    fireEvent.click(await screen.findByRole('button', { name: '人工评价' }))
    expect(await screen.findByRole('dialog', { name: '人工评价' })).toBeInTheDocument()

    queryClient.setQueryData(['task', 'task-1'], { ...task, status: 'running' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '人工评价' })).not.toBeInTheDocument())

    queryClient.setQueryData(['task', 'task-1'], { ...task, status: 'completed' })
    await waitFor(() => expect(screen.getByRole('button', { name: '人工评价' })).toBeInTheDocument())
    expect(screen.queryByRole('dialog', { name: '人工评价' })).not.toBeInTheDocument()
  })

  it('uploads resume files to OSS, keeps failed input for retry, then reopens blank', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
    }))
    vi.mocked(api.tasks.resume)
      .mockRejectedValueOnce(new Error('resume failed'))
      .mockResolvedValueOnce(taskWith({ id: 'task-1', status: 'pending' }))

    render(<TaskDetailPage />)

    const dialog = await openResumeDialog()
    expect(within(dialog).getByLabelText('继续任务要求')).toHaveValue('')
    fireEvent.change(within(dialog).getByLabelText('继续任务要求'), {
      target: { value: '请基于现有草稿继续修改' },
    })
    const file = new File(['notes'], 'notes.md', { type: 'text/markdown' })
    fireEvent.change(within(dialog).getByLabelText('选择附件文件'), {
      target: { files: [file] },
    })
    await waitFor(() => expect(uploadToOSSMock).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'ai_entry_attachment',
      file,
    })))
    fireEvent.click(within(dialog).getByRole('button', { name: '预览 notes.md' }))
    expect(await screen.findByRole('heading', { name: 'notes.md' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '关闭附件预览' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '编辑 notes.md 的附件说明' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '附件说明' }), {
      target: { value: '修改意见' },
    })
    fireEvent.click(within(dialog).getByLabelText('提交并继续'))

    await waitFor(() => expect(api.tasks.resume).toHaveBeenCalledTimes(1))
    expect(within(dialog).getByLabelText('继续任务要求')).toHaveValue('请基于现有草稿继续修改')
    expect(within(dialog).getByRole('button', { name: '预览 notes.md' })).toBeInTheDocument()

    fireEvent.click(within(dialog).getByLabelText('提交并继续'))
    await waitFor(() => expect(api.tasks.resume).toHaveBeenCalledTimes(2))
    expect(api.tasks.resume).toHaveBeenLastCalledWith('task-1', {
      prompt: '请基于现有草稿继续修改',
      input_attachments: [{
        type: 'text',
        upload_id: 'upload-notes.md',
        key: 'uploads/pending/user-1/upload-notes.md/notes.md',
        file_name: 'notes.md',
        content_type: 'text/markdown',
        size: file.size,
        instruction: '修改意见',
      }],
    })
    expect(mockNavigate).not.toHaveBeenCalledWith('/tasks/task-clone')
    await waitFor(() => expect(screen.queryByRole('heading', { name: '继续执行此任务' })).not.toBeInTheDocument())

    const reopened = await openResumeDialog()
    expect(within(reopened).getByLabelText('继续任务要求')).toHaveValue('')
    expect(within(reopened).queryByText('notes.md')).not.toBeInTheDocument()
  })

  it('submits a prompt-only resume request from the composer', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
    }))

    render(<TaskDetailPage />)

    const dialog = await openResumeDialog()
    fireEvent.change(within(dialog).getByLabelText('继续任务要求'), {
      target: { value: 'continue' },
    })
    fireEvent.click(within(dialog).getByLabelText('提交并继续'))

    await waitFor(() => {
      expect(api.tasks.resume).toHaveBeenCalledWith('task-1', {
        prompt: 'continue',
        input_attachments: [],
      })
    })
  })

  it('keeps the resume dialog mounted and dismiss controls disabled until its request settles', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
    }))
    const request = deferred<Task>()
    vi.mocked(api.tasks.resume).mockReturnValue(request.promise)

    render(<TaskDetailPage />)

    const reopenTrigger = await screen.findByRole('button', { name: '继续执行' })
    fireEvent.click(reopenTrigger)
    const title = await screen.findByRole('heading', { name: '继续执行此任务' })
    const dialog = title.closest('[data-slot="dialog-content"]') as HTMLElement
    fireEvent.change(within(dialog).getByLabelText('继续任务要求'), {
      target: { value: '继续完成当前任务' },
    })
    fireEvent.click(within(dialog).getByLabelText('提交并继续'))
    await waitFor(() => expect(api.tasks.resume).toHaveBeenCalledTimes(1))

    const cancel = within(dialog).getByRole('button', { name: '取消' })
    const close = within(dialog).getByRole('button', { name: 'Close' })
    await waitFor(() => {
      expect(cancel).toBeDisabled()
      expect(close).toBeDisabled()
    })
    fireEvent.click(cancel)
    fireEvent.click(close)
    fireEvent.keyDown(document, { key: 'Escape' })
    const overlay = document.querySelector<HTMLElement>('[data-slot="dialog-overlay"]')
    expect(overlay).not.toBeNull()
    fireEvent.pointerDown(overlay as HTMLElement)
    fireEvent.pointerUp(overlay as HTMLElement)
    fireEvent.click(overlay as HTMLElement)
    fireEvent.click(reopenTrigger)

    expect(screen.getAllByRole('heading', { name: '继续执行此任务' })).toHaveLength(1)
    expect(api.tasks.resume).toHaveBeenCalledTimes(1)
    expect(toastSuccessMock).not.toHaveBeenCalled()

    await act(async () => {
      request.resolve(taskWith({ id: 'task-1', status: 'pending' }))
      await request.promise
    })

    await waitFor(() => expect(screen.queryByRole('heading', { name: '继续执行此任务' })).not.toBeInTheDocument())
    expect(api.tasks.resume).toHaveBeenCalledTimes(1)
    expect(toastSuccessMock).toHaveBeenCalledTimes(1)
  })

  it('discards resume input after cancel, close, or Escape and keeps remaining labels in file order', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
    }))

    render(<TaskDetailPage />)

    let dialog = await openResumeDialog()
    fireEvent.change(within(dialog).getByLabelText('继续任务要求'), { target: { value: '取消这次输入' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '取消' }))

    dialog = await openResumeDialog()
    expect(within(dialog).getByLabelText('继续任务要求')).toHaveValue('')
    fireEvent.change(within(dialog).getByLabelText('继续任务要求'), { target: { value: '关闭这次输入' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }))

    dialog = await openResumeDialog()
    expect(within(dialog).getByLabelText('继续任务要求')).toHaveValue('')
    fireEvent.change(within(dialog).getByLabelText('继续任务要求'), { target: { value: 'Escape 这次输入' } })
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('heading', { name: '继续执行此任务' })).not.toBeInTheDocument())

    dialog = await openResumeDialog()
    expect(within(dialog).getByLabelText('继续任务要求')).toHaveValue('')
    const first = new File(['first'], 'first.md', { type: 'text/markdown' })
    const second = new File(['second'], 'second.md', { type: 'text/markdown' })
    fireEvent.change(within(dialog).getByLabelText('选择附件文件'), {
      target: { files: [first, second] },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: '编辑 first.md 的附件说明' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '附件说明' }), { target: { value: '第一份' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '编辑 second.md 的附件说明' }))
    fireEvent.change(await screen.findByRole('textbox', { name: '附件说明' }), { target: { value: '第二份' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '删除 first.md' }))
    fireEvent.click(within(dialog).getByLabelText('提交并继续'))

    await waitFor(() => {
      expect(api.tasks.resume).toHaveBeenCalledWith('task-1', {
        prompt: '',
        input_attachments: [{
          type: 'text',
          upload_id: 'upload-second.md',
          key: 'uploads/pending/user-1/upload-second.md/second.md',
          file_name: 'second.md',
          content_type: 'text/markdown',
          size: second.size,
          instruction: '第二份',
        }],
      })
    })
  })

  it('matches the resume server policy of five files and 25 MiB per file', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
    }))

    render(<TaskDetailPage />)
    const dialog = await openResumeDialog()
    const files = Array.from({ length: 6 }, (_, index) => (
      new File([`${index}`], `file-${index + 1}.txt`, { type: 'text/plain' })
    ))
    fireEvent.change(within(dialog).getByLabelText('选择附件文件'), {
      target: { files },
    })

    expect(within(dialog).getByRole('button', { name: '预览 file-5.txt' })).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '预览 file-6.txt' })).not.toBeInTheDocument()
    expect(within(dialog).getByLabelText('选择附件文件')).toBeDisabled()

    const oversized = new File(['large'], 'too-large.pdf', { type: 'application/pdf' })
    Object.defineProperty(oversized, 'size', { value: 25 * 1024 * 1024 + 1 })
    fireEvent.click(within(dialog).getByRole('button', { name: '删除 file-5.txt' }))
    fireEvent.change(within(dialog).getByLabelText('选择附件文件'), {
      target: { files: [oversized] },
    })
    expect(within(dialog).queryByText('too-large.pdf')).not.toBeInTheDocument()
  })

  it.each(['running', 'pending'] as const)('shows only cancellation among task actions for a %s task', async (status) => {
    mockTask(taskWith({
      status,
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '取消任务' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '已发布' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '继续执行' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '克隆任务' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '删除任务' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多任务操作' })).not.toBeInTheDocument()
    expect(api.projects.list).not.toHaveBeenCalled()
    expect(api.billing.wallet).not.toHaveBeenCalled()
    expect(api.billing.catalog).not.toHaveBeenCalled()
    expect(api.imageCapabilities.list).not.toHaveBeenCalled()
  })

  it('shows visible continue, clone, and delete actions for a cancelled task', async () => {
    mockTask(taskWith({
      status: 'cancelled',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '继续执行' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除任务' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '已发布' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多任务操作' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '完成创作，已取消' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /重新执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /再试一次/ })).not.toBeInTheDocument()
  })

  it('shows failed-task diagnosis from the deployed API error_message field without duplicating header actions', async () => {
    mockTask(taskWith({
      status: 'failed',
      error_message: '模型超时',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '继续执行' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除任务' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '已发布' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多任务操作' })).not.toBeInTheDocument()
    expect(screen.getByText('执行失败')).toBeInTheDocument()
    expect(screen.getByText('模型超时')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '继续执行' })).toHaveLength(1)
    expect(screen.queryByRole('button', { name: /返回任务列表/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /检查项目配置/ })).not.toBeInTheDocument()
  })

  it('shows a localized recoverable execution identity failure and preserves operator diagnostics', async () => {
    mockTask(taskWith({
      status: 'failed',
      error_message: '执行环境未建立，暂时无法生成或结算图片',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByText('执行环境未建立')).toBeInTheDocument()
    expect(screen.getByText('执行环境未建立，暂时无法生成或结算图片。')).toBeInTheDocument()
    expect(screen.getByText('修复执行环境后可从“图片生成”阶段继续。')).toBeInTheDocument()

    await openTaskDetails('概览')
    expect(screen.getByRole('region', { name: '故障诊断' })).toBeInTheDocument()
    expect(screen.getByText('execution_identity_unavailable')).toBeInTheDocument()
    expect(screen.getByText('执行环境未建立，暂时无法生成或结算图片')).toBeInTheDocument()
  })

  it('does not invent an interruption reason or preserved workspace when failure details are absent', async () => {
    mockTask(taskWith({
      status: 'failed',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '完成创作，失败' })).toBeInTheDocument()
    expect(screen.getByText('服务端没有返回失败详情，可继续执行并补充说明。')).toBeInTheDocument()
    expect(screen.queryByText('失败原因')).not.toBeInTheDocument()
    expect(screen.queryByText(/工作目录已保留/)).not.toBeInTheDocument()
  })

  it('shows project parameters without the low-value reference image preview', async () => {
    mockTask(taskWith({
      type: 'article',
      status: 'completed',
      billing_price_credits: 6000,
      billing_total_credits: 7300,
      billing_charge_details: [
        {
          id: 'task-charge',
          charge_kind: 'task',
          policy: 'task_admission',
          sku_id: 'task.article.standard.v1',
          credits: 6000,
          created_at: '2026-07-10T00:00:00.000Z',
        },
        {
          id: 'standard-image-charge-1',
          charge_kind: 'operation',
          policy: 'accepted_task_operation',
          sku_id: 'image.standard',
          credits: 500,
          tool_call_id: 'image:content-1',
          created_at: '2026-07-10T00:01:00.000Z',
        },
        {
          id: 'standard-image-charge-2',
          charge_kind: 'operation',
          policy: 'accepted_task_operation',
          sku_id: 'image.standard',
          credits: 500,
          tool_call_id: 'image:cover-1',
          created_at: '2026-07-10T00:02:00.000Z',
        },
        {
          id: 'analysis-charge',
          charge_kind: 'operation',
          policy: 'accepted_task_operation',
          sku_id: 'analysis.content.v1',
          credits: 300,
          resource_type: 'analysis',
          tool_call_id: 'analysis:content-1',
          created_at: '2026-07-10T00:03:00.000Z',
        },
      ],
      project_snapshot: {
        project_name: '快照项目',
        platform: 'article',
        visual_style: '柔光生活摄影',
        image_ratio: '16:9',
        reference_image_asset_id: '44444444-4444-4444-8444-444444444444',
        author: '安般',
        writer: 'dan-koe',
        theme: 'autumn-warm',
      },
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: /更多详情/ })).toBeInTheDocument()
    expect(screen.queryByText('项目快照')).not.toBeInTheDocument()
    expect(screen.queryByText('任务配置')).not.toBeInTheDocument()

    await openTaskDetails('配置')
    expect(screen.getByText('项目快照')).toBeInTheDocument()
    expect(within(screen.getByRole('tabpanel')).getByText('快照项目')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: '参考图' })).not.toBeInTheDocument()
    expect(screen.queryByText('参考图')).not.toBeInTheDocument()
    expect(screen.getByText('视觉风格')).toBeInTheDocument()
    expect(screen.getByText('柔光生活摄影')).toBeInTheDocument()
    expect(screen.getByText('视觉风格')).toBeInTheDocument()
    expect(screen.getByText('图片比例')).toBeInTheDocument()
    expect(screen.getByText('图像能力')).toBeInTheDocument()
    expect(screen.getByText('安般')).toBeInTheDocument()
    expect(screen.getByText('dan-koe')).toBeInTheDocument()
    expect(screen.getByText('autumn-warm')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '概览' }))
    expect(screen.getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('积分明细')).toBeInTheDocument()
    expect(screen.getByText('累计扣费')).toBeInTheDocument()
    expect(screen.getByText('7,300 积分')).toBeInTheDocument()
    expect(screen.getByText('任务固定费')).toBeInTheDocument()
    expect(screen.getAllByText('标准图像生成费')).toHaveLength(2)
    expect(screen.getByText('增值操作费')).toBeInTheDocument()
    expect(screen.getByText('6,000 积分')).toBeInTheDocument()
    expect(screen.getAllByText('500 积分')).toHaveLength(2)
    expect(screen.getByText('300 积分')).toBeInTheDocument()
  })

  it('opens project details in a dialog instead of navigating to the projects list', async () => {
    mockTask(taskWith({
      type: 'article',
      status: 'completed',
    }))

    render(<TaskDetailPage />)

    const projectButton = await screen.findByRole('button', { name: /测试项目/ })
    fireEvent.click(projectButton)

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByRole('heading', { name: '项目信息' })).toBeInTheDocument()
    expect(within(dialog).getByText('测试项目')).toBeInTheDocument()
    expect(within(dialog).getByText('https://mp.weixin.qq.com/test')).toBeInTheDocument()
    expect(mockNavigate).not.toHaveBeenCalledWith('/projects')
  })

  it('labels the project ratio as a video setting for Montage', async () => {
    const montageProject = {
      ...mockProjectDetail,
      project: {
        ...mockProjectDetail.project,
        platform: 'montage' as const,
        image_ratio: '9:16',
      },
    }
    vi.mocked(api.projects.get).mockResolvedValue(montageProject)
    mockTask(taskWith({
      type: 'montage',
      status: 'completed',
      image_ratio: '9:16',
      project_snapshot: {
        project_name: montageProject.project.name,
        platform: 'montage',
        image_ratio: '9:16',
      },
    }))

    render(<TaskDetailPage />)

    fireEvent.click(await screen.findByRole('button', { name: /\u6d4b试项目/ }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('视频比例')).toBeInTheDocument()
    expect(within(dialog).queryByText('图片比例')).not.toBeInTheDocument()
  })

  it('renders dynamic logs as markdown and keeps copyable raw text', async () => {
    const writeText = vi.fn()
    Object.assign(navigator, {
      clipboard: { writeText },
    })
    mockTask(taskWith({
      status: 'running',
      progress_log: '## 阶段日志\n- 已完成选题\n```txt\nraw block\n```',
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    await openTaskDetails('日志')
    expect(screen.getByRole('heading', { name: '阶段日志' })).toBeInTheDocument()
    expect(screen.getByText('已完成选题')).toBeInTheDocument()

    screen.getByRole('button', { name: /复制/ }).click()
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(expect.stringContaining('## 阶段日志\n- 已完成选题\n```txt\nraw block\n```'))
    })
  })
})
