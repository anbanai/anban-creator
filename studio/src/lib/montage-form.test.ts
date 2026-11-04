import { describe, expect, it } from 'vitest'
import { buildMontageInputForSubmit, initialMontageInput } from './montage-form'

describe('montage form helpers', () => {
  it('creates stable defaults', () => {
    expect(initialMontageInput('新品短片')).toMatchObject({
      brief: '新品短片',
      pipeline_key: '',
      source_assets: [],
      preferences: {
        duration_seconds: undefined,
      },
    })
  })

  it('initializes execution input from project defaults', () => {
    expect(initialMontageInput('', undefined, {
      default_pipeline: 'social-short',
      preferences: {
        duration_seconds: 45,
        playbook_key: 'clean-professional',
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
        playbook_key: 'clean-professional',
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
        playbook_key: '',
      },
      delivery_targets: [],
    }, {
      default_pipeline: 'project',
      preferences: {
        duration_seconds: 45,
        playbook_key: 'minimalist-diagram',
        music_prompt: 'project music',
      },
      delivery_targets: ['final_video'],
    })

    expect(result.pipeline_key).toBe('manual')
    expect(result.preferences).not.toHaveProperty('aspect_ratio')
    expect(result.preferences?.duration_seconds).toBe(15)
    expect(result.preferences?.playbook_key).toBe('')
    expect(result.preferences?.music_prompt).toBe('project music')
    expect(result.delivery_targets).toEqual([])
  })

  it('trims submit fields without adding execution target', () => {
    const result = buildMontageInputForSubmit('', {
      brief: '  新品短片  ',
      pipeline_key: '  default  ',
      source_assets: [],
      preferences: {
        playbook_key: '  clean-professional  ',
      },
    })

    expect(result.brief).toBe('新品短片')
    expect(result.pipeline_key).toBe('default')
    expect(result.preferences?.playbook_key).toBe('clean-professional')
    expect(result).not.toHaveProperty('execution_target')
  })

  it('inherits the playbook and output profile while allowing explicit auto selection', () => {
    const result = initialMontageInput('', undefined, {
      default_pipeline: 'social-short',
      output_profile_key: 'tiktok',
      preferences: { playbook_key: 'brand-story' },
    })

    expect(result.output_profile_key).toBe('tiktok')
    expect(result.preferences?.playbook_key).toBe('brand-story')
    expect(initialMontageInput('', { preferences: { playbook_key: 'auto' } }, {
      preferences: { playbook_key: 'brand-story' },
    }).preferences?.playbook_key).toBe('auto')
  })

  it('submits profile and playbook keys but omits the resolved server profile object', () => {
    const result = buildMontageInputForSubmit('', {
      output_profile_key: 'tiktok',
      output_profile: { key: 'tiktok', display_name: 'TikTok', width: 1080, height: 1920, aspect_ratio: '9:16' },
      preferences: { playbook_key: '  brand-story  ' },
    })

    expect(result.output_profile_key).toBe('tiktok')
    expect(result.preferences?.playbook_key).toBe('brand-story')
    expect(result).not.toHaveProperty('output_profile')
  })
})
