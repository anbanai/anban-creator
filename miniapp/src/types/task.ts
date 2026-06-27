export type TaskType = 'seednote' | 'article' | 'ecommerce'

// E-commerce package config carried on a task (mirrors server model.EcommerceConfig).
// `selected_modules` maps module key → quantity; package price is the
// Σ(module price × quantity), computed from /credits/pricing.ecommerce_module_prices.
export interface EcommerceTaskConfig {
  selected_modules?: Record<string, number>
  product_photos?: string[]
  target_platform?: string
  selling_points?: string
  language?: string
}

export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface Task {
  id: string
  type: TaskType
  title?: string
  topic?: string
  prompt: string
  status: TaskStatus
  progress?: number
  progress_log?: string
  latest_progress?: {
    stage?: string
    title?: string
    description?: string
    percent?: number
  }
  image_ratio?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  watermark?: boolean
  error: string | null
  // miniapp-specific convenience (some endpoints return a human message)
  error_message?: string | null
  plan_id: string | null
  project_id: string
  result: TaskResult
  published: boolean
  published_at: string | null
  workflow_status?: WorkflowStatus | string | null
  // Goal mode: condition is prepended to user prompt as /goal slash command.
  goal?: string
  goal_mode?: boolean
  template_id?: string
  // 公众号人设（task > template > project 解析链）：作者署名 + 写作风格模仿 + 可选头像
  author?: string
  author_style_intro?: string
  author_avatar_url?: string
  // E-commerce package config (only present for platform=ecommerce tasks)
  ecommerce?: EcommerceTaskConfig
  // 执行中累计消耗的美元成本（服务端 model.Task.TotalCostUSD）
  total_cost_usd?: number | null
  created_at: string
  started_at: string | null
  completed_at: string | null
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
  topic?: string
  prompt?: string
  project_id: string
  quantity?: number
  image_ratio?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image_url?: string
  style?: string
  writing_style?: string
  theme?: string
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  template_id?: string
  // 公众号人设 override（作者署名 + 写作风格模仿 + 可选头像）。空则兜底到模板/项目。
  author?: string
  author_style_intro?: string
  author_avatar_url?: string
  // Seednote image composition: cover always generated. Server ignores for non-seednote.
  has_content_image?: boolean
  has_tail_image?: boolean
  // E-commerce package fields (server ignores for non-ecommerce)
  product_photos?: string[]
  selected_modules?: Record<string, number>
  target_platform?: string
  selling_points?: string
  language?: string
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
