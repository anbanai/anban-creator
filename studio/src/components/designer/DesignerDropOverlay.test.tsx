import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import DesignerDropOverlay from './DesignerDropOverlay'

describe('DesignerDropOverlay', () => {
  it('renders nothing while inactive', () => {
    render(
      <DesignerDropOverlay
        active={false}
        remainingCapacity={16}
      />,
    )

    expect(screen.queryByTestId('designer-drop-overlay')).not.toBeInTheDocument()
  })

  it('shows the incoming reference count and remaining capacity', () => {
    render(
      <DesignerDropOverlay
        active
        incomingCount={3}
        remainingCapacity={13}
      />,
    )

    expect(screen.getByText('释放以添加 3 张参考图')).toBeInTheDocument()
    expect(screen.getByText('当前模型还可添加 13 张')).toBeInTheDocument()
  })

  it('shows generic action copy when the incoming count is unknown', () => {
    render(
      <DesignerDropOverlay
        active
        remainingCapacity={4}
      />,
    )

    expect(screen.getByText('释放以添加参考图')).toBeInTheDocument()
  })
})
