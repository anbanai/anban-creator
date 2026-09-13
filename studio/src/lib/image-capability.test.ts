import { describe, expect, it } from 'vitest'

import type { ImageCapabilityOption } from '@/types'
import { explicitlyRejectsReferenceImages } from './image-capability'

function capability(
  overrides: Partial<NonNullable<ImageCapabilityOption['generation_features']>>,
): ImageCapabilityOption {
  return {
    key: 'test',
    display_name: 'Test',
    generation_features: {
      quality_levels: [],
      size_presets: [],
      default_size: '1:1',
      max_batch: 1,
      max_reference_images: 1,
      supports_reference: true,
      supports_mask: false,
      output_formats: ['png'],
      has_background: false,
      has_compression: false,
      watermark: false,
      ...overrides,
    },
  }
}

describe('explicitlyRejectsReferenceImages', () => {
  it('rejects an explicit unsupported flag or a zero reference limit', () => {
    expect(explicitlyRejectsReferenceImages(capability({ supports_reference: false }))).toBe(true)
    expect(explicitlyRejectsReferenceImages(capability({ max_reference_images: 0 }))).toBe(true)
  })

  it('allows supported and unspecified capability metadata', () => {
    expect(explicitlyRejectsReferenceImages(capability({}))).toBe(false)
    expect(explicitlyRejectsReferenceImages()).toBe(false)
  })
})
