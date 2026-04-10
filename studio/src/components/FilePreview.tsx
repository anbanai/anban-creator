import { useState } from 'react'
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

  const handlePreview = async () => {
    if (isHTML && !showPreview) {
      setLoading(true)
      try {
        const html = await api.tasks.fetchPreviewHTML(taskId)
        setHtmlContent(html)
      } catch (err) {
        console.error('Failed to fetch preview:', err)
      } finally {
        setLoading(false)
      }
    }
    setShowPreview(!showPreview)
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

  if (isImage) {
    const imgSrc = file.oss_url || file.wechat_url || ''
    return (
      <div className="space-y-2">
        <img
          src={imgSrc}
          alt={file.file_name}
          className="max-w-full rounded-lg border border max-h-80 object-contain cursor-pointer hover:opacity-90"
          onClick={handleDownload}
        />
        <div className="flex items-center justify-between text-sm text-muted-foreground">
          <span>{file.file_name}</span>
          <span>{formatSize(file.file_size)}</span>
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
            className="px-3 py-1.5 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90 disabled:opacity-50"
          >
            {showPreview ? '关闭预览' : '预览'}
          </button>
          <button
            onClick={handleDownload}
            className="px-3 py-1.5 text-sm bg-secondary text-foreground rounded hover:bg-accent"
          >
            下载
          </button>
          <span className="text-sm text-muted-foreground">{file.file_name} ({formatSize(file.file_size)})</span>
        </div>
        {showPreview && htmlContent && (
          <iframe
            srcDoc={htmlContent}
            sandbox="allow-scripts"
            className="w-full border border rounded-lg bg-white"
            style={{ height: '600px' }}
            title="文章预览"
          />
        )}
        {loading && <p className="text-sm text-muted-foreground">加载预览中...</p>}
      </div>
    )
  }

  // Generic file
  return (
    <div className="flex items-center justify-between p-2 border border rounded-lg">
      <div className="flex items-center gap-2">
        <svg className="w-5 h-5 text-muted-foreground" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
        </svg>
        <div>
          <p className="text-sm font-medium text-foreground">{file.file_name}</p>
          <p className="text-xs text-muted-foreground">{file.mime_type} &middot; {formatSize(file.file_size)}</p>
        </div>
      </div>
      <button
        onClick={handleDownload}
        className="px-3 py-1.5 text-sm bg-secondary text-foreground rounded hover:bg-accent"
      >
        下载
      </button>
    </div>
  )
}
