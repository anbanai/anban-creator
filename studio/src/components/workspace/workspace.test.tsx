import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { render } from '@/test/test-utils'
import { MetricStrip, ProgressRing, StatusPill, WorkspaceSubnav } from './index'

describe('workspace visual primitives', () => {
  it('renders a compact subnav with an active accessible route', () => {
    window.history.pushState({}, '', '/projects')
    render(
      <WorkspaceSubnav
        items={[
          { label: '项目', href: '/projects' },
          { label: '计划', href: '/plans' },
        ]}
      />,
    )

    expect(screen.getByRole('navigation', { name: '工作区导航' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '项目' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: '计划' })).not.toHaveAttribute('aria-current')
  })

  it('renders status pills with visible text and a semantic tone', () => {
    render(<StatusPill status="paused" label="已暂停" />)

    expect(screen.getByText('已暂停')).toBeInTheDocument()
    expect(screen.getByTestId('status-pill')).toHaveAttribute('data-tone', 'warning')
    expect(screen.getByTestId('status-pill').querySelector('svg')).toBeInTheDocument()
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

  it('keeps subnav links keyboard activatable', () => {
    window.history.pushState({}, '', '/projects')
    render(<WorkspaceSubnav items={[{ label: '项目', href: '/projects' }]} />)
    const link = screen.getByRole('link', { name: '项目' })
    link.focus()
    fireEvent.keyDown(link, { key: 'Enter' })
    expect(link).toHaveFocus()
  })
})
