import { createElement, type ReactNode } from 'react'
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { fireEvent, render as testingRender, screen, waitFor } from '@testing-library/react'
import { BrowserRouter } from 'react-router-dom'
import { ThemeProvider } from 'next-themes'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerPage from './DesignerPage'
import { AgentPromptDropProvider } from '@/components/agent-prompt/AgentPromptDropProvider'
import { api } from '@/lib/api'
import { designerApi } from '@/lib/api/designer'
import { createTestQueryClient, render } from '@/test/test-utils'

const navigateMock = vi.fn()
const toast = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}))
const uploadToOSSMock = vi.hoisted(() => vi.fn())

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => navigateMock }
})

vi.mock('sonner', () => ({ toast }))

vi.mock('@/lib/direct-upload', async () => {
  const actual = await vi.importActual<typeof import('@/lib/direct-upload')>('@/lib/direct-upload')
  return { ...actual, uploadToOSS: uploadToOSSMock }
})

vi.mock('@/components/designer/InlineMaskEditor', async () => {
  const React = await vi.importActual<typeof import('react')>('react')
  return {
    default: React.forwardRef((_props, ref) => {
      React.useImperativeHandle(ref, () => ({
        exportMask: async () => new File(['mask'], 'mask.png', { type: 'image/png' }),
      }))
      return <div data-testid="inline-mask-editor" />
    }),
  }
})

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    api: {
      ...actual.api,
      projects: {
        ...actual.api.projects,
        list: vi.fn().mockResolvedValue([]),
      },
      billing: {
        ...actual.api.billing,
        wallet: vi.fn(),
      },
    },
  }
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

const provider = {
  id: 'gpt_image_2',
  name: 'GPT Image 2',
  provider: 'openai',
  providerKey: 'wangcai_openai',
  route: 'image_generation.designer.gpt_image_2',
  model: 'gpt-image-2',
  credits: 500,
  enabled: true,
  idx: 0,
  capabilities: {
    qualityLevels: ['auto', 'low', 'medium', 'high'],
    sizePresets: ['auto', '1024x1024', '1536x1024', '1024x1536'],
    defaultSize: 'auto',
    maxBatch: 10,
    maxReferenceImages: 16,
    supportsReference: true,
    supportsMask: true,
    outputFormats: ['png', 'jpeg', 'webp'],
    hasBackground: true,
    hasCompression: true,
    watermark: false,
  },
  pricing: {
    pricingType: 'fixed_sku' as const,
    currency: 'credits' as const,
    billingNote: 'fixed retail SKU',
  },
}

function renderWithQueryClient(queryClient: QueryClient) {
  return testingRender(<DesignerPage />, {
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
}

describe('Designer billing guidance', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    window.sessionStorage.clear()
    vi.mocked(designerApi.getProviders).mockResolvedValue([provider])
    vi.mocked(designerApi.generate).mockResolvedValue({
      generation_id: 'generation-1',
      status: 'generating',
      price_credits: provider.credits,
    })
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 1000, promotional: 0, debt: 0, balance: 1000 })
    vi.mocked(designerApi.uploadReferenceFromUrl).mockResolvedValue({
      file_id: 'source-file',
      filename: 'source.png',
      size: 5,
    })
    vi.mocked(designerApi.registerReference).mockResolvedValue({
      file_id: 'mask-file',
      filename: 'mask.png',
      size: 4,
    })
    uploadToOSSMock.mockResolvedValue({
      uploadId: 'mask-upload',
      key: 'uploads/pending/user/mask.png',
      publicUrl: 'https://cdn.example/mask.png',
      contentType: 'image/png',
      size: 4,
    })
  })

  it('does not expose Agent execution profiles for image operations', async () => {
    render(createElement(DesignerPage))

    await screen.findAllByText('GPT Image 2')
    expect(screen.queryByRole('group', { name: 'Agent 执行配置' })).not.toBeInTheDocument()
  })

  it('shows price and balance and disables generation when credits are insufficient', async () => {
    vi.mocked(api.billing.wallet).mockResolvedValue({ paid: 100, promotional: 0, debt: 0, balance: 100 })

    render(createElement(DesignerPage))

    expect(await screen.findByText('500 积分 · 余额 100')).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), {
      target: { value: '生成海报' },
    })

    expect(screen.getByRole('button', { name: '生成' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '去充值' }))
    expect(navigateMock).toHaveBeenCalledWith('/billing')
  })

  it('does not disable generation when wallet loading fails', async () => {
    vi.mocked(api.billing.wallet).mockRejectedValue(new Error('network'))

    render(createElement(DesignerPage))

    await screen.findAllByText('GPT Image 2')
    fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), {
      target: { value: '生成海报' },
    })

    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    expect(screen.queryByText(/余额 0/)).not.toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('does not show a stale wallet balance after a background refresh fails', async () => {
    const queryClient = createTestQueryClient()
    queryClient.setQueryData(['billing', 'wallet'], { paid: 1000, promotional: 0, debt: 0, balance: 1000 })
    vi.mocked(api.billing.wallet).mockRejectedValue(new Error('network'))

    renderWithQueryClient(queryClient)

    await screen.findAllByText('GPT Image 2')
    await waitFor(() => expect(queryClient.getQueryState(['billing', 'wallet'])?.status).toBe('error'))
    expect(screen.queryByText('500 积分 · 余额 1,000')).not.toBeInTheDocument()
    expect(screen.getByText('500 积分')).toBeInTheDocument()
  })

  it('refreshes the wallet after a normal generation is accepted', async () => {
    render(createElement(DesignerPage))

    await screen.findAllByText('GPT Image 2')
    fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), {
      target: { value: '生成海报' },
    })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(api.billing.wallet).toHaveBeenCalledTimes(2))
  })

  it('refreshes the wallet after an edit generation is accepted', async () => {
    window.sessionStorage.setItem('designer_active_generation', JSON.stringify({
      generationId: 'completed-generation',
      startedAt: Date.now(),
    }))
    vi.mocked(designerApi.getGeneration).mockResolvedValue({
      id: 'completed-generation',
      user_id: 'user-1',
      project_id: 'default',
      prompt: '原图',
      provider: 'openai',
      model: 'gpt-image-2',
      n: 1,
      status: 'completed',
      created_at: '2026-07-24T00:00:00Z',
      updated_at: '2026-07-24T00:01:00Z',
      results: [{
        id: 1,
        generation_id: 'completed-generation',
        image_url: 'data:image/png;base64,aW1hZ2U=',
        index: 0,
      }],
    })

    render(createElement(DesignerPage))

    await screen.findByAltText('生成图片 1')
    fireEvent.click(screen.getByTitle('局部编辑'))
    expect(await screen.findByTestId('inline-mask-editor')).toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText('描述你想修改的区域...'), {
      target: { value: '改成蓝色' },
    })
    fireEvent.click(screen.getByRole('button', { name: '编辑' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(api.billing.wallet).toHaveBeenCalledTimes(2))
  })

  it('offers billing navigation when the server rejects for insufficient credits', async () => {
    vi.mocked(designerApi.generate).mockRejectedValue({
      response: { data: { code: 40203, msg: 'billing_insufficient_for_standalone_operation' } },
    })

    render(createElement(DesignerPage))

    await screen.findAllByText('GPT Image 2')
    fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), {
      target: { value: '生成海报' },
    })
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(
      '积分余额不足，请先充值后继续',
      expect.objectContaining({ action: expect.objectContaining({ label: '去充值' }) }),
    ))
    const options = toast.error.mock.calls[0]?.[1] as { action?: { onClick?: () => void } }
    options.action?.onClick?.()
    expect(navigateMock).toHaveBeenCalledWith('/billing')
    await waitFor(() => expect(api.billing.wallet).toHaveBeenCalledTimes(2))
  })
})
