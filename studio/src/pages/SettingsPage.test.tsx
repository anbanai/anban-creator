import { screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import SettingsPage from './SettingsPage'

vi.mock('@/lib/api', () => ({
  api: {
    apiKeys: { list: vi.fn().mockResolvedValue({ items: [] }), create: vi.fn(), revoke: vi.fn() },
    ilink: { status: vi.fn().mockResolvedValue({ bound: false }), createBindCode: vi.fn(), unbind: vi.fn(), setDefaultProject: vi.fn() },
    projects: { list: vi.fn().mockResolvedValue([]) },
    auth: { changePassword: vi.fn() },
  },
}))

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { email: 'a@b.com', nickname: '测试', tier: 'free', max_concurrent_limit: 2, created_at: '2025-01-01T00:00:00Z' } }),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

describe('SettingsPage password guidance', () => {
  it('states the new password rule and the session impact', () => {
    render(<SettingsPage />)

    expect(screen.getByText('8-128 个字符，且不能与当前密码相同。修改成功后所有登录会话失效，需要用新密码重新登录。')).toBeInTheDocument()
  })
})
