import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { ImageAspectRatioField } from './ImageAspectRatioField'

describe('ImageAspectRatioField', () => {
  it('renders the controlled selection and reports a selected ratio', () => {
    const onChange = vi.fn()

    render(<ImageAspectRatioField value="3:4" defaultValue="3:4" onChange={onChange} />)

    expect(screen.getByRole('radiogroup')).toBeInTheDocument()
    expect(screen.getAllByRole('radio')).toHaveLength(4)

    const vertical = screen.getByRole('radio', { name: '3:4 vertical default' })
    const square = screen.getByRole('radio', { name: '1:1 square' })
    const horizontal = screen.getByRole('radio', { name: '4:3 horizontal' })
    const widescreen = screen.getByRole('radio', { name: '16:9 widescreen' })

    expect(vertical).toBeChecked()
    expect(square).not.toBeChecked()
    expect(horizontal).not.toBeChecked()
    expect(widescreen).not.toBeChecked()

    fireEvent.click(widescreen)

    expect(onChange).toHaveBeenCalledWith('16:9')
  })

  it('keeps the field unselected while marking the configured default', () => {
    render(<ImageAspectRatioField value="" defaultValue="1:1" onChange={vi.fn()} />)

    expect(screen.getByRole('radio', { name: '3:4 vertical' })).not.toBeChecked()
    expect(screen.getByRole('radio', { name: '1:1 square default' })).not.toBeChecked()
    expect(screen.getByRole('radio', { name: '4:3 horizontal' })).not.toBeChecked()
    expect(screen.getByRole('radio', { name: '16:9 widescreen' })).not.toBeChecked()
  })
})
