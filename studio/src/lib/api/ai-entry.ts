import { http, unwrap } from '@/lib/http-client'
import type { AgentExecutionProfileID, Task } from '@/types'
import type {
  InputAttachment,
  InputAttachmentType,
} from '@/types/input-attachment'

export type AIEntryStatus = 'created' | 'needs_configuration' | 'error'
export type AIEntryAttachment = InputAttachment
export type AIEntryAttachmentType = InputAttachmentType
export type { InputAttachment, InputAttachmentType }

export interface AIEntrySubmitRequest {
  channel: 'studio' | 'ilink' | string
  project_id: string
  text: string
  attachments?: AIEntryAttachment[]
  execution_profile: AgentExecutionProfileID
  quantity?: number
  image_ratio?: string
  image_capability_key?: string
}

export interface AIEntrySubmitResult {
  status: AIEntryStatus
  tasks?: Task[]
  message?: string
  action_url?: string
}

export const aiEntryApi = {
  submit: (data: AIEntrySubmitRequest) =>
    unwrap<AIEntrySubmitResult>(http.post('/ai-entry/submit', data)),
}
