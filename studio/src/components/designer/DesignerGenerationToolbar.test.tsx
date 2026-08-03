import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { DesignerCapability, DesignerSettings } from '@/types/designer'
import { DesignerGenerationToolbar } from './DesignerGenerationToolbar'

const capabilities: DesignerCapability[] = [
  {
    id: 'professional',
    name: '专业增强',
    credits: 500,
    priceAvailable: true,
    enabled: true,
    idx: 0,
    designerFeatures: {
      qualityLevels: ['auto', 'high'],
      sizePresets: ['1:1:2K', '3:4:2K', '4:3:2K'],
      defaultSize: '1:1:2K',
      maxBatch: 3,
      maxReferenceImages: 0,
      supportsReference: false,
      supportsMask: false,
      outputFormats: ['png', 'webp'],
      hasBackground: false,
      hasCompression: false,
      watermark: false,
    },
  },
]

const settings: DesignerSettings = {
  quality: 'auto',
  size: '1:1:2K',
  n: 1,
  outputFormat: 'png',
  compression: 100,
  background: 'auto',
  watermark: false,
}

describe('DesignerGenerationToolbar', () => {
  it('renders a compact summary and fixed specifications from designer_features', () => {
    render(
      <DesignerGenerationToolbar
        capabilities={capabilities}
        capabilityKey="professional"
        settings={settings}
        onCapabilityChange={vi.fn()}
        onSettingsChange={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '图像设置：专业增强 · 1:1 · 2K · 自动 · PNG · 1 张' }))

    expect(within(screen.getByRole('group', { name: '尺寸' })).getAllByRole('button').map((button) => button.textContent)).toEqual([
      '1:1 · 2K', '3:4 · 2K', '4:3 · 2K',
    ])
    expect(within(screen.getByRole('group', { name: '质量' })).getAllByRole('button').map((button) => button.textContent)).toEqual(['自动', '高'])
    expect(within(screen.getByRole('group', { name: '输出格式' })).getAllByRole('button').map((button) => button.textContent)).toEqual(['PNG', 'WEBP'])
    expect(screen.queryByText('1:1')).not.toBeInTheDocument()
  })

  it('reports capability_key, size, quality, output_format, and capability-driven n changes', () => {
    const onCapabilityChange = vi.fn()
    const onSettingsChange = vi.fn()
    render(
      <DesignerGenerationToolbar
        capabilities={capabilities}
        capabilityKey="professional"
        settings={settings}
        onCapabilityChange={onCapabilityChange}
        onSettingsChange={onSettingsChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '图像设置：专业增强 · 1:1 · 2K · 自动 · PNG · 1 张' }))
    fireEvent.click(within(screen.getByRole('group', { name: '尺寸' })).getByRole('button', { name: '3:4 · 2K' }))
    fireEvent.click(within(screen.getByRole('group', { name: '质量' })).getByRole('button', { name: '高' }))
    fireEvent.click(within(screen.getByRole('group', { name: '输出格式' })).getByRole('button', { name: 'WEBP' }))

    expect(onSettingsChange).toHaveBeenCalledWith({ size: '3:4:2K' })
    expect(onSettingsChange).toHaveBeenCalledWith({ quality: 'high' })
    expect(onSettingsChange).toHaveBeenCalledWith({ outputFormat: 'webp' })
    fireEvent.click(screen.getByRole('button', { name: '增加图片数量' }))
    expect(onSettingsChange).toHaveBeenCalledWith({ n: 2 })
  })

  it('shows a static image quantity when the current capability only supports one image', () => {
    const singleImageCapabilities = [{
      ...capabilities[0],
      designerFeatures: { ...capabilities[0].designerFeatures, maxBatch: 1 },
    }]
    render(
      <DesignerGenerationToolbar
        capabilities={singleImageCapabilities}
        capabilityKey="professional"
        settings={{ ...settings, n: 3 }}
        onCapabilityChange={vi.fn()}
        onSettingsChange={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '图像设置：专业增强 · 1:1 · 2K · 自动 · PNG · 1 张' }))

    expect(screen.getByText('图片数量 1 · 当前能力上限')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '增加图片数量' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '减少图片数量' })).not.toBeInTheDocument()
  })

  it('constrains the fixed-specification popover to the available viewport height', () => {
    render(
      <DesignerGenerationToolbar
        capabilities={capabilities}
        capabilityKey="professional"
        settings={settings}
        onCapabilityChange={vi.fn()}
        onSettingsChange={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '图像设置：专业增强 · 1:1 · 2K · 自动 · PNG · 1 张' }))

    expect(screen.getByRole('dialog')).toHaveClass('max-h-[var(--available-height)]', 'overflow-y-auto')
  })
})
