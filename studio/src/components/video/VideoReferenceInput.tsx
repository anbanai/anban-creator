import { useEffect, useMemo, useRef, useState } from 'react'
import { FileAudio, FileImage, FileText, FileVideo, Loader2, Plus, Trash2, Upload } from 'lucide-react'
import { Button } from '@/components/common/button'
import { Progress } from '@/components/ui/progress'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/Select'
import { http } from '@/lib/http-client'
import { uploadToOSS } from '@/lib/direct-upload'
import { isInternalStorageUrl, normalizeStorageUrl } from '@/lib/storage-url'
import { cn } from '@/lib/utils'
import { videoReferenceRoleLabel, videoReferenceRoles } from '@/lib/video-display'
import type { VideoReferenceAsset, VideoReferenceType } from '@/types'

const MAX_REFERENCE_FILE_SIZE = 50 * 1024 * 1024
const AUTO_REFERENCE_ROLE = '__agent_auto__'

const quickReferenceActions = [
  {
    role: 'subject identity',
    label: '主体不变',
    text: '主体必须保持一致；如果没有主体图片或视频，按文字描述凭空创作。',
  },
  {
    role: 'full remake reference',
    label: '完整复刻参考',
    text: '参考素材用于复刻结构、节奏和镜头，不自动继承原人物、logo 或无关场景。',
  },
  {
    role: 'product appearance',
    label: '产品外观',
    text: '产品外观、材质、颜色和关键细节必须保持准确。',
  },
  {
    role: 'action',
    label: '动作参考',
    text: '动作作为参考，主体和场景以本次任务要求为准。',
  },
  {
    role: 'camera movement',
    label: '镜头运动',
    text: '镜头运动作为参考，构图和主体由 Agent 根据任务判断。',
  },
  {
    role: 'rhythm',
    label: '节奏参考',
    text: '剪辑节奏、转场和时间轴作为参考。',
  },
  {
    role: 'voice tone',
    label: '声音/BGM',
    text: '声音或 BGM 作为参考，画面主体和视觉风格由其他素材或任务要求决定。',
  },
]

type UploadProgressItem = {
  id: string
  name: string
  percent: number
}

function referenceTypeForFile(file: File): VideoReferenceType {
  if (file.type.startsWith('audio/')) return 'audio_url'
  if (file.type.startsWith('video/')) return 'video_url'
  const ext = file.name.toLowerCase().split('.').pop() || ''
  if (['mp3', 'wav', 'm4a', 'aac', 'ogg'].includes(ext)) return 'audio_url'
  if (['mp4', 'mov', 'webm', 'm4v'].includes(ext)) return 'video_url'
  return 'image_url'
}

function referenceTypeLabel(type: string) {
  if (type === 'text') return '文本'
  if (type === 'audio_url') return '音频'
  if (type === 'video_url') return '视频'
  return '图片'
}

function referenceIcon(type: string) {
  if (type === 'text') return FileText
  if (type === 'audio_url') return FileAudio
  if (type === 'image_url') return FileImage
  return FileVideo
}

function referenceDisplayName(ref: VideoReferenceAsset) {
  return ref.file_name || ref.text || ref.url || referenceTypeLabel(ref.type)
}

function friendlyUploadError(err: any) {
  const original = String(err?.response?.data?.msg || err?.message || '')
  const raw = original.toLowerCase()
  if (original.includes('上传凭证已过期')) return original
  if (original.includes('OSS 上传失败')) return original
  if (original.includes('未配置 OSS')) return original
  if (raw.includes('50') || raw.includes('too large') || raw.includes('size') || raw.includes('exceed')) {
    return '文件超过 50MB，请压缩后再上传。'
  }
  if (raw.includes('unsupported') || raw.includes('format') || raw.includes('mime') || raw.includes('content type')) {
    return '格式不支持，请上传图片、音频或 MP4/MOV/WebM 视频。'
  }
  return '上传失败，请稍后重试。'
}

function measureLocalVideoDuration(file: File): Promise<number | undefined> {
  if (typeof URL.createObjectURL !== 'function') return Promise.resolve(undefined)

  return new Promise((resolve) => {
    const objectUrl = URL.createObjectURL(file)
    const video = document.createElement('video')
    let settled = false
    let timeout: number | undefined

    const finish = (duration: number | undefined) => {
      if (settled) return
      settled = true
      if (timeout != null) window.clearTimeout(timeout)
      video.onloadedmetadata = null
      video.onerror = null
      URL.revokeObjectURL(objectUrl)
      resolve(duration)
    }

    timeout = window.setTimeout(() => finish(undefined), 10_000)
    video.preload = 'metadata'
    video.onloadedmetadata = () => {
      const duration = Number.isFinite(video.duration) && video.duration > 0 ? video.duration : undefined
      finish(duration)
    }
    video.onerror = () => finish(undefined)
    video.src = objectUrl
    const isJSDOM = typeof navigator !== 'undefined' && /\bjsdom\b/i.test(navigator.userAgent)
    const hasElementLoadOverride = Object.prototype.hasOwnProperty.call(video, 'load')
    if (isJSDOM && !hasElementLoadOverride) {
      window.setTimeout(() => finish(undefined), 0)
      return
    }
    try {
      video.load()
    } catch {
      finish(undefined)
    }
  })
}

function splitRuleList(value: string) {
  return value
    .split(/[,\n，、]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function joinRuleList(values: string[] | undefined) {
  return values?.join('、') ?? ''
}

function useAuthenticatedPreviewUrl(src: string | undefined) {
  const normalized = useMemo(() => src ? normalizeStorageUrl(src) : '', [src])
  const [previewUrl, setPreviewUrl] = useState(normalized)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    if (!normalized) {
      setPreviewUrl('')
      setFailed(false)
      return
    }
    if (!isInternalStorageUrl(normalized)) {
      setPreviewUrl(normalized)
      setFailed(false)
      return
    }

    let cancelled = false
    let objectUrl = ''
    setFailed(false)
    setPreviewUrl('')
    http.get(normalized, { responseType: 'blob' })
      .then((res) => {
        if (cancelled) return
        objectUrl = URL.createObjectURL(res.data)
        setPreviewUrl(objectUrl)
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })

    return () => {
      cancelled = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [normalized])

  return { previewUrl, failed }
}

function VideoReferencePreview({ ref }: { ref: VideoReferenceAsset }) {
  const label = referenceDisplayName(ref)
  const { previewUrl, failed } = useAuthenticatedPreviewUrl(ref.url)

  if (ref.type === 'image_url' && ref.url) {
    return (
      <div className="flex size-20 shrink-0 items-center justify-center overflow-hidden rounded-md border border-border bg-muted/30">
        {previewUrl && !failed ? (
          <img src={previewUrl} alt={label} className="size-full object-cover" loading="lazy" />
        ) : (
          <FileImage className="text-muted-foreground" />
        )}
      </div>
    )
  }

  if (ref.type === 'video_url' && ref.url) {
    return (
      <video
        aria-label={label}
        controls
        preload="metadata"
        className="size-20 shrink-0 rounded-md border border-border bg-muted object-cover"
      >
        {previewUrl && <source src={previewUrl} type={ref.mime_type || undefined} />}
      </video>
    )
  }

  if (ref.type === 'audio_url' && ref.url) {
    return (
      <div className="flex h-20 w-full min-w-0 items-center rounded-md border border-border bg-muted/30 px-2 sm:w-64">
        <audio aria-label={label} controls preload="metadata" className="h-9 w-full">
          {previewUrl && <source src={previewUrl} type={ref.mime_type || undefined} />}
        </audio>
      </div>
    )
  }

  if (ref.type === 'text' && ref.text) {
    return (
      <div className="h-20 w-full overflow-hidden rounded-md border border-border bg-muted/30 px-3 py-2 text-xs leading-5 text-muted-foreground sm:w-64">
        {ref.text}
      </div>
    )
  }

  return (
    <div className="flex size-20 shrink-0 items-center justify-center rounded-md border border-border bg-muted/30 text-muted-foreground">
      <FileVideo />
    </div>
  )
}

function UploadDropzone({
  uploading,
  isDragging,
  onBrowse,
  onDropFiles,
  onDragStateChange,
}: {
  uploading: boolean
  isDragging: boolean
  onBrowse: () => void
  onDropFiles: (files: File[]) => void
  onDragStateChange: (dragging: boolean) => void
}) {
  return (
    <button
      type="button"
      className={cn(
        'flex w-full flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border bg-muted/20 px-4 py-6 text-center transition-colors hover:border-primary/50 hover:bg-primary/5',
        isDragging && 'border-primary bg-primary/10',
      )}
      onClick={onBrowse}
      onDragOver={(event) => {
        event.preventDefault()
        onDragStateChange(true)
      }}
      onDragLeave={(event) => {
        event.preventDefault()
        onDragStateChange(false)
      }}
      onDrop={(event) => {
        event.preventDefault()
        onDragStateChange(false)
        onDropFiles(Array.from(event.dataTransfer.files))
      }}
      disabled={uploading}
    >
      <span className="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
        {uploading ? <Loader2 className="animate-spin" /> : <Upload />}
      </span>
      <span className="text-sm font-medium text-foreground">{uploading ? '正在上传素材...' : '上传图片 / 视频 / 音频素材'}</span>
      <span className="max-w-md text-xs text-muted-foreground">点击选择或拖拽多个文件到这里，单个文件不超过 50MB。</span>
    </button>
  )
}

export function VideoReferenceInput({
  value = [],
  onChange,
}: {
  value?: VideoReferenceAsset[]
  onChange: (value: VideoReferenceAsset[]) => void
}) {
  const fileInputRef = useRef<HTMLInputElement>(null)
  const latestValueRef = useRef(value)
  const [uploading, setUploading] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<UploadProgressItem[]>([])
  const [uploadErrors, setUploadErrors] = useState<string[]>([])
  const [isDragging, setIsDragging] = useState(false)
  const [textRef, setTextRef] = useState('')
  const [textRole, setTextRole] = useState(AUTO_REFERENCE_ROLE)

  useEffect(() => {
    latestValueRef.current = value
  }, [value])

  const updateAt = (index: number, patch: Partial<VideoReferenceAsset>) => {
    const next = value.map((item, i) => i === index ? { ...item, ...patch } : item)
    onChange(next)
  }

  const removeAt = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  const appendReference = (asset: VideoReferenceAsset) => {
    const next = [...latestValueRef.current, asset]
    latestValueRef.current = next
    onChange(next)
  }

  const addQuickTextReference = (action: typeof quickReferenceActions[number]) => {
    appendReference({
      type: 'text',
      text: action.text,
      reference_role: action.role,
    })
  }

  const uploadFiles = async (files: File[]) => {
    if (files.length === 0) return
    setUploading(true)
    setUploadErrors([])
    setUploadProgress(files.map((file, index) => ({
      id: `${file.name}-${file.size}-${index}`,
      name: file.name,
      percent: 0,
    })))
    const errors: string[] = []
    const updateProgress = (id: string, percent: number) => {
      setUploadProgress((items) => items.map((item) => item.id === id ? { ...item, percent } : item))
    }

    for (const [index, file] of files.entries()) {
      const progressId = `${file.name}-${file.size}-${index}`
      if (file.size > MAX_REFERENCE_FILE_SIZE) {
        errors.push(`${file.name}：文件超过 50MB，请压缩后再上传。`)
        updateProgress(progressId, 100)
        continue
      }
      try {
        const referenceType = referenceTypeForFile(file)
        const measuredDuration = referenceType === 'video_url'
          ? await measureLocalVideoDuration(file)
          : undefined
        if (referenceType === 'video_url' && measuredDuration == null) {
          errors.push(`${file.name}：无法读取视频时长，请转换为浏览器支持的 MP4、MOV 或 WebM 后重试。`)
          updateProgress(progressId, 100)
          continue
        }
        const result = await uploadToOSS({
          purpose: 'video_reference',
          file,
          onProgress: (percent) => updateProgress(progressId, percent),
        })
        updateProgress(progressId, 100)
        appendReference({
          type: referenceType,
          url: result.publicUrl,
          reference_role: undefined,
          file_name: file.name,
          mime_type: result.contentType || file.type,
          file_size: result.size || file.size,
          input_duration_seconds: measuredDuration,
        })
      } catch (err: any) {
        updateProgress(progressId, 100)
        errors.push(`${file.name}：${friendlyUploadError(err)}`)
      }
    }

    setUploadErrors(errors)
    setUploading(false)
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  const addTextReference = () => {
    const text = textRef.trim()
    if (!text) return
    onChange([...value, { type: 'text', text, reference_role: textRole === AUTO_REFERENCE_ROLE ? undefined : textRole }])
    setTextRef('')
  }

  const hasSubjectIdentity = value.some((ref) => ref.reference_role === 'subject identity')
  const hasSubjectMedia = value.some((ref) =>
    ref.reference_role === 'subject identity' &&
    (ref.type === 'image_url' || ref.type === 'video_url') &&
    Boolean(ref.url || ref.task_file_id),
  )

  return (
    <div className="flex flex-col gap-3">
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*,audio/*,video/*"
        multiple
        className="hidden"
        onChange={(event) => void uploadFiles(Array.from(event.target.files ?? []))}
      />
      <UploadDropzone
        uploading={uploading}
        isDragging={isDragging}
        onBrowse={() => fileInputRef.current?.click()}
        onDropFiles={(files) => void uploadFiles(files)}
        onDragStateChange={setIsDragging}
      />
      <div className="rounded-lg border border-border bg-muted/10 p-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">素材角色快捷入口</span>
          {quickReferenceActions.map((action) => (
            <Button
              key={action.role}
              type="button"
              variant="outline"
              size="sm"
              onClick={() => addQuickTextReference(action)}
            >
              {action.label}
            </Button>
          ))}
        </div>
        {hasSubjectIdentity && !hasSubjectMedia && (
          <p className="mt-2 text-xs text-amber-600">
            主体不变最好上传主体图片或视频；没有素材也可以继续凭空创作，Agent 会按文字约束执行。
          </p>
        )}
      </div>
      {uploadErrors.length > 0 && (
        <div className="flex flex-col gap-1 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {uploadErrors.map((item) => <p key={item}>{item}</p>)}
        </div>
      )}
      {uploading && uploadProgress.length > 0 && (
        <div className="flex flex-col gap-2 rounded-md border border-border bg-muted/20 px-3 py-2">
          {uploadProgress.map((item) => (
            <div key={item.id} className="grid gap-1">
              <div className="flex items-center justify-between gap-3 text-xs">
                <span className="truncate text-muted-foreground">{item.name}</span>
                <span className="tabular-nums text-foreground">{item.percent}%</span>
              </div>
              <Progress value={item.percent} />
            </div>
          ))}
        </div>
      )}

      <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_180px_auto] md:items-start">
        <Textarea
          value={textRef}
          onChange={(event) => setTextRef(event.target.value)}
          placeholder="例如：杯身必须保持银色金属质感，禁止变成卡通杯。"
          className="min-h-[72px] resize-y"
        />
        <Select value={textRole} onValueChange={(role) => setTextRole(role || AUTO_REFERENCE_ROLE)}>
          <SelectTrigger className="w-full"><SelectValue>{videoReferenceRoleLabel(textRole)}</SelectValue></SelectTrigger>
          <SelectContent>
            <SelectItem value={AUTO_REFERENCE_ROLE} label="由 Agent 判断">由 Agent 判断</SelectItem>
            {videoReferenceRoles.map((role) => (
              <SelectItem key={role.value} value={role.value} label={role.label}>{role.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="button" variant="secondary" className="w-full md:w-auto" onClick={addTextReference} disabled={!textRef.trim()}>
          <Plus />
          添加文本约束
        </Button>
      </div>

      {value.length > 0 && (
        <div className="divide-y divide-border rounded-lg border border-border">
          {value.map((ref, index) => {
            const Icon = referenceIcon(ref.type)
            return (
              <div key={`${ref.type}-${ref.url || ref.text}-${index}`} className="flex flex-col gap-3 p-3">
                <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_180px_auto] lg:items-center">
                  <div className="flex min-w-0 gap-3">
                    <VideoReferencePreview ref={ref} />
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-center gap-2">
                        <Icon className="shrink-0 text-muted-foreground" />
                        <span className="text-xs text-muted-foreground">{referenceTypeLabel(ref.type)}</span>
                        <span className="truncate text-sm text-foreground">{referenceDisplayName(ref)}</span>
                      </div>
                      <p className="mt-1 text-xs text-muted-foreground">用途：{videoReferenceRoleLabel(ref.reference_role)}</p>
                      {ref.url && <p className="mt-1 truncate text-xs text-muted-foreground">{ref.url}</p>}
                      {ref.input_duration_seconds ? (
                        <p className="mt-1 text-xs text-muted-foreground">输入时长 {ref.input_duration_seconds.toFixed(1)} 秒</p>
                      ) : null}
                    </div>
                  </div>
                  <Select value={ref.reference_role || AUTO_REFERENCE_ROLE} onValueChange={(role) => updateAt(index, { reference_role: !role || role === AUTO_REFERENCE_ROLE ? undefined : role })}>
                    <SelectTrigger className="w-full"><SelectValue>{videoReferenceRoleLabel(ref.reference_role)}</SelectValue></SelectTrigger>
                    <SelectContent>
                      <SelectItem value={AUTO_REFERENCE_ROLE} label="由 Agent 判断">由 Agent 判断</SelectItem>
                      {videoReferenceRoles.map((role) => (
                        <SelectItem key={role.value} value={role.value} label={role.label}>{role.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button type="button" variant="ghost" size="sm" onClick={() => removeAt(index)} aria-label="移除参考素材">
                    <Trash2 />
                  </Button>
                </div>
                {ref.type === 'video_url' && (
                  <p className="text-xs text-muted-foreground">视频素材默认约束运镜、节奏、动作；当要求同款/复刻/完全一样时，会作为段子结构和时间轴参考，但主体、产品、场景仍以你的其他素材和文字为准。</p>
                )}
                <div className="grid gap-2 md:grid-cols-3">
                  <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                    控制什么
                    <Textarea
                      aria-label="控制什么"
                      value={joinRuleList(ref.must_keep)}
                      onChange={(event) => updateAt(index, { must_keep: splitRuleList(event.target.value) })}
                      placeholder="例如：产品外观、动作节奏"
                      className="min-h-16 resize-y text-sm"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                    可变什么
                    <Textarea
                      aria-label="可变什么"
                      value={joinRuleList(ref.can_change)}
                      onChange={(event) => updateAt(index, { can_change: splitRuleList(event.target.value) })}
                      placeholder="例如：背景、服装、道具"
                      className="min-h-16 resize-y text-sm"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-xs text-muted-foreground">
                    不传递什么
                    <Textarea
                      aria-label="不传递什么"
                      value={joinRuleList(ref.must_not_transfer)}
                      onChange={(event) => updateAt(index, { must_not_transfer: splitRuleList(event.target.value) })}
                      placeholder="例如：原人物、logo、场景"
                      className="min-h-16 resize-y text-sm"
                    />
                  </label>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
