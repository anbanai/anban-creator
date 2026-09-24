import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { render } from '@/test/test-utils'
import ContentDataPage from './ContentDataPage'
import { loadContentAnalytics } from '@/lib/content-analytics'
import { defaultAnalyticsPeriod } from '@/lib/analytics-period'

vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => ({ user: { id: 'user-1' } }) }))
const projects = [{ id: 'wechat', platform: 'article', name: '公众号账号', avatar_url: '' }, { id: 'notes', platform: 'seednote', name: '生活笔记', avatar_url: '' }]
vi.mock('@/lib/api', () => ({ api: { projects: { list: vi.fn(async () => projects) } } }))
vi.mock('@/components/analytics/ContentImportDialog', () => ({ default: ({ project, onClose }: { project: {name: string}; onClose: () => void }) => <div role="dialog">导入账号：{project.name}<button onClick={onClose}>关闭导入</button></div> }))
vi.mock('@/components/analytics/ContentImportHistory', () => ({ default: ({ onClose }: { onClose: () => void }) => <div role="dialog">导入历史<button onClick={onClose}>关闭历史</button></div> }))
vi.mock('@/components/analytics/AnalyticsTrend', () => ({ default: ({ selectedMetric }: {selectedMetric:string}) => <div aria-label="趋势图">{selectedMetric}</div> }))
vi.mock('@/lib/content-analytics', () => ({ metricsFor: () => [{ key: 'views', label: '阅读量', color: '#ac6747', summary: true, kind: 'count', format: (v: number) => v.toLocaleString() }], descriptionFor: () => '按范围内最新记录统计', loadContentAnalytics: vi.fn() }))
const contents = Array.from({ length: 27 }, (_, i) => ({ id: `task:${i}`, title: `内容标题 ${String(i).padStart(2, '0')}`, contentType: i % 2 ? '视频' : '图文', metrics: { views: i === 0 ? null : i }, date: '2026-09-23T16:00:00Z' }))
beforeEach(() => {
  localStorage.clear()
  window.history.replaceState(null, '', '/content-data')
  vi.mocked(loadContentAnalytics).mockImplementation(async (_project, _period, contentId) => ({ contents, selected: contents.find(c => c.id === contentId), totals: { views: 351 }, series: [], availableDates: ['2026-09-24'] }))
})

describe('ContentDataPage', () => {
  it('records the initial date range and granularity in the URL for later reloads', async () => {
    const expected = defaultAnalyticsPeriod()
    render(<ContentDataPage />)
    await waitFor(() => {
      const query = new URLSearchParams(location.search)
      expect(query.get('account')).toBe('wechat')
      expect(query.get('from')).toBe(expected.from)
      expect(query.get('to')).toBe(expected.to)
      expect(query.get('preset')).toBe(expected.preset)
      expect(query.get('granularity')).toBe(expected.granularity)
    })
  })
  it('uses URL account before remembered account and retains dates and granularity', async () => {
    localStorage.setItem('content-data:account:user-1', 'wechat')
    window.history.replaceState(null, '', '/content-data?account=notes&from=2026-08-01&to=2026-08-31&granularity=week')
    render(<ContentDataPage />)
    await waitFor(() => expect(loadContentAnalytics).toHaveBeenCalledWith(expect.objectContaining({ id: 'notes' }), expect.objectContaining({ from: '2026-08-01', to: '2026-08-31', granularity: 'week' }), undefined))
    expect(screen.getByRole('button', { name: /选择账号.*生活笔记/ })).toBeInTheDocument()
  })
  it('remembers accounts per user and offers grouped searchable accounts', async () => {
    localStorage.setItem('content-data:account:user-1', 'notes')
    render(<ContentDataPage />)
    fireEvent.click(await screen.findByRole('button', { name: /选择账号.*生活笔记/ }))
    fireEvent.change(screen.getByRole('textbox', { name: '搜索账号' }), { target: { value: '公众号' } })
    fireEvent.click(screen.getByRole('button', { name: '公众号账号 公众号' }))
    await waitFor(() => expect(new URLSearchParams(location.search).get('account')).toBe('wechat'))
    expect(localStorage.getItem('content-data:account:user-1')).toBe('wechat')
  })
  it('paginates 25 rows and preserves filters after single-content analysis', async () => {
    render(<ContentDataPage />)
    const table = await screen.findByRole('table')
    await waitFor(() => expect(within(table).getAllByRole('row')).toHaveLength(26))
    fireEvent.click(screen.getByRole('button', { name: '下一页' }))
    expect(within(table).getAllByRole('row')).toHaveLength(3)
    fireEvent.change(screen.getByRole('textbox', { name: '搜索内容' }), { target: { value: '内容标题 00' } })
    expect(screen.getByText('—')).toBeInTheDocument()
    expect(within(table).getByText('2026-09-24')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '内容标题 00' }))
    expect(await screen.findByRole('heading', { name: '内容标题 00' })).toBeInTheDocument()
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(new URLSearchParams(location.search).get('content')).toBe('task:0')
    fireEvent.click(screen.getByRole('button', { name: '返回全部内容' }))
    expect(screen.getByRole('textbox', { name: '搜索内容' })).toHaveValue('内容标题 00')
    expect(screen.getByRole('table')).toBeInTheDocument()
  })
  it('opens import for the current account from the unified shell', async () => {
    render(<ContentDataPage />)
    await waitFor(() => expect(screen.getByRole('button', { name: '导入数据' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '导入数据' }))
    expect(screen.getByRole('dialog')).toHaveTextContent('公众号账号')
    fireEvent.click(screen.getByRole('button', { name: '关闭导入' }))
    expect(screen.getByRole('heading', { name: '内容数据' })).toBeInTheDocument()
  })
  it('restores a refreshed content URL and makes unavailable selections recoverable', async () => {
    window.history.replaceState(null, '', '/content-data?account=wechat&content=task:missing&q=保留搜索&granularity=month')
    render(<ContentDataPage />)
    expect(await screen.findByRole('heading', { name: '该内容暂不可用' })).toBeInTheDocument()
    expect(screen.queryByText('351')).not.toBeInTheDocument()
    expect(new URLSearchParams(location.search).get('granularity')).toBe('month')
    fireEvent.click(screen.getByRole('button', { name: '返回全部内容' }))
    expect(screen.getByRole('textbox', { name: '搜索内容' })).toHaveValue('保留搜索')
  })
  it('preserves table scroll and filter state after switching content', async () => {
    const scrollTo = vi.fn()
    render(<main id="main-content"><ContentDataPage /></main>)
    const main = document.getElementById('main-content')!
    main.scrollTo = scrollTo
    main.scrollTop = 620
    fireEvent.click(await screen.findByRole('button', { name: '内容标题 26' }))
    expect(await screen.findByRole('heading', { name: '内容标题 26' })).toHaveFocus()
    fireEvent.click(screen.getByRole('button', { name: '切换内容' }))
    fireEvent.change(screen.getByRole('textbox', { name: '搜索要切换的内容' }), { target: { value: '内容标题 01' } })
    fireEvent.click(screen.getByRole('button', { name: '内容标题 01' }))
    expect(await screen.findByRole('heading', { name: '内容标题 01' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '返回全部内容' }))
    await waitFor(() => expect(scrollTo).toHaveBeenCalledWith({ top: 620 }))
  })
  it('shows query errors with retry', async () => {
    vi.mocked(loadContentAnalytics).mockRejectedValue(new Error('服务暂不可用'))
    render(<ContentDataPage />)
    expect(await screen.findByRole('alert')).toHaveTextContent('服务暂不可用')
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  })
  it('offers date recovery for an empty single-content range even when other content has data', async () => {
    window.history.replaceState(null, '', '/content-data?account=wechat&content=task:0&from=2026-08-01&to=2026-08-31')
    vi.mocked(loadContentAnalytics).mockResolvedValue({ contents, selected: contents[0], totals: { views: null }, series: [], availableDates: ['2026-09-24'] })
    render(<ContentDataPage />)
    fireEvent.click(await screen.findByRole('button', { name: '查看全部数据日期' }))
    await waitFor(() => expect(new URLSearchParams(location.search).get('from')).toBe('2026-09-24'))
    expect(new URLSearchParams(location.search).get('content')).toBe('task:0')
  })
  it('restores time controls through browser back and forward', async () => {
    window.history.replaceState(null, '', '/content-data?account=wechat&from=2026-08-01&to=2026-08-31&preset=custom&granularity=day')
    render(<ContentDataPage />)
    await screen.findByRole('button', { name: '内容标题 26' })
    fireEvent.click(screen.getByRole('button', { name: '按周' }))
    await waitFor(() => expect(new URLSearchParams(location.search).get('granularity')).toBe('week'))
    window.history.back()
    await waitFor(() => expect(loadContentAnalytics).toHaveBeenLastCalledWith(expect.anything(), expect.objectContaining({ granularity: 'day', from: '2026-08-01' }), undefined))
    window.history.forward()
    await waitFor(() => expect(loadContentAnalytics).toHaveBeenLastCalledWith(expect.anything(), expect.objectContaining({ granularity: 'week', from: '2026-08-01' }), undefined))
  })
})
