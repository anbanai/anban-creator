import { describe, it, expect } from 'vitest'
import { statusBadgeVariant } from './labels'

describe('statusBadgeVariant', () => {
  it('returns "outline" for running', () => {
    expect(statusBadgeVariant('running')).toBe('outline')
  })

  it('returns "secondary" for completed', () => {
    expect(statusBadgeVariant('completed')).toBe('secondary')
  })

  it('returns "destructive" for failed', () => {
    expect(statusBadgeVariant('failed')).toBe('destructive')
  })

  it('returns "secondary" for pending', () => {
    expect(statusBadgeVariant('pending')).toBe('secondary')
  })

  it('returns "secondary" for cancelled', () => {
    expect(statusBadgeVariant('cancelled')).toBe('secondary')
  })

  it('returns "secondary" for unknown status', () => {
    expect(statusBadgeVariant('unknown')).toBe('secondary')
  })
})
