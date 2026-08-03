import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { render } from '@/test/test-utils'
import { ComposerQuantityControl } from './ComposerQuantityControl'

describe('ComposerQuantityControl', () => {
  it('opens the shared stepper from a compact quantity summary', async () => {
    const onChange = vi.fn()
    render(
      <ComposerQuantityControl
        label="任务数量"
        value={2}
        min={1}
        max={5}
        onChange={onChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '任务数量：2' }))
    fireEvent.click(await screen.findByRole('button', { name: '增加任务数量' }))

    expect(onChange).toHaveBeenCalledWith(3)
  })

  it('renders the static shared stepper without a popover trigger at a fixed limit', () => {
    render(
      <ComposerQuantityControl
        label="图片数量"
        value={1}
        min={1}
        max={1}
        onChange={vi.fn()}
      />,
    )

    expect(screen.getByLabelText('图片数量：1，当前能力上限')).toHaveTextContent(
      '图片数量 1 · 当前能力上限',
    )
    expect(screen.queryByRole('button', { name: /图片数量/ })).not.toBeInTheDocument()
  })
})
