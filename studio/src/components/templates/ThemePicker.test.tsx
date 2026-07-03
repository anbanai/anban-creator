import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ThemePicker } from './ThemePicker'

vi.mock('@/lib/api', () => ({
  api: {
    resources: {
      list: vi.fn(),
      previewTheme: vi.fn(),
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

const themes = [
  {
    name: 'autumn-warm',
    category: 'themes',
    description: '【秋日暖光】温暖治愈，橙色调，文艺美学',
    mood: '温暖治愈',
    best_for: '情感故事、生活随笔',
    colors: {
      background: '#faf9f5',
      primary: '#d97758',
      secondary: '#c06b4d',
    },
  },
  {
    name: 'geek-terminal',
    category: 'themes',
    description: '【极客终端】等宽字，终端绿，程序员风',
    mood: '极客/终端',
    best_for: '编程、开发者、极客向',
    colors: {
      background: '#f6f8fa',
      primary: '#1a7f37',
      secondary: '#0550ae',
    },
  },
]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.resources.list).mockResolvedValue({ category: 'themes', items: themes })
  vi.mocked(api.resources.previewTheme).mockResolvedValue('<!doctype html><html><body>preview</body></html>')
})

describe('ThemePicker', () => {
  it('renders the selected theme as an avatar-style rich select with color preview metadata', async () => {
    renderWithClient(<ThemePicker theme="autumn-warm" onTheme={() => {}} />)

    await waitFor(() => {
      expect(screen.getByText('【秋日暖光】温暖治愈，橙色调，文艺美学')).toBeInTheDocument()
    })

    expect(screen.getByLabelText('排版风格色彩预览 【秋日暖光】温暖治愈，橙色调，文艺美学')).toBeInTheDocument()
    expect(screen.getByText('情感故事、生活随笔')).toBeInTheDocument()
    expect(screen.getByTitle('排版预览')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('combobox'))
    await waitFor(() => {
      expect(screen.getByText('【极客终端】等宽字，终端绿，程序员风')).toBeInTheDocument()
    })
    expect(screen.getByLabelText('排版风格色彩预览 【极客终端】等宽字，终端绿，程序员风')).toBeInTheDocument()
  })

  it('keeps showing the saved theme key when metadata is missing', async () => {
    vi.mocked(api.resources.list).mockResolvedValue({ category: 'themes', items: [] })

    renderWithClient(<ThemePicker theme="custom-theme" onTheme={() => {}} />)

    await waitFor(() => {
      expect(screen.getByText('custom-theme')).toBeInTheDocument()
    })
    expect(screen.getByLabelText('排版风格色彩预览 custom-theme')).toBeInTheDocument()
  })
})
