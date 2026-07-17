import { useEffect, useRef, useState, type ChangeEvent, type DragEvent, type KeyboardEvent, type ReactNode } from 'react'
import {
  AlertCircle,
  File,
  FileText,
  Image as ImageIcon,
  Loader2,
  Music2,
  Paperclip,
  RefreshCw,
  Trash2,
  Upload,
  Video,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { Textarea } from '@/components/ui/textarea'
import { uploadToOSS, type DirectUploadPurpose } from '@/lib/direct-upload'
import { cn } from '@/lib/utils'
import type { InputAttachment, InputAttachmentType } from '@/types/input-attachment'

export interface ReferenceMaterialInputProps {
  value: InputAttachment[]
  onChange: (value: InputAttachment[]) => void
  allowedTypes: InputAttachmentType[]
  maxCount?: number
  instructionEnabled?: boolean
  instructionMaxLength?: number
  compact?: boolean
  hint?: string
  onUploadingChange?: (uploading: boolean) => void
  uploadPurpose?: DirectUploadPurpose
}

type UploadRow = {
  id: string
  order: number
  file: File
  progress: number
  status: 'uploading' | 'failed'
  error?: string
}

const DOCUMENT_EXTENSIONS = '.pdf,.doc,.docx,.ppt,.pptx,.xls,.xlsx,.json,.csv,.md,.txt'
const DEFAULT_HINT = 'AI 会根据创作需求逐张理解参考素材，并自动核验生成结果。参考素材越多，执行时间和积分消耗可能越高。'
const INSTRUCTION_PLACEHOLDER = '可选：告诉 AI 这张图是什么，或哪些内容需要保留、忽略。留空也会自动理解。'

let nextUploadRowID = 0

const typeFromFile = (file: File): InputAttachmentType | null => {
  if (file.type.startsWith('image/')) return 'image'
  if (file.type.startsWith('audio/')) return 'audio'
  if (file.type.startsWith('video/')) return 'video'
  if (file.type.startsWith('text/')) return 'text'
  if (/\.(pdf|docx?|pptx?|xlsx?|json|csv|md|txt)$/i.test(file.name)) return 'document'
  return null
}

const codePointLength = (value: string) => Array.from(value).length

const maxBytesForType = (type: InputAttachmentType) => (
  type === 'document' || type === 'text'
    ? 25 * 1024 * 1024
    : 50 * 1024 * 1024
)

const acceptForTypes = (allowedTypes: InputAttachmentType[]) => {
  const accepted: string[] = []
  const allowed = new Set(allowedTypes)
  if (allowed.has('image')) accepted.push('image/*')
  if (allowed.has('audio')) accepted.push('audio/*')
  if (allowed.has('video')) accepted.push('video/*')
  if (allowed.has('text')) accepted.push('text/*')
  if (allowed.has('document')) accepted.push(DOCUMENT_EXTENSIONS)
  return accepted.join(',')
}

const iconForType = (type: InputAttachmentType, className: string): ReactNode => {
  switch (type) {
    case 'image':
      return <ImageIcon className={className} />
    case 'audio':
      return <Music2 className={className} />
    case 'video':
      return <Video className={className} />
    case 'document':
      return <FileText className={className} />
    case 'text':
      return <File className={className} />
  }
}

const formatBytes = (size?: number) => {
  if (size === undefined) return ''
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${Math.ceil(size / 1024)} KB`
  return `${(size / 1024 / 1024).toFixed(size >= 10 * 1024 * 1024 ? 0 : 1)} MB`
}

const attachmentName = (attachment: InputAttachment, index: number) => (
  attachment.file_name || (attachment.type === 'image' ? `参考图 ${index + 1}` : `附件 ${index + 1}`)
)

export function ReferenceMaterialInput({
  value,
  onChange,
  allowedTypes,
  maxCount,
  instructionEnabled = false,
  instructionMaxLength = 1000,
  compact = false,
  hint = DEFAULT_HINT,
  onUploadingChange,
  uploadPurpose = 'ai_entry_attachment',
}: ReferenceMaterialInputProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const valueRef = useRef(value)
  const rowsRef = useRef<UploadRow[]>([])
  const uploadOrderRef = useRef(new Map<string, number>())
  const [rows, setRows] = useState<UploadRow[]>([])
  const [validationError, setValidationError] = useState('')
  const [instructionErrors, setInstructionErrors] = useState<Record<number, boolean>>({})
  const [isDragging, setIsDragging] = useState(false)

  useEffect(() => {
    valueRef.current = value
  }, [value])

  useEffect(() => {
    rowsRef.current = rows
    onUploadingChange?.(rows.some((row) => row.status === 'uploading'))
  }, [rows, onUploadingChange])

  const updateRows = (updater: (current: UploadRow[]) => UploadRow[]) => {
    setRows((current) => {
      const next = updater(current)
      rowsRef.current = next
      return next
    })
  }

  const emitValue = (nextValue: InputAttachment[]) => {
    valueRef.current = nextValue
    onChange(nextValue)
  }

  const uploadFile = async (row: UploadRow) => {
    updateRows((current) => current.map((item) => (
      item.id === row.id
        ? { ...item, progress: 0, status: 'uploading', error: undefined }
        : item
    )))

    try {
      const result = await uploadToOSS({
        purpose: uploadPurpose,
        file: row.file,
        onProgress: (progress) => {
          updateRows((current) => current.map((item) => (
            item.id === row.id ? { ...item, progress } : item
          )))
        },
      })
      const type = typeFromFile(row.file)
      if (!type) throw new Error(`不支持 ${row.file.name} 的文件类型`)

      const attachment = {
        type,
        url: result.publicUrl,
        file_name: row.file.name,
        content_type: result.contentType,
        size: result.size,
        upload_id: result.uploadId,
        key: result.key,
        instruction: instructionEnabled ? '' : undefined,
      } satisfies InputAttachment
      uploadOrderRef.current.set(result.uploadId, row.order)

      // Uploads run concurrently, but the controlled value must preserve the
      // order in which the user selected files. Keep pre-existing attachments
      // first and sort only attachments uploaded by this mounted component.
      const existing = valueRef.current.filter((item) => (
        !item.upload_id || !uploadOrderRef.current.has(item.upload_id)
      ))
      const uploaded = [
        ...valueRef.current.filter((item) => (
          Boolean(item.upload_id) && uploadOrderRef.current.has(item.upload_id as string)
        )),
        attachment,
      ].sort((left, right) => (
        uploadOrderRef.current.get(left.upload_id as string)! - uploadOrderRef.current.get(right.upload_id as string)!
      ))
      emitValue([...existing, ...uploaded])
      updateRows((current) => current.filter((item) => item.id !== row.id))
    } catch (error) {
      updateRows((current) => current.map((item) => (
        item.id === row.id
          ? {
              ...item,
              status: 'failed',
              error: error instanceof Error ? error.message : '上传失败，请重试',
            }
          : item
      )))
    }
  }

  const addFiles = (files: File[]) => {
    if (files.length === 0) return

    setValidationError('')
    const acceptedRows: UploadRow[] = []
    let occupiedCount = valueRef.current.length + rowsRef.current.length

    for (const file of files) {
      const type = typeFromFile(file)
      if (!type) {
        setValidationError(`不支持 ${file.name} 的文件类型`)
        continue
      }
      if (!allowedTypes.includes(type)) {
        setValidationError(`当前不支持添加 ${type} 类型的参考素材`)
        continue
      }
      const maxBytes = maxBytesForType(type)
      if (file.size > maxBytes) {
        setValidationError(`${file.name} 不能超过 ${Math.round(maxBytes / 1024 / 1024)}MB`)
        continue
      }
      if (maxCount !== undefined && occupiedCount >= maxCount) {
        setValidationError(`最多添加 ${maxCount} 个参考素材`)
        continue
      }

      nextUploadRowID += 1
      acceptedRows.push({
        id: `reference-upload-${nextUploadRowID}`,
        order: nextUploadRowID,
        file,
        progress: 0,
        status: 'uploading',
      })
      occupiedCount += 1
    }

    if (acceptedRows.length === 0) return
    updateRows((current) => [...current, ...acceptedRows])
    for (const row of acceptedRows) void uploadFile(row)
  }

  const handleInputChange = (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? [])
    event.target.value = ''
    addFiles(files)
  }

  const handleDragOver = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy'
    setIsDragging(true)
  }

  const handleDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault()
    setIsDragging(false)
    addFiles(Array.from(event.dataTransfer.files ?? []))
  }

  const openFilePicker = () => inputRef.current?.click()

  const handleDropZoneKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      openFilePicker()
    }
  }

  const removeAttachment = (index: number) => {
    emitValue(valueRef.current.filter((_, itemIndex) => itemIndex !== index))
    setInstructionErrors((current) => {
      const next: Record<number, boolean> = {}
      for (const [rawIndex, hasError] of Object.entries(current)) {
        const itemIndex = Number(rawIndex)
        if (!hasError || itemIndex === index) continue
        next[itemIndex > index ? itemIndex - 1 : itemIndex] = true
      }
      return next
    })
  }

  const updateInstruction = (index: number, instruction: string) => {
    if (codePointLength(instruction) > instructionMaxLength) {
      setInstructionErrors((current) => ({ ...current, [index]: true }))
      return
    }
    setInstructionErrors((current) => {
      if (!current[index]) return current
      const next = { ...current }
      delete next[index]
      return next
    })
    emitValue(valueRef.current.map((attachment, itemIndex) => (
      itemIndex === index ? { ...attachment, instruction } : attachment
    )))
  }

  const totalCount = value.length + rows.length
  const allowedSummary = allowedTypes.map((type) => ({
    image: '图片',
    audio: '音频',
    video: '视频',
    document: '文档',
    text: '文本',
  })[type]).join('、')

  return (
    <div className={cn('space-y-3', compact && 'space-y-2')}>
      <div
        role="button"
        tabIndex={0}
        aria-label="参考素材上传区域"
        onClick={openFilePicker}
        onKeyDown={handleDropZoneKeyDown}
        onDragOver={handleDragOver}
        onDragLeave={() => setIsDragging(false)}
        onDrop={handleDrop}
        className={cn(
          'group flex cursor-pointer items-center rounded-xl border border-dashed border-border bg-muted/20 text-left transition-colors hover:border-foreground/30 hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
          compact ? 'gap-2.5 px-3 py-2.5' : 'gap-3 px-4 py-4',
          isDragging && 'border-primary bg-primary/5 ring-2 ring-primary/15',
        )}
      >
        <div className={cn(
          'flex shrink-0 items-center justify-center rounded-lg border bg-background text-muted-foreground shadow-sm transition-colors group-hover:text-foreground',
          compact ? 'size-8' : 'size-10',
        )}>
          <Upload className={compact ? 'size-4' : 'size-5'} />
        </div>
        <div className="min-w-0 flex-1">
          <div className={cn('font-medium text-foreground', compact ? 'text-xs' : 'text-sm')}>
            {isDragging ? '松开即可添加' : '添加参考素材'}
          </div>
          <div className={cn('mt-0.5 text-muted-foreground', compact ? 'text-[11px]' : 'text-xs')}>
            点击选择或拖放多个文件 · 支持{allowedSummary || '指定类型'}
          </div>
        </div>
        <div className="shrink-0 text-xs tabular-nums text-muted-foreground">
          {maxCount === undefined ? totalCount : `${totalCount}/${maxCount}`}
        </div>
        <input
          ref={inputRef}
          aria-label="添加参考素材"
          type="file"
          multiple
          accept={acceptForTypes(allowedTypes)}
          className="sr-only"
          onChange={handleInputChange}
          onClick={(event) => event.stopPropagation()}
        />
      </div>

      {validationError ? (
        <div role="alert" className="flex items-start gap-1.5 text-xs text-destructive">
          <AlertCircle className="mt-0.5 size-3.5 shrink-0" />
          <span>{validationError}</span>
        </div>
      ) : null}

      {hint ? (
        <p className={cn('text-muted-foreground', compact ? 'text-[11px]' : 'text-xs')}>
          {hint}
        </p>
      ) : null}

      {totalCount > 0 ? (
        <div className={cn('grid gap-3', compact ? 'grid-cols-1' : 'grid-cols-1 xl:grid-cols-2')}>
          {value.map((attachment, index) => {
            const name = attachmentName(attachment, index)
            const instructionLength = codePointLength(attachment.instruction ?? '')
            return (
              <div key={`${attachment.upload_id || attachment.key || attachment.url || name}-${index}`} className="rounded-xl border bg-card p-3 shadow-xs">
                <div className="flex gap-3">
                  {attachment.type === 'image' && attachment.url ? (
                    <img
                      src={attachment.url}
                      alt={name}
                      className={cn('shrink-0 rounded-lg border bg-muted object-cover', compact ? 'size-16' : 'size-24')}
                    />
                  ) : (
                    <div
                      aria-label={`${name} 文件卡片`}
                      className={cn(
                        'flex shrink-0 items-center justify-center rounded-lg border bg-muted/40 text-muted-foreground',
                        compact ? 'size-16' : 'size-24',
                      )}
                    >
                      {iconForType(attachment.type, compact ? 'size-6' : 'size-8')}
                    </div>
                  )}

                  <div className="min-w-0 flex-1">
                    <div className="flex items-start gap-2">
                      <div className="min-w-0 flex-1">
                        <div className="truncate text-sm font-medium text-foreground" title={name}>{name}</div>
                        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-[11px] text-muted-foreground">
                          <span>{attachment.type}</span>
                          {attachment.size !== undefined ? <span>{formatBytes(attachment.size)}</span> : null}
                        </div>
                      </div>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        aria-label={`删除 ${name}`}
                        title={`删除 ${name}`}
                        onClick={() => removeAttachment(index)}
                      >
                        <Trash2 />
                      </Button>
                    </div>

                    {instructionEnabled ? (
                      <div className="mt-2">
                        <Textarea
                          aria-label={`${name} 的说明`}
                          value={attachment.instruction ?? ''}
                          placeholder={INSTRUCTION_PLACEHOLDER}
                          rows={compact ? 2 : 3}
                          className={cn('min-h-0 resize-y text-xs', compact ? 'py-1.5' : 'py-2')}
                          aria-invalid={instructionErrors[index] || undefined}
                          onChange={(event) => updateInstruction(index, event.target.value)}
                        />
                        <div className={cn(
                          'mt-1 flex justify-end text-[10px] tabular-nums',
                          instructionErrors[index] ? 'text-destructive' : 'text-muted-foreground',
                        )}>
                          {instructionErrors[index]
                            ? `最多 ${instructionMaxLength} 个字符`
                            : `${instructionLength}/${instructionMaxLength}`}
                        </div>
                      </div>
                    ) : null}
                  </div>
                </div>
              </div>
            )
          })}

          {rows.map((row) => (
            <div key={row.id} className={cn(
              'rounded-xl border p-3',
              row.status === 'failed' ? 'border-destructive/35 bg-destructive/5' : 'bg-card',
            )}>
              <div className="flex items-center gap-3">
                <div className={cn(
                  'flex shrink-0 items-center justify-center rounded-lg border bg-background text-muted-foreground',
                  compact ? 'size-10' : 'size-12',
                )}>
                  {row.status === 'uploading'
                    ? <Loader2 className="size-5 animate-spin" />
                    : <AlertCircle className="size-5 text-destructive" />}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium" title={row.file.name}>{row.file.name}</div>
                  {row.status === 'uploading' ? (
                    <div className="mt-2 flex items-center gap-2">
                      <Progress value={row.progress} className="flex-1 gap-0" aria-label={`${row.file.name} 上传进度`} />
                      <span className="w-9 text-right text-[11px] tabular-nums text-muted-foreground">{Math.round(row.progress)}%</span>
                    </div>
                  ) : (
                    <div className="mt-1 flex items-center justify-between gap-2">
                      <span className="text-xs text-destructive">{row.error}</span>
                      <div className="flex shrink-0 items-center gap-1">
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          aria-label={`重试 ${row.file.name}`}
                          onClick={() => void uploadFile(row)}
                        >
                          <RefreshCw data-icon="inline-start" />
                          重试
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon-xs"
                          aria-label={`删除 ${row.file.name}`}
                          title={`删除 ${row.file.name}`}
                          onClick={() => updateRows((current) => current.filter((item) => item.id !== row.id))}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="flex items-center gap-2 rounded-lg border border-dashed border-transparent px-1 text-xs text-muted-foreground">
          <Paperclip className="size-3.5" />
          <span>尚未添加参考素材</span>
        </div>
      )}
    </div>
  )
}
