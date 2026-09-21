import { describe, expect, it } from 'vitest'
import { initialHypitInput, hypitReadinessError } from './hypit-form'

describe('video replication input', () => {
  it('inherits preferences without losing explicit follow-source duration', () => {
    expect(initialHypitInput('new brief', { preferences: { duration_seconds: 0 } }, { preferences: { duration_seconds: 30, aspect_ratio: '9:16' } }).preferences).toEqual({ duration_seconds: 0, aspect_ratio: '9:16' })
  })
  it('permits the supplemental asset limit plus reference while counting all bytes', () => {
    const limits = { max_duration_seconds: 180, max_assets: 20, max_asset_bytes: 100, max_input_bytes: 210 }
    const input = { brief: 'replicate', reference: { type: 'video' as const, url: '/ref.mp4', file_size: 10 }, source_assets: Array.from({ length: 20 }, () => ({ type: 'image' as const, url: '/product.png', file_size: 10 })) }
    expect(hypitReadinessError(input, limits)).toBe('')
    expect(hypitReadinessError({ ...input, source_assets: [...input.source_assets, input.source_assets[0]] }, limits)).toContain('20')
    expect(hypitReadinessError(input, { ...limits, max_input_bytes: 209 })).toContain('总大小')
  })
  it('requires a reference for fresh work and honors server limits', () => {
    const limits = { max_duration_seconds: 60, max_assets: 2, max_asset_bytes: 100, max_input_bytes: 200 }
    expect(hypitReadinessError({ brief: 'test' }, limits)).toContain('参考视频')
    expect(hypitReadinessError({ brief: 'test', reference: { type: 'video_url', url: 'https://example.com/a.mp4' }, preferences: { duration_seconds: 61 } }, limits)).toContain('60')
    expect(hypitReadinessError({ brief: 'remix' }, limits, true)).toBe('')
  })
})
