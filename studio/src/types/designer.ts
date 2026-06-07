export interface ModelCapabilities {
  maxRefImages: number
  batch: boolean
  maxBatch: number
  streaming: boolean
  inpainting: boolean
  qualityLevels: string[]
  outputFormats: string[]
  flexibleSize: boolean
}

// Capabilities keyed by provider name (matches config designer.*.provider)
const PROVIDER_CAPABILITIES: Record<string, ModelCapabilities> = {
  openai: {
    maxRefImages: 16,
    batch: true,
    maxBatch: 10,
    streaming: true,
    inpainting: true,
    qualityLevels: ['auto', 'low', 'medium', 'high'],
    outputFormats: ['png', 'jpeg', 'webp'],
    flexibleSize: true,
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
  },
  volcengine: {
    maxRefImages: 1,
    batch: false,
    maxBatch: 1,
    streaming: false,
    inpainting: false,
    qualityLevels: [],
    outputFormats: ['png', 'jpeg'],
    flexibleSize: false,
  },
}

export function getModelCapabilities(provider: string): ModelCapabilities | undefined {
  return PROVIDER_CAPABILITIES[provider]
}

// Dynamic provider info from backend API
export interface DesignerProvider {
  id: string
  name: string
  provider: string
  model: string
  description?: string
}

export interface DesignerSettings {
  quality: string
  size: string
  n: number
  outputFormat: string
  referenceFiles: File[]
  maskFile: File | null
}

export interface GenerateRequest {
  channel_id: string
  prompt: string
  provider: string
  model?: string
  quality?: string
  size?: string
  n?: number
  output_format?: string
  reference_file_ids?: string[]
  mask_file_id?: string
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
  channel_id: string
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
  input_tokens?: number
  output_tokens?: number
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
