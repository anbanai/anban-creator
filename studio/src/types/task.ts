export type TaskType = 'seednote' | 'article'
export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface Task {
  id: string
  type: TaskType
  title?: string
  prompt: string
  status: TaskStatus
  progress?: number
  progress_log?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  error: string | null
  plan_id: string | null
  channel_id: string
  result: TaskResult
  published: boolean
  published_at: string | null
  workflow_status?: WorkflowStatus | string | null
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
  skip_reference_image?: boolean
  reference_image_url?: string
}

export interface WorkflowStatus {
  version: string
  current_stage: string
  stages: WorkflowStage[]
  warnings?: WorkflowWarning[]
  review?: WorkflowReview | null
}

export interface WorkflowStage {
  key: string
  label: string
  status: 'pending' | 'running' | 'completed' | 'warning' | 'failed' | string
  artifact_paths?: string[]
  error?: string
}

export interface WorkflowWarning {
  code: string
  message: string
}

export interface WorkflowReview {
  overall_score: number
  readiness: string
  scores?: Record<string, number>
  strengths?: string[]
  risks?: string[]
  next_actions?: string[]
  metadata?: Record<string, string>
}
