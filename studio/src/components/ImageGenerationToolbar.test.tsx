import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ImageGenerationSettings } from './ImageGenerationToolbar'

const capabilities = [
  {
    key: 'standard', display_name: '标准图像', description: '适合日常配图',
    enabled: true, price_available: true, price_credits: 500,
  },
  {
    key: 'professional', display_name: '专业增强', description: '适合复杂构图',
    enabled: true, price_available: true, price_credits: 800,
  },
]

describe('ImageGenerationSettings', () => {
  it('shows only the current business ratios and capability details', () => {
    render(
      <ImageGenerationSettings
        ratios={['3:4', '1:1', '4:3']}
        ratio="3:4"
        onRatioChange={vi.fn()}
        capabilities={capabilities}
        capabilityKey="professional"
        onCapabilityChange={vi.fn()}
      />,
    )

    expect(screen.getByText('智能适配')).toBeInTheDocument()
    expect(screen.getByText('3:4')).toBeInTheDocument()
    expect(screen.getByText('1:1')).toBeInTheDocument()
    expect(screen.getByText('4:3')).toBeInTheDocument()
    expect(screen.queryByText('16:9')).not.toBeInTheDocument()
    expect(screen.getByText('适合复杂构图')).toBeInTheDocument()
    expect(screen.getByText('每张 800 积分')).toBeInTheDocument()
  })

  it('reports explicit auto and capability selections', () => {
    const onRatioChange = vi.fn()
    const onCapabilityChange = vi.fn()
    render(
      <ImageGenerationSettings
        ratios={['16:9', '4:3', '1:1']}
        ratio="16:9"
        onRatioChange={onRatioChange}
        capabilities={capabilities}
        capabilityKey="standard"
        onCapabilityChange={onCapabilityChange}
      />,
    )

    fireEvent.click(screen.getByText('智能适配'))
    fireEvent.click(screen.getByText('专业增强'))
    expect(onRatioChange).toHaveBeenCalledWith('auto')
    expect(onCapabilityChange).toHaveBeenCalledWith('professional')
  })
})
