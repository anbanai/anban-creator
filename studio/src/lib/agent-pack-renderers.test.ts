import { describe, expect, it } from 'vitest'

import { registeredAgentPackForm } from './agent-pack-renderers'

describe('registeredAgentPackForm', () => {
  it('registers every existing typed business form by key', () => {
    for (const key of ['article', 'seednote', 'moments', 'ecommerce', 'montage', 'hypit']) {
      expect(registeredAgentPackForm(`custom:${key}`)).toBe(key)
    }
  })

  it('does not silently accept an unknown custom renderer', () => {
    expect(registeredAgentPackForm('custom:unknown')).toBeNull()
  })
})
