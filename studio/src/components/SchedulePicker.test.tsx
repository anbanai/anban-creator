import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import SchedulePicker from './SchedulePicker'

describe('SchedulePicker', () => {
  it('wraps weekday controls into a responsive grid', () => {
    render(<SchedulePicker value="0 9 * * 1,3,5" onChange={vi.fn()} />)

    const monday = screen.getByRole('button', { name: '周一' })
    expect(monday.parentElement).toHaveClass('grid', 'grid-cols-4', 'sm:grid-cols-7')
  })
})
