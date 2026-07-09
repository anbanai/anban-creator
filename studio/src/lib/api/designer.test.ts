import { describe, expect, it } from 'vitest'

import { normalizeProvider } from './designer'
import type { RawDesignerProvider } from '@/types/designer'

describe('designer API normalization', () => {
  it('maps provider capabilities without model-specific size inference', () => {
    const provider = normalizeProvider({
      id: 'gpt_image_2',
      name: 'GPT Image 2',
      provider: 'wangcai_openai',
      model: 'gpt-image-2',
      credits: 0,
      enabled: true,
      idx: 0,
      capabilities: {
        quality_levels: ['auto', 'low', 'medium', 'high'],
        size_presets: ['1024x1024', '1536x1024', '1024x1536'],
        default_size: '1024x1024',
        max_batch: 10,
        max_reference_images: 16,
        supports_reference: true,
        supports_mask: true,
        output_formats: ['png', 'jpeg', 'webp'],
        has_background: true,
        has_compression: true,
      },
    } satisfies RawDesignerProvider)

    expect(provider.capabilities.defaultSize).toBe('1024x1024')
    expect(provider.capabilities.sizePresets).toEqual([
      '1024x1024',
      '1536x1024',
      '1024x1536',
    ])
  })
})
