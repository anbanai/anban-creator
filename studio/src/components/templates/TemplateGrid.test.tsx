import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { Template } from '@/types'
import { TemplateGrid } from './TemplateGrid'

const apiMocks = vi.hoisted(() => ({
  list: vi.fn(),
  remove: vi.fn(),
  retry: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: {
    templates: {
      list: apiMocks.list,
      remove: apiMocks.remove,
    },
    imageAnalyses: { retry: apiMocks.retry },
  },
}))

vi.mock('./TemplateCreateDialog', () => ({
  TemplateCreateDialog: ({ open, template }: { open: boolean; template?: Template | null }) => (
    open ? <div>编辑对象：{template?.name ?? '新模板'}</div> : null
  ),
}))

const template: Template = {
  id: 'template-1',
  type: 'seednote',
  name: '清透说明书',
  category: '美妆护肤',
  thumbnail: { asset_id: 'asset-1', file_name: 'template.png', content_type: 'image/png', size: 100, download_url: 'https://cdn.example/template.png', download_expires_at: '2026-08-02T01:00:00Z' },
  prompt: '清透版式',
  prompt_source: 'manual', readiness_status: 'ready', activate_when_ready: false,
  visibility: 'private',
  sort_order: 30,
  is_active: false,
  created_at: '2026-08-02T00:00:00Z',
  updated_at: '2026-08-02T00:00:00Z',
}

function renderGrid() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <TemplateGrid />
    </QueryClientProvider>,
  )
}

describe('TemplateGrid', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.list.mockResolvedValue({ items: [template], total: 1 })
    apiMocks.remove.mockResolvedValue({ deleted: template.id })
    apiMocks.retry.mockResolvedValue({ status: 'queued' })
  })

  afterEach(() => vi.useRealTimers())

  it('按管理员范围和固定行业筛选，并展示排序、可见性及启停状态', async () => {
    renderGrid()
    expect(await screen.findByText('清透说明书')).toBeInTheDocument()
    expect(screen.getByText('私有', { selector: '[data-slot="badge"]' })).toBeInTheDocument()
    expect(screen.getByText('已停用')).toBeInTheDocument()
    expect(screen.getByText('排序 30')).toBeInTheDocument()

    expect(apiMocks.list).toHaveBeenCalledWith(expect.objectContaining({
      type: 'seednote',
      scope: 'all',
      limit: 100,
    }))

    fireEvent.click(screen.getByRole('button', { name: '停用' }))
    await waitFor(() => expect(apiMocks.list).toHaveBeenCalledWith(expect.objectContaining({
      type: 'seednote',
      scope: 'inactive',
    })))

    fireEvent.click(screen.getByRole('button', { name: '美妆护肤' }))
    await waitFor(() => expect(apiMocks.list).toHaveBeenCalledWith(expect.objectContaining({
      category: '美妆护肤',
      scope: 'inactive',
    })))
  })

  it('从卡片打开编辑弹窗', async () => {
    renderGrid()
    await screen.findByText('清透说明书')
    fireEvent.click(screen.getByRole('button', { name: '编辑 清透说明书' }))
    expect(screen.getByText('编辑对象：清透说明书')).toBeInTheDocument()
  })

  it('确认后删除模板并刷新列表', async () => {
    renderGrid()
    await screen.findByText('清透说明书')
    fireEvent.click(screen.getByRole('button', { name: '删除 清透说明书' }))
    expect(screen.getByRole('alertdialog', { name: '删除模板？' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(apiMocks.remove).toHaveBeenCalledWith(template.id))
    await waitFor(() => expect(apiMocks.list).toHaveBeenCalledTimes(2))
  })

  it('识别中明确显示完成后自动启用', async () => {
    apiMocks.list.mockResolvedValue({
      items: [{
        ...template,
        readiness_status: 'analyzing',
        activate_when_ready: true,
        image_analysis: {
          id: 'analysis-active',
          kind: 'template_prompt',
          status: 'running',
          attempt_count: 1,
          can_retry: false,
          updated_at: '2026-09-18T10:00:00Z',
        },
      }],
      total: 1,
    })

    renderGrid()

    expect(await screen.findByText('识别中，完成后自动启用')).toBeInTheDocument()
  })

  it('识别失败时展示安全错误并允许重试', async () => {
    apiMocks.list.mockResolvedValue({
      items: [{
        ...template,
        readiness_status: 'failed',
        image_analysis: {
          id: 'analysis-failed',
          kind: 'template_prompt',
          status: 'failed',
          attempt_count: 3,
          error_message: '图片服务暂时不可用，请稍后重试',
          can_retry: true,
          updated_at: '2026-09-18T10:05:00Z',
        },
      }],
      total: 1,
    })

    renderGrid()

    expect(await screen.findByText('图片服务暂时不可用，请稍后重试')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '重试识别 清透说明书' }))
    await waitFor(() => expect(apiMocks.retry).toHaveBeenCalledWith('analysis-failed'))
  })

  it('仅在存在活动识别作业时每两秒刷新', async () => {
    vi.useFakeTimers()
    const activeTemplate: Template = {
      ...template,
      image_analysis: {
        id: 'analysis-active',
        kind: 'template_prompt',
        status: 'queued',
        attempt_count: 0,
        can_retry: false,
        updated_at: '2026-09-18T10:00:00Z',
      },
    }
    apiMocks.list
      .mockResolvedValueOnce({ items: [activeTemplate], total: 1 })
      .mockResolvedValue({ items: [template], total: 1 })

    renderGrid()
    await vi.waitFor(() => expect(apiMocks.list).toHaveBeenCalledTimes(1))
    await vi.advanceTimersByTimeAsync(2100)
    await vi.waitFor(() => expect(apiMocks.list).toHaveBeenCalledTimes(2))

    await vi.advanceTimersByTimeAsync(2100)
    expect(apiMocks.list).toHaveBeenCalledTimes(2)
  })
})
