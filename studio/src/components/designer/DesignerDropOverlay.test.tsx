import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import DesignerDropOverlay from './DesignerDropOverlay'

describe('DesignerDropOverlay', () => {
  it('renders nothing while inactive', () => {
    render(
      <DesignerDropOverlay
        active={false}
        remainingCapacity={16}
      />,
    )

    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('shows the incoming reference count and remaining capacity', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={3}
        remainingCapacity={13}
      />,
    )

    expect(screen.getByText('释放以添加 3 张参考图')).toBeInTheDocument()
    expect(screen.getByText('当前模型还可添加 13 张')).toBeInTheDocument()
  })

  it('shows generic action copy when the incoming count is unknown', () => {
    render(
      <DesignerDropOverlay
        active
        remainingCapacity={4}
      />,
    )

    expect(screen.getByText('释放以添加参考图')).toBeInTheDocument()
  })

  it('announces the complete feedback in a polite atomic live region', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={3}
        remainingCapacity={13}
      />,
    )

    const status = screen.getByRole('status')

    expect(status).toHaveAttribute('aria-live', 'polite')
    expect(status).toHaveAttribute('aria-atomic', 'true')
    expect(status).toHaveTextContent('释放以添加 3 张参考图。当前模型还可添加 13 张')
  })

  it('communicates a reached limit and normalizes nonpositive capacity', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={2}
        remainingCapacity={-4}
      />,
    )

    expect(screen.getByText('参考图已达上限，无法继续添加')).toBeInTheDocument()
    expect(screen.getByText('当前模型还可添加 0 张')).toBeInTheDocument()
  })

  it('communicates when no addable images are detected', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={0}
        remainingCapacity={4}
      />,
    )

    expect(screen.getByText('未检测到可添加的图片')).toBeInTheDocument()
  })

  it('communicates the remaining capacity without promising source positions', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={7}
        remainingCapacity={4}
      />,
    )

    expect(screen.getByText('检测到 7 张参考图，最多可添加 4 张')).toBeInTheDocument()
    expect(screen.getByText('当前模型还可添加 4 张')).toBeInTheDocument()
  })

  it('normalizes fractional display counts to nonnegative integers', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={3.9}
        remainingCapacity={13.8}
      />,
    )

    expect(screen.getByText('释放以添加 3 张参考图')).toBeInTheDocument()
    expect(screen.getByText('当前模型还可添加 13 张')).toBeInTheDocument()
  })

  it('preserves pointer transparency, positioning, reduced motion, and message contrast', () => {
    render(
      <DesignerDropOverlay
        active
        remainingCapacity={4}
      />,
    )

    const overlay = screen.getByTestId('designer-drop-overlay')
    const visualLayer = overlay.firstElementChild
    const messagePanel = visualLayer?.firstElementChild

    expect(overlay).toHaveAttribute('aria-hidden', 'true')
    expect(overlay).toHaveClass('pointer-events-none', 'absolute', 'inset-0')
    expect(visualLayer).toHaveClass('motion-reduce:animate-none')
    expect(messagePanel).toHaveClass('bg-card', 'text-card-foreground')
  })
})
