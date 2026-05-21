export type PlanType = 'seednote' | 'article'
export type PlanStatus = 'active' | 'paused' | 'completed'

export interface Plan {
  id: string
  type: PlanType
  title: string
  description: string
  cron_expr: string
  prompt: string
  status: PlanStatus
  next_run_at: string
  channel_id: string
  skip_reference_image?: boolean
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  cron_expr: string
  prompt?: string
  channel_id?: string
  skip_reference_image?: boolean
}

export interface UpdatePlanRequest {
  cron_expr?: string
  prompt?: string
  skip_reference_image?: boolean
}
