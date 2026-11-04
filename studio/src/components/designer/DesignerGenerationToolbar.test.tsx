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
      sizePresets: ['1:1:2K', '3:4:2K', '4:3:2K', '9:16:2K'],
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
  it('shows standard and enhanced choices with only their names and server prices', () => {
    const onCapabilityChange = vi.fn()
    render(
      <DesignerGenerationToolbar
        capabilities={[
          { ...capabilities[0], id: 'standard', name: '标准图像', credits: 400, description: '适合日常视觉与产品图片' },
          { ...capabilities[0], description: '适合复杂构图与高细节视觉任务' },
        ]}
        capabilityKey="standard"
        settings={settings}
        onCapabilityChange={onCapabilityChange}
        onSettingsChange={vi.fn()}
      />,
    )

    const choices = within(screen.getByRole('group', { name: '图像能力' }))
    expect(choices.getByRole('button', { name: /^标准\s*400 积分\/张$/ })).toHaveAttribute('aria-pressed', 'true')
    fireEvent.click(choices.getByRole('button', { name: /^增强\s*500 积分\/张$/ }))
    expect(onCapabilityChange).toHaveBeenCalledWith('professional')
    expect(screen.queryByText(/适合/)).not.toBeInTheDocument()
  })

  it('shows generation controls immediately without opening a popover', () => {
    render(
      <DesignerGenerationToolbar
        capabilities={capabilities}
        capabilityKey="professional"
        settings={settings}
        onCapabilityChange={vi.fn()}
        onSettingsChange={vi.fn()}
      />,
    )

    const parameters = screen.getByRole('group', { name: '创作参数' })
    expect(within(parameters).getByRole('group', { name: '图像能力' })).toBeVisible()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(within(screen.getByRole('group', { name: '尺寸' })).getAllByRole('button').map((button) => button.getAttribute('aria-label'))).toEqual([
      '1:1 · 2K', '3:4 · 2K', '4:3 · 2K', '9:16 · 2K',
    ])
    expect(within(screen.getByRole('group', { name: '质量' })).getAllByRole('button').map((button) => button.textContent)).toEqual(['自动', '高'])
    expect(within(screen.getByRole('group', { name: '输出格式' })).getAllByRole('button').map((button) => button.textContent)).toEqual(['PNG', 'WEBP'])
    expect(screen.getByText('1:1')).toBeVisible()
  })

  it('reports supported size, quality and output format while keeping quantity fixed', () => {
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

    fireEvent.click(within(screen.getByRole('group', { name: '尺寸' })).getByRole('button', { name: '3:4 · 2K' }))
    fireEvent.click(within(screen.getByRole('group', { name: '尺寸' })).getByRole('button', { name: '9:16 · 2K' }))
    fireEvent.click(within(screen.getByRole('group', { name: '质量' })).getByRole('button', { name: '高' }))
    fireEvent.click(within(screen.getByRole('group', { name: '输出格式' })).getByRole('button', { name: 'WEBP' }))

    expect(onSettingsChange).toHaveBeenCalledWith({ size: '3:4:2K' })
    expect(onSettingsChange).toHaveBeenCalledWith({ size: '9:16:2K' })
    expect(onSettingsChange).toHaveBeenCalledWith({ quality: 'high' })
    expect(onSettingsChange).toHaveBeenCalledWith({ outputFormat: 'webp' })
    expect(screen.queryByRole('button', { name: '增加图片数量' })).not.toBeInTheDocument()
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

    const fixedQuantity = screen.getByLabelText('图片数量：1，当前能力上限')
    expect(fixedQuantity).toHaveTextContent('1 张')
    expect(screen.queryByRole('button', { name: '增加图片数量' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '减少图片数量' })).not.toBeInTheDocument()
  })

  it('disables the expanded controls while generation is in progress', () => {
    render(
      <DesignerGenerationToolbar
        capabilities={capabilities}
        capabilityKey="professional"
        settings={settings}
        onCapabilityChange={vi.fn()}
        onSettingsChange={vi.fn()}
        disabled
      />,
    )

    for (const button of within(screen.getByRole('group', { name: '创作参数' })).getAllByRole('button')) {
      expect(button).toBeDisabled()
    }
  })
})
