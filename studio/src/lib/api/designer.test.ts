import { afterEach, describe, expect, it, vi } from 'vitest'

import { normalizeProvider } from './designer'
import { http } from '@/lib/http-client'
import type { RawDesignerProvider } from '@/types/designer'

describe('designer API normalization', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

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

  it('registers an immutable direct upload identity without multipart data', async () => {
    const post = vi.spyOn(http, 'post').mockResolvedValue({
      data: {
        code: 0,
        msg: 'success',
        data: { file_id: 'reference-1', filename: 'reference.png', size: 42 },
      },
    })

    const { designerApi } = await import('./designer')
    const result = await designerApi.registerReference({
      upload_id: 'upload-1',
      key: 'uploads/finalized/user-1/upload-1/reference.png',
    })

    expect(post).toHaveBeenCalledWith('/designer/register-reference', {
      upload_id: 'upload-1',
      key: 'uploads/finalized/user-1/upload-1/reference.png',
    })
    expect(result.file_id).toBe('reference-1')
  })

  it('passes cancellation signals to generation, registration, source import, and polling requests', async () => {
    const post = vi.spyOn(http, 'post').mockResolvedValue({
      data: { code: 0, msg: 'success', data: {} },
    })
    const get = vi.spyOn(http, 'get').mockResolvedValue({
      data: { code: 0, msg: 'success', data: {} },
    })
    const controller = new AbortController()
    const { designerApi } = await import('./designer')

    await designerApi.generate({ project_id: 'default', prompt: 'test', provider: 'openai' }, controller.signal)
    await designerApi.registerReference({ upload_id: 'upload-1', key: 'uploads/finalized/reference.png' }, controller.signal)
    await designerApi.uploadReferenceFromUrl('https://example.com/source.png', controller.signal)
    await designerApi.getGeneration('generation-1', controller.signal)

    expect(post).toHaveBeenNthCalledWith(1, '/designer/quote', expect.any(Object), { signal: controller.signal })
    expect(post).toHaveBeenNthCalledWith(2, '/designer/generate', expect.any(Object), { signal: controller.signal })
    expect(post).toHaveBeenNthCalledWith(3, '/designer/register-reference', expect.any(Object), { signal: controller.signal })
    expect(post).toHaveBeenNthCalledWith(4, '/designer/upload-reference-from-url', expect.any(Object), { signal: controller.signal })
    expect(get).toHaveBeenCalledWith('/designer/generations/generation-1', { signal: controller.signal })
  })

  it('keeps Agent execution profiles out of quote and generation requests', async () => {
    const post = vi.spyOn(http, 'post')
      .mockResolvedValueOnce({
        data: {
          code: 0,
          msg: 'success',
          data: { quote_id: 'quote-1', request_fingerprint: 'fingerprint-1' },
        },
      })
      .mockResolvedValueOnce({
        data: {
          code: 0,
          msg: 'success',
          data: { generation_id: 'generation-1', status: 'generating', price_credits: 500 },
        },
      })
    const { designerApi } = await import('./designer')

    await designerApi.generate({ project_id: 'default', prompt: 'test', provider: 'openai' })

    const quoteRequest = post.mock.calls[0]?.[1]
    const generateRequest = post.mock.calls[1]?.[1]
    expect(quoteRequest).not.toHaveProperty('execution_profile')
    expect(generateRequest).not.toHaveProperty('execution_profile')
  })
})
