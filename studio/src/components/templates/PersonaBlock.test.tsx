import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { PersonaBlock } from './PersonaBlock'

vi.mock('@/lib/api', () => ({
  api: {
    resources: {
      list: vi.fn(),
    },
  },
}))

import { api } from '@/lib/api'

function renderWithClient(node: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>)
}

const writers = [
  {
    name: '丹·科',
    english_name: 'dan-koe',
    display_name: 'Dan Koe',
    category: 'writers',
    category_cn: '商业洞察',
    description: '清晰、直接、带有个人品牌感的长文表达。',
  },
  {
    name: '日常科普',
    english_name: 'casual-science',
    display_name: '日常科普',
    category: 'writers',
    category_cn: '科普',
    description: '把复杂概念讲得轻松可信。',
  },
]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.resources.list).mockResolvedValue({ category: 'writers', items: writers })
})

describe('PersonaBlock', () => {
  it('separates publish author from writer style and renders writer as avatar profile select', async () => {
    const onAuthor = vi.fn()
    const onWriter = vi.fn()

    renderWithClient(
      <PersonaBlock
        author="案板"
        onAuthor={onAuthor}
        writer="dan-koe"
        onWriter={onWriter}
      />,
    )

    expect(screen.getByText('发布署名')).toBeInTheDocument()
    expect(screen.getByText('写作风格')).toBeInTheDocument()
    expect(screen.queryByText('发布署名 · 写作风格')).not.toBeInTheDocument()
    expect(screen.getByLabelText('公众号发布署名')).toHaveValue('案板')
    expect(screen.getByLabelText('公众号发布署名').closest('section')?.parentElement).toHaveClass('space-y-3')
    expect(screen.getByLabelText('公众号发布署名').closest('section')?.parentElement).not.toHaveClass('sm:grid-cols-2')

    await waitFor(() => {
      expect(screen.getByText('Dan Koe')).toBeInTheDocument()
    })
    expect(screen.getByText('dan-koe')).toBeInTheDocument()
    expect(screen.getByText('商业洞察')).toBeInTheDocument()
    expect(screen.getByLabelText('写作风格头像 Dan Koe')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('combobox'))
    await waitFor(() => {
      expect(screen.getByText(/清晰、直接、带有个人品牌感的长文表达/)).toBeInTheDocument()
    })
  })

  it('keeps showing the saved writer key when metadata is missing', async () => {
    vi.mocked(api.resources.list).mockResolvedValue({ category: 'writers', items: [] })

    renderWithClient(
      <PersonaBlock
        author=""
        onAuthor={() => {}}
        writer="custom-style"
        onWriter={() => {}}
      />,
    )

    expect(screen.getByText('custom-style')).toBeInTheDocument()
    expect(screen.getByLabelText('写作风格头像 custom-style')).toBeInTheDocument()
    expect(screen.queryByText('选择写作风格')).not.toBeInTheDocument()
  })
})
