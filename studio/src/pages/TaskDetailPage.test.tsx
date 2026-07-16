import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TaskDetailPage from './TaskDetailPage'
import { render } from '@/test/test-utils'
import { mockProjectDetail, mockTasks } from '@/test/mocks/handlers'
import type { Task, TaskFile } from '@/types'
import { api } from '@/lib/api'

const mockNavigate = vi.fn()

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
    useParams: () => ({ id: 'task-1' }),
    useNavigate: () => mockNavigate,
  }
})

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ token: 'test-token' }),
}))

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
        clone: vi.fn(),
        resume: vi.fn(),
        markPublished: vi.fn(),
        files: vi.fn().mockResolvedValue([]),
        downloadZipBlob: vi.fn(),
        videoProduction: vi.fn(),
      },
      projects: {
        ...actual.api.projects,
        get: vi.fn().mockResolvedValue(mockProjectDetail),
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

async function openTaskDetails(tab?: '概览' | '配置' | '素材' | '日志') {
  fireEvent.click(await screen.findByRole('button', { name: /更多详情/ }))
  expect(await screen.findByRole('heading', { name: '任务详情' })).toBeInTheDocument()
  if (tab) {
    fireEvent.click(screen.getByRole('tab', { name: tab }))
  }
}

describe('TaskDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.files).mockResolvedValue([])
    vi.mocked(api.tasks.videoProduction).mockResolvedValue({
      task_id: 'task-1',
      scenario_key: 'live_selling',
      production_mode: 'guided',
      artifacts: {},
      retake_actions: [],
      next_actions: [],
    })
    vi.mocked(api.tasks.clone).mockResolvedValue(taskWith({ id: 'task-clone', status: 'pending' }))
    vi.mocked(api.tasks.resume).mockResolvedValue(taskWith({ id: 'task-1', status: 'pending' }))
    vi.mocked(api.tasks.markPublished).mockResolvedValue(taskWith({ id: 'task-1', status: 'completed', published: false }))
    vi.mocked(api.tasks.downloadZipBlob).mockResolvedValue(new Blob(['zip'], { type: 'application/zip' }))
    vi.mocked(api.projects.get).mockResolvedValue(mockProjectDetail)
    vi.mocked(api.seednoteAnalytics.getByTask).mockResolvedValue({ series: [] })
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
    expect(screen.getByText('正在写作正文')).toBeInTheDocument()
    // progress_log noise like "Using tool: ..." must NOT leak into the card.
    expect(screen.queryByText('Using tool: Read')).not.toBeInTheDocument()
    expect(screen.queryByText('创作进度')).not.toBeInTheDocument()
    expect(screen.queryByText('当前阶段')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '任务结果' })).toBeInTheDocument()
    expect(screen.getByText('结果生成后将在这里显示')).toBeInTheDocument()
    expect(screen.queryByText('任务配置')).not.toBeInTheDocument()
    expect(screen.queryByText('执行日志')).not.toBeInTheDocument()
    expect(screen.queryByText('未生成素材使用结论，仅展示任务输入。')).not.toBeInTheDocument()
  })

  it('keeps the pending result destination when video production has only missing artifacts', async () => {
    mockTask(taskWith({
      type: 'videocreator',
      status: 'pending',
      progress: 0,
      video_creator_config: {
        scenario_key: 'live_selling',
        production_mode: 'guided',
      },
      result: null,
      completed_at: '',
    }))
    let resolveProduction!: (value: Awaited<ReturnType<typeof api.tasks.videoProduction>>) => void
    vi.mocked(api.tasks.videoProduction).mockImplementation(() => new Promise((resolve) => {
      resolveProduction = resolve
    }))
    const missingProduction: Awaited<ReturnType<typeof api.tasks.videoProduction>> = {
      task_id: 'task-1',
      scenario_key: 'live_selling',
      production_mode: 'guided',
      artifacts: {
        'creative-brief.md': { status: 'missing', file_name: 'creative-brief.md' },
        'quality-review.md': { status: 'missing', file_name: 'quality-review.md' },
        'delivery-manifest.json': { status: 'missing', file_name: 'delivery-manifest.json' },
      },
      retake_actions: ['keep', 're_roll'],
      next_actions: ['continue_editing'],
    }

    render(<TaskDetailPage />)

    await waitFor(() => expect(api.tasks.videoProduction).toHaveBeenCalledWith('task-1'))
    await act(async () => {
      resolveProduction(missingProduction)
    })
    expect(screen.getByRole('heading', { name: '任务结果' })).toBeInTheDocument()
    expect(screen.getByText('结果生成后将在这里显示')).toBeInTheDocument()
    expect(screen.queryByText('制作状态')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Re-roll' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '继续剪辑' })).not.toBeInTheDocument()
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
    expect(screen.queryByText('创建时项目快照')).not.toBeInTheDocument()

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
    const moreDetails = screen.getByRole('button', { name: /更多详情/ })
    expect(generatedHeading.compareDocumentPosition(analyticsHeading) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(analyticsHeading.compareDocumentPosition(moreDetails) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('disables delivery controls for payment-required tasks', async () => {
    mockTask(taskWith({
      status: 'completed',
      billing_status: 'payment_required',
      billing_shortfall_credits: 3200,
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
    expect(screen.getByText('交付已锁定')).toBeInTheDocument()
    const zipButton = screen.getByRole('button', { name: /下载全部/ })
    expect(zipButton).toBeDisabled()
    expect(screen.getByRole('button', { name: /预览 article\.html/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /下载 article\.html/ })).toBeDisabled()
    expect(api.tasks.videoProduction).not.toHaveBeenCalled()
  })

  it('lets completed tasks be cloned as a fresh task', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'completed',
      progress: 100,
      result: null,
    }))

    render(<TaskDetailPage />)

    fireEvent.click(await screen.findByRole('button', { name: '更多任务操作' }))
    const rerunButton = await screen.findByRole('menuitem', { name: /克隆任务/ })
    fireEvent.click(rerunButton)

    await waitFor(() => {
      expect(api.tasks.clone).toHaveBeenCalledWith('task-1')
      expect(mockNavigate).toHaveBeenCalledWith('/tasks/task-clone')
    })
  })

  it('keeps the low-frequency unpublish action in the more menu', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'completed',
      published: true,
      result: null,
    }))

    render(<TaskDetailPage />)

    fireEvent.click(await screen.findByRole('button', { name: '更多任务操作' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: '取消发布标记' }))

    await waitFor(() => {
      expect(api.tasks.markPublished).toHaveBeenCalledWith('task-1', false)
    })
  })

  it('opens a continue dialog and submits prompt files and labels for the current task', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
      result: null,
    }))

    render(<TaskDetailPage />)

    const continueButton = await screen.findByRole('button', { name: '补充信息并继续' })
    fireEvent.click(continueButton)

    const dialogTitle = await screen.findByText('继续执行此任务')
    const dialog = dialogTitle.closest('[data-slot="dialog-content"]') as HTMLElement
    expect(dialog).toBeTruthy()
    fireEvent.change(within(dialog).getByLabelText('补充指令'), {
      target: { value: '请基于现有草稿继续修改' },
    })
    const file = new File(['notes'], 'notes.md', { type: 'text/markdown' })
    fireEvent.change(within(dialog).getByLabelText('补充文件'), {
      target: { files: [file] },
    })
    fireEvent.change(within(dialog).getByPlaceholderText('例如：客户反馈、参考图、修改意见、产品参数'), {
      target: { value: '修改意见' },
    })
    fireEvent.click(within(dialog).getByText('提交并继续').closest('button') as HTMLButtonElement)

    await waitFor(() => {
      expect(api.tasks.resume).toHaveBeenCalledWith('task-1', {
        prompt: '请基于现有草稿继续修改',
        files: [file],
        fileLabels: ['修改意见'],
      })
      expect(mockNavigate).not.toHaveBeenCalledWith('/tasks/task-clone')
    })
  })

  it('keeps resume file labels aligned when a file is removed', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
      result: null,
    }))

    render(<TaskDetailPage />)

    const continueButton = await screen.findByRole('button', { name: '补充信息并继续' })
    fireEvent.click(continueButton)

    const dialogTitle = await screen.findByText('继续执行此任务')
    const dialog = dialogTitle.closest('[data-slot="dialog-content"]') as HTMLElement
    const first = new File(['first'], 'first.md', { type: 'text/markdown' })
    const second = new File(['second'], 'second.md', { type: 'text/markdown' })
    fireEvent.change(within(dialog).getByLabelText('补充文件'), {
      target: { files: [first, second] },
    })
    const labelInputs = within(dialog).getAllByPlaceholderText('例如：客户反馈、参考图、修改意见、产品参数')
    fireEvent.change(labelInputs[0], { target: { value: '第一份' } })
    fireEvent.change(labelInputs[1], { target: { value: '第二份' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '移除 first.md' }))
    fireEvent.click(within(dialog).getByText('提交并继续').closest('button') as HTMLButtonElement)

    await waitFor(() => {
      expect(api.tasks.resume).toHaveBeenCalledWith('task-1', {
        prompt: '',
        files: [second],
        fileLabels: ['第二份'],
      })
    })
  })

  it('does not show clone for running tasks', async () => {
    mockTask(taskWith({
      status: 'running',
      progress: 42,
      latest_progress: { stage: 'writing', title: '正在写作正文', percent: 42 },
      result: null,
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByText('正在写作正文')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /继续执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /克隆任务/ })).not.toBeInTheDocument()
  })

  it('does not show clone for pending tasks', async () => {
    mockTask(taskWith({
      status: 'pending',
      progress: 0,
      latest_progress: undefined,
      result: null,
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    await waitFor(() => expect(screen.getByText('任务等待执行中...')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /继续执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /克隆任务/ })).not.toBeInTheDocument()
  })

  it('keeps recovery actions in the header for cancelled tasks', async () => {
    mockTask(taskWith({
      status: 'cancelled',
      result: null,
    }))

    render(<TaskDetailPage />)

    expect(await screen.findAllByRole('button', { name: /继续执行/ })).toHaveLength(1)
    expect(screen.queryByRole('button', { name: /克隆任务/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '更多任务操作' })).toBeInTheDocument()
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
    expect(screen.queryByRole('button', { name: /继续执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /克隆任务/ })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '更多任务操作' })).toBeInTheDocument()
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
      credits_charged: 128,
      credits_summary: {
        task_consumed: 128,
        operation_consumed: 80,
        refunded: 20,
        net_consumed: 188,
      },
      credit_transactions: [
        {
          id: 1,
          user_id: 'user-1',
          type: 'task_deduct',
          amount: -128,
          balance_after: 872,
          task_id: 'task-1',
          description: '任务消耗 (article) -128',
          created_at: '2026-07-03T08:00:00Z',
        },
        {
          id: 2,
          user_id: 'user-1',
          type: 'image_gen',
          amount: -80,
          balance_after: 792,
          task_id: 'task-1',
          description: '操作扣费 (image_gen) -80',
          created_at: '2026-07-03T08:01:00Z',
        },
        {
          id: 3,
          user_id: 'user-1',
          type: 'task_refund',
          amount: 20,
          balance_after: 812,
          task_id: 'task-1',
          description: '任务取消退还 +20',
          created_at: '2026-07-03T08:02:00Z',
        },
      ],
      total_cost_usd: 1.23,
      result: null,
      project_snapshot: {
        project_name: '快照项目',
        platform: 'article',
        visual_style: '柔光生活摄影',
        image_ratio: '16:9',
        reference_image_url: 'https://cdn.example.com/ref.png',
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
    expect(screen.getByText('积分消耗')).toBeInTheDocument()
    expect(screen.queryByText('操作消耗')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '查看明细' }))
    await waitFor(() => {
      expect(screen.queryByRole('heading', { name: '任务详情' })).not.toBeInTheDocument()
    })
    const creditDialogTitle = await screen.findByRole('heading', { name: '积分明细' })
    const creditDialog = creditDialogTitle.closest('[data-slot="dialog-content"]') as HTMLElement
    expect(within(creditDialog).getByText('积分明细')).toBeInTheDocument()
    expect(within(creditDialog).getAllByText('积分消耗').length).toBeGreaterThan(0)
    expect(within(creditDialog).getByText('操作消耗')).toBeInTheDocument()
    expect(within(creditDialog).getByText('退还积分')).toBeInTheDocument()
    expect(within(creditDialog).getByText('净消耗')).toBeInTheDocument()
    expect(screen.getAllByText('188').length).toBeGreaterThan(0)
    expect(screen.getByText('图片生成')).toBeInTheDocument()
    expect(screen.getByText('-80')).toBeInTheDocument()
    expect(screen.getByText('生成公众号文章扣除积分128')).toBeInTheDocument()
    expect(screen.getByText('AI 生图扣除积分80')).toBeInTheDocument()
    expect(screen.queryByText('执行成本')).not.toBeInTheDocument()
    expect(screen.queryByText('$1.23')).not.toBeInTheDocument()

    fireEvent.click(within(creditDialog).getByRole('button', { name: 'Close' }))
    expect(await screen.findByRole('heading', { name: '任务详情' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '概览' })).toHaveAttribute('aria-selected', 'true')
  })

  it('shows generated video files in the files list and opens the video result in a dialog', async () => {
    mockTask(taskWith({
      type: 'videocreator',
      status: 'completed',
      prompt: '做一条办公室个人 IP 种草视频',
      video_generation_id: 'vg-1',
      video_estimated_credits: 7440,
      video_credits_charged: 7440,
      video_creator_input: {
        brief: '做一条办公室个人 IP 种草视频',
        references: [{ type: 'video_url', url: 'https://cdn.example.com/ref.mp4' }],
        hard_constraints: { ratio: '9:16', duration: 15 },
      },
      video_creator_config: {
        creative_type: 'personal_ip',
        purpose: 'planting',
        subject_profile: '30 岁效率博主，黑色衬衫，语速快但亲和',
        audience: '想提升工作效率的职场新人',
        single_message: '用一个可复制的方法把会议记录变成行动清单',
        model_key: 'seedance-2.0-mini',
        resolution: '720p',
        ratio: '9:16',
        duration: 15,
        estimated_credits: 7440,
        references: [{ type: 'video_url', url: 'https://cdn.example.com/ref.mp4', reference_role: 'rhythm', input_duration_seconds: 60 }],
      },
      result: null,
    }))
    vi.mocked(api.tasks.files).mockResolvedValue([
      {
        id: 'file-video',
        task_id: 'task-1',
        role: 'output',
        file_name: 'final.mp4',
        mime_type: 'video/mp4',
        file_size: 8200000,
        url: 'https://cdn.example.com/final.mp4',
        created_at: '2026-07-04T08:00:00Z',
      },
      {
        id: 'file-plan',
        task_id: 'task-1',
        role: 'output',
        file_name: 'quality-review.md',
        mime_type: 'text/markdown',
        file_size: 512,
        url: '/api/v1/files/file-plan',
        created_at: '2026-07-04T08:00:00Z',
      },
    ])

    render(<TaskDetailPage />)

    await openTaskDetails('配置')
    expect(screen.getByRole('heading', { name: '用户输入' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Agent 解析结果' })).toBeInTheDocument()
    expect(screen.getAllByText('做一条办公室个人 IP 种草视频').length).toBeGreaterThan(0)
    expect(screen.getByText('个人 IP')).toBeInTheDocument()
    expect(screen.getByText('种草')).toBeInTheDocument()
    expect(screen.getByText('30 岁效率博主，黑色衬衫，语速快但亲和')).toBeInTheDocument()
    expect(screen.getByText('想提升工作效率的职场新人')).toBeInTheDocument()
    expect(screen.getByText('用一个可复制的方法把会议记录变成行动清单')).toBeInTheDocument()
    expect(screen.getByText(/节奏参考 · https:\/\/cdn\.example\.com\/ref\.mp4/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(await screen.findByText('生成文件 (2)')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: '视频结果' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /预览 final\.mp4/ }))
    const videoDialog = await screen.findByRole('dialog')
    expect(within(videoDialog).getByRole('heading', { name: '视频结果' })).toBeInTheDocument()
    expect(within(videoDialog).getByText('创作参数')).toBeInTheDocument()
    expect(within(videoDialog).getByText('生成任务 ID')).toBeInTheDocument()
    expect(within(videoDialog).getByText('参考素材')).toBeInTheDocument()
    expect(within(videoDialog).getByText(/已消耗 7,440/)).toBeInTheDocument()
  })

  it('shows only user video input before agent resolves execution params', async () => {
    mockTask(taskWith({
      type: 'videocreator',
      status: 'pending',
      prompt: '做一条办公室个人 IP 种草视频',
      video_creator_input: {
        brief: '做一条办公室个人 IP 种草视频',
        references: [{ type: 'text', text: '不要卡通化' }],
        hard_constraints: { ratio: '9:16' },
      },
      video_creator_config: {},
      result: null,
    }))

    render(<TaskDetailPage />)

    await openTaskDetails('配置')
    expect(screen.getByRole('heading', { name: '用户输入' })).toBeInTheDocument()
    expect(screen.getAllByText(/不要卡通化/).length).toBeGreaterThan(0)
    expect(screen.getAllByText('9:16').length).toBeGreaterThan(0)
    expect(screen.queryByText('Agent 解析结果')).not.toBeInTheDocument()
    expect(screen.queryByText('视频模型')).not.toBeInTheDocument()
    expect(screen.queryByText('人物 / 主体')).not.toBeInTheDocument()
    expect(screen.queryByText('目标受众')).not.toBeInTheDocument()
    expect(screen.queryByText('核心信息')).not.toBeInTheDocument()
  })

  it('shows video production tabs, QC, delivery actions, and retake cloning', async () => {
    mockTask(taskWith({
      id: 'task-1',
      type: 'videocreator',
      status: 'completed',
      prompt: '生成一条直播带货视频',
      video_creator_config: {
        scenario_key: 'live_selling',
        production_mode: 'guided',
        creative_type: 'product_demo',
        purpose: 'ecommerce',
        model_key: 'seedance-2.0-mini',
        resolution: '720p',
        ratio: '9:16',
        duration: 16,
      },
      result: null,
    }))
    vi.mocked(api.tasks.videoProduction).mockResolvedValue({
      task_id: 'task-1',
      scenario_key: 'live_selling',
      production_mode: 'guided',
      artifacts: {
        'creative-brief.md': {
          status: 'available',
          file_name: 'creative-brief.md',
          content: '# Brief\n产品可信感',
        },
        'quality-review.md': {
          status: 'available',
          file_name: 'quality-review.md',
          content: '主体一致性：通过\n产品保真：通过\nCTA：需要补强',
        },
        'delivery-manifest.json': {
          status: 'available',
          file_name: 'delivery-manifest.json',
          parsed_json: { final_video_task_file: 'file-final' },
        },
      },
      retake_actions: ['keep', 'fix_in_post', 'edit', 're_roll', 'rewrite'],
      next_actions: ['continue_editing', 'generate_cover', 'export_capcut_draft'],
    })

    render(<TaskDetailPage />)

    expect(await screen.findByText('制作状态')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Brief' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'QC' })).toBeInTheDocument()
    expect(screen.getByText('产品可信感')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('tab', { name: 'QC' }))
    expect(screen.getByText(/主体一致性：通过/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '继续剪辑' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '生成封面' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '导出剪映草稿' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Re-roll' }))
    await waitFor(() => {
      expect(api.tasks.clone).toHaveBeenCalledWith('task-1')
      expect(mockNavigate).toHaveBeenCalledWith('/tasks/task-clone')
    })
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
