import type { MontageInput } from './montage'
import type { InputAttachment } from './input-attachment'
import type { ReferenceAssetView, ReferenceImageSelection } from './asset'

export type PlanType = 'seednote' | 'article' | 'montage'
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
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceAssetView | null
  input_attachments?: InputAttachment[]
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable. Both default true; spawned article tasks inherit them.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  montage_input?: MontageInput
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  cron_expr: string
  prompt?: string
  project_id?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  input_attachments?: InputAttachment[]
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  // Seednote image composition (see CreateTaskRequest).
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): cover + content images each independently
  // toggleable; both default true. Server ignores for non-article plans.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  montage_input?: MontageInput
}

export interface UpdatePlanRequest {
  cron_expr?: string
  prompt?: string
  image_model_key?: string
  skip_reference_image?: boolean
  reference_image?: ReferenceImageSelection | null
  input_attachments?: InputAttachment[]
  watermark?: boolean
  goal?: string
  goal_mode?: boolean
  has_content_image?: boolean
  has_tail_image?: boolean
  // Article image toggles (公众号文章): leave-unchanged when omitted.
  article_with_cover?: boolean
  article_with_content_images?: boolean
  montage_input?: MontageInput
}
