import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import TaskDetailPage from './TaskDetailPage'
import { render } from '@/test/test-utils'
import { mockTasks } from '@/test/mocks/handlers'
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
  const { mockChannelDetail } = await vi.importActual<typeof import('@/test/mocks/handlers')>('@/test/mocks/handlers')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        get: vi.fn(),
        files: vi.fn().mockResolvedValue([]),
      },
      channels: {
        ...actual.api.channels,
        get: vi.fn().mockResolvedValue(mockChannelDetail),
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
