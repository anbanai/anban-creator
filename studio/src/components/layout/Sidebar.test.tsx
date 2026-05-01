import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import Sidebar from './Sidebar'

const themeMock = vi.hoisted(() => ({
  currentTheme: 'system',
  setTheme: vi.fn(),
}))

vi.mock('next-themes', () => ({
  useTheme: () => ({
    theme: themeMock.currentTheme,
    setTheme: themeMock.setTheme,
  }),
}))

vi.mock('@/components/auth/UserAccountPopover', () => ({
  default: () => null,
}))

function renderSidebar() {
  return render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>,
  )
}

describe('Sidebar theme switcher', () => {
  beforeEach(() => {
    themeMock.currentTheme = 'system'
    themeMock.setTheme.mockClear()
  })

  it('shows light, dark, and system options', async () => {
    renderSidebar()

    const trigger = await screen.findByRole('button', { name: '主题模式：系统' })
    fireEvent.click(trigger)

    expect(await screen.findByText('亮色')).toBeInTheDocument()
    expect(screen.getByText('暗色')).toBeInTheDocument()
    expect(screen.getByText('系统')).toBeInTheDocument()
  })

  it.each([
    ['亮色', 'light'],
    ['暗色', 'dark'],
    ['系统', 'system'],
  ])('applies %s theme when selected', async (label, value) => {
    renderSidebar()

    fireEvent.click(await screen.findByRole('button', { name: '主题模式：系统' }))

    fireEvent.click(await screen.findByText(label))
    expect(themeMock.setTheme).toHaveBeenCalledWith(value)
  })
})
