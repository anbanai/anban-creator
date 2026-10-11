import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { http } from '@/lib/http-client'
import { wechatAccountAnalyticsApi, type WechatAccountOverview, type WechatTrafficSource } from '@/lib/wechat-account-analytics'
import WechatAccountOverviewPanel from './WechatAccountOverview'
import WechatTrafficSources from './WechatTrafficSources'

vi.mock('@/lib/http-client', async importOriginal => ({ ...await importOriginal<typeof import('@/lib/http-client')>(), http: { get: vi.fn() } }))

const overview: WechatAccountOverview = {
  revision: 12,
  from: '2026-09-01',
  to: '2026-09-03',
  data_through: '2026-09-03',
  covered_dates: ['2026-09-01', '2026-09-02', '2026-09-03'],
  coverage: { covered_days: 3, expected_days: 3, complete: true },
  series: [
    { date: '2026-09-01', read_users: 10, share_users: 0 },
    { date: '2026-09-02', read_users: 20, share_users: null },
    { date: '2026-09-03', read_users: 30, share_users: 2 },
  ],
  totals: { read_users: 60, share_users: 2 },
  comparisons: {
    read_users: {
      previous_day: { label: '较前一日', value: 0.5 },
      previous_week: { label: '较上周同期', value: null },
    },
  },
}

describe('WeChat account analytics contract', () => {
  beforeEach(() => vi.clearAllMocks())

  it('calls the account overview endpoint with the selected inclusive range', async () => {
    vi.mocked(http.get).mockResolvedValue({ data: { data: overview } })
    await expect(wechatAccountAnalyticsApi.overview('project-1', { from: '2026-09-01', to: '2026-09-03' })).resolves.toEqual(overview)
    expect(http.get).toHaveBeenCalledWith('/projects/project-1/wechat-analytics/overview', expect.objectContaining({ params: { from: '2026-09-01', to: '2026-09-03' } }))
  })

  it('renders zero separately from unavailable values and exposes incomplete coverage', () => {
    function Fixture() {
      const [metric, setMetric] = useState<'read_users' | 'share_users'>('read_users')
      return <WechatAccountOverviewPanel overview={{ ...overview, coverage: { covered_days: 2, expected_days: 3, complete: false }, covered_dates: ['2026-09-01', '2026-09-03'] }} loading={false} error={undefined} selectedMetric={metric} onMetricChange={value => setMetric(value as 'read_users' | 'share_users')} />
    }
    render(<Fixture />)
    expect(screen.getByText('数据覆盖 2 / 3 天')).toBeInTheDocument()
    expect(screen.getByText('60')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /分享人数2/ })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /分享人数2/ }))
    expect(within(screen.getByText('查看趋势明细').closest('details')!).getByText('0')).toBeInTheDocument()
    const coverageStatus = screen.getAllByRole('status').find(element => element.textContent?.includes('数据覆盖'))
    expect(coverageStatus).toHaveTextContent('数据覆盖 2 / 3 天')
    expect(coverageStatus).toHaveTextContent('比较值暂不可用。')
  })

  it('renders source shares from the server and filters source without deriving percentages', async () => {
    vi.mocked(http.get).mockResolvedValue({ data: { data: { revision: 12, metric: 'read_users', total: 100, coverage: { covered_days: 2, expected_days: 2, complete: true }, data_quality: null, items: [{ source: 'message', label: '公众号消息', value: 60, share: 0.6 }, { source: 'search', label: '搜一搜', value: 40, share: 0.4 }] } } })
    function Fixture() {
      const [source, setSource] = useState<WechatTrafficSource>('all')
      return <WechatTrafficSources projectId="project-1" from="2026-09-01" to="2026-09-02" source={source} onSourceChange={setSource} />
    }
    render(<Fixture />)
    expect(await screen.findByText('公众号消息')).toBeInTheDocument()
    expect(screen.getByText('60.00%')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('combobox', { name: '流量来源' }))
    const searchOption = await screen.findByRole('option', { name: '搜一搜' })
    fireEvent.pointerDown(searchOption, { pointerType: 'mouse' })
    fireEvent.click(searchOption)
    await waitFor(() => expect(http.get).toHaveBeenCalledWith('/projects/project-1/wechat-analytics/traffic-sources', expect.objectContaining({ params: expect.objectContaining({ source: 'search' }) })) )
    expect(screen.getAllByText('搜一搜').length).toBeGreaterThan(0)
  })
})
