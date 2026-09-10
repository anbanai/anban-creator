import { describe, expect, it } from 'vitest'
import { buildMontageInputForSubmit, initialMontageInput } from './montage-form'

describe('montage form helpers', () => {
  it('creates stable defaults', () => {
    expect(initialMontageInput('新品短片')).toMatchObject({
      brief: '新品短片',
      pipeline_key: '',
      source_assets: [],
      preferences: {
        duration_seconds: 30,
      },
    })
  })

  it('initializes execution input from project defaults', () => {
    expect(initialMontageInput('', undefined, {
      default_pipeline: 'social-short',
      preferences: {
        duration_seconds: 45,
        style: 'clean',
        music_prompt: 'minimal electronic',
        subtitle_mode: 'burned-in',
        voiceover_mode: 'narrated',
      },
      delivery_targets: ['final_video', 'subtitles'],
    })).toMatchObject({
      pipeline_key: 'social-short',
      source_assets: [],
      preferences: {
        duration_seconds: 45,
        style: 'clean',
        music_prompt: 'minimal electronic',
        subtitle_mode: 'burned-in',
        voiceover_mode: 'narrated',
      },
      delivery_targets: ['final_video', 'subtitles'],
    })
  })

  it('keeps explicit execution fields ahead of project defaults', () => {
    const result = initialMontageInput('brief', {
      pipeline_key: 'manual',
      preferences: {
        duration_seconds: 15,
        style: '',
      },
      delivery_targets: [],
    }, {
      default_pipeline: 'project',
      preferences: {
        duration_seconds: 45,
        style: 'project style',
        music_prompt: 'project music',
      },
      delivery_targets: ['final_video'],
    })

    expect(result.pipeline_key).toBe('manual')
    expect(result.preferences).not.toHaveProperty('aspect_ratio')
    expect(result.preferences?.duration_seconds).toBe(15)
    expect(result.preferences?.style).toBe('')
    expect(result.preferences?.music_prompt).toBe('project music')
    expect(result.delivery_targets).toEqual([])
  })

  it('trims submit fields without adding execution target', () => {
    const result = buildMontageInputForSubmit('', {
      brief: '  新品短片  ',
      pipeline_key: '  default  ',
      source_assets: [],
      preferences: {
        style: '  干净高级  ',
      },
    })

    expect(result.brief).toBe('新品短片')
    expect(result.pipeline_key).toBe('default')
    expect(result.preferences?.style).toBe('干净高级')
    expect(result).not.toHaveProperty('execution_target')
  })
})
