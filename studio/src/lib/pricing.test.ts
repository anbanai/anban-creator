import { describe, expect, it } from 'vitest'

import { cheapestAvailableExecutionProfile, taskCostFor } from './pricing'
import type { BillingCatalog } from '@/types'

describe('taskCostFor', () => {
  it('does not invent a local price when the active catalog is unavailable', () => {
    expect(taskCostFor(undefined, 'moments')).toBeUndefined()
  })

  it('matches task admission prices by execution profile', () => {
    const catalog = {
      catalog_id: 'retail-profile-v1',
      currency: 'credits' as const,
      skus: [
        { id: 'article-cost', operation: 'task.article', execution_profile: 'effective', charge_policy: 'task_admission' as const, price_credits: 4800, delivery: 'article' },
        { id: 'article-balanced', operation: 'task.article', execution_profile: 'balanced', charge_policy: 'task_admission' as const, price_credits: 6000, delivery: 'article' },
      ],
    } satisfies BillingCatalog

    expect(taskCostFor(catalog, 'article', 'effective')).toBe(4800)
    expect(taskCostFor(catalog, 'article', 'balanced')).toBe(6000)
    expect(taskCostFor(catalog, 'article', 'quality')).toBeUndefined()
  })

  it('chooses the cheapest available profile with an exact task SKU', () => {
    const catalog = {
      catalog_id: 'retail-profile-v1',
      currency: 'credits' as const,
      skus: [
        { id: 'article-cost', operation: 'task.article', execution_profile: 'effective' as const, charge_policy: 'task_admission' as const, price_credits: 4800, delivery: 'article' },
        { id: 'article-balanced', operation: 'task.article', execution_profile: 'balanced' as const, charge_policy: 'task_admission' as const, price_credits: 4000, delivery: 'article' },
      ],
    }
    const profiles = [
      { id: 'effective' as const, display_name: '性价比', provider: 'provider-a', model_name: 'a', description: '', min_tier: 'free' as const, available: true },
      { id: 'balanced' as const, display_name: '平衡型', provider: 'provider-b', model_name: 'b', description: '', min_tier: 'pro' as const, available: true },
      { id: 'quality' as const, display_name: '极致效果', provider: 'provider-c', model_name: 'c', description: '', min_tier: 'enterprise' as const, available: true },
    ]

    expect(cheapestAvailableExecutionProfile(profiles, catalog, 'article')).toBe('balanced')
    expect(cheapestAvailableExecutionProfile(
      profiles.map((profile) => profile.id === 'balanced' ? { ...profile, available: false } : profile),
      catalog,
      'article',
    )).toBe('effective')
  })
})
