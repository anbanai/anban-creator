import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import InlineMaskEditor from './InlineMaskEditor'

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

describe('InlineMaskEditor', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

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
