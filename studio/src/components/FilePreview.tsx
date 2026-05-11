import { useState, useEffect, useRef, useCallback } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { FileText, Download, Eye, Loader2, FileCode, File, ChevronLeft, ChevronRight, Video } from 'lucide-react'
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
import { Button } from '@/components/ui/button'

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

// --- Modal Content Renderer (stateless per-file renderer) ---

function FilePreviewModalContent({
  file,
  taskId,
  children,
}: {
  file: TaskFile
  taskId: string
  children?: React.ReactNode
}) {
  const [loading, setLoading] = useState(false)
  const [htmlContent, setHtmlContent] = useState('')
  const [textContent, setTextContent] = useState('')
  const [imgSrc, setImgSrc] = useState('')
  const blobUrlRef = useRef('')

  const isImage = file.mime_type?.startsWith('image/')
  const isVideo = file.mime_type?.startsWith('video/')
  const isHTML = file.mime_type === 'text/html'
  const isText = !isImage && !isVideo && !isHTML && (file.mime_type?.startsWith('text/') || file.file_name?.match(/\.(md|txt|json|yaml|yml|csv|log)$/i))
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

    if (isVideo) {
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
      toast.error('文件预览加载失败')
    } finally {
      setLoading(false)
    }
  }, [file, taskId, isImage, isVideo, isHTML, isText])

  useEffect(() => {
    fetchContent()
    return () => {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [fetchContent])

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
      toast.error('文件下载失败')
    }
  }

  // Determine modal size
  const modalClass = isImage
    ? 'sm:max-w-4xl'
    : isVideo
      ? 'sm:max-w-3xl'
      : isHTML
        ? 'sm:max-w-4xl'
        : isMD
          ? 'sm:max-w-3xl'
          : 'sm:max-w-2xl'

  return (
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

      {!loading && isVideo && imgSrc && (
        <video
          src={imgSrc}
          controls
          className="max-h-[70vh] w-full rounded-lg"
        />
      )}

      {!loading && isHTML && htmlContent && (
        <iframe
          srcDoc={htmlContent}
          sandbox="allow-scripts"
          className="w-full rounded-lg border border-border bg-background"
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
                .replace(new RegExp('>{1,3}\\s', 'gm'), '')
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
      {children}
    </DialogContent>
  )
}

// --- FilePreviewGallery (grouped modal with navigation) ---

export function FilePreviewGallery({ files, taskId, inlineItemClassName }: { files: TaskFile[]; taskId: string; inlineItemClassName?: string }) {
  const [open, setOpen] = useState(false)
  const [currentIndex, setCurrentIndex] = useState(0)

  const hasMultiple = files.length > 1

  const goPrev = useCallback(() => {
    setCurrentIndex((i) => (i - 1 + files.length) % files.length)
  }, [files.length])

  const goNext = useCallback(() => {
    setCurrentIndex((i) => (i + 1) % files.length)
  }, [files.length])

  const handleOpen = useCallback((index: number) => {
    setCurrentIndex(index)
    setOpen(true)
  }, [])

  // Keyboard navigation
  useEffect(() => {
    if (!open || !hasMultiple) return

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft') {
        e.preventDefault()
        goPrev()
      } else if (e.key === 'ArrowRight') {
        e.preventDefault()
        goNext()
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [open, hasMultiple, goPrev, goNext])

  const currentFile = files[currentIndex]

  return (
    <>
      {files.map((file, index) => (
        inlineItemClassName ? (
          <div key={file.id} className={inlineItemClassName}>
            <FilePreviewInline
              file={file}
              taskId={taskId}
              onClick={() => handleOpen(index)}
            />
          </div>
        ) : (
          <FilePreviewInline
            key={file.id}
            file={file}
            taskId={taskId}
            onClick={() => handleOpen(index)}
          />
        )
      ))}
      {open && currentFile && (
        <Dialog open={open} onOpenChange={setOpen}>
          <FilePreviewModalContent key={currentFile.id} file={currentFile} taskId={taskId}>
            {hasMultiple && (
              <>
                {/* Counter */}
                <div className="pointer-events-none absolute bottom-4 left-1/2 z-50 -translate-x-1/2">
                  <span className="rounded-full bg-black/60 px-3 py-1 text-xs font-medium text-white">
                    {currentIndex + 1} / {files.length}
                  </span>
                </div>
                {/* Prev arrow */}
                <button
                  onClick={goPrev}
                  aria-label="上一张"
                  className="absolute left-2 top-1/2 z-50 -translate-y-1/2 rounded-full bg-black/50 p-2 text-white/80 transition-colors hover:bg-black/70 hover:text-white"
                >
                  <ChevronLeft className="h-5 w-5" />
                </button>
                {/* Next arrow */}
                <button
                  onClick={goNext}
                  aria-label="下一张"
                  className="absolute right-2 top-1/2 z-50 -translate-y-1/2 rounded-full bg-black/50 p-2 text-white/80 transition-colors hover:bg-black/70 hover:text-white"
                >
                  <ChevronRight className="h-5 w-5" />
                </button>
              </>
            )}
          </FilePreviewModalContent>
        </Dialog>
      )}
    </>
  )
}

// --- Inline File Preview (no modal, click triggers parent callback) ---

function FilePreviewInline({
  file,
  taskId,
  onClick,
}: {
  file: TaskFile
  taskId: string
  onClick: () => void
}) {
  const isImage = file.mime_type?.startsWith('image/')
  const isVideo = file.mime_type?.startsWith('video/')
  const isHTML = file.mime_type === 'text/html'
  const isText = !isImage && !isVideo && !isHTML && (file.mime_type?.startsWith('text/') || file.file_name?.match(/\.(md|txt|json|yaml|yml|csv|log)$/i))
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
      toast.error('文件下载失败')
    }
  }

  const [imgSrc, setImgSrc] = useState<string>('')
  const blobUrlRef = useRef<string>('')
  useEffect(() => {
    if (!isImage && !isVideo) return
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
  }, [isImage, isVideo, file.url, taskId, file.id])

  if (isImage) {
    return (
      <div className="space-y-1">
        {imgSrc ? (
          <img
            src={imgSrc}
            alt={file.file_name}
            className="h-48 w-auto cursor-pointer rounded-md ring-1 ring-border transition-opacity hover:opacity-90"
            onClick={onClick}
          />
        ) : (
          <div className="flex h-48 w-36 items-center justify-center rounded-md border border-dashed border-border text-xs text-muted-foreground">
            加载中...
          </div>
        )}
        <p className="truncate text-xs text-muted-foreground" title={file.file_name}>{file.file_name}</p>
      </div>
    )
  }

  if (isVideo) {
    return (
      <div className="space-y-1">
        <div
          className="flex h-48 w-36 cursor-pointer items-center justify-center rounded-md ring-1 ring-border transition-opacity hover:opacity-90"
          onClick={onClick}
        >
          <div className="flex flex-col items-center gap-1.5 text-muted-foreground">
            <Video className="h-8 w-8" />
            <span className="text-xs">{formatSize(file.file_size)}</span>
          </div>
        </div>
        <p className="truncate text-xs text-muted-foreground" title={file.file_name}>{file.file_name}</p>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between rounded-lg border border-border p-3">
        <div className="flex items-center gap-3">
          {(isMD || isHTML) ? (
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
          {(isText || isHTML) && (
            <button
              onClick={onClick}
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
  )
}

// --- Standalone FilePreview (backward compat, single-file modal) ---

export function FilePreview({ file, taskId }: FilePreviewProps) {
  return (
    <FilePreviewGallery files={[file]} taskId={taskId} />
  )
}
