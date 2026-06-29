export type PlanType = 'seednote' | 'article'
export type PlanStatus = 'active' | 'paused' | 'completed'

export interface Plan {
  id: string
  type: PlanType
  title: string
  description: string
  cron_expr: string
  prompt: string
  topic_hint?: string
  status: PlanStatus
  next_run_at: string
  project_id: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  visual_style?: string
  writer?: string
  theme?: string
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable. Both default true; spawned article tasks inherit them.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // 公众号发布署名 + 写作风格 key，spawned task 继承。
  author?: string
  // template_id records the template selected when creating the plan; spawned
  // tasks inherit it so the agent can surface the template's content scaffold
  // via get_project_profile(task_id).
  template_id?: string
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  cron_expr: string
  prompt?: string
  project_id?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  visual_style?: string
  writer?: string
  theme?: string
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  // Seednote image composition (see CreateTaskRequest).
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable; both default true. Server ignores for non-article plans.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // 公众号发布署名 + 写作风格 key override。
  author?: string
  template_id?: string
}

export interface UpdatePlanRequest {
  cron_expr?: string
  prompt?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  visual_style?: string
  writer?: string
  theme?: string
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): leave-unchanged when omitted.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // 公众号发布署名 + 写作风格 key override。
  author?: string
  template_id?: string
}
