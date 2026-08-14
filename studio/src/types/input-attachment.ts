export type InputAttachmentType =
  | 'image'
  | 'audio'
  | 'video'
  | 'document'
  | 'text'

export interface InputAttachment {
  type: InputAttachmentType
  /** Server-verified asset identity for legacy/project-backed materials. */
  asset_id?: string
  /** Legacy server-owned file URL. New composer attachments use upload_id + key. */
  url?: string
  /** Legacy inline text attachment. New composer files remain key-backed. */
  text?: string
  file_name?: string
  content_type?: string
  size?: number
  role?: string
  upload_id?: string
  key?: string
  instruction?: string
}

export type PromptAttachmentStatus =
  | 'queued'
  | 'uploading'
  | 'uploaded'
  | 'failed'

export interface PromptAttachment {
  id: string
  type: InputAttachmentType
  file?: File
  fileName: string
  contentType?: string
  size: number
  lastModified?: number
  status: PromptAttachmentStatus
  progress: number
  error?: string
  uploadId?: string
  key?: string
  assetId?: string
  instruction?: string
  role?: string
}

export interface AgentPromptValue {
  prompt: string
  /** Composer source of truth. Pass this array to usePromptAttachments controlled mode. */
  attachments: PromptAttachment[]
}

export interface AttachmentAdmissionPolicy {
  allowedTypes: readonly InputAttachmentType[]
  maxCount: number
  maxBytes?: Partial<Record<InputAttachmentType, number>>
}

export enum AttachmentRejectionReason {
  Duplicate = 'duplicate',
  UnsupportedType = 'unsupported_type',
  TooLarge = 'too_large',
  Capacity = 'capacity',
}

export interface AttachmentRejection {
  file: File
  reason: AttachmentRejectionReason
}

export type PromptAttachmentAdapter =
  | { mode: 'direct'; purpose?: 'ai_entry_attachment' }
  | { mode: 'local'; purpose?: never }
