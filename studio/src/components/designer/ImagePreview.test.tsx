import { act, fireEvent, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import ImagePreview, { type PreviewMetadata } from './ImagePreview'

Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
  configurable: true,
  value: vi.fn(() => []),
})

vi.mock('@/lib/tauri', () => ({
  downloadBlob: vi.fn(),
  isDesktop: vi.fn(() => false),
  openInExternalWindow: vi.fn(),
  saveUrlToFile: vi.fn(() => Promise.resolve(false)),
}))

const metadata: PreviewMetadata = {
  provider: 'openai',
  model: 'gpt-image-1',
  prompt: 'A precise line drawing',
  outputFormat: 'png',
  createdAt: '2026-07-16T00:00:00Z',
}

async function settleBaseUi() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

describe('ImagePreview', () => {
  beforeEach(() => vi.clearAllMocks())

  it('reuses shared image zoom controls while preserving navigation and metadata', async () => {
    render(
      <ImagePreview
        images={[
          { url: 'blob:first', index: 0, width: 1024, height: 1024 },
          { url: 'blob:second', index: 1, width: 1536, height: 1024 },
        ]}
        metadata={metadata}
        onClose={vi.fn()}
      />,
    )
    await settleBaseUi()

    expect(screen.getByRole('button', { name: '放大' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '放大' }))
    expect(screen.getByText('125%')).toBeInTheDocument()
    expect(screen.getByText('openai / gpt-image-1')).toBeInTheDocument()
    expect(screen.getByText('1024×1024')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '下一项' }))
    expect(screen.getByRole('img', { name: '预览图片' })).toHaveAttribute('src', 'blob:second')
    expect(screen.getByText('1536×1024')).toBeInTheDocument()
    expect(screen.getByText('100%')).toBeInTheDocument()
    await settleBaseUi()
  })

  it('preserves edit, download, and Escape close actions', async () => {
    const onEdit = vi.fn()
    const onClose = vi.fn()
    render(
      <ImagePreview
        images={[{ url: 'blob:first', index: 0 }]}
        metadata={metadata}
        canInpaint
        onEdit={onEdit}
        onClose={onClose}
      />,
    )
    await settleBaseUi()

    fireEvent.click(screen.getByRole('button', { name: '局部重绘' }))
    expect(onEdit).toHaveBeenCalledWith({ url: 'blob:first', index: 0 }, 0)
    expect(screen.getByRole('button', { name: '下载' })).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledOnce()
    await settleBaseUi()
  })

  it('does not navigate images while the zoom slider handles arrow keys', async () => {
    render(
      <ImagePreview
        images={[
          { url: 'blob:first', index: 0 },
          { url: 'blob:second', index: 1 },
        ]}
        metadata={metadata}
        onClose={vi.fn()}
      />,
    )
    const slider = screen.getByRole('group', { name: '缩放比例' })
    const thumb = slider.querySelector('[data-slot="slider-thumb"]') as HTMLElement

    fireEvent.keyDown(thumb, { key: 'ArrowRight' })

    expect(screen.getByRole('img', { name: '预览图片' })).toHaveAttribute('src', 'blob:first')
    expect(screen.getByText('125%')).toBeInTheDocument()
    await settleBaseUi()
  })
})
