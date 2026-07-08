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

  it('uses the provider default size when generating with GPT Image', async () => {
    render(createElement(DesignerPage))

    await screen.findAllByText('GPT Image 2')
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
})
