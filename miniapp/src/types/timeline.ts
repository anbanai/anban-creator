import type { TaskType, TaskStatus } from './task'
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
  progress?: number
  error?: string
}

export interface TimelineResponse {
  items: TimelineItem[]
}
