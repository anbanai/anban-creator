export type PlanType = 'rednote' | 'article' | 'xls'
export type PlanStatus = 'active' | 'paused' | 'completed'

export interface Plan {
  id: string
  type: PlanType
  title: string
  description: string
  cron_expr: string
  topic_hint: string
  status: PlanStatus
  next_run_at: string
  channel_id: string
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  title: string
  description?: string
  cron_expr: string
  topic_hint?: string
  channel_id?: string
}

export interface UpdatePlanRequest {
  title?: string
  description?: string
  cron_expr?: string
  topic_hint?: string
}
