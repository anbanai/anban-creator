export type TaskType = 'rednote' | 'article' | 'xls'
export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface Task {
  id: string
  type: TaskType
  title?: string
  prompt: string
  status: TaskStatus
  progress?: number
  error: string | null
  plan_id: string | null
  channel_id: string
  result: TaskResult
  published: boolean
  published_at: string | null
  created_at: string
  started_at: string
  completed_at: string
}

export interface TaskResult {
  files: TaskFile[] | null
  output: string
}

export interface TaskFile {
  id: string
  task_id: string
  role: string
  file_name: string
  mime_type: string
  file_size: number
  url: string
  media_id?: string
  wechat_url?: string
  created_at: string
}

export interface CreateTaskRequest {
  type: TaskType
  prompt?: string
  channel_id: string
  quantity?: number
  image_ratio?: string
  generate_video?: boolean
}
