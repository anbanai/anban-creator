import type { MontageInput } from './montage'
import type { InputAttachment } from './input-attachment'
import type { ReferenceAssetView, ReferenceImageSelection } from './asset'
import type { AgentExecutionProfileID, AgentProfileSnapshot } from './agent-profile'

export type TaskType = 'seednote' | 'article' | 'moments' | 'viral_analysis' | 'ecommerce' | 'montage'

// E-commerce package config carried on a task (server model.EcommerceConfig).
// `selected_modules` maps module key → quantity. Delivery module selection
// affects later MCP image/vision usage; task admission uses the fixed ecommerce SKU.
export interface EcommerceTaskConfig {
  selected_modules?: Record<string, number>
  product_photos?: string[]
  target_platform?: string
  selling_points?: string
  language?: string
}
export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

// Legacy per-task overrides kept for older rows. New tasks use project_snapshot.
export interface StyleOverrides {
  visual_style?: string
  writer?: string
  author?: string
  theme?: string
}

export interface ProjectSnapshot {
  project_name?: string
  platform?: TaskType
  instructions?: string
  keywords?: string
  visual_style?: string
  reference_image_asset_id?: string
  image_ratio?: string
  writer?: string
  theme?: string
  author?: string
  ecommerce_defaults?: {
    default_selected_modules?: Record<string, number>
    target_platform?: string
    brand_brief?: string
    image_capability_key?: string
  }
}

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
  image_capability_key?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceAssetView | null
  input_attachments?: InputAttachment[]
  watermark?: boolean
  // Seednote image composition persisted with the task.
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image composition persisted with the task.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // Failure detail persisted by server model.Task.ErrorMessage; omitted when empty.
  error_message?: string
  plan_id?: string | null
  project_id: string
  execution_profile: AgentExecutionProfileID
  agent_profile_snapshot?: AgentProfileSnapshot
  agent_profile_fingerprint?: string
  result?: string | null
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
  overrides?: StyleOverrides
  project_snapshot?: ProjectSnapshot
  // E-commerce package config (only present for platform=ecommerce tasks).
  ecommerce?: EcommerceTaskConfig
  montage_input?: MontageInput
  billing_quote_id?: string
  billing_catalog_id?: string
  billing_sku_id?: string
  billing_pricing_tier?: 'free' | 'pro' | 'enterprise'
  billing_charge_id?: string
  billing_price_credits: number
  billing_total_credits?: number
  billing_charge_details?: TaskBillingChargeDetail[]
  billing_terminal_reason?: string
  // Where the task runs (mirrors server model.ExecutionTarget*):
  // ''/'cloud' = cloud Asynq/Docker; 'local' = awaiting a desktop local-executor
  // claim; 'local_claimed' = a desktop claimed it and is running it on the user's
  // machine. Drives the "本地运行中" badge on task cards. Absent on older rows.
  execution_target?: ExecutionTarget
  created_at: string
  started_at: string
  completed_at: string
}

export interface TaskBillingChargeDetail {
  id?: string
  charge_kind: 'task' | 'operation' | 'reversal'
  policy?: string
  sku_id?: string
  credits: number
  pricing_tier?: 'free' | 'pro' | 'enterprise'
  list_price_credits?: number
  discount_credits?: number
  resource_type?: string
  resource_id?: string
  tool_call_id?: string
  reversal_of_id?: string
  created_at?: string
}

// Publish-approval gate state (mirrors server model.PublishApprovalState*).
export type PublishApprovalState = '' | 'pending' | 'approved' | 'rejected'

export interface ReferenceUsageSummaryData {
  version: '1.0'
  inputs: Array<{
    attachment_index: number
    file_name?: string
    url?: string
    instruction?: string
    status: 'used' | 'excluded' | 'analysis_failed'
    decision_summary: string
    analysis_attempts: number
    warnings?: string[]
  }>
  outputs: Array<{
    file_name: string
    references: Array<{
      attachment_index: number
      purpose: string
    }>
    generation_attempts: number
    verification: {
      status: 'passed' | 'warning' | 'failed'
      summary: string
    }
    provider?: string
    model?: string
    selection_reason?: string
  }>
  warnings?: string[]
  model_fallback_reason?: string
}

// Bulk operation per-task outcome (mirrors server handler.bulkTaskResult).
// OK=false tasks carry a machine-readable Reason (not_found / forbidden /
// not_cancellable / not_cloneable / running_cancel_first / insufficient_credits / failed).
export interface BulkTaskResult {
  id: string
  ok: boolean
  reason?: string
  new_task_id?: string // clone only: the freshly created task id
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
  execution_id?: string
  state?: 'published' | 'collected'
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
  execution_profile: AgentExecutionProfileID
  topic?: string
  prompt?: string
  project_id: string
  quantity?: number
  image_ratio?: string
  image_capability_key?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  input_attachments?: InputAttachment[]
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  // Seednote image composition: cover always generated. Server ignores for non-seednote.
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable. Both default true. Server ignores for non-article task types.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  // E-commerce package fields (server ignores for non-ecommerce). Product photos
  // are server-owned storage URLs returned by direct upload; the executor
  // materializes them into the agent workspace.
  product_photos?: string[]
  selected_modules?: Record<string, number>
  target_platform?: string
  selling_points?: string
  language?: string
  montage_input?: MontageInput
}

// Cloning creates another task through the same complete creation contract.
export type CloneTaskRequest = CreateTaskRequest

// Where a task executes (mirrors server model.ExecutionTarget* constants).
export type ExecutionTarget = '' | 'cloud' | 'local' | 'local_claimed'

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
