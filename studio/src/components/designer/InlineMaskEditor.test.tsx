import { createRef } from 'react'
import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import InlineMaskEditor, { type InlineMaskEditorHandle } from './InlineMaskEditor'

function createCanvasContextMock() {
  return {
    beginPath: vi.fn(),
    clearRect: vi.fn(),
    arc: vi.fn(),
    ellipse: vi.fn(),
    fill: vi.fn(),
    fillRect: vi.fn(),
    lineTo: vi.fn(),
    moveTo: vi.fn(),
    stroke: vi.fn(),
  }
}

function setElementRect(element: Element, rect: Partial<DOMRect>) {
  element.getBoundingClientRect = vi.fn(() => ({
    bottom: 500,
    height: 500,
    left: 0,
    right: 500,
    top: 0,
    width: 500,
    x: 0,
    y: 0,
    toJSON: () => ({}),
    ...rect,
  } as DOMRect))
}

afterEach(() => vi.restoreAllMocks())

describe('InlineMaskEditor', () => {

  it('uses image-sized canvas coordinates for the first brush drag', async () => {
    const context = createCanvasContextMock()
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context as unknown as CanvasRenderingContext2D)

    const { container } = render(
      <InlineMaskEditor
        imageUrl="data:image/png;base64,iVBORw0KGgo="
        onClose={vi.fn()}
      />,
    )

    const image = screen.getByAltText('编辑图片') as HTMLImageElement
    Object.defineProperty(image, 'naturalWidth', { configurable: true, value: 1000 })
    Object.defineProperty(image, 'naturalHeight', { configurable: true, value: 1000 })

    fireEvent.load(image)

    await waitFor(() => {
      expect(container.querySelector('canvas')).toBeInTheDocument()
    })

    const canvas = container.querySelector('canvas') as HTMLCanvasElement
    setElementRect(canvas, { height: 500, width: 500 })

    fireEvent.mouseDown(canvas, { clientX: 50, clientY: 50 })
    fireEvent.mouseMove(canvas, { clientX: 60, clientY: 60 })
    fireEvent.mouseUp(canvas)

    expect(context.moveTo).toHaveBeenCalledWith(100, 100)
    expect(context.lineTo).toHaveBeenCalledWith(120, 120)
  })

  it('renders brush drags as continuous line segments', async () => {
    const context = createCanvasContextMock()
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context as unknown as CanvasRenderingContext2D)

    const { container } = render(
      <InlineMaskEditor
        imageUrl="data:image/png;base64,iVBORw0KGgo="
        onClose={vi.fn()}
      />,
    )

    const image = screen.getByAltText('编辑图片') as HTMLImageElement
    Object.defineProperty(image, 'naturalWidth', { configurable: true, value: 500 })
    Object.defineProperty(image, 'naturalHeight', { configurable: true, value: 500 })

    fireEvent.load(image)

    await waitFor(() => {
      expect(container.querySelector('canvas')).toBeInTheDocument()
    })

    const canvas = container.querySelector('canvas') as HTMLCanvasElement
    setElementRect(canvas, { height: 500, width: 500 })

    fireEvent.mouseDown(canvas, { clientX: 50, clientY: 50 })
    fireEvent.mouseMove(canvas, { clientX: 80, clientY: 80 })
    fireEvent.mouseUp(canvas)

    expect(context.moveTo).toHaveBeenCalledWith(50, 50)
    expect(context.lineTo).toHaveBeenCalledWith(80, 80)
    expect(context.stroke).toHaveBeenCalled()
  })
})

// Test-only pixel raster for the canvas operations used by these strokes. It
// applies Canvas compositing to RGBA values so assertions check the mask pixels.
function createPixelRaster(width: number, height: number) {
  const pixels = new Uint8ClampedArray(width * height * 4)
  let circle = { x: 0, y: 0, radius: 0 }
  const context = {
    globalCompositeOperation: 'source-over',
    fillStyle: '#000000',
    strokeStyle: '#000000',
    beginPath() {},
    arc(x: number, y: number, radius: number) { circle = { x, y, radius } },
    fill() {
      paint((x, y) => (x + 0.5 - circle.x) ** 2 + (y + 0.5 - circle.y) ** 2 <= circle.radius ** 2)
    },
    fillRect(left: number, top: number, w: number, h: number) {
      paint((x, y) => x >= left && x < left + w && y >= top && y < top + h)
    },
    getImageData(x: number, y: number) {
      return { data: pixels.slice((y * width + x) * 4, (y * width + x) * 4 + 4) }
    },
  }
  function paint(contains: (x: number, y: number) => boolean) {
    for (let y = 0; y < height; y++) {
      for (let x = 0; x < width; x++) {
        if (!contains(x, y)) continue
        const index = (y * width + x) * 4
        if (context.globalCompositeOperation === 'destination-out') {
          pixels.fill(0, index, index + 4)
        } else {
          pixels.fill(context.fillStyle === '#ffffff' ? 255 : 0, index, index + 3)
          pixels[index + 3] = 255
        }
      }
    }
  }
  return context
}

async function loadEditorImage(container: HTMLElement) {
  const image = screen.getByAltText('编辑图片')
  Object.defineProperties(image, {
    naturalWidth: { configurable: true, value: 100 },
    naturalHeight: { configurable: true, value: 100 },
  })
  await act(async () => {
    fireEvent.load(image)
    await new Promise(requestAnimationFrame)
  })
  await waitFor(() => expect(container.querySelector('canvas')).toBeInTheDocument())
  const canvas = container.querySelector('canvas')!
  setElementRect(canvas, { width: 100, height: 100 })
  return canvas
}

it('exports erased pixels as opaque white while preserving stroke order and selected alpha', async () => {
  const rasters = new WeakMap<HTMLCanvasElement, ReturnType<typeof createPixelRaster>>()
  let exportedCanvas!: HTMLCanvasElement
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(function (this: HTMLCanvasElement) {
    if (this.isConnected) return createCanvasContextMock() as unknown as CanvasRenderingContext2D
    if (!rasters.has(this)) rasters.set(this, createPixelRaster(this.width, this.height))
    return rasters.get(this) as unknown as CanvasRenderingContext2D
  })
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (this: HTMLCanvasElement, callback) {
    exportedCanvas = this
    callback(new Blob(['encoded-mask'], { type: 'image/png' }))
  })
  const ref = createRef<InlineMaskEditorHandle>()
  const { container } = render(<InlineMaskEditor ref={ref} imageUrl="data:image/png;base64,aW1hZ2U=" onClose={vi.fn()} />)
  const canvas = await loadEditorImage(container)
  const clickCanvas = (x: number, y: number) => {
    fireEvent.mouseDown(canvas, { clientX: x, clientY: y })
    fireEvent.mouseUp(canvas)
  }

  fireEvent.click(screen.getByTitle('矩形'))
  fireEvent.mouseDown(canvas, { clientX: 20, clientY: 20 })
  fireEvent.mouseMove(canvas, { clientX: 80, clientY: 80 })
  fireEvent.mouseUp(canvas)
  fireEvent.click(screen.getByTitle('橡皮擦'))
  clickCanvas(50, 50)
  clickCanvas(5, 5)
  let exported: File | null = null
  await act(async () => { exported = await ref.current!.exportMask() })
  expect((exported as File | null)?.type).toBe('image/png')
  let raster = rasters.get(exportedCanvas)!
  expect([...raster.getImageData(50, 50).data]).toEqual([255, 255, 255, 255])
  expect([...raster.getImageData(5, 5).data]).toEqual([255, 255, 255, 255])
  expect(raster.getImageData(25, 25).data[3]).toBe(0)

  fireEvent.click(screen.getByTitle('画笔'))
  clickCanvas(50, 50)
  await act(async () => { await ref.current!.exportMask() })
  raster = rasters.get(exportedCanvas)!
  expect(raster.getImageData(50, 50).data[3]).toBe(0)
  expect(raster.getImageData(5, 5).data[3]).toBe(255)
})

it.each(['input', 'textarea', 'contenteditable'])('allows prompt spaces in %s without entering canvas pan mode', async (target) => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(createCanvasContextMock() as unknown as CanvasRenderingContext2D)
  const { container } = render(<>
    <input aria-label="input prompt" />
    <textarea aria-label="textarea prompt" />
    <div contentEditable suppressContentEditableWarning aria-label="contenteditable prompt"><span>nested text</span></div>
    <InlineMaskEditor imageUrl="data:image/png;base64,aW1hZ2U=" onClose={vi.fn()} />
  </>)
  const canvas = await loadEditorImage(container)
  const prompt = target === 'contenteditable' ? screen.getByText('nested text') : screen.getByLabelText(`${target} prompt`)
  const event = new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true })
  fireEvent(prompt, event)
  expect(event.defaultPrevented).toBe(false)
  expect(canvas).toHaveClass('cursor-crosshair')
})

it('keeps space-to-pan on the canvas and releases it on keyup', async () => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(createCanvasContextMock() as unknown as CanvasRenderingContext2D)
  const { container } = render(<InlineMaskEditor imageUrl="data:image/png;base64,aW1hZ2U=" onClose={vi.fn()} />)
  const canvas = await loadEditorImage(container)
  const event = new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true })
  fireEvent(canvas, event)
  expect(event.defaultPrevented).toBe(true)
  expect(canvas).toHaveClass('cursor-grab')
  fireEvent.mouseDown(canvas, { clientX: 20, clientY: 20 })
  fireEvent.mouseMove(canvas, { clientX: 40, clientY: 30 })
  fireEvent.mouseUp(canvas)
  expect(canvas.parentElement!.style.transform).toBe('translate(20px, 10px) scale(1)')
  fireEvent.keyUp(canvas, { key: ' ' })
  expect(canvas).toHaveClass('cursor-crosshair')
})
