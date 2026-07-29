import { screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import type { ReferenceUsageSummaryData, Task, TaskFile } from '@/types'
import ReferenceUsageSummary from './ReferenceUsageSummary'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        downloadFileBlob: vi.fn(),
      },
    },
  }
})

const validSummary: ReferenceUsageSummaryData = {
  version: '1.0',
  inputs: [{
    attachment_index: 1,
    file_name: 'front.png',
    instruction: '保持 Logo',
    status: 'used',
    decision_summary: '正面图是产品身份和包装文字的主要证据',
    analysis_attempts: 1,
    warnings: ['包装侧面的批次号不清晰'],
  }],
  outputs: [{
    file_name: 'cover.png',
    references: [{ attachment_index: 1, purpose: '保持产品身份、包装和 Logo' }],
    generation_attempts: 2,
    verification: { status: 'passed', summary: '产品与文字核验通过' },
    provider: 'openai',
    model: 'gpt-image-2',
    selection_reason: 'reference_compatible_fallback',
  }],
  warnings: ['未使用侧面图，因为与正面包装版本冲突'],
  model_fallback_reason: '首选模型参考图上限不足',
}

const seednoteTask: Task = {
  id: 'task-1',
  type: 'seednote',
  title: '种草图文',
  topic: '新品体验',
  prompt: '生成一篇新品种草图文',
  status: 'completed',
  progress: 100,
  input_attachments: [],
  plan_id: null,
  project_id: 'project-1',
  execution_profile: 'cost_effective',
  result: null,
  published: false,
  published_at: null,
  billing_price_credits: 5000,
  created_at: '2026-07-10T00:00:00.000Z',
  started_at: '2026-07-10T00:00:01.000Z',
  completed_at: '2026-07-10T00:01:00.000Z',
}

const summaryTaskFile: TaskFile = {
  id: 'file-summary',
  task_id: 'task-1',
  role: 'artifact',
  file_name: 'reference-usage-summary.json',
  mime_type: 'application/json',
  file_size: 1024,
  url: '/tasks/task-1/files/file-summary',
  created_at: '2026-07-10T00:00:00.000Z',
}

describe('ReferenceUsageSummary', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders validated input decisions and per-output reference usage', async () => {
    vi.mocked(api.tasks.downloadFileBlob).mockResolvedValue(
      new Blob([JSON.stringify(validSummary)], { type: 'application/json' }),
    )

    render(<ReferenceUsageSummary task={seednoteTask} files={[summaryTaskFile]} />)

    expect(await screen.findByText('输入素材决策')).toBeInTheDocument()
    expect(screen.getByText('参考素材使用')).toBeInTheDocument()
    expect(screen.getByText('#1 · front.png')).toBeInTheDocument()
    expect(screen.getByText('已使用')).toBeInTheDocument()
    expect(screen.getByText('正面图是产品身份和包装文字的主要证据')).toBeInTheDocument()
    expect(screen.getByText('分析 1 次')).toBeInTheDocument()
    expect(screen.getByText('包装侧面的批次号不清晰')).toBeInTheDocument()

    expect(screen.getByText('输出图片使用情况')).toBeInTheDocument()
    expect(screen.getByText('cover.png')).toBeInTheDocument()
    expect(screen.getByText('#1')).toBeInTheDocument()
    expect(screen.getByText('保持产品身份、包装和 Logo')).toBeInTheDocument()
    expect(screen.getByText('生成 2 次')).toBeInTheDocument()
    expect(screen.getByText('核验通过')).toBeInTheDocument()
    expect(screen.getByText('产品与文字核验通过')).toBeInTheDocument()
    expect(screen.getByText('openai / gpt-image-2')).toBeInTheDocument()
    expect(screen.getByText('reference_compatible_fallback')).toBeInTheDocument()
    expect(screen.getByText('首选模型参考图上限不足')).toBeInTheDocument()
    expect(screen.getByText('未使用侧面图，因为与正面包装版本冲突')).toBeInTheDocument()
    expect(api.tasks.downloadFileBlob).toHaveBeenCalledWith('task-1', 'file-summary')

    for (const surface of [
      screen.getByText('#1 · front.png').closest('article'),
      screen.getByText('cover.png').closest('article'),
    ]) {
      expect(surface).toHaveClass(
        'rounded-lg',
        'border',
        'border-border/70',
        'bg-background/70',
      )
    }
  })

  it('shows the first-input snapshot when the summary artifact is missing', () => {
    render(
      <ReferenceUsageSummary
        task={{
          ...seednoteTask,
          input_attachments: [{
            type: 'image',
            file_name: 'product-front.png',
            url: 'https://cdn.test/product-front.png',
            instruction: '保留瓶身标签',
          }],
        }}
        files={[]}
      />,
    )

    expect(screen.getByText('未找到参考使用摘要，以下仅展示首次输入快照。')).toBeInTheDocument()
    expect(screen.getByText('首次输入')).toBeInTheDocument()
    expect(screen.getByText('product-front.png')).toBeInTheDocument()
    expect(screen.getByText('https://cdn.test/product-front.png')).toBeInTheDocument()
    expect(screen.getByText('保留瓶身标签')).toBeInTheDocument()
    expect(screen.getByText('输入快照不代表 AI 实际使用结论')).toBeInTheDocument()
    expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()

    const attachmentSurface = screen.getByText('product-front.png')
      .parentElement?.parentElement?.parentElement
    expect(attachmentSurface).toHaveClass(
      'rounded-lg',
      'border',
      'border-border/70',
      'bg-background/70',
    )
  })

  it('falls back to the plan snapshot when the summary artifact is malformed', async () => {
    vi.mocked(api.tasks.downloadFileBlob).mockResolvedValue(
      new Blob([JSON.stringify({ version: '1.0', inputs: 'invalid', outputs: [] })], {
        type: 'application/json',
      }),
    )

    render(
      <ReferenceUsageSummary
        task={{
          ...seednoteTask,
          plan_id: 'plan-1',
          input_attachments: [{
            type: 'image',
            file_name: 'plan-product.png',
            instruction: '优先识别新版包装',
          }],
        }}
        files={[summaryTaskFile]}
      />,
    )

    expect(await screen.findByText('参考使用摘要无法解析，以下仅展示计划快照。')).toBeInTheDocument()
    expect(screen.getByText('计划快照')).toBeInTheDocument()
    expect(screen.getByText('plan-product.png')).toBeInTheDocument()
    expect(screen.getByText('优先识别新版包装')).toBeInTheDocument()
    expect(screen.getByText('输入快照不代表 AI 实际使用结论')).toBeInTheDocument()
    await waitFor(() => {
      expect(api.tasks.downloadFileBlob).toHaveBeenCalledWith('task-1', 'file-summary')
    })
  })

  it('renders the complete valid summary as a compact single-column flow', async () => {
    vi.mocked(api.tasks.downloadFileBlob).mockResolvedValue(
      new Blob([JSON.stringify(validSummary)], { type: 'application/json' }),
    )

    const { container } = render(
      <ReferenceUsageSummary task={seednoteTask} files={[summaryTaskFile]} variant="compact" />,
    )

    expect(await screen.findByText('输入素材决策')).toBeInTheDocument()
    expect(screen.queryByText('参考素材使用')).not.toBeInTheDocument()
    expect(container.querySelector('[data-slot="card"]')).not.toBeInTheDocument()
    expect(container.querySelector('.xl\\:grid-cols-2')).not.toBeInTheDocument()
    expect(screen.getByText('#1 · front.png')).toBeInTheDocument()
    expect(screen.getByText('说明：保持 Logo')).toBeInTheDocument()
    expect(screen.getByText('已使用')).toBeInTheDocument()
    expect(screen.getByText('正面图是产品身份和包装文字的主要证据')).toBeInTheDocument()
    expect(screen.getByText('分析 1 次')).toBeInTheDocument()
    expect(screen.getByText('包装侧面的批次号不清晰')).toBeInTheDocument()
    expect(screen.getByText('输出图片使用情况')).toBeInTheDocument()
    expect(screen.getByText('cover.png')).toBeInTheDocument()
    expect(screen.getByText('保持产品身份、包装和 Logo')).toBeInTheDocument()
    expect(screen.getByText('生成 2 次')).toBeInTheDocument()
    expect(screen.getByText('核验通过')).toBeInTheDocument()
    expect(screen.getByText('产品与文字核验通过')).toBeInTheDocument()
    expect(screen.getByText('openai / gpt-image-2')).toBeInTheDocument()
    expect(screen.getByText('reference_compatible_fallback')).toBeInTheDocument()
    expect(screen.getByText('首选模型参考图上限不足')).toBeInTheDocument()
    expect(screen.getByText('未使用侧面图，因为与正面包装版本冲突')).toBeInTheDocument()
    expect(screen.getAllByText('1 张')).toHaveLength(2)

    for (const surface of [
      screen.getByText('#1 · front.png').closest('article'),
      screen.getByText('cover.png').closest('article'),
    ]) {
      expect(surface).toHaveClass('min-w-0', 'p-3')
      expect(surface).not.toHaveClass(
        'rounded-lg',
        'border',
        'border-border/70',
        'bg-background/70',
      )
    }
  })

  it('renders compact loading skeletons without a card or header', () => {
    vi.mocked(api.tasks.downloadFileBlob).mockImplementation(
      () => new Promise<Blob>(() => undefined),
    )

    const { container } = render(
      <ReferenceUsageSummary task={seednoteTask} files={[summaryTaskFile]} variant="compact" />,
    )

    const status = screen.getByText('正在读取参考素材使用摘要').closest('[role="status"]')

    expect(status).not.toBeNull()
    expect(status).toHaveAttribute('aria-live', 'polite')
    expect(status).toHaveAttribute('aria-atomic', 'true')
    expect(status).toHaveTextContent('正在读取参考素材使用摘要')
    expect(screen.queryByText('参考素材使用')).not.toBeInTheDocument()
    expect(container.querySelector('[data-slot="card"]')).not.toBeInTheDocument()
  })

  it('renders a compact input fallback when the summary artifact is missing', () => {
    const { container } = render(
      <ReferenceUsageSummary
        task={{
          ...seednoteTask,
          input_attachments: [{
            type: 'image',
            file_name: 'product-front.png',
            url: 'https://cdn.test/product-front.png',
            instruction: '保留瓶身标签',
          }],
        }}
        files={[]}
        variant="compact"
      />,
    )

    expect(screen.getByText('未生成素材使用结论，仅展示任务输入。')).toBeInTheDocument()
    expect(screen.getByText('product-front.png')).toBeInTheDocument()
    expect(screen.getByText('https://cdn.test/product-front.png')).toBeInTheDocument()
    expect(screen.getByText('保留瓶身标签')).toBeInTheDocument()
    expect(screen.queryByText('仅显示输入快照')).not.toBeInTheDocument()
    expect(screen.queryByText('输入快照不代表 AI 实际使用结论')).not.toBeInTheDocument()
    expect(container.querySelector('[data-slot="card"]')).not.toBeInTheDocument()
    expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()

    const attachmentSurface = screen.getByText('product-front.png')
      .parentElement?.parentElement?.parentElement
    expect(attachmentSurface).toHaveClass('min-w-0', 'p-3')
    expect(attachmentSurface).not.toHaveClass(
      'rounded-lg',
      'border',
      'border-border/70',
      'bg-background/70',
    )
  })

  it('renders a compact input fallback when the summary artifact is malformed', async () => {
    vi.mocked(api.tasks.downloadFileBlob).mockResolvedValue(
      new Blob([JSON.stringify({ version: '1.0', inputs: 'invalid', outputs: [] })], {
        type: 'application/json',
      }),
    )

    render(
      <ReferenceUsageSummary
        task={{
          ...seednoteTask,
          input_attachments: [{
            type: 'image',
            file_name: 'plan-product.png',
            url: 'https://cdn.test/plan-product.png',
            instruction: '优先识别新版包装',
          }],
        }}
        files={[summaryTaskFile]}
        variant="compact"
      />,
    )

    expect(await screen.findByText('素材使用结论无法解析，仅展示任务输入。')).toBeInTheDocument()
    expect(screen.getByText('plan-product.png')).toBeInTheDocument()
    expect(screen.getByText('https://cdn.test/plan-product.png')).toBeInTheDocument()
    expect(screen.getByText('优先识别新版包装')).toBeInTheDocument()
    expect(screen.queryByText('仅显示输入快照')).not.toBeInTheDocument()
    expect(screen.queryByText('输入快照不代表 AI 实际使用结论')).not.toBeInTheDocument()
  })

  it('renders a quiet compact empty state without a summary or input attachments', () => {
    const { container } = render(
      <ReferenceUsageSummary task={seednoteTask} files={[]} variant="compact" />,
    )

    expect(screen.getByText('没有参考素材。')).toBeInTheDocument()
    expect(container.querySelector('[data-slot="card"]')).not.toBeInTheDocument()
    expect(api.tasks.downloadFileBlob).not.toHaveBeenCalled()
  })
})
