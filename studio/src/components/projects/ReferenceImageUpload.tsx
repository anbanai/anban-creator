import { useState, useEffect, useRef } from 'react'
import { Loader2, X, ZoomIn, Upload } from 'lucide-react'
import http from '@/lib/http-client'
import { uploadToOSS, type DirectUploadPurpose } from '@/lib/direct-upload'
import { isInternalStorageUrl, normalizeStorageUrl } from '@/lib/storage-url'
import {
  Dialog,
  DialogContent,
} from '@/components/ui/dialog'

interface ReferenceImageUploadProps {
  value?: string
  onChange?: (url: string) => void
  purpose?: "project" | "reference"
  compact?: boolean
}

const MAX_SIZE_MB = 10
const ACCEPTED = "image/jpeg,image/png,image/webp,image/gif"

function directPurposeForReference(purpose?: "project" | "reference"): DirectUploadPurpose {
  return purpose === 'project' ? 'project_reference' : 'task_reference'
}

export function ReferenceImageUpload({ value, onChange, purpose, compact }: ReferenceImageUploadProps) {
  const [previewUrl, setPreviewUrl] = useState<string>('')
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const [enlargeOpen, setEnlargeOpen] = useState(false)
  const [isDragging, setIsDragging] = useState(false)
  const blobUrlRef = useRef('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!value) {
      setPreviewUrl('')
      setPreviewError(false)
      return
    }

    setPreviewLoading(true)
    setPreviewError(false)

    const normalized = normalizeStorageUrl(value)

    if (isInternalStorageUrl(normalized)) {
      http.get(normalized, { responseType: 'blob' })
        .then((res) => {
          if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
          blobUrlRef.current = URL.createObjectURL(res.data)
          setPreviewUrl(blobUrlRef.current)
        })
        .catch(() => {
          setPreviewError(true)
          setPreviewUrl('')
        })
        .finally(() => setPreviewLoading(false))
    } else {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
      setPreviewUrl(value)
      setPreviewLoading(false)
    }

    return () => {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [value])

  const uploadFile = async (file: File) => {
    if (file.size > MAX_SIZE_MB * 1024 * 1024) {
      setUploadError(`文件大小不能超过 ${MAX_SIZE_MB}MB`)
      return
    }
    setUploadError('')
    setUploading(true)
    try {
      const result = await uploadToOSS({
        purpose: directPurposeForReference(purpose),
        file,
      })
      if (result.publicUrl) onChange?.(result.publicUrl)
    } catch (err: any) {
      setUploadError(err?.message || err?.response?.data?.msg || '上传失败，请重试')
    } finally {
      setUploading(false)
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (file) void uploadFile(file)
    e.target.value = ''
  }

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleDragLeave = (e: React.DragEvent) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) {
      setIsDragging(false)
    }
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    const file = e.dataTransfer.files?.[0]
    if (file) void uploadFile(file)
  }

  const thumbSize = compact ? 'h-7 w-7' : 'h-32 w-32'

  return (
    <>
      {previewUrl ? (
        <div className={compact ? 'flex items-center gap-1.5' : 'relative group'}>
          <button
            type="button"
            className="group/preview relative block rounded-lg p-0"
            onClick={() => setEnlargeOpen(true)}
            aria-label="查看参考图"
          >
            <img
              src={previewUrl}
              alt="参考图"
              className={`${thumbSize} rounded-lg border object-cover`}
              onError={() => setPreviewError(true)}
            />
            <div className="absolute inset-0 flex items-center justify-center rounded-lg bg-black/0 transition-colors group-hover/preview:bg-black/30">
              <ZoomIn className={`${compact ? 'h-3 w-3' : 'h-5 w-5'} text-white opacity-0 transition-opacity group-hover/preview:opacity-100`} />
            </div>
          </button>
          <button
            type="button"
            aria-label="移除参考图"
            onClick={() => onChange?.('')}
            className={compact
              ? 'flex h-7 w-7 items-center justify-center rounded-md border border-input text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground'
              : 'absolute -right-2 -top-2 flex h-5 w-5 items-center justify-center rounded-full bg-background text-muted-foreground shadow-sm ring-1 ring-border transition-colors hover:text-foreground'
            }
          >
            <X className={compact ? 'h-3.5 w-3.5' : 'h-3 w-3'} />
          </button>
        </div>
      ) : previewLoading ? (
        <div className={`${thumbSize} flex items-center justify-center rounded-lg border border-dashed`}>
          <Loader2 className={`${compact ? 'h-3 w-3' : 'h-5 w-5'} animate-spin text-muted-foreground`} />
        </div>
      ) : previewError ? (
        <div className={`${thumbSize} flex flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-destructive/50 text-destructive`} style={{ minWidth: compact ? 28 : undefined }}>
          <span className="text-[10px]">加载失败</span>
          <button
            type="button"
            onClick={() => onChange?.('')}
            className="text-[10px] text-muted-foreground hover:text-foreground"
          >
            重新上传
          </button>
        </div>
      ) : compact ? (
        <>
          <button
            type="button"
            onClick={() => !uploading && fileInputRef.current?.click()}
            disabled={uploading}
            className="flex items-center gap-1 rounded-md border border-input bg-transparent px-2 py-1 text-xs text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground disabled:opacity-50"
          >
            {uploading ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                上传中
              </>
            ) : (
              <>
                <Upload className="h-3 w-3" />
                上传图片识别
              </>
            )}
          </button>
          {uploadError && <span className="text-[10px] text-destructive">{uploadError}</span>}
        </>
      ) : (
        <div
          className={`flex h-32 w-32 cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border-2 border-dashed px-2 text-center transition-colors ${
            isDragging
              ? 'border-ring bg-muted/50'
              : 'border-muted-foreground/25 hover:border-muted-foreground/50'
          }`}
          onClick={() => {
            if (uploading) return
            fileInputRef.current?.click()
          }}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
        >
          {uploading ? (
            <>
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              <span className="text-[10px] text-muted-foreground">上传中…</span>
            </>
          ) : uploadError ? (
            <div className="flex flex-col items-center gap-0.5">
              <span className="text-[9px] text-destructive">{uploadError}</span>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  setUploadError('')
                  fileInputRef.current?.click()
                }}
                className="text-[9px] text-muted-foreground hover:text-foreground"
              >
                重新上传
              </button>
            </div>
          ) : (
            <>
              <Upload className="h-5 w-5 text-muted-foreground" />
              <span className="text-[10px] font-medium text-foreground">点击上传</span>
              <span className="text-[8px] text-muted-foreground/70">JPG/PNG/WebP/GIF · ≤10MB</span>
            </>
          )}
        </div>
      )}

      <input
        ref={fileInputRef}
        type="file"
        accept={ACCEPTED}
        onChange={handleFileChange}
        className="hidden"
      />

      <Dialog open={enlargeOpen} onOpenChange={setEnlargeOpen}>
        <DialogContent className="sm:max-w-2xl">
          {previewUrl && (
            <div className="flex items-center justify-center">
              <img
                src={previewUrl}
                alt="参考图放大"
                className="max-h-[80vh] max-w-full rounded-lg object-contain"
              />
            </div>
          )}
        </DialogContent>
      </Dialog>
    </>
  )
}
