export type VideoEditQualityTier = 'effective' | 'balanced' | 'quality'
export type VideoEditSubtitleMode = 'off' | 'required'

export interface VideoEditPreferences {
  quality_tier?: VideoEditQualityTier
  subtitle_mode?: VideoEditSubtitleMode
  style?: string
  max_duration_seconds?: number
}

export interface VideoEditAsset {
  type: 'video' | 'audio' | 'image'
  url?: string
  task_file_id?: string
  file_name?: string
  mime_type?: string
  file_size?: number
}

export interface VideoEditInput {
  brief: string
  source_assets: VideoEditAsset[]
  preferences?: VideoEditPreferences
  delivery_targets?: string[]
}

export interface VideoEditDefaults {
  preferences?: VideoEditPreferences
  delivery_targets?: string[]
}
