import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { render } from '@/test/test-utils'
import LandingPage from './LandingPage'

describe('LandingPage', () => {
  it('presents the product story and authenticated entry points', () => {
    render(<LandingPage />)

    expect(screen.getByRole('heading', { name: /让每一轮内容生产/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /不是又一个/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /从灵感到影响力/ })).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: /^注册/ }).every((link) => link.getAttribute('href') === '/register')).toBe(true)
    expect(screen.getAllByRole('link', { name: /^登录/ }).every((link) => link.getAttribute('href') === '/login')).toBe(true)
  })

  it('renders the brand lockups for navigation, hero, and footer', () => {
    render(<LandingPage />)

    expect(screen.getAllByRole('img', { name: 'Anban AI' })).toHaveLength(5)
  })

  it('keeps the workflow map and studio preview in sync', () => {
    render(<LandingPage />)

    const visualStage = screen.getAllByRole('tab').filter((tab) => tab.textContent?.includes('视觉'))
    expect(visualStage).toHaveLength(2)
    fireEvent.click(visualStage[0])

    expect(visualStage[0]).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByText('让内容拥有自己的画面。')).toBeInTheDocument()
    expect(screen.getAllByText('封面方向与视觉提示')).toHaveLength(2)
    expect(screen.getByText('FRAME COMPOSING')).toBeInTheDocument()
  })
})
