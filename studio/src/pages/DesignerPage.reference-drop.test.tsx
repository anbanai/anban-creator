import { AxiosError, CanceledError } from 'axios'
import http from '@/lib/http-client'
import standaloneRequest from '../../../server/service/testdata/designer-standalone-request.json'
import { useLayoutEffect, useRef, type ReactNode } from 'react'
import { act, fireEvent, render as testingRender, screen, waitFor, within } from '@testing-library/react'
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
import type { DesignerCapability, ImageGeneration } from '@/types/designer'

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'user-1' } }) }))

const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn(), warning: vi.fn() }))
const exportMaskMock = vi.hoisted(() => vi.fn())

vi.mock('sonner', () => ({ toast }))
vi.mock('@/lib/direct-upload', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/direct-upload')>()
  return { ...actual, uploadToOSS: vi.fn() }
})
vi.mock('@/lib/api/designer', () => ({
  designerApi: {
    getCapabilities: vi.fn(),
    generate: vi.fn(),
    registerReference: vi.fn(),
    uploadReferenceFromUrl: vi.fn(),
    getHistory: vi.fn(),
    getGeneration: vi.fn(),
  },
}))
vi.mock('@/lib/api', () => ({
  api: {
    billing: { wallet: vi.fn().mockResolvedValue({ balance: 1000 }) },
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

function provider(overrides: Partial<DesignerCapability['designerFeatures']> = {}): DesignerCapability {
  return {
    id: 'professional',
    name: '专业增强',
    description: '适合复杂构图与高细节视觉任务',
    minTier: 'enterprise',
    credits: 0,
    priceAvailable: true,
    enabled: true,
    idx: 0,
    designerFeatures: {
      qualityLevels: ['auto'], sizePresets: ['1:1:2K'], defaultSize: '1:1:2K', maxBatch: 1,
      maxReferenceImages: 3, supportsReference: true, supportsMask: true,
      outputFormats: ['png'], hasBackground: false, hasCompression: false, watermark: false,
      ...overrides,
    },
  }
}

function image(name: string, lastModified = 1) {
  return new File(['image'], name, { type: 'image/png', lastModified })
}

function uploadResult(file: File) {
  return {
    uploadSessionId: `session:${file.name}`,
    uploadId: `upload:${file.name}`,
    key: `uploads/finalized/user-1/upload:${file.name}/${file.name}`,
    previewUrl: '', publicUrl: '', contentType: file.type, size: file.size,
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
    queryKey: ['designer', 'capabilities'],
    queryFn: () => designerApi.getCapabilities(),
  })
  useLayoutEffect(() => {
    if (submitted.current || data?.items[0]?.designerFeatures.maxReferenceImages !== 1) return
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
    for (const file of files) expect(screen.getAllByText(file.name).length).toBeGreaterThan(0)
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
    vi.mocked(designerApi.getCapabilities).mockResolvedValue({
      items: [provider()],
      defaultCapability: 'professional',
    })
    exportMaskMock.mockResolvedValue(new File(['mask'], 'mask.png', { type: 'image/png' }))
    vi.mocked(api.projects.list).mockResolvedValue([
      { id: 'project-1', name: '品牌项目', platform: 'article' } as never,
    ])
    vi.mocked(api.designer.getHistory).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 50 })
    vi.mocked(uploadToOSS).mockImplementation(async ({ file }) => uploadResult(file))
    vi.mocked(designerApi.registerReference).mockImplementation(async ({ upload_id }) => ({
      file_id: `registered:${upload_id}`, filename: upload_id, size: 5,
    }))
    vi.mocked(designerApi.generate).mockResolvedValue({ generation_id: 'generation-1', status: 'generating', price_credits: 500 })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('sends the shared Server standalone quote/admission payload when no project is selected', async () => {
    vi.mocked(designerApi.getCapabilities).mockResolvedValue({
      items: [{ ...provider({ defaultSize: '1:1', sizePresets: ['1:1'] }), id: 'standard' }], defaultCapability: 'standard',
    })
    render(<DesignerPage />)
    fireEvent.change(await screen.findByLabelText('Designer prompt'), { target: { value: standaloneRequest.prompt } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining(standaloneRequest), expect.any(AbortSignal), expect.any(Function)))
  })

  it.each(['network loss', 'abort'] as const)('recovers the same server-accepted operation after %s and remount', async (mode) => {
    const actual = await vi.importActual<typeof import('@/lib/api/designer')>('@/lib/api/designer')
    vi.mocked(designerApi.generate).mockImplementationOnce(actual.designerApi.generate)
    let operation = ''
    let savedAtAdmission: string | null = null
    let admissions = 0
    const adapter = http.defaults.adapter
    http.defaults.adapter = async (config) => {
      if (config.url === '/designer/quote') return { config, status: 200, statusText: 'OK', headers: {}, data: { data: { quote_id: 'q', request_fingerprint: 'f' } } }
      admissions++
      operation = JSON.parse(config.data).operation_id
      savedAtAdmission = sessionStorage.getItem('designer_active_generation:user-1')
      if (mode === 'network loss') throw new AxiosError('response lost', 'ERR_NETWORK', config)
      return new Promise((_resolve, reject) => config.signal?.addEventListener?.('abort', () => reject(new CanceledError('response aborted', config)), { once: true }))
    }
    try {
      const view = render(<DesignerPage />)
      fireEvent.change(await screen.findByLabelText('Designer prompt'), { target: { value: 'accepted but response lost' } })
      fireEvent.click(screen.getByRole('button', { name: '生成' }))
      await waitFor(() => expect(admissions).toBe(1))
      expect(savedAtAdmission).toContain(operation)
      if (mode === 'abort') fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
      await act(async () => {})
      expect(sessionStorage.getItem('designer_active_generation:user-1')).toContain(operation)
      view.unmount()
      vi.mocked(designerApi.getGeneration).mockResolvedValueOnce({ id: operation, project_id: '', prompt: 'accepted', n: 1, status: 'completed', created_at: '', updated_at: '' })
      render(<DesignerPage />)
      await waitFor(() => expect(designerApi.getGeneration).toHaveBeenCalledWith(operation, expect.any(AbortSignal)))
      await waitFor(() => expect(sessionStorage.getItem('designer_active_generation:user-1')).toBeNull())
      expect(admissions).toBe(1)
    } finally { http.defaults.adapter = adapter }
  })

  it('preserves uncertain identity when reconciliation fails, including a server 404', async () => {
    sessionStorage.setItem('designer_active_generation:user-1', JSON.stringify({ generationId: 'unaccepted', startedAt: Date.now() }))
    vi.mocked(designerApi.getGeneration).mockRejectedValueOnce(new AxiosError('not found', 'ERR_BAD_REQUEST', undefined, undefined, { status: 404, data: {}, statusText: 'Not Found', headers: {}, config: {} as never }))
    render(<DesignerPage />)
    await waitFor(() => expect(designerApi.getGeneration).toHaveBeenCalled())
    await waitFor(() => expect(screen.getByText('已停止等待；已受理的任务仍会继续处理，可在历史记录或刷新后查看结果')).toBeInTheDocument())
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toContain('unaccepted')
    expect(designerApi.generate).not.toHaveBeenCalled()
  })

  it('clears recovery after a definitive admission rejection', async () => {
    const actual = await vi.importActual<typeof import('@/lib/api/designer')>('@/lib/api/designer')
    vi.mocked(designerApi.generate).mockImplementationOnce(actual.designerApi.generate)
    vi.spyOn(http, 'post').mockImplementation(async (path, _payload, config) => {
      if (path === '/designer/quote') return { data: { data: { quote_id: 'q', request_fingerprint: 'f' } } }
      throw new AxiosError('insufficient funds', 'ERR_BAD_REQUEST', undefined, undefined, { status: 402, data: {}, statusText: 'Payment Required', headers: {}, config: config as never })
    })
    const view = render(<DesignerPage />)
    fireEvent.change(await screen.findByLabelText('Designer prompt'), { target: { value: 'rejected' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalled())
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toBeNull()
    view.unmount()
    render(<DesignerPage />)
    await act(async () => {})
    expect(designerApi.getGeneration).not.toHaveBeenCalled()
  })

  it('does not save or admit a canceled quote even when its response arrives late', async () => {
    const actual = await vi.importActual<typeof import('@/lib/api/designer')>('@/lib/api/designer')
    vi.mocked(designerApi.generate).mockImplementationOnce(actual.designerApi.generate)
    const quote = deferred<never>()
    const post = vi.spyOn(http, 'post').mockReturnValueOnce(quote.promise)
    render(<DesignerPage />)
    fireEvent.change(await screen.findByLabelText('Designer prompt'), { target: { value: 'cancel quote' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(post).toHaveBeenCalledOnce())
    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
    quote.resolve({ data: { data: { quote_id: 'q', request_fingerprint: 'f' } } } as never)
    await act(async () => {})
    expect(post).toHaveBeenCalledOnce()
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toBeNull()
  })

  it('registers a dropped reference and sends its owned file identity', async () => {
    render(<DesignerPage />)
    const prompt = await screen.findByLabelText('Designer prompt')
    await waitFor(() => expect(screen.getByRole('button', { name: '添加附件' })).not.toBeDisabled())
    const composer = prompt.closest('[data-slot="agent-prompt-input"]')!
    fireEvent.drop(composer, { dataTransfer: { files: [image('dropped.png')], types: ['Files'] } })
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getAllByText('dropped.png').length).toBeGreaterThan(0))
    fireEvent.change(prompt, { target: { value: '使用拖入参考图' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({ reference_file_ids: ['registered:upload:dropped.png'] }), expect.any(AbortSignal), expect.any(Function)))
  })

  it('offers a retry with the same operation after an ambiguous response', async () => {
    vi.mocked(designerApi.generate).mockRejectedValueOnce(new Error('network response lost'))
    render(<DesignerPage />)
    fireEvent.change(await screen.findByLabelText('Designer prompt'), { target: { value: 'one logical image' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    fireEvent.click(await screen.findByRole('button', { name: '重试上次提交' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(2))
    const [first, second] = vi.mocked(designerApi.generate).mock.calls
    expect(first[0].operation_id).toBeTruthy()
    expect(second[0]).toEqual(first[0])
  })

  it('uploads on selection, then only registers ordered keys before generation', async () => {
    render(<DesignerPage />)
    const first = image('first.png')
    const second = image('second.png', 2)
    await addFiles([first, second])

    expect(uploadToOSS).toHaveBeenNthCalledWith(1, expect.objectContaining({ purpose: 'designer_reference', file: first }))
    expect(designerApi.registerReference).not.toHaveBeenCalled()
    expect(designerApi.generate).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toBeNull()

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '生成海报' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    expect(vi.mocked(designerApi.registerReference).mock.calls.map(([value]) => value.upload_id)).toEqual([
      'upload:first.png', 'upload:second.png',
    ])
    expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      project_id: '',
      reference_file_ids: ['registered:upload:first.png', 'registered:upload:second.png'],
    }), expect.any(AbortSignal), expect.any(Function))
    await waitFor(() => expect(screen.getByLabelText('Designer prompt')).toHaveValue(''))
  })

  it('uses the Designer capability reference limit rather than the general five-file limit', async () => {
    vi.mocked(designerApi.getCapabilities).mockResolvedValue({ items: [provider({ maxReferenceImages: 6 })], defaultCapability: 'professional' })
    render(<DesignerPage />)
    await addFiles(Array.from({ length: 6 }, (_, index) => image(`reference-${index}.png`, index)))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: 'six references' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({ reference_file_ids: Array.from({ length: 6 }, (_, index) => `registered:upload:reference-${index}.png`) }), expect.any(AbortSignal), expect.any(Function)))
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
    await waitFor(() => expect(screen.getAllByText('third.png').length).toBeGreaterThan(0))

    await act(async () => {
      queryClient.setQueryData(['designer', 'capabilities'], {
        items: [provider({ maxReferenceImages: 1 })],
        defaultCapability: 'professional',
      })
    })
    await waitFor(() => expect(pending.get('second.png')?.signal?.aborted).toBe(true))
    expect(pending.get('third.png')?.signal?.aborted).toBe(true)
    await act(async () => {
      pending.get('first.png')?.resolve(uploadResult(image('first.png')))
    })
    await waitFor(() => expect(screen.getByRole('status', { name: 'first.png 状态' })).toHaveTextContent('已上传'))
    expect(screen.queryByText('second.png')).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '只用第一张' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      reference_file_ids: ['registered:upload:first.png'],
    }), expect.any(AbortSignal), expect.any(Function)))
  })

  it('clears references when the provider does not support them', async () => {
    vi.mocked(designerApi.getCapabilities).mockResolvedValueOnce({
      items: [provider({ supportsReference: false, maxReferenceImages: 9 })],
      defaultCapability: 'professional',
    })
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    expect(screen.getByRole('button', { name: '添加附件' })).toBeDisabled()
  })

  it('keeps a single quantity even when capability advertises a larger batch', async () => {
    vi.mocked(designerApi.getCapabilities).mockResolvedValue({ items: [provider({ maxBatch: 3 })], defaultCapability: 'professional' })
    render(<DesignerPage />)
    fireEvent.click(await screen.findByRole('button', { name: '创作参数：专业增强 · 1:1 · 2K · 自动 · PNG · 1 张' }))
    expect(screen.queryByRole('button', { name: '增加图片数量' })).not.toBeInTheDocument()
  })

  it('uses selected project for generation and explicit project IDs for history', async () => {
    render(<DesignerPage />)
    const projectControl = await screen.findByRole('combobox', { name: '项目：未选择' })
    await waitFor(() => expect(projectControl).not.toBeDisabled())
    const promptAddon = projectControl.closest('[data-slot="input-group-addon"]')
    expect(promptAddon).not.toBeNull()
    const imageSettings = within(promptAddon as HTMLElement).getByRole('button', {
      name: '创作参数：专业增强 · 1:1 · 2K · 自动 · PNG · 1 张',
    })
    expect(projectControl.compareDocumentPosition(imageSettings) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    fireEvent.click(projectControl)
    fireEvent.click(await screen.findByRole('option', { name: /品牌项目/ }))
    expect(screen.getByRole('combobox', { name: '项目：品牌项目' })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '品牌图' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(
      expect.objectContaining({ project_id: 'project-1' }),
      expect.any(AbortSignal), expect.any(Function),
    ))

    fireEvent.click(screen.getAllByText('历史记录')[0])
    await waitFor(() => expect(api.designer.getHistory).toHaveBeenCalledWith({ project_id: 'project-1', page_size: 50 }))
  })

  it('requests history without a project filter when no project is selected', async () => {
    render(<DesignerPage />)
    fireEvent.click((await screen.findAllByText('历史记录'))[0])
    await waitFor(() => expect(api.designer.getHistory).toHaveBeenCalledWith({ project_id: undefined, page_size: 50 }))
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
      queryClient.setQueryData(['designer', 'capabilities'], {
        items: [provider({ maxReferenceImages: 1 })],
        defaultCapability: 'professional',
      })
    })

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      reference_file_ids: ['registered:upload:first.png'],
    }), expect.any(AbortSignal), expect.any(Function)))
  })

  it('uploads the edit mask to OSS and registers it before generating', async () => {
    vi.mocked(designerApi.uploadReferenceFromUrl).mockResolvedValue({ file_id: 'source-1', filename: 'source', size: 0 })
    render(<DesignerPage />)
    fireEvent.click(await screen.findByRole('button', { name: '测试编辑' }))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '改成红色' } })
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))

    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledWith(expect.objectContaining({
      purpose: 'designer_reference',
      file: expect.objectContaining({ name: 'mask.png' }),
      signal: expect.any(AbortSignal),
    })))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      project_id: '', reference_file_ids: ['source-1'], mask_file_id: 'registered:upload:mask.png',
    }), expect.any(AbortSignal), expect.any(Function)))
  })

  it('restores advanced history settings with visible retained inputs and sends them again', async () => {
    vi.mocked(designerApi.getCapabilities).mockResolvedValue({ items: [provider({ hasCompression: true, hasBackground: true, watermark: true, outputFormats: ['png', 'webp'] })], defaultCapability: 'professional' })
    const generation = {
      id: 'history-1', project_id: '', prompt: '历史提示词', capability_key: 'professional',
      quality: 'auto', size: '1:1:2K', output_format: 'webp', output_compression: 75,
      background: 'opaque', watermark: true, reference_file_ids: ['retained-reference'], mask_file_id: 'retained-mask',
      n: 1, status: 'completed', created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    } satisfies ImageGeneration
    vi.mocked(api.designer.getHistory).mockResolvedValue({ items: [generation], total: 1, page: 1, page_size: 50 })
    render(<DesignerPage />)
    fireEvent.click((await screen.findAllByText('历史记录'))[0])
    fireEvent.click(await screen.findByRole('button', { name: '重新生成' }))
    expect(screen.getByLabelText('Designer prompt')).toHaveValue('历史提示词')
    expect(screen.getByText('已保留历史参考图 1 张及蒙版')).toBeInTheDocument()
    expect(screen.queryByText('retained-mask')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledWith(expect.objectContaining({
      output_format: 'webp', output_compression: 75, background: 'opaque', watermark: true,
      reference_file_ids: ['retained-reference'], mask_file_id: 'retained-mask', n: 1,
    }), expect.any(AbortSignal), expect.any(Function)))
  })

  it('stops local waiting without clearing recovery or promising cancellation or refund', async () => {
    const view = render(<DesignerPage />)
    fireEvent.change(await screen.findByLabelText('Designer prompt'), { target: { value: 'accepted image' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(sessionStorage.getItem('designer_active_generation:user-1')).toContain('generation-1'))
    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
    expect(screen.getByText('已停止等待；已受理的任务仍会继续处理，可在历史记录或刷新后查看结果')).toBeInTheDocument()
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toContain('generation-1')
    view.unmount()
    vi.mocked(designerApi.getGeneration).mockResolvedValue({ id: 'generation-1', project_id: '', prompt: 'accepted image', n: 1, status: 'generating', created_at: '', updated_at: '' })
    render(<DesignerPage />)
    await waitFor(() => expect(designerApi.getGeneration).toHaveBeenCalledWith('generation-1', expect.any(AbortSignal)))
  })

  it('stops local waiting from the shared composer', async () => {
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '生成后取消' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '停止等待' })).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))

    await waitFor(() => expect(screen.queryByRole('button', { name: '停止等待' })).not.toBeInTheDocument())
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

    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '提交 B' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))

    registration.resolve({ file_id: 'late-reference', filename: 'late.png', size: 5 })
    await act(async () => {})
    expect(designerApi.generate).toHaveBeenCalledTimes(1)
  })

  it('keeps a newer prompt and does not begin polling when canceled generate resolves late', async () => {
	const generation = deferred<{ generation_id: string; status: string; price_credits: number }>()
    vi.mocked(designerApi.generate).mockReturnValueOnce(generation.promise)
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '提交 A' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '用户继续输入 B' } })
    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
	generation.resolve({ generation_id: 'late-generation', status: 'generating', price_credits: 500 })
    await act(async () => {})

    expect(screen.getByLabelText('Designer prompt')).toHaveValue('用户继续输入 B')
    expect(designerApi.getGeneration).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toContain('late-generation')
    expect(screen.queryByRole('button', { name: '停止等待' })).not.toBeInTheDocument()
  })

  it('aborts the dispatched generation request and never starts polling after cancellation', async () => {
    let dispatchedSignal: AbortSignal | undefined
    vi.mocked(designerApi.generate).mockImplementationOnce((_request, signal) => {
      dispatchedSignal = signal
      return new Promise((_resolve, reject) => {
        signal?.addEventListener('abort', () => reject(new DOMException('canceled', 'AbortError')), { once: true })
      })
    })
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '真实取消' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))

    expect(dispatchedSignal).toBeInstanceOf(AbortSignal)
    expect(dispatchedSignal?.aborted).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))

    await waitFor(() => expect(dispatchedSignal?.aborted).toBe(true))
    expect(designerApi.getGeneration).not.toHaveBeenCalled()
    expect(toast.error).not.toHaveBeenCalled()
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

    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
    poll.resolve({
      id: 'late-generation', user_id: 'user-1', project_id: '', prompt: '等待轮询',
      capability_key: 'professional', capability_name: '专业增强', n: 1, status: 'completed',
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      results: [{ id: 1, generation_id: 'late-generation', image_url: 'https://example.com/late.png', index: 0 }],
    })
    await act(async () => {})

    expect(toast.success).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: '停止等待' })).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '新的提交' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await act(async () => {})
    expect(designerApi.generate).toHaveBeenCalledTimes(2)
  })

  it('serializes polling and emits one terminal success notification', async () => {
    const firstPoll = deferred<ImageGeneration>()
    const completed = {
      id: 'generation-1', user_id: 'user-1', project_id: '', prompt: '串行轮询',
      capability_key: 'professional', capability_name: '专业增强', n: 1, status: 'completed' as const,
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
      results: [{ id: 1, generation_id: 'generation-1', image_url: 'https://example.com/final.png', index: 0 }],
    }
    vi.mocked(designerApi.getGeneration)
      .mockReturnValueOnce(firstPoll.promise)
      .mockResolvedValue(completed)
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    vi.useFakeTimers()
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '串行轮询' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })

    expect(designerApi.getGeneration).toHaveBeenCalledTimes(1)
    expect(toast.success).not.toHaveBeenCalled()

    firstPoll.resolve({ ...completed, status: 'generating', results: undefined })
    await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(designerApi.getGeneration).toHaveBeenCalledTimes(2)
    expect(toast.success).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(designerApi.getGeneration).toHaveBeenCalledTimes(2)
    expect(toast.success).toHaveBeenCalledTimes(1)
  })

  it('finalizes a failed generation exactly once', async () => {
    vi.mocked(designerApi.getGeneration).mockResolvedValue({
      id: 'generation-1', user_id: 'user-1', project_id: '', prompt: '失败轮询',
      capability_key: 'professional', capability_name: '专业增强', n: 1, status: 'failed', error: '生成失败',
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    vi.useFakeTimers()
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '失败轮询' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })

    expect(designerApi.getGeneration).toHaveBeenCalledTimes(1)
    expect(toast.error).toHaveBeenCalledTimes(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(designerApi.getGeneration).toHaveBeenCalledTimes(1)
    expect(toast.error).toHaveBeenCalledTimes(1)
  })

  it('continues polling beyond the former six-minute cap', async () => {
    vi.mocked(designerApi.getGeneration).mockResolvedValue({
      id: 'generation-1', user_id: 'user-1', project_id: '', prompt: '超时轮询',
      capability_key: 'professional', capability_name: '专业增强', n: 1, status: 'generating',
      created_at: new Date().toISOString(), updated_at: new Date().toISOString(),
    })
    render(<DesignerPage />)
    await screen.findByLabelText('Designer prompt')
    vi.useFakeTimers()
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '超时轮询' } })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    await act(async () => {})
    await act(async () => { await vi.advanceTimersByTimeAsync(360_000) })

    expect(toast.error).not.toHaveBeenCalled()
    const pollCount = vi.mocked(designerApi.getGeneration).mock.calls.length
    await act(async () => { await vi.advanceTimersByTimeAsync(10_000) })
    expect(designerApi.getGeneration).toHaveBeenCalledTimes(pollCount + 5)
    expect(toast.error).not.toHaveBeenCalled()
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

    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    expect(screen.queryByText('提交失败，请重试')).not.toBeInTheDocument()

    mask.resolve(new File(['mask'], 'mask.png', { type: 'image/png' }))
    await act(async () => {})
    expect(designerApi.uploadReferenceFromUrl).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toBeNull()
  })

  it('aborts the edit mask OSS upload when generation is canceled', async () => {
    const maskUpload = deferred<ReturnType<typeof uploadResult>>()
    let uploadSignal: AbortSignal | undefined
    vi.mocked(uploadToOSS).mockImplementationOnce(({ signal }) => {
      uploadSignal = signal
      return maskUpload.promise
    })
    vi.mocked(designerApi.uploadReferenceFromUrl).mockResolvedValue({
      file_id: 'source-1', filename: 'source', size: 0,
    })
    render(<DesignerPage />)
    fireEvent.click(await screen.findByRole('button', { name: '测试编辑' }))
    fireEvent.change(screen.getByLabelText('Designer prompt'), { target: { value: '取消上传' } })
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))
    await waitFor(() => expect(uploadToOSS).toHaveBeenCalledTimes(1))
    expect(uploadSignal?.aborted).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: '停止等待' }))
    expect(uploadSignal?.aborted).toBe(true)
    maskUpload.resolve(uploadResult(new File(['mask'], 'mask.png', { type: 'image/png' })))
    await act(async () => {})

    expect(designerApi.registerReference).not.toHaveBeenCalled()
    expect(designerApi.generate).not.toHaveBeenCalled()
    expect(sessionStorage.getItem('designer_active_generation:user-1')).toBeNull()
  })
})
