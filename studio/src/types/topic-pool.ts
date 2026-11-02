export type TopicPoolStatus = 'unused' | 'used'

export interface TopicPool {
  id: number
  user_id: string
  project_id: string
  topic: string
  status: TopicPoolStatus
  source_digest_id?: string
  source_suggestion_id?: string
  source_label?: string
  task_id?: string
  used_at?: string
  created_at: string
  updated_at: string
}
