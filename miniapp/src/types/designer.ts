export interface ModelCapabilities {
  maxRefImages: number
  batch: boolean
  maxBatch: number
  streaming: boolean
  inpainting: boolean
  qualityLevels: string[]
  outputFormats: string[]
  flexibleSize: boolean
  watermark: boolean
  sizePresets: string[]
  hasCompression: boolean
  hasBackground: boolean
}

const PROVIDER_CAPABILITIES: Record<string, ModelCapabilities> = {
  openai: {
    maxRefImages: 16,
    batch: true,
    maxBatch: 10,
    streaming: true,
    inpainting: true,
    qualityLevels: ['auto', 'low', 'medium', 'high'],
    outputFormats: ['png', 'jpeg', 'webp'],
    flexibleSize: false,
    watermark: false,
    sizePresets: ['auto', '1024x1024', '1536x1024', '1024x1536', '2048x1152', '2048x2048', '3840x2160', '2160x3840'],
    hasCompression: true,
    hasBackground: true,
  },
  gemini: {
    maxRefImages: 10,
    batch: false,
    maxBatch: 1,
    streaming: false,
    inpainting: false,
    qualityLevels: [],
    outputFormats: ['png'],
    flexibleSize: false,
    watermark: false,
    sizePresets: [],
    hasCompression: false,
    hasBackground: false,
  },
  volcengine: {
    maxRefImages: 1,
    batch: false,
    maxBatch: 1,
    streaming: false,
    inpainting: false,
    qualityLevels: [],
    outputFormats: ['png', 'jpeg'],
    flexibleSize: true,
    watermark: true,
    sizePresets: [],
    hasCompression: false,
    hasBackground: false,
  },
}

export function getModelCapabilities(provider: string, model?: string): ModelCapabilities | undefined {
  const baseCaps = PROVIDER_CAPABILITIES[provider]
  if (!baseCaps) return undefined

  if (provider === 'openai' && model) {
    const isGPTImage = model.startsWith('gpt-image-') || model === 'chatgpt-image-latest'
    const isDallE2 = model === 'dall-e-2'
    if (!isGPTImage && !isDallE2) {
      return {
        ...baseCaps,
        batch: false,
        maxBatch: 1,
        streaming: false,
        inpainting: false,
        maxRefImages: 1,
        qualityLevels: ['standard', 'hd'],
        outputFormats: ['png'],
        sizePresets: [],
        hasCompression: false,
        hasBackground: false,
      }
    }
  }

  return baseCaps
}

export interface DesignerProvider {
  id: string
  name: string
  provider: string
  model: string
  credits: number
  enabled: boolean
  description?: string
}

export interface GenerateRequest {
  project_id: string
  prompt: string
  provider: string
  provider_id?: string
  model?: string
  quality?: string
  size?: string
  n?: number
  output_format?: string
  output_compression?: number
  background?: string
  reference_file_ids?: string[]
  mask_file_id?: string
  watermark?: boolean
}

export interface GenerateImage {
  url: string
  width?: number
  height?: number
  index: number
}

export interface ImageGeneration {
  id: string
  user_id: string
  project_id: string
  prompt: string
  revised_prompt?: string
  provider: string
  model: string
  quality?: string
  size?: string
  n: number
  output_format?: string
  status: 'generating' | 'completed' | 'failed'
  error?: string
  created_at: string
  updated_at: string
  results?: ImageGenerationResult[]
}

export interface ImageGenerationResult {
  id: number
  generation_id: string
  image_url?: string
  image_path?: string
  width?: number
  height?: number
  index: number
  file_id?: string
}

export interface HistoryResponse {
  items: ImageGeneration[]
  total: number
  page: number
  page_size: number
}
