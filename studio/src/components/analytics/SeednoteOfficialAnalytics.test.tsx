import { describe, expect, it, vi } from 'vitest'
import { fireEvent, screen } from '@testing-library/react'
import { render } from '@/test/test-utils'
import type { AnalyticsOverview } from '@/lib/content-analytics'
import SeednoteOfficialAnalytics from './SeednoteOfficialAnalytics'

vi.mock('@/components/analytics/AnalyticsTrend', () => ({ default: ({ selectedMetric }: { selectedMetric: string }) => <div data-testid="trend">趋势：{selectedMetric}</div> }))

const overview: AnalyticsOverview = {
  revision: 1,
  updated_at: '2026-09-30T12:00:00+08:00',
  metric_basis: 'cumulative',
  coverage: { contents: 2 },
  totals: { exposure_count: 0, view_count: null, like_count: 12, follower_gain_count: 3 },
  series: [],
  unavailable_metrics: {},
}

describe('SeednoteOfficialAnalytics', () => {
  it('shows four official views and defaults to exposure count', () => {
    render(<SeednoteOfficialAnalytics data={overview} view="watch" selectedMetric="exposure_count" onViewChange={vi.fn()} onMetricChange={vi.fn()} />)
    expect(screen.getByRole('tab', { name: '观看数据' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '互动数据' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '涨粉数据' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '发布数据' })).toBeInTheDocument()
    expect(screen.getByTestId('trend')).toHaveTextContent('趋势：exposure_count')
    expect(screen.getByRole('button', { name: /曝光数0/ })).toBeInTheDocument()
  })

  it('keeps missing values distinct from zero and marks unavailable metrics', () => {
    render(<SeednoteOfficialAnalytics data={overview} view="watch" selectedMetric="exposure_count" onViewChange={vi.fn()} onMetricChange={vi.fn()} />)
    expect(screen.getByRole('button', { name: /曝光数0/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /观看数—/ })).toBeInTheDocument()
    const unavailable = screen.getByRole('button', { name: /观看总时长待接入/ })
    expect(unavailable).toBeDisabled()
    expect(unavailable).toHaveAttribute('aria-disabled', 'true')
  })

  it('shows explicit unavailable states when source and hour data are absent', () => {
    render(<SeednoteOfficialAnalytics data={overview} view="watch" selectedMetric="exposure_count" onViewChange={vi.fn()} onMetricChange={vi.fn()} />)
    expect(screen.getByText('观看来源数据暂未接入')).toBeInTheDocument()
    expect(screen.getByText('观看时段数据暂未接入')).toBeInTheDocument()
  })

  it('changes view through the tabs', () => {
    const onViewChange = vi.fn()
    render(<SeednoteOfficialAnalytics data={overview} view="watch" selectedMetric="exposure_count" onViewChange={onViewChange} onMetricChange={vi.fn()} />)
    fireEvent.click(screen.getByRole('tab', { name: '互动数据' }))
    expect(onViewChange).toHaveBeenCalledWith('interaction')
  })
})
