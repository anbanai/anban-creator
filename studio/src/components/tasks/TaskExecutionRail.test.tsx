import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { shouldApplyLifecycleRevision } from '@/lib/task-lifecycle'
import { render } from '@/test/test-utils'
import type { TaskLifecycle } from '@/types'
import { TaskExecutionRail } from './TaskExecutionRail'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        getWechatPublication: vi.fn(),
        reconcileWechat: vi.fn(),
        publishWechat: vi.fn(),
        retryWechatPublish: vi.fn(),
        selectWechatArticle: vi.fn(),
        recoverWechatPublication: vi.fn(),
      },
    },
  }
})

const lifecycle: TaskLifecycle = {
  version: 1,
  revision: 7,
  execution_id: 'execution-1',
  updated_at: '2026-09-17T08:00:00Z',
  stages: [
    {
      id: 'research', title: '研究素材', goal: '确认事实与角度', source: 'agent', kind: 'work',
      state: 'complete', latest_update: '事实已经核验', completed_at: '2026-09-17T07:50:00Z',
    },
    {
      id: 'writing', title: '撰写内容', goal: '形成完整初稿', source: 'agent', kind: 'work',
      state: 'active', latest_update: '正在收束文章结构', started_at: '2026-09-17T07:51:00Z',
    },
    { id: 'review', title: '质量复核', source: 'agent', kind: 'work', state: 'pending' },
  ],
}

describe('TaskExecutionRail', () => {
  it('shows artifacts inside their stage and preserves an explicit collapse across live updates', () => {
    const files = [{ id: 'file', task_id: 'task-1', state: 'delivered' as const, role: 'topic', file_name: 'research.md', mime_type: 'text/markdown', file_size: 100, url: '', created_at: '' }]
    const props = { taskId: 'task-1', status: 'running' as const, onOpenLogs: vi.fn(), files }
    const { rerender } = render(<TaskExecutionRail {...props} lifecycle={lifecycle} />)
    const research = screen.getByRole('button', { name: '研究素材，已完成' })
    expect(within(research.closest('li')!).getByRole('button', { name: '预览 research.md' })).toBeVisible()
    fireEvent.click(research)
    expect(screen.queryByRole('button', { name: '预览 research.md' })).not.toBeInTheDocument()
    rerender(<TaskExecutionRail {...props} lifecycle={{ ...lifecycle, revision: 8 }} />)
    expect(research).toHaveAttribute('aria-expanded', 'false')
  })

  it('shows a failed stage error once when it repeats the task error', () => {
    render(<TaskExecutionRail taskId="task-1" status="failed" errorMessage="供应商请求失败。" lifecycle={{ ...lifecycle, stages: [{ ...lifecycle.stages[1], state: 'failed', latest_update: '供应商请求失败。' }] }} onOpenLogs={vi.fn()} />)
    expect(screen.getAllByText('供应商请求失败。')).toHaveLength(1)
  })

  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(api.tasks.getWechatPublication).mockRejectedValue({ response: { status: 404 } })
    vi.mocked(api.tasks.publishWechat).mockResolvedValue({
      id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'publishing',
    })
    vi.mocked(api.tasks.reconcileWechat).mockResolvedValue({ reconciled: true })
    vi.mocked(api.tasks.retryWechatPublish).mockResolvedValue({
      id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'publishing',
    })
    vi.mocked(api.tasks.recoverWechatPublication).mockResolvedValue({ status: 'blocked', attempted: false })
  })

  it('shows a single planning state before the agent declares stages', () => {
    render(<TaskExecutionRail taskId="task-1" status="running" onOpenLogs={vi.fn()} />)

    expect(screen.getByText('正在制定执行计划')).toBeInTheDocument()
    expect(screen.queryByText('%')).not.toBeInTheDocument()
  })

  it('expands the active stage, folds completed work, and marks the current step', () => {
    render(<TaskExecutionRail taskId="task-1" status="running" lifecycle={lifecycle} onOpenLogs={vi.fn()} />)

    expect(screen.getByText('正在收束文章结构')).toBeVisible()
    expect(screen.getByText('形成完整初稿')).not.toBeVisible()
    fireEvent.click(screen.getByText('环节说明'))
    expect(screen.getByText('形成完整初稿')).toBeVisible()
    expect(screen.queryByText('事实已经核验')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '撰写内容，进行中' })).toHaveAttribute('aria-current', 'step')

    fireEvent.click(screen.getByRole('button', { name: '研究素材，已完成' }))
    expect(screen.getByText('事实已经核验')).toBeVisible()
  })

  it('folds a previously active stage after it completes', () => {
    const { rerender } = render(
      <TaskExecutionRail taskId="task-1" status="running" lifecycle={lifecycle} onOpenLogs={vi.fn()} />,
    )

    expect(screen.getByText('正在收束文章结构')).toBeVisible()

    rerender(
      <TaskExecutionRail
        taskId="task-1"
        status="running"
        lifecycle={{
          ...lifecycle,
          revision: 8,
          stages: lifecycle.stages.map((stage) => {
            if (stage.id === 'writing') {
              return { ...stage, state: 'complete' as const, latest_update: '文章初稿已完成' }
            }
            if (stage.id === 'review') {
              return { ...stage, state: 'active' as const, latest_update: '正在检查内容质量' }
            }
            return stage
          }),
        }}
        onOpenLogs={vi.fn()}
      />,
    )

    expect(screen.queryByText('文章初稿已完成')).not.toBeInTheDocument()
    expect(screen.getByText('正在检查内容质量')).toBeVisible()
  })

  it('marks and expands the cancelled execution point before the skipped tail', () => {
    render(
      <TaskExecutionRail
        taskId="task-1"
        status="cancelled"
        lifecycle={{
          ...lifecycle,
          revision: 8,
          stages: [
            { ...lifecycle.stages[0], state: 'complete' },
            { ...lifecycle.stages[1], state: 'cancelled', latest_update: '用户停止了本次执行' },
            { ...lifecycle.stages[2], state: 'skipped' },
          ],
        }}
        onOpenLogs={vi.fn()}
        onResume={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: '撰写内容，已取消' })).toHaveAttribute('aria-current', 'step')
    expect(screen.getByText('用户停止了本次执行')).toBeVisible()
    expect(screen.getByRole('button', { name: '质量复核，已跳过' })).not.toHaveAttribute('aria-current')
  })

  it('keeps terminal recovery visible when every declared work stage already completed', () => {
    const onResume = vi.fn()
    render(
      <TaskExecutionRail
        taskId="task-1"
        status="failed"
        errorMessage="最终交付校验失败"
        lifecycle={{
          ...lifecycle,
          revision: 9,
          stages: [
            ...lifecycle.stages.map((stage) => ({ ...stage, state: 'complete' as const })),
            { id: 'system_draft', title: '创建公众号草稿', source: 'server', kind: 'draft', state: 'skipped' },
            { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'skipped' },
          ],
        }}
        onOpenLogs={vi.fn()}
        onResume={onResume}
      />,
    )

    expect(screen.getByRole('button', { name: '质量复核，已完成' })).toHaveAttribute('aria-current', 'step')
    expect(screen.getByText('最终交付校验失败')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '继续执行' }))
    expect(onResume).toHaveBeenCalledOnce()
  })

  it('puts manual formal publishing beside the publication stage with confirmation', async () => {
    vi.mocked(api.tasks.getWechatPublication).mockResolvedValue({
      id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'drafted',
      draft_media_id: 'draft-1',
    })
    const articleLifecycle: TaskLifecycle = {
      ...lifecycle,
      stages: [
        ...lifecycle.stages.map((stage) => ({ ...stage, state: 'complete' as const })),
        { id: 'system_draft', title: '创建公众号草稿', source: 'server', kind: 'draft', state: 'complete' },
        { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'pending' },
      ],
    }

    render(<TaskExecutionRail taskId="task-1" status="completed" lifecycle={articleLifecycle} onOpenLogs={vi.fn()} />)

    fireEvent.click(await screen.findByRole('button', { name: '正式发布' }))
    expect(screen.getByRole('heading', { name: '确认正式发布' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认发布' }))
    await waitFor(() => expect(api.tasks.publishWechat).toHaveBeenCalledWith('task-1'))
  })

  it('never offers a blind retry when formal submission evidence exists', async () => {
    vi.mocked(api.tasks.getWechatPublication).mockResolvedValue({
      id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'unsupported',
      draft_media_id: 'draft-1', submit_attempted_at: '2026-09-17T08:00:00Z', wechat_status_code: 48001,
    })
    const blockedLifecycle: TaskLifecycle = {
      ...lifecycle,
      stages: [
        ...lifecycle.stages.map((stage) => ({ ...stage, state: 'complete' as const })),
        { id: 'system_draft', title: '创建公众号草稿', source: 'server', kind: 'draft', state: 'complete' },
        { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'blocked', latest_update: '只能检测结果' },
      ],
    }

    render(<TaskExecutionRail taskId="task-1" status="completed" lifecycle={blockedLifecycle} onOpenLogs={vi.fn()} />)

    fireEvent.click(await screen.findByRole('button', { name: '检测状态' }))
    await waitFor(() => expect(api.tasks.reconcileWechat).toHaveBeenCalledWith('task-1'))
    expect(api.tasks.retryWechatPublish).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '重试发布' })).not.toBeInTheDocument()
  })

  it('refreshes publication details when a server-owned stage changes', async () => {
    const pendingLifecycle: TaskLifecycle = {
      ...lifecycle,
      stages: [
        ...lifecycle.stages,
        { id: 'system_draft', title: '创建公众号草稿', source: 'server', kind: 'draft', state: 'pending' },
        { id: 'system_publication', title: '正式发布', source: 'server', kind: 'publication', state: 'pending' },
      ],
    }
    const { rerender } = render(
      <TaskExecutionRail taskId="task-1" status="completed" lifecycle={pendingLifecycle} onOpenLogs={vi.fn()} />,
    )

    await waitFor(() => expect(api.tasks.getWechatPublication).toHaveBeenCalledTimes(1))
    vi.mocked(api.tasks.getWechatPublication).mockResolvedValue({
      id: 'publication-1', task_id: 'task-1', project_id: 'project-1', source: 'anban_api', status: 'drafted',
      draft_media_id: 'draft-1',
    })

    rerender(
      <TaskExecutionRail
        taskId="task-1"
        status="completed"
        lifecycle={{
          ...pendingLifecycle,
          revision: pendingLifecycle.revision + 1,
          stages: pendingLifecycle.stages.map((stage) => stage.id === 'system_draft'
            ? { ...stage, state: 'complete' as const, latest_update: '公众号草稿已创建' }
            : stage),
        }}
        onOpenLogs={vi.fn()}
      />,
    )

    await waitFor(() => expect(api.tasks.getWechatPublication).toHaveBeenCalledTimes(2))
    expect(await screen.findByRole('button', { name: '正式发布' })).toBeInTheDocument()
  })

  it('keeps a failed task actionable when execution stops before a plan is declared', () => {
    const onResume = vi.fn()

    render(
      <TaskExecutionRail
        taskId="task-1"
        status="failed"
        errorMessage="执行环境未建立，暂时无法生成或结算图片"
        onOpenLogs={vi.fn()}
        onResume={onResume}
      />,
    )

    expect(screen.getByText('执行环境未建立')).toBeInTheDocument()
    expect(screen.getByText('执行环境未建立，暂时无法生成或结算图片。')).toBeInTheDocument()
    expect(screen.getByText('修复执行环境后可从“图片生成”阶段继续。')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '继续执行' }))
    expect(onResume).toHaveBeenCalledOnce()
  })
})

describe('shouldApplyLifecycleRevision', () => {
  it('ignores duplicate and stale SSE lifecycle snapshots', () => {
    expect(shouldApplyLifecycleRevision(7, 6)).toBe(false)
    expect(shouldApplyLifecycleRevision(7, 7)).toBe(false)
    expect(shouldApplyLifecycleRevision(7, 8)).toBe(true)
  })
})
