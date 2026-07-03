import { describe, expect, it } from 'vitest'
import { videoModelDisplayName } from './video-display'

describe('video display helpers', () => {
  it('uses configured display_name before any fallback', () => {
    expect(videoModelDisplayName({ key: 'seedance-2.0-mini', display_name: '团队配置模型' })).toBe('团队配置模型')
  })

  it('uses Chinese fallback labels for known model keys', () => {
    expect(videoModelDisplayName({ key: 'seedance-2.0-mini' })).toBe('豆包 Seedance 2.0 Mini（轻量）')
    expect(videoModelDisplayName({ key: 'seedance-2.0-fast' })).toBe('豆包 Seedance 2.0 Fast（快速）')
    expect(videoModelDisplayName({ key: 'seedance-2.0' })).toBe('豆包 Seedance 2.0（标准）')
  })

  it('falls back to the raw key for unknown configured models', () => {
    expect(videoModelDisplayName({ key: 'custom-video-model' })).toBe('custom-video-model')
  })
})
