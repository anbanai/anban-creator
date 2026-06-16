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
  goal_max_attempts?: number
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
}
