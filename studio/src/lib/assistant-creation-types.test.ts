import { describe, expect, it } from 'vitest'
import { assistantCreationTypes, assistantPrimaryPlatform } from './assistant-creation-types'

describe('assistant creation capabilities', () => {
  it('respects explicit empty capabilities even with a legacy platform', () => {
    expect(assistantCreationTypes({ platform: 'wechat', agent_ids: [] })).toEqual([])
    expect(assistantCreationTypes({ platform: 'wechat' }).map(type => type.value)).toEqual(['wechat-article'])
  })

  it('offers every enabled homepage type across platforms, preferring the primary platform', () => {
    expect(assistantCreationTypes({ platform: 'wechat', agent_ids: ['wechat-picture', 'seednote'] }).map(type => type.value)).toEqual(['wechat-picture', 'seednote'])
    expect(assistantCreationTypes({ platform: 'seednote', agent_ids: ['wechat-article', 'seednote'] }).map(type => type.value)).toEqual(['seednote', 'wechat-article'])
    expect(assistantCreationTypes({ platform: 'wechat', agent_ids: ['seednote', 'montage', 'video-edit'] }).map(type => type.value)).toEqual(['seednote', 'video', 'video-edit'])
    expect(assistantCreationTypes({ platform: 'montage', agent_ids: ['montage'] }).map(type => type.value)).toEqual(['video'])
    expect(assistantCreationTypes({ platform: 'hypit', agent_ids: ['hypit'] })).toEqual([])
  })

  it('initializes a missing primary platform and retains compatible existing settings', () => {
    expect(assistantPrimaryPlatform('', ['wechat-picture'])).toBe('wechat')
    expect(assistantPrimaryPlatform('seednote', ['wechat-article', 'seednote'])).toBe('seednote')
    expect(assistantPrimaryPlatform('wechat', ['montage'])).toBe('montage')
    expect(assistantPrimaryPlatform('hypit', ['hypit'])).toBeUndefined()
  })
})
