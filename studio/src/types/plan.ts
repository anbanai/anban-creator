import type { HypitInput } from './hypit'
import type { MontageInput } from './montage'
import type { InputAttachment } from './input-attachment'
import type { ReferenceAssetView, ReferenceImageSelection } from './asset'
import type { AgentExecutionProfileID } from './agent-profile'

export type PlanType = 'seednote' | 'article' | 'montage' | 'hypit'
export type PlanStatus = 'active' | 'paused' | 'completed'

export interface Plan {
  id: string
  type: PlanType
  title: string
  description: string
  cron_expr: string
  prompt: string
  topic_hint?: string
  status: PlanStatus
  next_run_at: string
  project_id: string
  execution_profile: AgentExecutionProfileID
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceAssetView | null
  input_attachments?: InputAttachment[]
  agent_input?: Record<string, unknown>
  watermark?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable. Both default true; spawned article tasks inherit them.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  hypit_input?: HypitInput
  montage_input?: MontageInput
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  execution_profile: AgentExecutionProfileID
  cron_expr: string
  prompt?: string
  project_id?: string
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  input_attachments?: InputAttachment[]
  agent_input?: Record<string, unknown>
  watermark?: boolean
  // Seednote image composition (see CreateTaskRequest).
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable; both default true. Server ignores for non-article plans.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  hypit_input?: HypitInput
  montage_input?: MontageInput
}

export interface UpdatePlanRequest {
  execution_profile: AgentExecutionProfileID
  cron_expr?: string
  prompt?: string
  image_capability_key?: string
  image_ratio?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  input_attachments?: InputAttachment[]
  agent_input?: Record<string, unknown>
  watermark?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): leave-unchanged when omitted.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  hypit_input?: HypitInput
  montage_input?: MontageInput
}
