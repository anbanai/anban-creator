import { useCallback, useEffect, useRef, useState } from 'react'

import {
  directUploadResultToInputAttachment,
  uploadToOSS,
  type UploadToOSSOptions,
  type UploadToOSSResult,
} from '@/lib/direct-upload'
import type {
  AttachmentAdmissionPolicy,
  InputAttachment,
  PromptAttachment,
  PromptAttachmentAdapter,
} from '@/types/input-attachment'
import {
  admitPromptAttachments,
  type AttachmentAdmissionResult,
} from './attachment-admission'

export interface UsePromptAttachmentsOptions {
  adapter: PromptAttachmentAdapter
  policy: AttachmentAdmissionPolicy
  initialAttachments?: readonly InputAttachment[]
  upload?: (options: UploadToOSSOptions) => Promise<UploadToOSSResult>
  createId?: () => string
  createObjectURL?: (file: File) => string
  revokeObjectURL?: (url: string) => void
}

export interface PromptAttachmentsController {
  attachments: PromptAttachment[]
  addFiles: (files: readonly File[]) => AttachmentAdmissionResult
  retry: (id: string) => void
  remove: (id: string) => void
  clear: () => void
  uploading: boolean
  hasFailures: boolean
  toInputAttachments: () => InputAttachment[]
  localFiles: () => File[]
  previewSource: (id: string) => string | undefined
}

let nextPromptAttachmentId = 0

function defaultCreateId() {
  nextPromptAttachmentId += 1
  return `prompt-attachment-${nextPromptAttachmentId}`
}

function hydrateAttachment(attachment: InputAttachment, id: string): PromptAttachment {
  return {
    id,
    type: attachment.type,
    fileName: attachment.file_name ?? '附件',
    contentType: attachment.content_type,
    size: attachment.size ?? 0,
    status: 'uploaded',
    progress: 100,
    uploadId: attachment.upload_id,
    key: attachment.key,
    instruction: attachment.instruction,
    role: attachment.role,
  }
}

function serializeUploadedAttachment(attachment: PromptAttachment): InputAttachment | null {
  if (attachment.status !== 'uploaded' || !attachment.uploadId || !attachment.key) return null

  const serialized: InputAttachment = {
    type: attachment.type,
    upload_id: attachment.uploadId,
    key: attachment.key,
    file_name: attachment.fileName,
    size: attachment.size,
  }
  if (attachment.contentType) serialized.content_type = attachment.contentType
  if (attachment.instruction !== undefined) serialized.instruction = attachment.instruction
  if (attachment.role !== undefined) serialized.role = attachment.role
  return serialized
}

export function usePromptAttachments(options: UsePromptAttachmentsOptions): PromptAttachmentsController {
  const createIdRef = useRef(options.createId ?? defaultCreateId)
  const uploadRef = useRef(options.upload ?? uploadToOSS)
  const adapterRef = useRef(options.adapter)
  const policyRef = useRef(options.policy)
  const createObjectURLRef = useRef(options.createObjectURL ?? ((file: File) => URL.createObjectURL(file)))
  const revokeObjectURLRef = useRef(options.revokeObjectURL ?? ((url: string) => URL.revokeObjectURL(url)))
  const previewsRef = useRef(new Map<string, string>())
  const inheritedSourcesRef = useRef(new Map<string, InputAttachment>())
  const attemptsRef = useRef(new Map<string, number>())
  const activeUploadsRef = useRef(new Map<string, AbortController>())
  const mountedRef = useRef(true)
  const [attachments, setAttachments] = useState<PromptAttachment[]>(() => (
    (options.initialAttachments ?? []).map((attachment) => {
      const id = createIdRef.current()
      inheritedSourcesRef.current.set(id, { ...attachment })
      return hydrateAttachment(attachment, id)
    })
  ))
  const attachmentsRef = useRef(attachments)

  uploadRef.current = options.upload ?? uploadToOSS
  adapterRef.current = options.adapter
  policyRef.current = options.policy

  const updateAttachments = useCallback((
    updater: (current: PromptAttachment[]) => PromptAttachment[],
  ) => {
    if (!mountedRef.current) return
    const next = updater(attachmentsRef.current)
    if (next === attachmentsRef.current) return
    attachmentsRef.current = next
    setAttachments(() => next)
  }, [])

  const revokePreview = useCallback((id: string) => {
    const url = previewsRef.current.get(id)
    if (url === undefined) return
    previewsRef.current.delete(id)
    revokeObjectURLRef.current(url)
  }, [])

  const cancelUpload = useCallback((id: string) => {
    const controller = activeUploadsRef.current.get(id)
    if (!controller) return
    activeUploadsRef.current.delete(id)
    controller.abort()
  }, [])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      for (const controller of activeUploadsRef.current.values()) controller.abort()
      activeUploadsRef.current.clear()
      attemptsRef.current.clear()
      for (const id of [...previewsRef.current.keys()]) revokePreview(id)
    }
  }, [revokePreview])

  const startUpload = useCallback((id: string) => {
    const attachment = attachmentsRef.current.find((item) => item.id === id)
    const mode = adapterRef.current.mode
    if (!attachment?.file || mode === 'local') return

    cancelUpload(id)
    const controller = new AbortController()
    activeUploadsRef.current.set(id, controller)
    const attempt = (attemptsRef.current.get(id) ?? 0) + 1
    attemptsRef.current.set(id, attempt)
    updateAttachments((current) => current.map((item) => (
      item.id === id
        ? {
            ...item,
            status: 'uploading',
            progress: 0,
            error: undefined,
            uploadId: undefined,
            key: undefined,
          }
        : item
    )))

    const isCurrent = () => (
      mountedRef.current
      && attemptsRef.current.get(id) === attempt
      && attachmentsRef.current.some((item) => item.id === id)
    )
    const purpose = adapterRef.current.purpose
      ?? (mode === 'designer' ? 'designer_reference' : 'ai_entry_attachment')

    void uploadRef.current({
      purpose,
      file: attachment.file,
      signal: controller.signal,
      onProgress: (progress) => {
        if (!isCurrent()) return
        updateAttachments((current) => current.map((item) => (
          item.id === id ? { ...item, progress } : item
        )))
      },
    }).then((result) => {
      if (!isCurrent()) return
      const serialized = directUploadResultToInputAttachment(
        result,
        attachment.file as File,
        attachment.type,
        attachment.instruction,
      )
      updateAttachments((current) => current.map((item) => (
        item.id === id
          ? {
              ...item,
              status: 'uploaded',
              progress: 100,
              error: undefined,
              uploadId: serialized.upload_id,
              key: serialized.key,
              contentType: serialized.content_type,
              size: serialized.size ?? item.size,
            }
          : item
      )))
    }).catch((error: unknown) => {
      if (!isCurrent()) return
      updateAttachments((current) => current.map((item) => (
        item.id === id
          ? {
              ...item,
              status: 'failed',
              progress: 0,
              error: error instanceof Error ? error.message : '上传失败，请重试',
            }
          : item
      )))
    }).finally(() => {
      if (activeUploadsRef.current.get(id) === controller) {
        activeUploadsRef.current.delete(id)
      }
    })
  }, [cancelUpload, updateAttachments])

  const addFiles = useCallback((files: readonly File[]) => {
    const admission = admitPromptAttachments(
      attachmentsRef.current,
      files,
      policyRef.current,
    )
    if (admission.accepted.length === 0) return admission

    const added = admission.accepted.map(({ file, type }): PromptAttachment => {
      const id = createIdRef.current()
      try {
        previewsRef.current.set(id, createObjectURLRef.current(file))
      } catch {
        // Preview creation is optional; file admission and upload remain valid.
      }
      return {
        id,
        type,
        file,
        fileName: file.name,
        contentType: file.type || undefined,
        size: file.size,
        lastModified: file.lastModified,
        status: 'queued',
        progress: 0,
      }
    })
    updateAttachments((current) => [...current, ...added])

    if (adapterRef.current.mode !== 'local') {
      for (const attachment of added) startUpload(attachment.id)
    }
    return admission
  }, [startUpload, updateAttachments])

  const retry = useCallback((id: string) => {
    const attachment = attachmentsRef.current.find((item) => item.id === id)
    if (attachment?.status !== 'failed') return
    startUpload(id)
  }, [startUpload])

  const remove = useCallback((id: string) => {
    if (!attachmentsRef.current.some((item) => item.id === id)) return
    cancelUpload(id)
    attemptsRef.current.delete(id)
    inheritedSourcesRef.current.delete(id)
    revokePreview(id)
    updateAttachments((current) => current.filter((item) => item.id !== id))
  }, [cancelUpload, revokePreview, updateAttachments])

  const clear = useCallback(() => {
    if (attachmentsRef.current.length === 0) return
    for (const id of [...activeUploadsRef.current.keys()]) cancelUpload(id)
    attemptsRef.current.clear()
    inheritedSourcesRef.current.clear()
    for (const id of attachmentsRef.current.map((attachment) => attachment.id)) revokePreview(id)
    updateAttachments(() => [])
  }, [cancelUpload, revokePreview, updateAttachments])

  const toInputAttachments = useCallback(() => attachmentsRef.current.flatMap((attachment) => {
    if (attachment.status !== 'uploaded') return []
    const inherited = inheritedSourcesRef.current.get(attachment.id)
    if (inherited) {
      if (!inherited.upload_id || !inherited.key) return [{ ...inherited }]
      const serialized = serializeUploadedAttachment(attachment)
      return serialized ? [serialized] : []
    }
    if (adapterRef.current.mode === 'local' && attachment.file) return []
    const serialized = serializeUploadedAttachment(attachment)
    return serialized ? [serialized] : []
  }), [])

  const localFiles = useCallback(() => {
    if (adapterRef.current.mode !== 'local') return []
    return attachmentsRef.current.flatMap((attachment) => attachment.file ? [attachment.file] : [])
  }, [])

  const previewSource = useCallback((id: string) => previewsRef.current.get(id), [])

  return {
    attachments,
    addFiles,
    retry,
    remove,
    clear,
    uploading: attachments.some((attachment) => attachment.status === 'uploading'),
    hasFailures: attachments.some((attachment) => attachment.status === 'failed'),
    toInputAttachments,
    localFiles,
    previewSource,
  }
}
