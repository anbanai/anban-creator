import { Download, Maximize2, ImageIcon } from 'lucide-react'
import type { GenerateImage } from '@/types/designer'

interface DesignerCanvasProps {
  images: GenerateImage[]
  isGenerating: boolean
  onImageClick: (image: GenerateImage) => void
}

export default function DesignerCanvas({ images, isGenerating, onImageClick }: DesignerCanvasProps) {
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

  if (isGenerating) {
    return (
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            className="aspect-square w-full rounded-xl animate-shimmer"
            style={{
              background: 'linear-gradient(90deg, rgba(255,255,255,0.03) 25%, rgba(255,255,255,0.06) 50%, rgba(255,255,255,0.03) 75%)',
              backgroundSize: '200% 100%',
            }}
          />
        ))}
      </div>
    )
  }

  if (images.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-4 text-center">
        <div className="relative">
          <div className="absolute -inset-4 rounded-full bg-primary/5 blur-xl" />
          <div className="relative flex h-16 w-16 items-center justify-center rounded-2xl border border-white/10 bg-white/5">
            <ImageIcon className="h-8 w-8 text-white/30" />
          </div>
        </div>
        <div className="space-y-1.5">
          <h3 className="text-base font-medium text-white/50">图片工作室</h3>
          <p className="max-w-xs text-sm text-white/30">
            输入提示词，调整参数，生成你想要的图片。
          </p>
        </div>
        <div className="mt-2 flex items-center gap-2 text-[11px] text-white/20">
          <kbd className="rounded-md border border-white/10 bg-white/5 px-1.5 py-0.5 font-mono text-[10px]">Ctrl</kbd>
          <span>+</span>
          <kbd className="rounded-md border border-white/10 bg-white/5 px-1.5 py-0.5 font-mono text-[10px]">Enter</kbd>
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
          className="group relative overflow-hidden rounded-2xl bg-white/5 animate-scale-in ring-1 ring-white/10 transition-all duration-300 hover:ring-white/20 hover:shadow-2xl hover:shadow-black/40"
          style={{ animationDelay: `${i * 50}ms`, animationFillMode: 'backwards' }}
        >
          <img
            src={img.url}
            alt={`生成图片 ${i + 1}`}
            className="aspect-square w-full object-cover transition-transform duration-500 group-hover:scale-[1.03]"
          />
          <span className="absolute top-2 left-2 flex h-5 w-5 items-center justify-center rounded-md bg-black/40 text-[10px] font-medium text-white/60 backdrop-blur-sm">
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
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-white/15 text-white/90 backdrop-blur-sm transition-all duration-200 hover:bg-white/25 hover:text-white"
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
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-white/15 text-white/90 backdrop-blur-sm transition-all duration-200 hover:bg-white/25 hover:text-white"
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
