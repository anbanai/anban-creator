import { useLayoutEffect, useRef, type ReactNode } from 'react'
import { act, fireEvent, render as testingRender, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider, useQuery } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { ThemeProvider } from 'next-themes'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerPage from './DesignerPage'
import { designerApi } from '@/lib/api/designer'
import { api } from '@/lib/api'
import { uploadToOSS } from '@/lib/direct-upload'
import { createTestQueryClient, render } from '@/test/test-utils'
import { AgentPromptDropProvider } from '@/components/agent-prompt/AgentPromptDropProvider'
import type { DesignerProvider, ImageGeneration } from '@/types/designer'

const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn(), warning: vi.fn() }))
const exportMaskMock = vi.hoisted(() => vi.fn())

vi.mock('sonner', () => ({ toast }))
vi.mock('@/lib/direct-upload', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/direct-upload')>()
  return { ...actual, uploadToOSS: vi.fn() }
})
vi.mock('@/lib/api/designer', () => ({
  designerApi: {
    getProviders: vi.fn(),
    generate: vi.fn(),
    registerReference: vi.fn(),
    uploadReferenceFromUrl: vi.fn(),
    getHistory: vi.fn(),
    getGeneration: vi.fn(),
  },
}))
vi.mock('@/lib/api', () => ({
  api: {
    credits: { balance: vi.fn() },
    projects: { list: vi.fn() },
    designer: {
      getHistory: vi.fn(),
    },
  },
}))
vi.mock('@/components/designer/DesignerCanvas', () => ({
  default: ({ onEdit }: { onEdit: (image: { url: string; index: number }) => void }) => (
    <button type="button" onClick={() => onEdit({ url: '/api/v1/files/user-1/designer/source.png', index: 0 })}>
      测试编辑
    </button>
  ),
}))
vi.mock('@/components/designer/InlineMaskEditor', async () => {
  const React = await import('react')
  return {
    default: React.forwardRef(function MaskEditor(_props, ref) {
      React.useImperativeHandle(ref, () => ({
        exportMask: exportMaskMock,
      }))
      return <div>蒙版编辑器</div>
    }),
  }
})

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
      qualityLevels: ['auto'], sizePresets: ['auto'], defaultSize: 'auto', maxBatch: 1,
      maxReferenceImages: 3, supportsReference: true, supportsMask: true,
      outputFormats: ['png'], hasBackground: false, hasCompression: false, watermark: false,
      ...overrides,
    },
    pricing: {},
  }
}

function image(name: string, lastModified = 1) {
  return new File(['image'], name, { type: 'image/png', lastModified })
}

function uploadResult(file: File) {
  return {
    uploadId: `upload:${file.name}`,
    key: `uploads/finalized/user-1/upload:${file.name}/${file.name}`,
    publicUrl: '', contentType: file.type, size: file.size,
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function SubmitBeforeProviderReconciliation() {
  const submitted = useRef(false)
  const { data } = useQuery({
    queryKey: ['designer', 'providers'],
    queryFn: () => designerApi.getProviders(),
  })
  useLayoutEffect(() => {
    if (submitted.current || data?.[0]?.capabilities.maxReferenceImages !== 1) return
    submitted.current = true
    screen.getByRole('button', { name: '生成' }).click()
  }, [data])
  return null
}

async function addFiles(files: File[]) {
  await waitFor(() => expect(screen.getByRole('button', { name: '添加附件' })).not.toBeDisabled())
  const input = screen.getByLabelText('选择附件文件')
  fireEvent.change(input, { target: { files } })
  await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(files.length))
  await waitFor(() => {
    for (const file of files) expect(screen.getByText(file.name)).toBeInTheDocument()
  })
}

describe('Designer shared prompt composer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    window.sessionStorage.clear()
    Object.defineProperty(Element.prototype, 'getAnimations', {
      configurable: true,
      value: vi.fn(() => []),
    })
    vi.mocked(designerApi.getProviders).mockResolvedValue([provider()])
    exportMaskMock.mockResolvedValue(new File(['mask'], 'mask.png', { type: 'image/png' }))
    vi.mocked(api.projects.list).mockResolvedValue([
      { id: 'project-1', name: '品牌项目', platform: 'article' } as never,
    ])
    vi.mocked(api.credits.balance).mockResolvedValue({ balance: 1000 } as never)
    vi.mocked(api.designer.getHistory).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 50 })
    vi.mocked(uploadToOSS).mockImplementation(async ({ file }) => uploadResult(file))
    vi.mocked(designerApi.registerReference).mockImplementation(async ({ upload_id }) => ({
      file_id: `registered:${upload_id}`, filename: upload_id, size: 5,
    }))
    vi.mocked(designerApi.generate).mockResolvedValue({ generation_id: 'generation-1', status: 'generating' })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the single shared composer in the canvas and removes the old dock and overlay', async () => {
    render(<DesignerPage />)
    const canvas = await screen.findByTestId('designer-canvas-frame')
    expect(canvas.querySelector('[data-slot="agent-prompt-input"]')).toBeInTheDocument()
    expect(screen.queryByTestId('designer-reference-dock')).not.toBeInTheDocument()
    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('uploads on selection, then only registers ordered keys before generation', async () => {
    render(<DesignerPage />)
    const first = image('first.png')
    const second = image('second.png', 2)
    await addFiles([first, second])

    expect(uploadToOSS).toHaveBeenNthCalledWith(1, expect.objectContaining({ purpose: 'designer_reference', file: first }))
    expect(designerApi.registerReference).not.toHaveBeenCalled()
    expect(designerApi.generate).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '生成海报' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    expect(vi.mocked(designerApi.registerReference).mock.calls.map(([value]) => value.upload_id)).toEqual([
      'upload:first.png', 'upload:second.png',
    ])
    expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      project_id: 'default',
      reference_file_ids: ['registered:upload:first.png', 'registered:upload:second.png'],
    }))
    await waitFor(() => expect(screen.getByLabelText('Designer prompt')).toHaveValue(''))
  })

  it('retains the ordered prefix and aborts overflow uploads when provider capacity shrinks', async () => {
    const pending = new Map<string, { signal?: AbortSignal; resolve: (value: ReturnType<typeof uploadResult>) => void }>()
    vi.mocked(uploadToOSS).mockImplementation(({ file, signal }) => new Promise((resolve) => {
      pending.set(file.name, { signal, resolve })
    }))
    const queryClient = createTestQueryClient()
    testingRender(<DesignerPage />, {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
              <AgentPromptDropProvider>{children}</AgentPromptDropProvider>
            </ThemeProvider>
          </BrowserRouter>
        </QueryClientProvider>
      ),
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '添加附件' })).not.toBeDisabled())
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: { files: [image('first.png'), image('second.png', 2), image('third.png', 3)] },
    })
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(3))
    await waitFor(() => expect(screen.getByText('third.png')).toBeInTheDocument())

    await act(async () => {
      queryClient.setQueryData(['designer', 'providers'], [provider({ maxReferenceImages: 1 })])
    })
    await waitFor(() => expect(pending.get('second.png')?.signal?.aborted).toBe(true))
    expect(pending.get('third.png')?.signal?.aborted).toBe(true)
    pending.get('first.png')?.resolve(uploadResult(image('first.png')))
    await waitFor(() => expect(screen.getByText('first.png')).toBeInTheDocument())
    expect(screen.queryByText('second.png')).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '只用第一张' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      reference_file_ids: ['registered:upload:first.png'],
    })))
  })

  it('clears references when the provider does not support them', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([provider({ supportsReference: false, maxReferenceImages: 9 })])
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    expect(screen.getByRole('button', { name: '添加附件' })).toBeDisabled()
  })

  it('uses selected project for generation and explicit project IDs for history', async () => {
    render(<DesignerPage />)
    const projectControl = await screen.findByRole('combobox', { name: '项目上下文' })
    await waitFor(() => expect(projectControl).not.toBeDisabled())
    fireEvent.click(projectControl)
    fireEvent.click(await screen.findByRole('option', { name: '品牌项目' }))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '品牌图' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({ project_id: 'project-1' })))

    fireEvent.click(screen.getAllByText('历史记录')[0])
    await waitFor(() => expect(api.designer.getHistory).toHaveBeenCalledWith({ project_id: 'project-1', page_size: 50 }))
  })

  it('requests default-project history explicitly', async () => {
    render(<DesignerPage />)
    fireEvent.click((await screen.findAllByText('历史记录'))[0])
    await waitFor(() => expect(api.designer.getHistory).toHaveBeenCalledWith({ project_id: 'default', page_size: 50 }))
  })

  it('bounds submit to the new provider prefix before passive reconciliation', async () => {
    const queryClient = createTestQueryClient()
    testingRender(
      <>
        <DesignerPage />
        <SubmitBeforeProviderReconciliation />
      </>,
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={queryClient}>
            <BrowserRouter>
              <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
                <AgentPromptDropProvider>{children}</AgentPromptDropProvider>
              </ThemeProvider>
            </BrowserRouter>
          </QueryClientProvider>
        ),
      },
    )
    await addFiles([image('first.png'), image('second.png', 2), image('third.png', 3)])
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '只用新容量' } })

    await act(async () => {
      queryClient.setQueryData(['designer', 'providers'], [provider({ maxReferenceImages: 1 })])
    })

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      reference_file_ids: ['registered:upload:first.png'],
    })))
  })

  it('uploads the edit mask to OSS and registers it before generating', async () => {
    vi.mocked(designerApi.uploadReferenceFromUrl).mockResolvedValue({ file_id: 'source-1', filename: 'source', size: 0 })
    render(<DesignerPage />)
    fireEvent.click(await screen.findByRole('button', { name: '测试编辑' }))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '改成红色' } })
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))

    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'designer_reference', file: expect.objectContaining({ name: 'mask.png' }),
    })))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      project_id: 'default', reference_file_ids: ['source-1'], mask_file_id: 'registered:upload:mask.png',
    })))
  })

  it('restores a history prompt without losing current references', async () => {
    const generation = {
      id: 'history-1', user_id: 'user-1', project_id: 'default', prompt: '历史提示词', provider: 'openai',
      model: 'gpt-image-2', n: 1, status: 'completed', created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    } satisfies ImageGeneration
    vi.mocked(api.designer.getHistory).mockResolvedValue({ items: [generation], total: 1, page: 1, page_size: 50 })
    render(<DesignerPage />)
    await addFiles([image('keep.png')])
    fireEvent.click(screen.getAllByText('历史记录')[0])
    fireEvent.click(await screen.findByRole('button', { name: '重新生成' }))
    expect(screen.getByLabelText('Designer prompt')).toHaveValue('历史提示词')
    expect(screen.getByText('keep.png')).toBeInTheDocument()
  })

  it('cancels an active generation from the shared composer', async () => {
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '生成后取消' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '取消生成' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '取消生成' }))

    await waitFor(() => expect(screen.queryByRole('button', { name: '取消生成' })).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: '生成' })).toBeInTheDocument()
  })

  it('does not restart generation when a canceled reference registration resolves late', async () => {
    const registration = deferred<{ file_id: string; filename: string; size: number }>()
    vi.mocked(designerApi.registerReference).mockReturnValueOnce(registration.promise)
    render(<DesignerPage />)
    await addFiles([image('late-register.png')])
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '提交 A' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.registerReference).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: '取消生成' }))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '提交 B' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))

    registration.resolve({ file_id: 'late-reference', filename: 'late.png', size: 5 })
    await act(async () => {})
    expect(designerApi.generate).toHaveBeenCalledTimes(1)
  })

  it('keeps a newer prompt and does not begin polling when canceled generate resolves late', async () => {
    const generation = deferred<{ generation_id: string; status: string }>()
    vi.mocked(designerApi.generate).mockReturnValueOnce(generation.promise)
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '提交 A' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '用户继续输入 B' } })
    fireEvent.click(screen.getByRole('button', { name: '取消生成' }))
    generation.resolve({ generation_id: 'late-generation', status: 'generating' })
    await act(async () => {})

    expect(screen.getByLabelText('Designer prompt')).toHaveValue('用户继续输入 B')
    expect(designerApi.getGeneration).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '取消生成' })).not.toBeInTheDocument()
  })

  it('ignores a canceled in-flight poll result and allows a fresh submit', async () => {
    const poll = deferred<ImageGeneration>()
    vi.mocked(designerApi.getGeneration).mockReturnValueOnce(poll.promise)
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    vi.useFakeTimers()
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '等待轮询' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(designerApi.getGeneration).toHaveBeenCalledTimes(1)

    fireEvent.click(screen.getByRole('button', { name: '取消生成' }))
    poll.resolve({
      id: 'late-generation', user_id: 'user-1', project_id: 'default', prompt: '等待轮询',
      provider: 'openai', model: 'gpt-image-2', n: 1, status: 'completed',
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      results: [{ id: 1, generation_id: 'late-generation', image_url: 'https://example.com/late.png', index: 0 }],
    })
    await act(async () => {})

    expect(toast.success).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '取消生成' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '新的提交' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await act(async () => {})
    expect(designerApi.generate).toHaveBeenCalledTimes(2)
  })

  it('shows one visible aggregated warning for rejected attachments', async () => {
    render(<DesignerPage />)
    await addFiles([image('duplicate.png')])
    toast.warning.mockClear()
    const tooLarge = new File(
      [new Uint8Array(10 * 1024 * 1024 + 1)],
      'too-large.png',
      { type: 'image/png' },
    )
    fireEvent.change(screen.getByLabelText('选择附件文件'), {
      target: {
        files: [
          image('duplicate.png'),
          new File(['notes'], 'notes.txt', { type: 'text/plain' }),
          tooLarge,
          image('accepted-1.png', 2),
          image('accepted-2.png', 3),
          image('overflow.png', 4),
        ],
      },
    })

    await waitFor(() => expect(toast.warning).toHaveBeenCalledTimes(1))
    expect(toast.warning).toHaveBeenCalledWith(
      '忽略 1 个重复文件，忽略 1 个不支持的文件，忽略 1 个超过大小限制的文件，忽略 1 个超过数量上限的文件',
    )
  })

  it('cancels a pending mask export without surfacing a submit error', async () => {
    const mask = deferred<File>()
    exportMaskMock.mockReturnValueOnce(mask.promise)
    render(<DesignerPage />)
    fireEvent.click(await screen.findByRole('button', { name: '测试编辑' }))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '取消蒙版' } })
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    await waitFor(() => expect(exportMaskMock).toHaveBeenCalledTimes(1))

    fireEvent.click(screen.getByRole('button', { name: '取消生成' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    expect(screen.queryByText('提交失败，请重试')).not.toBeInTheDocument()

    mask.resolve(new File(['mask'], 'mask.png', { type: 'image/png' }))
    await act(async () => {})
    expect(designerApi.uploadReferenceFromUrl).not.toHaveBeenCalled()
  })
})
