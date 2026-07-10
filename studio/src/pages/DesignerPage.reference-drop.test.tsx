import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerPage from './DesignerPage'
import { designerApi } from '@/lib/api/designer'
import { render } from '@/test/test-utils'
import type { DesignerProvider } from '@/types/designer'

const toast = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}))

vi.mock('sonner', () => ({ toast }))

vi.mock('@/lib/api/designer', () => ({
  designerApi: {
    getProviders: vi.fn(),
    generate: vi.fn(),
    uploadReference: vi.fn(),
    uploadReferenceFromUrl: vi.fn(),
    getHistory: vi.fn(),
    getGeneration: vi.fn(),
  },
}))

function provider(overrides: Partial<DesignerProvider['capabilities']> = {}): DesignerProvider {
  return {
    id: 'gpt_image_2',
    name: 'GPT Image 2',
    provider: 'openai',
    providerKey: 'wangcai_openai',
    route: 'image_generation.designer.gpt_image_2',
    model: 'gpt-image-2',
    credits: 0,
    enabled: true,
    idx: 0,
    capabilities: {
      qualityLevels: ['auto'],
      sizePresets: ['auto'],
      defaultSize: 'auto',
      maxBatch: 1,
      maxReferenceImages: 2,
      supportsReference: true,
      supportsMask: false,
      outputFormats: ['png'],
      hasBackground: false,
      hasCompression: false,
      watermark: false,
      ...overrides,
    },
    pricing: {},
  }
}

function dragData(files: File[] = [], types: string[] = ['Files']) {
  return {
    types,
    files,
    items: files.map((file) => ({ kind: 'file', type: file.type })),
    dropEffect: 'none',
  } as unknown as DataTransfer
}

function image(name: string, lastModified = 100) {
  return new File(['image'], name, { type: 'image/png', lastModified })
}

function getResponsiveDocks() {
  const docks = screen.getAllByTestId('designer-reference-dock')
  expect(docks.map((dock) => dock.dataset.compact)).toEqual(
    expect.arrayContaining(['false', 'true']),
  )
  return docks
}

async function findResponsiveDocks() {
  await screen.findAllByTestId('designer-reference-dock')
  return getResponsiveDocks()
}

describe('Designer workspace reference drop', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    window.sessionStorage.clear()
    const NativeURL = URL
    vi.stubGlobal('URL', class extends NativeURL {
      static createObjectURL = vi.fn((file: File) => `blob:${file.name}`)
      static revokeObjectURL = vi.fn()
    })
    vi.mocked(designerApi.getProviders).mockResolvedValue([provider()])
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows a stable overlay across nested drag enter and leave events', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    await findResponsiveDocks()
    const canvas = screen.getByTestId('designer-canvas-frame')
    const dataTransfer = dragData()

    fireEvent.dragEnter(workspace, { dataTransfer })
    expect(screen.getByText('释放以添加参考图')).toBeInTheDocument()

    fireEvent.dragOver(workspace, { dataTransfer })
    expect(dataTransfer.dropEffect).toBe('copy')

    fireEvent.dragEnter(canvas, { dataTransfer })
    fireEvent.dragLeave(canvas, { dataTransfer })
    expect(screen.getByText('释放以添加参考图')).toBeInTheDocument()

    fireEvent.dragLeave(workspace, { dataTransfer })
    expect(screen.queryByText('释放以添加参考图')).not.toBeInTheDocument()
  })

  it('handles a drop over the dock once and updates every responsive dock', async () => {
    render(<DesignerPage />)
    await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const first = image('first.png')
    const second = image('second.png', 200)
    const dataTransfer = dragData([first, second])

    fireEvent.dragEnter(docks[0], { dataTransfer })
    expect(screen.getByText('释放以添加 2 张参考图')).toBeInTheDocument()
    fireEvent.drop(docks[0], { dataTransfer })

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).getByRole('img', { name: 'first.png' })).toBeInTheDocument()
        expect(within(dock).getByRole('img', { name: 'second.png' })).toBeInTheDocument()
      }
    })
    expect(toast.warning).not.toHaveBeenCalled()
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('enforces provider capacity and reports partial admission', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ maxReferenceImages: 1 }),
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first, second]) })

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).getByRole('img', { name: 'first.png' })).toBeInTheDocument()
        expect(within(dock).queryByRole('img', { name: 'second.png' })).not.toBeInTheDocument()
      }
    })
    expect(toast.warning).toHaveBeenCalledWith(
      '已添加 1 张，另外 1 张超过当前模型的 1 张上限',
    )
  })

  it('retains only the supported prefix when switching to a lower-capacity provider', async () => {
    const singleReferenceProvider = {
      ...provider({ maxReferenceImages: 1 }),
      id: 'single_reference',
      name: 'Single Reference',
      idx: 1,
    }
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ maxReferenceImages: 2 }),
      singleReferenceProvider,
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first, second]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'second.png' })).toBeInTheDocument()
    })

    fireEvent.click(screen.getAllByRole('combobox')[0])
    fireEvent.click(await screen.findByText('Single Reference'))

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).getByRole('img', { name: 'first.png' })).toBeInTheDocument()
        expect(within(dock).queryByRole('img', { name: 'second.png' })).not.toBeInTheDocument()
      }
    })
    expect(toast.warning).toHaveBeenCalledWith(
      '当前模型最多支持 1 张参考图，已移除 1 张',
    )
  })

  it('reports duplicate and non-image files instead of silently discarding them', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const existing = image('existing.png')
    const duplicate = image('existing.png')
    const text = new File(['notes'], 'notes.txt', { type: 'text/plain' })

    fireEvent.drop(workspace, { dataTransfer: dragData([existing]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'existing.png' })).toBeInTheDocument()
    })
    toast.warning.mockClear()

    fireEvent.drop(workspace, { dataTransfer: dragData([duplicate, text]) })

    expect(toast.warning).toHaveBeenCalledWith(
      '忽略 1 个非图片文件，忽略 1 张重复图片',
    )
  })

  it('does not activate for text drags', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    await findResponsiveDocks()

    fireEvent.dragEnter(workspace, { dataTransfer: dragData([], ['text/plain']) })

    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('does not activate for providers without reference support', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ supportsReference: false, maxReferenceImages: 0 }),
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    await screen.findAllByText('GPT Image 2')

    fireEvent.dragEnter(workspace, { dataTransfer: dragData([image('ignored.png')]) })

    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('clears the overlay when Escape is pressed', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    await findResponsiveDocks()

    fireEvent.dragEnter(workspace, { dataTransfer: dragData() })
    expect(screen.getByTestId('designer-drop-overlay')).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('shares picker additions and removals through page-owned state and capacity', async () => {
    render(<DesignerPage />)
    await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const desktopDock = docks.find((dock) => dock.dataset.compact === 'false')!
    const mobileDock = docks.find((dock) => dock.dataset.compact === 'true')!
    const picker = within(desktopDock).getByTestId('designer-reference-input')
    const selected = image('picker.png')

    fireEvent.change(picker, { target: { files: [selected] } })

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).getByRole('img', { name: 'picker.png' })).toBeInTheDocument()
      }
    })

    fireEvent.click(within(mobileDock).getByRole('button', { name: '移除参考图：picker.png' }))

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).queryByRole('img', { name: 'picker.png' })).not.toBeInTheDocument()
        expect(within(dock).getByRole('button', { name: '添加参考图' })).toBeInTheDocument()
      }
    })
    expect(screen.getByText('0/2')).toBeInTheDocument()
  })
})
