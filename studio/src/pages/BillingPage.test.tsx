import { fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BillingPage from './BillingPage'
import { render } from '@/test/test-utils'

vi.mock('sonner', () => ({ toast: { success: vi.fn() } }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      billing: {
        wallet: vi.fn().mockResolvedValue({ paid: 8000, promotional: 1000, debt: 2500, balance: 6500 }),
        transactions: vi.fn().mockResolvedValue({ items: [], total: 0, offset: 0, limit: 20 }),
        referral: vi.fn().mockResolvedValue({
          invite_code: 'INVITE01',
          invite_link: 'https://example.com/register?invite=INVITE01',
          status: 'not_issued',
          program: {
            id: 'referral-v1',
            catalog_id: 'promotion-v1',
            minimum_topup_credits: 10000,
            inviter_credits: 1000,
            invitee_credits: 1000,
            expires_after_seconds: 2592000,
            max_inviter_rewards: 10,
          },
        }),
      },
    },
  }
})

describe('BillingPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows wallet buckets and debt admission policy', async () => {
    render(<BillingPage />)

    expect(await screen.findByText('6,500')).toBeInTheDocument()
    expect(screen.getByText('8,000')).toBeInTheDocument()
    expect(screen.getByText('1,000')).toBeInTheDocument()
    expect(screen.getAllByText('2,500').length).toBeGreaterThan(0)
    expect(screen.getByText(/补齐前不能创建新任务/)).toBeInTheDocument()
  })

  it('explains offline top-up without bonus credits', async () => {
    render(<BillingPage />)
    fireEvent.click(await screen.findByRole('button', { name: '充值' }))

    expect(await screen.findByRole('dialog', { name: '联系客服充值' })).toBeInTheDocument()
    expect(screen.getByText(/充值不附赠积分/)).toBeInTheDocument()
  })
})
