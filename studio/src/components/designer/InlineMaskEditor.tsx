import { useRef, useState, useCallback, useEffect, forwardRef, useImperativeHandle } from 'react'
import { ArrowLeft, Eraser, Paintbrush, RotateCcw, Square, Circle, ZoomIn, ZoomOut, Maximize, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Slider } from '@/components/ui/slider'
import http from '@/lib/http-client'

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

// Draw a brush stroke as a continuous line connecting all sampled points.
// Without this, fast pointer movement produces sparse pointermove events
// and the stroke looks like disconnected dots.
function paintBrushStroke(ctx: CanvasRenderingContext2D, points: BrushPoint[]) {
  if (points.length === 0) return
  ctx.lineCap = 'round'
  ctx.lineJoin = 'round'

  if (points.length === 1) {
    const p = points[0]
    ctx.beginPath()
    ctx.arc(p.x, p.y, p.size / 2, 0, Math.PI * 2)
    ctx.fill()
    return
  }

  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1]
    const b = points[i]
    ctx.beginPath()
    ctx.lineWidth = (a.size + b.size) / 2
    ctx.moveTo(a.x, a.y)
    ctx.lineTo(b.x, b.y)
    ctx.stroke()
  }

  // Cap both ends with circles so the stroke keeps its diameter at the endpoints.
  const first = points[0]
  const last = points[points.length - 1]
  ctx.beginPath()
  ctx.arc(first.x, first.y, first.size / 2, 0, Math.PI * 2)
  ctx.fill()
  ctx.beginPath()
  ctx.arc(last.x, last.y, last.size / 2, 0, Math.PI * 2)
  ctx.fill()
}

export interface InlineMaskEditorHandle {
  exportMask: () => Promise<File | null>
}

interface InlineMaskEditorProps {
  imageUrl: string
  onClose: () => void
  onMaskChange?: (hasStrokes: boolean) => void
}

const ZOOM_LEVELS = [0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4]

const InlineMaskEditor = forwardRef<InlineMaskEditorHandle, InlineMaskEditorProps>(
  function InlineMaskEditor({ imageUrl, onClose, onMaskChange }, ref) {
    const canvasRef = useRef<HTMLCanvasElement>(null)
    const imgRef = useRef<HTMLImageElement>(null)
    const [tool, setTool] = useState<ToolType>('brush')
    const [brushSize, setBrushSize] = useState(30)
    const [feather, setFeather] = useState(0)
    const [imageLoaded, setImageLoaded] = useState(false)
    const [imgSrc, setImgSrc] = useState<string>('')
    const [imgError, setImgError] = useState(false)
    const blobUrlRef = useRef<string>('')
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
    const featherRef = useRef(0)

    featherRef.current = feather

    // Load image via authenticated backend proxy when it's a remote OSS URL,
    // to avoid CORS on <img crossOrigin="anonymous"> + canvas export. For
    // same-origin / blob URLs, use them directly.
    useEffect(() => {
      let cancelled = false
      setImgError(false)
      setImageLoaded(false)

      if (!imageUrl) {
        setImgSrc('')
        return
      }

      if (imageUrl.startsWith('/files/') ||
          imageUrl.startsWith('/api/v1/files/') ||
          imageUrl.startsWith('blob:') ||
          imageUrl.startsWith('data:')) {
        setImgSrc(imageUrl)
        return
      }

      let normalized = imageUrl
      try {
        const u = new URL(imageUrl)
        if (u.hostname.endsWith('.aliyuncs.com')) {
          normalized = '/files/' + decodeURIComponent(u.pathname).slice(1)
        }
      } catch {
        // not a parseable URL — fall through and try as-is
      }

      http.get(normalized, { responseType: 'blob' })
        .then((res) => {
          if (cancelled) return
          if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
          const blobUrl = URL.createObjectURL(res.data)
          blobUrlRef.current = blobUrl
          setImgSrc(blobUrl)
        })
        .catch(() => {
          if (cancelled) return
          setImgError(true)
          setImgSrc('')
        })

      return () => {
        cancelled = true
      }
    }, [imageUrl])

    useEffect(() => {
      return () => {
        if (blobUrlRef.current) {
          URL.revokeObjectURL(blobUrlRef.current)
          blobUrlRef.current = ''
        }
      }
    }, [])

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
      if (currentStrokeRef.current.length > 0) {
        allStrokes.push({ type: 'brush', points: currentStrokeRef.current })
      }
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
          const points = stroke.points
          if (points.length === 0) continue

          if (featherRef.current > 0 && stroke.type === 'brush') {
            ctx.shadowBlur = featherRef.current
            ctx.shadowColor = 'rgba(255, 100, 50, 0.3)'
          } else {
            ctx.shadowBlur = 0
          }

          ctx.strokeStyle = 'rgba(255, 100, 50, 0.45)'
          ctx.fillStyle = 'rgba(255, 100, 50, 0.45)'
          paintBrushStroke(ctx, points)
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
    }, [imageLoaded, tool])

    function getCanvasPoint(e: React.MouseEvent | React.TouchEvent) {
      const canvas = canvasRef.current
      if (!canvas) return null

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

      const has = strokesRef.current.length > 0
      setHasStrokes(has)
      onMaskChange?.(has)
    }

    function handleUndo() {
      strokesRef.current.pop()
      const has = strokesRef.current.length > 0
      setHasStrokes(has)
      onMaskChange?.(has)
      redrawCanvas()
    }

    function handleClear() {
      strokesRef.current = []
      currentStrokeRef.current = []
      currentShapeRef.current = null
      setHasStrokes(false)
      onMaskChange?.(false)
      redrawCanvas()
    }

    function handleZoomIn() {
      const idx = ZOOM_LEVELS.findIndex((z: number) => z > zoom)
      if (idx >= 0) setZoom(ZOOM_LEVELS[idx])
    }

    function handleZoomOut() {
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

    function exportMask(): Promise<File | null> {
      if (strokesRef.current.length === 0) return Promise.resolve(null)

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

          if (featherRef.current > 0 && stroke.type === 'brush') {
            ctx.shadowBlur = featherRef.current
            ctx.shadowColor = 'rgba(0, 0, 0, 1)'
          }

          ctx.strokeStyle = '#000000'
          ctx.fillStyle = '#000000'
          paintBrushStroke(ctx, points)
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

    useImperativeHandle(ref, () => ({ exportMask }), [])

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
      <div className="relative flex h-full flex-col">
        {/* Top toolbar */}
        <div className="absolute top-3 left-3 right-3 z-10 flex items-center justify-between">
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            className="gap-1.5 rounded-lg bg-card/90 text-foreground shadow-sm hover:bg-card hover:text-foreground"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            返回
          </Button>

          <div className="flex items-center gap-1.5 rounded-xl border border-border/70 bg-card/90 px-2 py-1.5 shadow-sm">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('brush')}
              className={`rounded-lg ${tool === 'brush' ? 'bg-primary/20 text-primary' : 'text-foreground/70 hover:text-foreground'}`}
              title="画笔"
            >
              <Paintbrush className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('rect')}
              className={`rounded-lg ${tool === 'rect' ? 'bg-primary/20 text-primary' : 'text-foreground/70 hover:text-foreground'}`}
              title="矩形"
            >
              <Square className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('circle')}
              className={`rounded-lg ${tool === 'circle' ? 'bg-primary/20 text-primary' : 'text-foreground/70 hover:text-foreground'}`}
              title="圆形"
            >
              <Circle className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setTool('eraser')}
              className={`rounded-lg ${tool === 'eraser' ? 'bg-primary/20 text-primary' : 'text-foreground/70 hover:text-foreground'}`}
              title="橡皮擦"
            >
              <Eraser className="h-3.5 w-3.5" />
            </Button>

            <div className="mx-0.5 h-5 w-px bg-border/50" />

            {(tool === 'brush' || tool === 'eraser') && (
              <>
                <Slider
                  value={[brushSize]}
                  onValueChange={(v) => setBrushSize(Array.isArray(v) ? v[0] : v)}
                  min={5}
                  max={100}
                  step={1}
                  className="w-20"
                />
                <span className="w-5 text-center text-[10px] tabular-nums text-foreground/75">{brushSize}</span>
              </>
            )}

            {tool === 'brush' && (
              <>
                <div className="mx-0.5 h-5 w-px bg-border/50" />
                <Slider
                  value={[feather]}
                  onValueChange={(v) => setFeather(Array.isArray(v) ? v[0] : v)}
                  min={0}
                  max={50}
                  step={1}
                  className="w-14"
                  title="羽化"
                />
                <span className="w-4 text-center text-[10px] tabular-nums text-foreground/75" title="羽化">{feather}</span>
              </>
            )}

            <div className="mx-0.5 h-5 w-px bg-border/50" />
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleUndo}
              disabled={!hasStrokes}
              className="rounded-lg text-foreground/70 hover:text-foreground"
              title="撤销"
            >
              <RotateCcw className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleClear}
              disabled={!hasStrokes}
              className="text-[11px] text-foreground/70 hover:text-foreground"
            >
              清除
            </Button>
          </div>

          {/* Hint */}
          <span className="hidden text-[11px] text-muted-foreground md:block">
            涂抹需要修改的区域，在下方描述变化
          </span>
        </div>

        {/* Image + Canvas */}
        <div className="flex flex-1 items-center justify-center overflow-hidden p-4 pt-14">
          <div
            className="relative"
            style={{
              transform: `translate(${panOffset.x}px, ${panOffset.y}px) scale(${zoom})`,
              transformOrigin: 'center center',
              transition: isPanning ? 'none' : 'transform 0.15s ease-out',
            }}
          >
            {imgError ? (
              <div className="flex max-h-[60vh] min-w-[280px] max-w-full flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-destructive/50 p-8 text-center text-sm text-destructive">
                <span>图片加载失败</span>
                <span className="text-xs text-muted-foreground">请关闭后重试</span>
              </div>
            ) : imgSrc ? (
              <>
                <img
                  ref={imgRef}
                  src={imgSrc}
                  alt="编辑图片"
                  className="block max-h-[60vh] max-w-full rounded-xl shadow-lg object-contain"
                  onLoad={() => setImageLoaded(true)}
                  onError={() => setImgError(true)}
                  draggable={false}
                />
                {imageLoaded && (
                  <canvas
                    ref={canvasRef}
                    className={`absolute inset-0 h-full w-full touch-none rounded-xl ${cursorStyle}`}
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
              </>
            ) : (
              <div className="flex h-32 w-64 items-center justify-center gap-2 rounded-xl border border-border/50 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                <span>加载图片中…</span>
              </div>
            )}
          </div>
        </div>

        {/* Zoom controls */}
        <div className="absolute bottom-3 right-3 flex items-center gap-1 rounded-lg border border-border/70 bg-card/90 px-2 py-1 shadow-sm">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={handleZoomOut}
            className="h-6 w-6 text-foreground/70 hover:text-foreground"
          >
            <ZoomOut className="h-3 w-3" />
          </Button>
          <button
            type="button"
            onClick={handleZoomFit}
            className="px-1.5 text-[10px] tabular-nums text-foreground/75 hover:text-foreground"
          >
            {Math.round(zoom * 100)}%
          </button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={handleZoomIn}
            className="h-6 w-6 text-foreground/70 hover:text-foreground"
          >
            <ZoomIn className="h-3 w-3" />
          </Button>
          <div className="mx-0.5 h-4 w-px bg-border/50" />
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={handleZoomFit}
            className="h-6 w-6 text-foreground/70 hover:text-foreground"
            title="适应窗口"
          >
            <Maximize className="h-3 w-3" />
          </Button>
        </div>
      </div>
    )
  },
)

export default InlineMaskEditor
