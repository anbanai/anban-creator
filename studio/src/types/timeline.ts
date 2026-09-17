import type { TaskType, TaskStatus, TaskLifecycleStageState } from './task'
import type { PlanType, PlanStatus } from './plan'

export type TimelineItemType = 'task' | 'plan'

export interface TimelineItem {
  id: string
  type: TimelineItemType
  content_type: TaskType | PlanType
  title: string
  status: TaskStatus | PlanStatus
  project_id?: string
  project_name?: string
  platform?: string
  scheduled_at: string
  completed_at: string
  created_at: string
  plan_id?: number
  task_id?: string
  current_stage?: {
    title: string
    state: TaskLifecycleStageState
  }
  error?: string
}

export interface TimelineResponse {
  items: TimelineItem[]
}
