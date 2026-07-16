import type {
  AttachmentAdmissionPolicy,
  AttachmentRejection,
  InputAttachmentType,
  PromptAttachment,
} from '@/types/input-attachment'
import { AttachmentRejectionReason } from '@/types/input-attachment'

export { AttachmentRejectionReason }

export interface AdmittedPromptAttachment {
  file: File
  type: InputAttachmentType
}

export interface AttachmentAdmissionResult {
  accepted: AdmittedPromptAttachment[]
  rejected: AttachmentRejection[]
}

const MB = 1024 * 1024
const GENERIC_MIME_TYPES = new Set(['', 'application/octet-stream'])
const DOCUMENT_MIME_TYPES = new Set([
  'application/pdf',
  'application/msword',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/vnd.ms-powerpoint',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  'application/vnd.ms-excel',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  'application/csv',
  'application/json',
])

const EXTENSION_TYPES = new Map<string, readonly InputAttachmentType[]>([
  ['jpg', ['image']],
  ['jpeg', ['image']],
  ['png', ['image']],
  ['webp', ['image']],
  ['gif', ['image']],
  ['bmp', ['image']],
  ['mp3', ['audio']],
  ['wav', ['audio']],
  ['m4a', ['audio']],
  ['aac', ['audio']],
  ['ogg', ['audio']],
  ['mp4', ['video']],
  ['mov', ['video']],
  ['webm', ['video']],
  ['pdf', ['document']],
  ['doc', ['document']],
  ['docx', ['document']],
  ['ppt', ['document']],
  ['pptx', ['document']],
  ['xls', ['document']],
  ['xlsx', ['document']],
  ['json', ['document', 'text']],
  ['csv', ['text', 'document']],
  ['txt', ['text', 'document']],
  ['md', ['text', 'document']],
  ['markdown', ['text', 'document']],
])

function normalizedMime(file: File) {
  return file.type.split(';', 1)[0].trim().toLowerCase()
}

function fileExtension(name: string) {
  const dot = name.lastIndexOf('.')
  return dot >= 0 ? name.slice(dot + 1).toLowerCase() : ''
}

function typeFromMime(mime: string): InputAttachmentType | null {
  if (mime.startsWith('image/')) return 'image'
  if (mime.startsWith('audio/')) return 'audio'
  if (mime.startsWith('video/')) return 'video'
  if (mime.startsWith('text/')) return 'text'
  if (DOCUMENT_MIME_TYPES.has(mime)) return 'document'
  return null
}

export function classifyPromptAttachment(file: File): InputAttachmentType | null {
  const mime = normalizedMime(file)
  const extension = fileExtension(file.name)
  const extensionTypes = extension ? EXTENSION_TYPES.get(extension) : undefined

  if (extension && !extensionTypes) return null
  if (GENERIC_MIME_TYPES.has(mime)) return extensionTypes?.[0] ?? null

  const mimeType = typeFromMime(mime)
  if (!mimeType) return null
  if (extensionTypes && !extensionTypes.includes(mimeType)) return null
  return mimeType
}

function fileIdentity(file: Pick<File, 'name' | 'size' | 'lastModified' | 'type'>) {
  return JSON.stringify([file.name, file.size, file.lastModified, file.type])
}

function attachmentIdentity(attachment: PromptAttachment) {
  if (attachment.file) return fileIdentity(attachment.file)
  return JSON.stringify([
    attachment.fileName,
    attachment.size,
    attachment.lastModified ?? 0,
    attachment.contentType ?? '',
  ])
}

function maxBytes(type: InputAttachmentType) {
  return type === 'document' || type === 'text' ? 25 * MB : 50 * MB
}

export function admitPromptAttachments(
  current: readonly PromptAttachment[],
  incoming: readonly File[],
  policy: AttachmentAdmissionPolicy,
): AttachmentAdmissionResult {
  const accepted: AdmittedPromptAttachment[] = []
  const rejected: AttachmentRejection[] = []
  const seen = new Set(current.map(attachmentIdentity))
  const allowed = new Set(policy.allowedTypes)

  for (const file of incoming) {
    const identity = fileIdentity(file)
    if (seen.has(identity)) {
      rejected.push({ file, reason: AttachmentRejectionReason.Duplicate })
      continue
    }
    seen.add(identity)

    const type = classifyPromptAttachment(file)
    if (!type || !allowed.has(type)) {
      rejected.push({ file, reason: AttachmentRejectionReason.UnsupportedType })
      continue
    }
    if (file.size > maxBytes(type)) {
      rejected.push({ file, reason: AttachmentRejectionReason.TooLarge })
      continue
    }
    if (current.length + accepted.length >= policy.maxCount) {
      rejected.push({ file, reason: AttachmentRejectionReason.Capacity })
      continue
    }
    accepted.push({ file, type })
  }

  return { accepted, rejected }
}
