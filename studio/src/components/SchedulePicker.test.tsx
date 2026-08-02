import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import SchedulePicker from './SchedulePicker'

describe('SchedulePicker', () => {
  it('renders a readable weekly summary in stable Monday-first order', () => {
    render(<SchedulePicker value="0 20 * * 5,1,3" onChange={vi.fn()} />)

    const monday = screen.getByRole('button', { name: '周一' })
    expect(monday.parentElement).toHaveClass('grid', 'grid-cols-7')
    expect(screen.getByText('每周一、三、五 20:00 自动执行')).toBeInTheDocument()
  })

  it('marks an empty weekly selection invalid without emitting an invalid cron', () => {
    const onChange = vi.fn()
    const onValidityChange = vi.fn()
    render(
      <SchedulePicker
        value="0 20 * * 1"
        onChange={onChange}
        onValidityChange={onValidityChange}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: '周一' }))

    expect(screen.getByRole('alert')).toHaveTextContent('请至少选择一天')
    expect(onValidityChange).toHaveBeenLastCalledWith(false)
    expect(onChange).not.toHaveBeenCalledWith('0 20 * * ')
  })

  it('switches between daily and weekly schedules', () => {
    const onChange = vi.fn()
    render(<SchedulePicker value="0 9 * * *" onChange={onChange} />)

    expect(screen.getByText('每天 09:00 自动执行')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '每周' }))
    expect(screen.getByText('每周一 09:00 自动执行')).toBeInTheDocument()
    expect(onChange).toHaveBeenCalledWith('0 9 * * 1')
  })

  it.each(['0 9 * * 1-5', '0 9 * * 7', '75 99 * * *'])(
    'normalizes unsupported cron %s to a safe daily schedule',
    (cron) => {
      const onChange = vi.fn()
      render(<SchedulePicker value={cron} onChange={onChange} />)

      expect(screen.getByText('每天 09:00 自动执行')).toBeInTheDocument()
      expect(onChange).toHaveBeenCalledWith('0 9 * * *')
    },
  )

  it('labels the time picker trigger with its purpose', () => {
    render(<SchedulePicker value="0 9 * * *" onChange={vi.fn()} />)

    expect(screen.getByRole('button', { name: '执行时间' })).toHaveTextContent('09:00')
  })
})
