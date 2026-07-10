import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerReferenceDock from './DesignerReferenceDock'

function image(name: string, lastModified = 100) {
  return new File(['image'], name, { type: 'image/png', lastModified })
}

describe('DesignerReferenceDock', () => {
  const createObjectURL = vi.fn((file: File) => `blob:${file.name}`)
  const revokeObjectURL = vi.fn()

  beforeEach(() => {
    const NativeURL = URL
    vi.stubGlobal('URL', class extends NativeURL {
      static createObjectURL = createObjectURL
      static revokeObjectURL = revokeObjectURL
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders a full-surface empty picker with provider capacity', () => {
    render(
      <DesignerReferenceDock
        files={[]}
        maxFiles={16}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: '添加参考图' })).toBeInTheDocument()
    expect(screen.getByText('拖入参考图')).toBeInTheDocument()
    expect(screen.getByText('或点按浏览 · 最多 16 张')).toBeInTheDocument()
  })

  it('forwards every picker selection and resets the input', () => {
    const onFilesAdded = vi.fn()
    const first = image('first.png')
    const second = image('second.png', 200)

    render(
      <DesignerReferenceDock
        files={[]}
        maxFiles={16}
        onFilesAdded={onFilesAdded}
        onFileRemove={vi.fn()}
      />,
    )

    const input = screen.getByTestId('designer-reference-input') as HTMLInputElement
    fireEvent.change(input, { target: { files: [first, second] } })

    expect(onFilesAdded).toHaveBeenCalledWith([first, second])
    expect(input.value).toBe('')
  })

  it('renders thumbnails, accessible removal, and the add-more tile', async () => {
    const onFileRemove = vi.fn()
    const first = image('first.png')
    const second = image('second.png', 200)

    render(
      <DesignerReferenceDock
        files={[first, second]}
        maxFiles={3}
        onFilesAdded={vi.fn()}
        onFileRemove={onFileRemove}
      />,
    )

    expect(await screen.findByRole('img', { name: 'first.png' })).toHaveAttribute('src', 'blob:first.png')
    expect(screen.getByRole('img', { name: 'second.png' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '添加更多参考图' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '移除参考图：second.png' }))
    expect(onFileRemove).toHaveBeenCalledWith(1)
  })

  it('renders a clear full-capacity state without an add control', async () => {
    const first = image('first.png')

    render(
      <DesignerReferenceDock
        files={[first]}
        maxFiles={1}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
      />,
    )

    expect(await screen.findByRole('img', { name: 'first.png' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '添加更多参考图' })).not.toBeInTheDocument()
    expect(screen.getByText('已达上限')).toBeInTheDocument()
  })

  it('uses a horizontal material rail in compact mode', () => {
    render(
      <DesignerReferenceDock
        files={[]}
        maxFiles={4}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
        compact
      />,
    )

    expect(screen.getByTestId('designer-reference-dock')).toHaveAttribute('data-compact', 'true')
  })

  it('revokes preview URLs when files change and on unmount', async () => {
    const first = image('first.png')
    const second = image('second.png', 200)
    const props = {
      maxFiles: 3,
      onFilesAdded: vi.fn(),
      onFileRemove: vi.fn(),
    }
    const { rerender, unmount } = render(
      <DesignerReferenceDock files={[first]} {...props} />,
    )

    await screen.findByRole('img', { name: 'first.png' })
    rerender(<DesignerReferenceDock files={[second]} {...props} />)

    await waitFor(() => expect(revokeObjectURL).toHaveBeenCalledWith('blob:first.png'))
    unmount()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:second.png')
  })
})
