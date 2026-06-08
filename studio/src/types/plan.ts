export type PlanType = 'seednote' | 'article' | 'xls'
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
  skip_reference_image?: boolean
  reference_image_url?: string
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  cron_expr: string
  prompt?: string
  channel_id?: string
  skip_reference_image?: boolean
  reference_image_url?: string
}

export interface UpdatePlanRequest {
  cron_expr?: string
  prompt?: string
  skip_reference_image?: boolean
  reference_image_url?: string
}
