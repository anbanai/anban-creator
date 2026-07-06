import { describe, expect, it } from 'vitest'
import type { VideoDefaults, VideoTaskConfig } from '@/types'
import {
  VIDEO_PROJECT_DEFAULT_VALUE,
  buildVideoFormConfig,
  defaultVideoPurposeForCreativeType,
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
      creative_type: 'personal_ip',
      production_mode: 'guided',
      workflow: 'creator',
      references: [],
    })
  })

  it('submits creator workflow by default for video generation', () => {
    expect(normalizeVideoConfigForSubmit(buildVideoFormConfig())).toEqual({
      workflow: 'creator',
      production_mode: 'guided',
      creative_type: 'personal_ip',
      purpose: 'planting',
    })
  })

  it('defaults high-efficiency jokes to promotion purpose', () => {
    expect(defaultVideoPurposeForCreativeType('high_efficiency_joke')).toBe('promotion')
    expect(normalizeVideoConfigForSubmit({ creative_type: 'high_efficiency_joke' })).toEqual({
      workflow: 'creator',
      creative_type: 'high_efficiency_joke',
      purpose: 'promotion',
    })
  })

  it('keeps explicit false values and references while dropping default select sentinels', () => {
    const config: VideoTaskConfig = {
      workflow: 'editor',
      scenario_key: 'live_selling',
      production_mode: 'guided',
      purpose: 'promotion',
      model_key: VIDEO_PROJECT_DEFAULT_VALUE,
      resolution: '',
      ratio: '16:9',
      duration: 0,
      watermark: false,
      preflight: true,
      retake_budget: 4,
      delivery_targets: ['vertical_9x16', 'textless_master'],
      references: [
        {
          type: 'text',
          text: '镜头要明亮',
          must_keep: ['杯身'],
          can_change: ['背景'],
          must_not_transfer: ['参考人物'],
        },
        { type: 'image_url', url: '' },
      ],
    }

    expect(normalizeVideoConfigForSubmit(config)).toEqual({
      workflow: 'editor',
      scenario_key: 'live_selling',
      production_mode: 'guided',
      creative_type: 'personal_ip',
      purpose: 'promotion',
      ratio: '16:9',
      watermark: false,
      preflight: true,
      retake_budget: 4,
      delivery_targets: ['vertical_9x16', 'textless_master'],
      references: [{
        type: 'text',
        text: '镜头要明亮',
        must_keep: ['杯身'],
        can_change: ['背景'],
        must_not_transfer: ['参考人物'],
      }],
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
      production_mode: 'guided',
      creative_type: 'personal_ip',
    })
    expect(normalizeVideoConfigForSubmit({ ...buildVideoFormConfig(defaults), duration: 60 }, defaults)).toEqual({
      workflow: 'creator',
      production_mode: 'guided',
      creative_type: 'personal_ip',
      duration: 60,
    })
  })
})
