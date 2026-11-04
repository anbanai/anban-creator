import { http, unwrap } from '@/lib/http-client'
import { isAxiosError } from 'axios'
import { loadDesignerBatch, saveDesignerBatch } from '@/lib/designer-session'
import type { ImageCapabilityListResponse, ImageCapabilityOption } from '@/types'
import type { DesignerCapability, GenerateQuote, GenerateRequest, HistoryResponse, ImageGeneration } from '@/types/designer'

export function normalizeCapability(raw: ImageCapabilityOption): DesignerCapability {
  const designerFeatures = raw.generation_features ?? {
    quality_levels: [], size_presets: [], default_size: 'auto', max_batch: 1,
    max_reference_images: 0, supports_reference: false, supports_mask: false,
    output_formats: ['png'], has_background: false, has_compression: false, watermark: false,
  }
  return {
    id: raw.key,
    name: raw.display_name,
    description: raw.description,
    minTier: raw.min_tier,
    credits: raw.price_credits ?? 0,
    priceAvailable: raw.price_available,
    enabled: raw.enabled !== false,
    idx: raw.sort_order ?? 0,
    designerFeatures: {
      qualityLevels: designerFeatures.quality_levels ?? [],
      sizePresets: designerFeatures.size_presets ?? [],
      defaultSize: designerFeatures.default_size ?? 'auto',
      maxBatch: 1,
      maxReferenceImages: designerFeatures.max_reference_images ?? 0,
      supportsReference: designerFeatures.supports_reference ?? false,
      supportsMask: designerFeatures.supports_mask ?? false,
      outputFormats: designerFeatures.output_formats ?? ['png'],
      hasBackground: designerFeatures.has_background ?? false,
      hasCompression: designerFeatures.has_compression ?? false,
      watermark: designerFeatures.watermark ?? false,
    },
  }
}

async function generateSingle(req: GenerateRequest, signal?: AbortSignal, onAdmitting?: (operationId: string) => void) {
    const operation_id = req.operation_id ?? crypto.randomUUID()
    const quotedRequest = { ...req, n: 1, operation_id }
    const quote = await unwrap<GenerateQuote>(
      signal ? http.post('/designer/quote', quotedRequest, { signal }) : http.post('/designer/quote', quotedRequest),
    )
    signal?.throwIfAborted()
    const generateRequest = {
      ...quotedRequest,
      quote_id: quote.quote_id,
      request_fingerprint: quote.request_fingerprint,
    }
    // Quote/upload cancellation must not create a recoverable operation. Persist
    // only at the admission boundary, before a response can be lost or aborted.
    onAdmitting?.(operation_id)
    return unwrap<{ generation_id: string; status: string; price_credits: number }>(
      signal ? http.post('/designer/generate', generateRequest, { signal }) : http.post('/designer/generate', generateRequest),
    )
}

function getSingleGeneration(id: string, signal?: AbortSignal) {
  return unwrap<ImageGeneration>(signal ? http.get(`/designer/generations/${id}`, { signal }) : http.get(`/designer/generations/${id}`))
}

export const designerApi = {
  getCapabilities: () =>
    unwrap<ImageCapabilityListResponse>(http.get('/image-capabilities')).then((response) => ({
      items: response.items.map(normalizeCapability),
      defaultCapability: response.default_capability,
    })),

  generate: async (req: GenerateRequest, signal?: AbortSignal, onAdmitting?: (operationId: string) => void) => {
    if (!Number.isInteger(req.n) || req.n < 1 || req.n > 4) throw new Error('生成数量需为 1–4 张')
    if (req.n === 1) return generateSingle(req, signal, onAdmitting)
    const id = req.operation_id ?? crypto.randomUUID()
    const fingerprint = JSON.stringify({ ...req, operation_id: undefined })
    const batch = loadDesignerBatch(id) ?? {
      fingerprint,
      operations: Array.from({ length: req.n }, (_, index) => ({ id: index === 0 ? id : crypto.randomUUID(), state: 'pending' as const })),
    }
    if (batch.fingerprint !== fingerprint) throw new Error('生成请求与上次提交不一致')
    saveDesignerBatch(id, batch)
    const outcomes = await Promise.allSettled(batch.operations.map(async (operation) => {
      try {
        return await generateSingle({ ...req, operation_id: operation.id, n: 1 }, signal, () => {
          onAdmitting?.(id)
          operation.state = 'admitted'
          saveDesignerBatch(id, batch)
        })
      } catch (error) {
        const status = isAxiosError(error) ? error.response?.status : undefined
        if (operation.state !== 'admitted' || (status !== undefined && status >= 400 && status < 500 && status !== 408)) {
          operation.state = 'rejected'
          saveDesignerBatch(id, batch)
        }
        throw error
      }
    }))
    signal?.throwIfAborted()
    const accepted = outcomes.filter((outcome) => outcome.status === 'fulfilled')
    if (accepted.length === 0) {
      const uncertainIndex = batch.operations.findIndex((operation) => operation.state === 'admitted')
      const failed = uncertainIndex >= 0 ? outcomes[uncertainIndex] : outcomes.find((outcome) => outcome.status === 'rejected')
      throw (failed as PromiseRejectedResult).reason
    }
    return { generation_id: id, status: 'generating', price_credits: accepted.reduce((sum, outcome) => sum + outcome.value.price_credits, 0) }
  },

  registerReference: (data: { upload_id: string; key: string }, signal?: AbortSignal) =>
    unwrap<{ file_id: string; filename: string; size: number }>(
      signal ? http.post('/designer/register-reference', data, { signal }) : http.post('/designer/register-reference', data),
    ),

  uploadReferenceFromUrl: (url: string, signal?: AbortSignal) =>
    unwrap<{ file_id: string; filename: string; size: number }>(
      signal
        ? http.post('/designer/upload-reference-from-url', { url }, { signal })
        : http.post('/designer/upload-reference-from-url', { url }),
    ),

  getHistory: (params: { project_id?: string; page?: number; page_size?: number } = {}) =>
    unwrap<HistoryResponse>(http.get('/designer/history', { params })),

  getGeneration: async (id: string, signal?: AbortSignal): Promise<ImageGeneration> => {
    const batch = loadDesignerBatch(id)
    if (!batch) return getSingleGeneration(id, signal)
    const admitted = batch.operations.filter((operation) => operation.state === 'admitted')
    if (!admitted.length) return { id, project_id: '', prompt: '', n: batch.operations.length, status: 'failed', error: '图片未能提交，请重试', created_at: '', updated_at: '' }
    const generations = await Promise.all(admitted.map((operation) => getSingleGeneration(operation.id, signal)))
    const results = generations.flatMap((generation) => generation.status === 'completed' ? generation.results ?? [] : []).map((result, index) => ({ ...result, index }))
    const generating = generations.some((generation) => generation.status === 'generating')
    return {
      ...generations[0], id, n: batch.operations.length, results,
      status: generating ? 'generating' : results.length ? 'completed' : 'failed',
      error: generations.find((generation) => generation.error)?.error,
      estimated_cost: generations.reduce((sum, generation) => sum + (generation.estimated_cost ?? 0), 0),
      final_cost: generations.reduce((sum, generation) => sum + (generation.final_cost ?? 0), 0),
    }
  },
}
