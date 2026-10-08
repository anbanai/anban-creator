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

  image_ratio: '1:1',
  max_concurrent_tasks: 1,
  status: 'active',
  created_at: '2026-07-10T00:00:00.000Z',
  updated_at: '2026-07-10T00:00:00.000Z',
}

const snapshotTask: Task = {
  id: 'task-1',
  type: 'wechat-article',
  prompt: '写一篇茶饮文章',
  status: 'completed',
  project_id: 'project-1',
  execution_profile: 'effective',
  billing_price_credits: 5000,
  billing_total_credits: 6800,
  plan_id: null,
  project_snapshot: {
    project_name: '茶小茶',
    platform: 'wechat',
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
  state: 'retained',
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

    const facts = screen.getByRole('region', { name: '关键事实' })
    expect(screen.getByRole('heading', { name: '关键事实', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('手动创建')).toBeInTheDocument()
    expect(screen.getByText('清新茶感摄影')).toBeInTheDocument()
    expect(screen.getByText('画幅 3:4')).toBeInTheDocument()
    expect(screen.getByText('累计扣费 6,800 积分')).toBeInTheDocument()
    expect(facts).not.toHaveTextContent('茶小茶')
    expect(facts).not.toHaveTextContent('公众号文章')
    expect(facts).not.toHaveTextContent('后来修改的项目')
    expect(facts).not.toHaveTextContent('后来修改的视觉')
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

  it('uses grouped mobile and desktop grid geometry for key facts', () => {
    renderSummary()

    const grid = screen.getByTestId('task-context-grid')
    const overview = screen.getByRole('button', { name: '打开任务概览' })
    const configuration = screen.getByRole('button', { name: '打开创作配置' })
    const materials = screen.getByRole('button', { name: '打开参考素材' })

    expect(grid).toHaveClass('grid-cols-2', 'gap-0', 'lg:grid-cols-3')
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
    expect(materials).toHaveClass('lg:border-r-0')
  })

  it('renders context actions as a vertical sidebar with row separators', () => {
    renderSummary({ layout: 'sidebar' })

    const grid = screen.getByTestId('task-context-grid')
    const actions = [
      screen.getByRole('button', { name: '打开任务概览' }),
      screen.getByRole('button', { name: '打开创作配置' }),
      screen.getByRole('button', { name: '打开参考素材' }),
    ]

    expect(screen.getByRole('heading', { name: '关键事实', level: 2 })).toBeInTheDocument()
    expect(grid).toHaveClass('grid-cols-1')
    actions.slice(0, 2).forEach((action) => {
      expect(action).toHaveClass('border-b', 'border-b-border')
      expect(action).not.toHaveClass('border-r')
    })
    expect(actions[2]).not.toHaveClass('border-b', 'border-r')
  })

  it('exposes visible facts as accessible descriptions without repeating task identity', () => {
    renderSummary()

    const actions = [
      screen.getByRole('button', { name: '打开任务概览' }),
      screen.getByRole('button', { name: '打开创作配置' }),
      screen.getByRole('button', { name: '打开参考素材' }),
    ]

    expect(actions[0]).toHaveAccessibleDescription(/手动创建.*创建方式.*累计扣费 6,800 积分/)
    expect(actions[1]).toHaveAccessibleDescription(/清新茶感摄影.*画幅 3:4/)
    expect(actions[2]).toHaveAccessibleDescription(/0 项输入.*仅任务输入/)

    const descriptionIds = actions.map((action) => action.getAttribute('aria-describedby'))
    expect(descriptionIds.every(Boolean)).toBe(true)
    expect(new Set(descriptionIds).size).toBe(actions.length)
    for (const descriptionId of descriptionIds) {
      expect(document.getElementById(descriptionId as string)).toBeInTheDocument()
    }
  })

  it('opens the matching key fact details tabs', () => {
    const onOpenTab = vi.fn()
    renderSummary({ onOpenTab })

    fireEvent.click(screen.getByRole('button', { name: '打开任务概览' }))
    fireEvent.click(screen.getByRole('button', { name: '打开创作配置' }))
    fireEvent.click(screen.getByRole('button', { name: '打开参考素材' }))

    expect(onOpenTab.mock.calls.map(([tab]) => tab)).toEqual([
      'overview',
      'configuration',
      'materials',
    ])
  })

  it('uses legacy and current-project fallbacks without inventing configuration defaults', () => {
    renderSummary({
      task: {
        ...snapshotTask,
        type: 'wechat-article',
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
    })

    expect(screen.queryByText('当前项目')).not.toBeInTheDocument()
    expect(screen.queryByText('公众号文章')).not.toBeInTheDocument()
    expect(screen.getByText('未设置风格')).toBeInTheDocument()
    expect(screen.getByText('未指定画幅')).toBeInTheDocument()
    expect(screen.getByText('0 项输入')).toBeInTheDocument()
    expect(screen.getByText('仅任务输入')).toBeInTheDocument()
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
