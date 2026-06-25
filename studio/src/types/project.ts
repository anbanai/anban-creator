export type ProjectPlatform = 'article' | 'seednote' | 'ecommerce'
export type ProjectStatus = 'active' | 'archived'

export interface ProjectConfig {
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
}

export interface Project {
  id: string
  user_id: string
  platform: ProjectPlatform
  name: string
  avatar_url: string
  profile_url: string
  description?: string
  positioning: string
  keywords: string
  style: string
  writing_style: string
  theme: string
  author: string
  author_style_intro: string
  author_avatar_url: string
  template_id: string
  reference_image_url: string
  image_ratio: string
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
  positioning?: string
  keywords?: string
  style?: string
  writing_style?: string
  theme?: string
  author?: string
  author_style_intro?: string
  author_avatar_url?: string
  template_id?: string
  reference_image_url?: string
  image_ratio?: string
  max_concurrent_tasks?: number
  wechat_app_id?: string
  wechat_secret?: string
  enable_publishing?: boolean
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
