import { createElement } from 'react'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerPage from './DesignerPage'
import { api } from '@/lib/api'
import { designerApi } from '@/lib/api/designer'
import { render } from '@/test/test-utils'

const navigateMock = vi.fn()
const toast = vi.hoisted(() => ({
  error: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
}))

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => navigateMock }
})

vi.mock('sonner', () => ({ toast }))

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
  })
})
