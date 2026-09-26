import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import LoginPage from './LoginPage'

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => vi.fn() }
})

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ login: vi.fn() }),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

describe('LoginPage field guidance', () => {
  it('explains the optional invite field', () => {
    window.history.pushState({}, '', '/login?invite=abc123')
    render(<LoginPage />)

    expect(screen.getByText('来自邀请链接，登录时无需输入。')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: '验证码登录' }))
    expect(screen.getByText('验证码邮件在有限时间内有效，过期后请重新发送。')).toBeInTheDocument()
  })
})
