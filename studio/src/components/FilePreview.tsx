import { useState, useEffect, useRef, useCallback } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { FileText, Download, Eye, Loader2, FileCode, File, ChevronLeft, ChevronRight, Video } from 'lucide-react'
import { toast } from 'sonner'
import type { TaskFile } from '@/types'
import { api } from '../lib/api'
import { downloadBlob, isDesktop, saveUrlToFile } from '@/lib/tauri'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function isMarkdownFile(fileName: string): boolean {
  return /\.md$/i.test(fileName)
}

function isVideoFile(file: TaskFile): boolean {
  return file.mime_type?.startsWith('video/') || /\.(mp4|mov|webm|m4v)$/i.test(file.file_name)
}

function filePreviewTone(file: TaskFile) {
  const name = file.file_name.toLowerCase()
  const mime = file.mime_type?.toLowerCase() || ''
  if (isVideoFile(file)) return 'border-sky-500/45 bg-sky-500/5 text-sky-500'
  if (/\.md$/i.test(name) || mime.includes('markdown')) return 'border-emerald-500/45 bg-emerald-500/5 text-emerald-500'
  if (/\.(json|ya?ml)$/i.test(name) || mime.includes('json') || mime.includes('yaml')) return 'border-amber-500/45 bg-amber-500/5 text-amber-500'
  if (/\.html?$/i.test(name) || mime === 'text/html') return 'border-violet-500/45 bg-violet-500/5 text-violet-500'
  return 'border-border bg-muted/20 text-muted-foreground'
}

function filePreviewIcon(file: TaskFile) {
  if (isVideoFile(file)) return Video
  if (file.mime_type === 'text/html' || isMarkdownFile(file.file_name) || /\.(json|ya?ml)$/i.test(file.file_name)) return FileCode
  if (file.mime_type?.startsWith('text/')) return FileText
  return File
}

const openMontageRoleLabel: Record<string, string> = {
  final_video: '最终视频',
  delivery_manifest: '交付清单',
  source_manifest: '素材清单',
  timeline: '时间线',
  subtitles: '字幕',
  audio: '音频',
  run_log: '运行日志',
  failure_diagnosis: '失败诊断',
}

function taskFileRoleLabel(file: TaskFile, taskType?: string) {
  if (taskType === 'montage') {
    return openMontageRoleLabel[file.role] ?? ''
  }
  return ''
}

// --- Modal Content Renderer (stateless per-file renderer) ---

function FilePreviewModalContent({
  file,
  taskId,
  details,
  children,
  accessLocked = false,
  lockedMessage = '交付已锁定，充值后可恢复下载、预览和发布。',
}: {
  file: TaskFile
  taskId: string
  details?: React.ReactNode
  children?: React.ReactNode
  accessLocked?: boolean
  lockedMessage?: string
}) {
  const [loading, setLoading] = useState(false)
  const [htmlContent, setHtmlContent] = useState('')
  const [textContent, setTextContent] = useState('')
  const [imgSrc, setImgSrc] = useState('')
  const blobUrlRef = useRef('')

  const isImage = file.mime_type?.startsWith('image/')
  const isVideo = isVideoFile(file)
  const isHTML = file.mime_type === 'text/html'
  const isText = !isImage && !isVideo && !isHTML && (file.mime_type?.startsWith('text/') || file.file_name?.match(/\.(md|txt|json|yaml|yml|csv|log)$/i))
  const isMD = isText && isMarkdownFile(file.file_name)

  useEffect(() => {
    let cancelled = false

    async function load() {
      if (accessLocked) {
        setLoading(false)
        setHtmlContent('')
        setTextContent('')
        setImgSrc('')
        if (blobUrlRef.current) {
          URL.revokeObjectURL(blobUrlRef.current)
          blobUrlRef.current = ''
        }
        return
      }
      if (isImage || isVideo) {
        const url = file.url || ''
        if (!url) {
          setImgSrc('')
          if (blobUrlRef.current) {
            URL.revokeObjectURL(blobUrlRef.current)
            blobUrlRef.current = ''
          }
          return
        }
        if (url.startsWith('/api/v1/files/')) {
          try {
            const blob = await api.tasks.downloadFileBlob(taskId, file.id)
            if (cancelled) return
            const newUrl = URL.createObjectURL(blob)
            const oldUrl = blobUrlRef.current
            blobUrlRef.current = newUrl
            setImgSrc(newUrl)
            if (oldUrl) URL.revokeObjectURL(oldUrl)
          } catch (err) {
            console.error('Failed to fetch preview:', err)
            toast.error('文件预览加载失败')
          }
        } else {
          // Keep blobUrlRef mirroring the displayed src — unmount cleanup
          // revokes whatever's in the ref, so it must not hold a stale blob.
          if (blobUrlRef.current) {
            URL.revokeObjectURL(blobUrlRef.current)
            blobUrlRef.current = ''
          }
          setImgSrc(url)
        }
        return
      }

      setLoading(true)
      try {
        if (isHTML) {
          const html = await api.tasks.fetchPreviewHTML(taskId)
          if (cancelled) return
          setHtmlContent(html)
        } else if (isText) {
          const blob = await api.tasks.downloadFileBlob(taskId, file.id)
          if (cancelled) return
          setTextContent(await blob.text())
        }
      } catch (err) {
        console.error('Failed to fetch preview:', err)
        toast.error('文件预览加载失败')
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => { cancelled = true }
  }, [file, taskId, isImage, isVideo, isHTML, isText, accessLocked])

  useEffect(() => {
    return () => {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [])

  const handleDownload = async () => {
    if (accessLocked) return
    try {
      if (isDesktop() && /^https?:\/\//i.test(file.url || '') && await saveUrlToFile(file.url, file.file_name)) {
        return
      }
      const blob = await api.tasks.downloadFileBlob(taskId, file.id)
      // Native save dialog on desktop (WKWebView ignores <a download>); anchor
      // click in the browser. See lib/tauri.ts downloadBlob.
      await downloadBlob(file.file_name, blob)
    } catch (err) {
      console.error('Failed to download file:', err)
      toast.error('文件下载失败')
    }
  }

  // Determine modal size
  const modalClass = isImage
    ? 'sm:max-w-4xl'
    : isVideo
      ? 'sm:max-w-5xl'
      : isHTML
        ? 'sm:max-w-4xl'
        : 'sm:max-w-4xl'

  return (
    <DialogContent className={modalClass}>
      <DialogHeader>
        <DialogTitle className="truncate">{isVideo ? '视频结果' : file.file_name}</DialogTitle>
        <DialogDescription>
          {isVideo ? `${file.file_name} · ` : ''}{file.mime_type} &middot; {formatSize(file.file_size)}
        </DialogDescription>
      </DialogHeader>

      {accessLocked && (
        <div className="flex min-h-[240px] items-center justify-center rounded-lg border border-dashed border-border bg-muted/30 p-6 text-center text-sm text-muted-foreground">
          {lockedMessage}
        </div>
      )}

      {loading && (
        <div className={isImage || isVideo ? 'flex items-center justify-center py-16' : 'flex min-h-[70vh] items-center justify-center rounded-lg border border-border bg-background'}>
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          <span className="ml-2 text-sm text-muted-foreground">加载中...</span>
        </div>
      )}

      {!accessLocked && !loading && isImage && imgSrc && (
        <div className="flex items-center justify-center">
          <img
            src={imgSrc}
            alt={file.file_name}
            className="max-h-[70vh] max-w-full rounded-lg object-contain"
          />
        </div>
      )}

      {!accessLocked && !loading && isVideo && imgSrc && (
        <div className="grid max-h-[80vh] gap-4 sm:grid-cols-[minmax(0,1fr)_320px]">
          <div className="min-w-0">
            <video
              src={imgSrc}
              controls
              className="max-h-[78vh] w-full rounded-lg bg-black"
            />
          </div>
          {details && (
            <div className="min-h-0 overflow-y-auto pr-1">
              {details}
            </div>
          )}
        </div>
      )}

      {!accessLocked && !loading && isHTML && htmlContent && (
        <iframe
          srcDoc={htmlContent}
          sandbox="allow-scripts"
          className="h-[70vh] w-full rounded-lg border border-border bg-background"
          style={{ height: '70vh' }}
          title="文章预览"
        />
      )}

      {!accessLocked && !loading && isMD && textContent && (
        <div className="h-[70vh] overflow-y-auto rounded-lg border border-border bg-background p-6">
          <article className="prose prose-sm dark:prose-invert max-w-none prose-headings:font-semibold prose-a:text-primary prose-code:rounded prose-code:bg-muted prose-code:px-1 prose-code:py-0.5 prose-pre:bg-muted prose-pre:p-4 prose-table:border prose-th:border prose-th:p-2 prose-td:border prose-td:p-2">
            <Markdown remarkPlugins={[remarkGfm]}>{textContent}</Markdown>
          </article>
        </div>
      )}

      {!accessLocked && !loading && isText && !isMD && textContent && (
        <pre className="h-[70vh] overflow-auto rounded-lg border border-border bg-muted/50 p-4 text-sm text-foreground whitespace-pre-wrap break-words">
          {textContent}
        </pre>
      )}

      <div className="flex items-center justify-end gap-2 pt-2">
        {!accessLocked && isMD && textContent && (
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
        {!accessLocked && isMD && textContent && (
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
        {!accessLocked && isText && !isMD && textContent && (
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
        {!accessLocked && isHTML && htmlContent && (
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
        <Button variant="secondary" size="sm" onClick={handleDownload} disabled={accessLocked}>
          <Download className="h-3.5 w-3.5" />
          下载
        </Button>
      </div>
      {children}
    </DialogContent>
  )
}

// --- FilePreviewGallery (grouped modal with navigation) ---

export function FilePreviewGallery({
  files,
  taskId,
  taskType,
  inlineItemClassName,
  renderPreviewDetails,
  accessLocked = false,
  lockedMessage = '交付已锁定，充值后可恢复下载、预览和发布。',
}: {
  files: TaskFile[]
  taskId: string
  taskType?: string
  inlineItemClassName?: string
  renderPreviewDetails?: (file: TaskFile) => React.ReactNode
  accessLocked?: boolean
  lockedMessage?: string
}) {
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
    if (accessLocked) return
    setCurrentIndex(index)
    setOpen(true)
  }, [accessLocked])

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
              taskType={taskType}
              accessLocked={accessLocked}
              lockedMessage={lockedMessage}
              onClick={() => handleOpen(index)}
            />
          </div>
        ) : (
          <FilePreviewInline
            key={file.id}
            file={file}
            taskId={taskId}
            taskType={taskType}
            accessLocked={accessLocked}
            lockedMessage={lockedMessage}
            onClick={() => handleOpen(index)}
          />
        )
      ))}
      {open && currentFile && (
        <Dialog open={open} onOpenChange={setOpen}>
          <FilePreviewModalContent
            file={currentFile}
            taskId={taskId}
            details={renderPreviewDetails?.(currentFile)}
            accessLocked={accessLocked}
            lockedMessage={lockedMessage}
          >
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
  taskType,
  accessLocked = false,
  lockedMessage = '交付已锁定，充值后可恢复下载、预览和发布。',
  onClick,
}: {
  file: TaskFile
  taskId: string
  taskType?: string
  accessLocked?: boolean
  lockedMessage?: string
  onClick: () => void
}) {
  const isImage = file.mime_type?.startsWith('image/')
  const isVideo = isVideoFile(file)
  const isHTML = file.mime_type === 'text/html'
  const isText = !isImage && !isVideo && !isHTML && (file.mime_type?.startsWith('text/') || file.file_name?.match(/\.(md|txt|json|yaml|yml|csv|log)$/i))

  const handleDownload = async () => {
    if (accessLocked) return
    try {
      if (isDesktop() && /^https?:\/\//i.test(file.url || '') && await saveUrlToFile(file.url, file.file_name)) {
        return
      }
      const blob = await api.tasks.downloadFileBlob(taskId, file.id)
      // Native save dialog on desktop (WKWebView ignores <a download>); anchor
      // click in the browser. See lib/tauri.ts downloadBlob.
      await downloadBlob(file.file_name, blob)
    } catch (err) {
      console.error('Failed to download file:', err)
      toast.error('文件下载失败')
    }
  }

  const [imgSrc, setImgSrc] = useState<string>('')
  const blobUrlRef = useRef<string>('')
  useEffect(() => {
    if (!isImage && !isVideo) return
    if (accessLocked) {
      setImgSrc('')
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
      return
    }
    const url = file.url || ''
    if (!url) {
      setImgSrc('')
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
      return
    }
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
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
      setImgSrc(url)
    }
  }, [isImage, isVideo, file.url, taskId, file.id, accessLocked])

  if (isImage) {
    return (
      <div className="space-y-1">
        {accessLocked ? (
          <div className="flex h-48 w-36 items-center justify-center rounded-md border border-dashed border-border bg-muted/30 px-3 text-center text-xs text-muted-foreground">
            {lockedMessage}
          </div>
        ) : imgSrc ? (
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

  const tone = filePreviewTone(file)
  const Icon = filePreviewIcon(file)
  const canPreview = isVideo || isText || isHTML
  const roleLabel = taskFileRoleLabel(file, taskType)
  return (
    <div className="space-y-2">
      <div className={`flex items-center justify-between rounded-lg border p-3 ${tone}`}>
        <div className="flex min-w-0 items-center gap-3">
          <Icon className="h-5 w-5 shrink-0" />
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-foreground" title={file.file_name}>
              {roleLabel ? `${roleLabel} · ${file.file_name}` : file.file_name}
            </p>
            <p className="text-xs text-muted-foreground">{file.mime_type} &middot; {formatSize(file.file_size)}</p>
          </div>
        </div>
        <div className="ml-3 flex shrink-0 items-center gap-2">
          {canPreview && (
          <button
            onClick={onClick}
              disabled={accessLocked}
              className="flex items-center gap-1.5 rounded-md bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
              aria-label={`预览 ${file.file_name}`}
            >
              <Eye className="h-3.5 w-3.5" />
              预览
            </button>
          )}
          <button
            onClick={handleDownload}
            disabled={accessLocked}
            className="flex items-center gap-1.5 rounded-md bg-secondary px-3 py-1.5 text-sm text-foreground transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
            aria-label={`下载 ${file.file_name}`}
          >
            <Download className="h-3.5 w-3.5" />
            下载
          </button>
        </div>
      </div>
    </div>
  )
}
