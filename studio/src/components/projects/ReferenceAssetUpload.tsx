import { useEffect, useRef, useState } from 'react'
import { Loader2, Upload, X } from 'lucide-react'

import { uploadToOSS, type DirectUploadPurpose } from '@/lib/direct-upload'
import { referenceSelectionFromUpload } from '@/lib/reference-image'
import type { ReferenceImageSelection, ReferenceImageValue } from '@/types/asset'

type ReferenceUploadPurpose = Extract<
  DirectUploadPurpose,
  'project_reference' | 'project_portrait_reference' | 'task_reference' | 'template_thumbnail'
>

interface ReferenceAssetUploadProps {
  value: ReferenceImageValue | null
  onChange: (value: ReferenceImageSelection | null) => void
  purpose: ReferenceUploadPurpose
  ariaLabel?: string
  onUploadingChange?: (uploading: boolean) => void
  onUploadedPreview?: (previewUrl: string) => void
  disabled?: boolean
}

interface ActiveUpload {
  generation: number
  controller: AbortController
  startValueIdentity: string
  notify?: (uploading: boolean) => void
}

const MAX_SIZE_MB = 10
const ACCEPTED_IMAGE_TYPES = 'image/jpeg,image/png,image/webp,image/gif'

export function ReferenceAssetUpload({
  value,
  onChange,
  purpose,
  ariaLabel = '参考图文件',
  onUploadingChange,
  onUploadedPreview,
  disabled = false,
}: ReferenceAssetUploadProps) {
  const [localPreviewUrl, setLocalPreviewUrl] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const [isDragging, setIsDragging] = useState(false)
  const blobUrlRef = useRef('')
  const localSessionIdRef = useRef('')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const mountedRef = useRef(true)
  const nextGenerationRef = useRef(0)
  const activeUploadRef = useRef<ActiveUpload | null>(null)
  const onChangeRef = useRef(onChange)
  const onUploadedPreviewRef = useRef(onUploadedPreview)

  const assetId = value && typeof value.asset_id === 'string' ? value.asset_id : ''
  const sessionId = value && 'upload_session_id' in value && typeof value.upload_session_id === 'string'
    ? value.upload_session_id
    : ''
  const valueIdentity = assetId ? `asset:${assetId}` : sessionId ? `session:${sessionId}` : ''
  const currentValueIdentityRef = useRef(valueIdentity)
  currentValueIdentityRef.current = valueIdentity
  onChangeRef.current = onChange
  onUploadedPreviewRef.current = onUploadedPreview
  const assetPreviewUrl = value && 'download_url' in value ? value.download_url : ''
  const previewUrl = localPreviewUrl || assetPreviewUrl

  useEffect(() => {
    const activeUpload = activeUploadRef.current
    if (activeUpload && activeUpload.startValueIdentity !== valueIdentity) {
      activeUploadRef.current = null
      activeUpload.controller.abort()
      setUploading(false)
      activeUpload.notify?.(false)
    }

    const localSessionId = localSessionIdRef.current
    const referencesDifferentValue = assetId
      || (localSessionId && localSessionId !== sessionId)
    if (!referencesDifferentValue || !blobUrlRef.current) return

    URL.revokeObjectURL(blobUrlRef.current)
    blobUrlRef.current = ''
    localSessionIdRef.current = ''
    setLocalPreviewUrl('')
  }, [assetId, sessionId, valueIdentity])

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      const activeUpload = activeUploadRef.current
      if (activeUpload) {
        activeUploadRef.current = null
        activeUpload.controller.abort()
        activeUpload.notify?.(false)
      }
      if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
      blobUrlRef.current = ''
      localSessionIdRef.current = ''
    }
  }, [])

  const replaceLocalPreview = (file: File) => {
    if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
    const objectUrl = URL.createObjectURL(file)
    blobUrlRef.current = objectUrl
    localSessionIdRef.current = ''
    setLocalPreviewUrl(objectUrl)
  }

  const clearLocalPreview = () => {
    if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
    blobUrlRef.current = ''
    localSessionIdRef.current = ''
    setLocalPreviewUrl('')
  }

  const uploadFile = async (file: File) => {
	if (activeUploadRef.current || disabled) return
    if (file.size > MAX_SIZE_MB * 1024 * 1024) {
      setUploadError(`文件大小不能超过 ${MAX_SIZE_MB}MB`)
      return
    }

    setUploadError('')
    replaceLocalPreview(file)
    const generation = ++nextGenerationRef.current
    const controller = new AbortController()
    const startValueIdentity = currentValueIdentityRef.current
    const activeUpload: ActiveUpload = {
      generation,
      controller,
      startValueIdentity,
      notify: onUploadingChange,
    }
    activeUploadRef.current = activeUpload
    setUploading(true)
    activeUpload.notify?.(true)
    try {
      const result = await uploadToOSS({ purpose, file, signal: controller.signal })
      if (
        !mountedRef.current
        || activeUploadRef.current?.generation !== generation
        || currentValueIdentityRef.current !== startValueIdentity
      ) return
      localSessionIdRef.current = result.uploadSessionId
      onChangeRef.current(referenceSelectionFromUpload(result))
      onUploadedPreviewRef.current?.(result.previewUrl)
    } catch (error) {
      if (
        !mountedRef.current
        || activeUploadRef.current?.generation !== generation
        || currentValueIdentityRef.current !== startValueIdentity
      ) return
      clearLocalPreview()
      if ((error as { name?: string } | null)?.name !== 'AbortError') {
        setUploadError((error as { message?: string } | null)?.message || '上传失败，请重试')
      }
    } finally {
      if (activeUploadRef.current?.generation !== generation) return
      activeUploadRef.current = null
      setUploading(false)
      activeUpload.notify?.(false)
    }
  }

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (file) void uploadFile(file)
    event.target.value = ''
  }

  const handleDrop = (event: React.DragEvent<HTMLButtonElement>) => {
    event.preventDefault()
    setIsDragging(false)
    const file = event.dataTransfer.files?.[0]
    if (file) void uploadFile(file)
  }

  const handleClear = () => {
    clearLocalPreview()
    setUploadError('')
    onChange(null)
  }

  return (
    <div className="space-y-1.5">
      {previewUrl ? (
        <div className="relative h-32 w-32">
          <img
            src={previewUrl}
            alt="参考图"
            className="h-32 w-32 rounded-lg border object-cover"
          />
          <button
            type="button"
            aria-label="移除参考图"
            onClick={handleClear}
            disabled={uploading || disabled}
            className="absolute -right-2 -top-2 flex h-6 w-6 items-center justify-center rounded-full bg-background text-muted-foreground shadow-sm ring-1 ring-border transition-colors hover:text-foreground disabled:opacity-50"
          >
            <X className="h-3.5 w-3.5" />
          </button>
          {uploading ? (
            <div className="absolute inset-0 flex items-center justify-center rounded-lg bg-black/40">
              <Loader2 className="h-5 w-5 animate-spin text-white" />
            </div>
          ) : null}
        </div>
      ) : (
        <button
          type="button"
          aria-label="上传参考图"
          disabled={uploading || disabled}
          className={`flex h-32 w-32 cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border-2 border-dashed px-2 text-center transition-colors ${
            isDragging
              ? 'border-ring bg-muted/50'
              : 'border-muted-foreground/25 hover:border-muted-foreground/50'
          }`}
          onClick={() => {
            if (!uploading && !disabled) fileInputRef.current?.click()
          }}
          onDragOver={(event) => {
            event.preventDefault()
            if (!uploading && !disabled) setIsDragging(true)
          }}
          onDragLeave={() => setIsDragging(false)}
          onDrop={handleDrop}
        >
          {uploading ? (
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          ) : (
            <>
              <Upload className="h-5 w-5 text-muted-foreground" />
              <span className="text-[10px] font-medium text-foreground">点击上传</span>
              <span className="text-[8px] text-muted-foreground/70">JPG/PNG/WebP/GIF · ≤10MB</span>
            </>
          )}
        </button>
      )}

      <input
        ref={fileInputRef}
        type="file"
        accept={ACCEPTED_IMAGE_TYPES}
        aria-label={ariaLabel}
        onChange={handleFileChange}
        disabled={uploading || disabled}
        className="hidden"
      />

      {uploadError ? (
        <p role="alert" className="max-w-48 text-xs text-destructive">{uploadError}</p>
      ) : null}
    </div>
  )
}
