import { Download, Maximize2, ImageIcon } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import type { GenerateImage } from '@/types/designer'
import EmptyState from '@/components/EmptyState'

interface GenerationGridProps {
  images: GenerateImage[]
  isGenerating: boolean
  onImageClick: (image: GenerateImage) => void
}

export default function GenerationGrid({ images, isGenerating, onImageClick }: GenerationGridProps) {
  if (isGenerating) {
    return (
      <div className="grid grid-cols-2 gap-3">
        {Array.from({ length: 2 }).map((_, i) => (
          <Skeleton key={i} className="aspect-square w-full rounded-lg" />
        ))}
      </div>
    )
  }

  if (images.length === 0) {
    return (
      <EmptyState
        icon={ImageIcon}
        title="图片工作室"
        description="输入提示词，调整参数，生成你想要的图片。"
      />
    )
  }

  function handleDownload(url: string, index: number) {
    const a = document.createElement('a')
    a.href = url
    a.download = `designer-${index + 1}.png`
    a.target = '_blank'
    a.rel = 'noopener noreferrer'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  }

  return (
    <div className="grid grid-cols-2 gap-3">
      {images.map((img, i) => (
        <div
          key={`${img.url}-${i}`}
          className="group relative overflow-hidden rounded-lg border border-border bg-muted"
        >
          <img
            src={img.url}
            alt={`生成图片 ${i + 1}`}
            className="aspect-square w-full object-cover"
          />
          <div className="absolute inset-0 flex items-end justify-center gap-2 bg-black/0 opacity-0 transition-all group-hover:bg-black/30 group-hover:opacity-100">
            <div className="mb-3 flex gap-2">
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  onImageClick(img)
                }}
                className="flex h-8 w-8 items-center justify-center rounded-full bg-white/90 text-foreground shadow transition-colors hover:bg-white"
                title="全屏查看"
              >
                <Maximize2 className="h-4 w-4" />
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  handleDownload(img.url, i)
                }}
                className="flex h-8 w-8 items-center justify-center rounded-full bg-white/90 text-foreground shadow transition-colors hover:bg-white"
                title="下载"
              >
                <Download className="h-4 w-4" />
              </button>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
