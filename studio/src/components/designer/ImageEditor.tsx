import { useRef, useState, useCallback, useEffect } from 'react'
import { X, Eraser, Paintbrush, RotateCcw, Send, Square, Circle, ZoomIn, ZoomOut, Maximize } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Slider } from '@/components/ui/slider'
import { Textarea } from '@/components/ui/textarea'
import { toast } from 'sonner'
import { designerApi } from '@/lib/api/designer'
import { getApiErrorMessage } from '@/lib/http-client'
import { saveActiveGeneration } from '@/lib/designer-session'

type ToolType = 'brush' | 'rect' | 'circle' | 'eraser'

interface BrushPoint {
  x: number
  y: number
  size: number
}

interface ShapeBounds {
  startX: number
  startY: number
  endX: number
  endY: number
}

interface BrushStroke {
  type: 'brush' | 'eraser'
  points: BrushPoint[]
}

interface ShapeStroke {
  type: 'rect' | 'circle'
  bounds: ShapeBounds
}

type AnyStroke = BrushStroke | ShapeStroke

function isShapeStroke(s: AnyStroke): s is ShapeStroke {
  return s.type === 'rect' || s.type === 'circle'
}

interface ImageEditorProps {
  imageUrl: string
  provider: string
  onClose: () => void
  onGenerating: (generationId: string) => void
}

const ZOOM_LEVELS = [0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4]

export default function ImageEditor({ imageUrl, provider, onClose, onGenerating }: ImageEditorProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const imgRef = useRef<HTMLImageElement>(null)
  const [tool, setTool] = useState<ToolType>('brush')
  const [brushSize, setBrushSize] = useState(30)
  const [feather, setFeather] = useState(0)
  const [prompt, setPrompt] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [imageLoaded, setImageLoaded] = useState(false)
  const [hasStrokes, setHasStrokes] = useState(false)
  const [zoom, setZoom] = useState(1)
  const [panOffset, setPanOffset] = useState({ x: 0, y: 0 })
  const [isPanning, setIsPanning] = useState(false)
  const [spaceHeld, setSpaceHeld] = useState(false)
  const panStartRef = useRef({ x: 0, y: 0 })
  const panOffsetStartRef = useRef({ x: 0, y: 0 })

  const paintingRef = useRef(false)
  const strokesRef = useRef<AnyStroke[]>([])
  const currentStrokeRef = useRef<BrushPoint[]>([])
  const currentShapeRef = useRef<ShapeBounds | null>(null)

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

    const allStrokes: AnyStroke[] = [...strokesRef.current]
    // Include current brush stroke
    if (currentStrokeRef.current.length > 0) {
      allStrokes.push({ type: 'brush', points: currentStrokeRef.current })
    }
    // Include current shape
    if (currentShapeRef.current && (tool === 'rect' || tool === 'circle')) {
      allStrokes.push({ type: tool, bounds: currentShapeRef.current })
    }

    for (const stroke of allStrokes) {
      if (stroke.type === 'eraser') {
        ctx.globalCompositeOperation = 'destination-out'
      } else {
        ctx.globalCompositeOperation = 'source-over'
        ctx.fillStyle = 'rgba(255, 100, 50, 0.45)'
      }

      if (!isShapeStroke(stroke)) {
        // Brush/eraser points
        const points = stroke.points
        if (points.length === 0) continue

        if (feather > 0 && stroke.type === 'brush') {
          ctx.shadowBlur = feather
          ctx.shadowColor = 'rgba(255, 100, 50, 0.3)'
        } else {
          ctx.shadowBlur = 0
        }

        ctx.beginPath()
        for (const point of points) {
          ctx.moveTo(point.x + point.size / 2, point.y)
          ctx.arc(point.x, point.y, point.size / 2, 0, Math.PI * 2)
        }
        ctx.fill()
        ctx.shadowBlur = 0
      } else {
        // Shape (rect or circle)
        const { startX, startY, endX, endY } = stroke.bounds
        if (stroke.type === 'rect') {
          ctx.fillRect(
            Math.min(startX, endX),
            Math.min(startY, endY),
            Math.abs(endX - startX),
            Math.abs(endY - startY),
          )
        } else {
          const cx = (startX + endX) / 2
          const cy = (startY + endY) / 2
          const rx = Math.abs(endX - startX) / 2
          const ry = Math.abs(endY - startY) / 2
          ctx.beginPath()
          ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2)
          ctx.fill()
        }
      }
    }

    ctx.globalCompositeOperation = 'source-over'
  }, [imageLoaded, tool, feather])

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

    const scaleX = canvas.width / rect.width
    const scaleY = canvas.height / rect.height

    return {
      x: (clientX - rect.left) * scaleX,
      y: (clientY - rect.top) * scaleY,
      size: (brushSize / zoom) * scaleX,
    }
  }

  function handlePointerDown(e: React.MouseEvent | React.TouchEvent) {
    // Panning with space held
    if (spaceHeld) {
      e.preventDefault()
      setIsPanning(true)
      const clientX = 'touches' in e ? (e.touches[0]?.clientX ?? 0) : e.clientX
      const clientY = 'touches' in e ? (e.touches[0]?.clientY ?? 0) : e.clientY
      panStartRef.current = { x: clientX, y: clientY }
      panOffsetStartRef.current = { ...panOffset }
      return
    }

    e.preventDefault()
    const point = getCanvasPoint(e)
    if (!point) return

    paintingRef.current = true

    if (tool === 'brush' || tool === 'eraser') {
      currentStrokeRef.current = [point]
    } else {
      currentShapeRef.current = { startX: point.x, startY: point.y, endX: point.x, endY: point.y }
    }
    redrawCanvas()
  }

  function handlePointerMove(e: React.MouseEvent | React.TouchEvent) {
    if (isPanning) {
      const clientX = 'touches' in e ? (e.touches[0]?.clientX ?? 0) : e.clientX
      const clientY = 'touches' in e ? (e.touches[0]?.clientY ?? 0) : e.clientY
      setPanOffset({
        x: panOffsetStartRef.current.x + (clientX - panStartRef.current.x),
        y: panOffsetStartRef.current.y + (clientY - panStartRef.current.y),
      })
      return
    }

    if (!paintingRef.current) return
    e.preventDefault()
    const point = getCanvasPoint(e)
    if (!point) return

    if (tool === 'brush' || tool === 'eraser') {
      currentStrokeRef.current.push(point)
    } else if (currentShapeRef.current) {
      currentShapeRef.current = { ...currentShapeRef.current, endX: point.x, endY: point.y }
    }
    redrawCanvas()
  }

  function handlePointerUp() {
    if (isPanning) {
      setIsPanning(false)
      return
    }

    if (!paintingRef.current) return
    paintingRef.current = false

    if (tool === 'brush' || tool === 'eraser') {
      if (currentStrokeRef.current.length > 0) {
        strokesRef.current.push({
          type: tool === 'eraser' ? 'eraser' : 'brush',
          points: [...currentStrokeRef.current],
        })
        currentStrokeRef.current = []
      }
    } else if (currentShapeRef.current) {
      strokesRef.current.push({
        type: tool as 'rect' | 'circle',
        bounds: currentShapeRef.current,
      })
      currentShapeRef.current = null
    }
    setHasStrokes(strokesRef.current.length > 0)
  }

  function handleUndo() {
    strokesRef.current.pop()
    setHasStrokes(strokesRef.current.length > 0)
    redrawCanvas()
  }

  function handleClear() {
    strokesRef.current = []
    currentStrokeRef.current = []
    currentShapeRef.current = null
    setHasStrokes(false)
    redrawCanvas()
  }

  function handleZoomIn() {
    const idx = ZOOM_LEVELS.findIndex((z: number) => z > zoom)
    if (idx >= 0) setZoom(ZOOM_LEVELS[idx])
  }

  function handleZoomOut() {
    // ES2020-compatible reverse find
    let idx = -1
    for (let i = ZOOM_LEVELS.length - 1; i >= 0; i--) {
      if (ZOOM_LEVELS[i] < zoom) {
        idx = i
        break
      }
    }
    if (idx >= 0) setZoom(ZOOM_LEVELS[idx])
  }

  function handleZoomFit() {
    setZoom(1)
    setPanOffset({ x: 0, y: 0 })
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

    ctx.fillStyle = '#ffffff'
    ctx.fillRect(0, 0, w, h)
    ctx.globalCompositeOperation = 'destination-out'

    for (const stroke of strokesRef.current) {
      if (!isShapeStroke(stroke)) {
        const points = stroke.points
        if (points.length === 0) continue

        if (feather > 0 && stroke.type === 'brush') {
          ctx.shadowBlur = feather
          ctx.shadowColor = 'rgba(0, 0, 0, 1)'
        }

        ctx.beginPath()
        for (const point of points) {
          ctx.moveTo(point.x + point.size / 2, point.y)
          ctx.arc(point.x, point.y, point.size / 2, 0, Math.PI * 2)
        }
        ctx.fill()
        ctx.shadowBlur = 0
      } else {
        const { startX, startY, endX, endY } = stroke.bounds
        if (stroke.type === 'rect') {
          ctx.fillRect(
            Math.min(startX, endX),
            Math.min(startY, endY),
            Math.abs(endX - startX),
            Math.abs(endY - startY),
          )
        } else {
          const cx = (startX + endX) / 2
          const cy = (startY + endY) / 2
          const rx = Math.abs(endX - startX) / 2
          const ry = Math.abs(endY - startY) / 2
          ctx.beginPath()
          ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2)
          ctx.fill()
        }
      }
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
      const maskFile = await exportMask()
      if (!maskFile) {
        toast.error('导出蒙版失败')
        setIsSubmitting(false)
        return
      }

      const [sourceRes, maskRes] = await Promise.all([
        fetch(imageUrl).then((r) => r.blob()).then((blob) => {
          const file = new File([blob], 'source.png', { type: 'image/png' })
          return designerApi.uploadReference(file)
        }),
        designerApi.uploadReference(maskFile),
      ])

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

  // Keyboard shortcuts
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
      if (e.key === ' ') {
        e.preventDefault()
        setSpaceHeld(true)
      }
    }
    function handleKeyUp(e: KeyboardEvent) {
      if (e.key === ' ') setSpaceHeld(false)
    }
    document.addEventListener('keydown', handleKey)
    document.addEventListener('keyup', handleKeyUp)
    return () => {
      document.removeEventListener('keydown', handleKey)
      document.removeEventListener('keyup', handleKeyUp)
    }
  }, [onClose])

  const cursorStyle = spaceHeld ? 'cursor-grab' : isPanning ? 'cursor-grabbing' : 'cursor-crosshair'

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

        {/* Canvas area with zoom/pan */}
        <div className="relative overflow-hidden rounded-xl ring-1 ring-white/10" style={{ maxHeight: '60vh' }}>
          <div
            style={{
              transform: `translate(${panOffset.x}px, ${panOffset.y}px) scale(${zoom})`,
              transformOrigin: 'center center',
              transition: isPanning ? 'none' : 'transform 0.15s ease-out',
            }}
          >
            <img
              ref={imgRef}
              src={imageUrl}
              alt="编辑图片"
              className="block max-h-[60vh] max-w-[80vw] object-contain"
              crossOrigin="anonymous"
              onLoad={() => setImageLoaded(true)}
              draggable={false}
            />
            {imageLoaded && (
              <canvas
                ref={canvasRef}
                className={`absolute inset-0 h-full w-full touch-none ${cursorStyle}`}
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

          {/* Zoom controls overlay */}
          <div className="absolute bottom-2 right-2 flex items-center gap-1 rounded-lg bg-black/50 px-2 py-1 backdrop-blur-sm">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleZoomOut}
              className="h-6 w-6 text-white/60 hover:text-white"
            >
              <ZoomOut className="h-3 w-3" />
            </Button>
            <button
              type="button"
              onClick={handleZoomFit}
              className="px-1.5 text-[10px] tabular-nums text-white/70 hover:text-white"
            >
              {Math.round(zoom * 100)}%
            </button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleZoomIn}
              className="h-6 w-6 text-white/60 hover:text-white"
            >
              <ZoomIn className="h-3 w-3" />
            </Button>
            <div className="mx-0.5 h-4 w-px bg-white/10" />
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleZoomFit}
              className="h-6 w-6 text-white/60 hover:text-white"
              title="适应窗口"
            >
              <Maximize className="h-3 w-3" />
            </Button>
          </div>
        </div>

        {/* Toolbar + prompt */}
        <div className="flex items-end gap-3">
          {/* Drawing tools */}
          <div className="flex items-center gap-2 rounded-xl bg-white/5 px-3 py-2 backdrop-blur-sm">
            {/* Tool selector */}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('brush')}
              className={`rounded-lg ${tool === 'brush' ? 'bg-primary/20 text-primary' : 'text-white/50 hover:text-white/80'}`}
              title="画笔"
            >
              <Paintbrush className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('rect')}
              className={`rounded-lg ${tool === 'rect' ? 'bg-primary/20 text-primary' : 'text-white/50 hover:text-white/80'}`}
              title="矩形"
            >
              <Square className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('circle')}
              className={`rounded-lg ${tool === 'circle' ? 'bg-primary/20 text-primary' : 'text-white/50 hover:text-white/80'}`}
              title="圆形"
            >
              <Circle className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('eraser')}
              className={`rounded-lg ${tool === 'eraser' ? 'bg-primary/20 text-primary' : 'text-white/50 hover:text-white/80'}`}
              title="橡皮擦"
            >
              <Eraser className="h-3.5 w-3.5" />
            </Button>

            <div className="mx-1 h-5 w-px bg-white/10" />

            {/* Brush size */}
            {(tool === 'brush' || tool === 'eraser') && (
              <>
                <Slider
                  value={[brushSize]}
                  onValueChange={(v) => setBrushSize(Array.isArray(v) ? v[0] : v)}
                  min={5}
                  max={100}
                  step={1}
                  className="w-16"
                />
                <span className="w-5 text-center text-[10px] tabular-nums text-white/50">{brushSize}</span>
              </>
            )}

            {/* Feather (soft edge) — only for brush */}
            {tool === 'brush' && (
              <>
                <div className="mx-0.5 h-5 w-px bg-white/10" />
                <Slider
                  value={[feather]}
                  onValueChange={(v) => setFeather(Array.isArray(v) ? v[0] : v)}
                  min={0}
                  max={50}
                  step={1}
                  className="w-12"
                  title="羽化"
                />
                <span className="w-4 text-center text-[10px] tabular-nums text-white/50" title="羽化">{feather}</span>
              </>
            )}

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
