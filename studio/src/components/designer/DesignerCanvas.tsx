import { Download, Maximize2, ImageIcon } from 'lucide-react'
import GeneratingAnimation from '@/components/designer/GeneratingAnimation'
import type { GenerateImage } from '@/types/designer'

interface DesignerCanvasProps {
  images: GenerateImage[]
  isGenerating: boolean
  onImageClick: (image: GenerateImage) => void
}

export default function DesignerCanvas({ images, isGenerating, onImageClick }: DesignerCanvasProps) {
  async function handleDownload(url: string, index: number) {
    try {
      const res = await fetch(url)
      const blob = await res.blob()
      const blobUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = `designer-${index + 1}.png`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(blobUrl)
    } catch {
      window.open(url, '_blank')
    }
  }

  if (isGenerating) {
    return <GeneratingAnimation />
  }

  if (images.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4 text-center">
        <div className="relative">
          <div className="absolute -inset-4 rounded-full bg-primary/5 blur-xl" />
          <div className="relative flex h-16 w-16 items-center justify-center rounded-2xl border border-border bg-muted/50">
            <ImageIcon className="h-8 w-8 text-muted-foreground/50" />
          </div>
        </div>
        <div className="space-y-1.5">
          <h3 className="text-base font-medium text-muted-foreground">图片工作室</h3>
          <p className="max-w-xs text-sm text-muted-foreground/60">
            输入提示词，调整参数，生成你想要的图片。
          </p>
        </div>
        <div className="mt-2 flex items-center gap-2 text-[11px] text-muted-foreground/50">
          <kbd className="rounded-md border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px]">Ctrl</kbd>
          <span>+</span>
          <kbd className="rounded-md border border-border bg-muted px-1.5 py-0.5 font-mono text-[10px]">Enter</kbd>
          <span>发送</span>
        </div>
      </div>
    )
  }

  return (
    <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4">
      {images.map((img, i) => (
        <div
          key={`${img.url}-${i}`}
          className="group relative overflow-hidden rounded-2xl bg-muted animate-scale-in ring-1 ring-border transition-all duration-300 hover:ring-primary/30 hover:shadow-2xl hover:shadow-black/20"
          style={{ animationDelay: `${i * 50}ms`, animationFillMode: 'backwards' }}
        >
          <img
            src={img.url}
            alt={`生成图片 ${i + 1}`}
            className="aspect-square w-full object-cover transition-transform duration-500 group-hover:scale-[1.03]"
          />
          <span className="absolute top-2 left-2 flex h-5 w-5 items-center justify-center rounded-md bg-background/60 text-[10px] font-medium text-muted-foreground backdrop-blur-sm">
            {i + 1}
          </span>
          <div className="absolute inset-0 bg-gradient-to-t from-black/60 via-transparent to-transparent opacity-0 transition-all duration-300 group-hover:opacity-100">
            <div className="absolute bottom-3 left-0 right-0 flex items-center justify-center gap-2">
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  onImageClick(img)
                }}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-background/60 text-foreground backdrop-blur-sm transition-all duration-200 hover:bg-background/80"
                title="全屏查看"
              >
                <Maximize2 className="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation()
                  handleDownload(img.url, i)
                }}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-background/60 text-foreground backdrop-blur-sm transition-all duration-200 hover:bg-background/80"
                title="下载"
              >
                <Download className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}
