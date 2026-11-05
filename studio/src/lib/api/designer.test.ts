import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { designerApi, normalizeCapability } from './designer'
import { http } from '@/lib/http-client'
import { AxiosError } from 'axios'
import { clearActiveGeneration, loadActiveGeneration, saveActiveGeneration } from '@/lib/designer-session'
import type { GenerateRequest } from '@/types/designer'

describe('designer API normalization', () => {
  beforeEach(() => sessionStorage.clear())
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('preserves the server default capability independently from list order', async () => {
    vi.spyOn(http, 'get').mockResolvedValue({
      data: { code: 0, msg: 'success', data: {
        default_capability: 'standard',
        tier: 'enterprise',
        items: [
          { key: 'professional', display_name: '专业增强' },
          { key: 'standard', display_name: '标准图像' },
        ],
      } },
    })
    const { designerApi } = await import('./designer')

    await expect(designerApi.getCapabilities()).resolves.toMatchObject({
      defaultCapability: 'standard',
      items: [{ id: 'professional' }, { id: 'standard' }],
    })
  })

  it('maps public capability metadata without internal provider identity', () => {
    const capability = normalizeCapability({
      key: 'professional',
      display_name: '专业增强',
      description: '适合复杂构图与高细节视觉任务',
      min_tier: 'enterprise',
      price_credits: 500,
      price_available: true,
      enabled: true,
      sort_order: 2,
      generation_features: {
        quality_levels: ['auto', 'low', 'medium', 'high'],
        size_presets: ['1:1:2K', '3:4:2K', '4:3:2K'],
        default_size: '1:1:2K',
        max_batch: 1,
        max_reference_images: 16,
        supports_reference: true,
        supports_mask: true,
        output_formats: ['png', 'jpeg', 'webp'],
        has_background: true,
        has_compression: true,
        watermark: false,
      },
    })

    expect(capability.designerFeatures.defaultSize).toBe('1:1:2K')
    expect(capability.designerFeatures.sizePresets).toEqual([
      '1:1:2K',
      '3:4:2K',
      '4:3:2K',
    ])
    expect(capability).not.toHaveProperty('provider')
    expect(capability).not.toHaveProperty('providerKey')
    expect(capability).not.toHaveProperty('route')
    expect(capability).not.toHaveProperty('model')
    expect(capability).not.toHaveProperty('features')
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

    await designerApi.generate({
      project_id: 'default', prompt: 'test', capability_key: 'professional',
      size: '1:1:2K', quality: 'auto', output_format: 'png', n: 1,
    }, controller.signal)
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

    await designerApi.generate({
      project_id: 'default', prompt: 'test', capability_key: 'professional',
      size: '1:1:2K', quality: 'auto', output_format: 'png', n: 1,
    })

    const quoteRequest = post.mock.calls[0]?.[1]
    const generateRequest = post.mock.calls[1]?.[1]
    expect(quoteRequest).not.toHaveProperty('execution_profile')
    expect(generateRequest).not.toHaveProperty('execution_profile')
  })

  it('quotes and submits each requested image independently at its single-image price', async () => {
    const post = vi.spyOn(http, 'post').mockImplementation(async (path, data) => {
      const request = data as GenerateRequest
      return { data: { data: path === '/designer/quote'
        ? { quote_id: `quote-${request.operation_id}`, request_fingerprint: request.operation_id }
        : { generation_id: request.operation_id, status: 'generating', price_credits: 500 } } }
    })

    await designerApi.generate({
      project_id: 'default',
      prompt: '生成三张海报',
      capability_key: 'professional',
      size: '1:1:2K',
      quality: 'high',
      output_format: 'webp',
      n: 3,
    })

    expect(post.mock.calls.filter(([path]) => path === '/designer/quote')).toHaveLength(3)
    const submitted = post.mock.calls.filter(([path]) => path === '/designer/generate').map(([, data]) => data as GenerateRequest)
    expect(submitted).toHaveLength(3)
    expect(new Set(submitted.map((data) => data.operation_id)).size).toBe(3)
    for (const data of submitted) expect(data).toMatchObject({ n: 1, quote_id: `quote-${data.operation_id}`, request_fingerprint: data.operation_id })
  })

  it.each([0, 5, 1.5])('rejects invalid image count %s before billing', async (n) => {
    const post = vi.spyOn(http, 'post')
    await expect(designerApi.generate({ project_id: '', prompt: 'image', capability_key: 'standard', size: '1:1:2K', quality: 'auto', output_format: 'png', n })).rejects.toThrow()
    expect(post).not.toHaveBeenCalled()
  })

  it('recovers a mixed batch from saved operation identities and only returns successful images', async () => {
    const ids: string[] = []
    vi.spyOn(http, 'post').mockImplementation(async (path, data) => {
      if (path === '/designer/quote') return { data: { data: { quote_id: 'quote', request_fingerprint: 'fingerprint' } } }
      const request = data as GenerateRequest
      ids.push(request.operation_id!)
      return { data: { data: { generation_id: request.operation_id, price_credits: 500, status: 'generating' } } }
    })
    const request = { operation_id: 'batch-root', project_id: '', prompt: 'image', capability_key: 'standard', size: '1:1:2K', quality: 'auto', output_format: 'png', n: 3 }
    const accepted = await designerApi.generate(request)
    vi.spyOn(http, 'get').mockImplementation(async (path) => {
      const id = String(path).split('/').pop()!
      const failed = id === ids[1]
      return { data: { data: { id, project_id: '', prompt: 'image', n: 1, status: failed ? 'failed' : 'completed', final_cost: failed ? 0 : 500, results: failed ? [] : [{ id: 1, generation_id: id, image_url: `/images/${id}.png`, index: 0 }], created_at: '', updated_at: '' } } }
    })
    const reloadedApi = (await import('./designer')).designerApi
    const result = await reloadedApi.getGeneration(accepted.generation_id)
    expect(result).toMatchObject({ status: 'completed', n: 3, final_cost: 1000 })
    expect(result.results).toHaveLength(2)
    expect(result.results?.map((image) => image.index)).toEqual([0, 1])
    await designerApi.generate(request)
    expect(ids.slice(3)).toEqual(ids.slice(0, 3))
  })

  it('retains uncertain admission even when another image is definitively rejected', async () => {
    const rejected = new AxiosError('rejected', 'ERR_BAD_REQUEST', undefined, undefined, { status: 402, data: {}, statusText: 'Payment Required', headers: {}, config: {} as never })
    vi.spyOn(http, 'post').mockImplementation(async (path, data) => {
      if (path === '/designer/quote') return { data: { data: { quote_id: 'q', request_fingerprint: 'f' } } }
      if ((data as GenerateRequest).operation_id === 'mixed-admission') throw rejected
      throw new Error('response lost')
    })
    await expect(designerApi.generate({ operation_id: 'mixed-admission', project_id: '', prompt: 'image', capability_key: 'standard', size: '1:1:2K', quality: 'auto', output_format: 'png', n: 2 }, undefined, (id) => saveActiveGeneration('alice', id))).rejects.toThrow('response lost')
    expect(loadActiveGeneration('alice')?.generationId).toBe('mixed-admission')
    const get = vi.spyOn(http, 'get').mockResolvedValue({ data: { data: { id: 'accepted-child', project_id: '', prompt: 'image', n: 1, status: 'completed', results: [{ id: 1, generation_id: 'accepted-child', image_url: '/image.png', index: 0 }], created_at: '', updated_at: '' } } })
    expect((await designerApi.getGeneration('mixed-admission')).results).toHaveLength(1)
    expect(get).toHaveBeenCalledTimes(1)
    clearActiveGeneration('alice')
    expect(sessionStorage.getItem('designer_batch:mixed-admission')).toBeNull()
  })
})

it('reuses the supplied operation ID through an ambiguous generation retry', async () => {
  const post = vi.spyOn(http, 'post').mockImplementation(async (path) => {
    if (path === '/designer/quote') return { data: { code: 0, data: { quote_id: 'q', request_fingerprint: 'f' } } }
    throw new Error('network response lost')
  })
  const request = { operation_id: 'stable-operation', project_id: 'default', prompt: 'image', capability_key: 'professional', size: '1:1:2K', quality: 'auto', output_format: 'png', n: 1 }
  await expect(designerApi.generate(request)).rejects.toThrow()
  await expect(designerApi.generate(request)).rejects.toThrow()
  expect(post.mock.calls.map(([, payload]) => (payload as { operation_id: string }).operation_id)).toEqual(Array(4).fill('stable-operation'))
  vi.restoreAllMocks()
})

it('does not dispatch generation when stopped while a quote response is pending', async () => {
  const controller = new AbortController()
  let resolveQuote!: (value: unknown) => void
  const quote = new Promise((resolve) => { resolveQuote = resolve })
  const post = vi.spyOn(http, 'post').mockReturnValue(quote as never)
  const pending = designerApi.generate({ operation_id: 'pending-quote', project_id: 'default', prompt: 'image', capability_key: 'professional', size: '1:1:2K', quality: 'auto', output_format: 'png', n: 1 }, controller.signal)
  controller.abort()
  resolveQuote({ data: { code: 0, data: { quote_id: 'q', request_fingerprint: 'f' } } })
  await expect(pending).rejects.toThrow()
  expect(post).toHaveBeenCalledTimes(1)
  vi.restoreAllMocks()
})
