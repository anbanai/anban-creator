import { describe, expect, it } from 'vitest'
import { normalizeProjectTypeSelection, primaryProjectPlatform, projectCreationTypes, profileLabAgents, supportsProfileLab } from './project-capabilities'
import type { Project } from '@/types'

describe('project creation capabilities', () => {
  it('offers ecommerce as a project type without adding it to Profile Lab agents', () => {
    expect(projectCreationTypes.some(({ id }) => id === 'ecommerce')).toBe(true)
    expect(profileLabAgents.map(({ id }) => id)).not.toContain('ecommerce')
  })

  it('keeps ecommerce mutually exclusive with other project types', () => {
    expect(normalizeProjectTypeSelection(['wechat-article'], ['wechat-article', 'ecommerce'])).toEqual(['ecommerce'])
    expect(normalizeProjectTypeSelection(['ecommerce'], ['ecommerce', 'seednote'])).toEqual(['seednote'])
    expect(normalizeProjectTypeSelection(['ecommerce'], [])).toEqual([])
    expect(normalizeProjectTypeSelection(['wechat-article'], ['wechat-article', 'seednote'])).toEqual(['wechat-article', 'seednote'])
  })

  it('keeps ecommerce projects outside Profile Lab support', () => {
    expect(supportsProfileLab({ platform: 'ecommerce', agent_ids: ['ecommerce'] } as Project)).toBe(false)
    expect(supportsProfileLab({ platform: 'wechat', agent_ids: ['wechat-article', 'ecommerce'] } as Project)).toBe(false)
    expect(supportsProfileLab({ platform: 'wechat', agent_ids: ['wechat-article'] } as Project)).toBe(true)
  })

  it('derives the primary platform for selected project types, including local life', () => {
    expect(primaryProjectPlatform(['seednote'])).toBe('seednote')
    expect(primaryProjectPlatform(['local-life'])).toBe('local-life')
    expect(primaryProjectPlatform(['ecommerce'])).toBe('ecommerce')
  })
})
