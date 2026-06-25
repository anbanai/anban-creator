import { useEffect, useRef, useState } from 'react'
import { Loader2, Plus, ZoomIn } from 'lucide-react'
import http from '@/lib/http-client'
import { isInternalStorageUrl, normalizeStorageUrl } from '@/lib/storage-url'
import {
  Dialog,
  DialogContent,
} from '@/components/ui/dialog'

interface MultiImageUploadProps {
  value?: string[]
  onChange?: (urls: string[]) => void
  purpose?: string
  /** Max number of images. Defaults to 16 (OpenAI GPT Image multi-reference cap). */
  max?: number
}

const MAX_SIZE_MB = 10
const ACCEPTED = 'image/jpeg,image/png,image/webp,image/gif,image/bmp'

// MultiImageUpload manages an ordered list of image URLs (e.g. e-commerce product
// photos). Each upload POSTs to /files/upload and appends the returned URL; order
// is preserved end-to-end (the server materializes them as product_01..NN in the
// agent workspace). Mirrors ReferenceImageUpload's upload + internal-storage
// preview pattern, extended to a list.
export function MultiImageUpload({ value = [], onChange, purpose, max = 16 }: MultiImageUploadProps) {
  const [uploading, setUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const [isDragging, setIsDragging] = useState(false)
  const [enlargeSrc, setEnlargeSrc] = useState<string>('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  const urls = value
  const atMax = urls.length >= max

  const uploadFiles = async (files: FileList | File[]) => {
    const incoming = Array.from(files)
    if (incoming.length === 0) return
    const room = max - urls.length
    if (room <= 0) {
      setUploadError(`最多 ${max} 张图片`)
      return
    }
    const toUpload = incoming.slice(0, room)
    setUploadError('')
    setUploading(true)
    const next: string[] = []
    try {
      for (const file of toUpload) {
        if (file.size > MAX_SIZE_MB * 1024 * 1024) {
          setUploadError(`${file.name} 超过 ${MAX_SIZE_MB}MB，已跳过`)
          continue
        }
        const formData = new FormData()
        formData.append('file', file)
        if (purpose) formData.append('purpose', purpose)
        const res = await http.post('/files/upload', formData, {
          headers: { 'Content-Type': 'multipart/form-data' },
        })
        const url = (res.data as { data?: { url?: string } })?.data?.url
        if (url) next.push(url)
      }
      if (next.length > 0) onChange?.([...urls, ...next])
    } catch (err: any) {
      setUploadError(err?.response?.data?.msg || '上传失败，请重试')
    } finally {
      setUploading(false)
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) void uploadFiles(e.target.files)
    e.target.value = ''
  }

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }
  const handleDragLeave = (e: React.DragEvent) => {
    if (!e.currentTarget.contains(e.relatedTarget as Node)) setIsDragging(false)
  }
  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    if (e.dataTransfer.files?.length) void uploadFiles(e.dataTransfer.files)
  }

  const removeAt = (idx: number) => {
    onChange?.(urls.filter((_, i) => i !== idx))
  }

  return (
    <div>
      <div className="flex flex-wrap gap-2">
        {urls.map((url, idx) => (
          <PhotoThumb
            key={`${url}-${idx}`}
            url={url}
            index={idx}
            onRemove={() => removeAt(idx)}
            onEnlarge={() => setEnlargeSrc(url)}
          />
        ))}

        {!atMax && (
          <button
            type="button"
            onClick={() => !uploading && fileInputRef.current?.click()}
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
            disabled={uploading}
            className={`flex h-24 w-24 cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border-2 border-dashed text-center transition-colors ${
              isDragging
                ? 'border-ring bg-muted/50'
                : 'border-muted-foreground/25 hover:border-muted-foreground/50'
            } disabled:cursor-not-allowed disabled:opacity-50`}
          >
            {uploading ? (
              <>
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                <span className="text-[10px] text-muted-foreground">上传中…</span>
              </>
            ) : (
              <>
                <Plus className="h-5 w-5 text-muted-foreground" />
                <span className="text-[10px] font-medium text-foreground">添加产品图</span>
                <span className="text-[8px] text-muted-foreground/70">{urls.length}/{max}</span>
              </>
            )}
          </button>
        )}
      </div>

      {uploadError && <p className="mt-1 text-xs text-destructive">{uploadError}</p>}
      {urls.length === 0 && !uploading && (
        <p className="mt-1 text-[11px] text-muted-foreground">
          上传多张产品图（正面 / 细节 / 包装 / 场景），用于构建产品档案并保证跨图一致。可拖拽到上方区域。
        </p>
      )}

      <input
        ref={fileInputRef}
        type="file"
        accept={ACCEPTED}
        multiple
        onChange={handleFileChange}
        className="hidden"
      />

      <Dialog open={!!enlargeSrc} onOpenChange={(open) => !open && setEnlargeSrc('')}>
        <DialogContent className="sm:max-w-2xl">
          {enlargeSrc && (
            <div className="flex items-center justify-center">
              <EnlargeableImage src={enlargeSrc} alt="产品图放大" />
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}

// PhotoThumb renders a single product photo with internal-storage blob preview,
// a remove button, and click-to-enlarge. Self-contained blob lifecycle.
function PhotoThumb({ url, index, onRemove, onEnlarge }: {
  url: string
  index: number
  onRemove: () => void
  onEnlarge: () => void
}) {
  const [previewUrl, setPreviewUrl] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const blobUrlRef = useRef('')

  useEffect(() => {
    setLoading(true)
    setError(false)
    const normalized = normalizeStorageUrl(url)
    if (isInternalStorageUrl(normalized)) {
      http.get(normalized, { responseType: 'blob' })
        .then((res) => {
          if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
          blobUrlRef.current = URL.createObjectURL(res.data)
          setPreviewUrl(blobUrlRef.current)
        })
        .catch(() => {
          setError(true)
          setPreviewUrl('')
        })
        .finally(() => setLoading(false))
    } else {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
      setPreviewUrl(url)
      setLoading(false)
    }
    return () => {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [url])

  return (
    <div className="group relative">
      <div className="relative cursor-pointer" onClick={onEnlarge}>
        {loading ? (
          <div className="flex h-24 w-24 items-center justify-center rounded-lg border">
            <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
          </div>
        ) : error ? (
          <div className="flex h-24 w-24 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-destructive/50 text-destructive">
            <span className="text-[10px]">加载失败</span>
          </div>
        ) : (
          <>
            <img
              src={previewUrl}
              alt={`产品图 ${index + 1}`}
              className="h-24 w-24 rounded-lg border object-cover"
              onError={() => setError(true)}
            />
            <div className="absolute inset-0 flex items-center justify-center rounded-lg bg-black/0 transition-colors group-hover:bg-black/30">
              <ZoomIn className="h-5 w-5 text-white opacity-0 transition-opacity group-hover:opacity-100" />
            </div>
          </>
        )}
        <span className="absolute left-1 top-1 rounded bg-black/60 px-1 text-[9px] text-white">
          {index + 1}
        </span>
      </div>
      <button
        type="button"
        onClick={onRemove}
        className="absolute -right-2 -top-2 flex h-5 w-5 items-center justify-center rounded-full bg-destructive text-destructive-foreground shadow-sm transition-colors hover:bg-destructive/90"
      >
        <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><path d="M18 6 6 18" /><path d="m6 6 12 12" /></svg>
      </button>
    </div>
  )
}

// EnlargeableImage resolves an internal-storage URL to a blob for full-size display.
function EnlargeableImage({ src, alt }: { src: string; alt: string }) {
  const [resolved, setResolved] = useState('')
  const blobUrlRef = useRef('')

  useEffect(() => {
    const normalized = normalizeStorageUrl(src)
    if (isInternalStorageUrl(normalized)) {
      http.get(normalized, { responseType: 'blob' })
        .then((res) => {
          if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
          blobUrlRef.current = URL.createObjectURL(res.data)
          setResolved(blobUrlRef.current)
        })
        .catch(() => setResolved(src))
    } else {
      setResolved(src)
    }
    return () => {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [src])

  if (!resolved) {
    return <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
  }
  return <img src={resolved} alt={alt} className="max-h-[80vh] max-w-full rounded-lg object-contain" />
}
