export interface ModelCapabilities {
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

export interface DesignerProviderPricing {
  pricingType?: string
  currency?: string
  estimateTable?: Record<string, Record<string, number>>
  creditsPerCny?: number
  requiresUsage?: boolean
  billingNote?: string
}

export interface RawDesignerProvider {
  id: string
  name: string
  alias?: string
  provider: string
  provider_key?: string
  route?: string
  model: string
  credits: number
  enabled: boolean
  idx: number
  capabilities?: {
    quality_levels?: string[]
    size_presets?: string[]
    default_size?: string
    max_batch?: number
    max_reference_images?: number
    supports_reference?: boolean
    supports_mask?: boolean
    output_formats?: string[]
    has_background?: boolean
    has_compression?: boolean
    watermark?: boolean
  }
  pricing?: {
    pricing_type?: string
    currency?: string
    estimate_table?: Record<string, Record<string, number>>
    credits_per_cny?: number
    requires_usage?: boolean
    billing_note?: string
  }
}

// Dynamic provider info from backend API
export interface DesignerProvider {
  id: string
  name: string
  alias?: string
  provider: string
  providerKey?: string
  route?: string
  model: string
  credits: number
  enabled: boolean
  idx: number
  description?: string
  capabilities: ModelCapabilities
  pricing: DesignerProviderPricing
}

export interface DesignerSettings {
  quality: string
  size: string
  resolution: string
  n: number
  outputFormat: string
  compression: number
  background: string
  watermark: boolean
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
  input_tokens?: number
  output_tokens?: number
  text_input_tokens?: number
  text_cached_input_tokens?: number
  image_input_tokens?: number
  image_cached_input_tokens?: number
  image_output_tokens?: number
  total_tokens?: number
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
