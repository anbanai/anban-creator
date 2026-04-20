import { useState, useEffect, useRef, useCallback } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { FileText, Download, Eye, Loader2, FileCode, File } from 'lucide-react'
import { toast } from 'sonner'
import type { TaskFile } from '../lib/api'
import { api } from '../lib/api'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/Button'

interface FilePreviewProps {
  file: TaskFile
  taskId: string
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function isMarkdownFile(fileName: string): boolean {
  return /\.md$/i.test(fileName)
}

// --- Modal Preview Component ---

function FilePreviewModal({
  file,
  taskId,
  open,
  onOpenChange,
}: {
  file: TaskFile
  taskId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [loading, setLoading] = useState(false)
  const [htmlContent, setHtmlContent] = useState('')
  const [textContent, setTextContent] = useState('')
  const [imgSrc, setImgSrc] = useState('')
  const blobUrlRef = useRef('')

  const isImage = file.mime_type?.startsWith('image/')
  const isHTML = file.mime_type === 'text/html'
  const isText = !isImage && !isHTML && (file.mime_type?.startsWith('text/') || file.file_name?.match(/\.(md|txt|json|yaml|yml|csv|log)$/i))
  const isMD = isText && isMarkdownFile(file.file_name)

  const fetchContent = useCallback(async () => {
    if (isImage) {
      const url = file.url || ''
      if (!url) return
      if (url.startsWith('/api/v1/files/')) {
        const blob = await api.tasks.downloadFileBlob(taskId, file.id)
        if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = URL.createObjectURL(blob)
        setImgSrc(blobUrlRef.current)
      } else {
        setImgSrc(url)
      }
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
    } catch (err) {
      console.error('Failed to fetch preview:', err)
    } finally {
      setLoading(false)
    }
  }, [file, taskId, isImage, isHTML, isText])

  // Fetch content when modal opens
  useEffect(() => {
    if (open) {
      fetchContent()
    }
    return () => {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [open, fetchContent])

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

  // Determine modal size
  const modalClass = isImage
    ? 'sm:max-w-4xl'
    : isHTML
      ? 'sm:max-w-4xl'
      : isMD
        ? 'sm:max-w-3xl'
        : 'sm:max-w-2xl'

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={modalClass}>
        <DialogHeader>
          <DialogTitle className="truncate">{file.file_name}</DialogTitle>
          <DialogDescription>
            {file.mime_type} &middot; {formatSize(file.file_size)}
          </DialogDescription>
        </DialogHeader>

        {loading && (
          <div className="flex items-center justify-center py-16">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            <span className="ml-2 text-sm text-muted-foreground">加载中...</span>
          </div>
        )}

        {!loading && isImage && imgSrc && (
          <div className="flex items-center justify-center">
            <img
              src={imgSrc}
              alt={file.file_name}
              className="max-h-[70vh] max-w-full rounded-lg object-contain"
            />
          </div>
        )}

        {!loading && isHTML && htmlContent && (
          <iframe
            srcDoc={htmlContent}
            sandbox="allow-scripts"
            className="w-full rounded-lg border border-border bg-white"
            style={{ height: '70vh' }}
            title="文章预览"
          />
        )}

        {!loading && isMD && textContent && (
          <div className="max-h-[70vh] overflow-y-auto rounded-lg border border-border bg-background p-6">
            <article className="prose prose-sm dark:prose-invert max-w-none prose-headings:font-semibold prose-a:text-primary prose-code:rounded prose-code:bg-muted prose-code:px-1 prose-code:py-0.5 prose-pre:bg-muted prose-pre:p-4 prose-table:border prose-th:border prose-th:p-2 prose-td:border prose-td:p-2">
              <Markdown remarkPlugins={[remarkGfm]}>{textContent}</Markdown>
            </article>
          </div>
        )}

        {!loading && isText && !isMD && textContent && (
          <pre className="max-h-[70vh] overflow-auto rounded-lg border border-border bg-muted/50 p-4 text-sm text-foreground whitespace-pre-wrap break-words">
            {textContent}
          </pre>
        )}

        <div className="flex items-center justify-end gap-2 pt-2">
          {isMD && textContent && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                // Strip markdown formatting for a clean plain-text copy
                const plain = textContent
                  .replace(/```[\s\S]*?```/g, (m) => m.replace(/```\w*\n?/g, ''))
                  .replace(/^#{1,6}\s+/gm, '')
                  .replace(/\*\*(.+?)\*\*/g, '$1')
                  .replace(/\*(.+?)\*/g, '$1')
                  .replace(/__(.+?)__/g, '$1')
                  .replace(/_(.+?)_/g, '$1')
                  .replace(/~~(.+?)~~/g, '$1')
                  .replace(/`([^`]+)`/g, '$1')
                  .replace(/^\s*[-*+]\s/gm, '  ')
                  .replace(/^\s*\d+\.\s/gm, '  ')
                  .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
                  .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
                  .replace(/^\|.*$/gm, (line) => line.replace(/^\|/, '  ').replace(/\|$/, '').replace(/\|/g, '  ').trim())
                  .replace(/^---+$/gm, '')
                  .replace(/>{1,3}\s/gm, '')
                  .replace(/\n{3,}/g, '\n\n')
                  .trim()
                navigator.clipboard.writeText(plain)
                toast.success('已复制为纯文本')
              }}
            >
              <FileText className="h-3.5 w-3.5" />
              复制纯文本
            </Button>
          )}
          {isMD && textContent && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                navigator.clipboard.writeText(textContent)
                toast.success('已复制 Markdown 源码')
              }}
            >
              <FileCode className="h-3.5 w-3.5" />
              复制 Markdown
            </Button>
          )}
          {isText && !isMD && textContent && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                navigator.clipboard.writeText(textContent)
                toast.success('已复制')
              }}
            >
              <File className="h-3.5 w-3.5" />
              复制文本
            </Button>
          )}
          {isHTML && htmlContent && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                navigator.clipboard.writeText(htmlContent)
                toast.success('已复制 HTML 源码')
              }}
            >
              <FileCode className="h-3.5 w-3.5" />
              复制 HTML
            </Button>
          )}
          <Button variant="secondary" size="sm" onClick={handleDownload}>
            <Download className="h-3.5 w-3.5" />
            下载
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// --- Main FilePreview Component ---

export function FilePreview({ file, taskId }: FilePreviewProps) {
  const [modalOpen, setModalOpen] = useState(false)

  const isImage = file.mime_type?.startsWith('image/')
  const isHTML = file.mime_type === 'text/html'
  const isText = !isImage && !isHTML && (file.mime_type?.startsWith('text/') || file.file_name?.match(/\.(md|txt|json|yaml|yml|csv|log)$/i))
  const isMD = isText && isMarkdownFile(file.file_name)

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
    const url = file.url || ''
    if (!url) return
    if (url.startsWith('/api/v1/files/')) {
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
  }, [isImage, file.url, taskId, file.id])

  if (isImage) {
    return (
      <>
        <div className="space-y-2">
          {imgSrc ? (
            <img
              src={imgSrc}
              alt={file.file_name}
              className="max-h-80 max-w-full cursor-pointer rounded-lg object-contain ring-1 ring-border transition-opacity hover:opacity-90"
              onClick={() => setModalOpen(true)}
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
        <FilePreviewModal file={file} taskId={taskId} open={modalOpen} onOpenChange={setModalOpen} />
      </>
    )
  }

  if (isHTML) {
    return (
      <>
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <button
              onClick={() => setModalOpen(true)}
              className="flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground transition-colors hover:bg-primary/90"
            >
              <Eye className="h-3.5 w-3.5" />
              预览
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
        </div>
        <FilePreviewModal file={file} taskId={taskId} open={modalOpen} onOpenChange={setModalOpen} />
      </>
    )
  }

  return (
    <>
      <div className="space-y-2">
        <div className="flex items-center justify-between rounded-lg border border-border p-3">
          <div className="flex items-center gap-3">
            {isMD ? (
              <FileCode className="h-5 w-5 text-muted-foreground" />
            ) : (
              <FileText className="h-5 w-5 text-muted-foreground" />
            )}
            <div>
              <p className="text-sm font-medium text-foreground">{file.file_name}</p>
              <p className="text-xs text-muted-foreground">{file.mime_type} &middot; {formatSize(file.file_size)}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            {isText && (
              <button
                onClick={() => setModalOpen(true)}
                className="flex items-center gap-1.5 rounded-md bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors hover:bg-accent"
              >
                <Eye className="h-3.5 w-3.5" />
                预览
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
      </div>
      <FilePreviewModal file={file} taskId={taskId} open={modalOpen} onOpenChange={setModalOpen} />
    </>
  )
}
