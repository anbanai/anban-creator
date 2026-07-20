import { http, unwrap } from '@/lib/http-client'
import type { DesignerProvider, GenerateQuote, GenerateRequest, HistoryResponse, ImageGeneration, RawDesignerProvider } from '@/types/designer'

export function normalizeProvider(raw: RawDesignerProvider): DesignerProvider {
  const caps = raw.capabilities ?? {}
  const pricing = raw.pricing
  return {
    id: raw.id,
    name: raw.name,
    alias: raw.alias,
    provider: raw.provider,
    providerKey: raw.provider_key,
    route: raw.route,
    model: raw.model,
    credits: raw.credits,
    enabled: raw.enabled,
    idx: raw.idx,
    capabilities: {
      qualityLevels: caps.quality_levels ?? [],
      sizePresets: caps.size_presets ?? [],
      defaultSize: caps.default_size ?? 'auto',
      maxBatch: caps.max_batch ?? 1,
      maxReferenceImages: caps.max_reference_images ?? 0,
      supportsReference: caps.supports_reference ?? false,
      supportsMask: caps.supports_mask ?? false,
      outputFormats: caps.output_formats ?? ['png'],
      hasBackground: caps.has_background ?? false,
      hasCompression: caps.has_compression ?? false,
      watermark: caps.watermark ?? false,
    },
    pricing: {
      pricingType: pricing?.pricing_type ?? 'fixed_sku',
      currency: pricing?.currency ?? 'credits',
      billingNote: pricing?.billing_note ?? 'fixed retail SKU',
    },
  }
}

export const designerApi = {
  getProviders: () =>
    unwrap<RawDesignerProvider[]>(http.get('/designer/providers')).then((items) => items.map(normalizeProvider)),

  generate: async (req: GenerateRequest) => {
    const operation_id = crypto.randomUUID()
    const quotedRequest = { ...req, n: 1, operation_id }
    const quote = await unwrap<GenerateQuote>(http.post('/designer/quote', quotedRequest))
    return unwrap<{ generation_id: string; status: string; price_credits: number }>(
      http.post('/designer/generate', {
        ...quotedRequest,
        quote_id: quote.quote_id,
        request_fingerprint: quote.request_fingerprint,
      }),
    )
  },

  uploadReference: (file: File) => {
    const form = new FormData()
    form.append('file', file)
    return unwrap<{ file_id: string; filename: string; size: number }>(
      http.post('/designer/upload-reference', form, {
        headers: { 'Content-Type': 'multipart/form-data' },
      }),
    )
  },

  uploadReferenceFromUrl: (url: string) =>
    unwrap<{ file_id: string; filename: string; size: number }>(
      http.post('/designer/upload-reference-from-url', { url }),
    ),

  getHistory: (params: { project_id?: string; page?: number; page_size?: number } = {}) =>
    unwrap<HistoryResponse>(http.get('/designer/history', { params })),

  getGeneration: (id: string) =>
    unwrap<ImageGeneration>(http.get(`/designer/generations/${id}`)),
}
