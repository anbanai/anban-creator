import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import GlobalCommandPalette from '@/components/GlobalCommandPalette'
import { NavigationProgress } from '@/components/NavigationProgress'
import { TooltipProvider } from '@/components/ui/tooltip'
import { commandPaletteStore } from '@/lib/command-palette'
import { QueryClientProvider } from '@tanstack/react-query'
import { createTestQueryClient } from '@/test/test-utils'
import Sidebar from './Sidebar'

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
              <Route path="/connect/claude-code" element={<h1>Claude Code 页</h1>} />
            </Routes>
          </main>
        </TooltipProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('Sidebar', () => {
  beforeEach(() => {
    commandPaletteStore.close()
    try {
      localStorage.clear()
    } catch {}
  })

  it('renders navigation items', () => {
    renderSidebar()

    expect(screen.getAllByText('首页').length).toBeGreaterThan(0)
    expect(screen.getByText('项目')).toBeInTheDocument()
    expect(screen.getByText('计划')).toBeInTheDocument()
    expect(screen.getByText('任务')).toBeInTheDocument()
  })

  it('renders user account popover', () => {
    renderSidebar()

    expect(screen.getByTestId('user-popover')).toBeInTheDocument()
  })

  it('does not render workshop navigation item', () => {
    renderSidebar()

    expect(screen.queryByText('创意工坊')).not.toBeInTheDocument()
  })

  it('keeps low-frequency connection setup behind the more menu', async () => {
    renderSidebar()

    expect(screen.queryByText('接入配置')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Claude Code' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '更多' }))

    expect(await screen.findByRole('link', { name: 'Claude Code' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'OpenClaw' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Codex' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '设置' })).toBeInTheDocument()
  })

  it('keeps the utility menu reachable when collapsed', async () => {
    renderSidebar()

    fireEvent.click(screen.getByRole('button', { name: '收起侧边栏' }))
    fireEvent.click(screen.getByRole('button', { name: '更多' }))

    expect(await screen.findByRole('link', { name: 'Claude Code' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'OpenClaw' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Codex' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '设置' })).toBeInTheDocument()
  })

  it('navigates from the utility menu and closes the popover', async () => {
    renderAuthenticatedShell()

    fireEvent.click(screen.getByRole('button', { name: '更多' }))
    fireEvent.click(await screen.findByRole('link', { name: '设置' }))

    expect(await screen.findByRole('heading', { name: '设置页' })).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.queryByRole('link', { name: 'Claude Code' })).not.toBeInTheDocument()
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
