import { useEffect } from 'react'
import { X, Download } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface ImagePreviewProps {
  imageUrl: string
  onClose: () => void
}

export default function ImagePreview({ imageUrl, onClose }: ImagePreviewProps) {
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  function handleDownload() {
    const a = document.createElement('a')
    a.href = imageUrl
    a.download = 'designer-image.png'
    a.target = '_blank'
    a.rel = 'noopener noreferrer'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
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
