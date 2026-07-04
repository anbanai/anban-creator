import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TaskDetailPage from './TaskDetailPage'
import { render } from '@/test/test-utils'
import { mockProjectDetail, mockTasks } from '@/test/mocks/handlers'
import type { Task } from '@/types'
import { api } from '@/lib/api'

const mockNavigate = vi.fn()

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
        retry: vi.fn(),
        resume: vi.fn(),
        files: vi.fn().mockResolvedValue([]),
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

describe('TaskDetailPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.files).mockResolvedValue([])
    vi.mocked(api.tasks.retry).mockResolvedValue(taskWith({ id: 'task-rerun', status: 'pending' }))
    vi.mocked(api.tasks.resume).mockResolvedValue(taskWith({ id: 'task-1', status: 'pending' }))
    vi.mocked(api.projects.get).mockResolvedValue(mockProjectDetail)
    vi.mocked(api.seednoteAnalytics.getByTask).mockResolvedValue({ series: [] })
  })

  it('shows compact running progress and hides workflow stage grid', async () => {
    mockTask(taskWith({
      status: 'running',
      progress: 42,
      progress_log: '准备素材\nUsing tool: Read',
      latest_progress: { stage: 'writing', title: '正在写作正文', percent: 42 },
      result: { files: null, output: '' },
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByText('42%')).toBeInTheDocument()
    expect(screen.getByText('正在写作正文')).toBeInTheDocument()
    // progress_log noise like "Using tool: ..." must NOT leak into the card.
    expect(screen.queryByText('Using tool: Read')).not.toBeInTheDocument()
    expect(screen.queryByText('创作进度')).not.toBeInTheDocument()
    expect(screen.queryByText('当前阶段')).not.toBeInTheDocument()
  })

  it('hides the progress card after completion', async () => {
    mockTask(taskWith({
      status: 'completed',
      progress: 100,
      progress_log: '[100%] 任务完成',
      result: { files: null, output: '' },
    }))

    render(<TaskDetailPage />)

    await waitFor(() => expect(screen.getByText('已完成')).toBeInTheDocument())
    expect(screen.queryByText('进度')).not.toBeInTheDocument()
    expect(screen.queryByText('任务执行成功')).not.toBeInTheDocument()
  })

  it('lets completed tasks be copied as a fresh task', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'completed',
      progress: 100,
      result: { files: null, output: '' },
    }))

    render(<TaskDetailPage />)

    const rerunButton = await screen.findByRole('button', { name: /复制重跑/ })
    fireEvent.click(rerunButton)

    await waitFor(() => {
      expect(api.tasks.retry).toHaveBeenCalledWith('task-1')
      expect(mockNavigate).toHaveBeenCalledWith('/tasks/task-rerun')
    })
  })

  it('opens a continue dialog and submits prompt files and labels for the current task', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
      result: { files: null, output: '' },
    }))

    render(<TaskDetailPage />)

    const continueButtons = await screen.findAllByRole('button', { name: /继续执行/ })
    fireEvent.click(continueButtons[0])

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
      expect(mockNavigate).not.toHaveBeenCalledWith('/tasks/task-rerun')
    })
  })

  it('keeps resume file labels aligned when a file is removed', async () => {
    mockTask(taskWith({
      id: 'task-1',
      status: 'failed',
      result: { files: null, output: '' },
    }))

    render(<TaskDetailPage />)

    const continueButtons = await screen.findAllByRole('button', { name: /继续执行/ })
    fireEvent.click(continueButtons[0])

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

  it('does not show rerun for running tasks', async () => {
    mockTask(taskWith({
      status: 'running',
      progress: 42,
      latest_progress: { stage: 'writing', title: '正在写作正文', percent: 42 },
      result: { files: null, output: '' },
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByText('正在写作正文')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /继续执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /复制重跑/ })).not.toBeInTheDocument()
  })

  it('does not show rerun for pending tasks', async () => {
    mockTask(taskWith({
      status: 'pending',
      progress: 0,
      latest_progress: undefined,
      result: { files: null, output: '' },
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    await waitFor(() => expect(screen.getByText('任务等待执行中...')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: /继续执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /复制重跑/ })).not.toBeInTheDocument()
  })

  it('uses distinct continue and copy-rerun wording for cancelled tasks', async () => {
    mockTask(taskWith({
      status: 'cancelled',
      result: { files: null, output: '' },
    }))

    render(<TaskDetailPage />)

    expect(await screen.findAllByRole('button', { name: /继续执行/ })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: /复制重跑/ })).toHaveLength(2)
    expect(screen.queryByRole('button', { name: /重新执行/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /再试一次/ })).not.toBeInTheDocument()
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
      result: { files: null, output: '' },
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

    expect(await screen.findByText('项目参数')).toBeInTheDocument()
    expect(screen.queryByText('项目快照')).not.toBeInTheDocument()
    expect(screen.getByText('快照项目')).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: '参考图' })).not.toBeInTheDocument()
    expect(screen.queryByText('参考图')).not.toBeInTheDocument()
    expect(screen.getByText('视觉风格')).toBeInTheDocument()
    expect(screen.getByText('柔光生活摄影')).toBeInTheDocument()
    const parameterCard = screen.getByText('项目参数').closest('[data-slot="card"]') as HTMLElement
    const parameterContent = within(parameterCard).getByText('视觉风格').closest('[data-slot="card-content"]')
    const visualStyleBlock = within(parameterCard).getByText('视觉风格').parentElement
    const imageRatioBlock = within(parameterCard).getByText('图片比例').parentElement
    const imageModelBlock = within(parameterCard).getByText('图片模型').parentElement
    expect(visualStyleBlock?.parentElement).toBe(parameterContent)
    expect(imageRatioBlock?.parentElement?.parentElement).toBe(parameterContent)
    expect(imageModelBlock?.parentElement?.parentElement).toBe(parameterContent)
    expect(screen.getByText('安般')).toBeInTheDocument()
    expect(screen.getByText('dan-koe')).toBeInTheDocument()
    expect(screen.getByText('autumn-warm')).toBeInTheDocument()
    expect(screen.getByText('消耗积分')).toBeInTheDocument()
    expect(screen.getAllByText('任务消耗').length).toBeGreaterThan(0)
    expect(screen.getByText('操作消耗')).toBeInTheDocument()
    expect(screen.getByText('退还积分')).toBeInTheDocument()
    expect(screen.getByText('净消耗')).toBeInTheDocument()
    expect(screen.getAllByText('188').length).toBeGreaterThan(0)
    expect(screen.getByText('图片生成')).toBeInTheDocument()
    expect(screen.getByText('-80')).toBeInTheDocument()
    expect(screen.queryByText('执行成本')).not.toBeInTheDocument()
    expect(screen.queryByText('$1.23')).not.toBeInTheDocument()
  })

  it('opens project details in a dialog instead of navigating to the projects list', async () => {
    mockTask(taskWith({
      type: 'article',
      status: 'completed',
      result: { files: null, output: '' },
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
      result: { files: null, output: '' },
      completed_at: '',
    }))

    render(<TaskDetailPage />)

    expect(await screen.findByRole('heading', { name: '阶段日志' })).toBeInTheDocument()
    expect(screen.getByText('已完成选题')).toBeInTheDocument()

    screen.getByRole('button', { name: /复制/ }).click()
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith('## 阶段日志\n- 已完成选题\n```txt\nraw block\n```')
    })
  })
})
