export type OpenMontageAssetType = 'text' | 'image_url' | 'video_url' | 'audio_url' | 'document_url'

export interface OpenMontageAsset {
  type: OpenMontageAssetType
  url?: string
  task_file_id?: string
  text?: string
  file_name?: string
  mime_type?: string
  file_size?: number
}

export interface OpenMontagePreferences {
  aspect_ratio?: string
  duration_seconds?: number
  style?: string
  music_prompt?: string
  subtitle_mode?: string
  voiceover_mode?: string
}

export interface OpenMontageInput {
  brief?: string
  pipeline_key?: string
  source_assets?: OpenMontageAsset[]
  preferences?: OpenMontagePreferences
  delivery_targets?: string[]
  advanced?: Record<string, unknown>
}
