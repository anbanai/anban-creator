import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Template } from '@/types'
import { SeednoteTemplateGallery } from './SeednoteTemplateGallery'

vi.mock('@/lib/api', () => ({
  api: {
    templates: {
      list: vi.fn(),
    },
  },
}))

import { api } from '@/lib/api'

const templates: Template[] = [
  {
    id: 'template-1',
    type: 'seednote',
    name: '清透成分说明书',
    category: '美妆护肤',
    thumbnail_url: 'https://example.com/beauty.png',
    prompt: '低饱和白底，封面使用产品特写与大标题，内容页保持双栏信息层级。',
    visibility: 'public',
    sort_order: 10,
    is_active: true,
    created_at: '2026-08-02T00:00:00Z',
    updated_at: '2026-08-02T00:00:00Z',
  },
  {
    id: 'template-2',
    type: 'seednote',
    name: '居家改造前后对比',
    category: '家居家装',
    thumbnail_url: '',
    prompt: '暖白自然光，封面使用前后对比构图，内容页以大图和短句交替推进。',
    visibility: 'public',
    sort_order: 20,
    is_active: true,
    created_at: '2026-08-02T00:00:00Z',
    updated_at: '2026-08-02T00:00:00Z',
  },
]

function renderGallery(props?: Partial<React.ComponentProps<typeof SeednoteTemplateGallery>>) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <SeednoteTemplateGallery platform="seednote" onApply={() => {}} {...props} />
    </QueryClientProvider>,
  )
}

describe('SeednoteTemplateGallery', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('只为小红书项目加载公开模板并展示固定分类', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: templates, total: templates.length })

    renderGallery()

    expect(await screen.findByRole('button', { name: /清透成分说明书/ })).toBeInTheDocument()
    expect(api.templates.list).toHaveBeenCalledWith({ type: 'seednote', limit: 100 })
    for (const category of ['全部', '好物种草', '美妆护肤', '健康养生', '美食生活', '家居家装', '知识科普']) {
      expect(screen.getByRole('button', { name: category })).toBeInTheDocument()
    }
  })

  it('切换分类后按分类重新查询', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: templates, total: templates.length })
    renderGallery()
    await screen.findByRole('button', { name: /清透成分说明书/ })

    fireEvent.click(screen.getByRole('button', { name: '家居家装' }))

    await waitFor(() => {
      expect(api.templates.list).toHaveBeenLastCalledWith({
        type: 'seednote',
        category: '家居家装',
        limit: 100,
      })
    })
  })

  it('点击可聚焦的模板按钮时立即交付 prompt', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: templates, total: templates.length })
    const onApply = vi.fn()
    renderGallery({ onApply })

    const card = await screen.findByRole('button', { name: /清透成分说明书/ })
    card.focus()
    expect(card).toHaveFocus()
    fireEvent.click(card)

    expect(onApply).toHaveBeenCalledWith(templates[0].prompt, templates[0])
  })

  it('非小红书项目不显示也不请求模板', () => {
    renderGallery({ platform: 'article' })

    expect(screen.queryByText('参考模板')).not.toBeInTheDocument()
    expect(api.templates.list).not.toHaveBeenCalled()
  })

  it('覆盖加载、空列表和接口失败状态', async () => {
    let resolveList: ((value: { items: Template[]; total: number }) => void) | undefined
    vi.mocked(api.templates.list).mockReturnValue(new Promise((resolve) => { resolveList = resolve }))
    const { unmount } = renderGallery()
    expect(screen.getByLabelText('模板加载中')).toBeInTheDocument()
    resolveList?.({ items: [], total: 0 })
    expect(await screen.findByText('这个分类还没有模板')).toBeInTheDocument()
    unmount()

    vi.mocked(api.templates.list).mockRejectedValue(new Error('network'))
    renderGallery()
    expect(await screen.findByText('模板加载失败')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重新加载' })).toBeInTheDocument()
  })

  it('缩略图加载失败时保留稳定卡片和名称', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: [templates[0]], total: 1 })
    renderGallery()

    const image = await screen.findByRole('img', { name: '清透成分说明书' })
    fireEvent.error(image)

    expect(screen.getByRole('button', { name: /清透成分说明书/ })).toBeInTheDocument()
    expect(screen.queryByRole('img', { name: '清透成分说明书' })).not.toBeInTheDocument()
  })
})
