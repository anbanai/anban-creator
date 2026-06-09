import { useRef, useState, useCallback, useEffect } from 'react'
import { X, Eraser, Paintbrush, RotateCcw, Send } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Slider } from '@/components/ui/slider'
import { Textarea } from '@/components/ui/textarea'
import { toast } from 'sonner'
import { designerApi } from '@/lib/api/designer'
import { getApiErrorMessage } from '@/lib/http-client'
import { saveActiveGeneration } from '@/lib/designer-session'

interface ImageEditorProps {
  imageUrl: string
  provider: string
  onClose: () => void
  onGenerating: (generationId: string) => void
}

export default function ImageEditor({ imageUrl, provider, onClose, onGenerating }: ImageEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const imgRef = useRef<HTMLImageElement>(null)
  const [brushSize, setBrushSize] = useState(30)
  const [isEraser, setIsEraser] = useState(false)
  const [prompt, setPrompt] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [imageLoaded, setImageLoaded] = useState(false)
  const [hasStrokes, setHasStrokes] = useState(false)
  const paintingRef = useRef(false)
  const strokesRef = useRef<{ x: number; y: number; size: number; eraser: boolean }[][]>([])
  const currentStrokeRef = useRef<{ x: number; y: number; size: number; eraser: boolean }[]>([])

  // Draw mask strokes on the canvas
  const redrawCanvas = useCallback(() => {
    const canvas = canvasRef.current
    const img = imgRef.current
    if (!canvas || !img || !imageLoaded) return

    const ctx = canvas.getContext('2d')
    if (!ctx) return

    canvas.width = img.naturalWidth
    canvas.height = img.naturalHeight
    ctx.clearRect(0, 0, canvas.width, canvas.height)

    // Draw all strokes
    const allStrokes = [...strokesRef.current]
    if (currentStrokeRef.current.length > 0) {
      allStrokes.push(currentStrokeRef.current)
    }

    for (const stroke of allStrokes) {
      if (stroke.length === 0) continue
      const first = stroke[0]

      if (first.eraser) {
        ctx.globalCompositeOperation = 'destination-out'
      } else {
        ctx.globalCompositeOperation = 'source-over'
        ctx.fillStyle = 'rgba(255, 100, 50, 0.45)'
      }

      ctx.beginPath()
      for (const point of stroke) {
        ctx.moveTo(point.x + point.size / 2, point.y)
        ctx.arc(point.x, point.y, point.size / 2, 0, Math.PI * 2)
      }
      ctx.fill()
    }

    ctx.globalCompositeOperation = 'source-over'
  }, [imageLoaded])

  // Get canvas coordinates from mouse/touch event
  function getCanvasPoint(e: React.MouseEvent | React.TouchEvent) {
    const canvas = canvasRef.current
    const img = imgRef.current
    if (!canvas || !img) return null

    const rect = canvas.getBoundingClientRect()
    let clientX: number, clientY: number
    if ('touches' in e) {
      if (e.touches.length === 0) return null
      clientX = e.touches[0].clientX
      clientY = e.touches[0].clientY
    } else {
      clientX = e.clientX
      clientY = e.clientY
    }

    // Scale from display size to actual canvas size
    const scaleX = canvas.width / rect.width
    const scaleY = canvas.height / rect.height
    const brushScale = scaleX // scale brush size proportionally

    return {
      x: (clientX - rect.left) * scaleX,
      y: (clientY - rect.top) * scaleY,
      size: brushSize * brushScale,
      eraser: isEraser,
    }
  }

  function handlePointerDown(e: React.MouseEvent | React.TouchEvent) {
    e.preventDefault()
    const point = getCanvasPoint(e)
    if (!point) return
    paintingRef.current = true
    currentStrokeRef.current = [point]
    redrawCanvas()
  }

  function handlePointerMove(e: React.MouseEvent | React.TouchEvent) {
    if (!paintingRef.current) return
    e.preventDefault()
    const point = getCanvasPoint(e)
    if (!point) return
    currentStrokeRef.current.push(point)
    redrawCanvas()
  }

  function handlePointerUp() {
    if (!paintingRef.current) return
    paintingRef.current = false
    if (currentStrokeRef.current.length > 0) {
      strokesRef.current.push(currentStrokeRef.current)
      currentStrokeRef.current = []
      setHasStrokes(true)
    }
  }

  function handleUndo() {
    strokesRef.current.pop()
    setHasStrokes(strokesRef.current.length > 0)
    redrawCanvas()
  }

  function handleClear() {
    strokesRef.current = []
    currentStrokeRef.current = []
    setHasStrokes(false)
    redrawCanvas()
  }

  // Export the mask as a PNG File (alpha channel: painted = transparent, unpainted = opaque)
  function exportMask(): Promise<File | null> {
    const canvas = canvasRef.current
    const img = imgRef.current
    if (!canvas || !img) return Promise.resolve(null)

    const w = img.naturalWidth
    const h = img.naturalHeight

    const maskCanvas = document.createElement('canvas')
    maskCanvas.width = w
    maskCanvas.height = h
    const ctx = maskCanvas.getContext('2d')!

    // Fill with opaque white (alpha = 255)
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(0, 0, w, h)

    // Cut out painted areas (make transparent)
    ctx.globalCompositeOperation = 'destination-out'

    for (const stroke of strokesRef.current) {
      if (stroke.length === 0) continue
      ctx.beginPath()
      for (const point of stroke) {
        ctx.moveTo(point.x + point.size / 2, point.y)
        ctx.arc(point.x, point.y, point.size / 2, 0, Math.PI * 2)
      }
      ctx.fill()
    }

    ctx.globalCompositeOperation = 'source-over'

    return new Promise((resolve) => {
      maskCanvas.toBlob((blob) => {
        if (!blob) resolve(null)
        else resolve(new File([blob], 'mask.png', { type: 'image/png' }))
      }, 'image/png')
    })
  }

  async function handleSubmit() {
    if (strokesRef.current.length === 0) {
      toast.error('请先涂抹需要编辑的区域')
      return
    }
    if (!prompt.trim()) {
      toast.error('请输入编辑描述')
      return
    }

    setIsSubmitting(true)

    try {
      // Export mask
      const maskFile = await exportMask()
      if (!maskFile) {
        toast.error('导出蒙版失败')
        setIsSubmitting(false)
        return
      }

      // Download source image and upload both files in parallel
      const [sourceRes, maskRes] = await Promise.all([
        fetch(imageUrl).then((r) => r.blob()).then((blob) => {
          const file = new File([blob], 'source.png', { type: 'image/png' })
          return designerApi.uploadReference(file)
        }),
        designerApi.uploadReference(maskFile),
      ])

      // Start inpainting generation
      const { generation_id } = await designerApi.generate({
        channel_id: '',
        prompt: prompt.trim(),
        provider,
        reference_file_ids: [sourceRes.file_id],
        mask_file_id: maskRes.file_id,
      })

      saveActiveGeneration(generation_id)
      onGenerating(generation_id)
      onClose()
    } catch (err) {
      toast.error(getApiErrorMessage(err, '编辑图片失败，请重试'))
    } finally {
      setIsSubmitting(false)
    }
  }

  // ESC to close
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/90 backdrop-blur-lg"
      onClick={onClose}
    >
      <div
        ref={containerRef}
        className="relative flex max-h-[95vh] max-w-[95vw] flex-col gap-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-1">
          <div className="flex items-center gap-3">
            <h3 className="text-sm font-semibold text-white/90">局部编辑</h3>
            <span className="text-[11px] text-white/40">涂抹需要重新生成的区域</span>
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onClose}
            className="rounded-lg bg-white/10 text-white/70 backdrop-blur-sm hover:bg-white/20 hover:text-white"
          >
            <X className="h-4 w-4" />
          </Button>
        </div>

        {/* Canvas area */}
        <div className="relative overflow-hidden rounded-xl ring-1 ring-white/10">
          <img
            ref={imgRef}
            src={imageUrl}
            alt="编辑图片"
            className="block max-h-[60vh] max-w-[80vw] object-contain"
            crossOrigin="anonymous"
            onLoad={() => setImageLoaded(true)}
          />
          {imageLoaded && (
            <canvas
              ref={canvasRef}
              className="absolute inset-0 h-full w-full cursor-crosshair touch-none"
              style={{ mixBlendMode: 'multiply' }}
              onMouseDown={handlePointerDown}
              onMouseMove={handlePointerMove}
              onMouseUp={handlePointerUp}
              onMouseLeave={handlePointerUp}
              onTouchStart={handlePointerDown}
              onTouchMove={handlePointerMove}
              onTouchEnd={handlePointerUp}
            />
          )}
        </div>

        {/* Toolbar + prompt */}
        <div className="flex items-end gap-3">
          {/* Brush tools */}
          <div className="flex items-center gap-2 rounded-xl bg-white/5 px-3 py-2 backdrop-blur-sm">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setIsEraser(false)}
              className={`rounded-lg ${!isEraser ? 'bg-primary/20 text-primary' : 'text-white/50 hover:text-white/80'}`}
              title="画笔"
            >
              <Paintbrush className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setIsEraser(true)}
              className={`rounded-lg ${isEraser ? 'bg-primary/20 text-primary' : 'text-white/50 hover:text-white/80'}`}
              title="橡皮擦"
            >
              <Eraser className="h-3.5 w-3.5" />
            </Button>
            <div className="mx-1 h-5 w-px bg-white/10" />
            <Slider
              value={[brushSize]}
              onValueChange={(v) => setBrushSize(Array.isArray(v) ? v[0] : v)}
              min={5}
              max={100}
              step={1}
              className="w-20"
            />
            <span className="w-6 text-center text-[11px] tabular-nums text-white/50">{brushSize}</span>
            <div className="mx-1 h-5 w-px bg-white/10" />
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleUndo}
              disabled={!hasStrokes}
              className="rounded-lg text-white/50 hover:text-white/80"
              title="撤销"
            >
              <RotateCcw className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleClear}
              disabled={!hasStrokes}
              className="text-[11px] text-white/50 hover:text-white/80"
            >
              清除
            </Button>
          </div>

          {/* Prompt + submit */}
          <div className="flex flex-1 items-center gap-2 rounded-xl bg-white/5 px-3 py-2 backdrop-blur-sm">
            <Textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="描述你想在涂抹区域生成的内容..."
              className="min-h-[36px] max-h-[60px] flex-1 resize-none border-0 bg-transparent text-sm text-white/90 placeholder:text-white/30 focus-visible:ring-0"
              rows={1}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                  e.preventDefault()
                  handleSubmit()
                }
              }}
            />
            <Button
              size="icon-sm"
              onClick={handleSubmit}
              disabled={isSubmitting || !prompt.trim()}
              className="shrink-0 rounded-lg bg-primary text-primary-foreground hover:bg-primary/90"
            >
              <Send className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
