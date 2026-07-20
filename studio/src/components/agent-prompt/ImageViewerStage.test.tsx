import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import { ImageViewerStage } from './ImageViewerStage'

function renderStage(overrides: Partial<React.ComponentProps<typeof ImageViewerStage>> = {}) {
  return render(
    <ImageViewerStage
      src="blob:first"
      alt="first.png"
      index={0}
      count={2}
      onPrevious={vi.fn()}
      onNext={vi.fn()}
      {...overrides}
    />,
  )
}

async function settleBaseUi() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

describe('ImageViewerStage', () => {
  it('clamps discrete zoom controls between 25 and 400 percent', async () => {
    renderStage()
    await settleBaseUi()

    const image = screen.getByRole('img', { name: 'first.png' })
    expect(image).toHaveStyle({ transform: 'scale(1)' })

    for (let index = 0; index < 20; index += 1) {
      fireEvent.click(screen.getByRole('button', { name: '放大' }))
    }
    expect(screen.getByText('400%')).toBeInTheDocument()
    expect(image).toHaveStyle({ transform: 'scale(4)' })
    expect(screen.getByRole('button', { name: '放大' })).toBeDisabled()
    await settleBaseUi()

    for (let index = 0; index < 20; index += 1) {
      fireEvent.click(screen.getByRole('button', { name: '缩小' }))
    }
    expect(screen.getByText('25%')).toBeInTheDocument()
    expect(image).toHaveStyle({ transform: 'scale(0.25)' })
    expect(screen.getByRole('button', { name: '缩小' })).toBeDisabled()
    await settleBaseUi()
  })

  it('supports the slider and fit reset, then resets when the source changes', async () => {
    const { rerender } = renderStage()
    const slider = screen.getByRole('group', { name: '缩放比例' })

    await waitFor(() => expect(slider.querySelector('[data-slot="slider-thumb"]')).toBeInTheDocument())
    expect(slider.querySelectorAll('[data-slot="slider-thumb"]')).toHaveLength(1)
    expect(slider.querySelectorAll('input')).toHaveLength(1)
    const thumb = slider.querySelector('[data-slot="slider-thumb"]') as HTMLElement
    for (let index = 0; index < 6; index += 1) fireEvent.keyDown(thumb, { key: 'ArrowUp' })
    expect(screen.getByText('250%')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '适合窗口' }))
    expect(screen.getByText('100%')).toBeInTheDocument()
    await settleBaseUi()

    for (let index = 0; index < 3; index += 1) fireEvent.keyDown(thumb, { key: 'ArrowUp' })
    rerender(
      <ImageViewerStage
        src="blob:second"
        alt="second.png"
        index={1}
        count={2}
        onPrevious={vi.fn()}
        onNext={vi.fn()}
      />,
    )
    expect(screen.getByText('100%')).toBeInTheDocument()
    await settleBaseUi()
  })

  it('exposes bounded previous and next navigation', async () => {
    const onPrevious = vi.fn()
    const onNext = vi.fn()
    const { rerender } = renderStage({ onPrevious, onNext })

    expect(screen.getByRole('button', { name: '上一项' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '下一项' }))
    expect(onNext).toHaveBeenCalledOnce()

    rerender(
      <ImageViewerStage
        src="blob:second"
        alt="second.png"
        index={1}
        count={2}
        onPrevious={onPrevious}
        onNext={onNext}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: '上一项' }))
    expect(onPrevious).toHaveBeenCalledOnce()
    expect(screen.getByRole('button', { name: '下一项' })).toBeDisabled()
    await settleBaseUi()
  })

  it('keeps lightbox navigation controls visible against the dark preview surface', () => {
    renderStage({ lightbox: true })

    expect(screen.getByRole('button', { name: '上一项' })).toHaveClass('bg-white', 'text-black')
    expect(screen.getByRole('button', { name: '下一项' })).toHaveClass('bg-white', 'text-black')
  })
})
