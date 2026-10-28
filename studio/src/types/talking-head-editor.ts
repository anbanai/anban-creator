export type TalkingHeadEditorQualityTier = 'effective' | 'balanced' | 'quality'
export type TalkingHeadEditorSubtitleMode = 'off' | 'required'
export type TalkingHeadEditorAssetRole = 'broll' | 'sfx' | 'bgm' | 'reference'

export interface TalkingHeadEditorAsset {
  role: TalkingHeadEditorAssetRole
  asset_id?: string
  url?: string
  file_name?: string
  mime_type?: string
  file_size?: number
}

export type TalkingHeadEditorPrimaryVideo = Omit<TalkingHeadEditorAsset, 'role'> & {
  role: 'reference'
}

export interface TalkingHeadEditorPreferences {
  quality_tier?: TalkingHeadEditorQualityTier
  subtitle_mode?: TalkingHeadEditorSubtitleMode
  target_duration_seconds?: number
  aspect_ratio?: '9:16' | '16:9' | '1:1' | '4:3'
  broll_mode?: 'auto' | 'provided_only'
  sound_mode?: 'after_review' | 'provided_only' | 'auto'
  style?: string
}

export interface TalkingHeadEditorInput {
  brief: string
  primary_video: TalkingHeadEditorPrimaryVideo
  supporting_assets?: TalkingHeadEditorAsset[]
  reference_cut?: TalkingHeadEditorAsset
  preferences?: TalkingHeadEditorPreferences
  delivery_targets?: string[]
}

export type TalkingHeadEditorDefaults = Pick<TalkingHeadEditorInput, 'preferences' | 'delivery_targets'>
