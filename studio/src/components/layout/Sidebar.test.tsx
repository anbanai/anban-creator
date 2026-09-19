import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import GlobalCommandPalette from '@/components/GlobalCommandPalette'
import { NavigationProgress } from '@/components/NavigationProgress'
import { TooltipProvider } from '@/components/ui/tooltip'
import { commandPaletteStore } from '@/lib/command-palette'
import { QueryClientProvider } from '@tanstack/react-query'
import { createTestQueryClient } from '@/test/test-utils'
import Sidebar from './Sidebar'

const authState = vi.hoisted(() => ({ isAdmin: false }))

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { is_admin: authState.isAdmin } }),
}))

vi.mock('next-themes', () => ({
  useTheme: () => ({
    theme: 'system',
    setTheme: vi.fn(),
  }),
}))

vi.mock('@/components/auth/UserAccountPopover', () => ({
  default: () => <div data-testid="user-popover" />,
}))

function renderSidebar() {
  return render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>,
  )
}

function renderAuthenticatedShell(initialPath = '/') {
  const queryClient = createTestQueryClient()
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[initialPath]}>
        <TooltipProvider>
          <NavigationProgress />
          <GlobalCommandPalette />
          <Sidebar />
          <main>
            <Routes>
              <Route path="/" element={<h1>首页</h1>} />
              <Route path="/projects" element={<h1>项目页</h1>} />
              <Route path="/tasks" element={<h1>任务页</h1>} />
              <Route path="/settings" element={<h1>设置页</h1>} />
              <Route path="/plugins" element={<h1>插件页</h1>} />
            </Routes>
          </main>
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Sidebar', () => {
  beforeEach(() => {
    authState.isAdmin = false
    commandPaletteStore.close()
    try {
      localStorage.clear()
    } catch {}
  })

  it('renders only the approved MVP navigation for regular users', () => {
    renderSidebar()

    const navigation = within(screen.getByRole('navigation', { name: '主导航' }))
    expect(navigation.getAllByRole('link').map((link) => link.textContent)).toEqual([
      'AI助手',
      '项目',
      '任务',
      '计划',
      '钱包',
      '插件',
      '设置',
      '小红书数据',
      '公众号内容',
    ])
    expect(navigation.queryByText('创作')).not.toBeInTheDocument()
    expect(navigation.queryByText('自动化')).not.toBeInTheDocument()
    expect(navigation.queryByText('经营')).not.toBeInTheDocument()
    expect(navigation.queryByRole('link', { name: '时间轴' })).not.toBeInTheDocument()
    expect(navigation.queryByRole('link', { name: '用量' })).not.toBeInTheDocument()
  })

  it('只向管理员显示管理功能导航', () => {
    const regular = renderSidebar()
    expect(screen.queryByRole('link', { name: '模板库' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '设计师' })).not.toBeInTheDocument()
    expect(screen.queryByText('平台与设置')).not.toBeInTheDocument()
    regular.unmount()

    authState.isAdmin = true
    renderSidebar()
    expect(screen.getByRole('link', { name: '模板库' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '设计师' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '插件' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '设置' })).toBeInTheDocument()
    expect(screen.queryByText('平台与设置')).not.toBeInTheDocument()
    expect(screen.queryByText('资产')).not.toBeInTheDocument()
  })

  it('renders user account popover', () => {
    renderSidebar()

    expect(screen.getByTestId('user-popover')).toBeInTheDocument()
  })

  it('does not render workshop navigation item', () => {
    renderSidebar()

    expect(screen.queryByText('创意工坊')).not.toBeInTheDocument()
  })

  it('renders plugin and settings links directly for regular users', () => {
    renderSidebar()

    expect(screen.queryByRole('button', { name: '更多' })).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: '插件' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '设置' })).toBeInTheDocument()
  })

  it('keeps expanded utility links reachable when collapsed', () => {
    renderSidebar()

    fireEvent.click(screen.getByRole('button', { name: '收起侧边栏' }))

    expect(screen.getByRole('link', { name: '插件' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '设置' })).toBeInTheDocument()
  })

  it('navigates directly to settings from the sidebar', async () => {
    renderAuthenticatedShell()

    fireEvent.click(screen.getByRole('link', { name: '设置' }))

    expect(await screen.findByRole('heading', { name: '设置页' })).toBeInTheDocument()
  })

  it('closes the mobile drawer after navigating from a utility link', async () => {
    renderAuthenticatedShell()

    fireEvent.click(screen.getByRole('button', { name: '打开菜单' }))
    expect(screen.getByRole('button', { name: '关闭菜单' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '打开菜单' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('link', { name: '插件' }))

    expect(await screen.findByRole('heading', { name: '插件页' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: '关闭菜单' })).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: '打开菜单' })).toBeInTheDocument()
    })
  })

  it('navigates from the authenticated shell without breaking external stores or tooltips', async () => {
    renderAuthenticatedShell()

    fireEvent.click(screen.getByRole('link', { name: '项目' }))

    expect(await screen.findByRole('heading', { name: '项目页' })).toBeInTheDocument()
  })

  it('opens the command palette from the shell search trigger', async () => {
    renderAuthenticatedShell()

    fireEvent.click(screen.getByRole('button', { name: /搜索/ }))

    expect(await screen.findByRole('dialog', { name: '行动面板' })).toBeInTheDocument()
  })

  it('keeps collapsed tooltip navigation clickable in the authenticated shell', async () => {
    renderAuthenticatedShell()

    fireEvent.click(screen.getByRole('button', { name: '收起侧边栏' }))
    fireEvent.click(screen.getByRole('link', { name: '任务' }))

    expect(await screen.findByRole('heading', { name: '任务页' })).toBeInTheDocument()
  })
})
