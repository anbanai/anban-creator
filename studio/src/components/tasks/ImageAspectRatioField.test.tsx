import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ImageAspectRatioField } from './ImageAspectRatioField'

describe('ImageAspectRatioField', () => {
  it('renders only the supplied business ratios', () => {
    render(
      <ImageAspectRatioField
        value="3:4"
        defaultValue="3:4"
        ratios={['3:4', '1:1', '4:3']}
        onChange={vi.fn()}
      />,
    )

    expect(screen.getByText('智能适配')).toBeInTheDocument()
    expect(screen.getByText('3:4 默认')).toBeInTheDocument()
    expect(screen.getByText('1:1')).toBeInTheDocument()
    expect(screen.getByText('4:3')).toBeInTheDocument()
    expect(screen.queryByText('16:9')).not.toBeInTheDocument()
    expect(screen.queryByText('2:3')).not.toBeInTheDocument()
  })

  it('emits auto as an explicit value', () => {
    const onChange = vi.fn()
    render(
      <ImageAspectRatioField
        value="1:1"
        defaultValue="1:1"
        ratios={['1:1', '3:4', '4:3', '16:9']}
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByText('智能适配'))
    expect(onChange).toHaveBeenCalledWith('auto')
  })
})
