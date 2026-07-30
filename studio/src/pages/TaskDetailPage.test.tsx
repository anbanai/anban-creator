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
        markPublished: vi.fn(),
        publishApprove: vi.fn(),
        publishReject: vi.fn(),
        files: vi.fn().mockResolvedValue([]),
        downloadZipBlob: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        get: vi.fn().mockResolvedValue(mockProjectDetail),
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
      seednoteAnalytics: {
        ...actual.api.seednoteAnalytics,
        getByTask: vi.fn(),
      },
    },
  }
})

function mockTask(task: Task) {
  vi.mocked(api.tasks.get).mockResolvedValue(task)
}

function taskWith(overrides: Partial<Task>): Task {
  return {
    ...mockTasks.items[0],
    progress_log: undefined,
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
  fireEvent.click(await screen.findByRole('button', { name: '补充信息并继续' }))
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
    vi.mocked(api.tasks.markPublished).mockResolvedValue(taskWith({ id: 'task-1', status: 'completed', published: false }))
    vi.mocked(api.tasks.publishApprove).mockResolvedValue({ approved: true })
    vi.mocked(api.tasks.publishReject).mockResolvedValue({ rejected: true })
    vi.mocked(api.tasks.downloadZipBlob).mockResolvedValue(new Blob(['zip'], { type: 'application/zip' }))
    vi.mocked(api.projects.get).mockResolvedValue(mockProjectDetail)
    vi.mocked(api.projects.list).mockResolvedValue(mockProjects)
    vi.mocked(api.billing.wallet).mockResolvedValue(mockBillingWallet)
    vi.mocked(api.billing.catalog).mockResolvedValue(mockBillingCatalog)
    vi.mocked(api.agentProfiles.list).mockResolvedValue([
      { id: 'effective', display_name: '性价比', provider: 'deepseek', model_name: 'deepseek-v4-flash', description: '适合日常创作', min_tier: 'free', available: true },
      { id: 'balanced', display_name: '平衡型', provider: 'volcengine_ark', model_name: 'doubao-seed-evolving', description: '质量与速度平衡', min_tier: 'pro', available: true },
      { id: 'quality', display_name: '极致效果', provider: 'moonshot', model_name: 'kimi-k3[1m]', description: '复杂高质量创作', min_tier: 'enterprise', available: true },
    ])
    vi.mocked(api.imageModels.list).mockResolvedValue({
      tier: 'pro',
      items: [
        { key: '', display_name: '系统默认', provider: '', min_tier: 'free', is_custom: false },
        { key: 'source-model', display_name: '源模型', provider: 'gemini', min_tier: 'pro', is_custom: false },
      ],
    })
    vi.mocked(api.seednoteAnalytics.getByTask).mockResolvedValue({ series: [] })
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

  it('shows compact running progress and hides workflow stage grid', async () => {
    mockTask(taskWith({
      status: 'running',
      progress: 42,
      progress_log: '准备素材\nUsing tool: Read',
      latest_progress: { stage: 'writing', title: '正在写作正文', percent: 42 },
      result: null,
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByText('42%')).toBeInTheDocument()
    const progressHeading = screen.getByRole('heading', { name: '正在写作正文' })
    const progressSection = progressHeading.closest('section') as HTMLElement
    // progress_log noise like "Using tool: ..." must NOT leak into the card.
    expect(within(progressSection).queryByText('Using tool: Read')).not.toBeInTheDocument()
    expect(screen.queryByText('创作进度')).not.toBeInTheDocument()
    expect(screen.queryByText('当前阶段')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '任务结果' })).toBeInTheDocument()
    expect(screen.getByText('结果生成后将在这里显示')).toBeInTheDocument()
    expect(screen.queryByText('任务配置')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '执行动态' })).not.toBeInTheDocument()
    expect(screen.queryByText('未生成素材使用结论，仅展示任务输入。')).not.toBeInTheDocument()
  })

  it('shows the frozen task model instead of the current capability catalog', async () => {
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
      result: null,
    }))

    render(<TaskDetailPage />)
    await openTaskDetails('配置')

    expect(screen.getByText('kimi-k2.7-code')).toBeInTheDocument()
    expect(screen.queryByText('kimi-k3[1m]')).not.toBeInTheDocument()
  })

  it('shows balanced context after results and opens logs in one action', async () => {
    mockTask(taskWith({
      status: 'running',
      progress: 42,
      progress_log: '已读取参考素材\n正在写作正文',
      latest_progress: {
        stage: 'writing',
        title: '正在写作正文',
        description: '正在优化标题与段落结构',
        percent: 42,
      },
      input_attachments: [{ type: 'image', file_name: 'tea-reference.jpg' }],
      project_snapshot: {
        project_name: '茶小茶',
        platform: 'article',
        visual_style: '清新茶感摄影',
        image_ratio: '3:4',
      },
      result: null,
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

  it('opens Overview from the general More details command', async () => {
    mockTask(taskWith({ status: 'completed', result: null }))

    render(<TaskDetailPage />)

    await openTaskDetails()
  })

  it('closes task details on a route change and reopens on Overview', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'running', result: null, completed_at: '' }))
    const view = render(<TaskDetailPage />)

    const context = await screen.findByRole('region', { name: '任务上下文' })
    fireEvent.click(within(context).getByRole('button', { name: '打开执行日志' }))
    expect(screen.getByRole('tab', { name: '日志' })).toHaveAttribute('aria-selected', 'true')

    routeState.taskId = 'task-2'
    vi.mocked(api.tasks.get).mockResolvedValue(taskWith({
      id: 'task-2',
      title: '第二个任务',
      status: 'running',
      result: null,
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
      result: null,
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '失败任务 B',
      status: 'failed',
      result: null,
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
      result: null,
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '已完成任务 B',
      prompt: '任务 B 的原始要求',
      status: 'completed',
      result: null,
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
      result: null,
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '已完成任务 B',
      status: 'completed',
      project_id: 'ch-1',
      result: null,
    })
    const request = deferred<Task>()
    vi.mocked(api.tasks.clone).mockReturnValue(request.promise)
    const view = renderWithCachedTasks(taskA, taskB)
    const invalidateQueries = vi.spyOn(view.queryClient, 'invalidateQueries')

    const dialog = await openCloneDialog()
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('测试项目'))
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
    const taskA = taskWith({ id: 'task-1', title: '已完成任务 A', status: 'completed', project_id: 'ch-1', result: null })
    const taskB = taskWith({ id: 'task-2', title: '已完成任务 B', status: 'completed', project_id: 'ch-1', result: null })
    const request = deferred<Task>()
    vi.mocked(api.tasks.clone).mockReturnValue(request.promise)
    const view = renderWithCachedTasks(taskA, taskB)

    const dialog = await openCloneDialog()
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('测试项目'))
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
    const taskA = taskWith({ id: 'task-1', title: '失败任务 A', status: 'failed', result: null })
    const taskB = taskWith({ id: 'task-2', title: '失败任务 B', status: 'failed', result: null })
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
    const taskA = taskWith({ id: 'task-1', title: '已完成任务 A', status: 'completed', result: null })
    const taskB = taskWith({ id: 'task-2', title: '运行中任务 B', status: 'running', result: null })
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

  it('does not show a stale published-state error after switching tasks', async () => {
    const taskA = taskWith({ id: 'task-1', title: '已完成任务 A', status: 'completed', published: false, result: null })
    const taskB = taskWith({ id: 'task-2', title: '已完成任务 B', status: 'completed', published: false, result: null })
    const publishRequest = deferred<Task>()
    vi.mocked(api.tasks.markPublished).mockReturnValue(publishRequest.promise)
    const view = renderWithCachedTasks(taskA, taskB)

    fireEvent.click(await screen.findByRole('checkbox', { name: '已发布' }))
    await waitFor(() => expect(api.tasks.markPublished).toHaveBeenCalledWith('task-1', true))

    routeState.taskId = 'task-2'
    view.rerender(<TaskDetailPage />)
    expect(await screen.findByRole('heading', { name: '已完成任务 B' })).toBeInTheDocument()

    await act(async () => {
      publishRequest.reject(new Error('task A publish failed'))
      await expect(publishRequest.promise).rejects.toThrow('task A publish failed')
    })

    expect(toastErrorMock).not.toHaveBeenCalled()
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
      result: null,
      completed_at: '',
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '已完成任务 B',
      status: 'completed',
      progress_log: 'B 持久化日志',
      result: null,
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
      result: null,
      completed_at: '',
    })
    const taskB = taskWith({
      id: 'task-2',
      title: '运行任务 B',
      status: 'running',
      progress_log: 'task-2 持久化日志',
      result: null,
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
        result: null,
        completed_at: '',
      })
      const taskB = taskWith({
        id: 'task-2',
        title: '已完成任务 B',
        status: 'completed',
        progress_log: 'B 终态日志',
        result: null,
      })
      const taskC = taskWith({
        id: 'task-3',
        title: '等待任务 C',
        status: 'pending',
        progress_log: 'C 等待日志',
        result: null,
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
        event: 'progress',
        data: JSON.stringify('第一条持久化日志\n第二条持久化日志\n第三条新增日志'),
      }
      await waitForAbort(signal)
    })
    const task = taskWith({
      id: 'task-1',
      status: 'running',
      progress_log: '第一条持久化日志\n第二条持久化日志',
      result: null,
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
        event: 'progress',
        data: JSON.stringify('第一条持久化日志\n第二条持久化日志'),
      }
      await waitForAbort(signal)
    })
    const task = taskWith({
      id: 'task-1',
      status: 'running',
      progress_log: '第一条持久化日志\n第二条持久化日志',
      result: null,
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
      result: null,
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
        result: null,
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
      progress: 42,
      progress_log: '## 阶段日志\n- 已完成选题',
      latest_progress: { stage: 'writing', title: '正在写作正文', percent: 42 },
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
      result: null,
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

  it('hides the progress card after completion', async () => {
    mockTask(taskWith({
      status: 'completed',
      progress: 100,
      progress_log: '[100%] 任务完成',
      result: null,
    }))

    render(<TaskDetailPage />)

    await waitFor(() => expect(screen.getByText('已完成')).toBeInTheDocument())
    expect(screen.queryByText('进度')).not.toBeInTheDocument()
    expect(screen.queryByText('任务执行成功')).not.toBeInTheDocument()
    expect(screen.queryByText('结果生成后将在这里显示')).not.toBeInTheDocument()
  })

  it('shows review summary without terminal workflow stage grid', async () => {
    mockTask(taskWith({
      status: 'completed',
      progress: 100,
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
      result: null,
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'file-1',
      task_id: 'task-1',
      role: 'output',
      file_name: 'article.html',
      mime_type: 'text/html',
      file_size: 1024,
      url: '/api/v1/files/file-1',
      created_at: '2026-07-06T03:00:00Z',
    }])

    render(<TaskDetailPage />)

    const review = await screen.findByText('发布前检查')
    const files = await screen.findByText('生成文件 (1)')
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
      progress: 100,
      input_attachments: [{ type: 'image', file_name: 'front.png' }],
      result: null,
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([
      {
        id: 'file-summary',
        task_id: 'task-1',
        role: 'artifact',
        file_name: 'reference-usage-summary.json',
        mime_type: 'application/json',
        file_size: 1024,
        url: '/api/v1/files/file-summary',
        created_at: '2026-07-10T00:00:00Z',
      },
      {
        id: 'file-1',
        task_id: 'task-1',
        role: 'output',
        file_name: 'article.html',
        mime_type: 'text/html',
        file_size: 1024,
        url: '/api/v1/files/file-1',
        created_at: '2026-07-10T00:00:01Z',
      },
    ])

    render(<TaskDetailPage />)

    const taskStatus = await screen.findByText('已完成')
    expect(taskStatus).toBeInTheDocument()
    const filesHeading = await screen.findByText('生成文件 (2)')
    expect(filesHeading).toBeInTheDocument()
    expect(screen.queryByText('参考素材摘要暂时无法显示')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /预览 article\.html/ })).toBeInTheDocument()

    await openTaskDetails('素材')
    expect(await screen.findByText('参考素材摘要暂时无法显示')).toBeInTheDocument()
    expect(taskStatus).toBeInTheDocument()
    expect(screen.getByText('生成文件 (2)')).toBeInTheDocument()
  })

  it('separates collected failure artifacts from generated files', async () => {
    mockTask(taskWith({
      status: 'failed',
      result: null,
    }))
    const now = '2026-07-15T03:00:00Z'
    const files: TaskFile[] = [
      {
        id: 'published-1',
        task_id: 'task-1',
        execution_id: 'execution-success',
        state: 'published',
        role: 'content',
        file_name: 'content.md',
        mime_type: 'text/markdown',
        file_size: 128,
        url: '/content.md',
        created_at: now,
      },
      {
        id: 'collected-1',
        task_id: 'task-1',
        execution_id: 'execution-failed',
        state: 'collected',
        role: 'other',
        file_name: 'failure-state.json',
        mime_type: 'application/json',
        file_size: 96,
        url: '/failure-state.json',
        created_at: now,
      },
    ]
    vi.mocked(api.tasks.files).mockResolvedValue(files)

    render(<TaskDetailPage />)

    const generatedHeading = await screen.findByText('生成文件 (1)')
    const failedHeading = await screen.findByText('失败执行文件 (1)')
    const generatedSection = generatedHeading.closest('[data-slot="card"]') as HTMLElement
    const failedSection = failedHeading.closest('[data-slot="card"]') as HTMLElement
    expect(within(generatedSection).getByText('content.md')).toBeInTheDocument()
    expect(within(generatedSection).queryByText('failure-state.json')).not.toBeInTheDocument()
    expect(within(failedSection).getByText('failure-state.json')).toBeInTheDocument()
    expect(within(failedSection).queryByText('content.md')).not.toBeInTheDocument()
  })

  it('places published Seednote analytics after generated deliverables', async () => {
    mockTask(taskWith({
      type: 'seednote',
      status: 'completed',
      published: true,
      result: null,
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([{
      id: 'seednote-output',
      task_id: 'task-1',
      state: 'published',
      role: 'content',
      file_name: 'content.md',
      mime_type: 'text/markdown',
      file_size: 128,
      url: '/content.md',
      created_at: '2026-07-15T03:00:00Z',
    }])

    render(<TaskDetailPage />)

    const generatedHeading = await screen.findByText('生成文件 (1)')
    const analyticsHeading = await screen.findByText('种草笔记数据')
    const context = screen.getByRole('region', { name: '任务上下文' })
    expect(generatedHeading.compareDocumentPosition(analyticsHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(analyticsHeading.compareDocumentPosition(context) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('keeps completed delivery controls available without a runtime balance lock', async () => {
    mockTask(taskWith({
      status: 'completed',
      result: null,
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([
      {
        id: 'file-1',
        task_id: 'task-1',
        role: 'output',
        file_name: 'article.html',
        mime_type: 'text/html',
        file_size: 1024,
        url: '/api/v1/files/file-1',
        created_at: '2026-07-06T03:00:00Z',
      },
    ])

    render(<TaskDetailPage />)

    expect(await screen.findByText('生成文件 (1)')).toBeInTheDocument()
    expect(screen.queryByText('交付已锁定')).not.toBeInTheDocument()
    const zipButton = screen.getByRole('button', { name: /下载全部/ })
    expect(zipButton).toBeEnabled()
    expect(screen.getByRole('button', { name: /预览 article\.html/ })).toBeEnabled()
    expect(screen.getByRole('button', { name: /下载 article\.html/ })).toBeEnabled()
  })

  it('does not load shared clone-form data until a terminal task opens the clone dialog', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'completed', project_id: 'ch-1', result: null }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(api.projects.list).not.toHaveBeenCalled()
    expect(api.billing.wallet).not.toHaveBeenCalled()
    expect(api.billing.catalog).not.toHaveBeenCalled()
    expect(api.imageModels.list).not.toHaveBeenCalled()

    await openCloneDialog()

    await waitFor(() => expect(api.projects.list).toHaveBeenCalledTimes(1))
    expect(api.billing.wallet).toHaveBeenCalledTimes(1)
    expect(api.billing.catalog).toHaveBeenCalledTimes(1)
    expect(api.imageModels.list).toHaveBeenCalledTimes(1)
  })

  it('opens the shared full clone form with editable source defaults and navigates to the first created task', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'completed',
      progress: 100,
      prompt: '原始任务要求',
      project_id: 'ch-1',
      image_ratio: '16:9',
      image_model_key: 'source-model',
      result: JSON.stringify({ files: null, output: '' }),
    }))

    render(<TaskDetailPage />)

    const dialog = await openCloneDialog()
    expect(api.tasks.clone).not.toHaveBeenCalled()
    await waitFor(() => expect(within(dialog).getByRole('combobox', { name: '项目上下文' })).toHaveTextContent('测试项目'))
    expect(within(dialog).getByPlaceholderText('描述创作目标、内容要求和素材使用方式...')).toHaveValue('原始任务要求')
    expect(within(dialog).getByText('数量')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '1' })).toBeInTheDocument()
    expect(within(dialog).getByRole('radio', { name: '16:9 widescreen default' })).toBeChecked()
    expect(await within(dialog).findByText('源模型')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '继续执行此任务' })).not.toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: '克隆' }))

    await waitFor(() => expect(api.tasks.clone).toHaveBeenCalledWith('task-1', expect.objectContaining({
      project_id: 'ch-1',
      quantity: 1,
      prompt: '原始任务要求',
      image_ratio: '16:9',
      image_model_key: 'source-model',
    })))
    await waitFor(() => expect(mockNavigate).toHaveBeenCalledWith('/tasks/task-clone'))
    expect(toastSuccessMock).toHaveBeenCalledTimes(1)
  })

  it('keeps Continue isolated to the prompt-only resume dialog', async () => {
    mockTask(taskWith({ id: 'task-1', status: 'completed', result: null }))

    render(<TaskDetailPage />)

    fireEvent.click(await screen.findByRole('button', { name: '继续执行' }))
    const dialog = await screen.findByRole('dialog', { name: '继续执行此任务' })
    expect(within(dialog).getByLabelText('继续任务要求')).toHaveValue('')
    expect(screen.queryByRole('dialog', { name: '克隆任务' })).not.toBeInTheDocument()
    expect(api.tasks.clone).not.toHaveBeenCalled()
  })

  it.each([
    { published: false, nextPublished: true },
    { published: true, nextPublished: false },
  ])('updates completed published state from $published to $nextPublished', async ({ published, nextPublished }) => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'completed',
      published,
      result: null,
    }))

    render(<TaskDetailPage />)

    const checkbox = await screen.findByRole('checkbox', { name: '已发布' })
    if (published) expect(checkbox).toBeChecked()
    else expect(checkbox).not.toBeChecked()
    fireEvent.click(checkbox)

    await waitFor(() => {
      expect(api.tasks.markPublished).toHaveBeenCalledWith('task-1', nextPublished)
    })
  })

  it('disables the published checkbox while pending and refetches unchanged server state after an error', async () => {
    const serverTask = taskWith({ id: 'task-1', status: 'completed', published: true, result: null })
    vi.mocked(api.tasks.get)
      .mockResolvedValueOnce(serverTask)
      .mockResolvedValue(serverTask)
    const request = deferred<Task>()
    vi.mocked(api.tasks.markPublished).mockReturnValue(request.promise)

    render(<TaskDetailPage />)

    const checkbox = await screen.findByRole('checkbox', { name: '已发布' })
    fireEvent.click(checkbox)
    await waitFor(() => expect(api.tasks.markPublished).toHaveBeenCalledWith('task-1', false))
    expect(checkbox).toHaveAttribute('aria-disabled', 'true')
    expect(checkbox).toBeChecked()

    await act(async () => {
      request.reject(new Error('publish failed'))
      await expect(request.promise).rejects.toThrow('publish failed')
    })

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalled())
    await waitFor(() => expect(api.tasks.get).toHaveBeenCalledTimes(2))
    expect(checkbox).not.toHaveAttribute('aria-disabled', 'true')
    expect(checkbox).toBeChecked()
  })

  it('reconciles an error-after-commit publish response with the persisted server state', async () => {
    const originalTask = taskWith({ id: 'task-1', status: 'completed', published: false, result: null })
    const persistedTask = taskWith({ ...originalTask, published: true })
    vi.mocked(api.tasks.get)
      .mockResolvedValueOnce(originalTask)
      .mockResolvedValue(persistedTask)
    vi.mocked(api.tasks.markPublished).mockRejectedValue(new Error('tracking failed after commit'))

    render(<TaskDetailPage />)

    const checkbox = await screen.findByRole('checkbox', { name: '已发布' })
    expect(checkbox).not.toBeChecked()
    fireEvent.click(checkbox)

    await waitFor(() => expect(api.tasks.markPublished).toHaveBeenCalledWith('task-1', true))
    await waitFor(() => expect(toastErrorMock).toHaveBeenCalled())
    await waitFor(() => expect(api.tasks.get).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(checkbox).toBeChecked())
  })

  it.each([
    { published: false },
    { published: true },
  ])('shows the complete terminal action group for a completed task with published=$published', async ({ published }) => {
    mockTask(taskWith({ id: 'task-1', status: 'completed', published, result: null }))

    render(<TaskDetailPage />)

    const checkbox = await screen.findByRole('checkbox', { name: '已发布' })
    if (published) expect(checkbox).toBeChecked()
    else expect(checkbox).not.toBeChecked()
    expect(screen.getByRole('button', { name: '继续执行' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除任务' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多任务操作' })).not.toBeInTheDocument()
  })

  it('uploads resume files to OSS, keeps failed input for retry, then reopens blank', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
      result: null,
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
      result: null,
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
      result: JSON.stringify({ files: null, output: '' }),
    }))
    const request = deferred<Task>()
    vi.mocked(api.tasks.resume).mockReturnValue(request.promise)

    render(<TaskDetailPage />)

    const reopenTrigger = await screen.findByRole('button', { name: '补充信息并继续' })
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
      result: null,
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
      result: JSON.stringify({ files: null, output: '' }),
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
      progress: status === 'running' ? 42 : 0,
      latest_progress: status === 'running'
        ? { stage: 'writing', title: '正在写作正文', percent: 42 }
        : undefined,
      result: null,
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
    expect(api.imageModels.list).not.toHaveBeenCalled()
  })

  it('shows visible continue, clone, and delete actions for a cancelled task', async () => {
    mockTask(taskWith({
      status: 'cancelled',
      result: null,
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '继续执行' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除任务' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '已发布' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多任务操作' })).not.toBeInTheDocument()
    expect(screen.getByText('执行已停止')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /重新执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /再试一次/ })).not.toBeInTheDocument()
  })

  it('shows failed-task diagnosis from the deployed API error_message field without duplicating header actions', async () => {
    mockTask(taskWith({
      status: 'failed',
      error_message: '模型超时',
      result: null,
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('button', { name: '补充信息并继续' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '继续执行' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '克隆任务' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '删除任务' })).toBeInTheDocument()
    expect(screen.queryByRole('checkbox', { name: '已发布' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '更多任务操作' })).not.toBeInTheDocument()
    expect(screen.getByText('执行失败')).toBeInTheDocument()
    expect(screen.getByText('模型超时')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '补充信息并继续' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /返回任务列表/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /检查项目配置/ })).not.toBeInTheDocument()
  })

  it('does not invent an interruption reason or preserved workspace when failure details are absent', async () => {
    mockTask(taskWith({
      status: 'failed',
      result: null,
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByText('任务未完成')).toBeInTheDocument()
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
          id: 'content-image-charge',
          charge_kind: 'operation',
          policy: 'accepted_task_operation',
          sku_id: 'image.seedream.content.v1',
          credits: 500,
          tool_call_id: 'image:content-1',
          created_at: '2026-07-10T00:01:00.000Z',
        },
        {
          id: 'cover-image-charge',
          charge_kind: 'operation',
          policy: 'accepted_task_operation',
          sku_id: 'image.seedream.cover.v1',
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
      result: null,
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
    expect(screen.getByText('图片模型')).toBeInTheDocument()
    expect(screen.getByText('安般')).toBeInTheDocument()
    expect(screen.getByText('dan-koe')).toBeInTheDocument()
    expect(screen.getByText('autumn-warm')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '概览' }))
    expect(screen.getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('积分明细')).toBeInTheDocument()
    expect(screen.getByText('累计扣费')).toBeInTheDocument()
    expect(screen.getByText('7,300 积分')).toBeInTheDocument()
    expect(screen.getByText('任务固定费')).toBeInTheDocument()
    expect(screen.getByText('内容图生成费')).toBeInTheDocument()
    expect(screen.getByText('封面图生成费')).toBeInTheDocument()
    expect(screen.getByText('增值操作费')).toBeInTheDocument()
    expect(screen.getByText('6,000 积分')).toBeInTheDocument()
    expect(screen.getAllByText('500 积分')).toHaveLength(2)
    expect(screen.getByText('300 积分')).toBeInTheDocument()
  })

  it('opens project details in a dialog instead of navigating to the projects list', async () => {
    mockTask(taskWith({
      type: 'article',
      status: 'completed',
      result: null,
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

  it('renders dynamic logs as markdown and keeps copyable raw text', async () => {
    const writeText = vi.fn()
    Object.assign(navigator, {
      clipboard: { writeText },
    })
    mockTask(taskWith({
      status: 'running',
      progress: 10,
      progress_log: '## 阶段日志\n- 已完成选题\n```txt\nraw block\n```',
      result: null,
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
