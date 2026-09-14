import { act, screen } from '@testing-library/react'
import type React from 'react'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import GlobalCommandPalette from './GlobalCommandPalette'
import { commandPaletteStore } from '@/lib/command-palette'
import { render as renderWithProviders } from '@/test/test-utils'

const authState = vi.hoisted(() => ({ isAdmin: false }))

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { is_admin: authState.isAdmin } }),
}))

vi.mock('next-themes', () => ({
  ThemeProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useTheme: () => ({
    setTheme: vi.fn(),
  }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      tasks: {
        ...actual.api.tasks,
        list: vi.fn().mockResolvedValue({
          items: [{
            id: 'failed-task',
            type: 'article',
            title: '失败文章',
            prompt: '失败任务',
            status: 'failed',
            progress: 0,
            error_message: '模型超时',
            plan_id: null,
            project_id: 'project-1',
            created_at: new Date().toISOString(),
            started_at: '',
            completed_at: '',
          }],
          total: 1,
        }),
      },
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue([{
          id: 'project-1',
          user_id: 'user-1',
          platform: 'article',
          name: '公众号项目',
          avatar_url: '',
          profile_url: '',
          keywords: '',
          visual_style: '',
          writer: '',
          theme: '',
          author: '',

          image_ratio: '',
          max_concurrent_tasks: 1,
          config: {},
          status: 'active',
          created_at: '2026-07-01T00:00:00.000Z',
          updated_at: '2026-07-01T00:00:00.000Z',
        }]),
      },
      plans: {
        ...actual.api.plans,
        list: vi.fn().mockResolvedValue({ items: [], total: 0 }),
      },
      billing: {
        ...actual.api.billing,
        wallet: vi.fn().mockResolvedValue({ paid: 80, promotional: 0, debt: 0, balance: 80 }),
      },
      apiKeys: {
        ...actual.api.apiKeys,
        list: vi.fn().mockResolvedValue({ items: [] }),
      },
    },
  }
})

describe('GlobalCommandPalette actions', () => {
  beforeEach(() => {
    authState.isAdmin = false
    commandPaletteStore.close()
  })

  it('groups contextual actions before navigation', async () => {
    renderWithProviders(<GlobalCommandPalette />)

    act(() => commandPaletteStore.open())

    expect(await screen.findByRole('dialog', { name: '行动面板' })).toBeInTheDocument()
    expect(await screen.findByText('继续工作')).toBeInTheDocument()
    expect(await screen.findByText('创建')).toBeInTheDocument()
    expect(await screen.findByText('恢复')).toBeInTheDocument()
    expect(await screen.findByText('跳转')).toBeInTheDocument()
    expect(screen.queryByText('设计师')).not.toBeInTheDocument()
    expect(screen.queryByText('Claude Code')).not.toBeInTheDocument()
    expect(await screen.findByText('插件')).toBeInTheDocument()
    expect((await screen.findAllByText('设置')).length).toBeGreaterThan(0)
    expect(screen.queryByText('打开接入就绪中心')).not.toBeInTheDocument()
    expect(await screen.findByText('恢复失败任务')).toBeInTheDocument()
    expect(await screen.findByText('新建公众号文章')).toBeInTheDocument()
  })

  it('surfaces setup review to administrators when readiness is missing', async () => {
    authState.isAdmin = true
    renderWithProviders(<GlobalCommandPalette />)

    act(() => commandPaletteStore.open())

    expect(await screen.findByText('检查接入设置')).toBeInTheDocument()
    expect(await screen.findByText('新建公众号文章')).toBeInTheDocument()
  })

  it('only exposes administrator navigation to administrators', async () => {
    const regularView = renderWithProviders(<GlobalCommandPalette />)
    act(() => commandPaletteStore.open())
    expect(await screen.findByRole('dialog', { name: '行动面板' })).toBeInTheDocument()
    expect(screen.queryByText('模板库')).not.toBeInTheDocument()
    expect(screen.queryByText('设计师')).not.toBeInTheDocument()
    expect(screen.queryByText('Claude Code')).not.toBeInTheDocument()
    expect(screen.queryByText('Codex')).not.toBeInTheDocument()
    expect(await screen.findByText('插件')).toBeInTheDocument()
    expect((await screen.findAllByText('设置')).length).toBeGreaterThan(0)
    regularView.unmount()

    commandPaletteStore.close()
    authState.isAdmin = true
    renderWithProviders(<GlobalCommandPalette />)
    act(() => commandPaletteStore.open())
    expect(await screen.findByText('模板库')).toBeInTheDocument()
    expect(screen.queryByText('设计师')).not.toBeInTheDocument()
    expect(screen.queryByText('Claude Code')).not.toBeInTheDocument()
    expect(screen.queryByText('Codex')).not.toBeInTheDocument()
    expect(await screen.findByText('插件')).toBeInTheDocument()
    expect(await screen.findByText('打开接入就绪中心')).toBeInTheDocument()
  })
})
