import { useEffect, useRef, useState } from 'react'
import { X, Download, Paintbrush, ChevronLeft, ChevronRight, Clock } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { ScrollArea } from '@/components/ui/scroll-area'
import { downloadBlob, openInExternalWindow } from '@/lib/tauri'

export interface PreviewImage {
  url: string
  width?: number
  height?: number
  index: number
}

export interface PreviewMetadata {
  provider: string
  model: string
  prompt: string
  revisedPrompt?: string
  quality?: string
  size?: string
  outputFormat?: string
  estimatedCost?: number
  finalCost?: number
  billingStatus?: string
  totalTokens?: number
  createdAt: string
}

interface ImagePreviewProps {
  images: PreviewImage[]
  initialIndex?: number
  metadata: PreviewMetadata
  canInpaint?: boolean
  onEdit?: (image: PreviewImage, index: number) => void
  onClose: () => void
}

function formatTime(iso: string): string {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return ''
  const diff = Date.now() - then
  const min = Math.floor(diff / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} 小时前`
  const day = Math.floor(hr / 24)
  if (day < 30) return `${day} 天前`
  return new Date(iso).toLocaleDateString('zh-CN')
}

export default function ImagePreview({
  images,
  initialIndex = 0,
  metadata,
  canInpaint,
  onEdit,
  onClose,
}: ImagePreviewProps) {
  const [currentIndex, setCurrentIndex] = useState(() =>
    Math.max(0, Math.min(initialIndex, Math.max(0, images.length - 1))),
  )
  const closeRef = useRef<HTMLButtonElement>(null)

  // Re-clamp when the images array reference changes (covers length change AND same-length replacement)
  useEffect(() => {
    setCurrentIndex((i) => Math.max(0, Math.min(i, Math.max(0, images.length - 1))))
  }, [images])

  // Focus close button on mount for keyboard users
  useEffect(() => {
    closeRef.current?.focus()
  }, [])

  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose()
        return
      }
      if (images.length <= 1) return
      if (e.key === 'ArrowLeft') {
        setCurrentIndex((i) => Math.max(0, i - 1))
      } else if (e.key === 'ArrowRight') {
        setCurrentIndex((i) => Math.min(images.length - 1, i + 1))
      }
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [images, onClose])

  if (images.length === 0) return null

  const current = images[Math.min(currentIndex, images.length - 1)]
  const canPrev = currentIndex > 0
  const canNext = currentIndex < images.length - 1
  const showNav = images.length > 1

  async function handleDownload() {
    try {
      const res = await fetch(current.url)
      const blob = await res.blob()
      // Native save dialog on desktop; anchor click in the browser.
      await downloadBlob(`designer-image-${currentIndex + 1}.${metadata.outputFormat || 'png'}`, blob)
    } catch {
      void openInExternalWindow(current.url)
    }
  }

  function handleEdit() {
    onEdit?.(current, currentIndex)
  }

  function goPrev() {
    setCurrentIndex((i) => Math.max(0, i - 1))
  }
  function goNext() {
    setCurrentIndex((i) => Math.min(images.length - 1, i + 1))
  }

  // Prefer actual rendered dimensions; fall back to requested size preset
  const sizeValue =
    current.width && current.height
      ? `${current.width}×${current.height}`
      : metadata.size || undefined

  const params: Array<{ label: string; value: string }> = [
    { label: '模型', value: `${metadata.provider} / ${metadata.model}` },
    ...(sizeValue ? [{ label: '尺寸', value: sizeValue }] : []),
    ...(metadata.quality ? [{ label: '质量', value: metadata.quality }] : []),
    ...(metadata.outputFormat
      ? [{ label: '格式', value: metadata.outputFormat.toUpperCase() }]
      : []),
    ...(metadata.estimatedCost ? [{ label: '预估积分', value: `${metadata.estimatedCost}` }] : []),
    ...(metadata.finalCost ? [{ label: '最终积分', value: `${metadata.finalCost}` }] : []),
    ...(metadata.billingStatus ? [{ label: '结算状态', value: metadata.billingStatus }] : []),
    ...(metadata.totalTokens ? [{ label: 'Usage Tokens', value: `${metadata.totalTokens}` }] : []),
  ]

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/90 p-4 backdrop-blur-lg"
      onClick={onClose}
    >
      <div
        className="relative flex max-h-[92vh] w-full max-w-[95vw] flex-col overflow-hidden rounded-2xl bg-card shadow-2xl shadow-black/60 ring-1 ring-white/10 md:flex-row"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Close button */}
        <Button
          ref={closeRef}
          variant="ghost"
          size="icon-sm"
          onClick={onClose}
          className="absolute right-3 top-3 z-10 rounded-lg bg-black/40 text-white/80 backdrop-blur-sm hover:bg-black/60 hover:text-white"
          title="关闭 (ESC)"
        >
          <X className="h-4 w-4" />
        </Button>

        {/* Image stage */}
        <div className="relative flex min-h-0 flex-1 items-center justify-center bg-black/60 p-4 md:p-8">
          <img
            src={current.url}
            alt="预览图片"
            className="max-h-[50vh] max-w-full object-contain md:max-h-[88vh]"
          />

          {showNav && (
            <>
              <div className="pointer-events-none absolute top-3 left-1/2 -translate-x-1/2 rounded-full bg-black/50 px-3 py-1 text-[11px] font-medium text-white/80 backdrop-blur-sm">
                {currentIndex + 1} / {images.length}
              </div>
              <Button
                variant="ghost"
                size="icon"
                disabled={!canPrev}
                onClick={goPrev}
                className="absolute top-1/2 left-3 size-10 -translate-y-1/2 rounded-full bg-black/40 text-white/80 backdrop-blur-sm hover:bg-black/60 hover:text-white disabled:opacity-30"
                title="上一张 (←)"
                aria-label="上一张"
              >
                <ChevronLeft className="h-5 w-5" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                disabled={!canNext}
                onClick={goNext}
                className="absolute top-1/2 right-3 size-10 -translate-y-1/2 rounded-full bg-black/40 text-white/80 backdrop-blur-sm hover:bg-black/60 hover:text-white disabled:opacity-30"
                title="下一张 (→)"
                aria-label="下一张"
              >
                <ChevronRight className="h-5 w-5" />
              </Button>
            </>
          )}
        </div>

        {/* Info panel */}
        <div className="flex min-h-0 w-full flex-col md:w-[380px] md:flex-shrink-0">
          <ScrollArea className="max-h-[40vh] flex-1 md:max-h-[92vh]">
            <div className="space-y-4 p-5">
              {/* Header */}
              <div className="space-y-1 pr-8">
                <div className="text-sm font-semibold leading-tight">{metadata.model}</div>
                <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
                  <Clock className="h-3 w-3" />
                  <span>{formatTime(metadata.createdAt)}</span>
                </div>
              </div>

              <Separator />

              {/* Prompt */}
              <div className="space-y-2">
                <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                  提示词
                </div>
                <p className="whitespace-pre-wrap break-words text-xs leading-relaxed text-foreground/90">
                  {metadata.prompt}
                </p>
                {metadata.revisedPrompt && (
                  <div className="mt-2 rounded-md bg-muted/40 p-2">
                    <div className="mb-1 text-[10px] uppercase tracking-wider text-muted-foreground">
                      改写后
                    </div>
                    <p className="whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">
                      {metadata.revisedPrompt}
                    </p>
                  </div>
                )}
              </div>

              <Separator />

              {/* Parameters */}
              <div className="space-y-2">
                <div className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                  参数
                </div>
                <div className="grid grid-cols-2 gap-1.5">
                  {params.map((p) => (
                    <Badge
                      key={p.label}
                      variant="secondary"
                      className="h-auto min-h-5 flex-col items-start gap-0 px-2 py-1 text-[10px] normal-case"
                    >
                      <span className="text-muted-foreground">{p.label}</span>
                      <span className="max-w-full truncate font-medium normal-case" title={p.value}>
                        {p.value}
                      </span>
                    </Badge>
                  ))}
                </div>
              </div>

              <Separator />

              {/* Actions */}
              <div className={`grid ${canInpaint && onEdit ? 'grid-cols-2' : 'grid-cols-1'} gap-2`}>
                {canInpaint && onEdit && (
                  <Button variant="outline" size="sm" onClick={handleEdit}>
                    <Paintbrush className="h-3.5 w-3.5" />
                    局部重绘
                  </Button>
                )}
                <Button variant="outline" size="sm" onClick={handleDownload}>
                  <Download className="h-3.5 w-3.5" />
                  下载
                </Button>
              </div>

              <div className="pt-1 text-center text-[10px] text-muted-foreground/60">
                按 ESC 关闭{showNav ? ' · ← → 切换' : ''}
              </div>
            </div>
          </ScrollArea>
        </div>
      </div>
    </div>
  )
}
