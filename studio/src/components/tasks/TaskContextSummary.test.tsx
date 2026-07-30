import { fireEvent, screen } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import type { Project, Task, TaskFile } from '@/types'
import { TaskContextSummary } from './TaskContextSummary'

const currentProject: Project = {
  id: 'project-1',
  user_id: 'user-1',
  platform: 'seednote',
  name: '后来修改的项目',
  avatar_url: '',
  profile_url: '',
  keywords: '',
  visual_style: '后来修改的视觉',
  writer: '',
  theme: '',
  author: '',
  template_id: '',
  image_ratio: '1:1',
  max_concurrent_tasks: 1,
  config: {},
  status: 'active',
  created_at: '2026-07-10T00:00:00.000Z',
  updated_at: '2026-07-10T00:00:00.000Z',
}

const snapshotTask: Task = {
  id: 'task-1',
  type: 'seednote',
  prompt: '写一篇茶饮文章',
  status: 'completed',
  project_id: 'project-1',
  execution_profile: 'effective',
  result: null,
  published: false,
  published_at: null,
  billing_price_credits: 5000,
  billing_total_credits: 6800,
  plan_id: null,
  project_snapshot: {
    project_name: '茶小茶',
    platform: 'article',
    visual_style: '清新茶感摄影',
    image_ratio: '3:4',
  },
  created_at: '2026-07-10T00:00:00.000Z',
  started_at: '2026-07-10T00:00:01.000Z',
  completed_at: '2026-07-10T00:01:00.000Z',
}

const summaryFile: TaskFile = {
  id: 'file-summary',
  task_id: 'task-1',
  role: 'artifact',
  file_name: 'reference-usage-summary.json',
  mime_type: 'application/json',
  file_size: 1024,
  url: '/api/v1/files/file-summary',
  created_at: '2026-07-10T00:00:10.000Z',
}

function renderSummary(overrides: Partial<ComponentProps<typeof TaskContextSummary>> = {}) {
  const props: ComponentProps<typeof TaskContextSummary> = {
    task: snapshotTask,
    project: currentProject,
    files: [],
    logs: [],
    progressDescription: null,
    sseError: null,
    onOpenTab: vi.fn(),
    ...overrides,
  }

  return { ...render(<TaskContextSummary {...props} />), props }
}

afterEach(() => {
  vi.restoreAllMocks()
})

describe('TaskContextSummary', () => {
  it('renders authoritative snapshot context instead of changed project values', () => {
    renderSummary()

    expect(screen.getByRole('region', { name: '任务上下文' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '任务上下文', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('茶小茶')).toBeInTheDocument()
    expect(screen.getByText('公众号文章')).toBeInTheDocument()
    expect(screen.getByText('清新茶感摄影 · 3:4')).toBeInTheDocument()
    expect(screen.getByText('累计扣费 6,800 积分')).toBeInTheDocument()
    expect(screen.queryByText('后来修改的项目')).not.toBeInTheDocument()
    expect(screen.queryByText('后来修改的视觉 · 1:1')).not.toBeInTheDocument()
  })

  it('summarizes task inputs and reference usage conclusion from file metadata', () => {
    renderSummary({
      task: {
        ...snapshotTask,
        input_attachments: [{ type: 'image', file_name: 'tea-reference.png' }],
      },
      files: [summaryFile],
    })

    expect(screen.getByText('1 项输入')).toBeInTheDocument()
    expect(screen.getByText('已生成使用结论')).toBeInTheDocument()
  })

  it('prioritizes the current progress description for a running task', () => {
    renderSummary({
      task: { ...snapshotTask, status: 'running' },
      logs: ['## 已完成大纲', '- 正在生成配图'],
      progressDescription: '正在润色正文',
    })

    expect(screen.getByText('2 条 · 实时')).toBeInTheDocument()
    expect(screen.getByText('正在润色正文')).toBeInTheDocument()
    expect(screen.queryByText('正在生成配图')).not.toBeInTheDocument()
  })

  it('uses grouped mobile and desktop grid geometry for all summary actions', () => {
    renderSummary()

    const grid = screen.getByTestId('task-context-grid')
    const overview = screen.getByRole('button', { name: '打开任务概览' })
    const configuration = screen.getByRole('button', { name: '打开创作配置' })
    const materials = screen.getByRole('button', { name: '打开参考素材' })
    const logs = screen.getByRole('button', { name: '打开执行日志' })

    expect(grid).toHaveClass('grid-cols-2', 'gap-0', 'lg:grid-cols-4')
    expect(grid.parentElement).toHaveAttribute('data-slot', 'card-content')
    expect(grid.parentElement).toHaveClass('p-0')

    expect(overview).toHaveClass(
      'rounded-none',
      'border-r',
      'border-b',
      'border-r-border',
      'border-b-border',
      'lg:border-b-0',
    )
    expect(configuration).toHaveClass(
      'rounded-none',
      'border-b',
      'border-b-border',
      'lg:border-r',
      'lg:border-r-border',
      'lg:border-b-0',
    )
    expect(configuration).not.toHaveClass('border-r')
    expect(materials).toHaveClass('rounded-none', 'border-r', 'border-r-border')
    expect(materials).not.toHaveClass('border-b')
    expect(materials).not.toHaveClass('lg:border-b-0')
    expect(logs).toHaveClass('rounded-none')
    expect(logs).not.toHaveClass('border-r')
    expect(logs).not.toHaveClass('border-b')
    expect(logs).not.toHaveClass('lg:border-r')
    expect(logs).not.toHaveClass('lg:border-b-0')
  })

  it('exposes visible overview and log summaries as accessible descriptions', () => {
    renderSummary({
      task: { ...snapshotTask, status: 'running' },
      logs: ['## 已完成大纲', '- 最新日志状态'],
    })

    const actions = [
      screen.getByRole('button', { name: '打开任务概览' }),
      screen.getByRole('button', { name: '打开创作配置' }),
      screen.getByRole('button', { name: '打开参考素材' }),
      screen.getByRole('button', { name: '打开执行日志' }),
    ]

    expect(actions[0]).toHaveAccessibleDescription(/茶小茶.*手动创建.*累计扣费 6,800 积分/)
    expect(actions[3]).toHaveAccessibleDescription(/2 条 · 实时.*最新日志状态/)

    const descriptionIds = actions.map((action) => action.getAttribute('aria-describedby'))
    expect(descriptionIds.every(Boolean)).toBe(true)
    expect(new Set(descriptionIds).size).toBe(actions.length)
    for (const descriptionId of descriptionIds) {
      expect(document.getElementById(descriptionId as string)).toBeInTheDocument()
    }
  })

  it('opens the matching details tab from every summary action', () => {
    const onOpenTab = vi.fn()
    renderSummary({ onOpenTab })

    fireEvent.click(screen.getByRole('button', { name: '打开任务概览' }))
    fireEvent.click(screen.getByRole('button', { name: '打开创作配置' }))
    fireEvent.click(screen.getByRole('button', { name: '打开参考素材' }))
    fireEvent.click(screen.getByRole('button', { name: '打开执行日志' }))
    fireEvent.click(screen.getByRole('button', { name: '更多详情' }))

    expect(onOpenTab.mock.calls.map(([tab]) => tab)).toEqual([
      'overview',
      'configuration',
      'materials',
      'logs',
      'overview',
    ])
  })

  it('uses legacy and current-project fallbacks without inventing configuration defaults', () => {
    renderSummary({
      task: {
        ...snapshotTask,
        type: 'article',
        project_snapshot: {},
        input_attachments: [],
      },
      project: {
        ...currentProject,
        name: '当前项目',
        visual_style: '',
        image_ratio: '',
      },
      files: [],
      logs: [],
    })

    expect(screen.getByText('当前项目')).toBeInTheDocument()
    expect(screen.getByText('种草笔记')).toBeInTheDocument()
    expect(screen.getByText('未设置')).toBeInTheDocument()
    expect(screen.getByText('0 项输入')).toBeInTheDocument()
    expect(screen.getByText('仅任务输入')).toBeInTheDocument()
    expect(screen.getByText('0 条 · 已结束')).toBeInTheDocument()
    expect(screen.getByText('暂无日志')).toBeInTheDocument()
  })

  it('shows a running connection error with the latest normalized non-empty log line', () => {
    renderSummary({
      task: { ...snapshotTask, status: 'running' },
      logs: ['## 第一条日志', '   ', '  - 最新日志状态  '],
      sseError: '网络连接已中断',
    })

    expect(screen.getByText('3 条 · 连接中断')).toBeInTheDocument()
    expect(screen.getByText('最新日志状态')).toBeInTheDocument()
  })

  it('limits normalized log fallback text to 80 characters', () => {
    const longLog = `## ${'长'.repeat(90)}`
    renderSummary({ logs: [longLog] })

    expect(screen.getByText('长'.repeat(80))).toBeInTheDocument()
    expect(screen.queryByText('长'.repeat(81))).not.toBeInTheDocument()
  })

  it('shows that a pending task is waiting to execute', () => {
    renderSummary({ task: { ...snapshotTask, status: 'pending' } })

    expect(screen.getByText('0 条 · 等待执行')).toBeInTheDocument()
  })

  it.each([
    ['pending', '等待执行'],
    ['completed', '已结束'],
    ['failed', '已结束'],
    ['cancelled', '已结束'],
  ] as const)('keeps %s lifecycle truth when an SSE error is stale', (status, expectedState) => {
    renderSummary({
      task: { ...snapshotTask, status },
      sseError: '已失效的连接错误',
    })

    expect(screen.getByText(`0 条 · ${expectedState}`)).toBeInTheDocument()
    expect(screen.queryByText('0 条 · 连接中断')).not.toBeInTheDocument()
  })

  it('does not request file lists, content, previews, or downloads', () => {
    const filesSpy = vi.spyOn(api.tasks, 'files')
    const contentSpy = vi.spyOn(api.tasks, 'downloadFileBlob')
    const previewSpy = vi.spyOn(api.tasks, 'fetchPreviewHTML')
    const zipSpy = vi.spyOn(api.tasks, 'downloadZipBlob')

    renderSummary({ files: [summaryFile] })

    expect(filesSpy).not.toHaveBeenCalled()
    expect(contentSpy).not.toHaveBeenCalled()
    expect(previewSpy).not.toHaveBeenCalled()
    expect(zipSpy).not.toHaveBeenCalled()
  })
})
