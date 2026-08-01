import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createElement } from 'react'
import { fireEvent, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import DesignerPage from './DesignerPage'
import { designerApi } from '@/lib/api/designer'
import { render } from '@/test/test-utils'
import type { GenerateRequest } from '@/types/designer'

const root = resolve(import.meta.dirname, '../..')

function read(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

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

describe('Designer provider contract', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    window.sessionStorage.clear()
    vi.mocked(designerApi.getProviders).mockResolvedValue([
      {
        id: 'professional_enhance',
        name: '专业增强',
        description: '适合复杂构图与高细节视觉任务',
        minTier: 'enterprise',
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
          pricingType: 'fixed_sku',
          currency: 'credits',
          billingNote: 'fixed retail SKU',
        },
      },
    ])
    vi.mocked(designerApi.generate).mockResolvedValue({
      generation_id: 'generation-1',
      status: 'generating',
      price_credits: 500,
    })
  })

  it('sends provider_id with designer generation requests', () => {
    const types = read('src/types/designer.ts')
    expect(types).toContain('provider_id: string')
    expect(types).not.toContain('provider?: string')
    expect(types).not.toContain('model?: string')

    const page = read('src/pages/DesignerPage.tsx')
    expect(page).toContain('provider_id: effectiveProvider.id')
  })

  it('floats the prompt bar inside the designer canvas frame', () => {
    const page = read('src/pages/DesignerPage.tsx')

    expect(page).toContain('<AgentPromptInput')
    expect(page).toContain('pointer-events-none absolute inset-x-4 bottom-4')
    expect(page).toContain('pointer-events-auto w-full')
    expect(page).toContain('pb-52')
    expect(page).toContain('md:pb-56')
  })

  it('caps provider reference capacity at the shared five-file limit', () => {
    const page = read('src/pages/DesignerPage.tsx')

    expect(page).toContain('Math.min(maxReferenceImages, GENERAL_AGENT_ATTACHMENT_POLICY.maxCount)')
  })

  it('does not infer GPT Image sizes in the Studio API client', () => {
    const apiClient = read('src/lib/api/designer.ts')

    expect(apiClient).not.toContain('gpt-image')
    expect(apiClient).not.toContain('chatgpt-image')
    expect(apiClient).not.toContain('GPT_IMAGE_SIZE_PRESETS')
  })

  it('uses the configured capability default size when generating', async () => {
    render(createElement(DesignerPage))

    await screen.findAllByText('专业增强')
    expect(await screen.findByRole('button', { name: /自动\s*智能选择/ })).toBeInTheDocument()
    const prompt = await screen.findByPlaceholderText('描述你想要生成的图片...')
    fireEvent.change(prompt, {
      target: { value: '给下周奶做一张竖版海报' },
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    const request = vi.mocked(designerApi.generate).mock.calls[0][0] as GenerateRequest
    expect(request.provider_id).toBe('professional_enhance')
    expect(request.size).toBe('auto')
  })

  it('renders ratio presets from configured Seedream capabilities without custom pixel controls', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      {
        id: 'standard_image',
        name: '标准图像',
        description: '适合日常内容配图和常规视觉创作',
        minTier: 'free',
        credits: 50,
        enabled: true,
        idx: 0,
        capabilities: {
          qualityLevels: [],
          sizePresets: ['1:1', '16:9', '9:16', '4:3', '3:4', '3:2', '2:3', '21:9'],
          defaultSize: '1:1',
          maxBatch: 1,
          maxReferenceImages: 10,
          supportsReference: true,
          supportsMask: false,
          outputFormats: ['png', 'jpeg'],
          hasBackground: false,
          hasCompression: false,
          watermark: true,
        },
        pricing: { pricingType: 'fixed_sku', currency: 'credits', billingNote: 'fixed retail SKU' },
      },
    ])

    render(createElement(DesignerPage))

    await screen.findAllByText('标准图像')
    expect(screen.getAllByRole('button', { name: /21:9/ }).length).toBeGreaterThan(0)
    expect(screen.getByText('分辨率')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /自定义\s*W×H/ })).not.toBeInTheDocument()
  })
})
