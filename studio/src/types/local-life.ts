export type LocalLifeAssetType = 'video' | 'image'

export interface LocalLifeAsset {
  type: LocalLifeAssetType
  url?: string
  task_file_id?: string
  file_name?: string
  mime_type?: string
  file_size?: number
}

export interface LocalLifeInput {
  mix_type: string
  brief: string
  merchant_facts?: string
  source_assets: LocalLifeAsset[]
  variant_count: number
  target_duration_seconds: number
  aspect_ratio: '9:16' | '16:9' | '1:1'
  voice_id?: string
  bgm_asset_id?: string
}

export interface LocalLifeMixType {
  id: string
  name: string
  description: string
  use_cases: string
  recommended_assets: string[]
  shot_order: string
  recommended_duration: string
  guardrails: string[]
  voice_style: string
}

export interface LocalLifeCapabilityListResponse {
  enabled: boolean
  default_mix_type: string
  default_variant_count: number
  max_variant_count: number
  default_duration_seconds: number
  max_duration_seconds: number
  aspect_ratios: string[]
  types: LocalLifeMixType[]
}
