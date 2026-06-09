import { useEffect } from 'react'
import { X, Download, Paintbrush } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface ImagePreviewProps {
  imageUrl: string
  canInpaint?: boolean
  onEdit?: () => void
  onClose: () => void
}

export default function ImagePreview({ imageUrl, canInpaint, onEdit, onClose }: ImagePreviewProps) {
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  async function handleDownload() {
    try {
      const res = await fetch(imageUrl)
      const blob = await res.blob()
      const blobUrl = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = blobUrl
      a.download = 'designer-image.png'
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(blobUrl)
    } catch {
      window.open(imageUrl, '_blank')
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/90 backdrop-blur-lg"
      onClick={onClose}
    >
      <div className="relative max-h-[90vh] max-w-[90vw]" onClick={(e) => e.stopPropagation()}>
        <img
          src={imageUrl}
          alt="预览图片"
          className="max-h-[90vh] max-w-[90vw] rounded-xl object-contain shadow-2xl shadow-black/60 ring-1 ring-white/10"
        />
        <div className="absolute top-4 right-4 flex gap-2">
          {canInpaint && onEdit && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={onEdit}
              className="rounded-lg bg-black/40 text-white/80 backdrop-blur-sm hover:bg-black/60 hover:text-white"
              title="局部编辑"
            >
              <Paintbrush className="h-4 w-4" />
            </Button>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={handleDownload}
            className="rounded-lg bg-black/40 text-white/80 backdrop-blur-sm hover:bg-black/60 hover:text-white"
          >
            <Download className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onClose}
            className="rounded-lg bg-black/40 text-white/80 backdrop-blur-sm hover:bg-black/60 hover:text-white"
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <div className="absolute bottom-6 left-1/2 -translate-x-1/2">
        <span className="rounded-lg bg-white/5 px-3 py-1.5 text-[11px] text-white/40 backdrop-blur-sm">
          按 ESC 或点击空白处关闭
        </span>
      </div>
    </div>
  )
}
