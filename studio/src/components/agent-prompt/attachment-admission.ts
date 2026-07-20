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
export const DEFAULT_ATTACHMENT_MAX_BYTES: Record<InputAttachmentType, number> = {
  image: 50 * MB,
  audio: 50 * MB,
  video: 50 * MB,
  document: 25 * MB,
  text: 25 * MB,
}
export const GENERAL_AGENT_ATTACHMENT_POLICY: AttachmentAdmissionPolicy = {
  allowedTypes: ['image', 'audio', 'video', 'document', 'text'],
  maxCount: 5,
  maxBytes: { ...DEFAULT_ATTACHMENT_MAX_BYTES },
}
const GENERIC_MIME_TYPES = new Set(['', 'application/octet-stream'])
type AttachmentTypeRule = {
  type: InputAttachmentType
  mimeTypes: readonly string[]
}

const EXTENSION_RULES = new Map<string, AttachmentTypeRule>([
  ['jpg', { type: 'image', mimeTypes: ['image/jpeg', 'image/jpg', 'image/pjpeg'] }],
  ['jpeg', { type: 'image', mimeTypes: ['image/jpeg', 'image/jpg', 'image/pjpeg'] }],
  ['png', { type: 'image', mimeTypes: ['image/png', 'image/x-png'] }],
  ['webp', { type: 'image', mimeTypes: ['image/webp'] }],
  ['gif', { type: 'image', mimeTypes: ['image/gif'] }],
  ['bmp', { type: 'image', mimeTypes: ['image/bmp', 'image/x-ms-bmp'] }],
  ['mp3', { type: 'audio', mimeTypes: ['audio/mpeg', 'audio/mp3'] }],
  ['wav', { type: 'audio', mimeTypes: ['audio/wav', 'audio/x-wav'] }],
  ['m4a', { type: 'audio', mimeTypes: ['audio/mp4', 'audio/x-m4a'] }],
  ['aac', { type: 'audio', mimeTypes: ['audio/aac'] }],
  ['ogg', { type: 'audio', mimeTypes: ['audio/ogg', 'application/ogg'] }],
  ['mp4', { type: 'video', mimeTypes: ['video/mp4'] }],
  ['mov', { type: 'video', mimeTypes: ['video/quicktime'] }],
  ['webm', { type: 'video', mimeTypes: ['video/webm'] }],
  ['pdf', { type: 'document', mimeTypes: ['application/pdf'] }],
  ['doc', { type: 'document', mimeTypes: ['application/msword'] }],
  ['docx', { type: 'document', mimeTypes: ['application/vnd.openxmlformats-officedocument.wordprocessingml.document'] }],
  ['ppt', { type: 'document', mimeTypes: ['application/vnd.ms-powerpoint'] }],
  ['pptx', { type: 'document', mimeTypes: ['application/vnd.openxmlformats-officedocument.presentationml.presentation'] }],
  ['xls', { type: 'document', mimeTypes: ['application/vnd.ms-excel'] }],
  ['xlsx', { type: 'document', mimeTypes: ['application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'] }],
  ['json', { type: 'document', mimeTypes: ['application/json', 'text/json'] }],
  ['csv', { type: 'text', mimeTypes: ['text/csv', 'application/csv'] }],
  ['txt', { type: 'text', mimeTypes: ['text/plain'] }],
  ['md', { type: 'text', mimeTypes: ['text/markdown', 'text/plain'] }],
  ['markdown', { type: 'text', mimeTypes: ['text/markdown', 'text/plain'] }],
])

const MIME_TYPES = new Map<string, InputAttachmentType>()
for (const rule of EXTENSION_RULES.values()) {
  for (const mimeType of rule.mimeTypes) MIME_TYPES.set(mimeType, rule.type)
}

function normalizedMime(contentType: string) {
  return contentType.split(';', 1)[0].trim().toLowerCase()
}

function fileExtension(name: string) {
  const dot = name.lastIndexOf('.')
  return dot >= 0 ? name.slice(dot + 1).toLowerCase() : ''
}

export function classifyPromptAttachmentMetadata(
  fileName: string,
  contentType: string,
): InputAttachmentType | null {
  const mime = normalizedMime(contentType)
  const extension = fileExtension(fileName)
  const extensionRule = extension ? EXTENSION_RULES.get(extension) : undefined

  if (extension && !extensionRule) return null
  if (GENERIC_MIME_TYPES.has(mime)) return extensionRule?.type ?? null

  const mimeType = MIME_TYPES.get(mime) ?? null
  if (!mimeType) return null
  if (extensionRule && !extensionRule.mimeTypes.includes(mime)) return null
  return mimeType
}

export function classifyPromptAttachment(file: File): InputAttachmentType | null {
  return classifyPromptAttachmentMetadata(file.name, file.type)
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
    const maxBytes = policy.maxBytes?.[type] ?? DEFAULT_ATTACHMENT_MAX_BYTES[type]
    if (file.size > maxBytes) {
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
