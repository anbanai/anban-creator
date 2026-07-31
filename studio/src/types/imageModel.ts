export interface ImageCapabilityOption {
  key: string
  display_name: string
  description?: string
  min_tier?: string
  sort_order?: number
  price_credits?: number
  price_available?: boolean
  is_custom?: boolean
}

export interface ImageModelListResponse {
  items: ImageCapabilityOption[]
  tier: string
}

export type ImageModelOption = ImageCapabilityOption
