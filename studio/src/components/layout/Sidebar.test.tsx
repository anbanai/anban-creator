import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

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

describe('Sidebar', () => {
  it('renders navigation items', () => {
    renderSidebar()

    expect(screen.getByText('仪表盘')).toBeInTheDocument()
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
})
