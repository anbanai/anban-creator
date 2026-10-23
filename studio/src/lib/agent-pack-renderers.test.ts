import { describe, expect, it } from 'vitest'

import { registeredAgentPackForm } from './agent-pack-renderers'

describe('registeredAgentPackForm', () => {
  it('registers every existing typed business form by key', () => {
    for (const key of ['wechat-article', 'seednote', 'moments', 'ecommerce', 'montage', 'hypit', 'video-edit']) {
      expect(registeredAgentPackForm(`custom:${key}`)).toBe(key)
    }
  })

  it('does not silently accept an unknown custom renderer', () => {
    expect(registeredAgentPackForm('custom:unknown')).toBeNull()
  })
})
