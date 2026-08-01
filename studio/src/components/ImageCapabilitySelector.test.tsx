import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ImageCapabilitySelector } from './ImageCapabilitySelector'
import { render } from '@/test/test-utils'

describe('ImageCapabilitySelector', () => {
	 it('does not present the first sorted option as an implicit default', () => {
		render(<ImageCapabilitySelector
			options={[
				{ key: 'standard', display_name: '标准图像', sort_order: 1, enabled: true, price_available: true },
				{ key: 'professional', display_name: '专业增强', sort_order: 2, enabled: true, price_available: true },
			]}
			value=""
			onChange={vi.fn()}
		/>)

		expect(screen.getByRole('combobox')).toHaveTextContent('请选择图像能力')
		expect(screen.getByRole('combobox')).not.toHaveTextContent('标准图像')
	})

  it('does not disguise a retired capability as the first available option', () => {
    render(<ImageCapabilitySelector
      options={[{ key: 'standard', display_name: '标准图像', enabled: true, price_available: true }]}
      value="retired"
      onChange={vi.fn()}
    />)

    expect(screen.getByRole('combobox')).toHaveTextContent('已停用能力')
    expect(screen.getByRole('combobox')).not.toHaveTextContent('标准图像')
    fireEvent.click(screen.getByRole('combobox'))
    expect(screen.getByText('标准图像')).toBeInTheDocument()
  })

  it('does not allow selecting a capability whose price is unavailable', () => {
    const onChange = vi.fn()
    render(<ImageCapabilitySelector
      options={[{ key: 'standard', display_name: '标准图像', enabled: true, price_available: false }]}
      value=""
      onChange={onChange}
    />)

    fireEvent.click(screen.getByRole('combobox'))
    fireEvent.click(screen.getByText('标准图像'))

    expect(onChange).not.toHaveBeenCalled()
  })

  it('does not allow selecting a capability whose enabled flag is missing', () => {
    const onChange = vi.fn()
    render(<ImageCapabilitySelector
      options={[{ key: 'standard', display_name: '标准图像', price_available: true }]}
      value=""
      onChange={onChange}
    />)

    fireEvent.click(screen.getByRole('combobox'))
    fireEvent.click(screen.getByText('标准图像'))

    expect(onChange).not.toHaveBeenCalled()
  })
})
