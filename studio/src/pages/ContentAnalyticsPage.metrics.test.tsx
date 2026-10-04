import { cloneElement } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { render } from '@/test/test-utils'
import { api } from '@/lib/api'
import { contentAnalyticsApi, type AnalyticsContent, type AnalyticsOverview } from '@/lib/content-analytics'
import ContentAnalyticsPage from './ContentAnalyticsPage'

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'user-1' } }) }))
vi.mock('@/lib/api', () => ({ api: { projects: { list: vi.fn() } } }))
vi.mock('@/lib/content-analytics', async importOriginal => ({ ...await importOriginal<typeof import('@/lib/content-analytics')>(), contentAnalyticsApi: { overview: vi.fn(), contents: vi.fn(), detail: vi.fn(), observations: vi.fn() } }))
// jsdom has no layout; keep the real trend component and chart at a fixed viewport.
vi.mock('recharts', async importOriginal => ({ ...await importOriginal<typeof import('recharts')>(), ResponsiveContainer: ({ children }: { children: React.ReactElement<{ width?: number; height?: number }> }) => cloneElement(children, { width: 800, height: 320 }) }))

const article: AnalyticsContent = { id: 'article-1', title: '已归档公众号文章', content_type: 'wechat-article', date: '2026-08-01', last_stat_date: '2026-09-30', metrics: { read_users: 1286, share_users: 0, read_to_follow_users: null, delivered_users: 9800 } }
const picture: AnalyticsContent = { id: 'picture-1', title: '尚无统计的贴图', content_type: 'wechat-picture', date: '2026-08-02', metrics: { read_users: null, share_users: null, read_to_follow_users: null, delivered_users: null } }
const overview: AnalyticsOverview = {
  revision: 7, updated_at: '2026-09-30T12:00:00+08:00', metric_basis: 'cumulative', coverage: { contents: 2 }, totals: article.metrics,
  series: [
    { date: '2026-09-29', read_users: 1000, share_users: 0, read_to_follow_users: null, delivered_users: 9700 },
    { date: '2026-09-30', read_users: 1286, share_users: 0, read_to_follow_users: null, delivered_users: 9800 },
  ],
  unavailable_metrics: { average_read_active_time: '缺少阅读人数权重', delivery_completion_rate: '缺少送达人数权重' },
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  window.history.replaceState(null, '', '/content-analytics?account=wechat&from=2026-09-01&to=2026-09-30')
  vi.mocked(api.projects.list).mockResolvedValue([{ id: 'wechat', platform: 'wechat', name: '公众号项目' }] as Awaited<ReturnType<typeof api.projects.list>>)
  vi.mocked(contentAnalyticsApi.overview).mockResolvedValue(overview)
  vi.mocked(contentAnalyticsApi.contents).mockImplementation(async (_project, params) => {
    const items = [article, picture].filter(item => !params.content_type || item.content_type === params.content_type)
    return { revision: 7, items, total: items.length, offset: 0, limit: 25 }
  })
  vi.mocked(contentAnalyticsApi.detail).mockResolvedValue({ ...overview, content: article })
  vi.mocked(contentAnalyticsApi.observations).mockResolvedValue({ revision: 7, total: 1, offset: 0, limit: 25, items: [{ id: 'observation-1', stat_date: '2026-09-30', source: 'wechat_import', metric_basis: 'cumulative', effective_at: '2026-09-30T12:00:00+08:00', received_at: '2026-09-30T12:01:00+08:00', metrics: { ...article.metrics, average_read_active_time: 12.5, delivery_completion_rate: 0.75 } }] })
})

describe('persisted WeChat analytics display', () => {
  it('preserves nonzero, zero and missing values in four summary metrics, trend details and list columns', async () => {
    render(<ContentAnalyticsPage />)
    await screen.findByRole('button', { name: article.title })
    const summary = within(screen.getByLabelText('关键指标'))
    expect(summary.getByRole('button', { name: '阅读人数1,286' })).toBeInTheDocument()
    expect(summary.getByRole('button', { name: '分享人数0' })).toBeInTheDocument()
    expect(summary.getByRole('button', { name: '阅读后关注—' })).toBeInTheDocument()
    expect(summary.getByRole('button', { name: '送达人数9,800' })).toBeInTheDocument()
    const row = screen.getByRole('button', { name: article.title }).closest('tr')!
    expect(within(row).getAllByRole('cell').slice(2, 6).map(cell => cell.textContent)).toEqual(['1,286', '0', '—', '9,800'])
    const trend = within(screen.getByText('查看趋势明细').closest('details')!)
    expect(trend.getByText('1,286')).toBeInTheDocument()
    fireEvent.click(summary.getByRole('button', { name: '分享人数0' }))
    expect(screen.getByRole('img', { name: '整体趋势：分享人数' })).toBeInTheDocument()
    expect(trend.getAllByText('0')).toHaveLength(2)
    fireEvent.click(summary.getByRole('button', { name: '送达人数9,800' }))
    expect(trend.getByText('9,800')).toBeInTheDocument()
    fireEvent.click(summary.getByRole('button', { name: '阅读后关注—' }))
    expect(screen.queryByRole('img', { name: '整体趋势：阅读后关注' })).not.toBeInTheDocument()
    expect(screen.getByText('当前时间范围暂无数据，请调整日期或导入数据。')).toBeInTheDocument()
  })

  it('uses canonical content types for labels and filters', async () => {
    render(<ContentAnalyticsPage />)
    await screen.findByRole('button', { name: article.title })
    const row = screen.getByRole('button', { name: article.title }).closest('tr')!
    expect(within(row).getByText('公众号文章')).toBeInTheDocument()
    const filter = screen.getByRole('combobox', { name: '内容类型' })
    expect(within(filter).getByRole('option', { name: '公众号文章' })).toHaveValue('wechat-article')
    expect(within(filter).getByRole('option', { name: '公众号贴图' })).toHaveValue('wechat-picture')
    fireEvent.change(filter, { target: { value: 'wechat-picture' } })
    await waitFor(() => expect(screen.queryByRole('button', { name: article.title })).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: picture.title })).toBeInTheDocument()
  })

  it('never substitutes publication or creation dates for the data-as-of date', async () => {
    render(<ContentAnalyticsPage />)
    await screen.findByRole('button', { name: picture.title })
    const cells = (title: string) => within(screen.getByRole('button', { name: title }).closest('tr')!).getAllByRole('cell')
    expect(cells(article.title)[6]).toHaveTextContent('2026-09-30')
    expect(cells(picture.title)[6]).toHaveTextContent('—')
    expect(screen.queryByText('2026-08-02')).not.toBeInTheDocument()
  })

  it('uses Chinese metric metadata for unavailable metrics and formatted observations', async () => {
    window.history.replaceState(null, '', '/content-analytics?account=wechat&content=article-1&from=2026-09-01&to=2026-09-30')
    render(<ContentAnalyticsPage />)
    await screen.findByRole('heading', { name: article.title })
    expect(screen.getByText('平均阅读时长：缺少阅读人数权重；送达完成率：缺少送达人数权重')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '查看观测记录' }))
    expect(await screen.findByText(/阅读人数：1,286 · 分享人数：0 · 阅读后关注：— · 送达人数：9,800 · 平均阅读时长：12.5 秒 · 送达完成率：75.00%/)).toBeInTheDocument()
    expect(screen.queryByText(/average_read_active_time|delivery_completion_rate/)).not.toBeInTheDocument()
  })

  it.each(['overview', 'contents', 'detail'] as const)('shows %s API errors instead of an empty result', async endpoint => {
    if (endpoint === 'detail') window.history.replaceState(null, '', '/content-analytics?account=wechat&content=article-1')
    vi.mocked(contentAnalyticsApi[endpoint]).mockRejectedValue(new Error('统计服务暂不可用'))
    render(<ContentAnalyticsPage />)
    expect(await screen.findByText('数据加载失败')).toBeInTheDocument()
    expect(screen.getByText('统计服务暂不可用')).toBeInTheDocument()
    expect(screen.queryByText('暂无分析数据，导入平台数据后即可查看。')).not.toBeInTheDocument()
  })
})
