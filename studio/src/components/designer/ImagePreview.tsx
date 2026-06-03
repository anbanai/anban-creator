import { X, Download } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface ImagePreviewProps {
  imageUrl: string
  onClose: () => void
}

export default function ImagePreview({ imageUrl, onClose }: ImagePreviewProps) {
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
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={onClose}
    >
      <div className="relative max-h-[90vh] max-w-[90vw]" onClick={(e) => e.stopPropagation()}>
        <img
          src={imageUrl}
          alt="预览图片"
          className="max-h-[90vh] max-w-[90vw] rounded-lg object-contain shadow-2xl"
        />
        <div className="absolute top-3 right-3 flex gap-2">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={handleDownload}
            className="bg-black/50 text-white hover:bg-black/70 hover:text-white"
          >
            <Download className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onClose}
            className="bg-black/50 text-white hover:bg-black/70 hover:text-white"
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  )
}
