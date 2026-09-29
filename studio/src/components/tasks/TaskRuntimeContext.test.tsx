import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import type { Task, TaskFile } from '@/types'
import { TaskRuntimeContext } from './TaskRuntimeContext'

const task = {
  id: 'task-runtime',
  type: 'article',
  prompt: '写一篇春日生活文章',
  status: 'failed',
  project_id: 'project-1',
  execution_profile: 'balanced',
  billing_price_credits: 1500,
  created_at: '2026-09-29T08:00:00.000Z',
  started_at: '2026-09-29T08:00:01.000Z',
  completed_at: '2026-09-29T08:02:00.000Z',
  project_snapshot: { project_name: '春日生活号', platform: 'article' },
  runtime_context: {
    profile: {
      status: 'conflict',
      label: '春日生活号',
      snapshot_id: 'snapshot-safe-id',
      summary: '轻松生活方式 · 任务已冻结',
      changed_since_snapshot: true,
    },
    execution: {
      status: 'recoverable',
      execution_id: 'execution-safe-id',
      resumable: true,
      recovery_stage: 'cover',
      workspace: 'bound',
      runtime_profile: 'balanced',
      runtime_image_digest: 'sha256:abcdef',
    },
    artifacts: {
      completed: 7,
      required: 8,
      failed: 0,
      missing: 1,
      items: [{ id: 'required:cover', label: '封面图', status: 'missing' }],
    },
    connectivity: {
      status: 'healthy',
      summary: '模型和 MCP 正常',
      checked_at: '2026-09-29T08:01:00.000Z',
    },
  },
} as Task

const files: TaskFile[] = []

describe('TaskRuntimeContext', () => {
  it('renders compact aggregated summaries and frozen snapshot notice', () => {
    render(<TaskRuntimeContext task={task} files={files} onResume={vi.fn()} />)

    expect(screen.getByRole('heading', { name: '运行上下文', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('春日生活号')).toBeInTheDocument()
    expect(screen.getByText('7 / 8 个')).toBeInTheDocument()
    expect(screen.getAllByText('点击查看详情')).toHaveLength(2)
    expect(screen.getByText('模型和 MCP 正常')).toBeInTheDocument()
    expect(screen.getByText('项目已更新，任务仍使用冻结版本')).toBeInTheDocument()
  })

  it('opens a detail sheet with safe technical identifiers only', () => {
    render(<TaskRuntimeContext task={task} files={files} onResume={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: '执行环境详情' }))

    expect(screen.getByRole('heading', { name: '执行环境' })).toBeInTheDocument()
    expect(screen.getByText('execution-safe-id')).toBeInTheDocument()
    expect(screen.getByText('balanced')).toBeInTheDocument()
    expect(screen.getByText('sha256:abcdef')).toBeInTheDocument()
    expect(screen.queryByText('/workspace/project')).not.toBeInTheDocument()
    expect(screen.queryByText('API_KEY')).not.toBeInTheDocument()
  })
})
