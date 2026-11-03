import { beforeEach, describe, expect, it } from 'vitest'
import { clearActiveGeneration, loadActiveGeneration, saveActiveGeneration } from './designer-session'

describe('Designer recovery', () => {
  beforeEach(() => sessionStorage.clear())
  it('isolates accepted jobs by user and retains long-running jobs', () => {
    sessionStorage.setItem('designer_active_generation:alice', JSON.stringify({ generationId: 'long-job', startedAt: Date.now() - 24 * 60 * 60 * 1000 }))
    expect(loadActiveGeneration('alice')?.generationId).toBe('long-job')
    expect(loadActiveGeneration('bob')).toBeNull()
    saveActiveGeneration('bob', 'other-job')
    clearActiveGeneration('bob')
    expect(loadActiveGeneration('bob')).toBeNull()
    expect(loadActiveGeneration('alice')?.generationId).toBe('long-job')
  })
  it('ignores legacy unscoped recovery and malformed scoped state', () => {
    sessionStorage.setItem('designer_active_generation', JSON.stringify({ generationId: 'another-account' }))
    expect(loadActiveGeneration('alice')).toBeNull()
    sessionStorage.setItem('designer_active_generation:alice', '{')
    expect(loadActiveGeneration('alice')).toBeNull()
  })
})
