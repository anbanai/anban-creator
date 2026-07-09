import { describe, expect, it } from 'vitest'
import { buildMontageInputForSubmit, initialMontageInput } from './montage-form'

describe('montage form helpers', () => {
  it('creates stable defaults', () => {
    expect(initialMontageInput('新品短片')).toMatchObject({
      brief: '新品短片',
      pipeline_key: '',
      source_assets: [],
      preferences: {
        aspect_ratio: '9:16',
        duration_seconds: 30,
      },
    })
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
