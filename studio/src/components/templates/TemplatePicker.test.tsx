import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { Template } from '@/types'
import { TemplatePicker } from './TemplatePicker'

vi.mock('@/lib/api', () => ({
  api: {
    templates: {
      list: vi.fn(),
    },
  },
}))

import { api } from '@/lib/api'

const mockTemplates: Template[] = [
  {
    id: 't1',
    type: 'seednote',
    name: '水彩治愈系',
    user_id: 'u1',
    visibility: 'public',
    category: '插画',
    thumbnail_url: 'https://example.com/t1.png',
    structure: {},
    style_prompt: '整体氛围：治愈',
    example_content: {},
    tags: [],
    sort_order: 0,
    is_active: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
  {
    id: 't2',
    type: 'seednote',
    name: '极简日系',
    user_id: 'u1',
    visibility: 'private',
    category: '摄影',
    thumbnail_url: 'https://example.com/t2.png',
    structure: {},
    style_prompt: '整体氛围：极简',
    example_content: {},
    tags: [],
    sort_order: 1,
    is_active: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
]

function renderWithClient(node: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>)
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('TemplatePicker', () => {
  it('未选状态渲染轮播区块和模板卡片', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: mockTemplates, total: 2 })

    renderWithClient(
      <TemplatePicker
        type="seednote"
        selected={null}
        onSelect={() => {}}
      />,
    )

    await waitFor(() => {
      expect(screen.getByText('水彩治愈系')).toBeInTheDocument()
    })

    expect(screen.getByText('选择模板覆盖账号风格')).toBeInTheDocument()
    expect(screen.getAllByText((_, node) => !!node?.textContent?.match(/^2\s*个$/)).length).toBeGreaterThan(0)
    expect(screen.getByText('极简日系')).toBeInTheDocument()
  })

  it('模板列表为空时显示空状态', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: [], total: 0 })

    renderWithClient(
      <TemplatePicker
        type="seednote"
        selected={null}
        onSelect={() => {}}
      />,
    )

    await waitFor(() => {
      expect(screen.getByText('暂无此类型的模板')).toBeInTheDocument()
    })
  })

  it('点击模板卡片调用 onSelect', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: mockTemplates, total: 2 })
    const onSelect = vi.fn()

    renderWithClient(
      <TemplatePicker type="seednote" selected={null} onSelect={onSelect} />,
    )

    await waitFor(() => {
      expect(screen.getByText('水彩治愈系')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('水彩治愈系'))
    expect(onSelect).toHaveBeenCalledWith(mockTemplates[0])
  })

  it('selected 状态保持轮播常驻并在选中卡片上显示激活态', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: mockTemplates, total: 2 })

    renderWithClient(
      <TemplatePicker
        type="seednote"
        selected={mockTemplates[0]}
        onSelect={() => {}}
      />,
    )

    await waitFor(() => {
      expect(screen.getByText('水彩治愈系')).toBeInTheDocument()
    })

    // 轮播仍常驻 —— 标题和其他卡片都可见
    expect(screen.getByText('选择模板覆盖账号风格')).toBeInTheDocument()
    expect(screen.getByText('极简日系')).toBeInTheDocument()

    // 选中卡片有 aria-pressed
    const activeCard = screen.getByText('水彩治愈系').closest('button')
    expect(activeCard).toHaveAttribute('aria-pressed', 'true')

    // 未选中卡片没有 aria-pressed=true
    const inactiveCard = screen.getByText('极简日系').closest('button')
    expect(inactiveCard).toHaveAttribute('aria-pressed', 'false')
  })

  it('点击已选中的卡片仍触发 onSelect', async () => {
    vi.mocked(api.templates.list).mockResolvedValue({ items: mockTemplates, total: 2 })
    const onSelect = vi.fn()

    renderWithClient(
      <TemplatePicker
        type="seednote"
        selected={mockTemplates[0]}
        onSelect={onSelect}
      />,
    )

    await waitFor(() => {
      expect(screen.getByText('水彩治愈系')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('水彩治愈系'))
    expect(onSelect).toHaveBeenCalledWith(mockTemplates[0])
  })
})
