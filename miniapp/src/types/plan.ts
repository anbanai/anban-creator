import type { ReferenceAssetView, ReferenceImageSelection } from './asset'
import type { AgentExecutionProfileID } from './agent-profile'

export type PlanType = 'seednote' | 'article'
export type PlanStatus = 'active' | 'paused' | 'completed'

export interface Plan {
  id: string
  type: PlanType
  execution_profile: AgentExecutionProfileID
  title: string
  description: string
  cron_expr: string
  prompt: string
  topic_hint?: string
  status: PlanStatus
  next_run_at: string
  project_id: string
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceAssetView | null
  visual_style?: string
  writer_key?: string
  theme?: string
  watermark?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable. Both default true; spawned article tasks inherit them.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // 公众号人设覆盖；spawned task 继承。
  byline?: string
  writing_voice?: string
  persona_avatar?: string
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  execution_profile: AgentExecutionProfileID
  cron_expr: string
  prompt?: string
  project_id?: string
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  visual_style?: string
  writer_key?: string
  theme?: string
  watermark?: boolean
  // Seednote image composition (see CreateTaskRequest).
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable; both default true. Server ignores for non-article plans.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // 公众号人设 override（署名 + 写作风格模仿 + 可选头像）。
  byline?: string
  writing_voice?: string
  persona_avatar?: string
}

export interface UpdatePlanRequest {
  execution_profile: AgentExecutionProfileID
  type?: PlanType
  cron_expr?: string
  prompt?: string
  project_id?: string
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  visual_style?: string
  writer_key?: string
  theme?: string
  watermark?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable; both default true. Server ignores for non-article plans.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // 公众号人设 override（署名 + 写作风格模仿 + 可选头像）。
  byline?: string
  writing_voice?: string
  persona_avatar?: string
}
