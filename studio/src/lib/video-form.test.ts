import { describe, expect, it } from 'vitest'
import type { VideoDefaults, VideoTaskConfig } from '@/types'
import {
  VIDEO_PROJECT_DEFAULT_VALUE,
  buildVideoFormConfig,
  normalizeVideoConfigForSubmit,
  readVideoSelectValue,
  writeVideoSelectValue,
} from './video-form'

describe('video form helpers', () => {
  it('uses a stable select value for project defaults and clears it before submit', () => {
    expect(readVideoSelectValue(undefined)).toBe(VIDEO_PROJECT_DEFAULT_VALUE)
    expect(writeVideoSelectValue(VIDEO_PROJECT_DEFAULT_VALUE)).toBeUndefined()
    expect(writeVideoSelectValue('seedance-2.0-mini')).toBe('seedance-2.0-mini')
  })

  it('seeds form values from project defaults with references ready for editing', () => {
    const watermark = false
    const defaults: VideoDefaults = {
      purpose: 'planting',
      model_key: 'seedance-2.0-mini',
      resolution: '720p',
      ratio: '9:16',
      duration: 10,
      watermark,
      preflight: true,
    }

    expect(buildVideoFormConfig(defaults)).toEqual({
      ...defaults,
      workflow: 'creator',
      references: [],
    })
  })

  it('submits creator workflow by default for video generation', () => {
    expect(normalizeVideoConfigForSubmit(buildVideoFormConfig())).toEqual({
      workflow: 'creator',
    })
  })

  it('keeps explicit false values and references while dropping default select sentinels', () => {
    const config: VideoTaskConfig = {
      workflow: 'editor',
      purpose: 'promotion',
      model_key: VIDEO_PROJECT_DEFAULT_VALUE,
      resolution: '',
      ratio: '16:9',
      duration: 0,
      watermark: false,
      preflight: true,
      references: [
        { type: 'text', text: '镜头要明亮' },
        { type: 'image_url', url: '' },
      ],
    }

    expect(normalizeVideoConfigForSubmit(config)).toEqual({
      workflow: 'editor',
      purpose: 'promotion',
      ratio: '16:9',
      watermark: false,
      preflight: true,
      references: [{ type: 'text', text: '镜头要明亮' }],
    })
  })

  it('does not submit inherited fallback duration as an explicit user duration', () => {
    const defaults: VideoDefaults = {
      purpose: 'planting',
      model_key: 'seedance-2.0-mini',
      resolution: '720p',
      ratio: '9:16',
      duration: 15,
      watermark: false,
      preflight: true,
    }

    expect(normalizeVideoConfigForSubmit(buildVideoFormConfig(defaults), defaults)).toEqual({
      workflow: 'creator',
    })
    expect(normalizeVideoConfigForSubmit({ ...buildVideoFormConfig(defaults), duration: 60 }, defaults)).toEqual({
      workflow: 'creator',
      duration: 60,
    })
  })
})
