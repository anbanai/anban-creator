import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { render } from '@/test/test-utils'
import { MetricStrip, ProgressRing, StatusPill } from './index'

describe('workspace visual primitives', () => {
  it('renders status pills with visible text and a semantic tone', () => {
    render(<StatusPill status="paused" label="已暂停" />)

    expect(screen.getByText('已暂停')).toBeInTheDocument()
    expect(screen.getByTestId('status-pill')).toHaveAttribute('data-tone', 'warning')
    expect(screen.getByTestId('status-pill').querySelector('svg')).toBeInTheDocument()
  })

  it('animates only running states and keeps pending distinct', () => {
    const { rerender } = render(<StatusPill status="pending" label="待执行" />)
    expect(screen.getByTestId('status-pill').querySelector('svg')).not.toHaveClass('animate-spin')
    rerender(<StatusPill status="running" label="运行中" />)
    expect(screen.getByTestId('status-pill').querySelector('svg')).toHaveClass('animate-spin')
    rerender(<StatusPill status="cancelled" label="已取消" />)
    expect(screen.getByTestId('status-pill').querySelector('svg')).not.toHaveClass('animate-spin')
  })

  it('renders metrics as a labelled definition strip', () => {
    render(
      <MetricStrip
        metrics={[
          { label: '任务', value: 12 },
          { label: '完成', value: '75%' },
        ]}
      />,
    )

    expect(screen.getByRole('group', { name: '指标概览' })).toBeInTheDocument()
    expect(screen.getByText('任务')).toBeInTheDocument()
    expect(screen.getByText('12')).toBeInTheDocument()
    expect(screen.getByText('75%')).toBeInTheDocument()
  })

  it('clamps progress and exposes an accessible progressbar without animation', () => {
    render(<ProgressRing value={140} label="任务完成率" />)

    const ring = screen.getByRole('progressbar', { name: '任务完成率：100%' })
    expect(ring).toHaveAttribute('aria-valuenow', '100')
    expect(ring).toHaveAttribute('data-progress', '100')
    expect(ring).toHaveAttribute('data-motion', 'reduced-safe')
  })
})
