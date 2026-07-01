export type VideoPurpose = 'planting' | 'ecommerce' | 'lead_gen' | 'promotion'

export interface VideoDefaults {
  purpose?: VideoPurpose
  model_key?: string
  resolution?: string
  ratio?: string
  duration?: number
  watermark?: boolean
  preflight?: boolean
}

export interface VideoModelPolicy {
  allowed_models?: string[]
  default_model?: string
  allow_auto_downgrade?: boolean
  max_resolution?: string
  max_duration?: number
}

export interface VideoPricingBreakdown {
  cny: number
  credit_multiplier: number
  input_video: boolean
  input_seconds?: number
  output_seconds: number
  resolution: string
  ratio: string
  model_key: string
}

export interface VideoTaskConfig extends VideoDefaults {
  model?: string
  estimated_credits?: number
  pricing_breakdown?: VideoPricingBreakdown
}
