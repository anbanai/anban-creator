import { describe, it, expect } from 'vitest'
import { statusBadgeVariant } from './labels'

describe('statusBadgeVariant', () => {
  it('returns "warning" for running', () => {
    expect(statusBadgeVariant('running')).toBe('warning')
  })

  it('returns "success" for completed', () => {
    expect(statusBadgeVariant('completed')).toBe('success')
  })

  it('returns "danger" for failed', () => {
    expect(statusBadgeVariant('failed')).toBe('danger')
  })

  it('returns "neutral" for pending', () => {
    expect(statusBadgeVariant('pending')).toBe('neutral')
  })

  it('returns "neutral" for cancelled', () => {
    expect(statusBadgeVariant('cancelled')).toBe('neutral')
  })

  it('returns "neutral" for unknown status', () => {
    expect(statusBadgeVariant('unknown')).toBe('neutral')
  })
})
