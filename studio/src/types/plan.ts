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
  channel_id: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  style?: string
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // template_id records the template selected when creating the plan; spawned
  // tasks inherit it so the agent can surface the template's content scaffold
  // via get_channel_profile(task_id).
  template_id?: string
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  cron_expr: string
  prompt?: string
  channel_id?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  style?: string
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  // Seednote image composition (see CreateTaskRequest).
  has_content_image?: boolean
  has_tail_image?: boolean
  template_id?: string
}

export interface UpdatePlanRequest {
  cron_expr?: string
  prompt?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  style?: string
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  template_id?: string
}
