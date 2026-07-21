export type VideoPurpose = 'planting' | 'ecommerce' | 'lead_gen' | 'promotion'
export type VideoReferenceType = 'text' | 'image_url' | 'audio_url' | 'video_url'
export type VideoCreativeType = 'personal_ip' | 'high_efficiency_joke' | 'product_demo' | 'brand_promo' | 'custom'
export type VideoProductionMode = 'fast_lane' | 'guided' | 'sequence' | 'remake'

export interface VideoReferenceAsset {
  type: VideoReferenceType
  url?: string
  text?: string
  task_file_id?: string
  reference_role?: string
  must_keep?: string[]
  can_change?: string[]
  must_not_transfer?: string[]
  file_name?: string
  mime_type?: string
  file_size?: number
  input_duration_seconds?: number
}

export interface VideoHardConstraints {
  ratio?: string
  duration?: number
  watermark?: boolean
}

export interface VideoInput {
  brief?: string
  references?: VideoReferenceAsset[]
  hard_constraints?: VideoHardConstraints
}

export interface VideoPlaybookSpec {
  key: string
  label: string
  creative_type: VideoCreativeType
  purpose: VideoPurpose
  required_reference_roles: string[]
  default_ratio: string
  prompt_scaffold: string
  qc_focus: string[]
  risk_notes: string[]
  agent_brief?: string
}

export interface VideoDefaults {
  scenario_key?: string
  production_mode?: VideoProductionMode
  purpose?: VideoPurpose
  creative_type?: VideoCreativeType
  subject_profile?: string
  audience?: string
  single_message?: string
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
  retake_budget?: number
  delivery_targets?: string[]
}

export interface VideoModelPolicy {
  allowed_models?: string[]
  default_model?: string
  allow_auto_downgrade?: boolean
  max_resolution?: string
  max_duration?: number
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
  }>
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
  video_creator_config?: VideoTaskConfig
}

export interface VideoEstimateResponse {
  available_models: VideoModelSpec[]
  resolved_creator_config: VideoTaskConfig
  warnings?: string[]
  missing_reference_roles?: string[]
  expected_artifacts?: string[]
  segment_plan?: Array<{
    index: number
    start_second: number
    end_second: number
    duration: number
    prompt?: string
    model_key?: string
    model?: string
    resolution?: string
    ratio?: string
  }>
}

export interface VideoProductionArtifact {
  status: 'available' | 'missing' | 'error' | string
  file_id?: string
  file_name: string
  url?: string
  content?: string
  parsed_json?: Record<string, unknown>
  error?: string
}

export interface VideoProductionResponse {
  task_id: string
  scenario_key?: string
  production_mode?: VideoProductionMode | string
  artifacts: Record<string, VideoProductionArtifact>
  retake_actions: string[]
  next_actions: string[]
}
