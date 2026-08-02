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
  alias?: string
  description?: string
  minTier?: string
  credits: number
  priceAvailable?: boolean
  enabled: boolean
  idx: number
  designerFeatures: DesignerCapabilityFeatures
}

export interface DesignerSettings {
  quality: string
  size: string
  n: number
  outputFormat: string
  compression: number
  background: string
  watermark: boolean
}

export interface GenerateRequest {
  project_id: string
  prompt: string
  capability_key: string
  quality: string
  size: string
  n: number
  output_format: string
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
  catalog_id: string
  sku_id: string
  pricing_tier: 'free' | 'pro' | 'enterprise'
  list_price_credits: number
  price_credits: number
  discount_credits: number
  expires_at: string
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
  capability_key?: string
  capability_name?: string
  quality?: string
  size?: string
  n: number
  output_format?: string
  status: 'generating' | 'completed' | 'failed'
  error?: string
  error_code?: 'image_ratio_mismatch'
  estimated_cost?: number
  final_cost?: number
  billing_mode?: string
  billing_status?: string
  price_snapshot?: Record<string, unknown>
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
