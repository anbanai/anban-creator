import { useState, useEffect, useRef } from 'react'
import { FileText, Download, Eye, EyeOff, Copy } from 'lucide-react'
import { toast } from 'sonner'
import type { TaskFile } from '../lib/api'
import { api } from '../lib/api'

interface FilePreviewProps {
  file: TaskFile
  taskId: string
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function FilePreview({ file, taskId }: FilePreviewProps) {
  const [showPreview, setShowPreview] = useState(false)
  const [htmlContent, setHtmlContent] = useState('')
  const [loading, setLoading] = useState(false)

  const isImage = file.mime_type?.startsWith('image/')
  const isHTML = file.mime_type === 'text/html'
  const isText = !isImage && !isHTML && (file.mime_type?.startsWith('text/') || file.file_name?.match(/\.(md|txt|json|yaml|yml|csv|log)$/i))

  const [textContent, setTextContent] = useState('')

  const handlePreview = async () => {
    if (showPreview) {
      setShowPreview(false)
      return
    }
    setLoading(true)
    try {
      if (isHTML) {
        const html = await api.tasks.fetchPreviewHTML(taskId)
        setHtmlContent(html)
      } else if (isText) {
        const blob = await api.tasks.downloadFileBlob(taskId, file.id)
        setTextContent(await blob.text())
      }
      setShowPreview(true)
    } catch (err) {
      console.error('Failed to fetch preview:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleDownload = async () => {
    try {
      const blob = await api.tasks.downloadFileBlob(taskId, file.id)
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = file.file_name
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      console.error('Failed to download file:', err)
    }
  }

  // For local storage, fetch image via authenticated API and use blob URL
  const [imgSrc, setImgSrc] = useState<string>('')
  const blobUrlRef = useRef<string>('')
  useEffect(() => {
    if (!isImage) return
    const url = file.oss_url || file.wechat_url || ''
    if (!url) return
    if (url.startsWith('/api/v1/files/')) {
      // Local storage: needs auth header, fetch as blob
      let cancelled = false
      api.tasks.downloadFileBlob(taskId, file.id).then(blob => {
        if (!cancelled) {
          if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
          blobUrlRef.current = URL.createObjectURL(blob)
          setImgSrc(blobUrlRef.current)
        }
      }).catch(() => {})
      return () => {
        cancelled = true
        if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    } else {
      setImgSrc(url)
    }
  }, [isImage, file.oss_url, file.wechat_url, taskId, file.id])

  if (isImage) {
    return (
      <div className="space-y-2">
        {imgSrc ? (
          <img
            src={imgSrc}
            alt={file.file_name}
            className="max-h-80 max-w-full cursor-pointer rounded-lg object-contain ring-1 ring-border transition-opacity hover:opacity-90"
            onClick={handleDownload}
          />
        ) : (
          <div className="flex h-48 items-center justify-center rounded-lg border border-dashed border-border text-sm text-muted-foreground">
            加载中...
          </div>
        )}
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          <span className="truncate">{file.file_name}</span>
          <span className="shrink-0">{formatSize(file.file_size)}</span>
        </div>
      </div>
    )
  }

  if (isHTML) {
    return (
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <button
            onClick={handlePreview}
            disabled={loading}
            className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
          >
            {showPreview ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            {showPreview ? '关闭预览' : '预览'}
          </button>
          <button
            onClick={handleDownload}
            className="flex items-center gap-1.5 rounded-md bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors hover:bg-accent"
          >
            <Download className="h-3.5 w-3.5" />
            下载
          </button>
          <span className="text-sm text-muted-foreground">{file.file_name} ({formatSize(file.file_size)})</span>
        </div>
        {showPreview && htmlContent && (
          <iframe
            srcDoc={htmlContent}
            sandbox="allow-scripts"
            className="w-full rounded-lg border border-border bg-white"
            style={{ height: '600px' }}
            title="文章预览"
          />
        )}
        {loading && <p className="text-sm text-muted-foreground">加载预览中...</p>}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between rounded-lg border border-border p-3">
        <div className="flex items-center gap-3">
          <FileText className="h-5 w-5 text-muted-foreground" />
          <div>
            <p className="text-sm font-medium text-foreground">{file.file_name}</p>
            <p className="text-xs text-muted-foreground">{file.mime_type} &middot; {formatSize(file.file_size)}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          {isText && (
            <button
              onClick={handlePreview}
              disabled={loading}
              className="flex items-center gap-1.5 rounded-md bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors hover:bg-accent disabled:opacity-50"
            >
              {showPreview ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              {showPreview ? '关闭' : '预览'}
            </button>
          )}
          {isText && (
            <button
              onClick={async () => {
                try {
                  const text = textContent || await (await api.tasks.downloadFileBlob(taskId, file.id)).text()
                  navigator.clipboard.writeText(text)
                  toast.success('已复制')
                } catch { toast.error('复制失败') }
              }}
              disabled={loading}
              className="flex items-center gap-1.5 rounded-md bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors hover:bg-accent disabled:opacity-50"
            >
              <Copy className="h-3.5 w-3.5" />
              复制
            </button>
          )}
          <button
            onClick={handleDownload}
            className="flex items-center gap-1.5 rounded-md bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors hover:bg-accent"
          >
            <Download className="h-3.5 w-3.5" />
            下载
          </button>
        </div>
      </div>
      {showPreview && textContent && (
        <pre className="max-h-96 overflow-auto rounded-lg border border-border bg-muted/50 p-4 text-sm text-foreground whitespace-pre-wrap break-words">
          {textContent}
        </pre>
      )}
      {loading && <p className="text-sm text-muted-foreground">加载预览中...</p>}
    </div>
  )
}
