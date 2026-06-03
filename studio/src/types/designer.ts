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

// Map full model IDs to their capability keys
const MODEL_CAPABILITY_MAP: Record<string, string> = {
  'gpt-image-2': 'gpt-image-2',
  'gemini-3-pro-image-preview': 'gemini',
  'doubao-seedream-5-0-250128': 'seedream',
}

export function getModelCapabilities(modelId: string): ModelCapabilities | undefined {
  const key = MODEL_CAPABILITY_MAP[modelId]
  return key ? MODEL_CAPABILITIES[key] : undefined
}

const MODEL_CAPABILITIES: Record<string, ModelCapabilities> = {
  'gpt-image-2': {
    maxRefImages: 16,
    batch: true,
    maxBatch: 10,
    streaming: true,
    inpainting: true,
    qualityLevels: ['auto', 'low', 'medium', 'high'],
    outputFormats: ['png', 'jpeg', 'webp'],
    flexibleSize: true,
  },
  'gemini': {
    maxRefImages: 10,
    batch: false,
    maxBatch: 1,
    streaming: false,
    inpainting: false,
    qualityLevels: [],
    outputFormats: ['png'],
    flexibleSize: false,
  },
  'seedream': {
    maxRefImages: 1,
    batch: true,
    maxBatch: 4,
    streaming: false,
    inpainting: false,
    qualityLevels: [],
    outputFormats: ['png', 'jpeg'],
    flexibleSize: false,
  },
}

export interface DesignerModel {
  id: string
  name: string
  provider: string
  description: string
}

export const DESIGNER_MODELS: DesignerModel[] = [
  { id: 'gpt-image-2', name: 'GPT Image 2', provider: 'openai', description: 'OpenAI 最强图片生成模型' },
  { id: 'gemini-3-pro-image-preview', name: 'Gemini', provider: 'gemini', description: 'Google Gemini 图片生成' },
  { id: 'doubao-seedream-5-0-250128', name: 'Seedream', provider: 'seedream', description: '火山引擎 Seedream 图片生成' },
]

export interface GenerateRequest {
  channel_id: string
  prompt: string
  provider: string
  model: string
  quality?: string
  size?: string
  n?: number
  output_format?: string
  reference_file_ids?: string[]
  mask_file_id?: string
  stream?: boolean
}

export interface GenerateImage {
  url: string
  width?: number
  height?: number
  index: number
}

export interface GenerateResult {
  generation_id: string
  images: GenerateImage[]
  revised_prompt?: string
  usage?: {
    input_tokens: number
    output_tokens: number
  }
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
