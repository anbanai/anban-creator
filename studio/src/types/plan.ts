import type { InputAttachment } from './input-attachment'
import type { ReferenceAssetView, ReferenceImageSelection } from './asset'
import type { AgentExecutionProfileID } from './agent-profile'

export type PlanStatus = 'active' | 'paused' | 'completed'

export type PlanEntryStatus = 'active' | 'paused' | 'failed'

export interface PlanEntry {
  id: string
  plan_id: string
  agent_id: string
  channel: string
  task_kind: string
  execution_profile: AgentExecutionProfileID
  agent_input?: Record<string, unknown>
  image_defaults?: Record<string, unknown>
  status: PlanEntryStatus
  created_at: string
  updated_at: string
}

export interface Plan {
  id: string
  title: string
  description: string
  cron_expr: string
  prompt: string
  topic_hint?: string
  status: PlanStatus
  next_run_at: string
  project_id: string
  execution_profile: AgentExecutionProfileID
  agent_ids?: string[]
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceAssetView | null
  portrait_reference_image?: ReferenceAssetView | null
  cover_use_portrait?: boolean
  input_attachments?: InputAttachment[]
  watermark?: boolean
  created_at: string
  updated_at: string
  entries?: PlanEntry[]
}

export interface CreatePlanRequest {
  project_id: string
  agent_ids: string[]
  execution_profile?: AgentExecutionProfileID
  cron_expr: string
  prompt?: string
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  portrait_reference_image?: ReferenceImageSelection | null
  cover_use_portrait?: boolean
  input_attachments?: InputAttachment[]
  watermark?: boolean
}

export interface UpdatePlanRequest {
  agent_ids: string[]
  execution_profile?: AgentExecutionProfileID
  cron_expr?: string
  prompt?: string
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  portrait_reference_image?: ReferenceImageSelection | null
  cover_use_portrait?: boolean
  input_attachments?: InputAttachment[]
  watermark?: boolean
}
