export type MontageAssetType = 'text' | 'image_url' | 'video' | 'video_url' | 'audio' | 'audio_url' | 'document_url'

export interface MontageAsset {
  type: MontageAssetType
  url?: string
  task_file_id?: string
  text?: string
  file_name?: string
  mime_type?: string
  file_size?: number
}

export interface MontagePreferences {
  duration_seconds?: number
  style?: string
  music_prompt?: string
  subtitle_mode?: string
  voiceover_mode?: string
}

export interface MontageInput {
  brief?: string
  pipeline_key?: string
  source_assets?: MontageAsset[]
  preferences?: MontagePreferences
  delivery_targets?: string[]
  advanced?: Record<string, unknown>
}

export type MontageSourceRequirement = 'optional' | 'video' | 'video_or_audio'
export type MontageOutputMode = 'single' | 'multiple'

export interface MontagePipelineCapability {
  key: string
  display_name: string
  description: string
  best_for: string[]
  source_hint: string
  output_hint: string
  source_requirement: MontageSourceRequirement
  output_mode: MontageOutputMode
  recommended_duration_seconds: number
}

export interface MontageCapabilityListResponse {
  enabled: boolean
  default_pipeline: string
  max_duration_seconds: number
  max_assets: number
  items: MontagePipelineCapability[]
}
