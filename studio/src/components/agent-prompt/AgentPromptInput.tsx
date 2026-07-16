import {
  useCallback,
  useRef,
  useState,
  type ChangeEvent,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import {
  ArrowUpIcon,
  FilePenLineIcon,
  LoaderCircleIcon,
  PlusIcon,
  RotateCcwIcon,
  Trash2Icon,
  type LucideIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupTextarea,
} from '@/components/ui/input-group'
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Progress } from '@/components/ui/progress'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type {
  AgentPromptValue,
  AttachmentAdmissionPolicy,
  AttachmentRejection,
  InputAttachmentType,
  PromptAttachment,
} from '@/types/input-attachment'
import {
  AttachmentRejectionReason,
  classifyPromptAttachment,
} from './attachment-admission'
import { useAgentPromptDropTarget } from './AgentPromptDropProvider'
import type { PromptAttachmentsController } from './usePromptAttachments'

const TYPE_LABELS: Record<InputAttachmentType, string> = {
  image: '图片',
  audio: '音频',
  video: '视频',
  document: '文档',
  text: '文本',
}

const STATUS_LABELS: Record<PromptAttachment['status'], string> = {
  queued: '等待中',
  uploading: '上传中',
  uploaded: '已上传',
  failed: '失败',
}

const REJECTION_LABELS: Record<AttachmentRejectionReason, string> = {
  [AttachmentRejectionReason.Duplicate]: '重复文件',
  [AttachmentRejectionReason.UnsupportedType]: '不支持的类型',
  [AttachmentRejectionReason.TooLarge]: '文件过大',
  [AttachmentRejectionReason.Capacity]: '超出数量限制',
}

const INPUT_ACCEPT_BY_TYPE: Record<InputAttachmentType, string> = {
  image: 'image/*',
  audio: 'audio/*',
  video: 'video/*',
  document: '.pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx',
  text: 'text/*,.md,.txt',
}

export interface AgentPromptInputProps {
  value: AgentPromptValue
  onChange: (value: AgentPromptValue) => void
  onSubmit: (value: AgentPromptValue) => void | Promise<void>
  /** Operations and upload lifecycle only; value.attachments is the render/submit source. */
  attachmentController: PromptAttachmentsController
  attachmentPolicy: AttachmentAdmissionPolicy
  contextBar?: ReactNode
  leadingTools?: ReactNode
  trailingTools?: ReactNode
  status?: ReactNode
  placeholder?: string
  submitLabel?: string
  submitIcon?: LucideIcon
  submitting?: boolean
  disabled?: boolean
  submitDisabled?: boolean
  autoFocus?: boolean
  ariaLabel?: string
  acceptedTypesLabel?: string
  onAttachmentRejected?: (rejections: AttachmentRejection[]) => void
  onSubmitError?: (error: unknown) => void
}

function formatFileSize(bytes: number) {
  if (bytes < 1_000) return `${bytes} B`
  if (bytes < 1_000_000) return `${Number((bytes / 1_000).toFixed(1))} KB`
  return `${Number((bytes / 1_000_000).toFixed(1))} MB`
}

function rejectionAnnouncement(rejections: readonly AttachmentRejection[]) {
  if (rejections.length === 0) return ''
  const counts = new Map<AttachmentRejectionReason, number>()
  for (const rejection of rejections) {
    counts.set(rejection.reason, (counts.get(rejection.reason) ?? 0) + 1)
  }
  const reasons = [...counts].map(([reason, count]) => `${REJECTION_LABELS[reason]} ${count}`).join('，')
  return `拒绝 ${rejections.length} 个附件：${reasons}`
}

interface AttachmentRowProps {
  attachment: PromptAttachment
  controller: PromptAttachmentsController
  disabled: boolean
}

function AttachmentRow({
  attachment,
  controller,
  disabled,
}: AttachmentRowProps) {
  const transient = attachment.status === 'queued' || attachment.status === 'uploading'
  const failed = attachment.status === 'failed'
  return (
    <div
      data-slot="agent-prompt-attachment"
      className="flex min-h-10 min-w-0 items-center gap-2 rounded-md border border-border px-2 py-1.5"
    >
      <div className="flex min-w-0 flex-1 flex-col items-start gap-0.5">
        <span
          data-slot="agent-prompt-attachment-name"
          className="w-full min-w-0 truncate text-sm font-medium"
        >
          {attachment.fileName}
        </span>
        <span
          data-slot="agent-prompt-attachment-meta"
          role={failed ? 'alert' : transient ? 'status' : undefined}
          aria-label={failed || transient ? `${attachment.fileName} 状态` : undefined}
          aria-live={transient ? 'polite' : undefined}
          aria-atomic={failed || transient ? 'true' : undefined}
          className="flex w-full min-w-0 items-center gap-1.5 text-xs text-muted-foreground"
        >
          <span className="shrink-0">{formatFileSize(attachment.size)}</span>
          <span data-slot="agent-prompt-attachment-status" className="shrink-0">
            <Badge variant={failed ? 'destructive' : 'secondary'}>
              {STATUS_LABELS[attachment.status]}
            </Badge>
          </span>
          {attachment.error ? (
            <span
              data-slot="agent-prompt-attachment-error"
              className="min-w-0 flex-1 truncate"
              title={attachment.error}
            >
              {attachment.error}
            </span>
          ) : null}
        </span>
        {attachment.status === 'uploading' ? (
          <Progress value={attachment.progress} aria-label={`${attachment.fileName} 上传进度`} className="mt-1 h-1 w-full" />
        ) : null}
      </div>

      <Popover>
        <Tooltip>
          <TooltipTrigger
            render={
              <PopoverTrigger
                render={
                  <InputGroupButton
                    size="icon-xs"
                    aria-label={`编辑 ${attachment.fileName} 的附件说明`}
                    disabled={disabled}
                  />
                }
              />
            }
          >
            <FilePenLineIcon />
          </TooltipTrigger>
          <TooltipContent>编辑附件说明</TooltipContent>
        </Tooltip>
        <PopoverContent align="end" className="w-72">
          <PopoverTitle>附件说明</PopoverTitle>
          <Input
            aria-label="附件说明"
            value={attachment.instruction ?? ''}
            maxLength={1000}
            placeholder="说明希望代理如何使用此附件"
            onChange={(event) => controller.updateInstruction(attachment.id, event.target.value)}
          />
        </PopoverContent>
      </Popover>

      {attachment.status === 'failed' ? (
        <Tooltip>
          <TooltipTrigger
            render={
              <InputGroupButton
                size="icon-xs"
                aria-label={`重试 ${attachment.fileName}`}
                disabled={disabled}
                onClick={() => controller.retry(attachment.id)}
              />
            }
          >
            <RotateCcwIcon />
          </TooltipTrigger>
          <TooltipContent>重试上传</TooltipContent>
        </Tooltip>
      ) : null}

      <Tooltip>
        <TooltipTrigger
          render={
            <InputGroupButton
              size="icon-xs"
              aria-label={`删除 ${attachment.fileName}`}
              disabled={disabled}
              onClick={() => controller.remove(attachment.id)}
            />
          }
        >
          <Trash2Icon />
        </TooltipTrigger>
        <TooltipContent>删除附件</TooltipContent>
      </Tooltip>
    </div>
  )
}

export function AgentPromptInput({
  value,
  onChange,
  onSubmit,
  attachmentController,
  attachmentPolicy,
  contextBar,
  leadingTools,
  trailingTools,
  status,
  placeholder = '输入任务要求…',
  submitLabel = 'Submit',
  submitIcon: SubmitIcon = ArrowUpIcon,
  submitting = false,
  disabled = false,
  submitDisabled = false,
  autoFocus = false,
  ariaLabel = 'Agent prompt',
  acceptedTypesLabel,
  onAttachmentRejected,
  onSubmitError,
}: AgentPromptInputProps) {
  const [locallySubmitting, setLocallySubmitting] = useState(false)
  const [rejectionStatus, setRejectionStatus] = useState('')
  const [submitErrorStatus, setSubmitErrorStatus] = useState('')
  const pendingRef = useRef(false)
  const composingRef = useRef(false)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const allowedTypes = attachmentPolicy.allowedTypes
  const remainingCapacity = Math.max(0, attachmentPolicy.maxCount - value.attachments.length)
  const typesLabel = acceptedTypesLabel ?? allowedTypes.map((type) => TYPE_LABELS[type]).join('、')
  const blocked = disabled
    || submitting
    || locallySubmitting
    || submitDisabled
    || attachmentController.uploading
    || attachmentController.hasFailures

  const acceptsFile = useCallback((file: File) => {
    const type = classifyPromptAttachment(file)
    if (!type || !attachmentPolicy.allowedTypes.includes(type)) return false
    const maxBytes = attachmentPolicy.maxBytes?.[type]
    return maxBytes === undefined || file.size <= maxBytes
  }, [attachmentPolicy])

  const acceptsItemType = useCallback((mime: string) => {
    const type = classifyPromptAttachment(new File([], 'dropped-file', { type: mime }))
    return type !== null && attachmentPolicy.allowedTypes.includes(type)
  }, [attachmentPolicy.allowedTypes])

  const addFiles = useCallback((files: readonly File[]) => {
    if (disabled || files.length === 0) return
    const result = attachmentController.addFiles(files)
    const announcement = rejectionAnnouncement(result.rejected)
    setRejectionStatus(announcement)
    if (result.rejected.length > 0) onAttachmentRejected?.(result.rejected)
  }, [attachmentController, disabled, onAttachmentRejected])

  const dropTarget = useAgentPromptDropTarget({
    enabled: !disabled,
    remainingCapacity,
    acceptedTypesLabel: typesLabel,
    acceptsFile,
    acceptsItemType,
    onFiles: addFiles,
  })

  const submit = useCallback(async () => {
    if (blocked || pendingRef.current) return
    pendingRef.current = true
    setLocallySubmitting(true)
    setSubmitErrorStatus('')
    try {
      await onSubmit({
        prompt: value.prompt,
        attachments: value.attachments,
      })
    } catch (error: unknown) {
      setSubmitErrorStatus('提交失败，请重试')
      try {
        onSubmitError?.(error)
      } catch {
        // Error observers must not break the composer's submit lifecycle.
      }
    } finally {
      pendingRef.current = false
      setLocallySubmitting(false)
    }
  }, [blocked, onSubmit, onSubmitError, value.attachments, value.prompt])

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Enter' || event.shiftKey) return
    const nativeEvent = event.nativeEvent
    if (composingRef.current || nativeEvent.isComposing || nativeEvent.keyCode === 229) return
    event.preventDefault()
    void submit()
  }

  const handlePaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(event.clipboardData.files ?? [])
    if (files.length === 0) return
    event.preventDefault()
    addFiles(files)
  }

  const handleDrop = (event: DragEvent<HTMLElement>) => {
    const files = Array.from(event.dataTransfer.files ?? [])
    if (files.length === 0) return
    event.preventDefault()
    event.stopPropagation()
    addFiles(files)
  }

  const handleDragOver = (event: DragEvent<HTMLElement>) => {
    if (!Array.from(event.dataTransfer.types ?? []).includes('Files')) return
    event.preventDefault()
    event.stopPropagation()
  }

  const handleFileSelection = (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.currentTarget.files ?? [])
    addFiles(files)
    event.currentTarget.value = ''
  }

  const pickerDisabled = disabled || remainingCapacity === 0

  return (
    <TooltipProvider>
      <section
        ref={dropTarget.ref}
        data-slot="agent-prompt-input"
        onFocusCapture={dropTarget.onFocusCapture}
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        className="flex min-w-0 flex-col gap-2"
      >
        {contextBar ? <header data-slot="agent-prompt-context">{contextBar}</header> : null}

        <InputGroup
          data-slot="agent-prompt-surface"
          className="min-h-40 h-auto max-h-[min(42rem,calc(100dvh-8rem))] flex-col overflow-hidden"
        >
          <div data-slot="agent-prompt-content" className="flex min-h-0 w-full flex-1 flex-col overflow-y-auto">
            {value.attachments.length > 0 ? (
              <div data-slot="agent-prompt-attachments" className="flex flex-col gap-1.5 px-2.5 pt-2.5">
                {value.attachments.map((item) => (
                  <AttachmentRow
                    key={item.id}
                    attachment={item}
                    controller={attachmentController}
                    disabled={disabled}
                  />
                ))}
              </div>
            ) : null}
            <InputGroupTextarea
              value={value.prompt}
              placeholder={placeholder}
              aria-label={ariaLabel}
              autoFocus={autoFocus}
              disabled={disabled}
              onChange={(event) => onChange({
                prompt: event.target.value,
                attachments: value.attachments,
              })}
              onKeyDown={handleKeyDown}
              onPaste={handlePaste}
              onCompositionStart={() => { composingRef.current = true }}
              onCompositionEnd={() => { composingRef.current = false }}
              className="min-h-24 field-sizing-content px-3 py-2.5"
            />
          </div>

          <InputGroupAddon align="block-end" className="shrink-0 gap-1.5">
            <input
              ref={fileInputRef}
              type="file"
              multiple
              className="sr-only"
              aria-label="选择附件文件"
              accept={allowedTypes.map((type) => INPUT_ACCEPT_BY_TYPE[type]).join(',')}
              disabled={pickerDisabled}
              onChange={handleFileSelection}
            />
            <Tooltip>
              <TooltipTrigger
                render={
                  <InputGroupButton
                    size="icon-xs"
                    aria-label="添加附件"
                    disabled={pickerDisabled}
                    onClick={() => fileInputRef.current?.click()}
                  />
                }
              >
                <PlusIcon />
              </TooltipTrigger>
              <TooltipContent>添加附件</TooltipContent>
            </Tooltip>
            {leadingTools}

            <div data-slot="agent-prompt-actions" className="ml-auto flex min-w-0 items-center gap-1.5">
              {status}
              {trailingTools}
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      type="button"
                      size="icon"
                      className="rounded-full"
                      aria-label={submitLabel}
                      disabled={blocked}
                      onClick={() => { void submit() }}
                    />
                  }
                >
                  {submitting || locallySubmitting
                    ? <LoaderCircleIcon className="animate-spin" />
                    : <SubmitIcon />}
                </TooltipTrigger>
                <TooltipContent>{submitLabel}</TooltipContent>
              </Tooltip>
            </div>
          </InputGroupAddon>
        </InputGroup>

        <div role="alert" aria-live="polite" aria-atomic="true" className="sr-only">
          {rejectionStatus}
        </div>
        {submitErrorStatus ? (
          <div role="alert" aria-live="assertive" aria-atomic="true" className="sr-only">
            {submitErrorStatus}
          </div>
        ) : null}
      </section>
    </TooltipProvider>
  )
}
