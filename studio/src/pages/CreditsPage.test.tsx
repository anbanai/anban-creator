import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import CreditsPage from './CreditsPage'
import { api } from '@/lib/api'
import { render } from '@/test/test-utils'

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ user: { tier: 'free' } }),
}))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      credits: {
        ...actual.api.credits,
        balance: vi.fn().mockResolvedValue({ balance: 1000 }),
        signInStatus: vi.fn().mockResolvedValue({ signed_in_today: false }),
        transactions: vi.fn().mockResolvedValue({ items: [], total: 0 }),
        pricing: vi.fn().mockResolvedValue({
          task_costs: {},
          model_costs: {},
          recharge_tiers: [],
          income: { daily_sign_in: 100, register_bonus: 1000, invite_reward: 1000 },
        }),
        signIn: vi.fn(),
      },
    },
  }
})

describe('CreditsPage recharge tiers', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('uses the production sign-in fallback when pricing omits income', async () => {
    vi.mocked(api.credits.pricing).mockResolvedValueOnce({
      task_costs: {},
      model_costs: {},
      recharge_tiers: [],
    } as Awaited<ReturnType<typeof api.credits.pricing>>)

    render(<CreditsPage />)

    expect(await screen.findByRole('button', { name: '签到 +100' })).toBeInTheDocument()
  })

  it('does not show fallback recharge packages when the server explicitly returns none', async () => {
    render(<CreditsPage />)

    fireEvent.click(await screen.findByRole('button', { name: '充值' }))

    expect(await screen.findByRole('dialog', { name: '充值积分' })).toBeInTheDocument()
    expect(screen.queryByText('基础包')).not.toBeInTheDocument()
    expect(screen.queryByText('标准包')).not.toBeInTheDocument()
    expect(screen.queryByText('进阶包')).not.toBeInTheDocument()
    expect(api.credits.pricing).toHaveBeenCalled()
  })
})
