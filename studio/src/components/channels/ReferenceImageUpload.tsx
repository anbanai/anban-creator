import { useState, useEffect, useRef } from 'react'
import { Loader2, ZoomIn } from 'lucide-react'
import http from '@/lib/http-client'
import { FileUpload } from '@/components/ui/FileUpload'
import {
  Dialog,
  DialogContent,
} from '@/components/ui/dialog'

interface ReferenceImageUploadProps {
  value?: string
  onChange?: (url: string) => void
}

function isInternalUrl(url: string) {
  return url.startsWith('/api/v1/files/') || url.startsWith('/files/')
}

export function ReferenceImageUpload({ value, onChange }: ReferenceImageUploadProps) {
  const [previewUrl, setPreviewUrl] = useState<string>('')
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState(false)
  const [enlargeOpen, setEnlargeOpen] = useState(false)
  const blobUrlRef = useRef('')

  useEffect(() => {
    if (!value) {
      setPreviewUrl('')
      setPreviewError(false)
      return
    }

    setPreviewLoading(true)
    setPreviewError(false)

    if (isInternalUrl(value)) {
      http.get(value, { responseType: 'blob' })
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

  return (
    <>
      {previewUrl ? (
        <div className="relative group">
          <div
            className="relative cursor-pointer"
            onClick={() => setEnlargeOpen(true)}
          >
            <img
              src={previewUrl}
              alt="参考图"
              className="h-32 w-32 rounded-lg border object-cover"
              onError={() => setPreviewError(true)}
            />
            <div className="absolute inset-0 flex items-center justify-center rounded-lg bg-black/0 transition-colors group-hover:bg-black/30">
              <ZoomIn className="h-5 w-5 text-white opacity-0 transition-opacity group-hover:opacity-100" />
            </div>
          </div>
          <button
            type="button"
            onClick={() => onChange?.('')}
            className="absolute -right-2 -top-2 flex h-5 w-5 items-center justify-center rounded-full bg-destructive text-destructive-foreground shadow-sm transition-colors hover:bg-destructive/90"
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
          </button>
        </div>
      ) : previewLoading ? (
        <div className="flex h-32 w-32 items-center justify-center rounded-lg border border-dashed">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : previewError ? (
        <div className="flex h-32 w-32 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-destructive/50 text-xs text-destructive">
          <span>加载失败</span>
          <button
            type="button"
            onClick={() => onChange?.('')}
            className="text-muted-foreground hover:text-foreground"
          >
            重新上传
          </button>
        </div>
      ) : (
        <FileUpload value={value} onChange={onChange} />
      )}

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
