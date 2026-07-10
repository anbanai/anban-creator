import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
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

  it('keeps non-compact removal visible and touch-sized at wide coarse or no-hover viewports', () => {
    const first = image('first.png')

    render(
      <DesignerReferenceDock
        files={[first]}
        maxFiles={4}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: '移除参考图：first.png' })).toHaveClass(
      'h-5',
      'w-5',
      'opacity-0',
      'group-hover:opacity-100',
      'group-focus-within:opacity-100',
      'focus-visible:opacity-100',
      'md:[@media(hover:none)]:h-7',
      'md:[@media(hover:none)]:w-7',
      'md:[@media(hover:none)]:opacity-100',
      'md:[@media(pointer:coarse)]:h-7',
      'md:[@media(pointer:coarse)]:w-7',
      'md:[@media(pointer:coarse)]:opacity-100',
    )
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

  it('renders the empty compact state as a horizontal add-tile rail', () => {
    render(
      <DesignerReferenceDock
        files={[]}
        maxFiles={4}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
        compact
      />,
    )

    const dock = screen.getByTestId('designer-reference-dock')
    const addButton = screen.getByRole('button', { name: '添加参考图' })

    expect(dock).toHaveAttribute('data-compact', 'true')
    expect(addButton.parentElement).toHaveClass('flex', 'gap-2', 'overflow-x-auto')
    expect(addButton).toHaveClass('aspect-square', 'min-w-14')
    expect(addButton).not.toHaveClass('w-full', 'py-5')
    expect(screen.getByText('添加')).toBeInTheDocument()
    expect(screen.queryByText('拖入参考图')).not.toBeInTheDocument()
  })

  it('keeps compact removal visible with a 28px touch target', () => {
    const first = image('first.png')

    render(
      <DesignerReferenceDock
        files={[first]}
        maxFiles={4}
        onFilesAdded={vi.fn()}
        onFileRemove={vi.fn()}
        compact
      />,
    )

    expect(screen.getByRole('button', { name: '移除参考图：first.png' })).toHaveClass(
      'h-7',
      'w-7',
      'opacity-100',
    )
  })

  it('preserves retained preview URLs when files are appended', async () => {
    const first = image('first.png')
    const second = image('second.png', 200)
    const props = {
      maxFiles: 3,
      onFilesAdded: vi.fn(),
      onFileRemove: vi.fn(),
    }
    const { rerender } = render(
      <DesignerReferenceDock files={[first]} {...props} />,
    )

    await screen.findByRole('img', { name: 'first.png' })
    expect(createObjectURL).toHaveBeenCalledTimes(1)

    rerender(<DesignerReferenceDock files={[first, second]} {...props} />)

    await screen.findByRole('img', { name: 'second.png' })
    expect(createObjectURL).toHaveBeenCalledTimes(2)
    expect(createObjectURL).toHaveBeenNthCalledWith(1, first)
    expect(createObjectURL).toHaveBeenNthCalledWith(2, second)
    expect(revokeObjectURL).not.toHaveBeenCalledWith('blob:first.png')
  })

  it('revokes preview URLs when files are replaced and on unmount', async () => {
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

  it('exposes drop activity and applies primary active styling', () => {
    const props = {
      files: [],
      maxFiles: 4,
      onFilesAdded: vi.fn(),
      onFileRemove: vi.fn(),
    }
    const { rerender } = render(
      <DesignerReferenceDock {...props} />,
    )
    const dock = screen.getByTestId('designer-reference-dock')

    expect(dock).toHaveAttribute('data-drop-active', 'false')
    expect(dock).not.toHaveClass('bg-primary/10', 'ring-primary/40')

    rerender(<DesignerReferenceDock {...props} dropActive />)

    expect(dock).toHaveAttribute('data-drop-active', 'true')
    expect(dock).toHaveClass(
      'bg-primary/10',
      'ring-1',
      'ring-primary/40',
      'shadow-[0_0_24px_-10px_var(--color-primary)]',
    )
  })

  it('composes the controlled reference dock instead of the legacy toolbar uploader', () => {
    const toolbarSource = readFileSync(resolve(import.meta.dirname, 'DesignerToolbar.tsx'), 'utf8')

    expect(toolbarSource).toContain('<DesignerReferenceDock')
    expect(toolbarSource).not.toContain('上传参考图')
    expect(toolbarSource).not.toContain('handleRefFiles')
    expect(toolbarSource).not.toContain('refInputRef')
  })
})
