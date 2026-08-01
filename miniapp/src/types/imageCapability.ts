export interface ImageCapabilityFeatures {
  quality_levels: string[]
  size_presets: string[]
  default_size: string
  max_batch: number
  max_reference_images: number
  supports_reference: boolean
  supports_mask: boolean
  output_formats: string[]
  has_background: boolean
  has_compression: boolean
  watermark: boolean
}

export interface ImageCapabilityOption {
  key: string
  display_name: string
  description?: string
  min_tier?: string
  sort_order?: number
  price_credits?: number
  price_available?: boolean
  enabled?: boolean
  features?: ImageCapabilityFeatures
}

export interface ImageCapabilityListResponse {
  items: ImageCapabilityOption[]
  tier: string
  default_capability: string
}
