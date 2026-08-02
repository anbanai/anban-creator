import type { ReferenceAssetView, ReferenceImageSelection } from './asset'

export type ProjectPlatform = 'article' | 'seednote' | 'moments' | 'ecommerce'
export type ProjectStatus = 'active' | 'archived'

export interface ProjectConfig {
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
}

export interface EcommerceProjectDefaults {
  default_selected_modules?: Record<string, number>
  target_platform?: string
  brand_brief?: string
  image_capability_key?: string
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
  // Visual style dimension — independent from writer_key/theme.
  visual_style: string
  // Optional template binding (article/ecommerce). When set, persona fields may
  // be sourced from the bound template at runtime (task>template>project chain).
  template_id?: string
  // Article-only persona fields — byline (署名) is DISTINCT from writer_key.
  // Per project memory: byline NEVER auto-fills from persona.
  writer_key?: string
  writing_voice?: string
  persona_avatar?: string
  theme: string
  byline: string
  reference_image?: ReferenceAssetView | null
  image_ratio: string
  ecommerce_defaults?: EcommerceProjectDefaults
  layout: string
  max_concurrent_tasks: number
  config: ProjectConfig
  status: ProjectStatus
  stats?: ProjectStats
  created_at: string
  updated_at: string
}

export interface ProjectStats {
  total_tasks: number
  completed_tasks: number
  failed_tasks: number
  running_tasks: number
  pending_tasks: number
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
  // Article-only persona (orthogonal: byline ≠ writer_key persona).
  writer_key?: string
  writing_voice?: string
  persona_avatar?: string
  template_id?: string
  theme?: string
  byline?: string
  reference_image?: ReferenceImageSelection | null
  image_ratio?: string
  ecommerce_defaults?: EcommerceProjectDefaults
  layout?: string
  max_concurrent_tasks?: number
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
}

export interface AnalyzeImageResponse {
  visual_style: string
}

export interface FileUploadResponse {
  url: string
  key: string
  size: number
  type: string
  upload_session_id: string
  preview_url: string
}

export interface CreateProjectResponse {
  project: Project
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
  supported_image_ratios: string[]
  fields: PlatformFieldConfig[]
}

export interface PlatformProfile {
  name: string
  avatar_url: string
  positioning: string
  keywords?: string
  visual_style?: string
  raw_data: Record<string, unknown>
}
