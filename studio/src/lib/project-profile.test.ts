import { describe, expect, it } from 'vitest'
import { normalizeProfileDimension, normalizeProjectProfile } from './project-profile'

describe('project profile normalization', () => {
  it('fills all six dimensions and replaces null fields with safe empty values', () => {
    const profile = normalizeProjectProfile({
      status: 'confirmed',
      initialization_status: 'ready',
      version: 7,
      dimensions: {
        identity: { content: null, sources: null, evidence: {}, missing_fields: null },
        style: { content: ['invalid'], sources: ['sample'], evidence: ['quote'], missing_fields: ['audience'] },
      },
      analysis_limits: null,
      follow_up_questions: null,
    })

    expect(profile?.dimensions.identity).toEqual({ content: {}, sources: [], evidence: [], missing_fields: [] })
    expect(profile?.dimensions.style).toEqual({ content: {}, sources: ['sample'], evidence: ['quote'], missing_fields: ['audience'] })
    expect(Object.keys(profile?.dimensions ?? {})).toEqual(['identity', 'style', 'audience', 'platforms', 'preferences', 'memory'])
    expect(profile?.analysis_limits).toEqual([])
    expect(profile?.follow_up_questions).toEqual([])
  })

  it('normalizes a standalone dimension and rejects a non-object profile', () => {
    expect(normalizeProfileDimension({ content: null, sources: null, evidence: null, missing_fields: null })).toEqual({
      content: {}, sources: [], evidence: [], missing_fields: [],
    })
    expect(normalizeProjectProfile(null)).toBeNull()
  })
})
