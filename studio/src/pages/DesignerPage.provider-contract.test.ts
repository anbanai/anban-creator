import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createElement } from 'react'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
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
    getCapabilities: vi.fn(),
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
    vi.mocked(designerApi.getCapabilities).mockResolvedValue({ items: [
      {
        id: 'professional',
        name: '专业增强',
        description: '适合复杂构图与高细节视觉任务',
        minTier: 'enterprise',
        credits: 500,
        priceAvailable: true,
        enabled: true,
        idx: 0,
        designerFeatures: {
          qualityLevels: ['auto', 'low', 'medium', 'high'],
          sizePresets: ['1:1:2K', '3:4:2K', '4:3:2K'],
          defaultSize: '1:1:2K',
          maxBatch: 1,
          maxReferenceImages: 16,
          supportsReference: true,
          supportsMask: true,
          outputFormats: ['png', 'jpeg', 'webp'],
          hasBackground: true,
          hasCompression: true,
          watermark: false,
        },
      },
    ], defaultCapability: 'professional' })
    vi.mocked(designerApi.generate).mockResolvedValue({
      generation_id: 'generation-1',
      status: 'generating',
      price_credits: 500,
    })
  })

  it('sends capability_key with designer generation requests', () => {
    const types = read('src/types/designer.ts')
    const requestType = types.match(/export interface GenerateRequest \{[\s\S]*?\n\}/)?.[0] ?? ''
    expect(requestType).toContain('capability_key: string')
    expect(requestType).toContain('quality: string')
    expect(requestType).toContain('size: string')
    expect(requestType).toContain('n: number')
    expect(requestType).toContain('output_format: string')
    expect(requestType).not.toMatch(/^\s*(?:quality|size|n|output_format)\?:/m)
    expect(types).not.toContain('provider?: string')
    expect(types).not.toContain('model?: string')
    expect(types).not.toContain('resolution: string')

    const page = read('src/pages/DesignerPage.tsx')
    expect(page).toContain('capability_key: effectiveCapability.id')
    expect(page).not.toContain('buildDesignerRequestSize')
    expect(page).toContain('size: settings.size')
  })

  it('floats the prompt bar inside the designer canvas frame', () => {
    const page = read('src/pages/DesignerPage.tsx')

    expect(page).toContain('<AgentPromptInput')
    expect(page).toContain('pointer-events-none absolute inset-x-4 bottom-4')
    expect(page).toContain('pointer-events-auto w-full')
    expect(page).toContain('pb-52')
    expect(page).toContain('md:pb-56')
  })

  it('places project selection before image settings in the prompt bottom toolbar', () => {
    const page = read('src/pages/DesignerPage.tsx')

    expect(page).not.toContain('contextBar=')
    expect(page).toMatch(
      /leadingTools=\{\(\s*<div[^>]*>\s*\{projectControl\}[\s\S]*?<DesignerGenerationToolbar/,
    )
  })

  it('caps provider reference capacity at the shared five-file limit', () => {
    const page = read('src/pages/DesignerPage.tsx')

    expect(page).toContain('Math.min(maxReferenceImages, GENERAL_AGENT_ATTACHMENT_POLICY.maxCount)')
  })

  it('does not infer GPT Image sizes in the Studio API client', () => {
    const apiClient = read('src/lib/api/designer.ts')

    expect(apiClient).not.toContain('professional')
    expect(apiClient).not.toContain('chatprofessional')
    expect(apiClient).not.toContain('GPT_IMAGE_SIZE_PRESETS')
  })

  it('uses the configured capability default size when generating', async () => {
    render(createElement(DesignerPage))

    expect(await screen.findByRole('button', { name: '创作设置：专业增强 · 1:1 · 2K · 自动 · PNG · 1 张' })).toBeInTheDocument()
    const prompt = await screen.findByPlaceholderText('描述你想要生成的图片...')
    fireEvent.change(prompt, {
      target: { value: '给下周奶做一张竖版海报' },
    })
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalledTimes(1))
    const request = vi.mocked(designerApi.generate).mock.calls[0][0] as GenerateRequest
    expect(request.capability_key).toBe('professional')
    expect(request.size).toBe('1:1:2K')
    expect(request.quality).toBe('auto')
    expect(request.output_format).toBe('png')
    expect(request.n).toBe(1)
  })

  it('renders ratio presets from configured Seedream capabilities without custom pixel controls', async () => {
    vi.mocked(designerApi.getCapabilities).mockResolvedValueOnce({ items: [
      {
        id: 'standard',
        name: '标准图像',
        description: '适合日常内容配图和常规视觉创作',
        minTier: 'free',
        credits: 50,
        priceAvailable: true,
        enabled: true,
        idx: 0,
        designerFeatures: {
          qualityLevels: [],
          sizePresets: ['1:1:2K', '3:4:2K', '4:3:2K', '16:9:2K'],
          defaultSize: '1:1:2K',
          maxBatch: 1,
          maxReferenceImages: 10,
          supportsReference: true,
          supportsMask: false,
          outputFormats: ['png', 'jpeg'],
          hasBackground: false,
          hasCompression: false,
          watermark: true,
        },
      },
    ], defaultCapability: 'standard' })

    render(createElement(DesignerPage))

    const imageSettings = await screen.findByRole('button', { name: '创作设置：标准图像 · 1:1 · 2K · PNG · 1 张' })
    fireEvent.click(imageSettings)
    expect(within(screen.getByRole('group', { name: '尺寸' })).getByRole('button', { name: '16:9 · 2K' })).toBeInTheDocument()
    expect(screen.queryByText('分辨率')).not.toBeInTheDocument()
  })

  it('uses the configured default capability instead of the first sorted option', async () => {
    const professional = (await designerApi.getCapabilities()).items[0]
    vi.mocked(designerApi.getCapabilities).mockResolvedValueOnce({
      defaultCapability: 'standard',
      items: [
        { ...professional, id: 'professional', name: '专业增强', idx: 1 },
        { ...professional, id: 'standard', name: '标准图像', idx: 2 },
      ],
    })
    render(createElement(DesignerPage))

    await screen.findByRole('button', { name: '创作设置：标准图像 · 1:1 · 2K · 自动 · PNG · 1 张' })
    fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), { target: { value: '生成海报' } })
    await waitFor(() => expect(screen.getByRole('button', { name: '生成' })).not.toBeDisabled())
    fireEvent.click(screen.getByRole('button', { name: '生成' }))

    await waitFor(() => expect(designerApi.generate).toHaveBeenCalled())
    expect(vi.mocked(designerApi.generate).mock.calls[0][0].capability_key).toBe('standard')
  })

  it('fails closed when capability pricing availability is missing', async () => {
    const professional = (await designerApi.getCapabilities()).items[0]
    vi.mocked(designerApi.getCapabilities).mockResolvedValueOnce({
      defaultCapability: 'professional',
      items: [{ ...professional, priceAvailable: undefined }],
    })
    render(createElement(DesignerPage))

    expect((await screen.findAllByText('暂无可用图像能力')).length).toBeGreaterThan(0)
    fireEvent.change(screen.getByPlaceholderText('描述你想要生成的图片...'), { target: { value: '生成海报' } })
    expect(screen.getByRole('button', { name: '生成' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: '生成' }))
    expect(designerApi.generate).not.toHaveBeenCalled()
  })
})
