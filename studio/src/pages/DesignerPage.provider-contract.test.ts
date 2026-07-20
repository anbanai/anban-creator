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
          pricingType: 'openai_image_usage',
          requiresUsage: true,
        },
      },
    ])
    vi.mocked(designerApi.generate).mockResolvedValue({
      generation_id: 'generation-1',
      status: 'generating',
      billing_mode: 'openai_image_usage',
    })
  })

  it('sends provider_id with designer generation requests', () => {
    expect(read('src/types/designer.ts')).toContain('provider_id?: string')

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

  it('uses the provider default size when generating with GPT Image', async () => {
    render(createElement(DesignerPage))

    await screen.findAllByText('GPT Image 2')
    expect(await screen.findByRole('button', { name: /自动\s*智能选择/ })).toBeInTheDocument()
    const prompt = await screen.findByPlaceholderText('描述你想要生成的图片...')
    fireEvent.change(prompt, {
      target: { value: '给下周奶做一张竖版海报' },
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    const request = vi.mocked(designerApi.generate).mock.calls[0][0] as GenerateRequest
    expect(request.provider_id).toBe('gpt_image_2')
    expect(request.size).toBe('auto')
  })

  it('renders ratio presets from configured Seedream capabilities without custom pixel controls', async () => {
    vi.mocked(designerApi.getProviders).mockResolvedValueOnce([
      {
        id: 'seedream',
        name: '豆包 Seedream',
        provider: 'volcengine',
        providerKey: 'volcengine_ark',
        route: 'image_generation.designer.seedream',
        model: 'doubao-seedream-5-0-pro-260628',
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
        pricing: {},
      },
    ])

    render(createElement(DesignerPage))

    await screen.findAllByText('豆包 Seedream')
    expect(screen.getAllByRole('button', { name: /21:9/ }).length).toBeGreaterThan(0)
    expect(screen.getByText('分辨率')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /自定义\s*W×H/ })).not.toBeInTheDocument()
  })
})
