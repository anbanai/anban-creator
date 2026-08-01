import { act, fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { render } from '@/test/test-utils'
import { ImageAspectRatioField } from './ImageAspectRatioField'

describe('ImageAspectRatioField', () => {
  it('renders the controlled selection and reports a selected ratio', () => {
    const onChange = vi.fn()

    render(<ImageAspectRatioField value="3:4" defaultValue="3:4" onChange={onChange} />)

    expect(screen.getByRole('radiogroup')).toHaveClass('overflow-x-clip')
    expect(screen.getAllByRole('radio')).toHaveLength(9)

    const vertical = screen.getByRole('radio', { name: '3:4 vertical default' })
    const square = screen.getByRole('radio', { name: '1:1 square' })
    const horizontal = screen.getByRole('radio', { name: '4:3 horizontal' })
    const widescreen = screen.getByRole('radio', { name: '16:9 widescreen' })

    expect(vertical).toBeChecked()
    expect(square).not.toBeChecked()
    expect(horizontal).not.toBeChecked()
    expect(widescreen).not.toBeChecked()

    fireEvent.click(screen.getByLabelText('16:9 widescreen', { selector: 'label' }))

    expect(onChange).toHaveBeenCalledWith('16:9')
  })

  it('uses Base UI keyboard navigation and follows the controlled value', async () => {
    const onChange = vi.fn()
    const view = render(<ImageAspectRatioField value="3:4" defaultValue="3:4" onChange={onChange} />)
    const vertical = screen.getByRole('radio', { name: '3:4 vertical default' })
    const square = screen.getByRole('radio', { name: '1:1 square' })

    vertical.focus()
    await act(async () => {
      fireEvent.keyDown(vertical, { key: 'ArrowRight' })
      await Promise.resolve()
    })

    expect(onChange).toHaveBeenCalledWith('1:1')
    expect(square).toHaveFocus()

    view.rerender(<ImageAspectRatioField value="1:1" defaultValue="3:4" onChange={onChange} />)

    expect(vertical).not.toBeChecked()
    expect(square).toBeChecked()
  })

  it('keeps the field unselected while marking the configured default', () => {
    render(<ImageAspectRatioField value="" defaultValue="1:1" onChange={vi.fn()} />)

    expect(screen.getByRole('radio', { name: '3:4 vertical' })).not.toBeChecked()
    expect(screen.getByRole('radio', { name: '1:1 square default' })).not.toBeChecked()
    expect(screen.getByRole('radio', { name: '4:3 horizontal' })).not.toBeChecked()
    expect(screen.getByRole('radio', { name: '16:9 widescreen' })).not.toBeChecked()
  })

  it('exposes smart adaptation and disables ratios outside the selected capability', () => {
    const onChange = vi.fn()
    render(
      <ImageAspectRatioField
        value=""
        defaultValue=""
        supportedSizes={['auto', '1:1', '3:2', '2:3']}
        onChange={onChange}
      />,
    )

    expect(screen.getByRole('radio', { name: '智能适配' })).toBeChecked()
    expect(screen.getByRole('radio', { name: '3:2 horizontal' })).not.toBeDisabled()
    expect(screen.getByRole('radio', { name: '16:9 widescreen' })).toHaveAttribute('aria-disabled', 'true')
    fireEvent.click(screen.getByLabelText('16:9 widescreen', { selector: 'label' }))
    expect(onChange).not.toHaveBeenCalled()
  })
})
