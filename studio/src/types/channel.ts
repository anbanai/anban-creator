export type ChannelPlatform = 'article' | 'xls' | 'seednote'
export type ChannelStatus = 'active' | 'archived'

export interface ChannelConfig {
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
}

export interface Channel {
  id: string
  user_id: string
  platform: ChannelPlatform
  name: string
  avatar_url: string
  profile_url: string
  positioning: string
  keywords: string
  style: string
  theme: string
  author: string
  reference_image_url: string
  image_ratio: string
  layout: string
  image_preset: string
  max_concurrent_tasks: number
  config: ChannelConfig
  status: ChannelStatus
  stats?: ChannelStats
  created_at: string
  updated_at: string
}

export interface ChannelStats {
  total_tasks: number
  completed_tasks: number
  failed_tasks: number
  running_tasks: number
  pending_tasks: number
  success_rate: number
  last_activity_at: string
}

export interface ChannelDetail {
  channel: Channel
  stats: ChannelStats
}

export interface CreateChannelRequest {
  platform: string
  name?: string
  profile_url?: string
  avatar_url?: string
  positioning?: string
  keywords?: string
  style?: string
  theme?: string
  author?: string
  reference_image_url?: string
  image_ratio?: string
  layout?: string
  image_preset?: string
  max_concurrent_tasks?: number
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
}

export interface CreateChannelResponse {
  channel: Channel
  recommended_templates?: import('./template').Template[]
}

export interface PlatformFieldConfig {
  key: string
  label: string
  placeholder: string
  required: boolean
  type: 'text' | 'password' | 'url' | 'textarea' | 'number' | 'select'
  group: 'basic' | 'credentials' | 'content' | 'advanced'
  auto_fetched: boolean
}

export interface PlatformConfig {
  id: string
  label: string
  badge_variant: string
  supports_publishing: boolean
  supports_auto_fetch: boolean
  profile_url_pattern: string
  default_image_ratio: string
  fields: PlatformFieldConfig[]
}

export interface PlatformProfile {
  name: string
  avatar_url: string
  positioning: string
  keywords?: string
  style?: string
  raw_data: Record<string, unknown>
}
