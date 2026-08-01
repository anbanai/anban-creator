export interface DesignerCapabilityFeatures {
  qualityLevels: string[]
  sizePresets: string[]
  defaultSize: string
  maxBatch: number
  maxReferenceImages: number
  supportsReference: boolean
  supportsMask: boolean
  outputFormats: string[]
  hasBackground: boolean
  hasCompression: boolean
  watermark: boolean
}

export interface DesignerCapability {
  id: string
  name: string
  description?: string
  minTier?: string
  credits: number
  priceAvailable?: boolean
  enabled: boolean
  idx: number
  features: DesignerCapabilityFeatures
}

export interface GenerateRequest {
  project_id: string
  prompt: string
  capability_key: string
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

export interface GenerateQuote {
  quote_id: string
  operation_id: string
  request_fingerprint: string
  price_credits: number
}

export interface GenerateImage { url: string; width?: number; height?: number; index: number }

export interface ImageGeneration {
  id: string
  user_id: string
  project_id: string
  prompt: string
  revised_prompt?: string
  capability_key?: string
  capability_name?: string
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

export interface HistoryResponse { items: ImageGeneration[]; total: number; page: number; page_size: number }
