export type VideoPurpose = 'planting' | 'ecommerce' | 'lead_gen' | 'promotion'
export type VideoWorkflow = 'creator' | 'editor'
export type VideoReferenceType = 'text' | 'image_url' | 'audio_url' | 'video_url'

export interface VideoReferenceAsset {
  type: VideoReferenceType
  url?: string
  text?: string
  reference_role?: string
  file_name?: string
  mime_type?: string
  file_size?: number
  input_duration_seconds?: number
}

export interface VideoDefaults {
  workflow?: VideoWorkflow
  purpose?: VideoPurpose
  model_key?: string
  resolution?: string
  ratio?: string
  duration?: number
  target_duration_seconds?: number
  target_duration_source?: 'user' | 'reference_video' | 'ai_planned' | 'project_default'
  target_duration_reason?: string
  segment_max_duration_seconds?: number
  segment_min_duration_seconds?: number
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
  segment_count?: number
  resolution: string
  ratio: string
  model_key: string
  segments?: Array<{
    index: number
    seconds: number
    cny: number
    credits: number
  }>
}

export interface VideoTaskConfig extends VideoDefaults {
  model?: string
  references?: VideoReferenceAsset[]
  segments?: Array<{
    index: number
    start_second: number
    end_second: number
    duration: number
    prompt?: string
    model_key?: string
    model?: string
    resolution?: string
    ratio?: string
    estimated_credits?: number
  }>
  estimated_credits?: number
  pricing_breakdown?: VideoPricingBreakdown
}

export interface VideoModelSpec {
  key: string
  display_name?: string
  model_id?: string
  supported_resolutions?: string[]
  supported_ratios?: string[]
  min_duration?: number
  max_duration?: number
  supports_video_input?: boolean
  supports_4k?: boolean
}

export interface VideoEstimateRequest {
  project_id: string
  prompt?: string
  video_config?: VideoTaskConfig
}

export interface VideoEstimateResponse {
  available_models: VideoModelSpec[]
  resolved_config: VideoTaskConfig
  estimated_credits: number
  pricing_breakdown?: VideoPricingBreakdown
  balance: number
  min_balance: number
  meets_min_balance: boolean
  warnings?: string[]
}
