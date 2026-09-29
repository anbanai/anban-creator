import type { HypitDefaults } from './hypit'
import type { MontagePreferences } from './montage'
import type { ReferenceAssetView, ReferenceImageSelection } from './asset'
import type { ImageAnalysis } from './image-analysis'

export type ProjectPlatform = 'article' | 'seednote' | 'moments' | 'ecommerce' | 'montage' | 'hypit'
export type ProjectStatus = 'active' | 'archived'
export interface ProjectConfig {
  wechat_app_id?: string
  wechat_secret?: string
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
  visual_style_source?: 'manual' | 'analysis' | ''
  image_analysis?: ImageAnalysis | null
  writer: string
  theme: string
  author: string
  reference_image?: ReferenceAssetView | null
  portrait_reference_image?: ReferenceAssetView | null
  image_ratio: string
  ecommerce_defaults?: EcommerceProjectDefaults
  hypit_defaults?: HypitDefaults
  montage_defaults?: MontageProjectDefaults
  agent_config?: Record<string, unknown>
  max_concurrent_tasks: number
  timezone?: string
  feedback_paused?: boolean
  config: ProjectConfig
  status: ProjectStatus
  stats?: ProjectStats
  created_at: string
  updated_at: string
}

export interface FeedbackDashboard {
  project_id: string
  platform: ProjectPlatform
  timezone: string
  feedback_paused: boolean
  analytics: {
    revision: number
    status: string
    content_count: number
    valid_observation_count: number
  }
  queue: {
    counts: Record<'queued' | 'running' | 'succeeded' | 'failed' | 'skipped' | 'blocked', number>
    last_success_at: string | null
    latest_skip?: { reason: string; cadence: string; operation: string; created_at: string }
  }
  next_runs: { daily?: string; weekly?: string; monthly?: string }
  strategy: { id: string; revision: number; source_revision?: number; status: string; digest?: string; expires_at?: string | null }
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

export interface ProfileDimension {
  content: Record<string, unknown>
  sources: string[]
  evidence: string[]
  missing_fields: string[]
}

export interface ProjectProfile {
  schema_version: number
  status: 'draft' | 'confirmed'
  version: number
  analysis_task_id?: string
  dimensions: Record<'identity' | 'style' | 'audience' | 'platforms' | 'preferences' | 'memory', ProfileDimension>
  analysis_limits: string[]
  follow_up_questions: string[]
}

export type ProfileAnalysisStatus = TaskStatusLike | 'not_started'

type TaskStatusLike = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface ProfileAnalysisResponse {
  status: ProfileAnalysisStatus
  task?: { id: string; status: TaskStatusLike; error_message?: string }
  profile: ProjectProfile
  draft?: ProjectProfile
}

export interface ProjectMemoryFile {
  path: string
  content: string
  size_bytes: number
  modified_at: string
  truncated: boolean
}

export interface ProjectMemory {
  status: 'empty' | 'ready'
  updated_at: string | null
  partial: boolean
  files: ProjectMemoryFile[]
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
  portrait_reference_image?: ReferenceImageSelection | null
  image_ratio?: string
  ecommerce_defaults?: EcommerceProjectDefaults
  hypit_defaults?: HypitDefaults
  montage_defaults?: MontageProjectDefaults
  agent_config?: Record<string, unknown>
  max_concurrent_tasks?: number
  wechat_app_id?: string
  wechat_secret?: string
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
