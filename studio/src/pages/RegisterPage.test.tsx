import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import RegisterPage from './RegisterPage'

const navigateMock = vi.hoisted(() => vi.fn())

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => navigateMock }
})

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ login: vi.fn() }),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

describe('RegisterPage field guidance', () => {
  it('explains the fields that block registration', () => {
    render(<RegisterPage />)

    expect(screen.getByText('验证码邮件在有限时间内有效，过期后请重新发送。')).toBeInTheDocument()
    expect(screen.getByText('开启邀请注册时必填；不区分大小写，提交后会占用邀请人的一次邀请额度。')).toBeInTheDocument()
    expect(screen.getByText('至少 8 个字符，用于登录和调用平台密钥。')).toBeInTheDocument()
  })
})
