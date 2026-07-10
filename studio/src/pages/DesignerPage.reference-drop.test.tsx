import { useLayoutEffect, useRef, type ReactNode } from 'react'
import {
  act,
  fireEvent,
  render as testingRender,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { QueryClientProvider, useQuery, type QueryClient } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { ThemeProvider } from 'next-themes'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerPage from './DesignerPage'
import { designerApi } from '@/lib/api/designer'
import { createTestQueryClient, render } from '@/test/test-utils'
import type { DesignerProvider } from '@/types/designer'

const PROVIDERS_QUERY_KEY = ['designer', 'providers'] as const

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

function dispatchReferenceDrop(target: Element, dataTransfer: DataTransfer) {
  const event = new Event('drop', { bubbles: true, cancelable: true })
  Object.defineProperty(event, 'dataTransfer', { value: dataTransfer })
  target.dispatchEvent(event)
}

function image(name: string, lastModified = 100) {
  return new File(['image'], name, { type: 'image/png', lastModified })
}

function renderWithQueryClient(queryClient: QueryClient, extra?: ReactNode) {
  return testingRender(
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
          <DesignerPage />
          {extra}
        </ThemeProvider>
      </BrowserRouter>
    </QueryClientProvider>,
  )
}

function GenerateBeforeProviderReconciliation() {
  const submittedRef = useRef(false)
  const { data: providers } = useQuery({
    queryKey: PROVIDERS_QUERY_KEY,
    queryFn: () => designerApi.getProviders(),
  })
  const effectiveProvider = providers?.find((candidate) => candidate.enabled)

  useLayoutEffect(() => {
    if (
      submittedRef.current
      || !effectiveProvider
      || effectiveProvider.capabilities.supportsReference
    ) {
      return
    }

    submittedRef.current = true
    screen.getByRole('button', { name: '生成' }).click()
  }, [effectiveProvider])

  return null
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
    vi.mocked(designerApi.uploadReference).mockImplementation(async (file) => ({
      file_id: `uploaded:${file.name}`,
      filename: file.name,
      size: file.size,
    }))
    vi.mocked(designerApi.generate).mockResolvedValue({
      generation_id: 'generation-1',
      status: 'generating',
    })
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

  it('accepts full-capacity dragover so drop can report overflow without activating the overlay', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ maxReferenceImages: 1 }),
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'first.png' })).toBeInTheDocument()
    })
    toast.warning.mockClear()

    const dataTransfer = dragData([second])
    fireEvent.dragEnter(workspace, { dataTransfer })
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()

    expect(fireEvent.dragOver(workspace, { dataTransfer })).toBe(false)
    expect(dataTransfer.dropEffect).toBe('copy')

    fireEvent.drop(workspace, { dataTransfer })

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).getByRole('img', { name: 'first.png' })).toBeInTheDocument()
        expect(within(dock).queryByRole('img', { name: 'second.png' })).not.toBeInTheDocument()
      }
    })
    expect(toast.warning).toHaveBeenCalledWith(
      '参考图已达到当前模型的 1 张上限',
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

  it('normalizes references once when the effective provider capacity shrinks in query data', async () => {
    const queryClient = createTestQueryClient()
    const initialProvider = provider({ maxReferenceImages: 2, watermark: true })
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([initialProvider])
    renderWithQueryClient(queryClient)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.click(screen.getByRole('button', { name: /水印/ }))
    expect(screen.getByRole('button', { name: /水印/ })).toHaveClass('border-primary')

    fireEvent.drop(workspace, { dataTransfer: dragData([first, second]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'second.png' })).toBeInTheDocument()
    })
    toast.warning.mockClear()

    await act(async () => {
      queryClient.setQueryData(PROVIDERS_QUERY_KEY, [
        provider({ maxReferenceImages: 1, watermark: true }),
      ])
    })

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).getByRole('img', { name: 'first.png' })).toBeInTheDocument()
        expect(within(dock).queryByRole('img', { name: 'second.png' })).not.toBeInTheDocument()
      }
    })
    expect(screen.getByRole('button', { name: /水印/ })).toHaveClass('border-primary')
    expect(toast.warning).toHaveBeenCalledTimes(1)
    expect(toast.warning).toHaveBeenCalledWith(
      '当前模型最多支持 1 张参考图，已移除 1 张',
    )
  })

  it('reconciles a disabled selection to an unsupported fallback and does not restore hidden refs', async () => {
    const queryClient = createTestQueryClient()
    const initialProvider = provider({ maxReferenceImages: 3 })
    const fallbackProvider: DesignerProvider = {
      ...provider({ supportsReference: false, maxReferenceImages: 0 }),
      id: 'fallback_unsupported',
      name: 'Fallback Unsupported',
      provider: 'gemini',
      model: 'fallback-model',
      idx: 1,
    }
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      initialProvider,
      fallbackProvider,
    ])
    renderWithQueryClient(queryClient)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first, second]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'second.png' })).toBeInTheDocument()
    })
    fireEvent.dragEnter(workspace, { dataTransfer: dragData() })
    expect(screen.getByTestId('designer-drop-overlay')).toBeInTheDocument()
    toast.warning.mockClear()

    await act(async () => {
      queryClient.setQueryData(PROVIDERS_QUERY_KEY, [
        { ...initialProvider, enabled: false },
        fallbackProvider,
      ])
    })

    await waitFor(() => {
      expect(screen.queryAllByTestId('designer-reference-dock')).toHaveLength(0)
      expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
      for (const selector of screen.getAllByRole('combobox')) {
        expect(selector).toHaveTextContent('Fallback Unsupported')
      }
    })
    expect(toast.warning).toHaveBeenCalledTimes(1)
    expect(toast.warning).toHaveBeenCalledWith(
      '当前模型最多支持 0 张参考图，已移除 2 张',
    )

    const unsupportedDrag = dragData([image('ignored.png')])
    expect(fireEvent.dragOver(workspace, { dataTransfer: unsupportedDrag })).toBe(true)
    expect(unsupportedDrag.dropEffect).toBe('none')

    await act(async () => {
      queryClient.setQueryData(PROVIDERS_QUERY_KEY, [initialProvider, fallbackProvider])
    })

    await waitFor(() => {
      expect(screen.queryAllByTestId('designer-reference-dock')).toHaveLength(0)
      for (const selector of screen.getAllByRole('combobox')) {
        expect(selector).toHaveTextContent('Fallback Unsupported')
      }
    })
    expect(toast.warning).toHaveBeenCalledTimes(1)
  })

  it('bounds submit-time reference uploads before passive provider reconciliation', async () => {
    const queryClient = createTestQueryClient()
    const initialProvider = provider({ maxReferenceImages: 2 })
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([initialProvider])
    renderWithQueryClient(queryClient, <GenerateBeforeProviderReconciliation />)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first, second]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'second.png' })).toBeInTheDocument()
    })

    fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), {
      target: { value: '生成一张测试图片' },
    })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled()
    })
    vi.mocked(designerApi.uploadReference).mockClear()
    vi.mocked(designerApi.generate).mockClear()

    await act(async () => {
      queryClient.setQueryData(PROVIDERS_QUERY_KEY, [
        provider({ supportsReference: false, maxReferenceImages: 0 }),
      ])
    })

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    expect(designerApi.uploadReference).not.toHaveBeenCalled()
    expect(vi.mocked(designerApi.generate).mock.calls[0][0].reference_file_ids).toBeUndefined()
  })

  it('keeps reference state intact across batched toolbar setting writes', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      provider({ maxReferenceImages: 3, watermark: true }),
    ])
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const mobileDock = docks.find((dock) => dock.dataset.compact === 'true')!
    const first = image('first.png')
    const second = image('second.png', 200)

    fireEvent.drop(workspace, { dataTransfer: dragData([first]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'first.png' })).toBeInTheDocument()
    })

    const watermarkButton = screen.getByRole('button', { name: /水印/ })
    act(() => {
      dispatchReferenceDrop(workspace, dragData([second]))
      watermarkButton.click()
    })

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).getAllByRole('img', { name: 'first.png' })).toHaveLength(1)
        expect(within(dock).getAllByRole('img', { name: 'second.png' })).toHaveLength(1)
      }
    })

    const removeFirst = within(mobileDock).getByRole('button', {
      name: '移除参考图：first.png',
    })
    act(() => {
      removeFirst.click()
      watermarkButton.click()
    })

    await waitFor(() => {
      for (const dock of docks) {
        expect(within(dock).queryByRole('img', { name: 'first.png' })).not.toBeInTheDocument()
        expect(within(dock).getAllByRole('img', { name: 'second.png' })).toHaveLength(1)
      }
    })
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

  it('reports duplicate-only admission with the focused aggregate message', async () => {
    render(<DesignerPage />)
    const workspace = await screen.findByTestId('designer-workspace')
    const docks = await findResponsiveDocks()
    const existing = image('existing.png')
    const duplicate = image('existing.png')

    fireEvent.drop(workspace, { dataTransfer: dragData([existing]) })
    await waitFor(() => {
      expect(within(docks[0]).getByRole('img', { name: 'existing.png' })).toBeInTheDocument()
    })
    toast.warning.mockClear()

    fireEvent.drop(workspace, { dataTransfer: dragData([duplicate]) })

    expect(toast.warning).toHaveBeenCalledWith('这些图片已经在参考素材中')
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

    const dataTransfer = dragData([image('ignored.png')])
    fireEvent.dragEnter(workspace, { dataTransfer })

    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
    expect(fireEvent.dragOver(workspace, { dataTransfer })).toBe(true)
    expect(dataTransfer.dropEffect).toBe('none')
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
