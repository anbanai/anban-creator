import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { render } from '@/test/test-utils'
import AdminBillingPage from './AdminBillingPage'

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      adminBilling: {
        users: vi.fn().mockResolvedValue({ items: [{ id: 'user-1', email: 'writer@example.com', nickname: '作者', tier: 'pro', is_admin: false, wallet: { paid: 8000, promotional: 1000, debt: 0, balance: 9000 } }] }),
        topups: vi.fn().mockResolvedValue({ items: [], total: 0, offset: 0, limit: 20 }),
        entries: vi.fn().mockResolvedValue({ items: [], total: 0, offset: 0, limit: 20 }),
        createTopUp: vi.fn().mockResolvedValue({ status: 'succeeded', entry_id: 'entry-1', paid_credits_added: 10000, debt_repaid_credits: 0 }),
      },
    },
  }
})

describe('AdminBillingPage', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows a user search, guarded recharge form, and record tabs', async () => {
    render(<AdminBillingPage />)

    expect(await screen.findByText('管理员充值')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '搜索用户' })).toBeInTheDocument()
    expect(screen.getByRole('spinbutton', { name: '充值积分' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '外部流水号' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '充值记录' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '钱包流水' })).toBeInTheDocument()
    expect(await screen.findByText('writer@example.com')).toBeInTheDocument()
  })

  it('previews and submits a top-up after user selection', async () => {
    render(<AdminBillingPage />)

    fireEvent.click(await screen.findByRole('button', { name: /选择作者/ }))
    fireEvent.change(screen.getByRole('spinbutton', { name: '充值积分' }), { target: { value: '10000' } })
    fireEvent.change(screen.getByRole('textbox', { name: '外部流水号' }), { target: { value: 'cashier-001' } })

    expect(screen.getByText('充值后可用余额')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认充值' }))
    fireEvent.click(await screen.findByRole('button', { name: '确认并入账' }))

    await waitFor(() => expect(api.adminBilling.createTopUp).toHaveBeenCalledWith({ user_id: 'user-1', credits: 10000, external_source_id: 'cashier-001' }))
  })

  it('does not submit without a selected user and external source id', async () => {
    render(<AdminBillingPage />)
    fireEvent.change(screen.getByRole('spinbutton', { name: '充值积分' }), { target: { value: '10000' } })
    fireEvent.click(screen.getByRole('button', { name: '确认充值' }))
    expect(api.adminBilling.createTopUp).not.toHaveBeenCalled()
  })

  it('queries recharge records with status, date, user, and external reference filters', async () => {
    render(<AdminBillingPage />)

    fireEvent.change(screen.getByRole('textbox', { name: '按用户 ID 筛选' }), { target: { value: 'user-1' } })
    fireEvent.change(screen.getByRole('combobox', { name: '按状态筛选' }), { target: { value: 'failed' } })
    fireEvent.change(screen.getByLabelText('开始时间'), { target: { value: '2026-10-12T10:00' } })
    fireEvent.change(screen.getByLabelText('结束时间'), { target: { value: '2026-10-12T11:00' } })
    fireEvent.change(screen.getByRole('textbox', { name: '按外部流水号筛选' }), { target: { value: 'cashier-001' } })

    await waitFor(() => expect(api.adminBilling.topups).toHaveBeenLastCalledWith(expect.objectContaining({
      user_id: 'user-1',
      status: 'failed',
      from: new Date('2026-10-12T10:00').toISOString(),
      to: new Date('2026-10-12T11:01').toISOString(),
      external_source_id: 'cashier-001',
    })))
  })

  it('keeps the submitted details when the original matching top-up failed', async () => {
    vi.mocked(api.adminBilling.createTopUp).mockResolvedValueOnce({
      status: 'replayed',
      operation: {
        id: 'replay-1', user_id: 'user-1', admin_user_id: 'admin-1', credits: 10000,
        external_source_type: 'manual_api', external_source_id: 'cashier-001', status: 'replayed',
        original_status: 'failed', error_message: 'catalog is unavailable', paid_credits_added: 0,
        debt_repaid_credits: 0, created_at: '2026-10-12T00:00:00Z', updated_at: '2026-10-12T00:00:00Z',
      },
    })

    render(<AdminBillingPage />)
    fireEvent.click(await screen.findByRole('button', { name: /选择作者/ }))
    fireEvent.change(screen.getByRole('spinbutton', { name: '充值积分' }), { target: { value: '10000' } })
    fireEvent.change(screen.getByRole('textbox', { name: '外部流水号' }), { target: { value: 'cashier-001' } })
    fireEvent.click(screen.getByRole('button', { name: '确认充值' }))
    fireEvent.click(await screen.findByRole('button', { name: '确认并入账' }))

    await waitFor(() => expect(screen.getByRole('textbox', { name: '外部流水号' })).toHaveValue('cashier-001'))
    expect(screen.getByRole('spinbutton', { name: '充值积分' })).toHaveValue(10000)
  })
})
