import { describe, expect, it } from 'vitest'
import { formatCreditDescription } from './credit-display'
import type { CreditTransaction } from '@/types'

function tx(overrides: Partial<CreditTransaction>): CreditTransaction {
  return {
    id: 1,
    user_id: 'user-1',
    type: 'task_deduct',
    amount: -1000,
    balance_after: 9000,
    description: '',
    created_at: '2026-07-03T08:00:00Z',
    ...overrides,
  }
}

describe('formatCreditDescription', () => {
  it('keeps friendly server descriptions unchanged', () => {
    expect(formatCreditDescription(tx({ description: '生成种草笔记扣除积分1000' }))).toBe('生成种草笔记扣除积分1000')
  })

  it('normalizes legacy task deduction descriptions', () => {
    expect(formatCreditDescription(tx({
      type: 'task_deduct',
      amount: -1000,
      description: '任务扣费 (seednote) -1000',
    }))).toBe('生成种草笔记扣除积分1000')
  })

  it('normalizes legacy task consumption descriptions', () => {
    expect(formatCreditDescription(tx({
      type: 'task_deduct',
      amount: -128,
      description: '任务消耗 (article) -128',
    }))).toBe('生成公众号文章扣除积分128')
  })

  it('normalizes legacy moments task deduction descriptions', () => {
    expect(formatCreditDescription(tx({
      type: 'task_deduct',
      amount: -3000,
      description: '任务扣费 (moments) -3000',
    }))).toBe('生成朋友圈扣除积分3000')
  })

  it('normalizes legacy operation deduction descriptions', () => {
    expect(formatCreditDescription(tx({
      type: 'image_gen',
      amount: -80,
      description: '操作扣费 (image_gen) -80',
    }))).toBe('AI 生图扣除积分80')
  })
})
