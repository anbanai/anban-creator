export type InputAttachmentType =
  | 'image'
  | 'audio'
  | 'video'
  | 'document'
  | 'text'

export interface InputAttachment {
  type: InputAttachmentType
  url?: string
  text?: string
  file_name?: string
  content_type?: string
  size?: number
  role?: string
  upload_id?: string
  key?: string
  instruction?: string
}
