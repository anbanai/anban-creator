export type TopicPoolStatus = 'unused' | 'used'

export interface TopicPool {
  id: number
  user_id: string
  project_id: string
  topic: string
  status: TopicPoolStatus
  task_id?: string
  used_at?: string
  created_at: string
  updated_at: string
}
