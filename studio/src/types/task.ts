export type TaskType = 'seednote' | 'article' | 'ecommerce'

// E-commerce package config carried on a task (server model.EcommerceConfig).
// `selected_modules` maps module key → quantity; the package price is the
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
  error: string | null
  plan_id: string | null
  project_id: string
  result: TaskResult
  published: boolean
  published_at: string | null
  // Publish-approval gate state (Batch 4A). Empty unless the owning project has
  // require_publish_approval + enable_publishing AND the task completed with
  // draft data: "pending" = held for human review, "approved"/"rejected" =
  // acted on. Drives the approval card on the task detail page.
  publish_approval_state?: PublishApprovalState
  workflow_status?: WorkflowStatus | string | null
  // Goal mode: condition is prepended to user prompt as /goal slash command;
  // the loop runs entirely inside Claude Code, server observes only the result.
  goal?: string
  goal_mode?: boolean
  template_id?: string
  // 公众号人设（与项目/模板同链解析：task > template > project）：作者署名 + 写作风格
  // 模仿（自由文本） + 可选人设头像，正交于 style/writing_style/theme。
  author?: string
  author_style_intro?: string
  author_avatar_url?: string
  // E-commerce package config (only present for platform=ecommerce tasks).
  ecommerce?: EcommerceTaskConfig
  // 执行中累计消耗的美元成本（服务端 model.Task.TotalCostUSD）。运行/失败/完成
  // 态可能填充；刚创建的 pending 任务为空。用于取消对话框展示「已消耗不退还」。
  total_cost_usd?: number | null
  created_at: string
  started_at: string
  completed_at: string
}

// Publish-approval gate state (mirrors server model.PublishApprovalState*).
export type PublishApprovalState = '' | 'pending' | 'approved' | 'rejected'

export interface TaskResult {
  files: TaskFile[] | null
  output: string
}

// Bulk operation per-task outcome (mirrors server handler.bulkTaskResult).
// OK=false tasks carry a machine-readable Reason (not_found / forbidden /
// not_cancellable / not_retryable / running_cancel_first / insufficient_credits / failed).
export interface BulkTaskResult {
  id: string
  ok: boolean
  reason?: string
  new_task_id?: string // retry only: the freshly created task id
}

// Bulk operation summary (mirrors server handler.bulkTasksResponse). Best-effort:
// Succeeded + Skipped = Total; the UI toasts the counts and details on demand.
export interface BulkTasksResponse {
  total: number
  succeeded: number
  skipped: number
  results: BulkTaskResult[]
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
  // 公众号人设 override（作者署名 + 写作风格模仿 + 可选头像）。空则按解析链兜底到
  // 模板/项目；非空即覆盖。
  author?: string
  author_style_intro?: string
  author_avatar_url?: string
  // Seednote image composition: cover always generated. Server ignores for non-seednote.
  has_content_image?: boolean
  has_tail_image?: boolean
  // E-commerce package fields (server ignores for non-ecommerce). Product photos
  // are server-owned URLs returned by /files/upload; the executor materializes
  // them into the agent workspace.
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
