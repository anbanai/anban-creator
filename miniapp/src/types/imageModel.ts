export interface ImageModelOption {
  key: string
  display_name: string
  provider: string
  min_tier?: string
  is_custom?: boolean
}

export interface ImageModelListResponse {
  items: ImageModelOption[]
  tier: string
}
