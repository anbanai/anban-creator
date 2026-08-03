import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import { QuantityStepper } from './QuantityStepper'

describe('QuantityStepper', () => {
  it('reports changes relative to the current controlled value', () => {
    const onChange = vi.fn()
    render(
      <QuantityStepper
        label="任务数量"
        value={2}
        min={1}
        max={5}
        onChange={onChange}
      />,
    )

    expect(screen.getByLabelText('任务数量：2')).toHaveTextContent('2')
    fireEvent.click(screen.getByRole('button', { name: '增加任务数量' }))
    fireEvent.click(screen.getByRole('button', { name: '减少任务数量' }))

    expect(onChange).toHaveBeenNthCalledWith(1, 3)
    expect(onChange).toHaveBeenNthCalledWith(2, 1)
  })

  it('disables boundary actions and never reports an out-of-range value', () => {
    const onChange = vi.fn()
    const { rerender } = render(
      <QuantityStepper label="任务数量" value={1} min={1} max={5} onChange={onChange} />,
    )

    fireEvent.click(screen.getByRole('button', { name: '减少任务数量' }))
    expect(screen.getByRole('button', { name: '减少任务数量' })).toBeDisabled()
    expect(onChange).not.toHaveBeenCalled()

    rerender(
      <QuantityStepper label="任务数量" value={5} min={1} max={5} onChange={onChange} />,
    )
    fireEvent.click(screen.getByRole('button', { name: '增加任务数量' }))
    expect(screen.getByRole('button', { name: '增加任务数量' })).toBeDisabled()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('disables both actions when the whole control is disabled', () => {
    const onChange = vi.fn()
    render(
      <QuantityStepper
        label="任务数量"
        value={2}
        min={1}
        max={5}
        onChange={onChange}
        disabled
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '增加任务数量' }))
    fireEvent.click(screen.getByRole('button', { name: '减少任务数量' }))
    for (const button of screen.getAllByRole('button')) expect(button).toBeDisabled()
    expect(onChange).not.toHaveBeenCalled()
  })

  it('renders a static capability-limit label when quantity cannot change', () => {
    render(
      <QuantityStepper label="图片数量" value={1} min={1} max={1} onChange={vi.fn()} />,
    )

    expect(screen.getByLabelText('图片数量：1，当前能力上限')).toHaveTextContent(
      '图片数量 1 · 当前能力上限',
    )
    expect(screen.queryByRole('button', { name: /图片数量/ })).not.toBeInTheDocument()
  })
})
