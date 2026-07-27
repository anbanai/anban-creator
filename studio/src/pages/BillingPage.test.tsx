import { fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import BillingPage from './BillingPage'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'

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

  it('identifies every paid charge and explains the top-up debt split', async () => {
    vi.mocked(api.billing.transactions).mockResolvedValueOnce({
      total: 6,
      offset: 0,
      limit: 20,
      items: [
        {
          id: 'paid-analysis-entry',
          event_kind: 'charge',
          paid_delta: -300,
          promotional_delta: 0,
          debt_delta: 0,
          charge_kind: 'operation',
          charge_policy: 'accepted_task_operation',
          sku_id: 'analysis.content.v1',
          price_credits: 300,
          charge_resource_type: 'analysis',
          charge_resource_id: 'analysis-id',
          operation_task_id: 'seednote-task-id',
          tool_call_id: 'analysis:paid-content',
          created_at: '2026-07-27T11:04:30.725Z',
        },
        {
          id: 'paid-image-entry',
          event_kind: 'charge',
          paid_delta: -500,
          promotional_delta: 0,
          debt_delta: 0,
          charge_kind: 'operation',
          charge_policy: 'accepted_task_operation',
          sku_id: 'image.seedream.content.v1',
          price_credits: 500,
          charge_resource_type: 'image',
          charge_resource_id: 'content-image-id',
          operation_task_id: 'seednote-task-id',
          tool_call_id: 'image:paid-content',
          created_at: '2026-07-27T11:03:30.725Z',
        },
        {
          id: 'task-entry',
          event_kind: 'charge',
          paid_delta: -5000,
          promotional_delta: 0,
          debt_delta: 0,
          charge_kind: 'task',
          charge_policy: 'task_admission',
          sku_id: 'task.seednote.standard.v1',
          price_credits: 5000,
          task_id: 'seednote-task-id',
          created_at: '2026-07-27T10:20:00Z',
        },
        {
          id: 'topup-entry',
          event_kind: 'topup',
          paid_delta: 97000,
          promotional_delta: 0,
          debt_delta: 0,
          topup_credits: 100000,
          debt_repaid_credits: 3000,
          source_type: 'manual_transfer',
          source_id: 'manual-transfer-20260727-000001',
          created_at: '2026-07-27T10:15:22.494Z',
        },
        {
          id: 'repayment-entry',
          event_kind: 'debt_repayment',
          paid_delta: 0,
          promotional_delta: 0,
          debt_delta: -500,
          charge_kind: 'operation',
          charge_policy: 'accepted_task_operation',
          sku_id: 'image.seedream.cover.v1',
          price_credits: 500,
          resource_type: 'topup',
          resource_id: 'topup-entry',
          operation_task_id: 'old-task-id',
          tool_call_id: 'image:old-cover',
          created_at: '2026-07-27T10:15:22.494Z',
        },
        {
          id: 'debt-entry',
          event_kind: 'debt_created',
          paid_delta: 0,
          promotional_delta: 0,
          debt_delta: 500,
          charge_kind: 'operation',
          charge_policy: 'accepted_task_operation',
          sku_id: 'image.seedream.cover.v1',
          price_credits: 500,
          charge_resource_type: 'image',
          charge_resource_id: 'cover-image-id',
          operation_task_id: 'old-task-id',
          tool_call_id: 'image:old-cover',
          created_at: '2026-07-26T10:15:22.494Z',
        },
      ],
    })

    render(<BillingPage />)

    expect(await screen.findByText('内容图生成费')).toBeInTheDocument()
    expect(screen.getByText('增值操作费')).toBeInTheDocument()
    expect(screen.getByText('固定价格 300 · 现金积分 -300')).toBeInTheDocument()
    expect(screen.getByText('操作 analysis:paid-content')).toBeInTheDocument()
    expect(screen.getByText('任务固定费')).toBeInTheDocument()
    expect(screen.getByText('封面图生成费转欠费')).toBeInTheDocument()
    expect(screen.getByText('补缴封面图欠费')).toBeInTheDocument()
    expect(screen.getByText('固定价格 500 · 现金积分 -500')).toBeInTheDocument()
    expect(screen.getByText('充值总额 100,000 · 现金到账 97,000 · 补缴欠费 3,000')).toBeInTheDocument()
    expect(screen.getByText('欠费减少 500 · 已包含在对应充值总额中')).toBeInTheDocument()
    expect(screen.queryByText('固定价扣费')).not.toBeInTheDocument()
  })

  it('shows tier price, list price, and discount on a paid charge', async () => {
    vi.mocked(api.billing.transactions).mockResolvedValueOnce({
      total: 1,
      offset: 0,
      limit: 20,
      items: [{
        id: 'tier-charge', event_kind: 'charge', paid_delta: -450, promotional_delta: 0, debt_delta: 0,
        charge_kind: 'operation', sku_id: 'image.content.v1', price_credits: 450,
        pricing_tier: 'pro', list_price_credits: 500, discount_credits: 50,
        created_at: '2026-07-27T11:03:30.725Z',
      }],
    })

    render(<BillingPage />)

    expect(await screen.findByText('专业版价格 450 · 标准价 500 · 优惠 50 · 现金积分 -450')).toBeInTheDocument()
  })
})
