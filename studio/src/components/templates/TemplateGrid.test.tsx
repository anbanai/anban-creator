import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Template } from '@/types'
import { TemplateGrid } from './TemplateGrid'

const apiMocks = vi.hoisted(() => ({
  list: vi.fn(),
  remove: vi.fn(),
}))

vi.mock('@/lib/api', () => ({
  api: { templates: apiMocks },
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
  thumbnail_url: 'https://cdn.example/template.png',
  prompt: '清透版式',
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
  })

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
})
