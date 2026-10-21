import { describe, expect, it } from 'vitest'
import { portraitProgress, portraitStatus } from './portrait-library'
import { normalizeProjectProfile } from './project-profile'

describe('persisted portrait status', () => {
  it('recognizes legacy saved profiles independently of analysis initialization', () => {
    const profile = normalizeProjectProfile({ status: 'confirmed', initialization_status: 'not_started', dimensions: { identity: { content: { summary: '钢厂老板' } } } })!
    expect(portraitStatus(profile)).toBe('画像已保存')
    expect(portraitProgress(profile)).toEqual({ count: 1, percent: 17 })
  })
  it('does not count metadata or empty arrays as completed dimensions', () => {
    expect(portraitProgress({ dimensions: { identity: { content: { original_certainty: 'stated', notes: [] } }, audience: null } })).toEqual({ count: 0, percent: 0 })
  })
})
