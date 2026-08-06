import type { MontagePreferences } from './montage'
import type { ReferenceAssetView, ReferenceImageSelection } from './asset'

export type ProjectPlatform = 'article' | 'seednote' | 'moments' | 'ecommerce' | 'montage'
export type ProjectStatus = 'active' | 'archived'

export interface ProjectConfig {
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
  // Publish-approval gate (Batch 4A): when true (and enable_publishing true),
  // a completed article task holds its draft for human review instead of
  // auto-publishing. See server model.ProjectConfig.RequirePublishApproval.
  require_publish_approval?: boolean
}

export interface Project {
  id: string
  user_id: string
  platform: ProjectPlatform
  name: string
  avatar_url: string
  profile_url: string
  description?: string
  /** @deprecated use instructions */
  positioning?: string
  keywords: string
  instructions?: string
  visual_style: string
  writer: string
  theme: string
  author: string
  reference_image?: ReferenceAssetView | null
  image_ratio: string
  ecommerce_defaults?: EcommerceProjectDefaults
  montage_defaults?: MontageProjectDefaults
  agent_config?: Record<string, unknown>
  max_concurrent_tasks: number
  config: ProjectConfig
  status: ProjectStatus
  stats?: ProjectStats
  created_at: string
  updated_at: string
}

export interface EcommerceProjectDefaults {
  default_selected_modules?: Record<string, number>
  target_platform?: string
  brand_brief?: string
  image_capability_key?: string
}

export interface MontageProjectDefaults {
  default_pipeline?: string
  preferences?: MontagePreferences
  asset_guidance?: string
  delivery_targets?: string[]
}

export interface ProjectStats {
  total_tasks: number
  completed_tasks: number
  failed_tasks: number
  running_tasks: number
  pending_tasks: number
  unused_topics: number
  success_rate: number
  last_activity_at: string
}

export interface ProjectDetail {
  project: Project
  stats: ProjectStats
}

export interface CreateProjectRequest {
  platform: string
  name?: string
  profile_url?: string
  avatar_url?: string
  /** @deprecated use instructions */
  positioning?: string
  keywords?: string
  instructions?: string
  visual_style?: string
  writer?: string
  theme?: string
  author?: string
  reference_image?: ReferenceImageSelection | null
  image_ratio?: string
  ecommerce_defaults?: EcommerceProjectDefaults
  montage_defaults?: MontageProjectDefaults
  agent_config?: Record<string, unknown>
  max_concurrent_tasks?: number
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
  require_publish_approval?: boolean
}

export interface CreateProjectResponse {
  project: Project
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
  supported_image_ratios: string[]
  fields: PlatformFieldConfig[]
}

export interface PlatformProfile {
  name: string
  avatar_url: string
  /** @deprecated use instructions */
  positioning?: string
  keywords?: string
  style?: string
  raw_data: Record<string, unknown>
}
