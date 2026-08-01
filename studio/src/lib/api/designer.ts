import { http, unwrap } from '@/lib/http-client'
import type { ImageCapabilityListResponse, ImageCapabilityOption } from '@/types'
import type { DesignerCapability, GenerateQuote, GenerateRequest, HistoryResponse, ImageGeneration } from '@/types/designer'

export function normalizeCapability(raw: ImageCapabilityOption): DesignerCapability {
  const features = raw.features ?? {
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
    features: {
      qualityLevels: features.quality_levels ?? [],
      sizePresets: features.size_presets ?? [],
      defaultSize: features.default_size ?? 'auto',
      maxBatch: features.max_batch ?? 1,
      maxReferenceImages: features.max_reference_images ?? 0,
      supportsReference: features.supports_reference ?? false,
      supportsMask: features.supports_mask ?? false,
      outputFormats: features.output_formats ?? ['png'],
      hasBackground: features.has_background ?? false,
      hasCompression: features.has_compression ?? false,
      watermark: features.watermark ?? false,
    },
  }
}

export const designerApi = {
  getCapabilities: () =>
    unwrap<ImageCapabilityListResponse>(http.get('/image-capabilities')).then((response) => ({
      items: response.items.map(normalizeCapability),
      defaultCapability: response.default_capability,
    })),

  generate: async (req: GenerateRequest, signal?: AbortSignal) => {
    const operation_id = crypto.randomUUID()
    const quotedRequest = { ...req, n: 1, operation_id }
    const quote = await unwrap<GenerateQuote>(
      signal ? http.post('/designer/quote', quotedRequest, { signal }) : http.post('/designer/quote', quotedRequest),
    )
    const generateRequest = {
      ...quotedRequest,
      quote_id: quote.quote_id,
      request_fingerprint: quote.request_fingerprint,
    }
    return unwrap<{ generation_id: string; status: string; price_credits: number }>(
      signal ? http.post('/designer/generate', generateRequest, { signal }) : http.post('/designer/generate', generateRequest),
    )
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

  getGeneration: (id: string, signal?: AbortSignal) =>
    unwrap<ImageGeneration>(
      signal ? http.get(`/designer/generations/${id}`, { signal }) : http.get(`/designer/generations/${id}`),
    ),
}
