import { http, unwrap } from '@/lib/http-client'
import type { ExecutionTarget, Task } from '@/types'

export type AIEntryStatus = 'created' | 'needs_configuration' | 'error'
export type AIEntryAttachmentType = 'image' | 'audio' | 'video' | 'document' | 'text'

export interface AIEntryAttachment {
  type: AIEntryAttachmentType
  url?: string
  text?: string
  file_name?: string
  content_type?: string
  size?: number
  role?: string
  upload_id?: string
  key?: string
}

export interface AIEntrySubmitRequest {
  channel: 'studio' | 'ilink' | string
  project_id: string
  text: string
  attachments?: AIEntryAttachment[]
  execution_target?: ExecutionTarget
}

export interface AIEntrySubmitResult {
  status: AIEntryStatus
  task?: Task
  message?: string
  action_url?: string
}

export const aiEntryApi = {
  submit: (data: AIEntrySubmitRequest) =>
    unwrap<AIEntrySubmitResult>(http.post('/ai-entry/submit', data)),
}
