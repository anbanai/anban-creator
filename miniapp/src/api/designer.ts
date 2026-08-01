import { get, post } from './request'
import { uploadUrl } from './api-base'
import type {
  DesignerCapability,
  GenerateQuote,
  GenerateRequest,
  HistoryResponse,
  ImageCapabilityListResponse,
  ImageCapabilityOption,
  ImageGeneration,
} from '@/types'
import { TOKEN_KEY } from '@/utils/constants'

function normalizeCapability(raw: ImageCapabilityOption): DesignerCapability {
  const features = raw.features
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
      qualityLevels: features?.quality_levels ?? [],
      sizePresets: features?.size_presets ?? [],
      defaultSize: features?.default_size ?? 'auto',
      maxBatch: features?.max_batch ?? 1,
      maxReferenceImages: features?.max_reference_images ?? 0,
      supportsReference: features?.supports_reference ?? false,
      supportsMask: features?.supports_mask ?? false,
      outputFormats: features?.output_formats ?? ['png'],
      hasBackground: features?.has_background ?? false,
      hasCompression: features?.has_compression ?? false,
      watermark: features?.watermark ?? false,
    },
  }
}

function operationID(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (token) => {
    const random = Math.floor(Math.random() * 16)
    const value = token === 'x' ? random : (random & 0x3) | 0x8
    return value.toString(16)
  })
}

export const designerApi = {
  getCapabilities: async () => {
    const response = await get<ImageCapabilityListResponse>('/image-capabilities')
    return {
      items: response.items.map(normalizeCapability),
      defaultCapability: response.default_capability,
    }
  },

  generate: async (req: GenerateRequest) => {
    const operation_id = operationID()
    const quotedRequest = { ...req, operation_id }
    const quote = await post<GenerateQuote>('/designer/quote', quotedRequest)
    return post<{ generation_id: string; status: string; price_credits: number }>('/designer/generate', {
      ...quotedRequest,
      quote_id: quote.quote_id,
      request_fingerprint: quote.request_fingerprint,
    })
  },

  uploadReference: (filePath: string, name = 'file') =>
    new Promise<{ file_id: string; filename: string; size: number }>((resolve, reject) => {
      const token = uni.getStorageSync(TOKEN_KEY)
      uni.uploadFile({
        url: uploadUrl('/designer/upload-reference'),
        filePath,
        name,
        header: token ? { Authorization: `Bearer ${token}` } : undefined,
        success(res) {
          try {
            const body = JSON.parse(res.data)
            if (body.code === 0) resolve(body.data)
            else reject(new Error(body.msg || '上传失败'))
          } catch { reject(new Error('上传响应解析失败')) }
        },
        fail(err) { reject(new Error(err.errMsg || '上传失败')) },
      })
    }),

  getHistory: (params: { project_id?: string; page?: number; page_size?: number } = {}) =>
    get<HistoryResponse>('/designer/history', params as Record<string, any>),

  getGeneration: (id: string) => get<ImageGeneration>(`/designer/generations/${id}`),
}
